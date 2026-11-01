package main

import (
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

func TestHubChatSet(t *testing.T) {
	s, st, _, h := hubSim(t)
	set := func(body string) {
		t.Helper()
		if _, err := s.handleChatSet([]byte(`{"chat":"` + simChats[2].jid + `",` + body + `}`)); err != nil {
			t.Fatal(err)
		}
		flush(t, st)
	}
	set(`"unread":true`) // 3 unread, no item: nothing to change in the Hub
	set(`"unread":false`)
	if c := h.take(); len(c) != 0 {
		t.Fatalf("a chat with no item: %v", c)
	}
	set(`"mute":-1`)
	msg(s, 2, "quiet")
	flush(t, st)
	item(t, only(t, h.take(), "add"), 2, "quiet", 1, false) // a muted chat's item is silent
	set(`"unread":true`)
	item(t, only(t, h.take(), "update"), 2, "quiet", 1, false) // marked unread by hand shows as one
	set(`"unread":false`)
	item(t, only(t, h.take(), "update"), 2, "quiet", 0, false)
}
