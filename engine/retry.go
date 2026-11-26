package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
)

// Sending a row of ours again from chats.db alone, whatever made it: the rows an earlier run left sending (sendQueued, at
// each connect) and the ones that failed, when the user asks (msg.retry). A row is rebuilt as the path that made it gave
// sendOut its attempt, with the same id, so WhatsApp drops a copy of one that had gone after all.

// resendAttempt is the attempt sendOut makes to send our row. An error says it can't be rebuilt: its file is gone, it is a
// kind we don't send, a sticker has no source; the caller fails the row.
// ponytail: a poll goes with a new secret.
func (s *Session) resendAttempt(row msgRow) (func(*whatsmeow.Client) error, error) {
	jid, err := types.ParseJID(row.Chat)
	if err != nil || jid.IsEmpty() {
		return nil, errors.New("not a chat key: " + row.Chat)
	}
	chat, id := row.Chat, row.ID
	if forwardScore(row.Extra) == 0 { // a forward is one message, whatever it holds: its upload is on the server already
		switch row.Kind {
		case msgPhoto, msgVideo, msgAudio, msgDoc:
			if fi, err := os.Stat(row.Path); err != nil || !fi.Mode().IsRegular() {
				return nil, errors.New("its file is gone: " + row.Path)
			}
			o := s.mediaOf(row)
			return func(c *whatsmeow.Client) error { return s.uploadMedia(c, jid, row.Path, o) }, nil
		case msgVoice:
			ogg, err := os.ReadFile(oggPath(s.srv.dir, id))
			if err != nil {
				return nil, errors.New("its recording is gone")
			}
			n := &voiceNote{ogg: ogg, wave: row.Waveform, secs: row.Duration, quote: s.quoteOf(row)}
			return func(c *whatsmeow.Client) error { return s.uploadVoice(c, jid, chat, id, n) }, nil
		case msgSticker:
			dir, err := filepath.Abs(s.srv.dir)
			if err != nil {
				return nil, err
			}
			var src stickerSrc
			var ok bool
			if err := s.store.runOp(func(t *storeTx) { src, ok = t.stickerSource(dir, row.Sticker) }); err != nil {
				return nil, err
			}
			if !ok || (!src.usable() && src.path == "") {
				return nil, errors.New("its sticker has no file to send")
			}
			q := s.quoteOf(row)
			return func(c *whatsmeow.Client) error { return s.sendSticker(c, jid, chat, id, row.Sticker, &src, q) }, nil
		}
	}
	msg, err := s.resendMessage(s.client(), row)
	if err != nil {
		return nil, err
	}
	return func(c *whatsmeow.Client) error { return s.sendMsg(c, jid, chat, id, msg) }, nil
}

// mediaOf is the upload of our media row, rebuilt: the mimetype by its file's extension, a video's poster read back from
// the file the send kept (none: it goes without), and the original the row's path is a copy of, from its extra.
func (s *Session) mediaOf(row msgRow) *mediaOut {
	_, mimeType := sendKind(row.Path)
	poster, _ := os.ReadFile(posterPath(s.srv.dir, row.ID))
	var x struct{ Original string }
	json.Unmarshal([]byte(row.Extra), &x) // "" or not ours: no original
	return &mediaOut{row: row, mime: mimeType, quote: s.quoteOf(row), poster: poster, original: x.Original}
}

// withOriginal sets the file a media row's path is a copy of in its extra ("original"), keeping what else is there, so a
// send rebuilt after a restart (mediaOf) still knows it and dropSendFiles removes its copy. Its readers look for their own
// keys by name, so it doesn't change what they see.
func withOriginal(extra, original string) string {
	b, err := json.Marshal(original)
	if err != nil {
		return extra
	}
	return withKey(extra, "original", string(b))
}

