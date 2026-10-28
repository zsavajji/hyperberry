package main

import (
	"path/filepath"
	"testing"
)

func TestOpenChats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chats.db")
	db, err := openChats(path)
	if err != nil {
		t.Fatal(err)
	}
	var v int
	var mode string
	db.QueryRow("PRAGMA user_version").Scan(&v)
	db.QueryRow("PRAGMA journal_mode").Scan(&mode)
	if v != len(migrations) || mode != "wal" {
		t.Fatalf("version %d, mode %q", v, mode)
	}
	if _, err := db.Exec(`INSERT INTO chats(jid, kind) VALUES('a@lid', ?)`, chatUser); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO messages(chat, id, ts, out, state, kind) VALUES('a@lid', 'm1', 1, 0, ?, ?)`, stateSent, msgText); err != nil {
		t.Fatal(err)
	}
	db.Close()

	db, err = openChats(path)
	if err != nil {
		t.Fatal(err)
	}
	var chats, msgs int
	db.QueryRow("PRAGMA user_version").Scan(&v)
	db.QueryRow("SELECT count(*) FROM chats").Scan(&chats)
	db.QueryRow("SELECT count(*) FROM messages").Scan(&msgs)
	if v != len(migrations) || chats != 1 || msgs != 1 {
		t.Fatalf("after reopen: version %d, %d chats, %d messages", v, chats, msgs)
	}
	if _, err := db.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if db, err = openChats(path); err == nil {
		db.Close()
		t.Fatal("a newer user_version must fail")
	}
}
