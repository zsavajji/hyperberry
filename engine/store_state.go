package main

import "database/sql"

// unreadMsg is an incoming message to mark read: its id and, in groups, its sender key.
type unreadMsg struct{ ID, Sender string }

// messageByID reads one message row.
func (t *storeTx) messageByID(chat, id string) (msgRow, bool) {
	r := msgRow{Chat: chat, ID: id}
	var sender, name, text, qid, qprev sql.NullString
	err := t.tx.QueryRow(`SELECT ts, out, state, sender, sender_name, kind, text, quote_id, quote_preview,
edited, deleted FROM messages WHERE chat = ? AND id = ?`, chat, id).
		Scan(&r.TS, &r.Out, &r.State, &sender, &name, &r.Kind, &text, &qid, &qprev, &r.Edited, &r.Deleted)
	r.Sender, r.SenderName, r.Text, r.QuoteID, r.QuotePreview =
		sender.String, name.String, text.String, qid.String, qprev.String
	return r, err == nil
}

// setState only raises the state of one of our messages: receipts arrive out of order, and a
// failed (4) message stays failed.
func (t *storeTx) setState(chat, id string, state int) {
	if t.exec("UPDATE messages SET state = ? WHERE chat = ? AND id = ? AND out = 1 AND state < ? AND state != ?",
		state, chat, id, state, stateFailed) > 0 {
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

func (t *storeTx) setUnread(chat string, n int) {
	t.touch(chat, "UPDATE chats SET unread = ?1 WHERE unread != ?1 AND jid = ?2", n)
}

// unreadIncoming lists the newest incoming messages the chat counts as unread (at least one,
// for "mark as unread"), newest first.
// ponytail: it trusts the counter; messages read on the phone but already counted are re-marked, which is harmless.
func (t *storeTx) unreadIncoming(chat string) []unreadMsg {
	rows, err := t.tx.Query(`SELECT id, COALESCE(sender, '') FROM messages WHERE chat = ?1 AND out = 0
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
