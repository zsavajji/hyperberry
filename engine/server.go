package main

import (
	"bufio"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	waLog "github.com/polymorfa/hypermeow/util/log"
)

// Error is a failure with a code the UI can switch on.
type Error struct{ Code, Message string }

func (e *Error) Error() string { return e.Message }

// Handler answers one request; req is the whole request line.
type Handler func(req json.RawMessage) (any, error)

// Server is the UI link of PLAN.md §5: newline-delimited JSON over loopback TCP.
type Server struct {
	dir   string
	log   waLog.Logger
	hello func() any
	// afterHello gives an event to send right after a hello reply (the live call, step 7.2), or nil.
	afterHello func() (string, map[string]any)
	// audio hands an audio link's connection to the live call. It writes ok first, and says false,
	// writing nothing, when id is not the live call.
	audio func(id string, conn net.Conn, ok []byte) bool
	// video is the same for a video link (step 7c.2).
	video    func(id string, conn net.Conn, ok []byte) bool
	token    string
	ln       net.Listener
	handlers map[string]Handler
	async    map[string]bool // the cmds in handlers that run in their own goroutine (HandleAsync)

	mu      sync.Mutex
	clients map[*client]bool
	closed  bool
	wg      sync.WaitGroup
}

type client struct {
	conn   net.Conn
	out    chan []byte
	done   chan struct{}
	once   sync.Once
	authed atomic.Bool
}

func (c *client) close() {
	c.once.Do(func() {
		close(c.done)
		c.conn.Close()
	})
}

// NewServer listens on an ephemeral loopback port and publishes port and token in
// dataDir/engine.json.
func NewServer(dataDir string, log waLog.Logger, hello func() any) (*Server, error) {
	// TCP, not a Unix socket: PLAN.md §2.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	tok := make([]byte, 16)
	if _, err := rand.Read(tok); err != nil {
		ln.Close()
		return nil, err
	}
	s := &Server{dir: dataDir, log: log, hello: hello, token: hex.EncodeToString(tok), ln: ln,
		handlers: map[string]Handler{}, async: map[string]bool{}, clients: map[*client]bool{}}
	info, _ := json.Marshal(map[string]any{"port": ln.Addr().(*net.TCPAddr).Port, "token": s.token})
	tmp := filepath.Join(dataDir, "engine.json.tmp")
	if err := os.WriteFile(tmp, info, 0o600); err == nil {
		err = os.Rename(tmp, filepath.Join(dataDir, "engine.json"))
	}
	if err != nil {
		ln.Close()
		return nil, err
	}
	return s, nil
}

// Handle registers the handler for cmd; do it before Serve.
func (s *Server) Handle(cmd string, h Handler) { s.handlers[cmd] = h }

// HandleAsync registers a handler that runs in its own goroutine, so a long one (a download) doesn't hold up the
// connection's later requests (its own cancel); do it before Serve. Its reply goes out when it returns.
func (s *Server) HandleAsync(cmd string, h Handler) {
	s.handlers[cmd] = h
	s.async[cmd] = true
}

// Call runs the handler of cmd with req as a UI request would, for the engine's own use (the
// Hub's actions). Like Handle, it is for after the handlers are in.
func (s *Server) Call(cmd string, req any) (any, error) {
	h := s.handlers[cmd]
	if h == nil {
		return nil, &Error{"unknown_cmd", "unknown cmd " + cmd}
	}
	line, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	return h(line)
}

// Serve accepts clients until Close, then returns nil.
func (s *Server) Serve() error {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed {
				return nil
			}
			return err
		}
		c := &client{conn: conn, out: make(chan []byte, 256), done: make(chan struct{})}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			conn.Close()
			return nil
		}
		s.clients[c] = true
		s.wg.Add(2)
		s.mu.Unlock()
		s.log.Infof("UI connected from %s", conn.RemoteAddr())
		go s.write(c)
		go s.read(c)
	}
}

// Emit sends an event to every authenticated client. It never blocks: a client whose queue
// is full is closed, and the UI reconnects and re-reads chats.db.
func (s *Server) Emit(ev string, body map[string]any) {
	line := s.eventLine(ev, body)
	if line == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.clients {
		if !c.authed.Load() {
			continue
		}
		select {
		case c.out <- line:
		default:
			s.log.Warnf("UI too slow, dropping it")
			c.close()
		}
	}
}

// eventLine is the line of an event, nil when it can't be encoded.
func (s *Server) eventLine(ev string, body map[string]any) []byte {
	m := make(map[string]any, len(body)+1)
	for k, v := range body {
		m[k] = v
	}
	m["ev"] = ev
	line, err := json.Marshal(m)
	if err != nil {
		s.log.Warnf("event %s: %v", ev, err)
		return nil
	}
	return append(line, '\n')
}

// helloReader reads a connection a byte at a time until it is authed: the hello line is then the
// last thing the scanner takes, so an audio link's first PCM bytes are still in the socket.
type helloReader struct {
	r      net.Conn
	authed *atomic.Bool
}

