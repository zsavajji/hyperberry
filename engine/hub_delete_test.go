package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The Hub's Delete (step 5.8c): the item goes, the chat stays. The fake hub and helpers are in
// notifier_test.go.

// hubItems reads the chats hub.json says have an item.
func hubItems(t *testing.T, st *Store) []string {
	t.Helper()
	var hs hubState
	b, err := os.ReadFile(st.nt.state)
	if err != nil || json.Unmarshal(b, &hs) != nil {
		t.Fatalf("hub.json: %v %s", err, b)
	}
	return hs.Items
}

func TestHubDelete(t *testing.T) {
	s, st, _, h := hubSim(t)
	chat := simChats[3].jid
	msg(s, 3, "one")
	flush(t, st)
	h.take()
	// The Hub's Delete takes the item and nothing else: the chat keeps its row and unread count.
	runHub(st, invocation{action: hubDelete, uri: "pim:" + hubItemID(chat)})
	flush(t, st)
	if it := only(t, h.take(), "remove"); it.id != hubItemID(chat) {
		t.Errorf("removed %q, want %q", it.id, hubItemID(chat))
	}
	if items := hubItems(t, st); len(items) != 0 {
		t.Errorf("hub.json still has %v", items)
	}
	eventually(t, st, 1, "SELECT count(*) FROM chats WHERE jid = ? AND unread = 1", chat)
	// Its next message brings the item back as a new one.
	msg(s, 3, "two")
	flush(t, st)
	item(t, only(t, h.take(), "add"), 3, "two", 2, true)
	// The Hub asked, so the remove goes even for an item hub.json lost track of; an invocation
	// with no chat in it does nothing.
	runHub(st, invocation{action: hubDelete, data: []byte(`{"id":"` + hubItemID(simChats[2].jid) + `"}`)},
		invocation{action: hubDelete, uri: "pim:nothing"})
	flush(t, st)
	only(t, h.take(), "remove")
}

func TestHubChatDeleted(t *testing.T) {
	s, st, _, h := hubSim(t)
	chat := simChats[3].jid
	msg(s, 3, "one")
	flush(t, st)
	h.take()
	if _, err := s.handleChatDelete([]byte(`{"chat":"` + chat + `"}`)); err != nil {
		t.Fatal(err)
	}
	flush(t, st)
	if it := only(t, h.take(), "remove"); it.id != hubItemID(chat) {
		t.Errorf("removed %q, want %q", it.id, hubItemID(chat))
	}
	if items := hubItems(t, st); len(items) != 0 {
		t.Errorf("hub.json still has %v", items)
	}
}

// A gone note wins over what the same batch asked for the chat.
func TestHubGoneNote(t *testing.T) {
	tx := &storeTx{}
	tx.note("a", note{add: true, alert: true, unread: 1, title: "A"})
	tx.note("a", note{gone: true})
	n := tx.notes["a"]
	if !n.gone {
		t.Errorf("note %+v: want gone", n)
	}
	// Merged with the add before it, it still adds nothing for a chat with no item.
	h := &fakeHub{}
	h.install(t)
	if got := ops(notifier(h, t.TempDir(), func(nt *Notifier) { nt.send(map[string]note{"a": n}) })); got != "open" {
		t.Errorf("gone note: %s", got)
	}
}

// An account added by an older build has the old actions: it is added again, once.
func TestHubActionsReadded(t *testing.T) {
	h := &fakeHub{}
	h.install(t)
	dir, chat := t.TempDir(), "a@s.whatsapp.net"
	old := `{"account":true,"items":["` + chat + `"]}` // no "actions"
	state := filepath.Join(dir, "hub.json")
	if err := os.WriteFile(state, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ops(notifier(h, dir, func(n *Notifier) { n.link() })); got != "open account- account+" {
		t.Errorf("old account: %s", got)
	}
	var hs hubState
	if b, _ := os.ReadFile(state); json.Unmarshal(b, &hs) != nil || !hs.Account || hs.Actions != 2 || len(hs.Items) != 0 {
		t.Errorf("hub.json after: %+v", hs)
	}
	if got := ops(notifier(h, dir, func(n *Notifier) { n.link() })); got != "open account+" { // the once-a-run update
		t.Errorf("current account: %s", got)
	}
	// A change to a chat whose item went with the old account does nothing; a message adds it.
	os.WriteFile(state, []byte(old), 0o600)
	nt := note{title: "A", unread: 1, ts: 100}
	if got := ops(notifier(h, dir, func(n *Notifier) { n.send(map[string]note{chat: nt}) })); got != "open account- account+" {
		t.Errorf("update after the old account: %s", got)
	}
	os.WriteFile(state, []byte(old), 0o600)
	nt.add = true
	if got := ops(notifier(h, dir, func(n *Notifier) { n.send(map[string]note{chat: nt}) })); got != "open account- account+ add" {
		t.Errorf("message after the old account: %s", got)
	}
}
