package main

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	waLog "github.com/polymorfa/hypermeow/util/log"
)

// What the notifier calls on the Hub; tests swap them for a recorder.
var (
	hubOpenFn    = udsOpen
	hubAccountFn = udsAccount
	hubItemFn    = udsItem
	hubRemoveFn  = udsRemove
)

// uiState is what the UI last said with ui.state, for both accounts: the chat on screen while
// fullscreen doesn't alert.
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

// note is what a committed transaction wants done to one chat's Hub item. add: a live incoming
// message, so the item may be created; otherwise only an existing item changes. alert: the Hub
// alerts (sound, LED, the instant preview). gone: the chat is deleted, so its item goes and
// nothing else is done. ts is unix seconds.
type note struct {
	add, alert, gone bool
	title, preview   string
	unread           int
	ts               int64
}

// hubItem queues the chat's item as the chat is now: its name, last message and unread count.
// alert asks for the Hub's alert, which stays only for a chat with unread messages that is
// neither muted nor on screen. Call it after the row has its new values.
func (t *storeTx) hubItem(chat string, add, alert bool) {
	var name, preview sql.NullString
	var last sql.NullInt64
	var unread int
	var muted int64
	err := t.tx.QueryRow("SELECT name, last_preview, unread, muted_until, last_ts FROM chats WHERE jid = ?", chat).
		Scan(&name, &preview, &unread, &muted, &last)
	if err != nil {
		return
	}
	alert = alert && unread > 0 && muted <= time.Now().Unix() && !t.st.ui.seen(chat)
	title := name.String
	if title == "" {
		title, _, _ = strings.Cut(chat, "@")
	}
	if title == "" {
		title = "Unknown"
	}
	ts := last.Int64
	if ts == 0 {
		ts = time.Now().Unix() // a chat with no message yet
	}
	t.note(chat, note{add: add, alert: alert, title: title, preview: preview.String, unread: unread, ts: ts})
}

// notifyIncoming queues the item of a chat that just got a live incoming message; a muted or
// on-screen chat gets it without the alert. During the offline backlog the chat waits for
// endBacklog. Call it after addUnread and setLast.
func (t *storeTx) notifyIncoming(chat string) {
	if t.st.backlog != nil {
		t.st.backlog[chat] = true
		return
	}
	t.hubItem(chat, true, true)
}

// endBacklog is the end of the offline backlog: every chat that got a message in it reaches
// the Hub now, as it stands.
func (t *storeTx) endBacklog() {
	chats := t.st.backlog
	t.st.backlog = nil
	for chat := range chats {
		t.hubItem(chat, true, true)
	}
}

// note keeps one note per chat in a batch: the last wins, but it may still create the item or
// alert for what an earlier one in the batch asked. A gone note wins over those: apply checks
// it first.
func (t *storeTx) note(chat string, n note) {
	if t.notes == nil {
		t.notes = map[string]note{}
	}
	if old, ok := t.notes[chat]; ok {
		n.add = n.add || old.add
		n.alert = n.alert || old.alert && n.unread > 0
	}
	t.notes[chat] = n
}

// hubState is what the notifier remembers of the Hub, in hub.json: whether our account is
// there, with which set of item actions, and which chats have an item. The Hub keeps its own
// copy; this tells add from update.
type hubState struct {
	Account bool     `json:"account"`
	Actions int      `json:"actions"`
	Items   []string `json:"items"`
}

// hubActionSet is the item actions an account is added with: 1 was Mark read/unread, 2 adds
// Delete. The Hub fixes them when the account is added, so a newer set means adding it again.
const hubActionSet = 2

// Notifier keeps the Hub account and its items from one goroutine, so UDS calls never run
// inside the store's writer and always on the same thread.
type Notifier struct {
	in      chan func()
	done    chan struct{}
	log     waLog.Logger
	path    string // prefs.json
	state   string // hub.json
	assets  string // our public assets, where the Hub looks for the icons
	open    func(assets string) (fresh bool, err error)
	account func(add bool) error
	item    func(it hubItem, add bool) error
	remove  func(id string) error

	mu      sync.Mutex
	preview bool
	closed  bool

	// Only the run goroutine touches these.
	ready   bool            // registered with the Hub
	acct    bool            // our account is in the Hub
	actions int             // the hubActionSet it was added with; 0 from an older build
	items   map[string]bool // chats with an item in the Hub
}

func newNotifier(dir string, log waLog.Logger) *Notifier {
	n := &Notifier{in: make(chan func(), 64), done: make(chan struct{}), log: log,
		path: filepath.Join(dir, "prefs.json"), state: filepath.Join(dir, "hub.json"), preview: true,
		// dir is /accounts/1000/appdata/<pkg>/data on the phone, and the Hub reads the public
		// assets from /apps/<pkg>/public/.
		assets: "/apps/" + filepath.Base(filepath.Dir(dir)) + "/public/",
		open:   hubOpenFn, account: hubAccountFn, item: hubItemFn, remove: hubRemoveFn, items: map[string]bool{}}
	var p struct{ NotifyPreview *bool }
	if b, err := os.ReadFile(n.path); err == nil && json.Unmarshal(b, &p) == nil && p.NotifyPreview != nil {
		n.preview = *p.NotifyPreview
	}
	var hs hubState
	if b, err := os.ReadFile(n.state); err == nil && json.Unmarshal(b, &hs) == nil {
		n.acct, n.actions = hs.Account, hs.Actions
		for _, c := range hs.Items {
			n.items[c] = true
		}
	}
	go n.run()
	return n
}

// run does the queued work. BPS and UDS belong to a thread, so it keeps this one.
func (n *Notifier) run() {
	runtime.LockOSThread()
	defer close(n.done)
	for f := range n.in {
		f()
	}
}

