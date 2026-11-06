package main

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/types/events"
	waLog "github.com/polymorfa/hypermeow/util/log"
)

// hubCall is one call the notifier made on the Hub: op is open, account+, account-, add, update
// or remove (item has just the id).
type hubCall struct {
	op   string
	item hubItem
}

// fakeHub stands in for the Hub: it records the calls and answers them as set.
type fakeHub struct {
	mu    sync.Mutex
	calls []hubCall
	fresh bool   // what open says: the Hub never saw the app
	deny  string // the item op, add or update, that the Hub refuses
}

func (h *fakeHub) rec(op string, it hubItem) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, hubCall{op, it})
	if op == h.deny {
		return errors.New("refused")
	}
	return nil
}

func (h *fakeHub) set(fresh bool, deny string) {
	h.mu.Lock()
	h.fresh, h.deny = fresh, deny
	h.mu.Unlock()
}

// install points the notifier at h until the test ends. Call it before the notifier is made.
func (h *fakeHub) install(t *testing.T) {
	open, account, item, remove := hubOpenFn, hubAccountFn, hubItemFn, hubRemoveFn
	hubOpenFn = func(string) (bool, error) {
		h.rec("open", hubItem{})
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.fresh, nil
	}
	hubAccountFn = func(add bool) error {
		if add {
			return h.rec("account+", hubItem{})
		}
		return h.rec("account-", hubItem{})
	}
	hubItemFn = func(it hubItem, add bool) error {
		if add {
			return h.rec("add", it)
		}
		return h.rec("update", it)
	}
	hubRemoveFn = func(id string) error { return h.rec("remove", hubItem{id: id}) }
	t.Cleanup(func() { hubOpenFn, hubAccountFn, hubItemFn, hubRemoveFn = open, account, item, remove }) // after the store's own
}

// take returns the calls since the last take.
func (h *fakeHub) take() []hubCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := h.calls
	h.calls = nil
	return c
}

// wait polls until n calls are in.
func (h *fakeHub) wait(t *testing.T, n int) {
	t.Helper()
	for end := time.Now().Add(2 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		h.mu.Lock()
		got := len(h.calls)
		h.mu.Unlock()
		if got >= n {
			return
		}
	}
	t.Fatalf("fewer than %d calls: %v", n, h.take())
}

// ops lists the calls' ops: "open account+ add".
func ops(c []hubCall) string {
	s := make([]string, len(c))
	for i, x := range c {
		s[i] = x.op
	}
	return strings.Join(s, " ")
}

// only wants exactly one call, of op, and gives its item.
func only(t *testing.T, c []hubCall, op string) hubItem {
	t.Helper()
	if len(c) != 1 || c[0].op != op {
		t.Fatalf("calls %q %v; want one %s", ops(c), c, op)
	}
	return c[0].item
}

// flush waits until everything queued on the store so far has reached the Hub's calls.
func flush(t *testing.T, st *Store) {
	t.Helper()
	done := make(chan struct{})
	st.afterCommit(func() { st.nt.queue(func() { close(done) }, "the test") })
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the notifier didn't catch up")
	}
}

// hubSim is a linked sim whose notifier talks to a fakeHub. The link opens the Hub, clears it
// (the wipe) and adds the account; the seed makes no items.
func hubSim(t *testing.T) (*Sim, *Store, *conn, *fakeHub) {
	h := &fakeHub{}
	h.install(t)
	s, st, c := linkedSim(t)
	h.wait(t, 3)
	flush(t, st)
	if got := ops(h.take()); got != "open account- account+" {
		t.Fatalf("after the link: %s", got)
	}
	return s, st, c, h
}

func msg(s *Sim, n int, text string) {
	s.handleMsg([]byte(fmt.Sprintf(`{"n":%d,"text":%q}`, n, text)))
}

