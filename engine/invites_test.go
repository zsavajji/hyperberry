package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/polymorfa/hypermeow"
)

func TestParseInvite(t *testing.T) {
	for _, c := range []struct{ in, kind, code string }{
		{"https://chat.whatsapp.com/AbC123", "group", "AbC123"},
		{"http://chat.whatsapp.com/AbC123", "group", "AbC123"},
		{"chat.whatsapp.com/AbC123", "group", "AbC123"},
		{"https://www.chat.whatsapp.com/AbC123", "group", "AbC123"},
		{"https://chat.whatsapp.com/invite/AbC123", "group", "AbC123"},
		{"https://chat.whatsapp.com/AbC123?mode=r_c", "group", "AbC123"},
		{"https://whatsapp.com/channel/0029Va", "channel", "0029Va"},
		{"  https://chat.whatsapp.com/AbC123 \n", "group", "AbC123"},
		{"https://example.com/chat.whatsapp.com/AbC123", "", ""},
		{"https://whatsapp.com/", "", ""},
		{"see https://chat.whatsapp.com/AbC123 now", "", ""},
	} {
		kind, code, ok := parseInvite(c.in)
		if kind != c.kind || code != c.code || ok != (c.kind != "") {
			t.Errorf("%q: got %q %q %v", c.in, kind, code, ok)
		}
	}
}

func TestInviteOf(t *testing.T) {
	good := `{"group":"120363@g.us","code":"ABC","expires":2000}`
	for _, c := range []struct {
		name, extra string
		ok          bool
		expires     int64
	}{
		{"good", good, true, 2000},
		{"expired", `{"group":"120363@g.us","code":"ABC","expires":900}`, false, 0},
		{"no expiry", `{"group":"120363@g.us","code":"ABC"}`, true, 0},
		{"no code", `{"group":"120363@g.us"}`, false, 0},
		{"bad json", `{`, false, 0},
	} {
		g, code, exp, err := inviteOf(msgRow{Extra: c.extra}, 1000)
		if (err == nil) != c.ok {
			t.Errorf("%s: err %v", c.name, err)
		} else if c.ok && (g.String() != "120363@g.us" || code != "ABC" || exp != c.expires) {
			t.Errorf("%s: got %v %q %d", c.name, g, code, exp)
		}
		var e *Error
		if err != nil && (!errors.As(err, &e) || e.Code != "invite_invalid") {
			t.Errorf("%s: wrong error %v", c.name, err)
		}
	}
}

func TestInviteErr(t *testing.T) {
	var e *Error
	if err := inviteErr(fmt.Errorf("x: %w", whatsmeow.ErrInviteLinkRevoked)); !errors.As(err, &e) || e.Code != "invite_invalid" {
		t.Errorf("revoked: %v", err)
	}
	other := errors.New("timeout")
	if inviteErr(other) != other {
		t.Error("another error changed")
	}
}
