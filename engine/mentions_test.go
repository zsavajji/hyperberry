package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/proto/waCommon"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"
)

func TestAtNames(t *testing.T) {
	// "@123" must not eat the start of "@1234", whichever order the people come in.
	names := map[string]string{"123": "Bob", "1234": "Anna Lee"}
	if got := atNames("hi @123, @1234 and @12 @123", names); got != "hi @Bob, @Anna Lee and @12 @Bob" {
		t.Errorf("got %q", got)
	}
	if got := atNames("no mentions", nil); got != "no mentions" {
		t.Errorf("got %q", got)
	}
}

func TestWithMentions(t *testing.T) {
	ms := []mention{{"1@lid", "Anna"}, {"", "Me"}}
	var got struct {
		Mentions []mention
		Title    string
		Lat      float64
	}
	for _, extra := range []string{"", `{"title":"T","lat":45.5}`} {
		got.Title, got.Lat = "", 0
		if err := json.Unmarshal([]byte(withMentions(extra, ms)), &got); err != nil || len(got.Mentions) != 2 || got.Mentions[0] != ms[0] {
			t.Fatalf("%q: %+v, %v", extra, got, err)
		}
		if extra != "" && (got.Title != "T" || got.Lat != 45.5) {
			t.Errorf("the link preview's keys were lost: %+v", got)
		}
	}
}

// A received mention shows the person's name, in the row, the chat preview and an edit; the row's
// extra lists who, with the key their page opens by ("" for us).
func TestLiveMention(t *testing.T) {
	st, cli := historyFixture(t)
	tx, _ := newTestTx(t)
	tx.st, st.backlog = st, map[string]bool{} // a backlog only notes the chat, which is all a test needs
	ctx := context.Background()
	group := types.NewJID("120363000000000001", types.GroupServer)
	bob := types.NewJID("4915700000002", types.DefaultUserServer)
	anna := types.NewJID("99887766", types.HiddenUserServer)
	cli.Store.PushName = "Me Myself"
	if err := cli.Store.Contacts.PutContactName(ctx, anna, "Anna", "Anna Lee"); err != nil {
		t.Fatal(err)
	}
	msg := func(id string, m *waE2E.Message) *events.Message {
		return &events.Message{Message: m, Info: types.MessageInfo{ID: id, PushName: "Bob",
			MessageSource: types.MessageSource{Chat: group, Sender: bob, IsGroup: true}, Timestamp: time.Unix(1700000100, 0)}}
	}
	text := func(s string, jids ...string) *waE2E.Message {
		return &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String(s),
			ContextInfo: &waE2E.ContextInfo{MentionedJID: jids}}}
	}

	// anna by LID, us by phone number, a stranger by LID, and one whose "@" isn't in the text.
	st.message(ctx, tx, cli, msg("M1", text("hi @99887766 @4915700000009 @555 bye",
		"99887766@lid", "4915700000009@s.whatsapp.net", "555@lid", "777@lid")))
	var got, extra string
	tx.tx.QueryRow("SELECT text, extra FROM messages WHERE id = 'M1'").Scan(&got, &extra)
	if got != "hi @Anna Lee @Me Myself @555 bye" {
		t.Errorf("text %q", got)
	}
	want := `{"mentions":[{"jid":"99887766@lid","name":"Anna Lee"},{"jid":"","name":"Me Myself"},{"jid":"555@lid","name":"555"}]}`
	if extra != want {
		t.Errorf("extra %s, want %s", extra, want)
	}
	if p := previewOf(tx, group.String()); !strings.Contains(p, "@Anna Lee") {
		t.Errorf("preview %q", p)
	}

	// No mentions: nothing changes, and a text that merely looks like one is left alone.
	st.message(ctx, tx, cli, msg("M2", &waE2E.Message{Conversation: proto.String("mail me @99887766")}))
	tx.tx.QueryRow("SELECT text FROM messages WHERE id = 'M2'").Scan(&got)
	if got != "mail me @99887766" {
		t.Errorf("plain text %q", got)
	}

	st.message(ctx, tx, cli, msg("E1", &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Type: waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(), Key: &waCommon.MessageKey{ID: proto.String("M1")},
		EditedMessage: text("edited @99887766", "99887766@lid")}}))
	tx.tx.QueryRow("SELECT text FROM messages WHERE id = 'M1'").Scan(&got)
	if got != "edited @Anna Lee" {
		t.Errorf("edited text %q", got)
	}
}
