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

func TestIsChannel(t *testing.T) {
	for key, want := range map[string]bool{"120363@newsletter": true, "120363@g.us": false, "1@s.whatsapp.net": false, "": false} {
		if isChannel(key) != want {
			t.Errorf("isChannel(%q) != %v", key, want)
		}
	}
}

func post(id string, ts int64, m *waE2E.Message) *types.NewsletterMessage {
	return &types.NewsletterMessage{MessageID: id, MessageServerID: 1, Type: "text", Timestamp: time.Unix(ts, 0), Message: m}
}

func unreadOf(tx *storeTx, chat string) (n int) {
	tx.tx.QueryRow("SELECT unread FROM chats WHERE jid = ?", chat).Scan(&n)
	return
}

func TestWritePosts(t *testing.T) {
	st, cli := historyFixture(t)
	tx, _ := newTestTx(t)
	tx.st = st
	ctx := context.Background()
	ch := types.NewJID("120363000000000009", types.NewsletterServer)
	key := ch.String()
	tx.ensureChat(key, chatNewsletter)
	text := func(s string) *waE2E.Message { return &waE2E.Message{Conversation: proto.String(s)} }
	photo := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("pic"), Mimetype: proto.String("image/jpeg")}}

	first := []*types.NewsletterMessage{post("P1", 1700000100, text("hello")), post("P2", 1700000200, photo), post("P3", 1700000300, nil)}
	st.writePosts(ctx, tx, cli, ch, first)
	var kind1, kind2, n int
	var ts int64
	tx.tx.QueryRow("SELECT kind, ts FROM messages WHERE id = 'P1'").Scan(&kind1, &ts)
	tx.tx.QueryRow("SELECT kind FROM messages WHERE id = 'P2'").Scan(&kind2)
	n = tx.messageCount(key)
	if kind1 != msgText || ts != 1700000100 || kind2 != msgPhoto || n != 2 {
		t.Errorf("kinds %d %d, ts %d, %d rows (a nil message is skipped)", kind1, kind2, ts, n)
	}
	if p := previewOf(tx, key); p == "" {
		t.Error("no preview")
	}
	if u := unreadOf(tx, key); u != 0 {
		t.Errorf("first fetch: unread %d", u)
	}

	st.writePosts(ctx, tx, cli, ch, first) // the same posts again
	if u := unreadOf(tx, key); u != 0 {
		t.Errorf("refetch: unread %d", u)
	}
	st.writePosts(ctx, tx, cli, ch, append([]*types.NewsletterMessage{post("P4", 1700000400, text("news"))}, first...))
	if u := unreadOf(tx, key); u != 1 {
		t.Errorf("one new post: unread %d", u)
	}
}

func TestChannelKeys(t *testing.T) {
	tx, _ := newTestTx(t)
	tx.ensureChat("1@newsletter", chatNewsletter)
	tx.ensureChat("g@g.us", chatGroup)
	tx.ensureChat("2@s.whatsapp.net", chatUser)
	if got := tx.channelKeys(); len(got) != 1 || got[0] != "1@newsletter" {
		t.Errorf("channelKeys %v", got)
	}
}

// A live post is unread but never a note for the Hub.
func TestLivePostIsSilent(t *testing.T) {
	st, cli := historyFixture(t)
	tx, _ := newTestTx(t)
	tx.st, st.backlog = st, map[string]bool{} // a note shows as a backlog entry
	ch := types.NewJID("120363000000000009", types.NewsletterServer)
	st.message(context.Background(), tx, cli, &events.Message{Message: &waE2E.Message{Conversation: proto.String("hi")},
		Info: types.MessageInfo{ID: "L1", Timestamp: time.Unix(1700000500, 0), MessageSource: types.MessageSource{Chat: ch, Sender: ch}}})
	if u := unreadOf(tx, ch.String()); u != 1 {
		t.Errorf("unread %d", u)
	}
	if len(st.backlog) != 0 {
		t.Errorf("notes %v", st.backlog)
	}
}
