package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	waLog "github.com/polymorfa/hypermeow/util/log"
)

const (
	batchOps  = 500
	batchTime = 50 * time.Millisecond
	// muteForever stands for WhatsApp's "always" (-1) as a unix time that never comes.
	muteForever = int64(1) << 40
)

// Store projects WhatsApp events into chats.db (PLAN.md §4) and tells the UI which rows changed.
type Store struct {
	db   *sql.DB
	srv  *Server
	log  waLog.Logger
	ops  chan func(*storeTx)
	done chan struct{}
	quit bool // only the writer touches it
	ui   uiState
	nt   *Notifier
}

func newStore(db *sql.DB, srv *Server, log waLog.Logger) *Store {
	st := &Store{db: db, srv: srv, log: log, ops: make(chan func(*storeTx), 1024), done: make(chan struct{})}
	st.nt = newNotifier(srv.dir, log)
	srv.Handle("prefs.set", st.nt.handlePrefs)
	go st.run()
	return st
}

// run is the one writer. It groups ops into transactions of up to 500 ops or 50 ms, so a
// history sync is a few commits and a lone message is one.
func (st *Store) run() {
	defer close(st.done)
	for op := range st.ops {
		st.batch(op)
		if st.quit {
			return
		}
	}
}

func (st *Store) batch(first func(*storeTx)) {
	tx, err := st.db.Begin()
	if err != nil {
		st.log.Warnf("begin: %v", err)
		return // ponytail: the ops of this batch are lost; chats.db is not locked for long, so it is rare
	}
	t := &storeTx{st: st, tx: tx, log: st.log, chats: map[string]bool{}, msgs: map[string]map[string]bool{}}
	t.run(first)
	timer := time.NewTimer(batchTime)
	defer timer.Stop()
fill:
	// A clear ends its batch, so its wipe runs before anything written after it (a re-link's seed).
	for n := 1; n < batchOps && !st.quit && !t.wipe; n++ {
		select {
		case op := <-st.ops:
			t.run(op)
		case <-timer.C:
			break fill
		}
	}
	if err := tx.Commit(); err != nil {
		st.log.Warnf("commit: %v", err)
		tx.Rollback()
		return
	}
	t.emit(st.srv)
	st.nt.send(t.notes)
	if t.wipe {
		st.wipe()
	}
}

// wipe takes what is left of the old link once clear has committed: the voice notes and the
// decoded cache, and the deleted rows still in chats.db's free pages and WAL. The UI's settings
// stay: they belong to the phone. Both folders come back on the next write.
func (st *Store) wipe() {
	for _, d := range []string{"voice", "cache"} {
		if err := os.RemoveAll(filepath.Join(st.srv.dir, d)); err != nil {
			st.log.Warnf("wipe %s: %v", d, err)
		}
	}
	for _, q := range []string{"VACUUM", "PRAGMA wal_checkpoint(TRUNCATE)"} {
		if _, err := st.db.Exec(q); err != nil {
			st.log.Warnf("wipe: %s: %v", q, err)
		}
	}
}

// handle queues an event for the writer. It only enqueues: hypermeow calls it on its receive loop.
func (st *Store) handle(cli *whatsmeow.Client, evt any) {
	switch evt.(type) {
	case *events.Connected:
		go st.syncGroups(cli)
		return
	case *events.HistorySync, *events.Message, *events.Contact, *events.PushName, *events.GroupInfo,
		*events.JoinedGroup, *events.Mute, *events.Pin, *events.Archive, *events.MarkChatAsRead,
		*events.Receipt, *events.ChatPresence, *events.Presence, *events.DeleteForMe:
		st.ops <- func(t *storeTx) { st.dispatch(t, cli, evt) }
	}
}

// syncGroups fetches the group subjects off the writer, since it is a network call, and
// queues the result.
func (st *Store) syncGroups(cli *whatsmeow.Client) {
	ctx := context.Background()
	groups, err := cli.GetJoinedGroups(ctx)
	if err != nil {
		st.log.Warnf("joined groups: %v", err)
	}
	st.ops <- func(t *storeTx) {
		for _, g := range groups {
			t.ensureChat(g.JID.String(), chatGroup)
			t.setName(g.JID.String(), g.Name)
		}
		sweep(ctx, t, cli.Store.LIDs)
	}
}

