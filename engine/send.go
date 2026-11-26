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
	"google.golang.org/protobuf/proto"
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
	srv.Handle("msg.cancel", s.handleCancel)
	srv.Handle("msg.retry", s.handleRetry)
	srv.Handle("msg.edit", s.handleEdit)
	srv.Handle("msg.forward", s.handleForward)
	srv.Handle("msg.resend", s.handleResend)
	srv.Handle("msg.react", s.handleReact)
	srv.Handle("msg.star", s.handleStar)
	srv.Handle("poll.vote", s.handlePollVote)
	srv.Handle("poll.create", s.handlePollCreate)
	srv.Handle("chat.set", s.handleChatSet)
	srv.Handle("contact.block", s.handleContactBlock)
	srv.Handle("chat.clear", s.handleChatClear)
	srv.Handle("chat.delete", s.handleChatDelete)
	srv.Handle("chat.export", s.store.handleExport)
	srv.HandleAsync("msg.translate", s.store.handleTranslate) // the services are slow: its own goroutine
}

func (s *Session) handleSend(req json.RawMessage) (any, error) {
	var r struct {
		Chat, Text, Quote, ID string
		Mentions              []mention
	}
	if err := json.Unmarshal(req, &r); err != nil || strings.TrimSpace(r.Text) == "" {
		return nil, &Error{"bad_request", "msg.send needs chat and non-empty text"}
	}
	cli, jid, id, dup, err := s.beginSend(r.Chat, r.ID)
	if err != nil {
		return nil, err
	}
	if dup {
		return sendReply(cli, id), nil
	}

	// Our row keeps the text as typed ("@Anna Lee") and the mentions, so its bubble links them as a
	// received one does; the wire message has "@<number>".
	extra := ""
	if _, jids := mentionWire(r.Text, r.Mentions); len(jids) > 0 {
		extra = withMentions("", r.Mentions)
	}
	_, created := s.store.queueOutgoing(jid, r.Chat, id, r.Text, r.Quote, extra, time.Now().Unix())
	s.nameNew(cli, jid, r.Chat, created)
	s.kick(r.Chat)
	return sendReply(cli, id), nil
}

// sendMsg sends msg as our row id, and returns the server's time, not the phone's: it drifts.
func (s *Session) sendMsg(cli *whatsmeow.Client, jid types.JID, id string, msg *waE2E.Message) (sendDone, error) {
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	resp, err := cli.SendMessage(ctx, jid, msg, whatsmeow.SendRequestExtra{ID: id})
	if err != nil {
		return sendDone{}, err
	}
	return sendDone{ts: resp.Timestamp.Unix()}, nil
}

// textMessage is the wire message of one of our texts: q is the message it quotes (empty for none).
func textMessage(cli *whatsmeow.Client, chat, text string, q msgRow, ms []mention) *waE2E.Message {
	wire, jids := mentionWire(text, ms)
	if q.ID == "" && len(jids) == 0 {
		return &waE2E.Message{Conversation: &text}
	}
	ci := &waE2E.ContextInfo{}
	if q.ID != "" {
		ci = quoteInfo(cli, chat, q)
	}
	ci.MentionedJID = jids
	return &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: &wire, ContextInfo: ci}}
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
	var wa []msgRow // call rows go locally only: our other devices have no such messages
	for _, m := range rows {
		id := m.ID
		s.store.ops <- func(t *storeTx) { t.deleteMessage(r.Chat, id) }
		if !isCallID(id) {
			wa = append(wa, m)
		}
	}
	if len(wa) > 0 {
		if err := cli.SendAppState(ctx, deleteForMePatch(jid, wa)); err != nil {
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
			if !m.Out || m.Deleted || m.State < stateSent || m.State > stateRead || isCallID(m.ID) {
				return nil, &Error{"not_ours", "only our own sent messages can be deleted for everyone"}
			}
		}
	}
	return rows, nil
}

type editReq struct {
	Chat, ID, Text string
	Mentions       []mention
}

