package main

import (
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"
)

func unavailable(chat, sender types.JID, id string, typ events.UnavailableType) *events.UndecryptableMessage {
	return &events.UndecryptableMessage{IsUnavailable: true, UnavailableType: typ, Info: types.MessageInfo{ID: id,
		Timestamp: time.Unix(1700000000, 0), PushName: "Ann",
		MessageSource: types.MessageSource{Chat: chat, Sender: sender, IsGroup: chat.Server == types.GroupServer}}}
}

// WhatsApp's "unavailable" stanza for view-once media is a notice, unread and alerting like a
// message, once per id; anything else undecryptable stays out of the chat.
func TestViewOnceUnavailable(t *testing.T) {
	st, cli := historyFixture(t)
	lid := types.NewJID("266396202233898", types.HiddenUserServer)
	k := lid.String()
	feed := func(e any) { st.handle(cli, e); st.runOp(func(*storeTx) {}) }
	state := func() (n, unread int, kind int, text, preview string) {
		n = rows(st, "SELECT count(*) FROM messages WHERE chat = ?", k)
		st.db.QueryRow("SELECT unread, last_preview FROM chats WHERE jid = ?", k).Scan(&unread, &preview)
		st.db.QueryRow("SELECT kind, text FROM messages WHERE chat = ? AND id = 'V1'", k).Scan(&kind, &text)
		return
	}

	for _, e := range []*events.UndecryptableMessage{
		unavailable(lid, lid, "X", events.UnavailableTypeUnknown),                                    // another unavailable type
		{Info: unavailable(lid, lid, "Y", "").Info, UnavailableType: events.UnavailableTypeViewOnce}, // a failed decrypt
	} {
		feed(e)
	}
	if n := rows(st, "SELECT count(*) FROM chats") + rows(st, "SELECT count(*) FROM messages"); n != 0 {
		t.Fatalf("%d rows from events that make none", n)
	}

	feed(unavailable(lid, lid, "V1", events.UnavailableTypeViewOnce))
	if n, unread, kind, text, preview := state(); n != 1 || unread != 1 || kind != msgNotice || text != "view_once" || preview != "View once message" {
		t.Errorf("rows %d unread %d kind %d text %q preview %q", n, unread, kind, text, preview)
	}
	if rows(st, "SELECT count(*) FROM messages WHERE chat = ? AND id = 'V1' AND ts = 1700000000 AND out = 0", k) != 1 {
		t.Error("the id, time or direction is wrong")
	}
	feed(unavailable(lid, lid, "V1", events.UnavailableTypeViewOnce))
	if n, unread, _, _, _ := state(); n != 1 || unread != 1 {
		t.Errorf("the same id again: rows %d unread %d, want 1 and 1", n, unread)
	}

	// A real message under that id replaces the notice and is not news again.
	feed(inbound(lid, "V1", &waE2E.Message{Conversation: proto.String("the real one")}, ""))
	var text, preview string
	st.db.QueryRow("SELECT text FROM messages WHERE chat = ? AND id = 'V1'", k).Scan(&text)
	st.db.QueryRow("SELECT last_preview FROM chats WHERE jid = ?", k).Scan(&preview)
	if n, unread, _, _, _ := state(); n != 1 || unread != 1 || text != "the real one" || preview != "the real one" {
		t.Errorf("after the real one: rows %d unread %d text %q preview %q", n, unread, text, preview)
	}
	// A view-once notice again (the phone's answer) leaves one row.
	feed(unavailable(lid, lid, "V2", events.UnavailableTypeViewOnce))
	feed(inbound(lid, "V2", &waE2E.Message{ImageMessage: &waE2E.ImageMessage{}}, "REQ"))
	if n := rows(st, "SELECT count(*) FROM messages WHERE chat = ? AND id = 'V2' AND kind = ?", k, msgPhoto); n != 1 {
		t.Errorf("after the answer to V2: %d photo rows, want 1", n)
	}
	if n := rows(st, "SELECT unread FROM chats WHERE jid = ?", k); n != 2 {
		t.Errorf("unread %d after V2 and its answer, want 2", n)
	}

	// Our own other device's: ours, nothing unread. In a group, with who sent it.
	me := unavailable(lid, lid, "V3", events.UnavailableTypeViewOnce)
	me.Info.IsFromMe = true
	feed(me)
	if n := rows(st, "SELECT count(*) FROM messages WHERE chat = ? AND id = 'V3' AND out = 1", k); n != 1 {
		t.Error("an own view-once is not out")
	}
	if n := rows(st, "SELECT unread FROM chats WHERE jid = ?", k); n != 2 {
		t.Errorf("unread %d after an own view-once, want 2", n)
	}
	g := types.NewJID("120363000000000001", types.GroupServer)
	feed(unavailable(g, lid, "G1", events.UnavailableTypeViewOnce))
	var sender, name, gp string
	st.db.QueryRow("SELECT sender, sender_name FROM messages WHERE chat = ? AND id = 'G1' AND kind = ? AND text = 'view_once'",
		g.String(), msgNotice).Scan(&sender, &name)
	st.db.QueryRow("SELECT last_preview FROM chats WHERE jid = ?", g.String()).Scan(&gp)
	if sender != k || name != "Ann" || gp != "Ann: View once message" {
		t.Errorf("group: sender %q name %q preview %q", sender, name, gp)
	}
}