func (st *Store) dispatch(t *storeTx, cli *whatsmeow.Client, evt any) {
	ctx := context.Background()
	lids := cli.Store.LIDs
	switch e := evt.(type) {
	case *events.HistorySync:
		st.history(ctx, t, cli, e)
	case *events.Message:
		st.message(ctx, t, cli, e)
	case *events.Contact:
		alt, _ := types.ParseJID(e.Action.GetLidJID())
		if alt.IsEmpty() {
			alt, _ = types.ParseJID(e.Action.GetPnJID())
		}
		st.rename(ctx, t, cli, e.JID, alt)
	case *events.PushName:
		st.rename(ctx, t, cli, e.JID, e.JIDAlt)
	case *events.GroupInfo:
		if e.Name != nil {
			t.setName(e.JID.String(), e.Name.Name)
		}
	case *events.JoinedGroup:
		t.ensureChat(e.JID.String(), chatGroup)
		t.setName(e.JID.String(), e.Name)
	case *events.Receipt:
		st.receipt(ctx, t, cli, e)
	case *events.ChatPresence:
		st.typing(ctx, t, cli, e)
	case *events.Presence:
		st.presence(ctx, t, cli, e)
	case *events.Mute:
		until := int64(0)
		if e.Action.GetMuted() {
			until = muteUntil(e.Action.GetMuteEndTimestamp())
		}
		t.setMuted(chatKey(ctx, t, lids, e.JID, types.EmptyJID), until)
	case *events.Pin:
		t.setPinned(chatKey(ctx, t, lids, e.JID, types.EmptyJID), e.Action.GetPinned())
	case *events.Archive:
		t.setArchived(chatKey(ctx, t, lids, e.JID, types.EmptyJID), e.Action.GetArchived())
	case *events.MarkChatAsRead:
		n := 0
		if !e.Action.GetRead() {
			n = -1
		}
		t.setUnread(chatKey(ctx, t, lids, e.JID, types.EmptyJID), n)
	case *events.DeleteForMe:
		// A delete on one of our other devices.
		t.deleteMessage(chatKey(ctx, t, lids, e.ChatJID, types.EmptyJID), e.MessageID)
	}
}

// rename re-resolves the name of a person's chat, if there is one.
func (st *Store) rename(ctx context.Context, t *storeTx, cli *whatsmeow.Client, jid, alt types.JID) {
	key := chatKey(ctx, t, cli.Store.LIDs, jid, alt)
	if t.hasChat(key) {
		t.setName(key, displayName(ctx, cli, jid, alt))
	}
}

// muteUntil turns WhatsApp's mute end (seconds or milliseconds, -1 for always) into unix seconds.
func muteUntil(v int64) int64 {
	switch {
	case v < 0:
		return muteForever
	case v > 1e11:
		return v / 1000
	}
	return v
}

// clear drops every chat and message, then wipes the rest (wipe): a logout ends the link the
// history belonged to.
func (st *Store) clear() {
	st.ops <- func(t *storeTx) {
		t.wipe = true
		rows, err := t.tx.Query("SELECT jid FROM chats")
		if err == nil {
			for rows.Next() {
				var j string
				rows.Scan(&j)
				t.chats[j] = true
			}
			rows.Close()
		}
		t.exec("DELETE FROM messages")
		t.exec("DELETE FROM chats")
	}
}

// close flushes the queued writes and stops the writer.
func (st *Store) close() {
	st.ops <- func(*storeTx) { st.quit = true }
	<-st.done
	st.nt.close()
}

// storeTx is one open transaction and the rows it touched.
type storeTx struct {
	st    *Store // nil in tests of single writes
	notes map[string]note
	tx    *sql.Tx
	log   waLog.Logger
	chats map[string]bool
	msgs  map[string]map[string]bool // chat -> ids
	wipe  bool                       // clear ran: wipe after the commit
}

// run runs one op; a failing or panicking op doesn't stop the batch.
func (t *storeTx) run(op func(*storeTx)) {
	defer func() {
		if r := recover(); r != nil {
			t.log.Warnf("store op panicked: %v", r)
		}
	}()
	op(t)
}

// exec runs a statement and logs a failure. It reports how many rows changed.
func (t *storeTx) exec(q string, args ...any) int64 {
	res, err := t.tx.Exec(q, args...)
	if err != nil {
		t.log.Warnf("store: %v", err)
		return 0
	}
	n, _ := res.RowsAffected()
	return n
}

// touch runs an UPDATE of one chat and marks it changed when a row changed.
func (t *storeTx) touch(jid, q string, args ...any) bool {
	if t.exec(q, append(args, jid)...) > 0 {
		t.chats[jid] = true
		return true
	}
	return false
}

func (t *storeTx) hasChat(jid string) bool {
	var one int
	return t.tx.QueryRow("SELECT 1 FROM chats WHERE jid = ?", jid).Scan(&one) == nil
}

func (t *storeTx) ensureChat(jid string, kind int) {
	if t.exec("INSERT OR IGNORE INTO chats(jid, kind) VALUES(?, ?)", jid, kind) > 0 {
		t.chats[jid] = true
	}
}

func (t *storeTx) setName(jid, name string) {
	if name != "" {
		t.touch(jid, "UPDATE chats SET name = ?1 WHERE name IS NOT ?1 AND jid = ?2", name)
	}
}

