package main

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/appstate"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/proto/waSyncAction"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
)

const sendTimeout = 60 * time.Second

// linked returns the client of a linked device and the chat's JID, or the error to reply with.
func (s *Session) linked(chat string) (*whatsmeow.Client, types.JID, error) {
	cli := s.client()
	if cli.Store.ID == nil {
		return nil, types.EmptyJID, &Error{"unlinked", "link this device first"}
	}
	jid, err := types.ParseJID(chat)
	if err != nil || jid.IsEmpty() {
		return nil, types.EmptyJID, &Error{"bad_chat", "not a chat key: " + chat}
	}
	return cli, jid, nil
}

func registerSend(srv *Server, s *Session) {
	srv.Handle("msg.send", s.handleSend)
	srv.Handle("msg.delete", s.handleDelete)
	srv.Handle("msg.resend", s.handleResend)
	srv.Handle("msg.react", s.handleReact)
	srv.Handle("poll.vote", s.handlePollVote)
	srv.Handle("chat.set", s.handleChatSet)
	srv.Handle("contact.block", s.handleContactBlock)
	srv.Handle("chat.clear", s.handleChatClear)
	srv.Handle("chat.delete", s.handleChatDelete)
	srv.Handle("chat.export", s.store.handleExport)
}

func (s *Session) handleSend(req json.RawMessage) (any, error) {
	var r struct {
		Chat, Text, Quote string
		Mentions          []mention
	}
	if err := json.Unmarshal(req, &r); err != nil || strings.TrimSpace(r.Text) == "" {
		return nil, &Error{"bad_request", "msg.send needs chat and non-empty text"}
	}
	cli, jid, err := s.linked(r.Chat)
	if err != nil {
		return nil, err
	}
	id, now := cli.GenerateMessageID(), time.Now().Unix()

	// Our row keeps the text as typed ("@Anna Lee") and the mentions, so its bubble links them as a
	// received one does; the wire message has "@<number>".
	wire, jids := mentionWire(r.Text, r.Mentions)
	extra := ""
	if len(jids) > 0 {
		extra = withMentions("", r.Mentions)
	}
	q, created := s.store.queueOutgoing(jid, r.Chat, id, r.Text, r.Quote, extra, now)
	s.nameNew(cli, jid, r.Chat, created)
	msg := &waE2E.Message{Conversation: &r.Text}
	if q.ID != "" || len(jids) > 0 {
		ci := &waE2E.ContextInfo{}
		if q.ID != "" {
			ci = quoteInfo(cli, r.Chat, q)
		}
		ci.MentionedJID = jids
		msg = &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: &wire, ContextInfo: ci}}
	}

	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	resp, err := cli.SendMessage(ctx, jid, msg, whatsmeow.SendRequestExtra{ID: id})
	if err != nil {
		s.store.ops <- func(t *storeTx) { t.sentAs(r.Chat, id, 0, stateFailed) }
		return nil, err
	}
	// The server's clock, not the phone's: it drifts.
	s.store.ops <- func(t *storeTx) { t.sentAs(r.Chat, id, resp.Timestamp.Unix(), stateSent) }
	return map[string]string{"id": id}, nil
}

// nameNew: the first message to someone with no chat names the chat.
func (s *Session) nameNew(cli *whatsmeow.Client, jid types.JID, chat string, created bool) {
	if created && chatKind(jid) == chatUser {
		nctx, ncancel := context.WithTimeout(context.Background(), netTimeout)
		name := displayName(nctx, cli, jid, types.EmptyJID)
		ncancel()
		s.store.ops <- func(t *storeTx) { t.setName(chat, name) }
	}
}

// quoteInfo is the ContextInfo that quotes q.
func quoteInfo(cli *whatsmeow.Client, chat string, q msgRow) *waE2E.ContextInfo {
	who := chat // a 1:1 message from them was sent by the chat itself
	switch {
	case q.Out:
		who = cli.Store.ID.ToNonAD().String()
	case q.Sender != "":
		who = q.Sender
	}
	return &waE2E.ContextInfo{StanzaID: &q.ID, Participant: &who, QuotedMessage: &waE2E.Message{Conversation: &q.Text}}
}

type deleteReq struct {
	Chat     string
	IDs      []string `json:"ids"`
	Everyone bool
}

func parseDelete(req json.RawMessage) (deleteReq, error) {
	var r deleteReq
	if err := json.Unmarshal(req, &r); err != nil || len(r.IDs) == 0 {
		return r, &Error{"bad_request", "msg.delete needs chat and ids"}
	}
	return r, nil
}

// handleDelete is msg.delete. For everyone it sends a revoke per message and marks each row once
// it went; for me the rows go first (our local action) and our other devices are told after.
func (s *Session) handleDelete(req json.RawMessage) (any, error) {
	r, err := parseDelete(req)
	if err != nil {
		return nil, err
	}
	cli, jid, err := s.linked(r.Chat)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.deletable(r.Chat, r.IDs, r.Everyone)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	if r.Everyone {
		for _, m := range rows {
			if _, err := cli.SendMessage(ctx, jid, cli.BuildRevoke(jid, types.EmptyJID, m.ID)); err != nil {
				return nil, err
			}
			id := m.ID
			s.store.ops <- func(t *storeTx) { t.revokeMessage(r.Chat, id) }
		}
		return nil, nil
	}
	for _, m := range rows {
		id := m.ID
		s.store.ops <- func(t *storeTx) { t.deleteMessage(r.Chat, id) }
	}
	if len(rows) > 0 {
		if err := cli.SendAppState(ctx, deleteForMePatch(jid, rows)); err != nil {
			// ponytail: our other devices keep the messages; queue and retry if that matters.
			s.log.Warnf("delete for me: app state: %v", err)
		}
	}
	return nil, nil
}

