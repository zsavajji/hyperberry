package main

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	waLog "github.com/polymorfa/hypermeow/util/log"
)

// notifyFn and withdrawFn are what the notifier calls; tests swap them for recorders.
var (
	notifyFn   = notify
	withdrawFn = withdraw
)

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
}

// notifyIncoming queues the post for a chat that just got a live incoming message, unless the
// chat is muted or on screen. Archived chats notify. Call it after addUnread and setLast.
func (t *storeTx) notifyIncoming(chat string) {
	var name, preview sql.NullString
	var unread int
	var muted int64
	err := t.tx.QueryRow("SELECT name, last_preview, unread, muted_until FROM chats WHERE jid = ?", chat).
		Scan(&name, &preview, &unread, &muted)
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
	t.note(chat, note{true, title, preview.String, unread})
}

func (t *storeTx) note(chat string, n note) {
	if t.notes == nil {
		t.notes = map[string]note{}
	}
	t.notes[chat] = n // the last one in the batch wins: a read after a message withdraws
}

// Notifier posts to the Hub from one goroutine, so BPS calls never run inside the store's writer.
type Notifier struct {
	in      chan map[string]note
	done    chan struct{}
	log     waLog.Logger
	path    string
	post    func(itemID, title, body, payload string) error
	drop    func(itemID string) error
	mu      sync.Mutex
	preview bool
}

func newNotifier(dir string, log waLog.Logger) *Notifier {
	n := &Notifier{in: make(chan map[string]note, 64), done: make(chan struct{}), log: log,
		path: filepath.Join(dir, "prefs.json"), post: notifyFn, drop: withdrawFn, preview: true}
	var p struct{ NotifyPreview *bool }
	if b, err := os.ReadFile(n.path); err == nil && json.Unmarshal(b, &p) == nil && p.NotifyPreview != nil {
		n.preview = *p.NotifyPreview
	}
	go n.run()
	return n
}

func (n *Notifier) run() {
	defer close(n.done)
	for notes := range n.in {
		for chat, nt := range notes {
			n.apply(chat, nt)
		}
	}
}

func (n *Notifier) apply(chat string, nt note) {
	item := hex.EncodeToString([]byte(chat))
	if !nt.post {
		if err := n.drop(item); err != nil {
			n.log.Debugf("withdraw %s: %v", chat, err) // normally nothing was posted
		}
		return
	}
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

// handlePrefs is prefs.set {notifyPreview}. The engine keeps the setting itself since it
// notifies while the UI is closed.
func (n *Notifier) handlePrefs(req json.RawMessage) (any, error) {
	var r struct{ NotifyPreview *bool }
	if json.Unmarshal(req, &r) != nil || r.NotifyPreview == nil {
		return nil, &Error{"bad_request", "prefs.set needs notifyPreview"}
	}
	n.mu.Lock()
	n.preview = *r.NotifyPreview
	n.mu.Unlock()
	b, _ := json.Marshal(map[string]bool{"notifyPreview": *r.NotifyPreview})
	tmp := n.path + ".tmp"
	err := os.WriteFile(tmp, b, 0o600)
	if err == nil {
		err = os.Rename(tmp, n.path)
	}
	return nil, err
}
