package main

import (
	"context"
	"sort"
	"strconv"

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
					text, _ = mentionNames(ctx, t, cli, contextInfo(ev.Message), text)
					t.editMessage(key, ev.Info.ID, text)
				}
				continue
			}
			st.apply(ctx, t, cli, key, ev, false)
			if ev.SourceWebMsg.GetStarred() {
				t.setStarred(key, ev.Info.ID, true)
			}
			historyReactions(ctx, t, cli, key, chat, alt, ev)
			historyVotes(ctx, t, cli, key, chat, alt, ev)
		}

		// After the messages: a time set first would keep setLast from writing the preview.
		t.stampChat(key, int64(conv.GetConversationTimestamp())) // a chat the phone lists, though it sent no message

		unread := int(conv.GetUnreadCount())
		if unread == 0 && conv.GetMarkedAsUnread() {
			unread = 1
		}
		t.setFlags(key, unread, muteUntil(int64(conv.GetMuteEndTime())), conv.GetPinned() > 0, conv.GetArchived())
	}
	sweep(ctx, t, lids)
	syncSettings(ctx, t, cli) // setFlags unpinned every chat (the conversations carry no pin), maybe unmuted it
}

// message handles a live message.
func (st *Store) message(ctx context.Context, t *storeTx, cli *whatsmeow.Client, e *events.Message) {
	chat := e.Info.Chat
	if chat == types.StatusBroadcastJID { // a story: its own table, no chat (stories.go)
		st.story(ctx, t, cli, e)
		return
	}
	// Device-to-device protocol messages (key shares, history notices) are never chat content;
	// one from our own phone would leave an empty chat with ourselves. The answers to msg.resend
	// carry no category (hypermeow builds their Info from the web message), so they pass.
	if e.Info.Category == "peer" {
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

// viewOnceStub is the line for a view-once message: WhatsApp gives linked devices no media for
// it, only an "unavailable" stanza. It goes the way of a live message, as the notice
// rowFromMessage makes of one; the empty photo is there for content() to find something to show.
func (st *Store) viewOnceStub(ctx context.Context, t *storeTx, cli *whatsmeow.Client, e *events.UndecryptableMessage) {
	st.message(ctx, t, cli, &events.Message{Info: e.Info, IsViewOnce: true,
		Message: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{}}})
}

// isViewOnce is whether r is the notice of a view-once message.
func isViewOnce(r msgRow) bool { return r.Kind == msgNotice && r.Text == "view_once" }

// apply writes one message event, live or from history: a new message, an edit, a revoke, a
// reaction or a poll vote (its row, and the chat preview).
func (st *Store) apply(ctx context.Context, t *storeTx, cli *whatsmeow.Client, key string, e *events.Message, live bool) {
	spoiler := e.Message.GetSpoilerMessage() != nil // before flatten opens the wrapper
	forwarded := e.Message.GetBotForwardedMessage() != nil
	// A business message is written as what it wraps (msgflat.go). On a copy: the event is
	// hypermeow's, and this runs on the writer after the handlers returned.
	c := *e
	c.Message = flatten(e.Message)
	e, m := &c, c.Message
	// A channel's edit isn't wrapped as others' are: it's the new content under the original id.
	if e.NewsletterMeta != nil && !e.NewsletterMeta.EditTS.IsZero() {
		if _, text, ok := content(m); ok {
			t.editMessage(key, e.Info.ID, text)
		}
		return
	}
	if pm := m.GetProtocolMessage(); pm != nil {
		switch pm.GetType() {
		case waE2E.ProtocolMessage_REVOKE:
			t.revokeMessage(key, pm.GetKey().GetID())
		case waE2E.ProtocolMessage_MESSAGE_EDIT:
			if _, text, ok := content(pm.GetEditedMessage()); ok {
				// ponytail: an edit's new mentions show the name but no link, as extra keeps the original's.
				text, _ = mentionNames(ctx, t, cli, contextInfo(pm.GetEditedMessage()), text)
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
		liveReaction(ctx, t, cli, key, e, r, name)
		return
	}
	if m.GetPollUpdateMessage() != nil { // a vote: no row, no unread, no preview (polls.go)
		if live { // history carries them decrypted, with the poll
			st.liveVote(ctx, t, cli, key, e, name)
		}
		return
	}
	row, ok := rowFromMessage(key, e, name)
	if !ok {
		return
	}
	if spoiler && (row.Kind == msgPhoto || row.Kind == msgVideo) {
		row.Extra = withKey(row.Extra, "spoiler", "true")
	}
	// A forward keeps WhatsApp's score, at least 1: 5 and up is "Forwarded many times".
	if ci := contextInfo(m); forwarded || ci.GetIsForwarded() {
		row.Extra = withKey(row.Extra, "forwarded", strconv.Itoa(max(1, int(ci.GetForwardingScore()))))
	}
	// A reply to a story (ours, or one we are answering): the quote is the story, drawn with its picture, and opens it.
	if ci := contextInfo(m); ci.GetRemoteJID() == types.StatusBroadcastJID.String() && ci.GetStanzaID() != "" {
		poster := ownStory
		if row.Out {
			poster = key
		}
		row.Extra = withStoryQuote(row.Extra, poster, quotedThumb(ci.GetQuotedMessage()))
	}
	if ci := contextInfo(m); len(ci.GetMentionedJID()) > 0 {
		var ms []mention
		if row.Text, ms = mentionNames(ctx, t, cli, ci, row.Text); len(ms) > 0 {
			row.Extra = withMentions(row.Extra, ms)
		}
	}
	if row.Kind == msgOther && row.Text == unknownText {
		st.log.Warnf("unknown message type: %s", fieldNames(m))
	}
	// The answer to msg.resend replaces the row of the same id. The user has seen that one, so
	// nothing is unread or alerts again; an answer to a request hypermeow made by itself finds
	// no row and is a new message. So does a real message under the id of a view-once stub
	// (viewOnceStub), answer or not.
	answer := e.UnavailableRequestID != ""
	if answer || live && !isViewOnce(row) {
		if old, had := t.messageByID(key, e.Info.ID); had && (answer || isViewOnce(old)) {
			t.deleteMessage(key, e.Info.ID)
			live = false
		}
	}
	if e.Info.IsGroup {
		row.Sender = chatKey(ctx, t, cli.Store.LIDs, e.Info.Sender, e.Info.SenderAlt)
	}
	fresh := t.insertMessage(row)
	t.setLast(key, row.TS, chatPreview(row), row.Out, row.State)
	// A pin is a line in the chat, not news: WhatsApp doesn't alert for it either.
	pin := row.Kind == msgNotice && (row.Text == "pin" || row.Text == "unpin")
	if live && fresh && !row.Out && !pin {
		if t.stateInt("unarchive_chats") == 1 { // before the unread: the chat gets its item, and the alert
			t.setArchived(key, false)
		}
		t.addUnread(key, 1)
		t.notifyIncoming(key)
	}
	if live && fresh && row.MediaState == 1 && !isChannel(key) {
		// Never in the writer: it is a network call. What the user's categories (prefs.go) leave out, and a channel's
		// media, wait for a tap; a video that waits still gets its larger thumbnail, one that downloads has no need.
		if st.nt.autoDownload(row.Kind) {
			go st.liveMedia(cli, row)
		} else if row.Kind == msgVideo {
			go st.liveVideoThumb(cli, row)
		}
	}
}