// opened registers with the Hub on first use, and again on each use until that works. A
// registration the Hub has never seen means it holds nothing of ours, whatever hub.json says.
func (n *Notifier) opened() bool {
	if n.ready {
		return true
	}
	fresh, err := n.open(n.assets)
	if err != nil {
		n.log.Warnf("hub: register: %v", err)
		return false
	}
	n.ready = true
	if fresh {
		n.acct, n.items = false, map[string]bool{}
		n.save()
	}
	n.log.Infof("hub: registered, new to the Hub: %v", fresh)
	return true
}

// save writes hub.json; tmp and rename, as prefs.json.
func (n *Notifier) save() {
	hs := hubState{n.acct, n.actions, make([]string, 0, len(n.items))}
	for c := range n.items {
		hs.Items = append(hs.Items, c)
	}
	sort.Strings(hs.Items)
	b, _ := json.Marshal(hs)
	if err := writeAtomic(n.state, b); err != nil {
		n.log.Warnf("hub: save: %v", err)
	}
}

// ensureAccount adds the account to the Hub unless it is there with the current actions.
func (n *Notifier) ensureAccount() {
	if !n.opened() {
		return
	}
	// An account added by an older build has the old actions, and the Hub only takes them when the
	// account is added. The cost: its items go with it, and each chat's comes back with its next
	// message.
	again := n.acct && n.actions < hubActionSet
	if again {
		n.dropAccount()
	}
	if n.acct {
		return
	}
	if err := n.account(true); err != nil {
		n.log.Warnf("hub: add account: %v", err)
		return
	}
	n.acct, n.actions = true, hubActionSet
	n.save()
	if again {
		n.log.Infof("hub: account added again for its new actions")
	} else {
		n.log.Infof("hub: account added")
	}
}

// dropAccount removes the account, and its items with it.
func (n *Notifier) dropAccount() {
	if !n.opened() {
		return
	}
	if err := n.account(false); err != nil {
		n.log.Debugf("hub: remove account: %v", err) // normally nothing was there
	}
	n.acct, n.items = false, map[string]bool{}
	n.save()
	n.log.Infof("hub: account removed")
}

// apply brings one chat's item to what the note says.
func (n *Notifier) apply(chat string, nt note) {
	if nt.gone {
		n.forget(chat)
		return
	}
	if !n.opened() {
		return
	}
	known := n.items[chat]
	if !known && !nt.add {
		return
	}
	n.ensureAccount()
	if !n.acct {
		return
	}
	known = n.items[chat] // a re-added account holds none
	if !known && !nt.add {
		return
	}
	n.mu.Lock()
	preview := n.preview
	n.mu.Unlock()
	body := nt.preview
	if !preview {
		body = ""
		if nt.unread != 0 {
			body = "New message"
		}
	}
	unread := nt.unread
	if unread < 0 {
		unread = 1 // marked unread by hand
	}
	it := hubItem{hubItemID(chat), nt.title, body, unread, nt.ts * 1000, nt.alert}
	// The Hub may have lost the item, or never had one we remember: if the call is refused,
	// the other one is tried.
	add := !known
	err := n.item(it, add)
	if err != nil && (!known || nt.add) {
		add = !add
		err = n.item(it, add)
	}
	verb := "update"
	if add {
		verb = "add"
	}
	if err != nil {
		n.log.Warnf("hub: %s %s: %v", verb, chat, err)
		return
	}
	n.log.Infof("hub: %s %s alert=%v", verb, chat, nt.alert)
	if !known {
		n.items[chat] = true
		n.save()
	}
}

// forget takes one chat's item out of the Hub, if there is one. A refusal only logs: the item
// may be gone already, and it is forgotten either way.
func (n *Notifier) forget(chat string) {
	if !n.opened() || !n.items[chat] {
		return
	}
	if err := n.remove(hubItemID(chat)); err != nil {
		n.log.Warnf("hub: remove %s: %v", chat, err)
	}
	delete(n.items, chat)
	n.save()
	n.log.Infof("hub: remove %s", chat)
}

// removeItem is the Hub's Delete: the chat's item goes, the chat stays, and its next message
// brings the item back.
// The Hub asked, so the item is there even when hub.json lost track of it: known it is.
func (n *Notifier) removeItem(chat string) {
	n.queue(func() {
		if n.opened() {
			n.items[chat] = true
			n.forget(chat)
		}
	}, "the removal")
}

// queue puts f on the notifier's thread. ponytail: a full queue (64 behind a stuck UDS) drops
// the work rather than block the writer; a dropped unlink leaves the account until the next one.
func (n *Notifier) queue(f func(), what string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		return
	}
	select {
	case n.in <- f:
	default:
		n.log.Warnf("notifier is behind, dropping %s", what)
	}
}

// send hands a committed batch's notes over.
func (n *Notifier) send(notes map[string]note) {
	if len(notes) == 0 {
		return
	}
	n.queue(func() {
		for chat, nt := range notes {
			n.apply(chat, nt)
		}
	}, "the notes")
}

// link makes sure the account is in the Hub: a new link's chats are in.
func (n *Notifier) link() { n.queue(n.ensureAccount, "the link") }

// unlink takes the account out: the chats are gone.
func (n *Notifier) unlink() { n.queue(n.dropAccount, "the unlink") }

func (n *Notifier) close() {
	n.mu.Lock()
	n.closed = true
	close(n.in)
	n.mu.Unlock()
	<-n.done
}

// writeAtomic writes path through a temp file and a rename, so a crash leaves the old one.
func writeAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	err := os.WriteFile(tmp, b, 0o600)
	if err == nil {
		err = os.Rename(tmp, path)
	}
	return err
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
	return nil, writeAtomic(n.path, b)
}
