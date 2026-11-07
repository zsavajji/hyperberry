package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/types"
	waLog "github.com/polymorfa/hypermeow/util/log"
	"github.com/purpshell/meowcaller"
)

// fakeCall stands in for meowcaller's *Call.
type fakeCall struct {
	id    string
	peer  types.JID
	mu    sync.Mutex
	phase meowcaller.CallPhase
	onSt  func(meowcaller.CallPhase)
	onEnd func(string)
	src   meowcaller.AudioSource
	sink  meowcaller.AudioSink
	calls []string // "answer", "reject", "hangup"
}

func (f *fakeCall) ID() string      { return f.id }
func (f *fakeCall) Peer() types.JID { return f.peer }
func (f *fakeCall) State() meowcaller.CallPhase {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.phase
}
func (f *fakeCall) OnStateChange(fn func(meowcaller.CallPhase)) {
	f.mu.Lock()
	f.onSt = fn
	f.mu.Unlock()
}
func (f *fakeCall) OnEnd(fn func(string)) { f.mu.Lock(); f.onEnd = fn; f.mu.Unlock() }
func (f *fakeCall) Play(s meowcaller.AudioSource) *meowcaller.Player {
	f.src = s
	return meowcaller.NewPlayer()
}
func (f *fakeCall) Receive(s meowcaller.AudioSink) { f.sink = s }

func (f *fakeCall) did(what string) {
	f.mu.Lock()
	f.calls = append(f.calls, what)
	f.mu.Unlock()
}

func (f *fakeCall) count(what string) (n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c == what {
			n++
		}
	}
	return
}

// setPhase is meowcaller moving the call on.
func (f *fakeCall) setPhase(p meowcaller.CallPhase) {
	f.mu.Lock()
	f.phase = p
	fn := f.onSt
	f.mu.Unlock()
	if fn != nil {
		fn(p)
	}
}

// end is meowcaller's finishCall.
func (f *fakeCall) end(reason string) {
	f.mu.Lock()
	if f.phase == meowcaller.CallPhaseEnded {
		f.mu.Unlock()
		return
	}
	f.phase = meowcaller.CallPhaseEnded
	fn := f.onEnd
	f.mu.Unlock()
	if fn != nil {
		fn(reason)
	}
}

func (f *fakeCall) Answer() error {
	f.did("answer")
	f.setPhase(meowcaller.CallPhaseConnecting)
	return nil
}
func (f *fakeCall) Reject() error { f.did("reject"); f.end("rejected"); return nil }
func (f *fakeCall) Hangup() error { f.did("hangup"); f.end("hangup"); return nil }

const annaKey = "111@lid"

// callRig is Calls on a real writer, a fake Hub and a connected UI.
type callRig struct {
	ts   *testServer
	st   *Store
	hub  *fakeHub
	c    *Calls
	ui   *conn
	next *fakeCall // what dial gives
	mu   sync.Mutex
}

