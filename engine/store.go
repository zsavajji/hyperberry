package main

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/appstate"
	waBinary "github.com/polymorfa/hypermeow/binary"
	"github.com/polymorfa/hypermeow/proto/waHistorySync"
	"github.com/polymorfa/hypermeow/store"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	waLog "github.com/polymorfa/hypermeow/util/log"
)

const (
	batchOps  = 500
	batchTime = 50 * time.Millisecond
	// backlogCap ends a backlog whose OfflineSyncCompleted never comes.
	// ponytail: a sync that never completes (a dropped connection) flushes after the cap; a
	// second preview inside it is ended by the first's timer, which only makes the Hub earlier.
	backlogCap = 2 * time.Minute
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
	// backlog is set from OfflineSyncPreview to OfflineSyncCompleted while the server hands over
	// what came during downtime; those chats reach the Hub only at the end, so chats read on the
	// main phone meanwhile don't alert (the user's call, 2026-11-01). Only the writer touches it.
	backlog map[string]bool
	ui      uiState
	nt      *Notifier
	// historyIn is called once a recent-chats history sync has committed: the session ends its
	// "syncing" then (step 5.5, after the Q10 test). Set before the client connects.
	historyIn func()
	// dl holds a *sync.Mutex per message being downloaded, so a tap and the live download of a
	// photo don't both write its file. ponytail: one small mutex per downloaded message, never pruned.
	dl sync.Map
	// hist takes the history-sync notifications and works them a chat at a time (history_stream.go).
	hist *historyQueue
}

func newStore(db *sql.DB, srv *Server, log waLog.Logger) *Store {
	st := &Store{db: db, srv: srv, log: log, ops: make(chan func(*storeTx), 1024), done: make(chan struct{})}
	st.nt = newNotifier(srv.dir, log)
	st.hist = newHistoryQueue(st)
	srv.Handle("prefs.set", st.handlePrefs)
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
	for _, f := range t.after {
		f()
	}
}

// afterCommit queues f to run once everything queued before it has committed.
func (st *Store) afterCommit(f func()) {
	st.ops <- func(t *storeTx) { t.after = append(t.after, f) }
}

