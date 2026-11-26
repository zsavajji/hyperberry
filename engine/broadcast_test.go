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

// A message to someone's broadcast list lands in their chat, not in a chat of the list.
func TestBroadcastInSendersChat(t *testing.T) {
	st, cli := historyFixture(t)
	tx, _ := newTestTx(t)
	tx.st, st.backlog = st, map[string]bool{}
	shop := types.NewJID("4915700000009", types.DefaultUserServer)
	list := types.NewJID("1700000000", types.BroadcastServer)
	st.message(context.Background(), tx, cli, &events.Message{Message: &waE2E.Message{Conversation: proto.String("sale")},
		Info: types.MessageInfo{ID: "B1", PushName: "Shop", Timestamp: time.Unix(1700000100, 0),
			MessageSource: types.MessageSource{Chat: list, Sender: shop}}})
	var chat string
	if err := tx.tx.QueryRow("SELECT chat FROM messages WHERE id = 'B1'").Scan(&chat); err != nil {
		t.Fatal(err)
	}
	if chat != shop.String() {
		t.Errorf("chat %s, want %s", chat, shop)
	}
	if tx.hasChat(list.String()) {
		t.Error("a chat for the list")
	}
}
