package main

import (
	"database/sql"
	"os"
)

// unreadMsg is an incoming message to mark read: its id and, in groups, its sender key.
type unreadMsg struct{ ID, Sender string }

// messageByID reads one message row.
func (t *storeTx) messageByID(chat, id string) (msgRow, bool) {
	r := msgRow{Chat: chat, ID: id}
	var sender, name, text, qid, qprev sql.NullString
	var secs sql.NullInt64
	var media, extra sql.NullString
	err := t.tx.QueryRow(`SELECT ts, out, state, sender, sender_name, kind, text, quote_id, quote_preview,
edited, deleted, duration, waveform, media, media_state, played, thumb, COALESCE(width, 0), COALESCE(height, 0),
COALESCE(size, 0), COALESCE(path, ''), extra, COALESCE(sticker, '') FROM messages WHERE chat = ? AND id = ?`, chat, id).
		Scan(&r.TS, &r.Out, &r.State, &sender, &name, &r.Kind, &text, &qid, &qprev, &r.Edited, &r.Deleted,
			&secs, &r.Waveform, &media, &r.MediaState, &r.Played, &r.Thumb, &r.Width, &r.Height, &r.Size, &r.Path, &extra, &r.Sticker)
	r.Sender, r.SenderName, r.Text, r.QuoteID, r.QuotePreview =
		sender.String, name.String, text.String, qid.String, qprev.String
	r.Duration, r.Media, r.Extra = int(secs.Int64), media.String, extra.String
	return r, err == nil
}

// deleteMessage is "delete for me": the row goes, and with it the files of a send of ours that are ours alone
// (dropOwnFiles). When it was the chat's newest, the preview falls back to the next one; last_ts stays, so the chat keeps its
// place in the list.
func (t *storeTx) deleteMessage(chat, id string) {
	m, had := t.messageByID(chat, id)
	var newest string
	wasLast := t.tx.QueryRow("SELECT id FROM messages WHERE chat = ? ORDER BY ts DESC LIMIT 1", chat).Scan(&newest) == nil && newest == id
	if t.exec("DELETE FROM messages WHERE chat = ? AND id = ?", chat, id) == 0 {
		return
	}
	t.changed(chat, id)
	t.exec("DELETE FROM reactions WHERE chat = ? AND msg = ?", chat, id)
	t.exec("DELETE FROM poll_votes WHERE chat = ? AND msg = ?", chat, id)
	if had {
		t.dropOwnFiles(m)
	}
	if !wasLast {
		return
	}
	t.chats[chat] = true
	t.lastFromNewest(chat)
}

// lastFromNewest sets the chat's last-message columns from its newest message, NULL for none.
func (t *storeTx) lastFromNewest(chat string) {
	var newest string
	if t.tx.QueryRow("SELECT id FROM messages WHERE chat = ? ORDER BY ts DESC LIMIT 1", chat).Scan(&newest) != nil {
		t.exec("UPDATE chats SET last_preview = NULL WHERE jid = ?", chat)
		return
	}
	r, _ := t.messageByID(chat, newest)
	p := chatPreview(r)
	if r.Deleted {
		p = word("This message was deleted")
	}
	t.exec("UPDATE chats SET last_preview = ?, last_out = ?, last_state = ? WHERE jid = ?", p, r.Out, r.State, chat)
}

// rewritePreviews words every chat's preview again, after the language changed.
func (t *storeTx) rewritePreviews() {
	rows, err := t.tx.Query("SELECT jid FROM chats")
	if err != nil {
		t.log.Warnf("rewrite previews: %v", err)
		return
	}
	var jids []string
	for rows.Next() {
		var j string
		if rows.Scan(&j) == nil {
			jids = append(jids, j)
		}
	}
	rows.Close()
	for _, j := range jids {
		t.chats[j] = true
		t.lastFromNewest(j)
	}
}

// clearChat is "clear chat", or with drop "delete chat": every message row goes, and the voice
// notes', photos' and stickers' files once it has committed (not Downloads: those are the user's, as a browser's). A cleared chat keeps its row and last_ts, so its place in
// the list, with no preview and nothing unread; a deleted one goes too.
func (t *storeTx) clearChat(chat string, drop bool) {
	rows, err := t.tx.Query("SELECT id, kind, out, COALESCE(path, '') FROM messages WHERE chat = ?", chat)
	if err != nil {
		t.log.Warnf("clear chat: %v", err)
		return
	}
	var voice, photos, stickers []string
	var ours []msgRow
	for rows.Next() {
		var id, path string
		var kind int
		var out bool
		if rows.Scan(&id, &kind, &out, &path) == nil {
			t.changed(chat, id)
			ours = append(ours, msgRow{ID: id, Kind: kind, Out: out, Path: path})
			switch kind {
			case msgVoice:
				voice = append(voice, id)
			case msgPhoto:
				photos = append(photos, id)
			case msgSticker:
				stickers = append(stickers, id)
			}
		}
	}
	rows.Close()
	t.exec("DELETE FROM messages WHERE chat = ?", chat)
	t.exec("DELETE FROM reactions WHERE chat = ?", chat)
	t.exec("DELETE FROM poll_votes WHERE chat = ?", chat)
	for _, m := range ours {
		t.dropOwnFiles(m) // the send files of our rows (the rows are gone: another chat's forward of one keeps its file)
	}
	if drop {
		if t.exec("DELETE FROM chats WHERE jid = ?", chat) > 0 {
			t.chats[chat] = true
		}
		t.note(chat, note{gone: true}) // its Hub item goes with it
	} else if t.touch(chat, "UPDATE chats SET last_preview = NULL, unread = 0 WHERE jid = ?") {
		t.hubItem(chat, false, false)
	}
	if t.st != nil && len(voice)+len(photos)+len(stickers) > 0 { // st is nil in tests of single writes
		dir := t.st.srv.dir
		t.after = append(t.after, func() {
			for _, id := range voice {
				os.Remove(oggPath(dir, id))
				os.Remove(cachedWAV(dir, id))
			}
			for _, id := range photos {
				os.Remove(photoPath(dir, id))
			}
			for _, id := range stickers {
				os.Remove(stickerPath(dir, id))
			}
		})
	}
}

