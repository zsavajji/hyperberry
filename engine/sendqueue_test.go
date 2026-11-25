package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow"
	waLog "github.com/polymorfa/hypermeow/util/log"
)

// waitQueued waits until n sends are in the queue: the media, sticker and story ones are queued by a goroutine.
func waitQueued(t *testing.T, s *Session, n int) {
	t.Helper()
	got := -1
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		s.queue.mu.Lock()
		got = len(s.queue.items)
		s.queue.mu.Unlock()
		if got == n {
			return
		}
	}
	t.Fatalf("%d sends queued, want %d", got, n)
}

// The queue sends in the order it was filled, one drain at a time; a send that finds the connection down again goes
// back to its place, and the drain leaves the rest for the next connect.
func TestSendQueueDrain(t *testing.T) {
	var q sendQueue
	var got []string
	up := true
	send := func(id string) queuedSend {
		return queuedSend{id, func(*whatsmeow.Client) { got = append(got, id) }}
	}
	q.add(send("a"))
	q.add(send("b"))
	q.add(send("c"))
	if !q.start() || q.start() {
		t.Fatal("start: the first should be the drain, the second not")
	}
	q.drain(nil, func() bool { return up })
	if strings.Join(got, "") != "abc" {
		t.Errorf("sent %v, want a b c", got)
	}
	if !q.start() { // the drain over: the next connect may start another
		t.Fatal("a finished drain still counts as running")
	}
	q.drain(nil, func() bool { return up })

	// "b" is down again when it is tried: it goes back before "c", and the drain stops there.
	var tries int
	q.add(send("a"))
	q.add(queuedSend{"b", func(*whatsmeow.Client) {
		if tries++; tries == 1 {
			up = false
			q.add(queuedSend{"b", func(*whatsmeow.Client) { got = append(got, "b") }}) // as sendOut does
		}
	}})
	q.add(send("c"))
	got = nil
	q.start()
	q.drain(nil, func() bool { return up })
	if strings.Join(got, "") != "a" || len(q.items) != 2 || q.items[0].id != "b" || q.items[1].id != "c" || q.running {
		t.Errorf("after the connection dropped: sent %v, queue %v, running %v", got, q.items, q.running)
	}
	up = true
	q.start()
	q.drain(nil, func() bool { return up })
	if strings.Join(got, "") != "abc" || len(q.items) != 0 {
		t.Errorf("after it came back: sent %v, queue %v", got, q.items)
	}

	// remove: found while waiting, busy while being sent, nothing otherwise.
	q.add(send("x"))
	q.add(queuedSend{"y", func(*whatsmeow.Client) {
		if f, b := q.remove("y"); f || !b {
			t.Errorf("remove of the one being sent: found %v, busy %v", f, b)
		}
		if f, b := q.remove("z"); !f || b {
			t.Errorf("remove of one waiting: found %v, busy %v", f, b)
		}
	}})
	q.add(send("z"))
	if f, b := q.remove("x"); !f || b {
		t.Errorf("remove of the first: found %v, busy %v", f, b)
	}
	if f, b := q.remove("nope"); f || b {
		t.Errorf("remove of none: found %v, busy %v", f, b)
	}
	got = nil
	q.start()
	q.drain(nil, func() bool { return true })
	if len(got) != 0 {
		t.Errorf("a removed one was sent: %v", got)
	}
}

// offlineSession is a linked session whose client isn't connected, so every send waits in the queue.
func offlineSession(t *testing.T) (*Session, *Store) {
	t.Helper()
	st, cli := historyFixture(t)
	return &Session{cli: cli, store: st, srv: st.srv, log: waLog.Noop}, st
}

// A send with no connection waits: its row stays sending, its id is held in s.sending (the flush of an earlier run's texts
// leaves it be) and it is in the queue. The same id again is the same message: nothing is inserted or queued twice.
func TestQueuedSendsAndTheirIDs(t *testing.T) {
	s, st := offlineSession(t)
	chat := "4915700000007@s.whatsapp.net"
	send := func(body string) (any, error) { return s.handleSend(json.RawMessage(body)) }

	r, err := send(`{"chat":"` + chat + `","text":"hi","id":"AB12"}`)
	if err != nil || r.(map[string]string)["id"] != "AB12" || r.(map[string]string)["queued"] != "true" {
		t.Fatalf("a text with no connection: %v, %v", r, err)
	}
	if _, held := s.sending.Load("AB12"); !held || len(s.queue.items) != 1 {
		t.Errorf("held %v, queued %d, want held and 1", held, len(s.queue.items))
	}
	if n := rows(st, "SELECT count(*) FROM messages WHERE chat = ? AND id = 'AB12' AND state = ?", chat, stateSending); n != 1 {
		t.Errorf("%d sending rows", n)
	}
	r, err = send(`{"chat":"` + chat + `","text":"hi","id":"AB12"}`)
	if err != nil || r.(map[string]string)["id"] != "AB12" {
		t.Errorf("the same id again: %v, %v", r, err)
	}
	if n := rows(st, "SELECT count(*) FROM messages WHERE chat = ?", chat); n != 1 || len(s.queue.items) != 1 {
		t.Errorf("the same id again made %d rows and %d queued", n, len(s.queue.items))
	}
	for _, id := range []string{"ab12", "A-B", strings.Repeat("A", 65), "É"} {
		if _, err := send(`{"chat":"` + chat + `","text":"x","id":"` + id + `"}`); code(err) != "bad_request" {
			t.Errorf("id %q: %v", id, err)
		}
	}

	// Another kind: a poll, by its id, and again.
	for i := 0; i < 2; i++ {
		if _, err := s.handlePollCreate(json.RawMessage(`{"chat":"` + chat + `","question":"q?","options":["a","b"],"id":"POLL1"}`)); err != nil {
			t.Fatal(err)
		}
	}
	if n := rows(st, "SELECT count(*) FROM messages WHERE chat = ? AND id = 'POLL1' AND kind = ?", chat, msgPoll); n != 1 || len(s.queue.items) != 2 {
		t.Errorf("a poll twice: %d rows, %d queued", n, len(s.queue.items))
	}

	// A voice note recorded while the engine was down: a complete WAV, no voice.start before it.
	wav, mono := stereoWAV(1)
	path := filepath.Join(s.srv.dir, "rec-down.wav")
	os.WriteFile(path, wav, 0o644)
	body := `{"chat":"` + chat + `","path":"` + path + `","id":"VN1"}`
	if _, err := s.handleVoiceSend(json.RawMessage(body)); err != nil {
		t.Fatal(err)
	}
	waitQueued(t, s, 3)
	if _, err := s.handleVoiceSend(json.RawMessage(body)); err != nil { // the WAV is gone: it is the same message
		t.Errorf("the same voice note again: %v", err)
	}
	row, ok := st.row(chat, "VN1")
	if !ok || row.Kind != msgVoice || row.State != stateSending || row.Duration != 1 || len(row.Waveform) == 0 {
		t.Errorf("voice row: %+v", row)
	}
	want, _ := encodeOpus(mono, 48000)
	if got, _ := os.ReadFile(oggPath(s.srv.dir, "VN1")); !bytes.Equal(got, want) {
		t.Error("the OGG of the voice note isn't the WAV's encoding")
	}
}