// resendMessage is the wire message of our row that goes as it is, with nothing to upload: a text (a reply, with
// mentions), a reply to a story, a poll, a forward of any kind.
func (s *Session) resendMessage(cli *whatsmeow.Client, row msgRow) (*waE2E.Message, error) {
	var x struct {
		Mentions  []mention
		Forwarded int
		Story     bool
		Options   []string
		Max       int
	}
	json.Unmarshal([]byte(row.Extra), &x) // "" or not ours: none of these
	switch {
	case x.Forwarded > 0:
		row.Extra = withKey(row.Extra, "forwarded", strconv.Itoa(x.Forwarded-1)) // the row has the score forwardContent adds
		msg, _, err := forwardContent(row)
		return msg, err
	case row.Kind == msgPoll:
		if len(x.Options) < minPollOptions {
			return nil, errors.New("its poll has no options")
		}
		return cli.BuildPollCreation(row.Text, x.Options, x.Max), nil
	case row.Kind != msgText:
		return nil, fmt.Errorf("a message of kind %d can't be sent again", row.Kind)
	case x.Story: // the story itself if it is still here, else the one line the row keeps of it
		quoted := quotedStory(msgText, row.QuotePreview, nil)
		if kind, text, thumb, ok := s.store.storyQuote(row.Chat, row.QuoteID); ok {
			quoted = quotedStory(kind, text, thumb)
		}
		return storyReplyMessage(row.Chat, row.QuoteID, row.Text, quoted), nil
	}
	return textMessage(cli, row.Chat, row.Text, s.quoteOf(row), x.Mentions), nil
}

// quoteOf is the message our row quotes, read again; empty for none, or gone since: it is sent without the quote.
func (s *Session) quoteOf(row msgRow) msgRow {
	if q, ok := s.store.row(row.Chat, row.QuoteID); ok {
		return q
	}
	return msgRow{}
}

// failRow marks our row failed, as sendOut does when an attempt fails.
func (s *Session) failRow(chat, id string) {
	s.store.ops <- func(t *storeTx) { t.sentAs(chat, id, 0, stateFailed) }
}

// sendQueued sends the rows an earlier run left sending, of every kind, oldest first, each rebuilt from its row
// (resendAttempt) and sent through sendOut, so a connection that drops queues it again. A row that can't be rebuilt is
// failed: the user may retry it, but it never stays sending for good. It stops, leaving the rest sending, when the
// connection is gone again. The sends of this run wait in s.queue instead (sendqueue.go), and are in s.sending, which this
// skips.
func (s *Session) sendQueued(cli *whatsmeow.Client) {
	for _, row := range s.store.queuedRows() {
		if !cli.IsConnected() {
			return
		}
		if result := s.flushRow(cli, row); result != "" {
			s.log.Infof("queued send: %s %s %s", row.Chat, row.ID, result)
		}
	}
}

// flushRow sends one row sendQueued read, and says how it went; "" when it isn't its to send.
func (s *Session) flushRow(cli *whatsmeow.Client, row msgRow) string {
	if _, busy := s.sending.LoadOrStore(row.ID, true); busy {
		return ""
	}
	if !s.store.isSending(row.Chat, row.ID) { // msg.cancel took it since the read, or a receipt moved it
		s.sending.Delete(row.ID)
		return ""
	}
	attempt, err := s.resendAttempt(row)
	if err != nil {
		s.sending.Delete(row.ID)
		s.failRow(row.Chat, row.ID)
		os.Remove(posterPath(s.srv.dir, row.ID)) // nothing will send it again: a video's poster is of no use
		return "failed, can't be rebuilt: " + err.Error()
	}
	switch queued, err := s.sendOut(cli, row.Chat, row.ID, attempt); {
	case queued:
		return "down again, queued"
	case err != nil:
		return "failed: " + err.Error()
	}
	return "sent"
}

// queuedRows reads our rows still sending, oldest first, of any kind: the ones an earlier run left (this run's are in s.queue).
func (st *Store) queuedRows() []msgRow {
	got := make(chan []msgRow, 1)
	st.ops <- func(t *storeTx) {
		var keys [][2]string
		rows, err := t.tx.Query(`SELECT chat, id FROM messages WHERE out = 1 AND state = ? AND deleted = 0 ORDER BY ts, rowid`, stateSending)
		if err != nil {
			t.log.Warnf("queued rows: %v", err)
		} else {
			for rows.Next() {
				var k [2]string
				if rows.Scan(&k[0], &k[1]) == nil {
					keys = append(keys, k)
				}
			}
			rows.Close()
		}
		var out []msgRow
		for _, k := range keys {
			if m, ok := t.messageByID(k[0], k[1]); ok {
				out = append(out, m)
			}
		}
		got <- out
	}
	return <-got
}

