package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/polymorfa/hypermeow/appstate"
	"github.com/polymorfa/hypermeow/proto/waCommon"
	"github.com/polymorfa/hypermeow/types"
)

// chatSetReq is chat.set; every field but Chat is optional, and a field that is absent is left alone.
type chatSetReq struct {
	Chat    string
	Pin     *bool
	Mute    *int64 // unix seconds until; 0 off; -1 always
	Archive *bool
	Unread  *bool
}

func parseChatSet(req json.RawMessage) (chatSetReq, error) {
	var r chatSetReq
	if err := json.Unmarshal(req, &r); err != nil || (r.Pin == nil && r.Mute == nil && r.Archive == nil && r.Unread == nil) ||
		(r.Mute != nil && *r.Mute < -1) {
		return r, &Error{"bad_request", "chat.set needs chat and one of pin, mute, archive, unread"}
	}
	return r, nil
}

// apply updates the row at once; the server's echo of the patch then agrees. Archiving unpins,
// as WhatsApp does.
func (st *Store) applyChatSet(r chatSetReq) {
	st.ops <- func(t *storeTx) {
		if r.Archive != nil {
			t.setArchived(r.Chat, *r.Archive)
			if *r.Archive {
				t.setPinned(r.Chat, false)
			}
		}
		if r.Pin != nil {
			t.setPinned(r.Chat, *r.Pin)
		}
		if r.Mute != nil {
			until := *r.Mute
			if until < 0 {
				until = muteForever
			}
			t.setMuted(r.Chat, until)
		}
		if r.Unread != nil {
			n := 0
			if *r.Unread {
				n = -1
			}
			t.setUnread(r.Chat, n)
		}
	}
}

func (s *Session) handleChatSet(req json.RawMessage) (any, error) {
	r, err := parseChatSet(req)
	if err != nil {
		return nil, err
	}
	cli, jid, err := s.linked(r.Chat)
	if err != nil {
		return nil, err
	}
	s.store.applyChatSet(r)
	// Local only: WhatsApp's app state has no channels.
	if jid.Server == types.NewsletterServer {
		return nil, nil
	}
	var patches []appstate.PatchInfo
	if r.Pin != nil {
		patches = append(patches, appstate.BuildPin(jid, *r.Pin))
	}
	if r.Mute != nil {
		switch {
		case *r.Mute == 0:
			patches = append(patches, appstate.BuildMuteAbs(jid, false, nil))
		case *r.Mute < 0:
			patches = append(patches, appstate.BuildMuteAbs(jid, true, nil))
		default:
			ms := *r.Mute * 1000
			patches = append(patches, appstate.BuildMuteAbs(jid, true, &ms))
		}
	}
	if r.Archive != nil || r.Unread != nil {
		ts, key := s.store.lastKey(r.Chat)
		if r.Archive != nil {
			patches = append(patches, appstate.BuildArchive(jid, *r.Archive, ts, key))
		}
		if r.Unread != nil {
			patches = append(patches, appstate.BuildMarkChatAsRead(jid, !*r.Unread, ts, key))
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	for _, p := range patches {
		if err := cli.SendAppState(ctx, p); err != nil {
			// ponytail: the local change stays; the main phone isn't told. Queue and retry if that matters.
			s.log.Warnf("chat.set: app state: %v", err)
		}
	}
	return nil, nil
}

// lastKey is the chat's newest message as app-state patches want it; the zero time and nil key
// when the chat has none (hypermeow then uses now).
func (st *Store) lastKey(chat string) (time.Time, *waCommon.MessageKey) {
	type res struct {
		ts  time.Time
		key *waCommon.MessageKey
	}
	got := make(chan res, 1)
	st.ops <- func(t *storeTx) {
		var id, sender string
		var ts int64
		var out bool
		if t.tx.QueryRow("SELECT id, ts, out, COALESCE(sender, '') FROM messages WHERE chat = ? AND id NOT LIKE 'call-%' ORDER BY ts DESC, rowid DESC LIMIT 1",
			chat).Scan(&id, &ts, &out, &sender) != nil {
			got <- res{}
			return
		}
		k := &waCommon.MessageKey{RemoteJID: &chat, FromMe: &out, ID: &id}
		if sender != "" && !out {
			k.Participant = &sender
		}
		got <- res{time.Unix(ts, 0), k}
	}
	r := <-got
	return r.ts, r.key
}

// handleChatSet is chat.set: the same rows change as with the real one, nothing is sent.
func (s *Sim) handleChatSet(req json.RawMessage) (any, error) {
	r, err := parseChatSet(req)
	if err != nil {
		return nil, err
	}
	s.log.Infof("sim: chat.set %s", req)
	if _, err := s.chat(r.Chat); err != nil {
		return nil, err
	}
	s.store.applyChatSet(r)
	return nil, nil
}
