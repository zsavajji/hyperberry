package main

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// linkedSim links a test sim and waits for the seed, including chat 0's 600 rows.
func linkedSim(t *testing.T) (*Sim, *Store, *conn) {
	t.Helper()
	s, st, c := newTestSim(t)
	s.handleQR(nil)
	next(t, c, "link")
	s.handleLink(nil)
	next(t, c, "link") // linking
	next(t, c, "link") // ready
	next(t, c, "chats")
	eventually(t, st, 600, "SELECT count(*) FROM messages WHERE chat = ?", simChats[0].jid)
	return s, st, c
}

func rows(st *Store, q string, args ...any) int {
	var n int
	st.db.QueryRow(q, args...).Scan(&n)
	return n
}

func TestSimHistory(t *testing.T) {
	_, st, _ := linkedSim(t)
	g := simChats[1].jid
	eventually(t, st, 1, "SELECT count(*) FROM messages WHERE chat = ? AND kind = ? AND text = 'Luca added Marco'", g, msgService)
	for _, k := range []int{msgPhoto, msgVoice, msgDoc, msgSticker} {
		if n := rows(st, "SELECT count(*) FROM messages WHERE chat = ? AND kind = ?", g, k); n < 1 {
			t.Errorf("kind %d: %d rows in the group", k, n)
		}
	}
	for _, col := range []string{"edited = 1", "deleted = 1", "quote_id IN (SELECT id FROM messages WHERE chat = ?1)"} {
		if rows(st, "SELECT count(*) FROM messages WHERE chat = ?1 AND "+col, g) < 1 {
			t.Errorf("no %s in the group", col)
		}
	}
	if n := rows(st, "SELECT count(DISTINCT sender) FROM messages WHERE chat = ?", g); n != 5 {
		t.Errorf("%d senders, want 5", n)
	}
	for i, c := range simChats {
		var id string
		st.db.QueryRow("SELECT id FROM messages WHERE chat = ? ORDER BY ts DESC LIMIT 1", c.jid).Scan(&id)
		if id != "SIMSEED"+strconv.Itoa(i) {
			t.Errorf("chat %d: newest is %s", i, id)
		}
		if c.unread > 0 {
			if n := rows(st, "SELECT count(*) FROM (SELECT out FROM messages WHERE chat = ? ORDER BY ts DESC LIMIT ?) WHERE out = 0", c.jid, c.unread); n != c.unread {
				t.Errorf("chat %d: %d of the newest %d are theirs", i, n, c.unread)
			}
		}
	}
	now := time.Now()
	for i := 0; i < 3; i++ {
		if !reflect.DeepEqual(simHistory(i, now), simHistory(i, now)) {
			t.Errorf("history %d changes between calls", i)
		}
	}
}