// resend puts our failed row back to sending; false when it isn't one.
func (t *storeTx) resend(chat, id string) bool {
	if t.exec("UPDATE messages SET state = ? WHERE chat = ? AND id = ? AND out = 1 AND state = ?", stateSending, chat, id, stateFailed) == 0 {
		return false
	}
	t.changed(chat, id)
	t.syncLast(chat, id)
	return true
}

// handleRetry is msg.retry: one of our failed rows goes back to sending and is sent again with its id, as the queue does at a
// connect. The reply is {id}, and queued "true" while WhatsApp is unreachable; the outcome is the row's state, as for a send.
// not_found; not_failed (not ours, or not failed: nothing to do); cannot_retry (it can't be rebuilt: it stays failed).
func (s *Session) handleRetry(req json.RawMessage) (any, error) {
	var r struct{ Chat, ID string }
	if err := json.Unmarshal(req, &r); err != nil || r.Chat == "" || r.ID == "" {
		return nil, &Error{"bad_request", "msg.retry needs chat and id"}
	}
	cli, _, err := s.linked(r.Chat)
	if err != nil {
		return nil, err
	}
	m, ok := s.store.row(r.Chat, r.ID)
	switch {
	case !ok:
		return nil, &Error{"not_found", "no such message"}
	case !m.Out || m.State != stateFailed || m.Deleted:
		return nil, &Error{"not_failed", "only a message of ours that failed can be sent again"}
	}
	attempt, err := s.resendAttempt(m)
	if err != nil {
		return nil, &Error{"cannot_retry", err.Error()}
	}
	s.sending.Store(r.ID, true) // before the row is sending again: the queue's flush leaves it be
	var back bool
	if err := s.store.runOp(func(t *storeTx) { back = t.resend(r.Chat, r.ID) }); err != nil || !back {
		s.sending.Delete(r.ID)
		return nil, &Error{"not_failed", "only a message of ours that failed can be sent again"}
	}
	reply := map[string]string{"id": r.ID}
	if !cli.IsConnected() {
		reply["queued"] = "true"
	}
	go s.sendOut(cli, r.Chat, r.ID, attempt) // an upload takes minutes: not in the request's way
	return reply, nil
}

// outboxDir is where the engine keeps the copies of the files its sends wait with.
func outboxDir(dir string) string {
	d, _ := filepath.Abs(filepath.Join(dir, "outbox-engine"))
	return d
}

// posterPath is where a video's poster waits with the send, for a rebuild after a restart: the row keeps none. It goes when
// the message is sent, cancelled, deleted (dropOwnFiles) or can't be rebuilt; a failed row keeps it for msg.retry.
func posterPath(dir, id string) string { return filepath.Join(outboxDir(dir), id+".poster") }

