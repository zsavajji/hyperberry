package main

import (
	"context"
	"testing"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waAdv"
	"github.com/polymorfa/hypermeow/store/sqlstore"
	"github.com/polymorfa/hypermeow/types"
	waLog "github.com/polymorfa/hypermeow/util/log"
)

// A contact name that landed without an event (the bulk insert at link time) reaches the
// person's chat and their group messages; a person we know no name of keeps what they had.
func TestRenameAll(t *testing.T) {
	ctx := context.Background()
	container, err := sqlstore.New(ctx, "sqlite3", "file:"+t.TempDir()+"/s.db?_foreign_keys=on", waLog.Noop)
	if err != nil {
		t.Fatal(err)
	}
	me := types.NewJID("4915700000009", types.DefaultUserServer)
	dev := container.NewDevice()
	dev.ID = &me
	dev.Account = &waAdv.ADVSignedDeviceIdentity{ // the device row's NOT NULL and length checks
		Details: []byte{0}, AccountSignature: make([]byte, 64), AccountSignatureKey: make([]byte, 32), DeviceSignature: make([]byte, 64)}
	if err := container.PutDevice(ctx, dev); err != nil { // this also gives the device its stores
		t.Fatal(err)
	}
	cli := whatsmeow.NewClient(dev, waLog.Noop)
	anna := types.NewJID("99887766", types.HiddenUserServer)
	if err := dev.Contacts.PutContactName(ctx, anna, "First", "Full Name"); err != nil {
		t.Fatal(err)
	}

	tx, _ := newTestTx(t)
	chat, group, other := anna.String(), "g@g.us", "55@lid"
	tx.ensureChat(chat, chatUser)
	tx.setName(chat, "push name")
	tx.ensureChat(group, chatGroup)
	tx.insertMessage(msgRow{Chat: group, ID: "1", TS: 1, Kind: msgText, Text: "a", Sender: chat, SenderName: "push name"})
	tx.insertMessage(msgRow{Chat: group, ID: "2", TS: 2, Kind: msgText, Text: "b", Sender: other, SenderName: "kept"})
	// Not in the contact store: a push name from the message must not become "+39…".
	mario := "393330000000@s.whatsapp.net"
	tx.insertMessage(msgRow{Chat: group, ID: "3", TS: 3, Kind: msgText, Text: "c", Sender: mario, SenderName: "Mario"})

	new(Store).renameAll(ctx, tx, cli)

	str := func(q string, args ...any) (s string) {
		tx.tx.QueryRow(q, args...).Scan(&s)
		return
	}
	name, sender := str("SELECT name FROM chats WHERE jid = ?", chat), str("SELECT sender_name FROM messages WHERE id = '1'")
	kept, push := str("SELECT sender_name FROM messages WHERE id = '2'"), str("SELECT sender_name FROM messages WHERE id = '3'")
	if name != "Full Name" || sender != "Full Name" || kept != "kept" || push != "Mario" {
		t.Errorf("chat %q, sender %q, unknown sender %q, push name %q", name, sender, kept, push)
	}

	// The names agree now: a second pass writes nothing.
	before := count(t, tx, "SELECT total_changes()")
	new(Store).renameAll(ctx, tx, cli)
	if n := count(t, tx, "SELECT total_changes()") - before; n != 0 {
		t.Errorf("second pass wrote %d rows", n)
	}
}
