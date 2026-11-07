package main

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/polymorfa/hypermeow/types"
	waLog "github.com/polymorfa/hypermeow/util/log"
	"github.com/purpshell/meowcaller"
)

// Calls of step 7.2 (docs/protocol2.md): one live voice call at a time, meowcaller's signalling
// and media on one side, the UI's `call` events, requests and audio link on the other. The audio
// itself lives in the UI (calls_audio.go).

// liveCall is what calls.go uses of meowcaller's *Call, so tests can fake one.
type liveCall interface {
	ID() string
	Peer() types.JID
	State() meowcaller.CallPhase
	Answer() error
	Reject() error
	Hangup() error
	OnStateChange(func(meowcaller.CallPhase))
	OnEnd(func(reason string))
	Play(meowcaller.AudioSource) *meowcaller.Player
	Receive(meowcaller.AudioSink)
}

// callState is the live call. Calls.mu guards it, except link.
type callState struct {
	call      liveCall // nil while an outgoing offer is going out
	id        string
	chat      string // the chats.db key of the person
	name      string // for the Hub item and a new chat
	out       bool
	offer     int64  // unix seconds: the row's ts
	state     string // the last state sent
	since     int64  // unix ms of "active"
	answering bool   // we pressed Answer
	forced    string // the reason to end with, whatever meowcaller says (offline)
	ring      chan struct{}
	ended     bool
	mic       *micSource
	link      atomic.Pointer[audioLink]
}

// Calls owns the live call. The seams are what Session fills in; tests fill their own.
type Calls struct {
	st  *Store
	srv *Server
	log waLog.Logger

	// ready checks that a call to chat can be placed and gives the person's name.
	ready func(chat string) (name string, err error)
	// dial places the call.
	dial func(ctx context.Context, target string) (liveCall, error)
	// resolve is the chats.db key and the name of a caller.
	resolve func(peer types.JID) (key, name string)

	mu  sync.Mutex
	cur *callState
}

func newCalls(st *Store, srv *Server, log waLog.Logger) *Calls {
	c := &Calls{st: st, srv: srv, log: log}
	srv.afterHello = c.helloEvent
	srv.audio = c.attachAudio
	srv.Handle("call.start", c.handleStart)
	srv.Handle("call.answer", c.handleAnswer)
	srv.Handle("call.reject", c.handleReject)
	srv.Handle("call.hangup", c.handleHangup)
	return c
}

// registerSimCalls: the simulated account has no calls.
func registerSimCalls(srv *Server) {
	srv.Handle("call.start", func(json.RawMessage) (any, error) {
		return nil, &Error{"simulated", "the simulated account has no calls"}
	})
	for _, cmd := range []string{"call.answer", "call.reject", "call.hangup"} {
		srv.Handle(cmd, func(json.RawMessage) (any, error) { return nil, &Error{"not_found", "no such live call"} })
	}
}

// callReason turns meowcaller's end reason into the contract's. The raw words seen in its source:
// "rejected" (our Reject, or the peer's), "hangup" (our Hangup), "server:<code>" (the server
// refused the offer), "offer_failed", and WhatsApp's own terminate reason ("accepted_elsewhere",
// "" for a plain hang-up; the rest are guesses by substring).
func callReason(raw string) string {
	r := strings.ToLower(raw)
	switch {
	case r == "" || r == "hangup":
		return "hangup"
	case strings.Contains(r, "elsewhere"):
		return "elsewhere"
	case strings.Contains(r, "reject") || strings.Contains(r, "declin"):
		return "declined"
	case strings.Contains(r, "busy"):
		return "busy"
	case strings.Contains(r, "timeout") || strings.Contains(r, "no_answer") || strings.Contains(r, "unanswered"):
		return "no_answer"
	}
	return "failed"
}

// callPhase is the state a meowcaller phase shows, "" for the ones that show nothing.
func callPhase(p meowcaller.CallPhase, out bool) string {
	switch p {
	case meowcaller.CallPhaseCalling:
		return "calling"
	case meowcaller.CallPhaseRinging:
		if out {
			return "calling" // the peer's phone rings: still the offer going out
		}
		return "ringing"
	case meowcaller.CallPhaseConnecting:
		return "connecting"
	case meowcaller.CallPhaseActive:
		return "active"
	}
	return ""
}

// body is the `call` event. Call it with c.mu held.
func (cs *callState) body(reason string) map[string]any {
	return map[string]any{"id": cs.id, "chat": cs.chat, "out": cs.out, "state": cs.state,
		"reason": reason, "since": cs.since}
}

