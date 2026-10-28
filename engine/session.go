package main

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/store"
	"github.com/polymorfa/hypermeow/store/sqlstore"
	"github.com/polymorfa/hypermeow/types/events"
	waLog "github.com/polymorfa/hypermeow/util/log"
)

// Link states (PLAN.md §5). "syncing" is left to the store, which knows when history is in.
const (
	linkUnlinked = "unlinked"
	linkQR       = "qr"
	linkCode     = "code"
	linkLinking  = "linking"
	linkReady    = "ready"
)

// Session owns the hypermeow client and the link state machine. When the device is removed,
// by the phone or by link.logout, hypermeow deletes it from session.db, so the next link
// starts with a fresh device and a fresh client.
type Session struct {
	container *sqlstore.Container
	srv       *Server
	store     *Store
	log       waLog.Logger

	mu     sync.Mutex
	cli    *whatsmeow.Client
	link   string
	conn   string        // offline, connecting, ready
	qr     string        // the current QR code, in state qr
	code   string        // the pairing code, in state code
	stopQR func()        // ends the running QR channel
	qrSeen chan struct{} // closed when the QR channel's first code arrives
}

func newSession(container *sqlstore.Container, srv *Server, store *Store, log waLog.Logger) (*Session, error) {
	device, err := container.GetFirstDevice(context.Background())
	if err != nil {
		return nil, err
	}
	s := &Session{container: container, srv: srv, store: store, log: log, link: linkUnlinked, conn: "offline"}
	s.cli = s.newClient(device)
	if s.cli.Store.ID != nil {
		s.link, s.conn = linkReady, "connecting"
		if err := s.cli.Connect(); err != nil {
			return nil, err
		}
	}
	srv.Handle("link.qr", s.handleQR)
	srv.Handle("link.code", s.handleCode)
	srv.Handle("link.logout", s.handleLogout)
	return s, nil
}

func (s *Session) newClient(device *store.Device) *whatsmeow.Client {
	cli := whatsmeow.NewClient(device, s.log.Sub("client"))
	cli.AddEventHandler(func(evt any) { s.onEvent(cli, evt) })
	cli.AddEventHandler(func(evt any) { s.store.handle(cli, evt) })
	return cli
}

// hello is the link part of the hello reply.
func (s *Session) hello() any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.linkBody()
}

func (s *Session) linkBody() map[string]any {
	b := map[string]any{"state": s.link, "conn": s.conn}
	switch {
	case s.link == linkQR:
		b["qr"] = s.qr
	case s.link == linkCode:
		b["code"] = s.code
	case s.cli.Store.ID != nil:
		b["me"] = s.cli.Store.ID.ToNonAD().String()
		b["name"] = s.cli.Store.PushName
	}
	return b
}

// setLink changes the link state and tells the UI. Call it with s.mu held.
func (s *Session) setLink(state string) {
	s.link = state
	s.srv.Emit("link", s.linkBody())
}

func (s *Session) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopQR != nil {
		s.stopQR()
	}
	s.cli.Disconnect()
}

// startQR opens the login websocket with a QR channel, unless one is already running.
// Call it with s.mu held.
func (s *Session) startQR() error {
	if s.stopQR != nil {
		return nil
	}
	if s.cli.IsConnected() {
		s.cli.Disconnect() // a QR channel has to be opened before connecting
	}
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := s.cli.GetQRChannel(ctx)
	if err == nil {
		err = s.cli.Connect()
	}
	if err != nil {
		cancel()
		return err
	}
	s.stopQR, s.qrSeen = cancel, make(chan struct{})
	go s.readQR(s.cli, ch, s.qrSeen)
	return nil
}