// deletable reads the rows a delete names; ids that aren't there are skipped. For everyone every
// row must be ours and already sent: WhatsApp revokes nothing else.
func (st *Store) deletable(chat string, ids []string, everyone bool) ([]msgRow, error) {
	got := make(chan []msgRow, 1)
	st.ops <- func(t *storeTx) {
		var rows []msgRow
		for _, id := range ids {
			if m, ok := t.messageByID(chat, id); ok {
				rows = append(rows, m)
			}
		}
		got <- rows
	}
	rows := <-got
	if everyone {
		for _, m := range rows {
			if !m.Out || m.Deleted || m.State < stateSent || m.State > stateRead {
				return nil, &Error{"not_ours", "only our own sent messages can be deleted for everyone"}
			}
		}
	}
	return rows, nil
}

// deleteForMePatch tells our other devices to drop the messages. hypermeow names the index but
// has no builder, so this follows BuildStar's layout (the sender is "0" unless it is someone
// else's message in a group). EncodePatch sets the value's own timestamp.
// (verify on the real link): the index layout, and that MessageTimestamp is in seconds.
func deleteForMePatch(chat types.JID, rows []msgRow) appstate.PatchInfo {
	p := appstate.PatchInfo{Type: appstate.WAPatchRegularHigh}
	for _, m := range rows {
		fromMe, who, ts, media := "0", "0", m.TS, false
		if m.Out {
			fromMe = "1"
		} else if m.Sender != "" {
			who = m.Sender
		}
		p.Mutations = append(p.Mutations, appstate.MutationInfo{
			Index:   []string{appstate.IndexDeleteMessageForMe, chat.String(), m.ID, fromMe, who},
			Version: 3,
			Value: &waSyncAction.SyncActionValue{DeleteMessageForMeAction: &waSyncAction.DeleteMessageForMeAction{
				DeleteMedia: &media, MessageTimestamp: &ts}},
		})
	}
	return p
}

// queueOutgoing queues our text; extra is the row's (its mentions), "" for none.
func (st *Store) queueOutgoing(jid types.JID, chat, id, text, quote, extra string, now int64) (msgRow, bool) {
	return st.queueRow(jid, chat, quote, msgRow{Chat: chat, ID: id, TS: now, Out: true, State: stateSending, Kind: msgText, Text: text, Extra: extra})
}

// queueRow puts our message in as sending, so the UI shows the bubble while the send is in
// flight, and returns the message it quotes (empty when there is none) and whether the chat
// was new.
func (st *Store) queueRow(jid types.JID, chat, quote string, row msgRow) (msgRow, bool) {
	quoted := make(chan msgRow, 1)
	made := make(chan bool, 1)
	st.ops <- func(t *storeTx) {
		made <- !t.hasChat(chat)
		var q msgRow
		if quote != "" {
			q, _ = t.messageByID(chat, quote)
		}
		quoted <- q
		if q.ID != "" {
			row.QuoteID, row.QuotePreview = q.ID, preview(q.Kind, q.Text, q.Duration)
		}
		t.ensureChat(chat, chatKind(jid))
		t.insertMessage(row)
		t.setLast(chat, row.TS, chatPreview(row), true, stateSending)
	}
	return <-quoted, <-made
}

// receipt moves the ticks of our messages, or clears unread when our other devices read a chat.
func (st *Store) receipt(ctx context.Context, t *storeTx, cli *whatsmeow.Client, e *events.Receipt) {
	alt := e.SenderAlt
	if e.IsFromMe {
		alt = e.RecipientAlt
	}
	key := chatKey(ctx, t, cli.Store.LIDs, e.Chat, alt)
	if e.IsFromMe {
		if e.Type == types.ReceiptTypeRead || e.Type == types.ReceiptTypeReadSelf {
			t.setUnread(key, 0)
		}
		if e.Type == types.ReceiptTypePlayedSelf { // we played theirs on another device
			for _, id := range e.MessageIDs {
				t.setPlayed(key, id)
			}
		}
		return
	}
	state := 0
	switch e.Type {
	case types.ReceiptTypeDelivered:
		state = stateDelivered
	case types.ReceiptTypeRead, types.ReceiptTypePlayed:
		if e.Type == types.ReceiptTypePlayed {
			for _, id := range e.MessageIDs {
				t.setPlayed(key, id)
			}
		}
		// ponytail: in groups the first read receipt marks it read; WhatsApp waits for every
		// member. Per-member state if anyone asks.
		state = stateRead
	default:
		return
	}
	for _, id := range e.MessageIDs {
		t.setState(key, id, state)
	}
}