// setLast never moves the chat's last message backwards.
func (t *storeTx) setLast(jid string, ts int64, preview string, out bool, state int) {
	t.touch(jid, `UPDATE chats SET last_ts = ?1, last_preview = ?2, last_out = ?3, last_state = ?4
WHERE (last_ts IS NULL OR last_ts <= ?1) AND jid = ?5`, ts, preview, out, state)
}

func (t *storeTx) setFlags(jid string, unread int, mutedUntil int64, pinned, archived bool) {
	t.touch(jid, "UPDATE chats SET unread = ?, muted_until = ?, pinned = ?, archived = ? WHERE jid = ?",
		unread, mutedUntil, pinned, archived)
}

func (t *storeTx) addUnread(jid string, n int) {
	t.touch(jid, "UPDATE chats SET unread = MAX(unread, 0) + ? WHERE jid = ?", n)
}

func (t *storeTx) setMuted(jid string, until int64) {
	t.touch(jid, "UPDATE chats SET muted_until = ?1 WHERE muted_until != ?1 AND jid = ?2", until)
}

func (t *storeTx) setPinned(jid string, v bool) {
	t.touch(jid, "UPDATE chats SET pinned = ?1 WHERE pinned != ?1 AND jid = ?2", v)
}

func (t *storeTx) setArchived(jid string, v bool) {
	t.touch(jid, "UPDATE chats SET archived = ?1 WHERE archived != ?1 AND jid = ?2", v)
}

// insertMessage is INSERT OR IGNORE since history and live can deliver the same message.
// It reports whether the message was new.
func (t *storeTx) insertMessage(m msgRow) bool {
	if t.exec(`INSERT OR IGNORE INTO messages(chat, id, ts, out, state, sender, sender_name, kind, text,
quote_id, quote_preview, edited, deleted, duration, waveform, media, media_state, played)
VALUES(?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), ?, ?,
NULLIF(?, 0), ?, NULLIF(?, ''), ?, ?)`,
		m.Chat, m.ID, m.TS, m.Out, m.State, m.Sender, m.SenderName, m.Kind, m.Text,
		m.QuoteID, m.QuotePreview, m.Edited, m.Deleted,
		m.Duration, blob(m.Waveform), m.Media, m.MediaState, m.Played) == 0 {
		return false
	}
	t.changed(m.Chat, m.ID)
	return true
}

func (t *storeTx) changed(chat, id string) {
	if t.msgs[chat] == nil {
		t.msgs[chat] = map[string]bool{}
	}
	t.msgs[chat][id] = true
}

func (t *storeTx) editMessage(chat, id, text string) {
	if t.exec("UPDATE messages SET text = ?, edited = 1 WHERE chat = ? AND id = ?", text, chat, id) > 0 {
		t.changed(chat, id)
		t.refreshLast(chat, id, false)
	}
}

func (t *storeTx) revokeMessage(chat, id string) {
	if t.exec("UPDATE messages SET deleted = 1, text = NULL WHERE chat = ? AND id = ?", chat, id) > 0 {
		t.changed(chat, id)
		t.refreshLast(chat, id, true)
	}
}

// refreshLast rewrites the chat preview when the changed message is the chat's newest.
func (t *storeTx) refreshLast(chat, id string, revoked bool) {
	var newest string
	err := t.tx.QueryRow("SELECT id FROM messages WHERE chat = ? ORDER BY ts DESC LIMIT 1", chat).Scan(&newest)
	if err != nil || newest != id {
		return
	}
	r := msgRow{Chat: chat, ID: id}
	var text, name sql.NullString
	if t.tx.QueryRow("SELECT ts, out, state, kind, text, sender_name, COALESCE(duration, 0) FROM messages WHERE chat = ? AND id = ?",
		chat, id).Scan(&r.TS, &r.Out, &r.State, &r.Kind, &text, &name, &r.Duration) != nil {
		return
	}
	r.Text, r.SenderName = text.String, name.String
	p := chatPreview(r)
	if revoked {
		p = "This message was deleted"
	}
	t.chats[chat] = true
	t.exec("UPDATE chats SET last_preview = ? WHERE jid = ?", p, chat)
}

// emit tells the UI what the committed batch changed.
func (t *storeTx) emit(srv *Server) {
	if len(t.chats) > 0 {
		srv.Emit("chats", map[string]any{"jids": keys(t.chats)})
	}
	chats := make([]string, 0, len(t.msgs))
	for c := range t.msgs {
		chats = append(chats, c)
	}
	sort.Strings(chats)
	for _, c := range chats {
		srv.Emit("msgs", map[string]any{"chat": c, "ids": keys(t.msgs[c])})
	}
}

func keys(m map[string]bool) []string {
	s := make([]string, 0, len(m))
	for k := range m {
		s = append(s, k)
	}
	sort.Strings(s)
	return s
}