// msg.cancel: a queued row goes from the queue and the table and the chat's preview falls back to the message before it;
// one nothing sends (an earlier run's) goes too; one being sent, or not sending, or not ours, is refused.
func TestCancel(t *testing.T) {
	s, st := offlineSession(t)
	chat := "4915700000007@s.whatsapp.net"
	first := msgRow{Chat: chat, ID: "FIRST", TS: 100, Out: true, State: stateSent, Kind: msgText, Text: "first"}
	st.runOp(func(tx *storeTx) {
		tx.ensureChat(chat, chatUser)
		tx.insertMessage(first)
		tx.insertMessage(msgRow{Chat: chat, ID: "THEIRS", TS: 101, Kind: msgText, Text: "hi", State: stateSending}) // not ours
		tx.insertMessage(msgRow{Chat: chat, ID: "ORPHAN", TS: 102, Out: true, State: stateSending, Kind: msgText, Text: "orphan"})
		tx.insertMessage(msgRow{Chat: chat, ID: "BUSY", TS: 103, Out: true, State: stateSending, Kind: msgText, Text: "busy"})
		tx.insertMessage(msgRow{Chat: chat, ID: "DRAIN", TS: 104, Out: true, State: stateSending, Kind: msgText, Text: "draining"})
		tx.setLast(chat, 100, chatPreview(first), true, stateSent)
	})
	cancel := func(id string) (any, error) {
		return s.handleCancel(json.RawMessage(`{"chat":"` + chat + `","id":"` + id + `"}`))
	}
	preview := func() string {
		var p string
		st.db.QueryRow("SELECT last_preview FROM chats WHERE jid = ?", chat).Scan(&p)
		return p
	}
	for id, want := range map[string]string{"NOPE": "not_found", "FIRST": "not_sending", "THEIRS": "not_sending"} {
		if _, err := cancel(id); code(err) != want {
			t.Errorf("cancel %s: %v, want %s", id, err, want)
		}
	}
	if _, err := s.handleCancel(json.RawMessage(`{"chat":"` + chat + `"}`)); code(err) != "bad_request" {
		t.Errorf("no id: %v", err)
	}

	// Being sent: marked, not in the queue; and the one the drain has in hand.
	s.sending.Store("BUSY", true)
	s.queue.cur = "DRAIN"
	for _, id := range []string{"BUSY", "DRAIN"} {
		if _, err := cancel(id); code(err) != "in_flight" {
			t.Errorf("cancel of %s being sent: %v", id, err)
		}
		if _, ok := st.row(chat, id); !ok {
			t.Errorf("%s was deleted", id)
		}
	}
	s.queue.cur = ""

	// A queued text, then its cancel: the newest row goes and the preview is the one before.
	if _, err := s.handleSend(json.RawMessage(`{"chat":"` + chat + `","text":"later","id":"LATER"}`)); err != nil {
		t.Fatal(err)
	}
	if p := preview(); !strings.Contains(p, "later") {
		t.Fatalf("preview %q doesn't show the queued text", p)
	}
	if r, err := cancel("LATER"); err != nil || r == nil {
		t.Fatalf("cancel of a queued text: %v, %v", r, err)
	}
	if _, ok := st.row(chat, "LATER"); ok || len(s.queue.items) != 0 {
		t.Errorf("the row or the queued send is still there (%d queued)", len(s.queue.items))
	}
	if _, held := s.sending.Load("LATER"); held {
		t.Error("the id is still held")
	}
	if p, want := preview(), "You: draining"; p != want { // the newest left is DRAIN's row
		t.Errorf("preview %q, want %q", p, want)
	}

	// An orphan: sending in the table, in no queue, held by nothing.
	if _, err := cancel("ORPHAN"); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.row(chat, "ORPHAN"); ok {
		t.Error("the orphan is still there")
	}
	if _, held := s.sending.Load("ORPHAN"); held {
		t.Error("the orphan's id is still held")
	}
}
