package main

import (
	"encoding/binary"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/purpshell/meowcaller"
)

// The audio link of step 7.2 (docs/protocol2.md): raw PCM, 16 kHz mono s16le, between the UI's
// microphone and speaker and meowcaller's float32 frames of 960 samples.

const (
	frameBytes = meowcaller.FrameSamples * 2
	// micQueue is how many microphone frames wait for meowcaller's send loop: 3 x 60 ms is the
	// most delay the link adds.
	micQueue = 3
	// peerQueue is how many of the peer's frames wait for the writer when the UI doesn't read.
	peerQueue = 8
)

// audioGrace is how long a call may be without an audio link, after it had one, before it is
// hung up: the UI was closed and nothing can play or record. A var for the tests.
var audioGrace = 3 * time.Second

// micSource is the call's AudioSource: the UI's frames, newest kept. meowcaller's send loop pulls
// it every 60 ms and must never wait, nor be told the end (io.EOF idles the player), so an empty
// queue is silence.
type micSource struct {
	mu sync.Mutex
	q  [][]float32
}

func (m *micSource) push(f []float32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.q) == micQueue {
		m.q = append(m.q[:0], m.q[1:]...) // the oldest goes
	}
	m.q = append(m.q, f)
}

func (m *micSource) ReadFrame() ([]float32, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.q) == 0 {
		return make([]float32, meowcaller.FrameSamples), nil
	}
	f := m.q[0]
	m.q = m.q[1:]
	return f, nil
}

func (m *micSource) Close() error { return nil }

// pcmToFloat reads one frame of s16le.
func pcmToFloat(b []byte) []float32 {
	f := make([]float32, len(b)/2)
	for i := range f {
		f[i] = float32(int16(binary.LittleEndian.Uint16(b[2*i:]))) / 32768
	}
	return f
}

// floatToPCM writes one frame as s16le, clipped.
func floatToPCM(f []float32) []byte {
	b := make([]byte, len(f)*2)
	for i, s := range f {
		v := s * 32768
		switch {
		case v > 32767:
			v = 32767
		case v < -32768:
			v = -32768
		}
		binary.LittleEndian.PutUint16(b[2*i:], uint16(int16(v)))
	}
	return b
}

// audioLink is one UI connection (the video link uses it too) and the peer's frames on their way to it.
type audioLink struct {
	conn net.Conn
	out  chan []byte
	done chan struct{}
	once sync.Once
}

func (l *audioLink) close() {
	l.once.Do(func() {
		close(l.done)
		l.conn.Close()
	})
}

// send queues a frame for the UI, or drops it when the UI isn't reading.
func (l *audioLink) send(b []byte) {
	select {
	case l.out <- b:
	default:
	}
}

func (l *audioLink) write() {
	for {
		select {
		case b := <-l.out:
			if _, err := l.conn.Write(b); err != nil {
				l.close()
				return
			}
		case <-l.done:
			return
		}
	}
}

// linkSink is the call's AudioSink: the peer's frames to the UI's link, if there is one.
type linkSink struct{ cs *callState }

func (s linkSink) WriteFrame(f []float32) error {
	if l := s.cs.link.Load(); l != nil {
		l.send(floatToPCM(f))
	}
	return nil
}

func (s linkSink) Close() error { return nil }

// attachAudio is Server.audio: the hello of an audio link for call id.
func (c *Calls) attachAudio(id string, conn net.Conn, ok []byte) bool {
	return c.attachLink(id, conn, ok, peerQueue, func(cs *callState) *atomic.Pointer[audioLink] { return &cs.link }, c.readLink)
}

// attachLink takes a link's connection over for the live call id, after the ok line. It fills the
// slot the call keeps that kind of link in; a link already there is replaced and closed. read runs
// on the connection until it closes.
func (c *Calls) attachLink(id string, conn net.Conn, ok []byte, queue int, slot func(*callState) *atomic.Pointer[audioLink], read func(*callState, *audioLink)) bool {
	c.mu.Lock()
	cs := c.cur
	if cs == nil || cs.call == nil || cs.ended || cs.id != id {
		c.mu.Unlock()
		return false
	}
	l := &audioLink{conn: conn, out: make(chan []byte, queue), done: make(chan struct{})}
	conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_, err := conn.Write(ok)
	conn.SetWriteDeadline(time.Time{})
	if err != nil {
		c.mu.Unlock()
		return false
	}
	old := slot(cs).Swap(l)
	c.mu.Unlock()
	if old != nil {
		old.close()
	}
	go l.write()
	go read(cs, l)
	return true
}

// readLink feeds the microphone queue until the link closes. A link that closes while the call
// goes on, with no other in its place within audioGrace, ends the call.
func (c *Calls) readLink(cs *callState, l *audioLink) {
	buf := make([]byte, frameBytes)
	for {
		if _, err := io.ReadFull(l.conn, buf); err != nil {
			break
		}
		cs.mic.push(pcmToFloat(buf))
	}
	l.close()
	if cs.link.CompareAndSwap(l, nil) {
		time.AfterFunc(audioGrace, func() { c.linkLost(cs) })
	}
}

func (c *Calls) linkLost(cs *callState) {
	c.mu.Lock()
	gone := cs.ended || cs.link.Load() != nil
	c.mu.Unlock()
	if gone {
		return
	}
	c.log.Infof("call %s: the UI's audio link is gone, hanging up", cs.id)
	if err := cs.call.Hangup(); err != nil {
		c.log.Warnf("call %s: hangup: %v", cs.id, err)
	}
	c.finish(cs, "hangup")
}
