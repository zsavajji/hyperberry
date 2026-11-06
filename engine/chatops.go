package main

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/appstate"
	"github.com/polymorfa/hypermeow/proto/waSyncAction"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"
)

// The contact page's account actions: block, clear, delete, and the text export.

// isPerson: the key is a one-to-one chat, by phone number or LID.
func isPerson(jid types.JID) bool {
	return jid.Server == types.DefaultUserServer || jid.Server == types.HiddenUserServer
}

// isBlocked: an item of the list has one of users (user parts, empty ones skipped) as its LID or number.
func isBlocked(bl *types.Blocklist, users ...string) bool {
	for _, it := range bl.Items {
		for _, a := range []types.JID{it.LID, it.PN} {
			for _, u := range users {
				if u != "" && a.User == u {
					return true
				}
			}
		}
	}
	return false
}

// parseChatReq reads the chat key of the requests that take nothing else.
func parseChatReq(req json.RawMessage, cmd string) (string, error) {
	var r struct{ Chat string }
	if err := json.Unmarshal(req, &r); err != nil {
		return "", &Error{"bad_request", cmd + " needs chat"}
	}
	return r.Chat, nil
}

// parseBlock is contact.block's {chat, block}; block must be there.
func parseBlock(req json.RawMessage) (chat string, block bool, err error) {
	var r struct {
		Chat  string
		Block *bool
	}
	if json.Unmarshal(req, &r) != nil || r.Block == nil {
		return "", false, &Error{"bad_request", "contact.block needs chat and block"}
	}
	return r.Chat, *r.Block, nil
}

func (s *Session) handleContactBlock(req json.RawMessage) (any, error) {
	chat, block, err := parseBlock(req)
	if err != nil {
		return nil, err
	}
	cli, jid, err := s.linked(chat)
	if err != nil {
		return nil, err
	}
	if !isPerson(jid) {
		return nil, &Error{"bad_chat", "not a person: " + chat}
	}
	action := events.BlocklistChangeActionUnblock
	if block {
		action = events.BlocklistChangeActionBlock
	}
	ctx, cancel := context.WithTimeout(context.Background(), netTimeout)
	defer cancel()
	_, err = cli.UpdateBlocklist(ctx, jid, action, "")
	return nil, err
}

// applyClear empties the chat at once; with drop the chat goes too.
func (st *Store) applyClear(chat string, drop bool) {
	st.ops <- func(t *storeTx) { t.clearChat(chat, drop) }
}

// clearChatPatch tells our other devices to clear the chat up to its last message. hypermeow has
// no builder: index 2 is unknown even to it ("1" taken as keep starred), unverified until the
// real link. A chat with no message ends at now, as BuildDeleteChat's does.
func clearChatPatch(chat types.JID, last time.Time) appstate.PatchInfo {
	if last.IsZero() {
		last = time.Now()
	}
	return appstate.PatchInfo{Type: appstate.WAPatchRegularHigh, Mutations: []appstate.MutationInfo{{
		Index:   []string{appstate.IndexClearChat, chat.String(), "1", "0"},
		Version: 6,
		Value: &waSyncAction.SyncActionValue{ClearChatAction: &waSyncAction.ClearChatAction{
			MessageRange: &waSyncAction.SyncActionMessageRange{LastMessageTimestamp: proto.Int64(last.Unix())}}},
	}}}
}

// sendPatch tells the main phone; a failure is only logged and the local change stays.
func (s *Session) sendPatch(cli *whatsmeow.Client, cmd string, p appstate.PatchInfo) {
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	if err := cli.SendAppState(ctx, p); err != nil {
		// ponytail: the main phone keeps the chat; queue and retry if that matters.
		s.log.Warnf("%s: app state: %v", cmd, err)
	}
}

func (s *Session) handleChatClear(req json.RawMessage) (any, error) {
	chat, err := parseChatReq(req, "chat.clear")
	if err != nil {
		return nil, err
	}
	cli, jid, err := s.linked(chat)
	if err != nil {
		return nil, err
	}
	last, _ := s.store.lastKey(chat) // before the rows go
	s.store.applyClear(chat, false)
	s.sendPatch(cli, "chat.clear", clearChatPatch(jid, last))
	return nil, nil
}