// setState only raises the state of one of our messages: receipts arrive out of order. A failed (4) message stays failed,
// unless a receipt says it arrived (delivered, read): a copy had gone after all, say under a connection that dropped, and
// the ticks must say so, and nothing may send it again (isSending).
func (t *storeTx) setState(chat, id string, state int) {
	if t.exec(`UPDATE messages SET state = ?1 WHERE chat = ?2 AND id = ?3 AND out = 1 AND state != ?1
AND (state < ?1 OR (state = ?4 AND ?1 > ?5))`, state, chat, id, stateFailed, stateSent) > 0 {
		t.changed(chat, id)
		t.syncLast(chat, id)
	}
}

// sentAs records the outcome of a send: ts is the server's time (0 keeps ours). A message
// still sending takes the new state; one that a receipt already moved on only moves forward,
// and a late failure doesn't undo a delivery.
func (t *storeTx) sentAs(chat, id string, ts int64, state int) {
	if t.exec(`UPDATE messages SET ts = COALESCE(NULLIF(?1, 0), ts),
state = CASE WHEN state = 0 THEN ?2 WHEN ?2 = 4 THEN state ELSE MAX(state, ?2) END
WHERE chat = ?3 AND id = ?4`, ts, state, chat, id) > 0 {
		t.changed(chat, id)
		t.syncLast(chat, id)
	}
}

// syncLast copies the message's time and state to the chat row if it is the chat's newest.
func (t *storeTx) syncLast(chat, id string) {
	var newest string
	if t.tx.QueryRow("SELECT id FROM messages WHERE chat = ? ORDER BY ts DESC LIMIT 1", chat).Scan(&newest) != nil || newest != id {
		return
	}
	if t.exec(`UPDATE chats SET last_ts = m.ts, last_state = m.state
FROM (SELECT ts, state FROM messages WHERE chat = ?1 AND id = ?2) AS m WHERE jid = ?1`, chat, id) > 0 {
		t.chats[chat] = true
	}
}

// blob is a column value: NULL for an empty slice, since the driver would bind a zero-length blob.
func blob(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

// setMedia moves a note's media_state; a waveform computed after the download is kept.
func (t *storeTx) setMedia(chat, id string, state int, wf []byte) {
	if t.exec("UPDATE messages SET media_state = ?, waveform = COALESCE(?, waveform) WHERE chat = ? AND id = ?",
		state, blob(wf), chat, id) > 0 {
		t.changed(chat, id)
	}
}

// setFile records a downloaded file: the state and its path. The row keeps its inline thumbnail.
func (t *storeTx) setFile(chat, id string, state int, path string) {
	if t.exec(`UPDATE messages SET media_state = ?, path = COALESCE(NULLIF(?, ''), path)
WHERE chat = ? AND id = ?`, state, path, chat, id) > 0 {
		t.changed(chat, id)
	}
}

// setThumb replaces a message's thumbnail: a video's premade one, once it is here.
func (t *storeTx) setThumb(chat, id string, thumb []byte) {
	if t.exec("UPDATE messages SET thumb = ? WHERE chat = ? AND id = ?", blob(thumb), chat, id) > 0 {
		t.changed(chat, id)
	}
}

// setPlayed marks a voice note played, by them (ours) or by us (theirs).
func (t *storeTx) setPlayed(chat, id string) {
	if t.exec("UPDATE messages SET played = 1 WHERE chat = ? AND id = ? AND kind = ? AND played = 0",
		chat, id, msgVoice) > 0 {
		t.changed(chat, id)
	}
}

// setUnread sets the chat's counter (0 read, -1 marked unread); the Hub's item follows.
func (t *storeTx) setUnread(chat string, n int) {
	if t.touch(chat, "UPDATE chats SET unread = ?1 WHERE unread != ?1 AND jid = ?2", n) {
		t.hubItem(chat, false, false)
	}
}

// unreadIncoming lists the newest incoming messages the chat counts as unread (at least one,
// for "mark as unread"), newest first.
// ponytail: it trusts the counter; messages read on the phone but already counted are re-marked, which is harmless.
func (t *storeTx) unreadIncoming(chat string) []unreadMsg {
	rows, err := t.tx.Query(`SELECT id, COALESCE(sender, '') FROM messages WHERE chat = ?1 AND out = 0 AND id NOT LIKE 'call-%'
ORDER BY ts DESC LIMIT MAX(1, COALESCE((SELECT unread FROM chats WHERE jid = ?1), 0))`, chat)
	if err != nil {
		t.log.Warnf("unread: %v", err)
		return nil
	}
	defer rows.Close()
	var out []unreadMsg
	for rows.Next() {
		var m unreadMsg
		if rows.Scan(&m.ID, &m.Sender) == nil {
			out = append(out, m)
		}
	}
	return out
}