// item is what the Hub should have been told of simChats[n] with these values; the time is
// the message's, checked apart.
func item(t *testing.T, got hubItem, n int, body string, unread int, alert bool) {
	t.Helper()
	if age := time.Now().UnixMilli() - got.ts; age < 0 || age > 60000 {
		t.Errorf("ts %d is not now", got.ts)
	}
	got.ts = 0
	if want := (hubItem{hubItemID(simChats[n].jid), simChats[n].name, body, unread, 0, alert}); got != want {
		t.Errorf("item %+v; want %+v", got, want)
	}
}

func TestHubLiveMessage(t *testing.T) {
	s, st, _, h := hubSim(t)
	msg(s, 3, "one")
	flush(t, st)
	item(t, only(t, h.take(), "add"), 3, "one", 1, true)
	msg(s, 3, "two")
	flush(t, st)
	item(t, only(t, h.take(), "update"), 3, "two", 2, true)
}

func TestHubSilentItems(t *testing.T) {
	s, st, _, h := hubSim(t)
	// Muted chats and the chat on screen get their item, but the Hub doesn't alert.
	msg(s, 11, "forever") // muted always, 41 unread
	msg(s, 14, "8 hours") // muted for a while
	flush(t, st)
	c := h.take()
	if ops(c) != "add add" {
		t.Fatalf("muted: %v", c)
	}
	for _, x := range c {
		if x.item.alert {
			t.Errorf("a muted chat alerts: %+v", x.item)
		}
		// The batch's notes come in map order, so the item is found by its id.
		if x.item.id == hubItemID(simChats[11].jid) && x.item.unread != 42 {
			t.Errorf("unread %d, want 42", x.item.unread)
		}
	}
	chat := simChats[0].jid
	s.handleUIState([]byte(`{"fullscreen":true,"openChat":"` + chat + `"}`))
	msg(s, 0, "seen")
	flush(t, st)
	item(t, only(t, h.take(), "add"), 0, "seen", 1, false)
	s.handleUIState([]byte(`{"fullscreen":false,"openChat":"` + chat + `"}`))
	msg(s, 0, "unseen")
	flush(t, st)
	item(t, only(t, h.take(), "update"), 0, "unseen", 2, true)
}

func TestHubArchivedSilent(t *testing.T) {
	s, st, _, h := hubSim(t)
	n := -1
	for i, c := range simChats {
		if c.archived {
			n = i
			break
		}
	}
	if n < 0 {
		t.Fatal("no archived chat in the sim")
	}
	msg(s, n, "hush")
	flush(t, st)
	item(t, only(t, h.take(), "add"), n, "hush", 1, false)
}

func TestHubReadAndSend(t *testing.T) {
	s, st, _, h := hubSim(t)
	chat := simChats[2].jid // 3 unread, no item
	if _, err := s.handleSend([]byte(`{"chat":"` + chat + `","text":"hi"}`)); err != nil {
		t.Fatal(err)
	}
	flush(t, st)
	if c := h.take(); len(c) != 0 {
		t.Errorf("a send: %v", c)
	}
	if _, err := s.handleRead([]byte(`{"chat":"` + simChats[1].jid + `"}`)); err != nil { // 12 unread, no item
		t.Fatal(err)
	}
	flush(t, st)
	if c := h.take(); len(c) != 0 {
		t.Errorf("a read of a chat with no item: %v", c)
	}
	msg(s, 3, "one")
	flush(t, st)
	h.take()
	if _, err := s.handleRead([]byte(`{"chat":"` + simChats[3].jid + `"}`)); err != nil {
		t.Fatal(err)
	}
	flush(t, st)
	item(t, only(t, h.take(), "update"), 3, "one", 0, false)
}

func TestHubPreviewOff(t *testing.T) {
	s, st, _, h := hubSim(t)
	if _, err := st.nt.setPrefs([]byte(`{"notifyPreview":false}`)); err != nil {
		t.Fatal(err)
	}
	msg(s, 3, "secret")
	flush(t, st)
	item(t, only(t, h.take(), "add"), 3, "New message", 1, true)
	s.handleRead([]byte(`{"chat":"` + simChats[3].jid + `"}`))
	flush(t, st)
	item(t, only(t, h.take(), "update"), 3, "", 0, false)
}