func (s *Session) handleChatDelete(req json.RawMessage) (any, error) {
	chat, err := parseChatReq(req, "chat.delete")
	if err != nil {
		return nil, err
	}
	cli, jid, err := s.linked(chat)
	if err != nil {
		return nil, err
	}
	last, key := s.store.lastKey(chat) // before the rows go
	s.store.applyClear(chat, true)
	s.sendPatch(cli, "chat.delete", appstate.BuildDeleteChat(jid, last, key, false))
	return nil, nil
}

// sharedDownloads is the BB10 phone's Downloads folder, there with the access_shared permission.
var sharedDownloads = "/accounts/1000/shared/downloads"

// downloadsDir is Downloads when this has one, else a folder of the data dir (the PC, tests).
func downloadsDir(dataDir string) string {
	if fi, err := os.Stat(sharedDownloads); err == nil && fi.IsDir() {
		return sharedDownloads
	}
	return filepath.Join(dataDir, "downloads")
}

// fileSafe turns a chat's name into something every file system takes.
var fileSafe = strings.NewReplacer("/", "_", `\`, "_", ":", "_", "*", "_", "?", "_", `"`, "_", "<", "_", ">", "_", "|", "_")

// exportText is the chat as WhatsApp's text export: a line per message, in the order given.
// name is the chat's name; the rows' senders are named in anything but a one-to-one chat.
func exportText(chat, name string, group bool, rows []msgRow) string {
	peer, _, _ := strings.Cut(chat, "@")
	peer = cmp.Or(name, peer)
	var b strings.Builder
	for _, r := range rows {
		line := r.Text
		if r.Kind != msgService {
			who := peer
			if r.Out {
				who = "You"
			} else if group {
				sender, _, _ := strings.Cut(r.Sender, "@")
				who = cmp.Or(r.SenderName, sender, peer)
			}
			switch {
			case r.Deleted:
				line = word("This message was deleted")
			case r.Kind == msgNotice: // the text is a code
				line = preview(msgNotice, r.Text, 0)
			case r.Kind == msgPhoto, r.Kind == msgVideo, r.Kind == msgVoice, r.Kind == msgAudio, r.Kind == msgDoc, r.Kind == msgSticker:
				line = "<Media omitted>"
			}
			line = who + ": " + line
		}
		b.WriteString(time.Unix(r.TS, 0).Format("02/01/06, 15:04 - "))
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// exportRows reads the chat's name, kind and messages, oldest first; ok is false for a chat
// that isn't in chats.db.
func (st *Store) exportRows(chat string) (name string, kind int, rows []msgRow, ok bool) {
	done := make(chan struct{})
	st.ops <- func(t *storeTx) {
		defer close(done)
		var n sql.NullString
		if t.tx.QueryRow("SELECT name, kind FROM chats WHERE jid = ?", chat).Scan(&n, &kind) != nil {
			return
		}
		name, ok = n.String, true
		q, err := t.tx.Query(`SELECT ts, out, COALESCE(sender, ''), COALESCE(sender_name, ''), kind, COALESCE(text, ''), deleted
FROM messages WHERE chat = ? ORDER BY ts, rowid`, chat)
		if err != nil {
			t.log.Warnf("export: %v", err)
			return
		}
		defer q.Close()
		for q.Next() {
			var r msgRow
			if q.Scan(&r.TS, &r.Out, &r.Sender, &r.SenderName, &r.Kind, &r.Text, &r.Deleted) == nil {
				rows = append(rows, r)
			}
		}
	}
	<-done
	return
}

// handleExport is chat.export for the real and the simulated account: it only reads chats.db.
func (st *Store) handleExport(req json.RawMessage) (any, error) {
	chat, err := parseChatReq(req, "chat.export")
	if err != nil {
		return nil, err
	}
	name, kind, rows, ok := st.exportRows(chat)
	if !ok {
		return nil, &Error{"bad_chat", "no such chat: " + chat}
	}
	title, _, _ := strings.Cut(chat, "@")
	title = cmp.Or(name, title)
	dir, err := filepath.Abs(downloadsDir(st.srv.dir))
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, fileSafe.Replace("WhatsApp Chat with "+title+".txt"))
	if err := writeFile(path, []byte(exportText(chat, name, kind != chatUser, rows))); err != nil {
		return nil, err
	}
	return map[string]string{"path": path}, nil
}
