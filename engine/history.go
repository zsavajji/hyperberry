package main

import (
	"context"
	"sort"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
)

// chatKind is the chats.kind of a chat JID.
func chatKind(j types.JID) int {
	switch j.Server {
	case types.GroupServer:
		return chatGroup
	case types.NewsletterServer:
		return chatNewsletter
	case types.BroadcastServer:
		return chatBroadcast
	}
	return chatUser
}

// history writes the chats of a history sync. Its messages go oldest first, so an edit or a
// revoke finds the message it changes.
func (st *Store) history(ctx context.Context, t *storeTx, cli *whatsmeow.Client, e *events.HistorySync) {
	lids := cli.Store.LIDs
	for _, conv := range e.Data.GetConversations() {
		chat, err := types.ParseJID(conv.GetID())
		if err != nil || chat == types.StatusBroadcastJID {
			continue
		}
		var alt types.JID
		for _, s := range []string{conv.GetLidJID(), conv.GetPnJID()} {
			if j, err := types.ParseJID(s); err == nil && !j.IsEmpty() && j.ToNonAD() != chat.ToNonAD() {
				alt = j
			}
		}
		key := chatKey(ctx, t, lids, chat, alt)
		t.ensureChat(key, chatKind(chat))
		if chat.Server == types.DefaultUserServer || chat.Server == types.HiddenUserServer {
			t.setName(key, displayName(ctx, cli, chat, alt))
		} else if n := conv.GetName(); n != "" {
			t.setName(key, n)
		} else {
			t.setName(key, conv.GetDisplayName())
		}

		var evs []*events.Message
		for _, hm := range conv.GetMessages() {
			if ev, err := cli.ParseWebMessage(chat, hm.GetMessage()); err == nil {
				evs = append(evs, ev)
			} else {
				st.log.Debugf("history message: %v", err)
			}
		}
		sort.SliceStable(evs, func(i, j int) bool { return evs[i].Info.Timestamp.Before(evs[j].Info.Timestamp) })
		for _, ev := range evs {
			if ev.SourceWebMsg.GetKey().GetID() != ev.Info.ID { // ParseWebMessage turned an edit into its target
				if _, text, ok := content(ev.Message); ok {
					t.editMessage(key, ev.Info.ID, text)
				}
				continue
			}
			st.apply(ctx, t, cli, key, ev, false)
		}

		unread := int(conv.GetUnreadCount())
		if unread == 0 && conv.GetMarkedAsUnread() {
			unread = 1
		}
		t.setFlags(key, unread, muteUntil(int64(conv.GetMuteEndTime())), conv.GetPinned() > 0, conv.GetArchived())
	}
	sweep(ctx, t, lids)
}

// message handles a live message.
func (st *Store) message(ctx context.Context, t *storeTx, cli *whatsmeow.Client, e *events.Message) {
	chat := e.Info.Chat
	if chat == types.StatusBroadcastJID {
		return
	}
	alt := e.Info.SenderAlt
	if e.Info.IsFromMe {
		alt = e.Info.RecipientAlt
	}
	key := chatKey(ctx, t, cli.Store.LIDs, chat, alt)
	t.ensureChat(key, chatKind(chat))
	if chatKind(chat) == chatUser {
		name := ""
		if !e.Info.IsFromMe {
			name = e.Info.PushName
		}
		t.setName(key, resolveName(ctx, cli, chat, alt, name))
	}
	st.apply(ctx, t, cli, key, e, true)
}

// apply writes one message event, live or from history: a new message, an edit, a revoke or
// a reaction (which only touches the chat preview).
func (st *Store) apply(ctx context.Context, t *storeTx, cli *whatsmeow.Client, key string, e *events.Message, live bool) {
	m := e.Message
	if pm := m.GetProtocolMessage(); pm != nil {
		switch pm.GetType() {
		case waE2E.ProtocolMessage_REVOKE:
			t.revokeMessage(key, pm.GetKey().GetID())
		case waE2E.ProtocolMessage_MESSAGE_EDIT:
			if _, text, ok := content(pm.GetEditedMessage()); ok {
				t.editMessage(key, pm.GetKey().GetID(), text)
			}
		}
		return
	}
	name := ""
	if !e.Info.IsFromMe {
		name = resolveName(ctx, cli, e.Info.Sender, e.Info.SenderAlt, e.Info.PushName)
	}
	if r := m.GetReactionMessage(); r != nil {
		if r.GetText() != "" {
			who := "You"
			if !e.Info.IsFromMe {
				who = firstWord(name)
			}
			state := stateRead
			if e.Info.IsFromMe {
				state = stateSent
			}
			t.setLast(key, e.Info.Timestamp.Unix(), who+" reacted "+r.GetText(), e.Info.IsFromMe, state)
		}
		return
	}
	row, ok := rowFromMessage(key, e, name)
	if !ok {
		return
	}
	if e.Info.IsGroup {
		row.Sender = chatKey(ctx, t, cli.Store.LIDs, e.Info.Sender, e.Info.SenderAlt)
	}
	fresh := t.insertMessage(row)
	t.setLast(key, row.TS, chatPreview(row), row.Out, row.State)
	if live && fresh && !row.Out {
		t.addUnread(key, 1)
		t.notifyIncoming(key)
	}
	if live && fresh && row.MediaState == 1 && (row.Kind == msgVoice || row.Kind == msgPhoto) {
		go st.liveMedia(cli, row) // never in the writer: it is a network call; the rest wait for a tap
	}
}
