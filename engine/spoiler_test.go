package main

import (
	"context"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"
)

func TestWithKey(t *testing.T) {
	if got := withKey("", "spoiler", "true"); got != `{"spoiler":true}` {
		t.Errorf("empty: %s", got)
	}
	if got := withKey(`{"gif":true}`, "spoiler", "true"); !sameJSON(got, `{"gif":true,"spoiler":true}`) {
		t.Errorf("kept: %s", got)
	}
	if got := withKey("nope", "spoiler", "true"); got != "nope" {
		t.Errorf("bad: %s", got)
	}
}

// Only a photo or video in the spoiler wrapper is marked; the wrapper is gone by the time the row is made.
func TestLiveSpoiler(t *testing.T) {
	st, cli := historyFixture(t)
	tx, _ := newTestTx(t)
	tx.st, st.backlog = st, map[string]bool{}
	ctx := context.Background()
	bob := types.NewJID("4915700000002", types.DefaultUserServer)
	msg := func(id string, m *waE2E.Message) *events.Message {
		return &events.Message{Message: m, Info: types.MessageInfo{ID: id, PushName: "Bob",
			MessageSource: types.MessageSource{Chat: bob, Sender: bob}, Timestamp: time.Unix(1700000100, 0)}}
	}
	photo := func() *waE2E.Message {
		return &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Mimetype: proto.String("image/jpeg")}}
	}
	wrap := func(m *waE2E.Message) *waE2E.Message {
		return &waE2E.Message{SpoilerMessage: &waE2E.FutureProofMessage{Message: m}}
	}
	st.message(ctx, tx, cli, msg("S1", wrap(photo())))
	st.message(ctx, tx, cli, msg("P1", photo()))
	st.message(ctx, tx, cli, msg("T1", wrap(&waE2E.Message{Conversation: proto.String("secret")})))
	for id, want := range map[string]struct {
		kind    int
		spoiler bool
	}{"S1": {msgPhoto, true}, "P1": {msgPhoto, false}, "T1": {msgText, false}} {
		var kind int
		var extra string
		if err := tx.tx.QueryRow("SELECT kind, COALESCE(extra, '') FROM messages WHERE id = ?", id).Scan(&kind, &extra); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if kind != want.kind || (extra != "" && sameJSON(extra, `{"spoiler":true}`)) != want.spoiler {
			t.Errorf("%s: kind %d extra %q", id, kind, extra)
		}
	}
}

// A forward is marked with its score, whether it came wrapped or with the flag; a plain message isn't.
func TestLiveForwarded(t *testing.T) {
	st, cli := historyFixture(t)
	tx, _ := newTestTx(t)
	tx.st, st.backlog = st, map[string]bool{}
	ctx := context.Background()
	bob := types.NewJID("4915700000002", types.DefaultUserServer)
	send := func(id string, m *waE2E.Message) {
		st.message(ctx, tx, cli, &events.Message{Message: m, Info: types.MessageInfo{ID: id, PushName: "Bob",
			MessageSource: types.MessageSource{Chat: bob, Sender: bob}, Timestamp: time.Unix(1700000100, 0)}})
	}
	text := func(ci *waE2E.ContextInfo) *waE2E.Message {
		return &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("hi"), ContextInfo: ci}}
	}
	send("W1", &waE2E.Message{BotForwardedMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{Conversation: proto.String("hi")}}})
	send("F1", text(&waE2E.ContextInfo{IsForwarded: proto.Bool(true), ForwardingScore: proto.Uint32(2)}))
	send("M1", text(&waE2E.ContextInfo{IsForwarded: proto.Bool(true), ForwardingScore: proto.Uint32(7)}))
	send("P1", text(nil))
	for id, want := range map[string]string{"W1": `{"forwarded":1}`, "F1": `{"forwarded":2}`, "M1": `{"forwarded":7}`, "P1": ""} {
		var extra string
		if err := tx.tx.QueryRow("SELECT COALESCE(extra, '') FROM messages WHERE id = ?", id).Scan(&extra); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if extra != want && !(want != "" && sameJSON(extra, want)) {
			t.Errorf("%s: extra %q, want %q", id, extra, want)
		}
	}
}
