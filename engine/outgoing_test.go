package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow"
	waLog "github.com/polymorfa/hypermeow/util/log"
)

// offlineSession is a linked session whose client isn't connected, with its gate shut: every send waits as a row sending. A
// test that opens the gate gets attempts that find the connection down, unless it brings its own (fakeSends).
func offlineSession(t *testing.T) (*Session, *Store) {
	t.Helper()
	st, cli := historyFixture(t)
	return &Session{cli: cli, store: st, srv: st.srv, log: waLog.Noop}, st
}

// fakeSends makes the worker's attempts ones that call send with the row, in place of the upload and the wire.
func fakeSends(s *Session, send func(row msgRow) error) {
	s.out.build = func(row msgRow) (attempt, error) {
		return func(*whatsmeow.Client) (sendDone, error) { return sendDone{ts: 5}, send(row) }, nil
	}
}

// idle waits until no worker runs.
func idle(t *testing.T, s *Session) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		s.out.mu.Lock()
		n := len(s.out.workers)
		s.out.mu.Unlock()
		if n == 0 {
			return
		}
	}
	t.Fatal("a worker is still running")
}

func insertOurs(st *Store, chat string, rows ...msgRow) {
	st.runOp(func(tx *storeTx) {
		tx.ensureChat(chat, chatUser)
		for _, m := range rows {
			m.Chat, m.Out, m.State = chat, true, stateSending // a text unless it says
			tx.insertMessage(m)
		}
	})
}

