package main

import (
	"path/filepath"
	"testing"
	"time"

	waLog "github.com/polymorfa/hypermeow/util/log"
)

func newTestSim(t *testing.T) (*Sim, *Store, *conn) {
	t.Helper()
	codes, delay, ticks, typing := simCodes, simDelay, simTicks, simTyping
	simCodes, simDelay = []time.Duration{50 * time.Millisecond, 50 * time.Millisecond}, 10*time.Millisecond
	// 100 ms apart keeps each state change in its own store batch (batches close after 50 ms).
	simTicks, simTyping = [3]time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 300 * time.Millisecond}, 100*time.Millisecond
	t.Cleanup(func() { simCodes, simDelay, simTicks, simTyping = codes, delay, ticks, typing })
	ts := newTestServer(t)
	db, err := openChats(filepath.Join(t.TempDir(), "chats.db"))
	if err != nil {
		t.Fatal(err)
	}
	st := newStore(db, ts.Server, waLog.Noop)
	t.Cleanup(func() { st.close(); db.Close() })
	return newSim(ts.Server, st, waLog.Noop), st, ts.login(t)
}

// next reads until event ev and returns it; other events and replies are skipped.
func next(t *testing.T, c *conn, ev string) map[string]any {
	t.Helper()
	for i := 0; i < 500; i++ {
		if m := c.read(t); m["ev"] == ev {
			return m
		}
	}
	t.Fatalf("no %s event", ev)
	return nil
}

// eventually polls chats.db until q gives want.
func eventually(t *testing.T, st *Store, want int, q string, args ...any) {
	t.Helper()
	n := -1
	for end := time.Now().Add(2 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if st.db.QueryRow(q, args...).Scan(&n); n == want {
			return
		}
	}
	t.Fatalf("%s: %d, want %d", q, n, want)
}

func TestSimLinkMessageRemove(t *testing.T) {
	s, st, c := newTestSim(t)
	if s.hello().(map[string]any)["state"] != linkUnlinked {
		t.Fatal("a new simulated account isn't unlinked")
	}
	if _, err := s.handleLink(nil); err == nil {
		t.Error("sim.link with no code showing: want an error")
	}
	s.handleQR(nil)
	if m := next(t, c, "link"); m["state"] != linkQR || m["qr"] == "" {
		t.Fatalf("after link.qr: %v", m)
	}
	r, err := s.handleCode([]byte(`{"phone":"+39 333 123 4567"}`))
	if err != nil || r.(map[string]string)["code"] == "" {
		t.Fatalf("link.code: %v %v", r, err)
	}
	if m := next(t, c, "link"); m["state"] != linkUnlinked {
		t.Errorf("link.code goes through unlinked: %v", m)
	}
	if m := next(t, c, "link"); m["state"] != linkCode {
		t.Errorf("then code: %v", m)
	}

	s.handleLink(nil)
	if m := next(t, c, "link"); m["state"] != linkLinking {
		t.Errorf("after sim.link: %v", m)
	}
	if m := next(t, c, "link"); m["state"] != linkReady || m["conn"] != "ready" {
		t.Errorf("then: %v", m)
	}
	next(t, c, "chats")
	eventually(t, st, len(simChats), "SELECT count(*) FROM chats")
	eventually(t, st, 1, "SELECT count(*) FROM chats WHERE archived = 1")
	eventually(t, st, 1, "SELECT count(*) FROM messages WHERE deleted = 1 AND id LIKE 'SIMSEED%'")

	// A restarted engine finds the account linked.
	if newSim(s.srv, st, waLog.Noop).hello().(map[string]any)["state"] != linkReady {
		t.Error("restart: want ready")
	}

	if _, err := s.handleMsg([]byte(`{"n":5,"text":"hello"}`)); err != nil {
		t.Fatal(err)
	}
	if m := next(t, c, "chats"); len(m["jids"].([]any)) != 1 || m["jids"].([]any)[0] != simChats[5].jid {
		t.Errorf("sim.msg: %v", m)
	}
	eventually(t, st, 1, "SELECT unread FROM chats WHERE jid = ?", simChats[5].jid)
	eventually(t, st, 1, "SELECT count(*) FROM chats WHERE jid = ? AND last_preview = 'hello'", simChats[5].jid)
	if _, err := s.handleMsg([]byte(`{"n":999}`)); err == nil {
		t.Error("sim.msg past the chats: want an error")
	}

	s.handleRemoved(nil)
	if m := next(t, c, "link"); m["state"] != linkUnlinked {
		t.Errorf("after sim.removed: %v", m)
	}
	eventually(t, st, 0, "SELECT count(*) FROM chats")
	eventually(t, st, 0, "SELECT count(*) FROM messages")
}

func TestSimCodesRunOut(t *testing.T) {
	s, _, c := newTestSim(t)
	s.handleQR(nil)
	first := next(t, c, "link")
	if second := next(t, c, "link"); second["qr"] == first["qr"] {
		t.Errorf("the code didn't change: %v", second)
	}
	if m := next(t, c, "link"); m["state"] != linkUnlinked {
		t.Errorf("after the last code: %v", m)
	}
	if _, err := s.handleQR(nil); err != nil {
		t.Errorf("a new run after the old one ran out: %v", err)
	}
}