// wipe takes what is left of the old link once clear has committed: the Hub's account, the
// voice notes, the photos, the decoded cache, our profile picture, and the deleted rows still in
// chats.db's free pages and WAL. The UI's settings stay: they belong to the phone. So do
// Downloads' files, as a browser's. The folders come back on the next write.
func (st *Store) wipe() {
	st.nt.unlink() // the Hub's account and items belong to the link too
	for _, d := range []string{"voice", "cache", "media", "profile", "history", "photos", "stories"} {
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
	if st.takeHistory(cli, evt) {
		return
	}
	switch e := evt.(type) {
	case *events.Connected:
		go st.syncGroups(cli)
		go st.syncChannels(cli)
		go st.hist.resume(cli) // the chunks a crash or a failure left on disk
		st.ops <- func(t *storeTx) {
			t.tidy(cli)
			syncSettings(context.Background(), t, cli)
			if t.stateInt("appstate_resync") == 1 {
				go st.resyncAppState(cli) // off the writer: it is network
			}
		}
		return
	case *events.NewsletterJoin: // followed on another device, or here
		go st.syncChannels(cli)
		return
	case *events.NewsletterLeave:
		st.ops <- func(t *storeTx) { t.clearChat(e.ID.String(), true) }
		return
	case *events.OfflineSyncPreview: // not through dispatch: cli may be nil
		st.ops <- func(*storeTx) {
			if st.backlog == nil {
				st.backlog = map[string]bool{}
			}
		}
		time.AfterFunc(backlogCap, func() { st.ops <- (*storeTx).endBacklog })
		return
	case *events.OfflineSyncCompleted:
		st.ops <- (*storeTx).endBacklog
		return
	case *events.UndecryptableMessage: // only this kind is a line; the rest stay in the log
		if e.IsUnavailable && e.UnavailableType == events.UnavailableTypeViewOnce {
			st.ops <- func(t *storeTx) { st.dispatch(t, cli, evt) }
		}
	case *events.HistorySync, *events.Message, *events.Contact, *events.LIDContact, *events.AppStateSyncComplete,
		*events.PushName, *events.GroupInfo, *events.JoinedGroup, *events.Mute, *events.Pin, *events.Archive, *events.UnarchiveChatsSetting, *events.UserStatusMute, *events.Star, *events.MarkChatAsRead,
		*events.Receipt, *events.ChatPresence, *events.Presence, *events.DeleteForMe, *events.ClearChat, *events.DeleteChat:
		st.ops <- func(t *storeTx) { st.dispatch(t, cli, evt) }
		if h, ok := e.(*events.HistorySync); ok && st.historyIn != nil {
			switch h.Data.GetSyncType() {
			case waHistorySync.HistorySync_INITIAL_BOOTSTRAP, waHistorySync.HistorySync_RECENT:
				st.afterCommit(st.historyIn)
			}
		}
	}
}

// resyncAppState fetches the three app-state collections that hold settings in full once more, so the
// stars, muted stories and "Keep chats archived" the phone set before this link existed arrive as
// events (session.go: EmitFullSyncIndexes). A fetch that fails leaves the flag for the next connect.
func (st *Store) resyncAppState(cli *whatsmeow.Client) {
	ctx := context.Background()
	for _, name := range []appstate.WAPatchName{appstate.WAPatchRegular, appstate.WAPatchRegularLow, appstate.WAPatchRegularHigh} {
		if err := cli.FetchAppState(ctx, name, true, false); err != nil {
			st.log.Warnf("app state resync %s: %v", name, err)
			return
		}
	}
	st.ops <- func(t *storeTx) { t.exec("DELETE FROM engine_state WHERE key = 'appstate_resync'") }
}

// syncGroups fetches the group subjects off the writer, since it is a network call, and
// queues the result. It names the groups we have and adds none: the server lists every group we
// are in, the community parents and the ones the phone doesn't show among them, and as WhatsApp
// Web we list a group once history or a message brings it (they sat at the bottom, "No messages").
func (st *Store) syncGroups(cli *whatsmeow.Client) {
	ctx := context.Background()
	names, err := joinedGroupNames(ctx, cli)
	if err != nil {
		st.log.Warnf("joined groups: %v", err)
	}
	st.ops <- func(t *storeTx) {
		for jid, name := range names {
			t.setName(jid, name)
		}
		sweep(ctx, t, cli.Store.LIDs)
		st.renameAll(ctx, t, cli) // the address book a link brings in comes with no event
	}
}

// joinedGroupNames is GetJoinedGroups without the members: each group's subject by its JID. A user whose
// groups held 5,500 members ran the engine out of memory on every connect, and the restart connected
// again (debug/user-logs/VX, 2026-11-18). The members come per group when a send or the info page needs them.
func joinedGroupNames(ctx context.Context, cli *whatsmeow.Client) (map[string]string, error) {
	resp, err := cli.DangerousInternals().SendIQ(ctx, whatsmeow.DangerousInfoQuery{
		Namespace: "w:g2",
		Type:      "get",
		To:        types.GroupServerJID,
		Content:   []waBinary.Node{{Tag: "participating"}},
	})
	if err != nil {
		return nil, err
	}
	groups, ok := resp.GetOptionalChildByTag("groups")
	if !ok {
		return nil, errors.New("no groups in the group list")
	}
	names := map[string]string{}
	for _, g := range groups.GetChildren() {
		ag := g.AttrGetter()
		if g.Tag == "group" {
			names[types.NewJID(ag.String("id"), types.GroupServer).String()] = ag.OptionalString("subject")
		}
	}
	return names, nil
}

func (st *Store) dispatch(t *storeTx, cli *whatsmeow.Client, evt any) {
	ctx := context.Background()
	lids := cli.Store.LIDs
	switch e := evt.(type) {
	case *events.HistorySync:
		st.history(ctx, t, cli, e)
	case *events.Message:
		st.message(ctx, t, cli, e)
	case *events.UndecryptableMessage:
		st.viewOnceStub(ctx, t, cli, e)
	case *events.Contact:
		alt, _ := types.ParseJID(e.Action.GetLidJID())
		if alt.IsEmpty() {
			alt, _ = types.ParseJID(e.Action.GetPnJID())
		}
		st.rename(ctx, t, cli, e.JID, alt)
	case *events.LIDContact: // a contact saved by LID: the action carries no phone number
		st.rename(ctx, t, cli, e.JID, types.EmptyJID)
	case *events.AppStateSyncComplete:
		// The contact list is in this patch; the link's bulk insert of it emits no Contact.
		if e.Name == appstate.WAPatchCriticalUnblockLow {
			st.renameAll(ctx, t, cli)
		}
		if e.Name == appstate.WAPatchRegularLow || e.Name == appstate.WAPatchRegularHigh { // pins, archives; mutes
			syncSettings(ctx, t, cli)
		}
	case *events.PushName:
		st.rename(ctx, t, cli, e.JID, e.JIDAlt)
	case *events.GroupInfo:
		if e.Name != nil {
			t.setName(e.JID.String(), e.Name.Name)
		}
	case *events.JoinedGroup:
		t.ensureChat(e.JID.String(), chatGroup)
		t.setName(e.JID.String(), e.Name)
		t.stampChat(e.JID.String(), time.Now().Unix()) // on top, as the phone's "You were added"; tidy keeps it
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
	case *events.UnarchiveChatsSetting:
		// "Keep chats archived" off on the phone is unarchiveChats true: a new message brings the chat back.
		v := 0
		if e.Action.GetUnarchiveChats() {
			v = 1
		}
		t.setStateInt("unarchive_chats", v)
	case *events.UserStatusMute:
		// Mirrored only: hypermeow has no builder for the mute, so the app can't set it, just show the phone's choice.
		key := chatKey(ctx, t, lids, e.JID, types.EmptyJID)
		q := "DELETE FROM story_muted WHERE poster = ?"
		if e.Action.GetMuted() {
			q = "INSERT OR IGNORE INTO story_muted(poster) VALUES(?)"
		}
		if t.exec(q, key) > 0 {
			t.stories = true
		}
	case *events.Star:
		t.setStarred(chatKey(ctx, t, lids, e.ChatJID, types.EmptyJID), e.MessageID, e.Action.GetStarred())
	case *events.MarkChatAsRead:
		n := 0
		if !e.Action.GetRead() {
			n = -1
		}
		t.setUnread(chatKey(ctx, t, lids, e.JID, types.EmptyJID), n)
	case *events.DeleteForMe:
		// A delete on one of our other devices.
		t.deleteMessage(chatKey(ctx, t, lids, e.ChatJID, types.EmptyJID), e.MessageID)
	case *events.ClearChat:
		// A full sync replays old clears, which would take the history that came after them.
		if !e.FromFullSync {
			t.clearChat(chatKey(ctx, t, lids, e.JID, types.EmptyJID), false)
		}
	case *events.DeleteChat:
		if !e.FromFullSync {
			t.clearChat(chatKey(ctx, t, lids, e.JID, types.EmptyJID), true)
		}
	}
}

// rename re-resolves the name of a person, on their chat if there is one and on their messages.
func (st *Store) rename(ctx context.Context, t *storeTx, cli *whatsmeow.Client, jid, alt types.JID) {
	key := chatKey(ctx, t, cli.Store.LIDs, jid, alt)
	if name := displayName(ctx, cli, jid, alt); name != "" {
		t.setName(key, name) // does nothing without that chat
		t.renameSender(key, name)
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

// settingsMute turns hypermeow's MutedUntil (zero: not muted) into chats.muted_until.
func settingsMute(v time.Time) int64 {
	switch {
	case v.IsZero():
		return 0
	case v.Equal(store.MutedForever):
		return muteForever
	}
	return v.Unix()
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
		t.exec("DELETE FROM reactions")
		t.exec("DELETE FROM poll_votes")
		t.exec("DELETE FROM stories")
		t.exec("DELETE FROM story_views")
		t.exec("DELETE FROM story_muted")
		t.exec("DELETE FROM engine_state WHERE key = 'unarchive_chats'") // the phone's settings go with its link
		t.stories = true
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
	after []func()                   // afterCommit's
	// A story was added, changed or dropped (stories.go): emit "stories" after the commit.
	stories bool
	// A view of one of our stories was recorded: emit "storyviews" after the commit.
	views bool
	stmts map[string]*sql.Stmt // exec's, by their SQL
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

// exec runs a statement and logs a failure. It reports how many rows changed. A statement is
// prepared once per transaction, which closes it when it ends: a history sync runs the same insert
// thousands of times in one, and preparing it each time was two thirds of the write (on the PC,
// 2026-11-13: an insert and its setLast went from 9.4 to 2.8 µs).
func (t *storeTx) exec(q string, args ...any) int64 {
	s := t.stmts[q]
	if s == nil {
		var err error
		if s, err = t.tx.Prepare(q); err != nil {
			t.log.Warnf("store: %v", err)
			return 0
		}
		if t.stmts == nil {
			t.stmts = map[string]*sql.Stmt{}
		}
		t.stmts[q] = s
	}
	res, err := s.Exec(args...)
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

// stateInt reads one of engine_state's facts, 0 when there is none.
func (t *storeTx) stateInt(key string) (v int) {
	t.tx.QueryRow("SELECT value FROM engine_state WHERE key = ?", key).Scan(&v)
	return v
}

func (t *storeTx) setStateInt(key string, v int) {
	t.exec("INSERT OR REPLACE INTO engine_state(key, value) VALUES(?, ?)", key, v)
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

// stampChat gives a chat with no time one, so it takes its place in the list with no preview, as a
// cleared chat does, and tidy's dropEmptyGroups keeps it.
func (t *storeTx) stampChat(jid string, ts int64) {
	if ts > 0 {
		t.touch(jid, "UPDATE chats SET last_ts = ? WHERE last_ts IS NULL AND jid = ?", ts)
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

// setMuted refreshes the chat's Hub item: muted shows read, unmuted shows its count again.
func (t *storeTx) setMuted(jid string, until int64) {
	if t.touch(jid, "UPDATE chats SET muted_until = ?1 WHERE muted_until != ?1 AND jid = ?2", until) {
		t.hubItem(jid, false, false)
	}
}

func (t *storeTx) setPinned(jid string, v bool) {
	t.touch(jid, "UPDATE chats SET pinned = ?1 WHERE pinned != ?1 AND jid = ?2", v)
}

// setArchived takes the chat's Hub item with it. Coming back, the chat gets its item now, silently,
// only if something is unread; otherwise with its next message.
func (t *storeTx) setArchived(jid string, v bool) {
	if t.touch(jid, "UPDATE chats SET archived = ?1 WHERE archived != ?1 AND jid = ?2", v) {
		var unread int
		t.tx.QueryRow("SELECT unread FROM chats WHERE jid = ?", jid).Scan(&unread)
		t.hubItem(jid, !v && unread != 0, false)
	}
}

// insertMessage is INSERT OR IGNORE since history and live can deliver the same message.
// It reports whether the message was new.
func (t *storeTx) insertMessage(m msgRow) bool {
	if t.exec(`INSERT OR IGNORE INTO messages(chat, id, ts, out, state, sender, sender_name, kind, text,
quote_id, quote_preview, edited, deleted, duration, waveform, media, media_state, played,
thumb, width, height, size, path, extra)
VALUES(?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), ?, ?,
NULLIF(?, 0), ?, NULLIF(?, ''), ?, ?, ?, NULLIF(?, 0), NULLIF(?, 0), NULLIF(?, 0), NULLIF(?, ''), NULLIF(?, ''))`,
		m.Chat, m.ID, m.TS, m.Out, m.State, m.Sender, m.SenderName, m.Kind, m.Text,
		m.QuoteID, m.QuotePreview, m.Edited, m.Deleted,
		m.Duration, blob(m.Waveform), m.Media, m.MediaState, m.Played,
		blob(m.Thumb), m.Width, m.Height, m.Size, m.Path, m.Extra) == 0 {
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
		t.exec("DELETE FROM reactions WHERE chat = ? AND msg = ?", chat, id)
		t.exec("DELETE FROM poll_votes WHERE chat = ? AND msg = ?", chat, id)
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
		p = word("This message was deleted")
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
	if t.stories {
		srv.Emit("stories", map[string]any{})
	}
	if t.views {
		srv.Emit("storyviews", map[string]any{})
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

// syncSettings copies hypermeow's chat settings' pins, mutes and archives into chats.db. The app-state full
// sync (a link, a version gap) stores them there without a Pin, Mute or Archive event, as
// EmitAppStateEventsOnFullSync is off, and a history sync's conversations carry no pin: a chat
// pinned or muted on the phone stayed unpinned and loud here.
// Only a chat hypermeow has settings for changes, so a pin, mute or archive from elsewhere stays.
func syncSettings(ctx context.Context, t *storeTx, cli *whatsmeow.Client) {
	rows, err := t.tx.Query("SELECT jid FROM chats")
	if err != nil {
		t.log.Warnf("pins: %v", err)
		return
	}
	var jids []string
	for rows.Next() {
		var j string
		if rows.Scan(&j) == nil {
			jids = append(jids, j)
		}
	}
	rows.Close() // closed before the writes below
	for _, j := range jids {
		if jid, err := types.ParseJID(j); err == nil {
			if s, err := cli.Store.ChatSettings.GetChatSettings(ctx, jid); err == nil && s.Found {
				t.setPinned(j, s.Pinned)
				t.setMuted(j, settingsMute(s.MutedUntil))
				t.setArchived(j, s.Archived)
			}
		}
	}
}
