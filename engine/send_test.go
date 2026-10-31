package main

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/store/sqlstore"
	"github.com/polymorfa/hypermeow/types"
	waLog "github.com/polymorfa/hypermeow/util/log"
)

// An unlinked session around a fresh device; nothing connects.
func newUnlinked(t *testing.T) *Session {
	t.Helper()
	container, err := sqlstore.New(context.Background(), "sqlite3", "file:"+t.TempDir()+"/s.db?_foreign_keys=on", waLog.Noop)
	if err != nil {
		t.Fatal(err)
	}
	return &Session{cli: whatsmeow.NewClient(container.NewDevice(), waLog.Noop)}
}

func code(err error) string {
	if e, ok := err.(*Error); ok {
		return e.Code
	}
	return "?"
}

func TestSendRejects(t *testing.T) {
	s := newUnlinked(t)
	call := func(h Handler, body string) error {
		_, err := h(json.RawMessage(body))
		return err
	}
	if c := code(call(s.handleSend, `{"chat":"a@s.whatsapp.net","text":"  "}`)); c != "bad_request" {
		t.Errorf("empty text: %s", c)
	}
	if c := code(call(s.handleSend, `{"chat":"a@s.whatsapp.net","text":"hi"}`)); c != "unlinked" {
		t.Errorf("unlinked: %s", c)
	}
	if c := code(call(s.handleGroupInfo, `{"chat":"1@g.us"}`)); c != "unlinked" {
		t.Errorf("group.info, unlinked: %s", c)
	}
	if c := code(call(s.handleContacts, `{}`)); c != "unlinked" {
		t.Errorf("contacts.list, unlinked: %s", c)
	}
	if c := code(call(s.handleVoicePlay, `{"chat":"a@s.whatsapp.net"}`)); c != "bad_request" {
		t.Errorf("voice.play, no id: %s", c)
	}
	if c := code(call(s.handleVoicePlay, `{"chat":"a@s.whatsapp.net","id":"X"}`)); c != "unlinked" {
		t.Errorf("voice.play, unlinked: %s", c)
	}
	if c := code(call(s.handleDelete, `{"chat":"a@s.whatsapp.net","ids":[]}`)); c != "bad_request" {
		t.Errorf("delete, no ids: %s", c)
	}
	if c := code(call(s.handleDelete, `{"chat":"a@s.whatsapp.net","ids":["X"]}`)); c != "unlinked" {
		t.Errorf("delete, unlinked: %s", c)
	}
	// A device ID fakes "linked", so the chat key gets checked.
	me := types.NewJID("4915700000009", types.DefaultUserServer)
	s.cli.Store.ID = &me
	if c := code(call(s.handleSend, `{"chat":"not a jid@@@","text":"hi"}`)); c != "bad_chat" {
		t.Errorf("bad chat: %s", c)
	}
	if c := code(call(s.handleRead, `{"chat":""}`)); c != "bad_chat" {
		t.Errorf("read, empty chat: %s", c)
	}
}

func TestDeleteForMePatch(t *testing.T) {
	chat := types.NewJID("4915700000001", types.DefaultUserServer)
	p := deleteForMePatch(chat, []msgRow{{ID: "A", TS: 1700000000, Out: true}})
	m := p.Mutations[0]
	if want := []string{"deleteMessageForMe", chat.String(), "A", "1", "0"}; !reflect.DeepEqual(m.Index, want) {
		t.Errorf("ours: %v, want %v", m.Index, want)
	}
	if m.Version != 3 || m.Value.DeleteMessageForMeAction.GetMessageTimestamp() != 1700000000 {
		t.Errorf("version %d, action %v", m.Version, m.Value.DeleteMessageForMeAction)
	}
	group := types.NewJID("120363000000000001", types.GroupServer)
	p = deleteForMePatch(group, []msgRow{{ID: "B", Sender: "4915700000002@lid"}})
	if want := []string{"deleteMessageForMe", group.String(), "B", "0", "4915700000002@lid"}; !reflect.DeepEqual(p.Mutations[0].Index, want) {
		t.Errorf("theirs, group: %v, want %v", p.Mutations[0].Index, want)
	}
}