// helloEvent is the live call, for the UI that just said hello.
func (c *Calls) helloEvent() (string, map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cs := c.cur; cs != nil && cs.call != nil && !cs.ended && cs.state != "" {
		return "call", cs.body("")
	}
	return "", nil
}

func (c *Calls) get(req json.RawMessage) (*callState, error) {
	var r struct{ ID string }
	json.Unmarshal(req, &r)
	c.mu.Lock()
	defer c.mu.Unlock()
	if cs := c.cur; cs != nil && cs.call != nil && !cs.ended && cs.id == r.ID && r.ID != "" {
		return cs, nil
	}
	return nil, &Error{"not_found", "no such live call"}
}

func (c *Calls) handleStart(req json.RawMessage) (any, error) {
	var r struct{ Chat string }
	json.Unmarshal(req, &r)
	name, err := c.ready(r.Chat)
	if err != nil {
		return nil, err
	}
	cs := &callState{chat: r.Chat, name: name, out: true, offer: time.Now().Unix()}
	c.mu.Lock()
	if c.cur != nil {
		c.mu.Unlock()
		return nil, &Error{"busy", "a call is live"}
	}
	c.cur = cs
	c.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	call, err := c.dial(ctx, r.Chat)
	if err != nil {
		c.mu.Lock()
		if c.cur == cs {
			c.cur = nil
		}
		c.mu.Unlock()
		c.log.Warnf("call to %s: %v", r.Chat, err)
		return nil, err
	}
	c.mu.Lock()
	if cs.ended { // the link dropped while the offer went out
		c.mu.Unlock()
		call.Hangup()
		return nil, &Error{"offline", "the connection to WhatsApp dropped"}
	}
	cs.call, cs.id = call, call.ID()
	c.hook(cs)
	c.mu.Unlock()
	// What the phases did before the callbacks were in.
	if call.State() == meowcaller.CallPhaseEnded {
		c.finish(cs, "offer_failed")
	} else {
		c.phase(cs, call.State())
	}
	return map[string]string{"id": cs.id}, nil
}

func (c *Calls) handleAnswer(req json.RawMessage) (any, error) {
	cs, err := c.get(req)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	cs.answering = true
	c.mu.Unlock()
	if err := cs.call.Answer(); err != nil {
		c.mu.Lock()
		cs.answering = false
		c.mu.Unlock()
		return nil, err
	}
	return nil, nil
}

func (c *Calls) handleReject(req json.RawMessage) (any, error) {
	cs, err := c.get(req)
	if err != nil {
		return nil, err
	}
	// A call of ours isn't rejected but hung up.
	if cs.out {
		return nil, c.endNow(cs, cs.call.Hangup, "hangup")
	}
	return nil, c.endNow(cs, cs.call.Reject, "rejected")
}

func (c *Calls) handleHangup(req json.RawMessage) (any, error) {
	cs, err := c.get(req)
	if err != nil {
		return nil, err
	}
	return nil, c.endNow(cs, cs.call.Hangup, "hangup")
}

// endNow runs meowcaller's end of the call, which finishes it before it sends: a failed send
// only means the other side finds out late, so it is logged, not the UI's error. The finish
// is also done here, for a call meowcaller no longer knew.
func (c *Calls) endNow(cs *callState, end func() error, raw string) error {
	if err := end(); err != nil {
		c.log.Warnf("call %s: ending: %v", cs.id, err)
	}
	c.finish(cs, raw)
	return nil
}

// hook puts our callbacks and the audio on the call. Call it with c.mu held.
func (c *Calls) hook(cs *callState) {
	cs.mic = &micSource{}
	cs.call.Play(cs.mic)
	cs.call.Receive(linkSink{cs})
	cs.call.OnStateChange(func(p meowcaller.CallPhase) { c.phase(cs, p) })
	cs.call.OnEnd(func(raw string) { c.finish(cs, raw) })
}

// offer is an incoming call. A second one while a call is live is turned away at once, and is a
// missed call in its chat.
func (c *Calls) offer(call liveCall) {
	c.mu.Lock()
	busy := c.cur != nil
	c.mu.Unlock()
	if busy {
		go c.turnAway(call)
		return
	}
	key, name := c.resolve(call.Peer())
	cs := &callState{call: call, id: call.ID(), chat: key, name: name, offer: time.Now().Unix()}
	c.mu.Lock()
	if c.cur != nil {
		c.mu.Unlock()
		go c.turnAway(call)
		return
	}
	c.cur = cs
	c.hook(cs)
	c.mu.Unlock()
	c.log.Infof("call %s: incoming from %s", cs.id, key)
	c.phase(cs, meowcaller.CallPhaseRinging)
}