func TestSimSend(t *testing.T) {
	s, st, c := linkedSim(t)
	chat := simChats[5].jid
	state := func(id string) int {
		return rows(st, "SELECT state FROM messages WHERE chat = ? AND id = ?", chat, id)
	}
	// The send blocks for the round trip, so it runs aside while this goroutine reads the events.
	sent := make(chan error, 1)
	go func() {
		r, err := s.handleSend([]byte(`{"chat":"` + chat + `","text":"hello"}`))
		if err == nil && !strings.HasPrefix(r.(map[string]string)["id"], "SIMOUT") {
			err = fmt.Errorf("reply %v", r)
		}
		sent <- err
	}()
	var seen []int
	for len(seen) < 4 {
		m := next(t, c, "msgs")
		if m["chat"] != chat {
			continue
		}
		for _, id := range m["ids"].([]any) {
			if strings.HasPrefix(id.(string), "SIMOUT") {
				if st := state(id.(string)); len(seen) == 0 || seen[len(seen)-1] != st {
					seen = append(seen, st)
				}
			}
		}
	}
	if err := <-sent; err != nil {
		t.Fatalf("send: %v", err)
	}
	if !reflect.DeepEqual(seen, []int{0, 1, 2, 3}) {
		t.Errorf("states %v, want [0 1 2 3]", seen)
	}

	if _, err := s.handleSend([]byte(`{"chat":"` + chat + `","text":"Fail now"}`)); err == nil {
		t.Error("a text starting with fail: want an error")
	}
	eventually(t, st, 1, "SELECT count(*) FROM messages WHERE chat = ? AND text = 'Fail now' AND state = ?", chat, stateFailed)

	g := simChats[1].jid
	if _, err := s.handleSend([]byte(`{"chat":"` + g + `","text":"yes","quote":"SIMH1.0"}`)); err != nil {
		t.Fatal(err)
	}
	eventually(t, st, 1, "SELECT count(*) FROM messages WHERE chat = ? AND text = 'yes' AND quote_id = 'SIMH1.0' AND quote_preview != ''", g)

	for req, code := range map[string]string{
		`{"chat":"` + chat + `","text":"  "}`: "bad_request",
		`{"chat":"","text":"hi"}`:             "bad_chat",
	} {
		if _, err := s.handleSend([]byte(req)); err == nil || err.(*Error).Code != code {
			t.Errorf("%s: %v, want %s", req, err, code)
		}
	}
	s.handleRemoved(nil)
	if _, err := s.handleSend([]byte(`{"chat":"` + chat + `","text":"hi"}`)); err == nil || err.(*Error).Code != "unlinked" {
		t.Errorf("unlinked sim: %v", err)
	}
}

func TestSimGroupInfo(t *testing.T) {
	s, _, _ := linkedSim(t)
	res, err := s.handleGroupInfo([]byte(`{"chat":"` + simChats[1].jid + `"}`))
	if err != nil {
		t.Fatal(err)
	}
	g := res.(groupInfo)
	p := g.Participants
	if len(p) != 6 || !p[0].Me || p[0].JID != simMe || g.Subject != "Family 🏠" {
		t.Fatalf("participants %+v", g)
	}
	if !p[1].Admin || !p[2].Admin || p[3].Admin {
		t.Errorf("admins first: %+v", p)
	}
	if p[1].Name != "Dad" || p[2].Name != "Mum" {
		t.Errorf("admins by name: %+v", p[1:3])
	}
	if _, err := s.handleGroupInfo([]byte(`{"chat":"` + simChats[0].jid + `"}`)); code(err) != "bad_chat" {
		t.Errorf("1:1: %v", err)
	}
}

func TestSimRead(t *testing.T) {
	s, st, _ := linkedSim(t)
	g := simChats[1].jid
	eventually(t, st, 12, "SELECT unread FROM chats WHERE jid = ?", g)
	if _, err := s.handleRead([]byte(`{"chat":"` + g + `"}`)); err != nil {
		t.Fatal(err)
	}
	eventually(t, st, 0, "SELECT unread FROM chats WHERE jid = ?", g)
}

func TestSimPresenceTyping(t *testing.T) {
	s, _, c := linkedSim(t)
	for _, tc := range []struct {
		i      int
		online bool
	}{{0, true}, {3, false}} {
		if _, err := s.handleOpen([]byte(`{"chat":"` + simChats[tc.i].jid + `"}`)); err != nil {
			t.Fatal(err)
		}
		m := next(t, c, "presence")
		want := int64(0)
		if !tc.online {
			want = time.Now().Add(-time.Hour).Unix()
		}
		if d := int64(m["lastSeen"].(float64)) - want; m["chat"] != simChats[tc.i].jid || m["online"] != tc.online || d < -60 || d > 60 {
			t.Errorf("presence of chat %d: %v", tc.i, m)
		}
	}
	if _, err := s.handleOpen([]byte(`{"chat":"` + simChats[1].jid + `"}`)); err != nil {
		t.Errorf("chat.open of a group: %v", err)
	}
	for n, who := range map[int]string{1: "Mum", 0: ""} {
		if _, err := s.handleTyping([]byte(`{"n":` + strconv.Itoa(n) + `}`)); err != nil {
			t.Fatal(err)
		}
		if m := next(t, c, "typing"); m["on"] != true || m["who"] != who {
			t.Errorf("typing %d: %v", n, m)
		}
		if m := next(t, c, "typing"); m["on"] != false {
			t.Errorf("typing %d ends: %v", n, m)
		}
	}
}