func (s *Session) readQR(cli *whatsmeow.Client, ch <-chan whatsmeow.QRChannelItem, seen chan struct{}) {
	first := true
	for item := range ch {
		s.mu.Lock()
		if cli != s.cli {
			s.mu.Unlock()
			return
		}
		switch item.Event {
		case whatsmeow.QRChannelEventCode:
			if first {
				first = false
				close(seen)
			}
			if s.link != linkCode {
				s.qr = item.Code
				s.setLink(linkQR)
			}
		case "success":
			s.setLink(linkLinking)
		default: // timeout, or an error: the login socket is gone, so start over
			s.log.Warnf("QR channel ended: %s %v", item.Event, item.Error)
			s.stopQR, s.qr, s.code = nil, "", ""
			if s.link != linkLinking && s.link != linkReady {
				s.setLink(linkUnlinked)
			}
		}
		s.mu.Unlock()
	}
}

func (s *Session) handleQR(json.RawMessage) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.link == linkReady || s.link == linkLinking {
		return nil, &Error{"linked", "this device is already linked"}
	}
	if err := s.startQR(); err != nil {
		return nil, err
	}
	if s.qr != "" {
		return map[string]string{"qr": s.qr}, nil
	}
	return nil, nil // the codes come as link events
}

func (s *Session) handleCode(req json.RawMessage) (any, error) {
	var r struct{ Phone string }
	json.Unmarshal(req, &r)
	digits := strings.Map(func(c rune) rune {
		if c >= '0' && c <= '9' {
			return c
		}
		return -1
	}, r.Phone)
	if len(digits) < 6 {
		return nil, &Error{"bad_phone", "the phone number needs its country code"}
	}
	s.mu.Lock()
	if s.link == linkReady || s.link == linkLinking {
		s.mu.Unlock()
		return nil, &Error{"linked", "this device is already linked"}
	}
	if err := s.startQR(); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	cli, seen := s.cli, s.qrSeen
	s.mu.Unlock()

	// PairPhone needs the login websocket up, which the first QR code shows.
	select {
	case <-seen:
	case <-time.After(20 * time.Second):
		return nil, &Error{"offline", "WhatsApp didn't answer; check the connection and try again"}
	}
	// The server accepts only "Browser (OS)" names it knows.
	code, err := cli.PairPhone(context.Background(), digits, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.code = code
	s.setLink(linkCode)
	return map[string]string{"code": code}, nil
}

func (s *Session) handleLogout(json.RawMessage) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cli.Store.ID != nil {
		if err := s.cli.Logout(context.Background()); err != nil {
			return nil, err
		}
	}
	return nil, s.reset()
}

// reset drops the client and starts over with a fresh device. Call it with s.mu held.
func (s *Session) reset() error {
	if s.stopQR != nil {
		s.stopQR()
	}
	s.cli.Disconnect()
	s.store.clear()
	s.cli = s.newClient(s.container.NewDevice())
	s.stopQR, s.qr, s.code, s.conn = nil, "", "", "offline"
	s.setLink(linkUnlinked)
	return nil
}

func (s *Session) onEvent(cli *whatsmeow.Client, evt any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cli != s.cli {
		return // an event from a client that reset() already replaced
	}
	switch e := evt.(type) {
	case *events.PairSuccess:
		s.setLink(linkLinking)
	case *events.Connected:
		s.conn = "ready"
		if cli.Store.ID != nil {
			if s.stopQR != nil {
				s.stopQR()
				s.stopQR = nil
			}
			s.setLink(linkReady)
		}
		s.srv.Emit("conn", map[string]any{"state": s.conn})
	case *events.Disconnected:
		s.conn = "connecting" // hypermeow reconnects by itself
		s.srv.Emit("conn", map[string]any{"state": s.conn})
	case *events.StreamReplaced:
		s.conn = "offline" // another engine took this session over
		s.srv.Emit("conn", map[string]any{"state": s.conn, "reason": "replaced"})
	case *events.LoggedOut:
		s.log.Warnf("logged out: %v", e.Reason)
		// Off this goroutine: reset disconnects the very client delivering the event.
		go func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if cli == s.cli {
				s.reset()
			}
		}()
	}
}
