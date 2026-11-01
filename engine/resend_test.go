package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"
)

func TestResendRejects(t *testing.T) {
	s := newUnlinked(t)
	call := func(body string) error {
		_, err := s.handleResend(json.RawMessage(body))
		return err
	}
	if c := code(call(`{"chat":"a@s.whatsapp.net"}`)); c != "bad_request" {
		t.Errorf("no id: %s", c)
	}
	if c := code(call(`{"id":"X"}`)); c != "bad_request" {
		t.Errorf("no chat: %s", c)
	}
	if c := code(call(`{"chat":"a@s.whatsapp.net","id":"X"}`)); c != "unlinked" {
		t.Errorf("unlinked: %s", c)
	}
	me := types.NewJID("4915700000009", types.DefaultUserServer)
	s.cli.Store.ID = &me
	if c := code(call(`{"chat":"not a jid@@@","id":"X"}`)); c != "bad_chat" {
		t.Errorf("bad chat: %s", c)
	}
}

// unknownMsg is a message content() has no kind for: it is stored as "Message".
func unknownMsg() *waE2E.Message {
	return &waE2E.Message{InteractiveMessage: &waE2E.InteractiveMessage{}}
}

func inbound(chat types.JID, id string, m *waE2E.Message, requestID string) *events.Message {
	return &events.Message{Message: m, UnavailableRequestID: requestID, Info: types.MessageInfo{ID: id,
		Timestamp: time.Unix(1700000000, 0), MessageSource: types.MessageSource{Chat: chat, Sender: chat}}}
}

// The request is for a message chats.db holds, and goes to the phone (here the send is what
// fails: no session with the phone, so the error is the library's, not one of ours).
func TestResendHandler(t *testing.T) {
	st, cli := historyFixture(t)
	s := &Session{cli: cli, store: st}
	chat := types.NewJID("4915700000001", types.DefaultUserServer)
	req := json.RawMessage(`{"chat":"` + chat.String() + `","id":"M1"}`)
	if _, err := s.handleResend(req); code(err) != "not_found" {
		t.Errorf("no such message: %v", err)
	}
	st.handle(cli, inbound(chat, "M1", unknownMsg(), ""))
	if _, err := s.handleResend(req); err == nil || code(err) != "?" {
		t.Errorf("a stored message: %v, want the send to be tried", err)
	}
}

// The phone's answer replaces the row of the same id and alerts nothing again; an answer for a
// message we never stored is a new message.
func TestResendAnswerReplaces(t *testing.T) {
	st, cli := historyFixture(t)
	chat := types.NewJID("4915700000001", types.DefaultUserServer)
	k := chat.String()
	st.handle(cli, inbound(chat, "M1", unknownMsg(), ""))
	st.runOp(func(*storeTx) {})
	if n := rows(st, "SELECT unread FROM chats WHERE jid = ?", k); n != 1 {
		t.Fatalf("unread %d, want 1", n)
	}

	st.handle(cli, inbound(chat, "M1", &waE2E.Message{Conversation: proto.String("the real text")}, "REQ1"))
	st.runOp(func(*storeTx) {})
	var text, preview string
	st.db.QueryRow("SELECT text FROM messages WHERE chat = ? AND id = 'M1'", k).Scan(&text)
	st.db.QueryRow("SELECT last_preview FROM chats WHERE jid = ?", k).Scan(&preview)
	if n := rows(st, "SELECT count(*) FROM messages WHERE chat = ?", k); n != 1 || text != "the real text" || preview != "the real text" {
		t.Errorf("%d rows, text %q, preview %q", n, text, preview)
	}
	if n := rows(st, "SELECT unread FROM chats WHERE jid = ?", k); n != 1 {
		t.Errorf("unread %d after the replace, want 1", n)
	}

	// An answer that shows nothing leaves the row alone.
	st.handle(cli, inbound(chat, "M1", &waE2E.Message{MessageContextInfo: &waE2E.MessageContextInfo{}}, "REQ2"))
	st.runOp(func(*storeTx) {})
	if n := rows(st, "SELECT count(*) FROM messages WHERE chat = ? AND text = 'the real text'", k); n != 1 {
		t.Error("an empty answer took the row")
	}

	st.handle(cli, inbound(chat, "M2", &waE2E.Message{Conversation: proto.String("new")}, "REQ3"))
	st.runOp(func(*storeTx) {})
	if n := rows(st, "SELECT unread FROM chats WHERE jid = ?", k); n != 2 {
		t.Errorf("unread %d after a message we never had, want 2", n)
	}
}

// Our own phone's protocol messages are no chat; its answers to a resend (no category) are.
func TestPeerMessageMakesNoChat(t *testing.T) {
	st, cli := historyFixture(t)
	lid := types.NewJID("266396202233898", types.HiddenUserServer)
	peer := inbound(lid, "KEYS", &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		AppStateSyncKeyShare: &waE2E.AppStateSyncKeyShare{}}}, "")
	peer.Info.Category, peer.Info.IsFromMe = "peer", true
	st.handle(cli, peer)
	st.runOp(func(*storeTx) {})
	if n := rows(st, "SELECT count(*) FROM chats"); n != 0 {
		t.Errorf("%d chats after a peer message, want 0", n)
	}
	st.handle(cli, inbound(lid, "M1", &waE2E.Message{Conversation: proto.String("hi")}, ""))
	st.runOp(func(*storeTx) {})
	if n := rows(st, "SELECT count(*) FROM chats"); n != 1 {
		t.Errorf("%d chats after a message, want 1", n)
	}
}

// At connect the empty chats with ourselves go, with the Hub item; one with messages stays;
// WhatsApp's account gets its name.
func TestConnectedDropsEmptySelfChats(t *testing.T) {
	st, cli := historyFixture(t)
	pn, lid := cli.Store.GetJID().ToNonAD(), types.NewJID("266396202233898", types.HiddenUserServer)
	cli.Store.LID = lid
	other := types.NewJID("4915700000001", types.DefaultUserServer)
	st.runOp(func(t *storeTx) {
		for _, j := range []types.JID{pn, lid, other, types.PSAJID} {
			t.ensureChat(j.String(), chatUser)
		}
		t.setName(types.PSAJID.String(), "+0")
		t.insertMessage(msgRow{Chat: other.String(), ID: "O1", TS: 1, Kind: msgText, Text: "hi"})
	})
	st.handle(cli, &events.Connected{})
	st.runOp(func(*storeTx) {})
	var name string
	st.db.QueryRow("SELECT name FROM chats WHERE jid = ?", types.PSAJID.String()).Scan(&name)
	if n := rows(st, "SELECT count(*) FROM chats WHERE jid IN (?, ?)", pn.String(), lid.String()); n != 0 || name != "WhatsApp" {
		t.Errorf("%d own chats left, WhatsApp's named %q", n, name)
	}
	if n := rows(st, "SELECT count(*) FROM chats"); n != 2 {
		t.Errorf("%d chats, want the other person's and WhatsApp's", n)
	}

	// "Message yourself" with a message in it stays.
	st.runOp(func(t *storeTx) {
		t.ensureChat(lid.String(), chatUser)
		t.insertMessage(msgRow{Chat: lid.String(), ID: "S1", TS: 2, Out: true, Kind: msgText, Text: "note"})
	})
	st.handle(cli, &events.Connected{})
	st.runOp(func(*storeTx) {})
	if n := rows(st, "SELECT count(*) FROM chats WHERE jid = ?", lid.String()); n != 1 {
		t.Error("a chat with ourselves that holds a message was dropped")
	}
}
