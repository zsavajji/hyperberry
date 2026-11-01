package main

import (
	"database/sql"
	"errors"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

// These numbers are stored in chats.db and read by the UI. They are part of the UI contract
// (docs/protocol.md, step 1.11) and are never renumbered.
const (
	chatUser = iota
	chatGroup
	chatBroadcast
	chatNewsletter
)

const (
	msgText = iota
	msgPhoto
	msgVideo
	msgVoice
	msgDoc
	msgSticker
	msgService
	msgOther
	msgAudio // audio that isn't a voice note (step 6.1); appended: the iota is never reordered
)

const (
	stateSending = iota
	stateSent
	stateDelivered
	stateRead
	stateFailed
)

// chats.db is the only copy of the history (PLAN.md §4), so a shipped step is never edited,
// only new ones appended. Element i takes PRAGMA user_version from i to i+1.
// Times (last_ts, ts) are unix seconds, WhatsApp's own resolution.
var migrations = []string{
	`CREATE TABLE chats(
  jid TEXT PRIMARY KEY,
  kind INTEGER NOT NULL,
  name TEXT,
  photo TEXT,
  last_ts INTEGER, last_preview TEXT, last_out INTEGER, last_state INTEGER,
  unread INTEGER NOT NULL DEFAULT 0,
  muted_until INTEGER NOT NULL DEFAULT 0,
  pinned INTEGER NOT NULL DEFAULT 0,
  archived INTEGER NOT NULL DEFAULT 0);
CREATE TABLE messages(
  chat TEXT NOT NULL, id TEXT NOT NULL,
  ts INTEGER NOT NULL,
  out INTEGER NOT NULL, state INTEGER NOT NULL,
  sender TEXT, sender_name TEXT,
  kind INTEGER NOT NULL, text TEXT,
  quote_id TEXT, quote_preview TEXT,
  edited INTEGER NOT NULL DEFAULT 0,
  deleted INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(chat, id));
CREATE INDEX messages_by_time ON messages(chat, ts);`,
	// Voice notes (step 4.3a). media_state: 0 none, 1 on the server, 2 here as voice/<id>.ogg,
	// 3 the download failed. media is the download details as JSON, real account only. played:
	// ours, they played it; theirs, we did.
	`ALTER TABLE messages ADD COLUMN duration INTEGER;
ALTER TABLE messages ADD COLUMN waveform BLOB;
ALTER TABLE messages ADD COLUMN media TEXT;
ALTER TABLE messages ADD COLUMN media_state INTEGER NOT NULL DEFAULT 0;
ALTER TABLE messages ADD COLUMN played INTEGER NOT NULL DEFAULT 0;`,
	// Photos, videos, audio files and documents (step 6.1). thumb is a JPEG: the inline one
	// (for a photo we send, the UI's). path is the downloaded file.
	`ALTER TABLE messages ADD COLUMN thumb BLOB;
ALTER TABLE messages ADD COLUMN width INTEGER;
ALTER TABLE messages ADD COLUMN height INTEGER;
ALTER TABLE messages ADD COLUMN size INTEGER;
ALTER TABLE messages ADD COLUMN path TEXT;`,
}

// openChats opens chats.db in WAL mode and migrates it to the newest schema.
func openChats(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", "file:"+path+"?_journal_mode=WAL&_busy_timeout=5000&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	// ponytail: one connection serialises the engine's own reads and writes; open a separate
	// read pool if engine-side reads ever wait on writes.
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	var v int
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return err
	}
	if v > len(migrations) {
		return errors.New("chats.db is from a newer HyperBerry")
	}
	for ; v < len(migrations); v++ {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[v]); err != nil {
			tx.Rollback()
			return fmt.Errorf("chats.db step %d: %w", v+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", v+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