// A send with no connection waits: its row stays sending, and the reply says queued. The same id again is the same
// message: nothing is inserted twice.
func TestQueuedSendsAndTheirIDs(t *testing.T) {
	s, st := offlineSession(t)
	chat := "4915700000007@s.whatsapp.net"
	send := func(body string) (any, error) { return s.handleSend(json.RawMessage(body)) }

	r, err := send(`{"chat":"` + chat + `","text":"hi","id":"AB12"}`)
	if err != nil || r.(map[string]string)["id"] != "AB12" || r.(map[string]string)["queued"] != "true" {
		t.Fatalf("a text with no connection: %v, %v", r, err)
	}
	if n := rows(st, "SELECT count(*) FROM messages WHERE chat = ? AND id = 'AB12' AND state = ?", chat, stateSending); n != 1 {
		t.Errorf("%d sending rows", n)
	}
	r, err = send(`{"chat":"` + chat + `","text":"hi","id":"AB12"}`)
	if err != nil || r.(map[string]string)["id"] != "AB12" || r.(map[string]string)["queued"] != "true" {
		t.Errorf("the same id again: %v, %v", r, err)
	}
	if n := rows(st, "SELECT count(*) FROM messages WHERE chat = ?", chat); n != 1 {
		t.Errorf("the same id again made %d rows", n)
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
	if n := rows(st, "SELECT count(*) FROM messages WHERE chat = ? AND id = 'POLL1' AND kind = ?", chat, msgPoll); n != 1 {
		t.Errorf("a poll twice: %d rows", n)
	}

	// A voice note recorded while the engine was down: a complete WAV, no voice.start before it.
	wav, mono := stereoWAV(1)
	path := filepath.Join(s.srv.dir, "rec-down.wav")
	os.WriteFile(path, wav, 0o644)
	body := `{"chat":"` + chat + `","path":"` + path + `","id":"VN1"}`
	if _, err := s.handleVoiceSend(json.RawMessage(body)); err != nil {
		t.Fatal(err)
	}
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

// A chat's worker sends its rows oldest first, one at a time, and another chat's worker doesn't wait for it.
func TestWorkersSendOldestFirstAndChatsApart(t *testing.T) {
	s, st := offlineSession(t)
	a, b := retryChat, "4915700000008@s.whatsapp.net"
	insertOurs(st, a, msgRow{ID: "A2", TS: 20}, msgRow{ID: "A1", TS: 10}, msgRow{ID: "A3", TS: 30})
	insertOurs(st, b, msgRow{ID: "B1", TS: 40})
	var mu sync.Mutex
	var order []string
	hold := make(chan struct{})
	fakeSends(s, func(row msgRow) error {
		if row.ID == "A1" {
			<-hold // the first chat's first row is a long upload
		}
		mu.Lock()
		order = append(order, row.ID)
		mu.Unlock()
		return nil
	})
	s.openGate(s.client())
	eventually(t, st, 1, "SELECT count(*) FROM messages WHERE chat = ? AND id = 'B1' AND state = ?", b, stateSent)
	if n := rows(st, "SELECT count(*) FROM messages WHERE chat = ? AND state = ?", a, stateSent); n != 0 {
		t.Errorf("%d of the first chat's rows went while its first was being sent", n)
	}
	close(hold)
	eventually(t, st, 3, "SELECT count(*) FROM messages WHERE chat = ? AND state = ?", a, stateSent)
	idle(t, s)
	if got := strings.Join(order, " "); got != "B1 A1 A2 A3" {
		t.Errorf("sent in the order %s, want B1 A1 A2 A3", got)
	}
	if n := rows(st, "SELECT count(*) FROM messages WHERE ts = 5 AND state = ?", stateSent); n != 4 {
		t.Errorf("%d rows took the server's time", n)
	}
}

// With the connection down under an attempt the row stays sending and its worker ends, the rows behind it untouched; the next
// gate opening starts them again, in order. A shut gate starts none.
func TestWorkerStopsWhenDownAndResumes(t *testing.T) {
	s, st := offlineSession(t)
	chat := retryChat
	insertOurs(st, chat, msgRow{ID: "A1", TS: 10}, msgRow{ID: "A2", TS: 20})
	var tries []string
	fail := true
	fakeSends(s, func(row msgRow) error {
		tries = append(tries, row.ID)
		if fail {
			return errors.New("not connected") // the session's client never connects: the worker takes it as down
		}
		return nil
	})
	s.kick(chat) // the gate is shut
	time.Sleep(50 * time.Millisecond)
	if len(tries) != 0 {
		t.Fatalf("a worker ran with the gate shut: %v", tries)
	}
	s.openGate(s.client())
	idle(t, s)
	if strings.Join(tries, " ") != "A1" || rows(st, "SELECT count(*) FROM messages WHERE chat = ? AND state = ?", chat, stateSending) != 2 {
		t.Fatalf("tries %v, sending %d: want A1 tried once, both sending", tries, rows(st, "SELECT count(*) FROM messages WHERE chat = ? AND state = ?", chat, stateSending))
	}
	s.out.shut() // events.Disconnected: the gate goes shut with the connection, not with the attempt
	s.kick(chat)
	time.Sleep(50 * time.Millisecond)
	if len(tries) != 1 {
		t.Errorf("a worker ran after the connection went: %v", tries)
	}
	fail = false
	s.openGate(s.client())
	eventually(t, st, 2, "SELECT count(*) FROM messages WHERE chat = ? AND state = ?", chat, stateSent)
	idle(t, s)
	if strings.Join(tries, " ") != "A1 A1 A2" {
		t.Errorf("tries %v, want A1 again, then A2", tries)
	}
}

// A row that can't be rebuilt fails, and its poster goes, and the rows behind it are sent as before; one whose attempt finds
// the connection down stays sending.
func TestBuildErrorFailsTheRow(t *testing.T) {
	s, st := offlineSession(t)
	chat := retryChat
	insertOurs(st, chat,
		msgRow{ID: "P1", TS: 1, Kind: msgVideo, Path: "/nonexistent/v.mp4"},
		msgRow{ID: "L1", TS: 2, Kind: msgLocation, Text: "here"},
		msgRow{ID: "T1", TS: 3, Text: "text"})
	poster := writeTo(posterPath(s.srv.dir, "P1"))
	s.openGate(s.client())
	idle(t, s)
	for id, want := range map[string]int{"P1": stateFailed, "L1": stateFailed, "T1": stateSending} {
		if row, _ := st.row(chat, id); row.State != want {
			t.Errorf("%s is %d, want %d", id, row.State, want)
		}
	}
	waitGone(t, poster)
}

// msg.cancel: a row still sending goes and the chat's preview falls back to the message before it; one nothing sends (an
// earlier run's) goes too; one being sent, or not sending, or not ours, is refused.
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

	// Being sent: the worker has it in flight.
	s.out.inflight = map[string]string{chat: "BUSY"}
	if _, err := cancel("BUSY"); code(err) != "in_flight" {
		t.Errorf("cancel of BUSY being sent: %v", err)
	}
	if _, ok := st.row(chat, "BUSY"); !ok {
		t.Error("BUSY was deleted")
	}
	s.out.inflight = nil

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
	if _, ok := st.row(chat, "LATER"); ok {
		t.Error("the row is still there")
	}
	if p, want := preview(), "You: busy"; p != want { // the newest left is BUSY's row
		t.Errorf("preview %q, want %q", p, want)
	}

	// An orphan: sending in the table, in no worker's hands.
	if _, err := cancel("ORPHAN"); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.row(chat, "ORPHAN"); ok {
		t.Error("the orphan is still there")
	}
}

// A cancel that comes while the worker is sending the row is refused, and the row ends sent; after that it is not sending.
func TestCancelInFlight(t *testing.T) {
	s, st := offlineSession(t)
	chat := retryChat
	insertOurs(st, chat, msgRow{ID: "A1", TS: 10, Text: "one"}, msgRow{ID: "A2", TS: 20, Text: "two"})
	started, release := make(chan struct{}), make(chan struct{})
	fakeSends(s, func(row msgRow) error {
		if row.ID == "A1" {
			close(started)
			<-release
		}
		return nil
	})
	cancel := func(id string) error {
		_, err := s.handleCancel(json.RawMessage(`{"chat":"` + chat + `","id":"` + id + `"}`))
		return err
	}
	s.openGate(s.client())
	<-started
	if err := cancel("A1"); code(err) != "in_flight" {
		t.Errorf("cancel of the row being sent: %v", err)
	}
	if err := cancel("A2"); err != nil { // the one behind it isn't in flight
		t.Errorf("cancel of the row behind: %v", err)
	}
	close(release)
	eventually(t, st, 1, "SELECT count(*) FROM messages WHERE chat = ? AND id = 'A1' AND state = ?", chat, stateSent)
	idle(t, s)
	if err := cancel("A1"); code(err) != "not_sending" {
		t.Errorf("cancel after it went: %v", err)
	}
	if _, ok := st.row(chat, "A2"); ok {
		t.Error("the cancelled row was sent anyway")
	}
}