func (h helloReader) Read(p []byte) (int, error) {
	if !h.authed.Load() && len(p) > 1 {
		p = p[:1]
	}
	return h.r.Read(p)
}

// Close stops accepting, closes every client and removes engine.json.
func (s *Server) Close() error {
	s.mu.Lock()
	s.closed = true
	for c := range s.clients {
		c.close()
	}
	s.mu.Unlock()
	s.ln.Close()
	s.wg.Wait()
	err := os.Remove(filepath.Join(s.dir, "engine.json"))
	if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	return err
}

func (s *Server) write(c *client) {
	defer s.wg.Done()
	for {
		select {
		case b := <-c.out:
			if _, err := c.conn.Write(b); err != nil {
				c.close()
				return
			}
		case <-c.done:
			return
		}
	}
}

// reply builds the answer line to request id.
func reply(id string, result any, err error) []byte {
	var b []byte
	if err == nil {
		m := map[string]any{"@": id, "ok": true}
		if result != nil {
			m["result"] = result
		}
		b, err = json.Marshal(m) // a result that can't be encoded becomes a "failed" reply
	}
	if err != nil {
		e := &Error{Code: "failed", Message: err.Error()}
		// No network, or WhatsApp's servers out of reach (a failed dial, a DNS lookup): the UI
		// says "offline" in words, not the dial's error (Q10, 0.0.1.125, Wi-Fi and data off).
		var ne net.Error
		if errors.As(err, &ne) {
			e.Code = "offline"
		}
		errors.As(err, &e)
		b, _ = json.Marshal(map[string]any{"@": id, "ok": false,
			"error": map[string]string{"code": e.Code, "message": e.Message}})
	}
	return append(b, '\n')
}

func (s *Server) read(c *client) {
	defer s.wg.Done()
	keep := false // an audio link's connection lives on, in the call
	defer func() {
		s.mu.Lock()
		delete(s.clients, c)
		s.mu.Unlock()
		if keep {
			c.once.Do(func() { close(c.done) }) // the writer goes; the socket stays open
			return
		}
		c.close()
		s.log.Infof("UI disconnected")
	}()
	sc := bufio.NewScanner(helloReader{c.conn, &c.authed})
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		line := append(json.RawMessage(nil), sc.Bytes()...)
		var r struct {
			At    string `json:"@"`
			Cmd   string `json:"cmd"`
			Token string `json:"token"`
			Proto int    `json:"proto"`
			Audio string `json:"audio"`
			// A media link's call id in its hello, but call.answer's bool on a video call: as a
			// string, a video call's answer failed the envelope as a bad request (Q10, 0.0.1.208).
			Video any `json:"video"`
		}
		bad := json.Unmarshal(line, &r) != nil || r.Cmd == ""
		if !c.authed.Load() {
			// Nothing but a good hello gets past here, so the replies go straight to the
			// socket: the writer is not needed yet and the connection closes after them.
			if bad || r.Cmd != "hello" || subtle.ConstantTimeCompare([]byte(r.Token), []byte(s.token)) != 1 {
				s.log.Warnf("UI refused: bad hello")
				c.conn.Write(reply(r.At, nil, &Error{"auth", "hello with the token first"}))
				return
			}
			if r.Proto != 1 {
				c.conn.Write(reply(r.At, nil, &Error{"proto", "this engine speaks proto 1"}))
				return
			}
			video, _ := r.Video.(string)
			if id, kind, attach := r.Audio, "audio", s.audio; id != "" || video != "" {
				if id == "" {
					id, kind, attach = video, "video", s.video
				}
				ok := reply(r.At, nil, nil)
				if attach == nil || !attach(id, c.conn, ok) {
					c.conn.Write(reply(r.At, nil, &Error{"not_found", "no such live call"}))
					return
				}
				keep = true
				s.log.Infof("%s link opened for call %s", kind, id)
				return
			}
			var res any
			if s.hello != nil {
				res = s.hello()
			}
			// The reply goes in the queue before authed is set, so no event overtakes it.
			c.out <- reply(r.At, res, nil)
			c.authed.Store(true)
			// After authed, so an event in between is not lost; at worst it comes twice.
			if s.afterHello != nil {
				if ev, body := s.afterHello(); ev != "" {
					if line := s.eventLine(ev, body); line != nil {
						c.out <- line
					}
				}
			}
			continue
		}
		var res any
		var err error
		if bad {
			err = &Error{"bad_request", "need a JSON object with cmd"}
		} else if h := s.handlers[r.Cmd]; h == nil {
			err = &Error{"unknown_cmd", "unknown cmd " + r.Cmd}
		} else if s.async[r.Cmd] {
			go func(at string) {
				res, err := h(line)
				select {
				case c.out <- reply(at, res, err):
				case <-c.done:
				}
			}(r.At)
			continue
		} else {
			res, err = h(line)
		}
		select {
		case c.out <- reply(r.At, res, err):
		case <-c.done:
			return
		}
	}
	if sc.Err() != nil {
		s.log.Warnf("UI link: %v", sc.Err())
	}
}
