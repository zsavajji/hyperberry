package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/polymorfa/hypermeow/types"
	waLog "github.com/polymorfa/hypermeow/util/log"
)

// Sim is the simulated account of step 1.12: link.* and a made-up account's chats, with nothing
// sent to WhatsApp, so the UI's flows run without linking the real number again (steps.md, "Real
// links are few"). scripts/sim.py drives it with sim.* requests: the phone accepting the device,
// a message arriving, the device removed on the phone. It also answers the chat requests from
// made-up data (step 3.0).
type Sim struct {
	srv   *Server
	store *Store
	log   waLog.Logger

	mu   sync.Mutex
	link string // as Session's
	qr   string
	code string
	run  int // counts the QR runs, so a stale one stops
	rec  voiceRec
	// syncing as Session's: from sim.link until the seed has committed.
	syncing bool
	blocked map[string]bool // contact.block's flags by chat; nil until the first
	prof    simProfile      // profile.get's name and about (sim_profile.go)
}

// simCodes are how long each made-up QR code lasts, as WhatsApp's: 60 s, then five of 20 s,
// then the run is over. simDelay stands for a round trip to WhatsApp. Tests shorten both.
var (
	simCodes = []time.Duration{60 * time.Second, 20 * time.Second, 20 * time.Second,
		20 * time.Second, 20 * time.Second, 20 * time.Second}
	simDelay = time.Second
	// simTicks are when a sent message turns sent, delivered and read, from the send.
	simTicks  = [3]time.Duration{500 * time.Millisecond, 1500 * time.Millisecond, 3 * time.Second}
	simTyping = 4 * time.Second
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
	srv.Handle("sim.typing", s.handleTyping)
	srv.Handle("sim.voice", s.handleVoice)
	srv.Handle("voice.play", s.handleVoicePlay)
	s.registerVoiceSend()
	s.registerSimMedia()
	s.registerSimReact()
	s.registerProfile()
	srv.Handle("media.send", s.handleMediaSend)
	srv.Handle("msg.send", s.handleSend)
	srv.Handle("msg.delete", s.handleDelete)
	srv.Handle("chat.read", s.handleRead)
	srv.Handle("chat.set", s.handleChatSet)
	srv.Handle("contact.block", s.handleContactBlock)
	srv.Handle("chat.clear", s.handleChatClear)
	srv.Handle("chat.delete", s.handleChatDelete)
	srv.Handle("chat.export", st.handleExport)
	srv.Handle("chat.open", s.handleOpen)
	srv.Handle("chat.typing", s.handleChatTyping)
	srv.Handle("group.info", s.handleGroupInfo)
	srv.Handle("group.participants", s.handleGroupParticipants)
	srv.Handle("contact.info", s.handleContactInfo)
	srv.Handle("contacts.list", s.handleContacts)
	srv.Handle("chat.close", s.handleLogged)
	srv.Handle("ui.state", s.handleUIState)
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
	if s.syncing {
		b["syncing"] = true
	}
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
	run, codes := s.run, simCodes
	go func() {
		for i, d := range codes {
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
	s.syncing = true
	s.setLink(linkLinking)
	delay := simDelay
	go func() {
		time.Sleep(delay)
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.link == linkLinking {
			s.setLink(linkReady)
			s.seed()
			s.store.afterCommit(func() { go s.endSync() }) // off the writer: endSync takes s.mu
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
	if err := s.ready(); err != nil {
		return nil, err
	}
	if r.N < 0 || r.N >= len(simChats) {
		return nil, &Error{"bad_request", fmt.Sprintf("n is 0 to %d", len(simChats)-1)}
	}
	c, now := simChats[r.N], time.Now()
	if r.Text == "" {
		r.Text = "A new message at " + now.Format("15:04:05")
	}
	s.deliver(c, now, msgRow{Kind: msgText, Text: r.Text})
	return nil, nil
}

// deliver puts an incoming message into chat c, unread, and returns its id.
func (s *Sim) deliver(c simChat, now time.Time, m msgRow) string {
	m.Chat, m.ID, m.TS, m.State, m.SenderName = c.jid, simID(now), now.Unix(), stateRead, c.sender
	if c.sender != "" {
		m.Sender = simMember(c.sender)
	}
	s.store.ops <- func(t *storeTx) {
		if t.insertMessage(m) {
			t.setLast(c.jid, m.TS, chatPreview(m), false, m.State)
			t.addUnread(c.jid, 1)
			t.notifyIncoming(c.jid)
		}
	}
	return m.ID
}

// handleRemoved is the device removed on the phone, and link.logout: the history goes with it.
func (s *Sim) handleRemoved(json.RawMessage) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.run++
	s.qr, s.code = "", ""
	s.store.clear()
	s.syncing = false
	s.setLink(linkUnlinked)
	return nil, nil
}

// endSync ends sim.link's "syncing" and tells the UI, then lets the Hub show the account.
func (s *Sim) endSync() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.syncing {
		s.syncing = false
		s.setLink(s.link)
		s.store.nt.link() // the new link's chats are in
	}
}

// seed writes simChats, each with its last message, in one transaction.
func (s *Sim) seed() {
	now := time.Now()
	s.store.ops <- func(t *storeTx) {
		for i, c := range simChats {
			for _, h := range simHistory(i, now) {
				s.seedMedia(&h)
				t.insertMessage(h)
			}
			m := msgRow{Chat: c.jid, ID: fmt.Sprintf("SIMSEED%d", i), TS: now.Add(-c.ago).Unix(), Out: c.out,
				State: c.state, SenderName: c.sender, Kind: c.kind, Text: c.text, Duration: c.secs}
			s.seedMedia(&m)
			if !c.out {
				m.State = stateRead
			}
			if c.sender != "" {
				m.Sender = simMember(c.sender)
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
		seedReactions(t, now)
	}
}

// ready reports the error a request gets while the simulated account isn't linked.
func (s *Sim) ready() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.link != linkReady {
		return &Error{"unlinked", "the simulated account isn't linked"}
	}
	return nil
}

// chat is ready, then the chat key parsed as Session.linked does.
func (s *Sim) chat(key string) (types.JID, error) {
	if err := s.ready(); err != nil {
		return types.EmptyJID, err
	}
	jid, err := types.ParseJID(key)
	if err != nil || jid.IsEmpty() {
		return types.EmptyJID, &Error{"bad_chat", "not a chat key: " + key}
	}
	return jid, nil
}

// simIndex is the place of a chat in simChats, -1 when it isn't there.
func simIndex(jid string) int {
	for i, c := range simChats {
		if c.jid == jid {
			return i
		}
	}
	return -1
}

// handleLogged answers chat.close with ok and logs the raw request.
func (s *Sim) handleLogged(req json.RawMessage) (any, error) {
	s.log.Infof("sim: %s", req)
	return nil, nil
}

// handleUIState records the UI's state for the notifier and logs the raw request, so the Q10
// check can see the UI's focus rhythm (step 3.4).
func (s *Sim) handleUIState(req json.RawMessage) (any, error) {
	var r struct {
		Fullscreen bool
		OpenChat   string
	}
	json.Unmarshal(req, &r)
	s.store.ui.set(r.Fullscreen, r.OpenChat)
	return s.handleLogged(req)
}

// handleSend is msg.send: the row as sending, then sent, delivered and read at simTicks. A text
// starting with "fail" ends failed. Like the real one it blocks for the round trip.
func (s *Sim) handleSend(req json.RawMessage) (any, error) {
	var r struct {
		Chat, Text, Quote string
		Mentions          []mention
	}
	if err := json.Unmarshal(req, &r); err != nil || strings.TrimSpace(r.Text) == "" {
		return nil, &Error{"bad_request", "msg.send needs chat and non-empty text"}
	}
	jid, err := s.chat(r.Chat)
	if err != nil {
		return nil, err
	}
	id, ops := fmt.Sprintf("SIMOUT%d", time.Now().UnixNano()), s.store.ops
	extra := "" // as the real one's row: the text as typed, and the mentions that would have been sent
	if _, jids := mentionWire(r.Text, r.Mentions); len(jids) > 0 {
		extra = withMentions("", r.Mentions)
	}
	_, created := s.store.queueOutgoing(jid, r.Chat, id, r.Text, r.Quote, extra, time.Now().Unix())
	if created && chatKind(jid) == chatUser {
		name := simBookName(r.Chat)
		s.store.ops <- func(t *storeTx) { t.setName(r.Chat, name) }
	}
	ticks := simTicks // copied before the goroutine: tests restore it while one may still sleep
	time.Sleep(ticks[0])
	// The Q10 capitalises the first letter, so the match ignores case.
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.Text)), "fail") {
		ops <- func(t *storeTx) { t.sentAs(r.Chat, id, 0, stateFailed) }
		return nil, errors.New("simulated send failure")
	}
	ops <- func(t *storeTx) { t.sentAs(r.Chat, id, 0, stateSent) }
	go func() {
		time.Sleep(ticks[1] - ticks[0])
		ops <- func(t *storeTx) { t.setState(r.Chat, id, stateDelivered) }
		time.Sleep(ticks[2] - ticks[1])
		ops <- func(t *storeTx) { t.setState(r.Chat, id, stateRead) }
	}()
	return map[string]string{"id": id}, nil
}

