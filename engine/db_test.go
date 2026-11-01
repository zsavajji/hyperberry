package main

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// A chats.db at migration 1 keeps its rows when it moves on to 2.
func TestMigrateKeepsRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chats.db")
	old, err := sql.Open("sqlite3", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{migrations[0], "PRAGMA user_version = 1",
		`INSERT INTO chats(jid, kind) VALUES('a@lid', 0)`,
		`INSERT INTO messages(chat, id, ts, out, state, kind) VALUES('a@lid', 'm1', 1, 0, 3, 3)`} {
		if _, err := old.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	old.Close()
	db, err := openChats(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var v, state, played int
	var duration, width, size sql.NullInt64
	var thumb []byte
	var path2 sql.NullString
	err = db.QueryRow("SELECT (SELECT user_version FROM pragma_user_version), duration, media_state, played, thumb, width, size, path FROM messages WHERE id = 'm1'").
		Scan(&v, &duration, &state, &played, &thumb, &width, &size, &path2)
	if err != nil || v != 3 || duration.Valid || state != 0 || played != 0 || thumb != nil || width.Valid || size.Valid || path2.Valid {
		t.Fatalf("version %d, duration %v, media_state %d, played %d, step 3's columns %v %v %v %v: %v", v, duration, state, played, thumb, width, size, path2, err)
	}
}

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
