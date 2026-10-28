package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/polymorfa/hypermeow/store/sqlstore"
	"github.com/polymorfa/hypermeow/types"
	waLog "github.com/polymorfa/hypermeow/util/log"
)

// newTestTx opens a fresh chats.db and a transaction on it; the tx rolls back at cleanup.
func newTestTx(t *testing.T) (*storeTx, *sql.DB) {
	t.Helper()
	db, err := openChats(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tx.Rollback() })
	return &storeTx{tx: tx, log: waLog.Noop, chats: map[string]bool{}, msgs: map[string]map[string]bool{}}, db
}

func count(t *testing.T, tx *storeTx, q string, args ...any) (n int) {
	t.Helper()
	if err := tx.tx.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestOnePersonOneChat(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	container, err := sqlstore.New(ctx, "sqlite3", "file:"+tmp+"/s.db?_foreign_keys=on", waLog.Noop)
	if err != nil {
		t.Fatal(err)
	}
	lids := container.LIDMap // NewDevice leaves device.LIDs nil until the device is saved
	tx, _ := newTestTx(t)
	pn := types.NewJID("4915700000001", types.DefaultUserServer)
	lid := types.NewJID("99887766", types.HiddenUserServer)

	put := func(key, id string, ts int64) {
		tx.ensureChat(key, chatUser)
		tx.insertMessage(msgRow{Chat: key, ID: id, TS: ts, Kind: msgText, Text: id})
		tx.setLast(key, ts, id, false, stateRead)
		tx.addUnread(key, 1)
	}
	put(chatKey(ctx, tx, lids, pn, types.EmptyJID), "m1", 30)
	if n := count(t, tx, "SELECT count(*) FROM chats WHERE jid = ?", pn.String()); n != 1 {
		t.Fatalf("before the mapping: %d rows under the phone number", n)
	}

	if err := lids.PutLIDMapping(ctx, lid, pn); err != nil {
		t.Fatal(err)
	}
	put(chatKey(ctx, tx, lids, lid, types.EmptyJID), "m2", 20)
	sweep(ctx, tx, lids)

	if n := count(t, tx, "SELECT count(*) FROM chats"); n != 1 {
		t.Fatalf("%d chat rows after the sweep, want 1", n)
	}
	var unread int
	var preview string
	tx.tx.QueryRow("SELECT unread, last_preview FROM chats WHERE jid = ?", lid.String()).Scan(&unread, &preview)
	if unread != 2 || preview != "m1" {
		t.Errorf("merged row: unread %d, preview %q", unread, preview)
	}
	if n := count(t, tx, "SELECT count(*) FROM messages WHERE chat = ?", lid.String()); n != 2 {
		t.Errorf("%d messages under the LID, want 2", n)
	}
	if k := chatKey(ctx, tx, lids, pn, types.EmptyJID); k != lid.String() {
		t.Errorf("phone number keyed %s, want the LID", k)
	}
}

func TestChatKeyOthers(t *testing.T) {
	tx, _ := newTestTx(t)
	for _, s := range []string{"1203630@g.us", "1203630@newsletter", "1203630@broadcast"} {
		j, _ := types.ParseJID(s)
		if k := chatKey(context.Background(), tx, nil, j, types.EmptyJID); k != s {
			t.Errorf("%s keyed %s", s, k)
		}
	}
}
