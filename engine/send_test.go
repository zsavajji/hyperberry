package main

import (
	"context"
	"encoding/json"
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
