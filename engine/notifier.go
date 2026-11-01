package main

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	waLog "github.com/polymorfa/hypermeow/util/log"
)

// notifyFn, withdrawFn and ledFn are what the notifier calls; tests swap them for recorders.
var (
	notifyFn   = notify
	withdrawFn = withdraw
	ledFn      = led
)

// The LED's kinds of chat (step 5.6), as prefs.set's led array orders them, and their requests.
var ledIDs = [3]string{"hyperberry_chats", "hyperberry_groups", "hyperberry_channels"}

func ledKind(chatKind int) int {
	switch chatKind {
	case chatGroup:
		return 1
	case chatNewsletter:
		return 2
	}
	return 0
}

// uiState is what the UI last said with ui.state, for both accounts: the notifier skips the
// chat on screen while fullscreen.
type uiState struct {
	mu         sync.Mutex
	fullscreen bool
	open       string
}

func (u *uiState) set(fullscreen bool, open string) {
	u.mu.Lock()
	u.fullscreen, u.open = fullscreen, open
	u.mu.Unlock()
}

func (u *uiState) seen(chat string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.fullscreen && u.open == chat
}

func (u *uiState) openChat() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.open
}

// note is what a committed transaction wants done to one chat's Hub item: post it (title,
// preview and unread of the chat then) or, with post false, withdraw it.
type note struct {
	post           bool
	title, preview string
	unread         int
	led            int // ledKind
}

// notifyIncoming queues the post for a chat that just got a live incoming message, unless the
// chat is muted or on screen. Archived chats notify. Call it after addUnread and setLast.
func (t *storeTx) notifyIncoming(chat string) {
	var name, preview sql.NullString
	var unread, kind int
	var muted int64
	err := t.tx.QueryRow("SELECT name, last_preview, unread, muted_until, kind FROM chats WHERE jid = ?", chat).
		Scan(&name, &preview, &unread, &muted, &kind)
	if err != nil || muted > time.Now().Unix() || t.st.ui.seen(chat) {
		return
	}
	title := name.String
	if title == "" {
		title, _, _ = strings.Cut(chat, "@")
	}
	if title == "" {
		title = "Unknown"
	}
	t.note(chat, note{true, title, preview.String, unread, ledKind(kind)})
}

func (t *storeTx) note(chat string, n note) {
	if t.notes == nil {
		t.notes = map[string]note{}
	}
	t.notes[chat] = n // the last one in the batch wins: a read after a message withdraws
}

// Notifier posts to the Hub and lights the LED from one goroutine, locked to its thread, so BPS
// calls never run inside the store's writer.
type Notifier struct {
	in      chan map[string]note
	relight chan struct{} // the LED colours changed
	done    chan struct{}
	log     waLog.Logger
	path    string
	post    func(itemID, title, body, payload string) error
	drop    func(itemID string) error
	led     func(id string, rgb, blinks int) error
	// The chats with a notification up, per ledKind: the LED shows a kind's colour while any is.
	// ponytail: kept in memory, so an engine restart leaves the Hub items but drops the LED.
	lit     [3]map[string]bool
	on      [3]int // the colour each kind's request flashes, 0 for none
	mu      sync.Mutex
	preview bool
	colours [3]int // per ledKind, 0x00rrggbb; 0 is off
}

// prefs is prefs.set's request and prefs.json's content.
type prefs struct {
	NotifyPreview *bool   `json:"notifyPreview,omitempty"`
	LED           *[3]int `json:"led,omitempty"`
}

func newNotifier(dir string, log waLog.Logger) *Notifier {
	n := &Notifier{in: make(chan map[string]note, 64), relight: make(chan struct{}, 1),
		done: make(chan struct{}), log: log, path: filepath.Join(dir, "prefs.json"),
		post: notifyFn, drop: withdrawFn, led: ledFn, preview: true,
		colours: [3]int{0xFF0000, 0xFF0000, 0xFF0000}} // red, as the system's own
	var p prefs
	if b, err := os.ReadFile(n.path); err == nil && json.Unmarshal(b, &p) == nil {
		n.set(p)
	}
	for k := range n.lit {
		n.lit[k] = map[string]bool{}
	}
	go n.run()
	return n
}

