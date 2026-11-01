package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
)

// Reactions (step 5.11): the reactions table, written from live messages, history and our own
// sends. sender is the reactor's chat key as messages.sender has it, "" for us.

// setReaction is the one writer of the reactions table. emoji "" takes a reaction back. They
// arrive out of order (history, a live one, the echo from our other devices), so the newest by
// ts wins: an older reaction doesn't replace a newer one, and a removal only takes a reaction no
// newer than itself. The message is named as changed only when a row did change.
// ponytail: no tombstones, so a removal that comes before the reaction it removes lets that one
// stay; keep removed rows (an empty emoji) if it shows.
func (t *storeTx) setReaction(chat, msg, sender, name, emoji string, ts int64) {
	var n int64
	if emoji == "" {
		n = t.exec("DELETE FROM reactions WHERE chat = ? AND msg = ? AND sender = ? AND ts <= ?", chat, msg, sender, ts)
	} else {
		n = t.exec(`INSERT INTO reactions(chat, msg, sender, sender_name, emoji, ts)
VALUES(?1, ?2, ?3, NULLIF(?4, ''), ?5, ?6)
ON CONFLICT(chat, msg, sender) DO UPDATE SET emoji = excluded.emoji,
  sender_name = COALESCE(excluded.sender_name, reactions.sender_name), ts = excluded.ts
WHERE excluded.ts >= reactions.ts AND (excluded.emoji != reactions.emoji OR excluded.ts > reactions.ts)`,
			chat, msg, sender, name, emoji, ts)
	}
	if n > 0 {
		t.changed(chat, msg)
	}
}

// reactPreview is the chat list line a live reaction leaves, "Anna reacted 👍". A removal
// leaves none.
func (t *storeTx) reactPreview(chat string, ts int64, who, emoji string, out bool) {
	state := stateRead
	if out {
		state = stateSent
	}
	t.setLast(chat, ts, who+" reacted "+emoji, out, state)
}

// reactedByUs writes a reaction of ours that was sent from this device: WhatsApp doesn't echo it
// back, so the row and the preview are written here, as a live one from another device would.
func (t *storeTx) reactedByUs(chat, msg, emoji string, ts int64) {
	t.setReaction(chat, msg, "", "", emoji, ts)
	if emoji != "" {
		t.reactPreview(chat, ts, "You", emoji, true)
	}
}

// liveReaction stores a reaction message (live, or one that history carries as a message) and
// leaves its preview on the chat. In a 1:1 chat the reactor is the chat, so the chat's own key
// stands for them: history addresses them by the chat's JID alone (ParseWebMessage), and
// chatKey(Sender, SenderAlt) would not always find the LID the chat is keyed by.
func liveReaction(ctx context.Context, t *storeTx, cli *whatsmeow.Client, key string, e *events.Message, r *waE2E.ReactionMessage, name string) {
	ts := r.GetSenderTimestampMS() / 1000
	if ts == 0 {
		ts = e.Info.Timestamp.Unix()
	}
	sender, who := "", "You"
	if !e.Info.IsFromMe {
		sender, who = key, firstWord(name)
		if e.Info.IsGroup {
			sender = chatKey(ctx, t, cli.Store.LIDs, e.Info.Sender, e.Info.SenderAlt)
		}
	}
	t.setReaction(key, r.GetKey().GetID(), sender, name, r.GetText(), ts)
	if r.GetText() != "" {
		t.reactPreview(key, e.Info.Timestamp.Unix(), who, r.GetText(), e.Info.IsFromMe)
	}
}

