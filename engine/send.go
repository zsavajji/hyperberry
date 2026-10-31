package main

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waE2E"
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

func registerSend(srv *Server, s *Session) { srv.Handle("msg.send", s.handleSend) }

func (s *Session) handleSend(req json.RawMessage) (any, error) {
	var r struct{ Chat, Text, Quote string }
	if err := json.Unmarshal(req, &r); err != nil || strings.TrimSpace(r.Text) == "" {
		return nil, &Error{"bad_request", "msg.send needs chat and non-empty text"}
	}
	cli, jid, err := s.linked(r.Chat)
	if err != nil {
		return nil, err
	}
	id, now := cli.GenerateMessageID(), time.Now().Unix()

	q := s.store.queueOutgoing(jid, r.Chat, id, r.Text, r.Quote, now)
	msg := &waE2E.Message{Conversation: &r.Text}
	if q.ID != "" {
		who := r.Chat // a 1:1 message from them was sent by the chat itself
		switch {
		case q.Out:
			who = cli.Store.ID.ToNonAD().String()
		case q.Sender != "":
			who = q.Sender
		}
		msg = &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: &r.Text,
			ContextInfo: &waE2E.ContextInfo{StanzaID: &q.ID, Participant: &who,
				QuotedMessage: &waE2E.Message{Conversation: &q.Text}}}}
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

// queueOutgoing puts our message in as sending, so the UI shows the bubble while the send is in
// flight, and returns the message it quotes (empty when there is none).
func (st *Store) queueOutgoing(jid types.JID, chat, id, text, quote string, now int64) msgRow {
	quoted := make(chan msgRow, 1)
	st.ops <- func(t *storeTx) {
		var q msgRow
		if quote != "" {
			q, _ = t.messageByID(chat, quote)
		}
		quoted <- q
		row := msgRow{Chat: chat, ID: id, TS: now, Out: true, State: stateSending, Kind: msgText, Text: text}
		if q.ID != "" {
			row.QuoteID, row.QuotePreview = q.ID, preview(q.Kind, q.Text)
		}
		t.ensureChat(chat, chatKind(jid))
		t.insertMessage(row)
		t.setLast(chat, now, chatPreview(row), true, stateSending)
	}
	return <-quoted
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
		return
	}
	state := 0
	switch e.Type {
	case types.ReceiptTypeDelivered:
		state = stateDelivered
	case types.ReceiptTypeRead, types.ReceiptTypePlayed:
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