func TestHubSimRemoved(t *testing.T) {
	s, st, c, h := hubSim(t)
	msg(s, 3, "one")
	flush(t, st)
	h.take()
	s.handleRemoved(nil)
	flush(t, st)
	if got := ops(h.take()); got != "account-" {
		t.Fatalf("after the removal: %s", got)
	}
	// Linking again brings the account back, and a message the item, which is new again.
	next(t, c, "link") // unlinked
	s.handleQR(nil)
	next(t, c, "link") // qr
	s.handleLink(nil)
	h.wait(t, 2)
	flush(t, st)
	if got := ops(h.take()); got != "account- account+" { // the new link clears first
		t.Fatalf("after the new link: %s", got)
	}
	msg(s, 3, "two")
	flush(t, st)
	item(t, only(t, h.take(), "add"), 3, "two", 1, true)
}

func TestHubBacklog(t *testing.T) {
	s, st, _, h := hubSim(t)
	st.handle(nil, &events.OfflineSyncPreview{})
	msg(s, 3, "one")
	msg(s, 5, "two")
	s.handleRead([]byte(`{"chat":"` + simChats[5].jid + `"}`)) // read on the main phone meanwhile
	flush(t, st)
	if c := h.take(); len(c) != 0 {
		t.Fatalf("during the backlog: %v", c)
	}
	st.handle(nil, &events.OfflineSyncCompleted{})
	flush(t, st)
	c := h.take()
	if ops(c) != "add add" {
		t.Fatalf("at its end: %v", c)
	}
	for _, x := range c {
		switch x.item.id {
		case hubItemID(simChats[3].jid):
			item(t, x.item, 3, "one", 1, true)
		case hubItemID(simChats[5].jid):
			item(t, x.item, 5, "two", 0, false)
		default:
			t.Errorf("item %+v", x.item)
		}
	}
	msg(s, 3, "three") // the backlog is over: live again
	flush(t, st)
	item(t, only(t, h.take(), "update"), 3, "three", 2, true)
}

// notifier runs f on a new notifier over dir and gives the calls it made.
func notifier(h *fakeHub, dir string, f func(*Notifier)) []hubCall {
	n := newNotifier(dir, waLog.Noop)
	f(n)
	n.close()
	return h.take()
}

func TestHubStateKept(t *testing.T) {
	h := &fakeHub{}
	h.install(t)
	dir, chat := t.TempDir(), "a@s.whatsapp.net"
	one := map[string]note{chat: {add: true, alert: true, title: "A", preview: "hi", unread: 1, ts: 100}}
	send := func(n *Notifier) { n.send(one) }
	if got := ops(notifier(h, dir, send)); got != "open account+ add" {
		t.Errorf("first message: %s", got)
	}
	// A new engine on the same dir: the account is there, and the item is known.
	if got := ops(notifier(h, dir, send)); got != "open update" {
		t.Errorf("after a restart: %s", got)
	}
	// The Hub never saw this registration: it holds nothing, whatever the file says.
	h.set(true, "")
	if got := ops(notifier(h, dir, send)); got != "open account+ add" {
		t.Errorf("fresh registration: %s", got)
	}
	h.set(false, "")
	// An unlink takes the account, and the next message brings it and the item back.
	if got := ops(notifier(h, dir, func(n *Notifier) { n.unlink(); n.send(one) })); got != "open account- account+ add" {
		t.Errorf("unlink: %s", got)
	}
	// Only a note that may create the item makes one.
	if got := ops(notifier(h, t.TempDir(), func(n *Notifier) { n.send(map[string]note{chat: {unread: 0}}) })); got != "open" {
		t.Errorf("a read of a chat with no item: %s", got)
	}
}