func newCallRig(t *testing.T) *callRig {
	t.Helper()
	ringStart = func(string) (func(), error) { return func() {}, nil }
	t.Cleanup(func() { ringStart = startRing })
	hub := &fakeHub{}
	hub.install(t)
	ts := newTestServer(t)
	db, err := openChats(t.TempDir() + "/c.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	st := newStore(db, ts.Server, waLog.Noop)
	t.Cleanup(st.close)
	r := &callRig{ts: ts, st: st, hub: hub}
	r.c = newCalls(st, ts.Server, waLog.Noop)
	r.c.ready = func(chat string) (string, error) { return "Anna", nil }
	r.c.resolve = func(peer types.JID) (string, string) { return annaKey, "Anna" }
	r.c.dial = func(_ context.Context, target string) (liveCall, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.next, nil
	}
	r.ui = ts.login(t)
	return r
}

func (r *callRig) fake(id string) *fakeCall {
	return &fakeCall{id: id, peer: types.NewJID("111", types.HiddenUserServer), phase: meowcaller.CallPhaseRinging}
}

// ev reads the next call event, skipping the store's.
func (r *callRig) ev(t *testing.T) map[string]any {
	t.Helper()
	for {
		if m := r.ui.read(t); m["ev"] == "call" {
			return m
		}
	}
}

func (r *callRig) want(t *testing.T, state, reason string) map[string]any {
	t.Helper()
	m := r.ev(t)
	if m["state"] != state || m["reason"] != reason {
		t.Fatalf("event %v; want %s/%q", m, state, reason)
	}
	return m
}

func (r *callRig) req(cmd, id string) error {
	_, err := r.ts.handlers[cmd]([]byte(fmt.Sprintf(`{"id":%q,"chat":%q}`, id, annaKey)))
	return err
}

// row is the call row of id, after the writer and the notifier caught up: text, out, state, extra,
// and the chat's unread count. ok is false when there is none.
func (r *callRig) row(t *testing.T, id string) (text string, out bool, state int, extra string, unread int, ok bool) {
	t.Helper()
	flush(t, r.st)
	var ex *string
	err := r.st.db.QueryRow(`SELECT text, out, state, extra, (SELECT unread FROM chats WHERE jid = ?2)
FROM messages WHERE chat = ?2 AND id = ?1 AND kind = ?3`, "call-"+id, annaKey, msgNotice).Scan(&text, &out, &state, &ex, &unread)
	if err != nil {
		return "", false, 0, "", 0, false
	}
	if ex != nil {
		extra = *ex
	}
	return text, out, state, extra, unread, true
}

func (r *callRig) answered(t *testing.T, f *fakeCall) {
	t.Helper()
	r.c.offer(f)
	r.want(t, "ringing", "")
	if err := r.req("call.answer", f.id); err != nil {
		t.Fatal(err)
	}
	r.want(t, "connecting", "")
	f.setPhase(meowcaller.CallPhaseActive)
	r.want(t, "active", "")
	r.c.mu.Lock()
	r.c.cur.since -= 5000 // five seconds of talk
	r.c.mu.Unlock()
}

func TestCallRows(t *testing.T) {
	r := newCallRig(t)

	// Theirs, answered: a call row with its length, nothing unread.
	f := r.fake("A1")
	r.answered(t, f)
	f.end("hangup")
	r.want(t, "ended", "hangup")
	if text, out, state, extra, unread, ok := r.row(t, "A1"); !ok || text != "call" || out || state != stateRead ||
		extra != `{"secs":5}` || unread != 0 {
		t.Errorf("answered in: %q out=%v state=%d %s unread=%d ok=%v", text, out, state, extra, unread, ok)
	}

	// Ours, answered.
	f = r.fake("B1")
	f.phase = meowcaller.CallPhaseCalling
	r.next = f
	if err := r.req("call.start", ""); err != nil {
		t.Fatal(err)
	}
	r.want(t, "calling", "")
	f.setPhase(meowcaller.CallPhaseActive)
	r.want(t, "active", "")
	r.c.mu.Lock()
	r.c.cur.since -= 3000
	r.c.mu.Unlock()
	if err := r.req("call.hangup", "B1"); err != nil {
		t.Fatal(err)
	}
	r.want(t, "ended", "hangup")
	if text, out, state, extra, _, ok := r.row(t, "B1"); !ok || text != "call" || !out || state != stateSent || extra != `{"secs":3}` {
		t.Errorf("answered out: %q out=%v state=%d %s ok=%v", text, out, state, extra, ok)
	}

	// Ours, declined by them: a call row, no length.
	f = r.fake("C1")
	f.phase = meowcaller.CallPhaseCalling
	r.next = f
	r.req("call.start", "")
	r.want(t, "calling", "")
	f.end("rejected")
	r.want(t, "ended", "declined")
	if text, out, _, extra, _, ok := r.row(t, "C1"); !ok || text != "call" || !out || extra != "" {
		t.Errorf("unanswered out: %q out=%v %q ok=%v", text, out, extra, ok)
	}

	// Theirs, never answered (they hung up, WhatsApp's reason empty): missed, unread, the Hub.
	r.hub.take()
	f = r.fake("D1")
	r.c.offer(f)
	r.want(t, "ringing", "")
	f.end("")
	r.want(t, "ended", "hangup")
	if text, out, _, extra, unread, ok := r.row(t, "D1"); !ok || text != "missed_call" || out || extra != "" || unread != 1 {
		t.Errorf("missed: %q out=%v %q unread=%d ok=%v", text, out, extra, unread, ok)
	}
	calls := r.hub.take()
	var ring, gone, chatItem bool
	for _, c := range calls {
		switch {
		case c.op == "add" && c.item.id == callItemID(annaKey):
			ring = c.item.alert && c.item.body == "Incoming voice call" && c.item.title == "Anna"
		case c.op == "remove" && c.item.id == callItemID(annaKey):
			gone = true
		case (c.op == "add" || c.op == "update") && c.item.id == hubItemID(annaKey):
			chatItem = c.item.alert && c.item.body == "Missed voice call" && c.item.unread == 1
		}
	}
	if !ring || !gone || !chatItem {
		t.Errorf("hub: ring item %v, removed %v, chat item %v: %v", ring, gone, chatItem, calls)
	}

	// Theirs, declined by us: missed too, and the other side hears it.
	f = r.fake("E1")
	r.c.offer(f)
	r.want(t, "ringing", "")
	if err := r.req("call.reject", "E1"); err != nil {
		t.Fatal(err)
	}
	r.want(t, "ended", "declined")
	if text, _, _, _, unread, ok := r.row(t, "E1"); !ok || text != "missed_call" || unread != 2 || f.count("reject") != 1 {
		t.Errorf("declined by us: %q unread=%d ok=%v", text, unread, ok)
	}

	// Answered on another device of ours: no row.
	f = r.fake("F1")
	r.c.offer(f)
	r.want(t, "ringing", "")
	f.end("accepted_elsewhere")
	r.want(t, "ended", "elsewhere")
	if _, _, _, _, _, ok := r.row(t, "F1"); ok {
		t.Error("elsewhere wrote a row")
	}
	if _, _, _, _, unread, _ := r.row(t, "E1"); unread != 2 {
		t.Errorf("elsewhere changed unread: %d", unread)
	}
}

func TestCallEventSequenceAndHello(t *testing.T) {
	r := newCallRig(t)
	f := r.fake("S1")
	r.c.offer(f)
	r.want(t, "ringing", "")

	// A UI that comes while it rings finds the call right after the hello reply.
	late := r.ts.dial(t)
	late.send(t, fmt.Sprintf(`{"@":"h","cmd":"hello","token":%q,"proto":1}`, r.ts.token))
	if m := late.read(t); m["ok"] != true {
		t.Fatal(m)
	}
	if m := late.read(t); m["ev"] != "call" || m["state"] != "ringing" || m["chat"] != annaKey || m["out"] != false ||
		m["id"] != "S1" || m["since"] != float64(0) {
		t.Errorf("after hello: %v", m)
	}

	if err := r.req("call.answer", "nope"); err == nil || err.(*Error).Code != "not_found" {
		t.Errorf("answer of another id: %v", err)
	}
	r.req("call.answer", "S1")
	r.want(t, "connecting", "")
	f.setPhase(meowcaller.CallPhaseActive)
	if m := r.want(t, "active", ""); m["since"].(float64) == 0 {
		t.Errorf("active without since: %v", m)
	}
	f.setPhase(meowcaller.CallPhaseActive) // the same phase twice is one event
	r.c.drop()                             // the connection went
	m := r.want(t, "ended", "offline")
	if m["since"].(float64) == 0 || f.count("hangup") != 1 {
		t.Errorf("ended: %v, hangups %d", m, f.count("hangup"))
	}
	if r.c.cur != nil {
		t.Error("the call is still live")
	}
	if err := r.req("call.hangup", "S1"); err == nil || err.(*Error).Code != "not_found" {
		t.Errorf("hangup after the end: %v", err)
	}
}

func TestSecondOfferRejected(t *testing.T) {
	r := newCallRig(t)
	f := r.fake("P1")
	r.c.offer(f)
	r.want(t, "ringing", "")
	g := r.fake("P2")
	r.c.offer(g)
	for i := 0; g.count("reject") == 0 && i < 200; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if g.count("reject") != 1 {
		t.Fatal("the second offer wasn't rejected")
	}
	if text, _, _, _, unread, ok := r.row(t, "P2"); !ok || text != "missed_call" || unread != 1 {
		t.Errorf("second offer's row: %q unread=%d ok=%v", text, unread, ok)
	}
	if f.count("reject") != 0 || r.c.cur == nil || r.c.cur.id != "P1" {
		t.Error("the first call was disturbed")
	}
	// And a call of ours is busy.
	if err := r.req("call.start", ""); err == nil || err.(*Error).Code != "busy" {
		t.Errorf("start while live: %v", err)
	}
}

func TestSimCalls(t *testing.T) {
	ts := newTestServer(t)
	registerSimCalls(ts.Server)
	c := ts.login(t)
	c.send(t, `{"@":"1","cmd":"call.start","chat":"x"}`)
	if m := c.read(t); errCode(m) != "simulated" {
		t.Errorf("%v", m)
	}
	c.send(t, `{"@":"2","cmd":"call.answer","id":"x"}`)
	if m := c.read(t); errCode(m) != "not_found" {
		t.Errorf("%v", m)
	}
}

func TestCallReasonAndRing(t *testing.T) {
	for raw, want := range map[string]string{
		"": "hangup", "hangup": "hangup", "rejected": "declined", "accepted_elsewhere": "elsewhere",
		"busy": "busy", "timeout": "no_answer", "server:404": "failed", "offer_failed": "failed", "odd": "failed",
	} {
		if got := callReason(raw); got != want {
			t.Errorf("callReason(%q) = %s, want %s", raw, got, want)
		}
	}
	dir := t.TempDir()
	tone := dir + "/tone.m4a"
	pps := dir + "/cat.phone"
	write := func(p, s string) {
		if err := writeAtomic(p, []byte(s)); err != nil {
			t.Fatal(err)
		}
	}
	old := ringPPS
	ringPPS = pps
	defer func() { ringPPS = old }()
	write(pps, "@cat.phone\nsound::on\ntone::"+tone+"\n")
	if got := ringFile(ringPPS); got != fallbackRing {
		t.Errorf("tone that doesn't exist: %s", got)
	}
	write(tone, "x")
	if got := ringFile(ringPPS); got != tone {
		t.Errorf("phone's tone: %s", got)
	}
	ringPPS = dir + "/none"
	if got := ringFile(ringPPS); !strings.HasSuffix(got, "notification_phone_ringer.wav") {
		t.Errorf("no PPS: %s", got)
	}
}

func TestCallSilence(t *testing.T) {
	r := newCallRig(t)
	stops := make(chan struct{}, 4)
	ringStart = func(string) (func(), error) { return func() { stops <- struct{}{} }, nil }
	f := r.fake("S1")
	r.c.offer(f)
	r.want(t, "ringing", "")
	r.hub.take()
	if err := r.req("call.silence", "nope"); err == nil {
		t.Error("silence of an unknown call: no error")
	}
	for i := 0; i < 2; i++ { // the second is a no-op
		if err := r.req("call.silence", "S1"); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-stops:
	case <-time.After(2 * time.Second):
		t.Fatal("the ringtone was not stopped")
	}
	for _, c := range r.hub.take() {
		if c.op == "remove" {
			t.Fatalf("silence removed the Hub item: %v", c)
		}
	}
	f.end("")
	r.want(t, "ended", "hangup")
	r.row(t, "S1") // lets the notifier catch up
	gone := false
	for _, c := range r.hub.take() {
		gone = gone || c.op == "remove" && c.item.id == callItemID(annaKey)
	}
	if !gone {
		t.Error("the Hub item stayed after the call ended")
	}
}