func parseEdit(req json.RawMessage) (editReq, error) {
	var r editReq
	if err := json.Unmarshal(req, &r); err != nil || r.ID == "" || strings.TrimSpace(r.Text) == "" {
		return r, &Error{"bad_request", "msg.edit needs chat, id and non-empty text"}
	}
	return r, nil
}

// editable reads the message an edit names: our own sent text, or photo or video with a caption
// whose upload we know (editContent sends it again). WhatsApp's 20 minutes are the UI's to offer
// and the server's to refuse.
func (st *Store) editable(chat, id string) (msgRow, error) {
	rows, _ := st.deletable(chat, []string{id}, false)
	if len(rows) == 0 {
		return msgRow{}, &Error{"not_found", "no such message"}
	}
	m := rows[0]
	captioned := (m.Kind == msgPhoto || m.Kind == msgVideo) && m.Text != "" && m.Media != ""
	if !m.Out || m.Deleted || m.State < stateSent || m.State > stateRead || (m.Kind != msgText && !captioned) {
		return m, &Error{"not_ours", "only our own sent texts and captions can be edited"}
	}
	return m, nil
}

// editContent is an edit's new content: a text, or the photo or video again with its new caption.
// (verify on the real link): a video's edit carries no thumbnail, our rows keep none.
func editContent(m msgRow, text string, jids []string) *waE2E.Message {
	var ci *waE2E.ContextInfo
	if len(jids) > 0 {
		ci = &waE2E.ContextInfo{MentionedJID: jids}
	}
	var info mediaInfo
	if m.Kind != msgText && json.Unmarshal([]byte(m.Media), &info) == nil {
		var w, h *uint32
		if m.Width > 0 && m.Height > 0 {
			w, h = proto.Uint32(uint32(m.Width)), proto.Uint32(uint32(m.Height))
		}
		switch d := info.message(m.Kind).(type) {
		case *waE2E.ImageMessage:
			d.Caption, d.JPEGThumbnail, d.Width, d.Height, d.ContextInfo = &text, m.Thumb, w, h, ci
			return &waE2E.Message{ImageMessage: d}
		case *waE2E.VideoMessage:
			d.Caption, d.JPEGThumbnail, d.Width, d.Height, d.ContextInfo = &text, m.Thumb, w, h, ci
			return &waE2E.Message{VideoMessage: d}
		}
	}
	if ci != nil {
		return &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: &text, ContextInfo: ci}}
	}
	return &waE2E.Message{Conversation: &text}
}

// handleEdit is msg.edit: the new text goes as an edit of ours, and the row changes once it went.
// (verify on the real link): a reply's edit carries no quote; WhatsApp's clients keep the original's.
func (s *Session) handleEdit(req json.RawMessage) (any, error) {
	r, err := parseEdit(req)
	if err != nil {
		return nil, err
	}
	cli, jid, err := s.linked(r.Chat)
	if err != nil {
		return nil, err
	}
	m, err := s.store.editable(r.Chat, r.ID)
	if err != nil {
		return nil, err
	}
	wire, jids := mentionWire(r.Text, r.Mentions)
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	if _, err := cli.SendMessage(ctx, jid, cli.BuildEdit(jid, r.ID, editContent(m, wire, jids))); err != nil {
		return nil, err
	}
	s.store.ops <- func(t *storeTx) { t.editMessage(r.Chat, r.ID, r.Text) }
	return nil, nil
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
			if !isCallID(quote) { // a call row can't be quoted: WhatsApp doesn't know it
				q, _ = t.messageByID(chat, quote)
			}
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

// receipt moves the ticks of our messages, or clears unread when our other devices read a chat. A story's
// receipts (status@broadcast) are the viewers of ours instead.
func (st *Store) receipt(ctx context.Context, t *storeTx, cli *whatsmeow.Client, e *events.Receipt) {
	if e.Chat == types.StatusBroadcastJID { // a story's receipts: who viewed ours (stories.go), never a chat's ticks
		st.storyView(ctx, t, cli, e)
		return
	}
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
