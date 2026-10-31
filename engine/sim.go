package main

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/polymorfa/hypermeow/types"
	waLog "github.com/polymorfa/hypermeow/util/log"
)

// Sim is the simulated account of step 1.12: link.* and a made-up account's chats, with nothing
// sent to WhatsApp, so the UI's flows run without linking the real number again (steps.md, "Real
// links are few"). scripts/sim.py drives it with sim.* requests: the phone accepting the device,
// a message arriving, the device removed on the phone.
type Sim struct {
	srv   *Server
	store *Store
	log   waLog.Logger

	mu   sync.Mutex
	link string // as Session's
	qr   string
	code string
	run  int // counts the QR runs, so a stale one stops
}

// simCodes are how long each made-up QR code lasts, as WhatsApp's: 60 s, then five of 20 s,
// then the run is over. simDelay stands for a round trip to WhatsApp. Tests shorten both.
var (
	simCodes = []time.Duration{60 * time.Second, 20 * time.Second, 20 * time.Second,
		20 * time.Second, 20 * time.Second, 20 * time.Second}
	simDelay = time.Second
)

const simMe = "390000000000@s.whatsapp.net"

func newSim(srv *Server, st *Store, log waLog.Logger) *Sim {
	s := &Sim{srv: srv, store: st, log: log, link: linkUnlinked}
	// The account is linked while its chats are in: sim.link writes them, and sim.removed and
	// link.logout empty chats.db. So an engine restart keeps the state, with no file to write.
	var n int
	st.db.QueryRow("SELECT count(*) FROM chats").Scan(&n)
	if n > 0 {
		s.link = linkReady
	}
	srv.Handle("link.qr", s.handleQR)
	srv.Handle("link.code", s.handleCode)
	srv.Handle("link.logout", s.handleRemoved)
	srv.Handle("sim.link", s.handleLink)
	srv.Handle("sim.msg", s.handleMsg)
	srv.Handle("sim.removed", s.handleRemoved)
	return s
}

func (s *Sim) hello() any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.body()
}

// body is the link body of docs/protocol.md.
func (s *Sim) body() map[string]any {
	b := map[string]any{"state": s.link, "conn": "offline"}
	switch s.link {
	case linkQR:
		b["qr"] = s.qr
	case linkCode:
		b["code"] = s.code
	case linkReady:
		b["conn"], b["me"], b["name"] = "ready", simMe, "Sim"
	}
	return b
}

// setLink changes the link state and tells the UI. Call it with s.mu held.
func (s *Sim) setLink(state string) {
	s.link = state
	s.srv.Emit("link", s.body())
}

// startQR starts a run of made-up codes; a code run out goes back to unlinked. Call it with
// s.mu held.
func (s *Sim) startQR() {
	s.run++
	run := s.run
	go func() {
		for i, d := range simCodes {
			s.mu.Lock()
			if run != s.run {
				s.mu.Unlock()
				return
			}
			s.qr = fmt.Sprintf("hyperberry-sim,%d,%d", run, i)
			if s.link != linkCode {
				s.setLink(linkQR)
			}
			s.mu.Unlock()
			time.Sleep(d)
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if run == s.run {
			s.run++
			s.qr, s.code = "", ""
			s.setLink(linkUnlinked)
		}
	}()
}

func (s *Sim) linked() bool {
	return s.link == linkReady || s.link == linkLinking
}

func (s *Sim) handleQR(json.RawMessage) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.linked() {
		return nil, &Error{"linked", "this device is already linked"}
	}
	if s.qr == "" {
		s.startQR()
		return nil, nil // the codes come as link events
	}
	return map[string]string{"qr": s.qr}, nil
}

// handleCode starts a fresh run through unlinked, as Session.restartLogin does, and answers a
// made-up code after a round trip.
func (s *Sim) handleCode(req json.RawMessage) (any, error) {
	var r struct{ Phone string }
	json.Unmarshal(req, &r)
	if _, err := pairDigits(r.Phone); err != nil {
		return nil, err
	}
	time.Sleep(simDelay)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.linked() {
		return nil, &Error{"linked", "this device is already linked"}
	}
	s.qr, s.code = "", ""
	s.setLink(linkUnlinked)
	s.startQR()
	s.code = "SIMU-LATE"
	s.setLink(linkCode)
	return map[string]string{"code": s.code}, nil
}

// handleLink is the phone accepting the device. As with a real link: linking, then ready after
// the reconnect, then the history.
func (s *Sim) handleLink(json.RawMessage) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.link != linkQR && s.link != linkCode {
		return nil, &Error{"bad_state", "nothing to accept: the linking page shows no code"}
	}
	s.run++ // ends the codes
	s.qr, s.code = "", ""
	s.store.clear()
	s.setLink(linkLinking)
	go func() {
		time.Sleep(simDelay)
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.link == linkLinking {
			s.setLink(linkReady)
			s.seed()
		}
	}()
	return nil, nil
}

// handleMsg is an incoming message in chat n of simChats, now.
func (s *Sim) handleMsg(req json.RawMessage) (any, error) {
	var r struct {
		N    int
		Text string
	}
	json.Unmarshal(req, &r)
	s.mu.Lock()
	ready := s.link == linkReady
	s.mu.Unlock()
	if !ready {
		return nil, &Error{"unlinked", "the simulated account isn't linked"}
	}
	if r.N < 0 || r.N >= len(simChats) {
		return nil, &Error{"bad_request", fmt.Sprintf("n is 0 to %d", len(simChats)-1)}
	}
	c, now := simChats[r.N], time.Now()
	if r.Text == "" {
		r.Text = "A new message at " + now.Format("15:04:05")
	}
	s.store.ops <- func(t *storeTx) {
		m := msgRow{Chat: c.jid, ID: fmt.Sprintf("SIM%d", now.UnixNano()), TS: now.Unix(), State: stateRead,
			Kind: msgText, Text: r.Text, SenderName: c.sender}
		if t.insertMessage(m) {
			t.setLast(c.jid, m.TS, chatPreview(m), false, m.State)
			t.addUnread(c.jid, 1)
		}
	}
	return nil, nil
}

// handleRemoved is the device removed on the phone, and link.logout: the history goes with it.
func (s *Sim) handleRemoved(json.RawMessage) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.run++
	s.qr, s.code = "", ""
	s.store.clear()
	s.setLink(linkUnlinked)
	return nil, nil
}

// seed writes simChats, each with its last message, in one transaction.
func (s *Sim) seed() {
	now := time.Now()
	s.store.ops <- func(t *storeTx) {
		for i, c := range simChats {
			m := msgRow{Chat: c.jid, ID: fmt.Sprintf("SIMSEED%d", i), TS: now.Add(-c.ago).Unix(), Out: c.out,
				State: c.state, SenderName: c.sender, Kind: c.kind, Text: c.text}
			if !c.out {
				m.State = stateRead
			}
			if c.kind == msgText && c.text == "" {
				m.Deleted = true
			}
			j, _ := types.ParseJID(c.jid)
			t.ensureChat(c.jid, chatKind(j))
			t.setName(c.jid, c.name)
			t.insertMessage(m)
			p := chatPreview(m)
			if m.Deleted {
				p = "This message was deleted"
			}
			t.setLast(c.jid, m.TS, p, m.Out, m.State)
			muted := int64(0)
			if c.muted > 0 {
				muted = now.Add(time.Duration(c.muted) * time.Hour).Unix()
			} else if c.muted < 0 {
				muted = muteForever
			}
			t.setFlags(c.jid, c.unread, muted, c.pinned, c.archived)
		}
	}
}