func TestSimDelete(t *testing.T) {
	s, st, c := linkedSim(t)
	del := func(chat, id string, everyone bool) error {
		_, err := s.handleDelete([]byte(fmt.Sprintf(`{"chat":%q,"ids":[%q],"everyone":%t}`, chat, id, everyone)))
		return err
	}
	chat := simChats[2].jid // Marco: theirs, newest SIMSEED2
	if err := del(chat, "SIMSEED2", true); err == nil || err.(*Error).Code != "not_ours" {
		t.Errorf("everyone, theirs: %v, want not_ours", err)
	}
	var prev, text string
	var out bool
	st.db.QueryRow("SELECT text, out FROM messages WHERE chat = ? AND id != 'SIMSEED2' ORDER BY ts DESC LIMIT 1", chat).Scan(&text, &out)
	prev = chatPreview(msgRow{Text: text, Out: out})
	if err := del(chat, "SIMSEED2", false); err != nil {
		t.Fatal(err)
	}
	for ok := false; !ok; {
		m := next(t, c, "msgs")
		for _, id := range m["ids"].([]any) {
			ok = ok || (m["chat"] == chat && id == "SIMSEED2")
		}
	}
	eventually(t, st, 0, "SELECT count(*) FROM messages WHERE chat = ? AND id = 'SIMSEED2'", chat)
	eventually(t, st, 1, "SELECT count(*) FROM chats WHERE jid = ? AND last_preview = ?", chat, prev)

	mine := simChats[5].jid
	r, err := s.handleSend([]byte(`{"chat":"` + mine + `","text":"to delete"}`))
	if err != nil {
		t.Fatal(err)
	}
	id := r.(map[string]string)["id"]
	eventually(t, st, 1, "SELECT count(*) FROM messages WHERE chat = ? AND id = ? AND state >= ?", mine, id, stateSent)
	if err := del(mine, id, true); err != nil {
		t.Fatal(err)
	}
	eventually(t, st, 1, "SELECT count(*) FROM messages WHERE chat = ? AND id = ? AND deleted = 1 AND text IS NULL", mine, id)
}

func TestSimContacts(t *testing.T) {
	s, st, _ := linkedSim(t)
	res, err := s.handleContacts(nil)
	if err != nil {
		t.Fatal(err)
	}
	book := res.([]contact)
	var stranger, anna bool
	for i, c := range book {
		stranger = stranger || c.Name == "Beatrice Conti"
		anna = anna || c.JID == simChats[0].jid
		if strings.HasSuffix(c.JID, "@g.us") {
			t.Errorf("group in the book: %+v", c)
		}
		if i > 0 && strings.ToLower(book[i-1].Name) > strings.ToLower(c.Name) {
			t.Errorf("not sorted: %q before %q", book[i-1].Name, c.Name)
		}
	}
	if !stranger || !anna {
		t.Errorf("want a contact with no chat and one with a chat: %+v", book)
	}

	var who contact
	for _, c := range book {
		if c.Name == "Beatrice Conti" {
			who = c
		}
	}
	if rows(st, "SELECT count(*) FROM chats WHERE jid = ?", who.JID) != 0 {
		t.Fatal("the stranger already has a chat")
	}
	if _, err := s.handleSend([]byte(`{"chat":"` + who.JID + `","text":"Hello Bea"}`)); err != nil {
		t.Fatal(err)
	}
	eventually(t, st, 1, "SELECT count(*) FROM chats WHERE jid = ? AND name = 'Beatrice Conti' AND last_preview = 'You: Hello Bea'", who.JID)
}
