package main

import (
	"encoding/json"
	"sync"

	"github.com/polymorfa/hypermeow"
)

// Sends that find WhatsApp unreachable wait here and go at the next connect: any kind of message, not the texts alone. The
// row stays sending in chats.db meanwhile. Each send path is one attempt, run by sendOut, which queues it again, to run
// with the client of the next connect, when the connection is down before or after the try; any other error fails the row.

// queuedSend is one send waiting for the connection: send makes the attempt again.
type queuedSend struct {
	id   string
	send func(cli *whatsmeow.Client)
}

// sendQueue holds them in the order they were made, and says who is draining them.
// The queue lives in the engine's memory, but the rows stay sending in chats.db, so at the next run's connect sendQueued
// sends them again from their rows (retry.go). ponytail: a send made while a drain runs goes at once, and may overtake the
// ones still queued.
type sendQueue struct {
	mu      sync.Mutex
	items   []queuedSend
	cur     string // the id the drain is sending now
	running bool   // a drain is going
}

func (q *sendQueue) add(it queuedSend) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if it.id == q.cur { // the drain's own send, down again: back at the front, in its place
		q.items, q.cur = append([]queuedSend{it}, q.items...), ""
		return
	}
	q.items = append(q.items, it)
}

// start says whether the caller is the drain: one runs at a time.
func (q *sendQueue) start() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.running {
		return false
	}
	q.running = true
	return true
}

// next takes the first one to send; with none, the drain is over.
func (q *sendQueue) next() (queuedSend, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		q.running, q.cur = false, ""
		return queuedSend{}, false
	}
	it := q.items[0]
	q.items, q.cur = q.items[1:], it.id
	return it, true
}

// drain, once start said so, sends them oldest first, one at a time, while up says the connection is; the rest waits
// for the next connect.
func (q *sendQueue) drain(cli *whatsmeow.Client, up func() bool) {
	for up() {
		it, ok := q.next()
		if !ok {
			return
		}
		it.send(cli)
	}
	q.mu.Lock()
	q.running, q.cur = false, ""
	q.mu.Unlock()
}

// remove takes id out of the queue: found if it was there, busy if the drain is sending it now.
func (q *sendQueue) remove(id string) (found, busy bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.cur == id {
		return false, true
	}
	for i, it := range q.items {
		if it.id == id {
			q.items = append(q.items[:i:i], q.items[i+1:]...)
			return true, false
		}
	}
	return false, false
}

// drop forgets them all: the account is gone, and so are their rows.
func (q *sendQueue) drop() {
	q.mu.Lock()
	q.items = nil
	q.mu.Unlock()
}

// queueSend keeps send for the next connect. Its id stays in s.sending, so the text flush never takes it.
func (s *Session) queueSend(cli *whatsmeow.Client, id string, send func(*whatsmeow.Client)) {
	s.sending.Store(id, true)
	s.queue.add(queuedSend{id, send})
	s.log.Infof("queued send: %s waits for the connection", id)
	if cli.IsConnected() { // a connect raced the caller's check
		go s.drainQueued(cli)
	}
}

// holdIfDown queues send when cli isn't connected, and says so.
func (s *Session) holdIfDown(cli *whatsmeow.Client, id string, send func(*whatsmeow.Client)) bool {
	if cli.IsConnected() {
		return false
	}
	s.queueSend(cli, id, send)
	return true
}

// sendOut makes one attempt at sending our row id of chat: attempt sends it and records it as sent, or says why it didn't.
// A connection that is down before the try, or after its error, queues it instead (queued), the row staying sending; any
// other error fails the row and comes back. Right before the try the row is read again: one that a receipt moved on, or
// that msg.cancel took back, is not sent.
func (s *Session) sendOut(cli *whatsmeow.Client, chat, id string, attempt func(*whatsmeow.Client) error) (queued bool, err error) {
	again := func(c *whatsmeow.Client) {
		if _, err := s.sendOut(c, chat, id, attempt); err != nil {
			s.log.Warnf("queued send %s %s: %v", chat, id, err)
		}
	}
	if s.holdIfDown(cli, id, again) {
		return true, nil
	}
	if !s.store.isSending(chat, id) { // a receipt says it went after all (a resent poll would split the votes), or msg.cancel took it
		s.log.Infof("send %s %s: no longer ours and sending, left alone", chat, id)
		s.sending.Delete(id)
		return false, nil
	}
	if err = attempt(cli); err != nil && s.holdIfDown(cli, id, again) {
		return true, nil
	}
	s.sending.Delete(id)
	if err != nil {
		s.store.ops <- func(t *storeTx) { t.sentAs(chat, id, 0, stateFailed) }
	}
	return false, err
}

// drainQueued runs at each connect: the rows an earlier run left sending, of every kind, and the stories it left pending
// (they are older), then the queue.
func (s *Session) drainQueued(cli *whatsmeow.Client) {
	if !s.queue.start() {
		return
	}
	s.sendQueued(cli)
	s.sendQueuedStories(cli)
	s.queue.drain(cli, cli.IsConnected)
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

// outID is the id of a send of ours: the one the caller chose, or a new one. dup says our row with that id is in the chat
// already: a send repeated (the UI's outbox after a crash), so nothing is inserted or sent again; if it is still sending
// it is in the queue, or goes with the next connect's text flush. Otherwise the id is marked as being sent from here,
// before its row, so msg.cancel and the flush leave it be; sendOut ends the mark, and a path that fails first must too.
func (s *Session) outID(cli *whatsmeow.Client, chat, given string) (id string, dup bool, err error) {
	if given == "" {
		id = cli.GenerateMessageID()
	} else if id = given; !validID(id) {
		return "", false, &Error{"bad_request", "id is 1 to 64 of 0-9 and A-Z"}
	} else if _, dup = s.store.row(chat, id); dup {
		return id, true, nil
	}
	s.sending.Store(id, true)
	return id, false, nil
}

// handleCancel is msg.cancel: one of our rows still sending is taken out of the queue and deleted, as delete for me does,
// and the chat's preview made right again. One being sent now (in_flight) is left to finish. A row nothing sends, that an
// earlier run left, is deleted too: the text flush finds it gone.
func (s *Session) handleCancel(req json.RawMessage) (any, error) {
	var r struct{ Chat, ID string }
	if err := json.Unmarshal(req, &r); err != nil || r.Chat == "" || r.ID == "" {
		return nil, &Error{"bad_request", "msg.cancel needs chat and id"}
	}
	m, ok := s.store.row(r.Chat, r.ID)
	switch {
	case !ok:
		return nil, &Error{"not_found", "no such message"}
	case !m.Out || m.State != stateSending:
		return nil, &Error{"not_sending", "only a message of ours that is still sending can be cancelled"}
	}
	found, busy := s.queue.remove(r.ID)
	if !found && !busy { // an attempt that runs, or nothing: the mark says which, and holds the row while it goes
		_, busy = s.sending.LoadOrStore(r.ID, true)
	}
	if busy {
		return nil, &Error{"in_flight", "it is being sent now"}
	}
	defer s.sending.Delete(r.ID)
	if err := s.store.runOp(func(t *storeTx) { t.deleteMessage(r.Chat, r.ID) }); err != nil {
		return nil, err
	}
	return map[string]string{}, nil // deleteMessage took its files: the copy, a video's poster, a voice note's OGG
}