// historyReactions stores the reactions a history message carries. Their keys are the
// reactions' own: FromMe says the reaction is ours, Participant who reacted in a group. The chat
// preview is not touched: they are old news.
// ponytail: a reaction to a message chats.db doesn't hold stays as a row of its own; prune if
// chats.db grows.
func historyReactions(ctx context.Context, t *storeTx, cli *whatsmeow.Client, key string, chat, alt types.JID, ev *events.Message) {
	for _, r := range ev.SourceWebMsg.GetReactions() {
		ts := r.GetSenderTimestampMS() / 1000
		if ts == 0 {
			ts = ev.Info.Timestamp.Unix()
		}
		sender, name := "", ""
		if !r.GetKey().GetFromMe() {
			who, whoAlt := chat, alt
			sender = key
			if chat.Server == types.GroupServer {
				p, err := types.ParseJID(r.GetKey().GetParticipant())
				if err != nil || p.IsEmpty() {
					continue
				}
				who, whoAlt = p, types.EmptyJID
				sender = chatKey(ctx, t, cli.Store.LIDs, p, types.EmptyJID)
			}
			name = resolveName(ctx, cli, who, whoAlt, "")
		}
		t.setReaction(key, ev.Info.ID, sender, name, r.GetText(), ts)
	}
}

// moveReactions takes the phone-number chat's reactions to the LID chat (mergeChat). In a 1:1
// chat their reactions are keyed by the chat itself, so that key moves too. What can't move,
// because the LID chat has the row already, is dropped.
func moveReactions(tx *storeTx, from, to string) {
	tx.exec("UPDATE OR IGNORE reactions SET chat = ? WHERE chat = ?", to, from)
	tx.exec("DELETE FROM reactions WHERE chat = ?", from)
	tx.exec("UPDATE OR IGNORE reactions SET sender = ?1 WHERE chat = ?2 AND sender = ?3", to, to, from)
	tx.exec("DELETE FROM reactions WHERE chat = ? AND sender = ?", to, from)
}

type reactReq struct{ Chat, ID, Emoji string }

func parseReact(req json.RawMessage) (reactReq, error) {
	var r reactReq
	if err := json.Unmarshal(req, &r); err != nil || r.Chat == "" || r.ID == "" {
		return r, &Error{"bad_request", "msg.react needs chat and id"}
	}
	return r, nil
}

// reactable reads the message a reaction names, through the writer as deletable does.
func (st *Store) reactable(chat, id string) (msgRow, error) {
	var row msgRow
	var ok bool
	if err := st.runOp(func(t *storeTx) { row, ok = t.messageByID(chat, id) }); err != nil {
		return row, err
	}
	if !ok {
		return row, &Error{"not_found", "no such message"}
	}
	return row, nil
}

// reactSender is the sender BuildReaction wants for a reaction to row: empty for our own message
// (the key then says it is ours), the chat for theirs in a 1:1 chat, the sender in a group.
func reactSender(row msgRow, chat types.JID) (types.JID, error) {
	switch {
	case row.Out:
		return types.EmptyJID, nil
	case row.Sender != "":
		if j, err := types.ParseJID(row.Sender); err == nil {
			return j, nil
		}
	case chatKind(chat) == chatUser:
		return chat, nil
	}
	// An empty sender would make the key ours.
	return types.EmptyJID, &Error{"not_found", "the message's sender is unknown"}
}

// handleReact is msg.react: it sends the reaction (emoji "" takes ours back), then writes it
// here, since WhatsApp doesn't echo our own sends to this device.
func (s *Session) handleReact(req json.RawMessage) (any, error) {
	r, err := parseReact(req)
	if err != nil {
		return nil, err
	}
	cli, jid, err := s.linked(r.Chat)
	if err != nil {
		return nil, err
	}
	row, err := s.store.reactable(r.Chat, r.ID)
	if err != nil {
		return nil, err
	}
	sender, err := reactSender(row, jid)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	if _, err := cli.SendMessage(ctx, jid, cli.BuildReaction(jid, sender, r.ID, r.Emoji)); err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	s.store.ops <- func(t *storeTx) { t.reactedByUs(r.Chat, r.ID, r.Emoji, now) }
	return nil, nil
}
