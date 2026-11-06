package main

import (
	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/types"
)

// tidy fixes what earlier builds left, at every connect: the empty chats with ourselves that our
// own phone's protocol messages made (a chat with ourselves that holds messages stays: it is
// "message yourself"), the "+0" that named WhatsApp's account, and archived chats' Hub items.
func (t *storeTx) tidy(cli *whatsmeow.Client) {
	for _, me := range []types.JID{cli.Store.GetJID(), cli.Store.GetLID()} {
		if !me.IsEmpty() {
			t.dropEmptyChat(me.ToNonAD().String())
		}
	}
	t.setName(types.PSAJID.String(), "WhatsApp")
	t.dropArchivedItems()
}

// dropArchivedItems takes away the Hub items of archived chats. The notifier ignores a chat with none.
func (t *storeTx) dropArchivedItems() {
	rows, err := t.tx.Query("SELECT jid FROM chats WHERE archived = 1")
	if err != nil {
		t.log.Warnf("archived items: %v", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var j string
		if rows.Scan(&j) == nil {
			t.note(j, note{gone: true})
		}
	}
}

// dropEmptyChat deletes the chat if it holds no message, and its Hub item with it, as
// clearChat's drop does.
func (t *storeTx) dropEmptyChat(jid string) {
	if t.exec("DELETE FROM chats WHERE jid = ? AND NOT EXISTS (SELECT 1 FROM messages WHERE chat = chats.jid)", jid) > 0 {
		t.chats[jid] = true
		t.note(jid, note{gone: true})
	}
}