func TestHubRefusals(t *testing.T) {
	h := &fakeHub{}
	h.install(t)
	dir, chat := t.TempDir(), "a@s.whatsapp.net"
	send := func(add bool) func(*Notifier) {
		return func(n *Notifier) { n.send(map[string]note{chat: {add: add, title: "A", unread: 1, ts: 100}}) }
	}
	// An item the Hub has already, that we don't remember, is updated when the add is refused.
	h.set(false, "add")
	if got := ops(notifier(h, dir, send(true))); got != "open account+ add update" {
		t.Errorf("add refused: %s", got)
	}
	// One the Hub lost is added again for a message, but a plain change leaves it be.
	h.set(false, "update")
	if got := ops(notifier(h, dir, send(true))); got != "open update add" {
		t.Errorf("update refused: %s", got)
	}
	if got := ops(notifier(h, dir, send(false))); got != "open update" {
		t.Errorf("update refused, no message: %s", got)
	}
}

func TestHubChat(t *testing.T) {
	chat := "393330000001@s.whatsapp.net"
	id := hubItemID(chat)
	for in, want := range map[string]string{
		"pim:" + id: chat,
		`{"sourceId":"` + id + `","accountId":1212305497}`: chat,
		"data://local " + id: chat,
		"":                   "", "data://local": "", "chat_zz": "", "chat_abc": "", // not hex, or half a byte
	} {
		if got := hubChat(in); got != want {
			t.Errorf("hubChat(%q) = %q, want %q", in, got, want)
		}
	}
}

// runHub delivers the Hub's invocations to hubActions, which returns once it has handled them.
func runHub(st *Store, invs ...invocation) {
	in := make(chan invocation, len(invs))
	for _, i := range invs {
		in <- i
	}
	close(in)
	hubActions(st.srv, in, st.nt.removeItem, waLog.Noop)
}

func TestHubActions(t *testing.T) {
	s, st, _, h := hubSim(t)
	chat := simChats[3].jid
	run := func(invs ...invocation) { runHub(st, invs...) }
	unread := func(want int) {
		t.Helper()
		eventually(t, st, 1, "SELECT count(*) FROM chats WHERE jid = ? AND unread = ?", chat, want)
	}
	if _, err := st.srv.Call("nope", nil); err == nil {
		t.Error("Call of a missing command: want an error")
	}
	run(invocation{action: hubMarkUnread, uri: "pim:" + hubItemID(chat)})
	unread(-1)
	run(invocation{action: hubMarkRead, data: []byte(`{"id":"` + hubItemID(chat) + `"}`)})
	unread(0)
	// Neither an action of another kind nor one with no chat in it changes anything.
	run(invocation{action: "com.zsavajji.hyperberry.START", uri: "pim:" + hubItemID(chat)},
		invocation{action: hubMarkUnread, uri: "pim:nothing"})
	flush(t, st)
	unread(0)
	if c := h.take(); len(c) != 0 { // no item for the chat: the changes made no calls
		t.Errorf("calls %v", c)
	}
	// With an item, the change reaches the Hub: marked unread by hand counts as one.
	msg(s, 3, "one")
	flush(t, st)
	h.take()
	run(invocation{action: hubMarkRead, uri: "pim:" + hubItemID(chat)})
	flush(t, st)
	item(t, only(t, h.take(), "update"), 3, "one", 0, false)
	run(invocation{action: hubMarkUnread, uri: "pim:" + hubItemID(chat)})
	flush(t, st)
	item(t, only(t, h.take(), "update"), 3, "one", 1, false)
	unread(-1)
}

func TestPrefsPersist(t *testing.T) {
	dir := t.TempDir()
	n := newNotifier(dir, waLog.Noop)
	if !n.notifyPreview() {
		t.Error("default: want previews on")
	}
	if _, err := n.setPrefs([]byte(`{}`)); err == nil {
		t.Error("prefs.set with nothing: want an error")
	}
	n.setPrefs([]byte(`{"notifyPreview":false}`))
	n.close()
	n = newNotifier(dir, waLog.Noop)
	defer n.close()
	if n.notifyPreview() {
		t.Error("a new engine on the same dir lost notifyPreview")
	}
}