// handleDelete is msg.delete: the same rows change as with the real one, nothing is sent.
func (s *Sim) handleDelete(req json.RawMessage) (any, error) {
	r, err := parseDelete(req)
	if err != nil {
		return nil, err
	}
	if _, err := s.chat(r.Chat); err != nil {
		return nil, err
	}
	rows, err := s.store.deletable(r.Chat, r.IDs, r.Everyone)
	if err != nil {
		return nil, err
	}
	for _, m := range rows {
		id := m.ID
		s.store.ops <- func(t *storeTx) {
			if r.Everyone {
				t.revokeMessage(r.Chat, id)
			} else {
				t.deleteMessage(r.Chat, id)
			}
		}
	}
	return nil, nil
}

func (s *Sim) handleRead(req json.RawMessage) (any, error) {
	var r struct{ Chat string }
	json.Unmarshal(req, &r)
	s.log.Infof("sim: %s", req)
	if _, err := s.chat(r.Chat); err != nil {
		return nil, err
	}
	s.store.ops <- func(t *storeTx) { t.setUnread(r.Chat, 0) }
	return nil, nil
}

// handleOpen answers with a presence after a round trip: online for even chats, last seen an
// hour ago for odd ones, hidden for chats that aren't in simChats. Groups have none.
func (s *Sim) handleOpen(req json.RawMessage) (any, error) {
	var r struct{ Chat string }
	json.Unmarshal(req, &r)
	s.log.Infof("sim: %s", req)
	jid, err := s.chat(r.Chat)
	if err != nil || jid.Server == types.GroupServer {
		return nil, err
	}
	delay := simDelay
	go func() {
		time.Sleep(delay)
		online, seen := false, int64(0)
		switch i := simIndex(r.Chat); {
		case i < 0:
		case i%2 == 0:
			online = true
		default:
			seen = time.Now().Add(-time.Hour).Unix()
		}
		s.srv.Emit("presence", map[string]any{"chat": r.Chat, "online": online, "lastSeen": seen})
	}()
	return nil, nil
}

// handleChatTyping logs the raw request so the Q10 check can see the UI's typing rhythm (step 3.2).
func (s *Sim) handleChatTyping(req json.RawMessage) (any, error) {
	s.log.Infof("sim: chat.typing %s", req)
	return nil, nil
}

// handleTyping is sim.typing: chat n types for simTyping; in a group, a member does.
func (s *Sim) handleTyping(req json.RawMessage) (any, error) {
	var r struct{ N int }
	json.Unmarshal(req, &r)
	if err := s.ready(); err != nil {
		return nil, err
	}
	if r.N < 0 || r.N >= len(simChats) {
		return nil, &Error{"bad_request", fmt.Sprintf("n is 0 to %d", len(simChats)-1)}
	}
	c, who := simChats[r.N], ""
	if j, _ := types.ParseJID(c.jid); j.Server == types.GroupServer {
		who = firstWord(c.sender)
	}
	typing := func(on bool) {
		s.srv.Emit("typing", map[string]any{"chat": c.jid, "who": who, "on": on, "voice": false})
	}
	typing(true)
	time.AfterFunc(simTyping, func() { typing(false) })
	return nil, nil
}
