package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
)

const netTimeout = 15 * time.Second

func registerPresence(srv *Server, s *Session) {
	srv.Handle("chat.read", s.handleRead)
	srv.Handle("chat.typing", s.handleTyping)
	srv.Handle("chat.open", s.handleOpen)
	srv.Handle("chat.close", s.handleClose)
	srv.Handle("ui.state", s.handleUIState)
}

func (s *Session) handleRead(req json.RawMessage) (any, error) {
	var r struct{ Chat string }
	json.Unmarshal(req, &r)
	cli, jid, err := s.linked(r.Chat)
	if err != nil {
		return nil, err
	}
	got := make(chan []unreadMsg, 1)
	s.store.ops <- func(t *storeTx) {
		got <- t.unreadIncoming(r.Chat)
		t.setUnread(r.Chat, 0)
	}
	// One receipt per sender: WhatsApp takes a batch only from a single sender.
	bySender := map[string][]types.MessageID{}
	for _, m := range <-got {
		k := ""
		if jid.Server == types.GroupServer {
			k = m.Sender
		}
		bySender[k] = append(bySender[k], m.ID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), netTimeout)
	defer cancel()
	for k, ids := range bySender {
		sender, _ := types.ParseJID(k) // empty in 1:1
		// A failure leaves unread at 0: the phone resyncs the read state.
		if err := cli.MarkRead(ctx, ids, time.Now(), jid, sender); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func (s *Session) handleTyping(req json.RawMessage) (any, error) {
	var r struct {
		Chat      string
		On, Voice bool
	}
	json.Unmarshal(req, &r)
	cli, jid, err := s.linked(r.Chat)
	if err != nil {
		return nil, err
	}
	return nil, chatState(cli, jid, r.On, r.Voice)
}

// chatState sends our "typing" or "recording…" state to a chat; on false it is paused.
func chatState(cli *whatsmeow.Client, jid types.JID, on, voice bool) error {
	state, media := types.ChatPresencePaused, types.ChatPresenceMediaText
	if on {
		state = types.ChatPresenceComposing
	}
	if voice {
		media = types.ChatPresenceMediaAudio
	}
	ctx, cancel := context.WithTimeout(context.Background(), netTimeout)
	defer cancel()
	return cli.SendChatPresence(ctx, jid, state, media)
}

func (s *Session) handleOpen(req json.RawMessage) (any, error) {
	var r struct{ Chat string }
	json.Unmarshal(req, &r)
	cli, jid, err := s.linked(r.Chat)
	if err != nil {
		return nil, err
	}
	if jid.Server == types.GroupServer { // groups have no online status
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), netTimeout)
	defer cancel()
	return nil, cli.SubscribePresence(ctx, jid)
}

// handleClose has nothing to undo: the chat on screen comes from ui.state.
func (s *Session) handleClose(json.RawMessage) (any, error) { return nil, nil }

// handleUIState sets the account's presence from the app's focus. WhatsApp sends typing and
// online status only to clients that are available, and an available client makes the main
// phone stop showing notifications, which is what WhatsApp Web does when focused.
func (s *Session) handleUIState(req json.RawMessage) (any, error) {
	var r struct {
		Fullscreen bool
		OpenChat   string
	}
	json.Unmarshal(req, &r)
	cli := s.client()
	if cli.Store.ID == nil {
		return nil, &Error{"unlinked", "link this device first"}
	}
	s.store.ui.set(r.Fullscreen, r.OpenChat)
	state := types.PresenceUnavailable
	if r.Fullscreen {
		state = types.PresenceAvailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), netTimeout)
	defer cancel()
	return nil, cli.SendPresence(ctx, state)
}

// typing and presence only emit: they key the chat in the writer (chatKey needs its tx) and
// touch no rows.
func (st *Store) typing(ctx context.Context, t *storeTx, cli *whatsmeow.Client, e *events.ChatPresence) {
	if e.IsFromMe {
		return
	}
	who := ""
	if e.IsGroup {
		who = firstWord(resolveName(ctx, cli, e.Sender, e.SenderAlt, ""))
	}
	st.srv.Emit("typing", map[string]any{"chat": chatKey(ctx, t, cli.Store.LIDs, e.Chat, e.SenderAlt), "who": who,
		"on": e.State == types.ChatPresenceComposing, "voice": e.Media == types.ChatPresenceMediaAudio})
}

func (st *Store) presence(ctx context.Context, t *storeTx, cli *whatsmeow.Client, e *events.Presence) {
	var seen int64
	if !e.LastSeen.IsZero() {
		seen = e.LastSeen.Unix()
	}
	st.srv.Emit("presence", map[string]any{"chat": chatKey(ctx, t, cli.Store.LIDs, e.From, types.EmptyJID),
		"online": !e.Unavailable, "lastSeen": seen})
}
