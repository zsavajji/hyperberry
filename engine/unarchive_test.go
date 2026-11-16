package main

import (
	"context"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/proto/waSyncAction"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"
)

// "Keep chats archived" off on the phone (the UnarchiveChatsSetting event) brings an archived chat back
// with its next incoming message; on, or never set, it stays archived. Our own messages never do.
func TestUnarchiveSetting(t *testing.T) {
	st, cli := historyFixture(t)
	tx, _ := newTestTx(t)
	tx.st, st.backlog = st, map[string]bool{} // a backlog only notes the chat, which is all a test needs
	ctx := context.Background()
	bob := types.NewJID("4915700000002", types.DefaultUserServer)
	key := bob.String()
	n := 0
	msg := func(fromMe bool) {
		n++
		st.message(ctx, tx, cli, &events.Message{Message: &waE2E.Message{Conversation: proto.String("hi")},
			Info: types.MessageInfo{ID: string(rune('A' + n)), Timestamp: time.Unix(1700000000+int64(n), 0),
				MessageSource: types.MessageSource{Chat: bob, Sender: bob, IsFromMe: fromMe}}})
	}
	setting := func(unarchive bool) {
		st.dispatch(tx, cli, &events.UnarchiveChatsSetting{Action: &waSyncAction.UnarchiveChatsSetting{UnarchiveChats: proto.Bool(unarchive)}})
	}
	archived := func() bool { return count(t, tx, "SELECT archived FROM chats WHERE jid = ?", key) == 1 }

	msg(false)
	tx.setArchived(key, true)
	msg(false)
	if !archived() {
		t.Error("no setting: a message unarchived the chat")
	}
	setting(false)
	msg(false)
	if !archived() {
		t.Error("keep archived: a message unarchived the chat")
	}
	setting(true)
	msg(true)
	if !archived() {
		t.Error("our own message unarchived the chat")
	}
	msg(false)
	if archived() {
		t.Error("unarchive on: an incoming message left the chat archived")
	}
	if count(t, tx, "SELECT unread FROM chats WHERE jid = ?", key) != 4 {
		t.Error("the unread count lost a message")
	}
}
