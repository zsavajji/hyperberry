package main

import (
	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/types"
)

// tidy fixes what earlier builds left in chats.db, at every connect: the empty chats with
// ourselves that our own phone's protocol messages made (a chat with ourselves that holds
// messages stays: it is "message yourself"), and the "+0" that named WhatsApp's account.
func (t *storeTx) tidy(cli *whatsmeow.Client) {
	for _, me := range []types.JID{cli.Store.GetJID(), cli.Store.GetLID()} {
		if !me.IsEmpty() {
			t.dropEmptyChat(me.ToNonAD().String())
		}
	}
	t.setName(types.PSAJID.String(), "WhatsApp")
}

// dropEmptyChat deletes the chat if it holds no message, and its Hub item with it, as
// clearChat's drop does.
func (t *storeTx) dropEmptyChat(jid string) {
	if t.exec("DELETE FROM chats WHERE jid = ? AND NOT EXISTS (SELECT 1 FROM messages WHERE chat = chats.jid)", jid) > 0 {
		t.chats[jid] = true
		t.note(jid, note{gone: true})
	}
}