// dropSendFiles runs in the op that records a media send as sent, with from the file it sent: what it kept for a rebuild goes
// (the video's poster), and so do the copies it made of the file, the engine's (keepOutgoing) and the UI outbox's
// (outbox/<id>/), the row's path going back to the original. A gone original keeps the copies: they are the only file the
// bubble can show. So does one under <data dir>/cache/ (the PNG converted to JPEG, say): the cache is cleared and its names
// are reused, so it is as good as gone.
// The original is in o as media.send gave it, or from the row's extra for a rebuild (mediaOf); a row made before extra kept it
// has none after a restart, and keeps its copy.
// ponytail: with two copies and no original both stay, though only the row's path is shown.
func (s *Session) dropSendFiles(t *storeTx, o *mediaOut, from string) {
	dir, id := s.srv.dir, o.row.ID
	eng, ui := outboxDir(dir), uiOutboxDir(dir, id)
	gone := []string{posterPath(dir, id)}
	var copies []string
	for _, c := range []string{from, o.row.Path} {
		if d := filepath.Dir(c); (d == eng || d == ui) && c != o.original {
			copies = append(copies, c)
		}
	}
	cache := filepath.Join(filepath.Dir(eng), "cache") + string(filepath.Separator)
	restored := false
	if _, err := os.Stat(o.original); err == nil && len(copies) > 0 && !strings.HasPrefix(o.original, cache) {
		t.setFile(o.row.Chat, id, 2, o.original)
		t.exec("UPDATE messages SET extra = NULLIF(json_remove(extra, '$.original'), '{}') WHERE chat = ? AND id = ? AND json_valid(extra)",
			o.row.Chat, id) // the path is the original again: nothing is a copy
		gone, restored = append(gone, copies...), true
	}
	t.after = append(t.after, func() {
		for _, p := range gone {
			os.Remove(p)
		}
		if restored { // the UI outbox's directory, with its copy: after a restart the row's path was the engine's copy, not this
			os.RemoveAll(ui)
		}
	})
}

// uiOutboxDir is where the UI outbox keeps the copy of the file of its media.send: <data dir>/outbox/<id>/.
func uiOutboxDir(dir, id string) string {
	return filepath.Join(filepath.Dir(outboxDir(dir)), "outbox", id)
}

// dropOwnFiles runs where our row m is deleted (deleteMessage, clearChat): once the transaction has committed, the files of
// its send that are ours alone go. Those are its path when it lies in outbox-engine/, the UI outbox's outbox/<id>/ with its
// copy (which the row's path need not point to: the engine's copy replaced it), a video's poster, and a voice note's OGG;
// never a file elsewhere, which is the user's own photo. What another row has too (a forward of this message carries its
// path) stays.
func (t *storeTx) dropOwnFiles(m msgRow) {
	if !m.Out || t.st == nil { // st is nil in tests of single writes
		return
	}
	dir := t.st.srv.dir
	held := func(prefix string) bool { // a row still points there
		var n int
		return t.tx.QueryRow("SELECT 1 FROM messages WHERE instr(path, ?) = 1 LIMIT 1", prefix).Scan(&n) == nil
	}
	gone := []string{posterPath(dir, m.ID)}
	if m.Kind == msgVoice {
		gone = append(gone, oggPath(dir, m.ID))
	}
	if m.Path != "" && filepath.Dir(m.Path) == outboxDir(dir) && !held(m.Path) {
		gone = append(gone, m.Path)
	}
	ui := uiOutboxDir(dir, m.ID)
	if _, err := os.Stat(ui); err != nil || held(ui+string(filepath.Separator)) { // held scans the table: only for a directory that is there
		ui = ""
	}
	t.after = append(t.after, func() {
		for _, p := range gone {
			os.Remove(p)
		}
		if ui != "" {
			os.RemoveAll(ui)
		}
	})
}

// keepOutgoing copies the file a media send waits with to outbox-engine/<id><ext> and points the row's path at the copy, so
// the send doesn't depend on the original staying, and a restart's rebuild finds it. The row's path is the bubble's picture
// until the send is over: then dropSendFiles puts the original back when it is still there, else the copy stays; msg.cancel
// removes it. original is the file the send was first made from, kept in the row's extra with the new path (withOriginal).
// It returns the path to send from: the copy, or the original when the copy failed.
func (s *Session) keepOutgoing(chat, id, path, original string) string {
	dst := filepath.Join(outboxDir(s.srv.dir), id+filepath.Ext(path))
	if err := copyFile(path, dst); err != nil {
		s.log.Warnf("outbox copy of %s: %v", id, err)
		return path
	}
	s.store.ops <- func(t *storeTx) {
		m, _ := t.messageByID(chat, id)
		t.exec("UPDATE messages SET path = ?, extra = ? WHERE chat = ? AND id = ?", dst, withOriginal(m.Extra, original), chat, id)
	}
	return dst
}
