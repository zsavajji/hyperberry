package main

import (
	"encoding/hex"
	"fmt"
	"testing"
)

func TestSimChatSet(t *testing.T) {
	s, st, c := linkedSim(t)
	chat := simChats[2].jid // 3 unread, not pinned
	set := func(body string) {
		t.Helper()
		if _, err := s.handleChatSet([]byte(`{"chat":"` + chat + `",` + body + `}`)); err != nil {
			t.Fatal(err)
		}
		if m := next(t, c, "chats"); m["jids"].([]any)[0] != chat {
			t.Errorf("%s: %v", body, m)
		}
	}
	q := "SELECT %s FROM chats WHERE jid = ?"
	set(`"pin":true`)
	eventually(t, st, 1, fmt.Sprintf(q, "pinned"), chat)
	set(`"archive":true`)
	eventually(t, st, 1, fmt.Sprintf(q, "archived"), chat)
	eventually(t, st, 0, fmt.Sprintf(q, "pinned"), chat) // archiving unpins
	set(`"archive":false`)
	eventually(t, st, 0, fmt.Sprintf(q, "archived"), chat)
	set(`"mute":-1`)
	eventually(t, st, 1, fmt.Sprintf(q, "muted_until = 1<<40"), chat)
	set(`"mute":0`)
	eventually(t, st, 0, fmt.Sprintf(q, "muted_until"), chat)
	set(`"unread":true`)
	eventually(t, st, -1, fmt.Sprintf(q, "unread"), chat)
	set(`"unread":false`)
	eventually(t, st, 0, fmt.Sprintf(q, "unread"), chat)
}

func TestSimChatSetRefuses(t *testing.T) {
	s, _, _ := linkedSim(t)
	for body, want := range map[string]string{
		`{"chat":"` + simChats[0].jid + `"}`:             "bad_request",
		`{"chat":"` + simChats[0].jid + `","mute":-5}`:   "bad_request",
		`{"chat":"` + simChats[0].jid + `","pin":"yes"}`: "bad_request",
		`{"chat":"","pin":true}`:                         "bad_chat",
	} {
		if _, err := s.handleChatSet([]byte(body)); code(err) != want {
			t.Errorf("%s: %v, want %s", body, err, want)
		}
	}
}

func TestNotifyChatSet(t *testing.T) {
	s, _, _, h := hubSim(t)
	chat := simChats[2].jid // 3 unread
	s.handleChatSet([]byte(`{"chat":"` + chat + `","unread":true}`))
	s.handleChatSet([]byte(`{"chat":"` + chat + `","unread":false}`))
	_, g := h.wait(t, 0, 1)
	if g[0] != hex.EncodeToString([]byte(chat)) {
		t.Errorf("withdrew %s", g[0])
	}
	s.handleChatSet([]byte(`{"chat":"` + chat + `","mute":-1}`))
	msg(s, 2, "quiet")
	h.wait(t, 0, 1)
}
