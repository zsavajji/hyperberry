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
	// Step 6.5: a kind per type the UI words itself (docs/protocol.md). Appended, like msgAudio.
	msgLocation
	msgContact
	msgPoll
	msgEvent
	msgNotice // a service line the UI words: text is a code (noticeWords)
	msgInvite
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
	// Reactions (step 5.11), one row per reactor and message; a removal deletes the row. msg is the
	// reacted message's id. sender is the reactor's key as messages.sender has it (in a 1:1 chat
	// that is the chat's own key), '' for us; sender_name is NULL for us. A plain table: the UI's
	// SQLite is Qt 4.8's.
	`CREATE TABLE reactions(
  chat TEXT NOT NULL, msg TEXT NOT NULL,
  sender TEXT NOT NULL,
  sender_name TEXT,
  emoji TEXT NOT NULL,
  ts INTEGER NOT NULL,
  PRIMARY KEY(chat, msg, sender));`,
	// Every message type (step 6.5). extra is each kind's own details as JSON (docs/protocol.md),
	// NULL when there are none. poll_votes is one row per voter and poll, keyed as reactions are:
	// msg is the poll's id, sender '' is us, options is the chosen options' indices joined by ","
	// ("0,2"); a vote that chooses nothing deletes the row. A plain table: the UI's SQLite is Qt 4.8's.
	`ALTER TABLE messages ADD COLUMN extra TEXT;
CREATE TABLE poll_votes(
  chat TEXT NOT NULL, msg TEXT NOT NULL,
  sender TEXT NOT NULL,
  sender_name TEXT,
  options TEXT NOT NULL,
  ts INTEGER NOT NULL,
  PRIMARY KEY(chat, msg, sender));`,
	// Calls (step 7.2): the Calls tab lists the call rows (kind 13, id call-<call id>) of every chat.
	`CREATE INDEX messages_by_kind ON messages(kind, ts);`,
	// Stories, WhatsApp's status posts (stories.go): the Stories tab's rows, apart from messages so that
	// status@broadcast is no chat in the list. poster is the poster's chat key (identity.go), so a
	// reply opens their 1:1 chat; name is theirs at receive time. kind is a messages.kind (text, photo
	// or video), text the caption or a text story's text, extra a text story's colours and font as JSON
	// (docs/protocol.md). thumb is the inline JPEG, media and media_state are messages' own, path is
	// the downloaded file in data/stories. seen is ours, set when the viewer shows the story. A story
	// lives 24 h: ts is the post's time, unix seconds.
	`CREATE TABLE stories(
  poster TEXT NOT NULL, id TEXT NOT NULL,
  ts INTEGER NOT NULL,
  name TEXT,
  kind INTEGER NOT NULL, text TEXT,
  duration INTEGER, width INTEGER, height INTEGER, size INTEGER,
  thumb BLOB, media TEXT, media_state INTEGER NOT NULL DEFAULT 0,
  path TEXT,
  extra TEXT,
  seen INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(poster, id));
CREATE INDEX stories_by_time ON stories(ts);`,
	// Who viewed our stories (stories.go): one row per story of ours (stories.poster = 'me') and viewer, from the read
	// receipt their phone sends. id is the story's, viewer their chat key (as chats.jid), name theirs when the receipt
	// came (NULL when none is known), ts when they viewed it, unix seconds. The rows go with their story.
	`CREATE TABLE story_views(
  id TEXT NOT NULL, viewer TEXT NOT NULL,
  name TEXT,
  ts INTEGER NOT NULL,
  PRIMARY KEY(id, viewer));`,
	// The engine's own small facts (store.go's stateInt), one integer per key; the UI never reads it.
	// appstate_resync: a chat list that exists was linked before the full app-state sync emitted stars,
	// muted stories and "Keep chats archived" (session.go), so it fetches the app state once more; a fresh
	// link has none yet and gets them with its own first sync.
	`CREATE TABLE engine_state(key TEXT PRIMARY KEY, value INTEGER NOT NULL);
INSERT INTO engine_state SELECT 'appstate_resync', 1 WHERE EXISTS (SELECT 1 FROM chats);`,
	// The posters whose status updates the phone muted (UserStatusMute), mirrored from it: the Stories tab greys
	// them and leaves them out of its badge. poster is the poster's chat key, as stories.poster has it.
	`CREATE TABLE story_muted(poster TEXT PRIMARY KEY);`,
	// Starred messages (star.go), as the phone's app state has them. ponytail: the Starred page scans messages for
	// starred = 1, with no index since the UI's SQLite is Qt 4.8's; add one if that gets slow.
	`ALTER TABLE messages ADD COLUMN starred INTEGER NOT NULL DEFAULT 0;`,
	// Translation (translate.go). tr is the translation, '' when there is nothing to show (already in that language,
	// or no letters), NULL when not asked; tr_lang the language it was made for, tr_from the one the service
	// detected ('' unknown). An edit clears all three. chats.translate is the language a chat's incoming messages
	// are shown in, '' off.
	`ALTER TABLE messages ADD COLUMN tr TEXT;
ALTER TABLE messages ADD COLUMN tr_lang TEXT;
ALTER TABLE messages ADD COLUMN tr_from TEXT;
ALTER TABLE chats ADD COLUMN translate TEXT NOT NULL DEFAULT '';`,
	// Stickers (stickers.go). sticker is the base64 plaintext SHA-256 of a sticker row's file, the name WhatsApp's app
	// state gives a favourite by; sticker_faves mirrors the phone's favourites, media a mediaInfo as messages.media has
	// it. The refetch brings the favourites set before this version, as the settings' step did.
	`ALTER TABLE messages ADD COLUMN sticker TEXT;
UPDATE messages SET sticker = json_extract(media, '$.fileSha256') WHERE kind = 5 AND media IS NOT NULL;
CREATE INDEX messages_sticker ON messages(sticker);
CREATE TABLE sticker_faves(hash TEXT PRIMARY KEY, media TEXT NOT NULL, width INTEGER, height INTEGER, ts INTEGER NOT NULL);
INSERT OR REPLACE INTO engine_state SELECT 'appstate_resync', 1 WHERE EXISTS (SELECT 1 FROM chats);`,
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