func (n *Notifier) set(p prefs) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if p.NotifyPreview != nil {
		n.preview = *p.NotifyPreview
	}
	if p.LED != nil {
		n.colours = *p.LED
	}
}

func (n *Notifier) run() {
	defer close(n.done)
	runtime.LockOSThread()
	if err := ledOpen(); err != nil {
		n.log.Warnf("led: %v", err)
	}
	for {
		select {
		case notes, ok := <-n.in:
			if !ok {
				return
			}
			for chat, nt := range notes {
				n.apply(chat, nt)
			}
		case <-n.relight:
			for k := range n.lit {
				if len(n.lit[k]) > 0 {
					n.light(k)
				}
			}
		}
	}
}

// light flashes kind k's colour while any of its chats has a notification up, else stops it.
// Each call is logged: the Q10 kept flashing after a cancel (0.0.1.85), cause unknown.
func (n *Notifier) light(k int) {
	n.mu.Lock()
	rgb := n.colours[k]
	n.mu.Unlock()
	if len(n.lit[k]) == 0 {
		rgb = 0
	}
	id := ledIDs[k]
	if rgb != 0 {
		n.on[k] = rgb
		n.log.Infof("led %s %06x: %v", id, rgb, n.led(id, rgb, 0))
		return
	}
	if n.on[k] == 0 {
		return
	}
	// One blink first, so the request ends by itself should the cancel not take.
	n.log.Infof("led %s once: %v", id, n.led(id, n.on[k], 1))
	n.log.Infof("led %s cancel: %v", id, n.led(id, 0, 0))
	n.on[k] = 0
}

func (n *Notifier) apply(chat string, nt note) {
	item := hex.EncodeToString([]byte(chat))
	if !nt.post {
		if err := n.drop(item); err != nil {
			n.log.Debugf("withdraw %s: %v", chat, err) // normally nothing was posted
		}
		for k := range n.lit {
			if n.lit[k][chat] {
				delete(n.lit[k], chat)
				n.light(k)
			}
		}
		return
	}
	n.lit[nt.led][chat] = true
	n.light(nt.led)
	n.mu.Lock()
	body := nt.preview
	if !n.preview {
		body = "New message"
	}
	n.mu.Unlock()
	if nt.unread > 1 {
		body = fmt.Sprintf("(%d) %s", nt.unread, body)
	}
	if err := n.post(item, nt.title, body, chat); err != nil {
		n.log.Warnf("notify %s: %v", chat, err)
	}
}

// send hands a committed batch's notes over. ponytail: a full queue (64 batches behind a
// stuck BPS) drops the batch rather than block the writer.
func (n *Notifier) send(notes map[string]note) {
	if len(notes) == 0 {
		return
	}
	select {
	case n.in <- notes:
	default:
		n.log.Warnf("notifier is behind, dropping %d notes", len(notes))
	}
}

func (n *Notifier) close() {
	close(n.in)
	<-n.done
}

// handlePrefs is prefs.set {notifyPreview?, led?}. The engine keeps the settings itself since
// it notifies while the UI is closed.
func (n *Notifier) handlePrefs(req json.RawMessage) (any, error) {
	var r prefs
	if json.Unmarshal(req, &r) != nil || r.NotifyPreview == nil && r.LED == nil {
		return nil, &Error{"bad_request", "prefs.set needs notifyPreview or led"}
	}
	if r.LED != nil {
		for _, c := range r.LED {
			if c < 0 || c > 0xFFFFFF {
				return nil, &Error{"bad_request", "led colours are 0x00rrggbb"}
			}
		}
	}
	n.set(r)
	if r.LED != nil {
		select {
		case n.relight <- struct{}{}:
		default: // one is already queued
		}
	}
	n.mu.Lock()
	all := prefs{&n.preview, &n.colours}
	b, _ := json.Marshal(all)
	n.mu.Unlock()
	tmp := n.path + ".tmp"
	err := os.WriteFile(tmp, b, 0o600)
	if err == nil {
		err = os.Rename(tmp, n.path)
	}
	return nil, err
}
