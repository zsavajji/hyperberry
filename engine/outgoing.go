package main

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/types"
)

// The outgoing pipeline. A message of ours that has not gone is its row in chats.db, state sending: the row is the queue,
// and nothing else holds a send. Each send handler checks its request (beginSend), writes the row and its files
// (sendfiles.go), kicks the chat and replies; the chat's worker then sends its rows oldest first, each rebuilt from its row
// alone (buildAttempt, retry.go), and records the outcome. A connection that is gone leaves the row sending and ends the
// worker; the next connect's gate (below) starts them again. Our pending stories are the same, as the "chat" ownStory.

// sendDone is what a send that went leaves for the worker to record: the server's time, and a media send's upload (the
// row's media column, as mediaInfo JSON).
type sendDone struct {
	ts    int64
	media string
}

// attempt is one try at sending a row, with the client of the moment.
type attempt func(cli *whatsmeow.Client) (sendDone, error)

// outgoing says who is sending what, and whether workers may run. The gate: after a connect the rows wait until the offline
// backlog is in (events.OfflineSyncCompleted, else offlineWait after the connect), since a receipt in it may say a message of
// ours had in fact gone (a poll sent again would get a new secret); it closes when the connection goes.
type outgoing struct {
	mu       sync.Mutex
	up       bool                          // the gate is open
	workers  map[string]bool               // the chats with a worker running
	inflight map[string]string             // chat -> the id its worker is attempting now
	timer    *time.Timer                   // opens the gate offlineWait after a connect, if the backlog's end hasn't
	build    func(msgRow) (attempt, error) // tests'; nil is buildAttempt
}

// offlineWait is how long after a connect the sends that waited go without the offline backlog's end.
var offlineWait = 10 * time.Second

// connected starts the wait for the backlog's end: open runs at it (backlogIn says so), else offlineWait after the connect.
func (o *outgoing) connected(open func()) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.timer != nil {
		o.timer.Stop()
	}
	o.timer = time.AfterFunc(offlineWait, open)
}

// backlogIn says whether the gate is still to open: the timer hasn't run, and now doesn't.
func (o *outgoing) backlogIn() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.timer != nil && o.timer.Stop()
}

// shut closes the gate: the connection is gone, or the account.
func (o *outgoing) shut() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.up = false
	if o.timer != nil {
		o.timer.Stop()
	}
}

// openGate lets the workers go: one for every chat that has a row sending, and the stories'. cli is the client the connect
// was for; once a logout has replaced it, it opens nothing.
func (s *Session) openGate(cli *whatsmeow.Client) {
	if cli != s.client() {
		return
	}
	s.out.mu.Lock()
	s.out.up = true
	s.out.mu.Unlock()
	for _, chat := range append(s.store.sendingChats(), ownStory) {
		s.kick(chat)
	}
}

// kick starts the worker of chat, if the gate is open and none runs: its row is in chats.db already.
func (s *Session) kick(chat string) {
	o := &s.out
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.up || o.workers[chat] {
		return
	}
	if o.workers == nil {
		o.workers, o.inflight = map[string]bool{}, map[string]string{}
	}
	o.workers[chat] = true
	go s.work(chat)
}

// work sends the rows of chat, oldest first, one at a time, until there are none or the connection is gone. The chats'
// workers run side by side.
func (s *Session) work(chat string) {
	o := &s.out
	for {
		row, ok := s.next(chat)
		if !ok {
			return
		}
		down := s.try(chat, row)
		o.mu.Lock()
		delete(o.inflight, chat)
		if down {
			// The worker ends; the gate is the connection events' alone: closing it here could undo a reconnect's opening
			// that came during the attempt, and the rows would wait for the next one. A kick or the next opening starts it.
			delete(o.workers, chat)
			o.mu.Unlock()
			return
		}
		o.mu.Unlock()
	}
}

// next takes the row to send and marks it in flight, or ends the worker: none left, or the gate closed. It reads under the
// mutex, so a kick that comes as the worker ends starts another, and msg.cancel finds the row in flight or not at all.
func (s *Session) next(chat string) (row msgRow, ok bool) {
	o := &s.out
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.up {
		if chat != ownStory {
			row, ok = s.store.oldestSending(chat)
		} else if p := s.store.pendingStories(time.Now().Unix()); len(p) > 0 {
			row, ok = p[0], true
		}
	}
	if !ok {
		delete(o.workers, chat)
		return msgRow{}, false
	}
	o.inflight[chat] = row.ID
	return row, true
}

// try makes one attempt at the row next gave, and records how it went: sent, or failed, with its files let go. down says the
// connection was gone, which leaves the row sending. A row that can't be rebuilt (its file or recording is gone, a kind we
// don't send) fails, and the user may retry it; it never stays sending for good.
func (s *Session) try(chat string, row msgRow) (down bool) {
	cli := s.client()
	if chat == ownStory {
		return s.postStory(cli, row)
	}
	build := s.buildAttempt
	if s.out.build != nil {
		build = s.out.build
	}
	attempt, err := build(row)
	if err != nil {
		s.log.Warnf("send %s %s: can't be rebuilt: %v", chat, row.ID, err)
		s.store.ops <- func(t *storeTx) {
			t.sentAs(chat, row.ID, 0, stateFailed)
			s.files().released(t, row, false)
		}
		return false
	}
	if !s.store.isSending(chat, row.ID) { // a receipt says it went after all, or msg.delete took it
		s.log.Infof("send %s %s: no longer ours and sending, left alone", chat, row.ID)
		return false
	}
	done, err := attempt(cli)
	switch {
	case err == nil:
		s.store.ops <- func(t *storeTx) {
			t.sentAs(chat, row.ID, done.ts, stateSent)
			if done.media != "" { // the upload, as a received file's: a caption edit or a forward sends the message again
				t.exec("UPDATE messages SET media = ? WHERE chat = ? AND id = ?", done.media, chat, row.ID)
			}
			s.files().released(t, row, true)
		}
	case !cli.IsConnected():
		s.log.Infof("send %s %s: the connection is gone, it waits: %v", chat, row.ID, err)
		return true
	default:
		s.log.Warnf("send %s %s: %v", chat, row.ID, err)
		s.store.ops <- func(t *storeTx) { t.sentAs(chat, row.ID, 0, stateFailed) }
	}
	return false
}

