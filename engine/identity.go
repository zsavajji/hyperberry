package main

import (
	"context"

	"github.com/polymorfa/hypermeow/store"
	"github.com/polymorfa/hypermeow/types"
)

// chatKey returns the chats.db key for a chat: groups, broadcasts and newsletters as they are;
// a person by their LID when hypermeow knows it (it learns mappings from history, messages and
// groups by itself, and announces none), so phone-number and LID addressing share one row.
func chatKey(ctx context.Context, tx *storeTx, lids store.LIDStore, chat, alt types.JID) string {
	chat = chat.ToNonAD()
	if chat.Server != types.DefaultUserServer {
		return chat.String()
	}
	lid := alt.ToNonAD()
	if lid.Server != types.HiddenUserServer {
		lid, _ = lids.GetLIDForPN(ctx, chat)
	}
	if lid.Server != types.HiddenUserServer {
		return chat.String()
	}
	mergeChat(tx, chat.String(), lid.String())
	return lid.String()
}

// mergeChat folds the phone-number row into the LID row: their messages, unread count, name,
// flags and last message. It does nothing when there is no phone-number row.
func mergeChat(tx *storeTx, from, to string) {
	if !tx.hasChat(from) {
		return
	}
	tx.ensureChat(to, chatUser)
	tx.exec("UPDATE OR IGNORE messages SET chat = ? WHERE chat = ?", to, from)
	tx.exec("DELETE FROM messages WHERE chat = ?", from) // what is left are duplicates
	tx.exec(`UPDATE chats SET (last_ts, last_preview, last_out, last_state) =
(SELECT last_ts, last_preview, last_out, last_state FROM chats WHERE jid = ?1)
WHERE jid = ?2 AND (SELECT last_ts FROM chats WHERE jid = ?1) > COALESCE(last_ts, -1)`, from, to)
	tx.exec(`UPDATE chats SET
unread = chats.unread + f.unread,
name = COALESCE(NULLIF(chats.name, ''), f.name),
photo = COALESCE(chats.photo, f.photo),
muted_until = MAX(chats.muted_until, f.muted_until),
pinned = chats.pinned | f.pinned,
archived = chats.archived | f.archived
FROM (SELECT * FROM chats WHERE jid = ?1) AS f WHERE chats.jid = ?2`, from, to)
	tx.exec("DELETE FROM chats WHERE jid = ?", from)
	tx.chats[from], tx.chats[to] = true, true
}

// sweep merges every phone-number chat whose LID has become known since it was written.
func sweep(ctx context.Context, tx *storeTx, lids store.LIDStore) {
	rows, err := tx.tx.Query("SELECT jid FROM chats WHERE jid LIKE '%@' || ?", types.DefaultUserServer)
	if err != nil {
		tx.log.Warnf("sweep: %v", err)
		return
	}
	var pns []types.JID
	for rows.Next() {
		var s string
		if rows.Scan(&s) == nil {
			if j, err := types.ParseJID(s); err == nil {
				pns = append(pns, j)
			}
		}
	}
	rows.Close()
	found, err := lids.GetManyLIDsForPNs(ctx, pns)
	if err != nil {
		tx.log.Warnf("sweep: %v", err)
		return
	}
	for pn, lid := range found {
		mergeChat(tx, pn.String(), lid.ToNonAD().String())
	}
}
