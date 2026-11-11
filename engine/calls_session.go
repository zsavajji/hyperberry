package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	waLog "github.com/polymorfa/hypermeow/util/log"
	"github.com/purpshell/meowcaller"
	"github.com/rs/zerolog"
)

// The calls' ties to the session (step 7.2): meowcaller on each client, and the seams of Calls
// filled from the live client.

// callDropGrace is how long the connection to WhatsApp may be down before the live call ends
// (offline). ponytail: 15 s is a guess; hypermeow's first reconnects come within seconds.
var callDropGrace = 15 * time.Second

// logWriter lets meowcaller's zerolog write into the engine's log, at the level it was given.
type logWriter struct{ log waLog.Logger }

func (w logWriter) Write(p []byte) (int, error) {
	w.log.Infof("%s", strings.TrimSpace(string(p)))
	return len(p), nil
}

// meowcallerLog is info and above only: its per-frame diagnostics are debug and trace.
func meowcallerLog(log waLog.Logger) zerolog.Logger {
	cw := zerolog.ConsoleWriter{Out: logWriter{log}, NoColor: true, PartsExclude: []string{zerolog.TimestampFieldName}}
	return zerolog.New(cw).Level(zerolog.InfoLevel)
}

// initCalls makes the Calls of this session.
func (s *Session) initCalls() {
	c := newCalls(s.store, s.srv, s.log.Sub("calls"))
	c.ready = s.callReady
	c.resolve = s.callResolve
	c.photo = func(key string) string {
		ps := []participant{{JID: key}}
		s.photos.members(ps)
		return ps[0].Photo
	}
	c.picture = func(chat string) string {
		jid, err := types.ParseJID(chat)
		if err != nil {
			return ""
		}
		return s.photos.full(jid)
	}
	c.chatName = s.storedChatName
	c.isSelf = func(j types.JID) bool {
		cli := s.client()
		j = j.ToNonAD()
		return j == cli.Store.GetLID().ToNonAD() || cli.Store.ID != nil && j == cli.Store.ID.ToNonAD()
	}
	c.dial = func(ctx context.Context, target string, video bool, members []string) (liveCall, error) {
		var call *meowcaller.Call
		var err error
		if len(members) > 0 {
			call, err = s.mc.Load().GroupCallWithOptions(ctx, members, meowcaller.GroupCallOptions{GroupJID: target})
		} else {
			call, err = s.mc.Load().CallWithOptions(ctx, target, meowcaller.CallOptions{Video: video})
		}
		if err != nil {
			return nil, err
		}
		return call, nil
	}
	s.calls = c
}

// callsOn puts meowcaller on a new client. It must come before the client connects.
func (s *Session) callsOn(cli *whatsmeow.Client) {
	// 248-254 recorded meowcaller's diagnostics, key material included, for the group calls'
	// audio (7d); what they left goes.
	os.RemoveAll(filepath.Join(s.srv.dir, "logs", "calldiag"))
	mc := meowcaller.NewClient(cli, meowcaller.WithLogger(meowcallerLog(s.log.Sub("meowcaller"))))
	mc.OnIncomingCall(func(call *meowcaller.Call) {
		if cli == s.client() {
			s.calls.offer(call)
		}
	})
	// The offers meowcaller drops as already over: a call that rang while the engine was off.
	cli.AddEventHandler(func(evt any) {
		if e, ok := evt.(*events.CallOffer); ok && cli == s.client() {
			s.calls.endedOffer(e)
		}
	})
	s.mc.Store(mc)
}

// callReady checks that chat can be called, and gives the name: the person's, or the group's
// stored one.
func (s *Session) callReady(chat string) (string, error) {
	cli, jid, err := s.linked(chat)
	if err != nil {
		return "", err
	}
	if jid.Server == types.GroupServer {
		if !cli.IsConnected() {
			return "", &Error{"offline", "not connected to WhatsApp"}
		}
		return s.storedChatName(chat), nil
	}
	if jid.Server != types.DefaultUserServer && jid.Server != types.HiddenUserServer {
		return "", &Error{"bad_chat", "not a person: " + chat}
	}
	if me := jid.ToNonAD(); me == cli.Store.ID.ToNonAD() || me == cli.Store.GetLID().ToNonAD() {
		return "", &Error{"bad_chat", "that is us"}
	}
	if !cli.IsConnected() {
		return "", &Error{"offline", "not connected to WhatsApp"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), netTimeout)
	defer cancel()
	return displayName(ctx, cli, jid, types.EmptyJID), nil
}

// storedChatName is the name chats.db holds for chat. A group with no chat (since 238 a joined
// group gets one only with a message) is asked of WhatsApp, so a call's Hub item and new chat
// have its subject; "" if neither knows.
func (s *Session) storedChatName(chat string) (name string) {
	s.store.runOp(func(t *storeTx) { t.tx.QueryRow("SELECT name FROM chats WHERE jid = ?", chat).Scan(&name) })
	if jid, err := types.ParseJID(chat); name == "" && err == nil && jid.Server == types.GroupServer {
		ctx, cancel := context.WithTimeout(context.Background(), netTimeout)
		defer cancel()
		if info, err := s.client().GetGroupInfo(ctx, jid); err == nil && info != nil {
			name = info.Name
		}
	}
	return name
}

// callResolve maps a caller to the chat key a message from them would have, and their name.
func (s *Session) callResolve(peer types.JID) (key, name string) {
	cli := s.client()
	ctx, cancel := context.WithTimeout(context.Background(), netTimeout)
	defer cancel()
	key = peer.ToNonAD().String()
	s.store.runOp(func(t *storeTx) { key = chatKey(ctx, t, cli.Store.LIDs, peer, types.EmptyJID) })
	return key, displayName(ctx, cli, peer.ToNonAD(), types.EmptyJID)
}