func (c *Calls) turnAway(call liveCall) {
	c.log.Infof("call %s: busy, rejected", call.ID())
	if err := call.Reject(); err != nil {
		c.log.Warnf("call %s: reject: %v", call.ID(), err)
	}
	key, name := c.resolve(call.Peer())
	c.writeRow(&callState{id: call.ID(), chat: key, name: name, offer: time.Now().Unix()}, "missed_call", 0, true)
}

// phase follows meowcaller's phase changes; the ending comes through finish.
func (c *Calls) phase(cs *callState, p meowcaller.CallPhase) {
	st := callPhase(p, cs.out)
	if st == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if cs.ended || c.cur != cs || cs.state == st {
		return
	}
	if st == "ringing" {
		c.startRing(cs)
	} else if cs.state == "ringing" {
		c.stopRing(cs)
	}
	cs.state = st
	if st == "active" {
		cs.since = time.Now().UnixMilli()
	}
	c.srv.Emit("call", cs.body(""))
}

// finish ends the call for good: the event, the audio link, the ring and the row. It is
// idempotent, since meowcaller's callback and our own request both come here.
func (c *Calls) finish(cs *callState, raw string) {
	c.mu.Lock()
	if cs.ended {
		c.mu.Unlock()
		return
	}
	cs.ended = true
	if c.cur == cs {
		c.cur = nil
	}
	reason := cs.forced
	if reason == "" {
		reason = callReason(raw)
	}
	c.log.Infof("call %s ended: %s (meowcaller: %q)", cs.id, reason, raw)
	c.stopRing(cs)
	active := cs.since > 0
	secs := 0
	if active {
		secs = int((time.Now().UnixMilli() - cs.since) / 1000)
	}
	cs.state = "ended"
	c.srv.Emit("call", cs.body(reason))
	link := cs.link.Swap(nil)
	c.mu.Unlock()
	if link != nil {
		link.close()
	}

	switch {
	case reason == "elsewhere": // the device that took it has the row
	case active:
		c.writeRow(cs, "call", secs, false)
	case cs.out || cs.answering:
		c.writeRow(cs, "call", -1, false)
	default: // theirs, and never answered, whoever hung up
		c.writeRow(cs, "missed_call", -1, true)
	}
}

// drop ends the live call because the connection to WhatsApp is gone (or the device unlinked).
// Off the caller's goroutine: it is a session event, and Hangup sends.
func (c *Calls) drop() {
	if c == nil {
		return
	}
	c.mu.Lock()
	cs := c.cur
	if cs != nil {
		cs.forced = "offline"
	}
	c.mu.Unlock()
	if cs == nil {
		return
	}
	go func() {
		if cs.call != nil {
			cs.call.Hangup() // nothing to send on, probably
		}
		c.finish(cs, "hangup")
	}()
}

// writeRow writes the call's row in its chat, through the writer, as a live message would: a
// chat that is new is made, the preview follows, and a missed call is unread and alerts. secs < 0
// is no duration.
func (c *Calls) writeRow(cs *callState, text string, secs int, missed bool) {
	row := msgRow{Chat: cs.chat, ID: "call-" + cs.id, TS: cs.offer, Out: cs.out, State: stateRead,
		Kind: msgNotice, Text: text}
	if cs.out {
		row.State = stateSent
	}
	if secs >= 0 {
		b, _ := json.Marshal(map[string]int{"secs": secs})
		row.Extra = string(b)
	}
	c.st.ops <- func(t *storeTx) {
		if !t.hasChat(cs.chat) {
			t.ensureChat(cs.chat, chatUser)
			t.setName(cs.chat, cs.name)
		}
		fresh := t.insertMessage(row)
		t.setLast(cs.chat, row.TS, chatPreview(row), row.Out, row.State)
		switch {
		case missed && fresh:
			t.addUnread(cs.chat, 1)
			t.notifyIncoming(cs.chat)
		default: // the chat's Hub item, if it has one, shows the new preview
			t.hubItem(cs.chat, false, false)
		}
	}
}

// isCallID is whether a message id is a call row's: ours alone, never sent to WhatsApp.
func isCallID(id string) bool { return strings.HasPrefix(id, "call-") }