// oldestSending reads the oldest of our rows in chat that is still sending, not deleted.
func (st *Store) oldestSending(chat string) (row msgRow, ok bool) {
	st.runOp(func(t *storeTx) {
		var id string
		if t.tx.QueryRow(`SELECT id FROM messages WHERE chat = ? AND out = 1 AND state = ? AND deleted = 0 ORDER BY ts, rowid LIMIT 1`,
			chat, stateSending).Scan(&id) == nil {
			row, ok = t.messageByID(chat, id)
		}
	})
	return
}

// sendingChats lists the chats that have a row of ours still sending.
func (st *Store) sendingChats() (chats []string) {
	st.runOp(func(t *storeTx) {
		rows, err := t.tx.Query(`SELECT DISTINCT chat FROM messages WHERE out = 1 AND state = ? AND deleted = 0`, stateSending)
		if err != nil {
			t.log.Warnf("sending chats: %v", err)
			return
		}
		defer rows.Close()
		for rows.Next() {
			var c string
			if rows.Scan(&c) == nil {
				chats = append(chats, c)
			}
		}
	})
	return
}

// validID says whether id is one a caller may choose for a message: 1 to 64 of 0-9 and A-Z, as WhatsApp's are.
func validID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if (c < '0' || c > '9') && (c < 'A' || c > 'Z') {
			return false
		}
	}
	return true
}

// beginSend is where every send of ours starts: the linked client and the chat's JID, and the id of the message, the one the
// caller chose or a new one. dup says our row with that id is in the chat already: a send repeated (the UI's outbox after a
// crash), so nothing is inserted or sent again; if it is still sending, its worker has it.
func (s *Session) beginSend(chat, given string) (cli *whatsmeow.Client, jid types.JID, id string, dup bool, err error) {
	if cli, jid, err = s.linked(chat); err != nil {
		return
	}
	if given == "" {
		id = cli.GenerateMessageID()
	} else if id = given; !validID(id) {
		err = &Error{"bad_request", "id is 1 to 64 of 0-9 and A-Z"}
	} else {
		_, dup = s.store.row(chat, id)
	}
	return
}

// sendReply is the answer to an accepted send: the id, and queued "true" while WhatsApp is unreachable. The outcome is the
// row's state.
func sendReply(cli *whatsmeow.Client, id string) map[string]string {
	if !cli.IsConnected() {
		return map[string]string{"id": id, "queued": "true"}
	}
	return map[string]string{"id": id}
}

// handleCancel is msg.cancel: one of our rows still sending is deleted, as delete for me does, and the chat's preview made
// right again. One being sent now (in_flight) is left to finish. The check and the delete are one step against the worker's
// next, so a row is either cancelled or sent, not both.
func (s *Session) handleCancel(req json.RawMessage) (any, error) {
	var r struct{ Chat, ID string }
	if err := json.Unmarshal(req, &r); err != nil || r.Chat == "" || r.ID == "" {
		return nil, &Error{"bad_request", "msg.cancel needs chat and id"}
	}
	s.out.mu.Lock()
	defer s.out.mu.Unlock()
	if s.out.inflight[r.Chat] == r.ID {
		return nil, &Error{"in_flight", "it is being sent now"}
	}
	m, ok := s.store.row(r.Chat, r.ID)
	switch {
	case !ok:
		return nil, &Error{"not_found", "no such message"}
	case !m.Out || m.State != stateSending:
		return nil, &Error{"not_sending", "only a message of ours that is still sending can be cancelled"}
	}
	if err := s.store.runOp(func(t *storeTx) { t.deleteMessage(r.Chat, r.ID) }); err != nil {
		return nil, err
	}
	return map[string]string{}, nil // deleteMessage took its files: the kept file, a video's poster, a voice note's OGG
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

// handleRetry is msg.retry: one of our failed rows goes back to sending, and its chat's worker sends it again with its id, in
// its place among the rows by age. The reply is as for a send. not_found; not_failed (not ours, or not failed: nothing to
// do); cannot_retry (it can't be rebuilt: it stays failed).
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
	if _, err := s.buildAttempt(m); err != nil {
		return nil, &Error{"cannot_retry", err.Error()}
	}
	var back bool
	if err := s.store.runOp(func(t *storeTx) { back = t.resend(r.Chat, r.ID) }); err != nil || !back {
		return nil, &Error{"not_failed", "only a message of ours that failed can be sent again"}
	}
	s.kick(r.Chat)
	return sendReply(cli, r.ID), nil
}
