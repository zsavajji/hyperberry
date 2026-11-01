package main

import (
	"testing"
)

// msgsNamed waits for a msgs event of chat that names id.
func msgsNamed(t *testing.T, c *conn, chat, id string) {
	t.Helper()
	for i := 0; i < 100; i++ {
		m := next(t, c, "msgs")
		if m["chat"] != chat {
			continue
		}
		for _, v := range m["ids"].([]any) {
			if v == id {
				return
			}
		}
	}
	t.Fatalf("no msgs event naming %s in %s", id, chat)
}

func TestSimSeedsReactions(t *testing.T) {
	_, st, _ := linkedSim(t)
	family, anna := simChats[1].jid, simChats[0].jid
	eventually(t, st, 5, "SELECT count(*) FROM reactions")
	eventually(t, st, 2, "SELECT count(DISTINCT msg) FROM reactions WHERE chat = ?", family)
	eventually(t, st, 1, "SELECT count(*) FROM reactions WHERE chat = ? AND sender = ''", family)
	eventually(t, st, 1, "SELECT count(*) FROM reactions WHERE chat = ? AND msg = 'SIMSEED0' AND sender = ? AND sender_name = 'Anna Rossi'", anna, anna)
	// Each one is on a message the history has, and a member's key is the history's.
	eventually(t, st, 5, "SELECT count(*) FROM reactions r JOIN messages m ON m.chat = r.chat AND m.id = r.msg")
	eventually(t, st, 1, "SELECT count(*) FROM reactions WHERE chat = ? AND sender = ?", family, simMember("Mum"))
}

func TestSimMsgReact(t *testing.T) {
	s, st, c := linkedSim(t)
	anna := simChats[0].jid
	react := func(emoji string) error {
		_, err := s.handleReact([]byte(`{"chat":"` + anna + `","id":"SIMSEED0","emoji":"` + emoji + `"}`))
		return err
	}
	if err := react("🔥"); err != nil {
		t.Fatal(err)
	}
	msgsNamed(t, c, anna, "SIMSEED0")
	eventually(t, st, 1, "SELECT count(*) FROM reactions WHERE chat = ? AND msg = 'SIMSEED0' AND sender = '' AND sender_name IS NULL AND emoji = '🔥'", anna)
	eventually(t, st, 1, "SELECT count(*) FROM chats WHERE jid = ? AND last_preview = 'You reacted 🔥' AND last_out = 1", anna)
	// Another emoji replaces it; Anna's own reaction beside it stays.
	if err := react("🙏"); err != nil {
		t.Fatal(err)
	}
	eventually(t, st, 1, "SELECT count(*) FROM reactions WHERE chat = ? AND msg = 'SIMSEED0' AND sender = '' AND emoji = '🙏'", anna)
	eventually(t, st, 2, "SELECT count(*) FROM reactions WHERE chat = ? AND msg = 'SIMSEED0'", anna)
	// "" takes ours back and leaves the preview.
	if err := react(""); err != nil {
		t.Fatal(err)
	}
	msgsNamed(t, c, anna, "SIMSEED0")
	eventually(t, st, 0, "SELECT count(*) FROM reactions WHERE chat = ? AND msg = 'SIMSEED0' AND sender = ''", anna)
	eventually(t, st, 1, "SELECT count(*) FROM chats WHERE jid = ? AND last_preview = 'You reacted 🙏'", anna)

	for body, want := range map[string]string{
		`{"chat":"` + anna + `"}`:                 "bad_request",
		`{"id":"SIMSEED0"}`:                       "bad_request",
		`{"chat":"` + anna + `","id":"nope"}`:     "not_found",
		`{"chat":"not a jid@@@","id":"SIMSEED0"}`: "bad_chat",
	} {
		if _, err := s.handleReact([]byte(body)); code(err) != want {
			t.Errorf("%s: %v, want %s", body, err, want)
		}
	}
	// An unlinked sim says so.
	fresh, _, _ := newTestSim(t)
	if _, err := fresh.handleReact([]byte(`{"chat":"` + anna + `","id":"SIMSEED0"}`)); code(err) != "unlinked" {
		t.Errorf("unlinked: %v", err)
	}
}

func TestSimReact(t *testing.T) {
	s, st, c := linkedSim(t)
	family, anna := simChats[1].jid, simChats[0].jid
	react := func(body string) error {
		_, err := s.handleSimReact([]byte(body))
		return err
	}
	// Our newest message in Family: "Thanks Luca" (SIMH1.13); the newest of all is Mum's.
	if err := react(`{"n":1}`); err != nil {
		t.Fatal(err)
	}
	msgsNamed(t, c, family, "SIMH1.13")
	eventually(t, st, 1, "SELECT count(*) FROM reactions WHERE chat = ? AND msg = 'SIMH1.13' AND emoji = '👍' AND sender != ''", family)
	var sender, name string
	st.db.QueryRow("SELECT sender, sender_name FROM reactions WHERE chat = ? AND msg = 'SIMH1.13'", family).Scan(&sender, &name)
	if sender != simMember(name) || name != simFamilyMembers[0] {
		t.Errorf("member %q keyed %q", name, sender)
	}
	eventually(t, st, 1, "SELECT count(*) FROM chats WHERE jid = ? AND last_preview = 'Mum reacted 👍' AND last_out = 0", family)
	// The next call is the next member; "" takes the newest back.
	react(`{"n":1,"emoji":"😂"}`)
	eventually(t, st, 2, "SELECT count(DISTINCT sender) FROM reactions WHERE chat = ? AND msg = 'SIMH1.13'", family)
	eventually(t, st, 1, "SELECT count(*) FROM chats WHERE jid = ? AND last_preview = 'Dad reacted 😂'", family)
	react(`{"n":1,"emoji":""}`)
	eventually(t, st, 1, "SELECT count(*) FROM reactions WHERE chat = ? AND msg = 'SIMH1.13'", family)
	eventually(t, st, 1, "SELECT count(*) FROM reactions WHERE chat = ? AND msg = 'SIMH1.13' AND emoji = '👍'", family)
	eventually(t, st, 1, "SELECT count(*) FROM chats WHERE jid = ? AND last_preview = 'Dad reacted 😂'", family)

	// A 1:1 chat: the chat reacts, to our newest message, replacing its seeded reaction.
	react(`{"n":0,"emoji":"🙏"}`)
	msgsNamed(t, c, anna, "SIMSEED0")
	eventually(t, st, 1, "SELECT count(*) FROM reactions WHERE chat = ? AND msg = 'SIMSEED0' AND sender = ? AND emoji = '🙏'", anna, anna)
	eventually(t, st, 1, "SELECT count(*) FROM chats WHERE jid = ? AND last_preview = 'Anna reacted 🙏'", anna)

	// A chat where we said nothing: its newest message.
	quiet := simChats[3].jid
	st.runOp(func(tx *storeTx) { tx.exec("DELETE FROM messages WHERE chat = ? AND out = 1", quiet) })
	react(`{"n":3}`)
	eventually(t, st, 1, `SELECT count(*) FROM reactions WHERE chat = ? AND msg = (
SELECT id FROM messages WHERE chat = ? ORDER BY ts DESC, rowid DESC LIMIT 1)`, quiet, quiet)

	if err := react(`{"n":999}`); code(err) != "bad_request" {
		t.Errorf("past the chats: %v", err)
	}
	fresh, _, _ := newTestSim(t)
	if _, err := fresh.handleSimReact([]byte(`{"n":0}`)); code(err) != "unlinked" {
		t.Errorf("unlinked: %v", err)
	}
}
