package main

import (
	"encoding/hex"
	"fmt"
	"sync"
	"testing"
	"time"

	waLog "github.com/polymorfa/hypermeow/util/log"
)

type posted struct{ item, title, body, payload string }

// fakeHub records what the notifier posts and withdraws instead of calling BPS.
type fakeHub struct {
	mu    sync.Mutex
	posts []posted
	gone  []string
	leds  []string // "id rgb blinks"
}

func (h *fakeHub) list() ([]posted, []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]posted(nil), h.posts...), append([]string(nil), h.gone...)
}

// wait polls until n posts and w withdraws are in, then a short beat for extras.
func (h *fakeHub) wait(t *testing.T, n, w int) ([]posted, []string) {
	t.Helper()
	for end := time.Now().Add(2 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if p, g := h.list(); len(p) >= n && len(g) >= w {
			break
		}
	}
	time.Sleep(150 * time.Millisecond)
	p, g := h.list()
	if len(p) != n || len(g) != w {
		t.Fatalf("%d posts %v, %d withdraws %v; want %d and %d", len(p), p, len(g), g, n, w)
	}
	return p, g
}

// hubSim is a linked sim whose notifier talks to a fakeHub. The seed posts nothing.
func hubSim(t *testing.T) (*Sim, *Store, *conn, *fakeHub) {
	h := &fakeHub{}
	post, drop, light := notifyFn, withdrawFn, ledFn
	notifyFn = func(item, title, body, payload string) error {
		h.mu.Lock()
		h.posts = append(h.posts, posted{item, title, body, payload})
		h.mu.Unlock()
		return nil
	}
	withdrawFn = func(item string) error {
		h.mu.Lock()
		h.gone = append(h.gone, item)
		h.mu.Unlock()
		return nil
	}
	ledFn = func(id string, rgb, blinks int) error {
		h.mu.Lock()
		h.leds = append(h.leds, fmt.Sprintf("%s %06x %d", id, rgb, blinks))
		h.mu.Unlock()
		return nil
	}
	t.Cleanup(func() { notifyFn, withdrawFn, ledFn = post, drop, light }) // after the store's own cleanup
	s, st, c := linkedSim(t)
	h.wait(t, 0, 0)
	return s, st, c, h
}

func msg(s *Sim, n int, text string) {
	s.handleMsg([]byte(fmt.Sprintf(`{"n":%d,"text":%q}`, n, text)))
}

func TestNotifyLiveMessage(t *testing.T) {
	s, _, _, h := hubSim(t)
	chat := simChats[3].jid
	msg(s, 3, "one")
	p, _ := h.wait(t, 1, 0)
	if p[0] != (posted{hex.EncodeToString([]byte(chat)), simChats[3].name, "one", chat}) {
		t.Errorf("first: %+v", p[0])
	}
	msg(s, 3, "two")
	p, _ = h.wait(t, 2, 0)
	if p[1].item != p[0].item || p[1].body != "(2) two" {
		t.Errorf("second: %+v", p[1])
	}
}

func TestNotifySkips(t *testing.T) {
	s, _, _, h := hubSim(t)
	msg(s, 11, "forever") // muted always
	msg(s, 14, "8 hours") // muted for a while
	h.wait(t, 0, 0)
	// ours don't: the sim's send never reaches the notifier
	chat := simChats[0].jid
	s.handleUIState([]byte(`{"fullscreen":true,"openChat":"` + chat + `"}`))
	msg(s, 0, "seen")
	h.wait(t, 0, 0)
	s.handleUIState([]byte(`{"fullscreen":false,"openChat":"` + chat + `"}`))
	msg(s, 0, "unseen")
	p, _ := h.wait(t, 1, 0)
	if p[0].body != "(2) unseen" {
		t.Errorf("after fullscreen ends: %+v", p[0])
	}
}

func TestNotifyOutgoingAndRead(t *testing.T) {
	s, _, _, h := hubSim(t)
	chat := simChats[2].jid // 3 unread
	if _, err := s.handleSend([]byte(`{"chat":"` + chat + `","text":"hi"}`)); err != nil {
		t.Fatal(err)
	}
	h.wait(t, 0, 0)
	if _, err := s.handleRead([]byte(`{"chat":"` + chat + `"}`)); err != nil {
		t.Fatal(err)
	}
	_, g := h.wait(t, 0, 1)
	if g[0] != hex.EncodeToString([]byte(chat)) {
		t.Errorf("withdrew %s", g[0])
	}
}

func TestNotifyPreviewOff(t *testing.T) {
	s, st, _, h := hubSim(t)
	if _, err := st.nt.handlePrefs([]byte(`{"notifyPreview":false}`)); err != nil {
		t.Fatal(err)
	}
	msg(s, 3, "secret")
	msg(s, 3, "secret") // same batch: one post, with the count
	p, _ := h.wait(t, 1, 0)
	if p[0].body != "(2) New message" {
		t.Errorf("%+v", p)
	}
}

func TestPrefsPersist(t *testing.T) {
	dir := t.TempDir()
	n := newNotifier(dir, waLog.Noop)
	if !n.preview {
		t.Error("default: want previews on")
	}
	if _, err := n.handlePrefs([]byte(`{}`)); err == nil {
		t.Error("prefs.set with nothing: want an error")
	}
	n.handlePrefs([]byte(`{"notifyPreview":false}`))
	n.handlePrefs([]byte(`{"led":[255,0,16776960]}`)) // keeps notifyPreview
	n.close()
	n = newNotifier(dir, waLog.Noop)
	defer n.close()
	if n.preview {
		t.Error("a new engine on the same dir lost notifyPreview")
	}
	if n.colours != [3]int{255, 0, 16776960} {
		t.Errorf("a new engine on the same dir has led %06x", n.colours)
	}
}

// The LED follows each kind's notifications in its colour (step 5.6), and a colour change
// relights what's up.
func TestNotifyLED(t *testing.T) {
	s, st, _, h := hubSim(t)
	leds := func(want ...string) {
		t.Helper()
		time.Sleep(150 * time.Millisecond)
		h.mu.Lock()
		got := fmt.Sprint(h.leds)
		h.leds = nil
		h.mu.Unlock()
		if got != fmt.Sprint(want) {
			t.Errorf("leds %s, want %v", got, want)
		}
	}
	msg(s, 2, "chat") // Marco
	h.wait(t, 1, 0)
	leds("hyperberry_chats ff0000 0")
	msg(s, 1, "group") // Family
	h.wait(t, 2, 0)
	leds("hyperberry_groups ff0000 0")
	if _, err := st.nt.handlePrefs([]byte(`{"led":[65280,0,255]}`)); err != nil {
		t.Fatal(err)
	}
	// Groups off: one blink, then the cancel. Channels have nothing up.
	leds("hyperberry_chats 00ff00 0", "hyperberry_groups ff0000 1", "hyperberry_groups 000000 0")
	msg(s, 1, "again") // a kind that's off lights nothing
	h.wait(t, 3, 0)
	leds()
	if _, err := s.handleRead([]byte(`{"chat":"` + simChats[2].jid + `"}`)); err != nil {
		t.Fatal(err)
	}
	h.wait(t, 3, 1)
	leds("hyperberry_chats 00ff00 1", "hyperberry_chats 000000 0")
	if _, err := st.nt.handlePrefs([]byte(`{"led":[16777216,0,0]}`)); err == nil {
		t.Error("a colour past 0xFFFFFF: want an error")
	}
}
