package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseGroupCreate(t *testing.T) {
	a, b := "391111@s.whatsapp.net", "392222@s.whatsapp.net"
	req := func(name string, ps ...string) json.RawMessage {
		r, _ := json.Marshal(map[string]any{"name": name, "participants": ps})
		return r
	}
	good := map[string]struct {
		req  json.RawMessage
		name string
		want []string
	}{
		"trims":      {req("  Pals \n", a), "Pals", []string{a}},
		"100 runes":  {req(strings.Repeat("è", 100), a), strings.Repeat("è", 100), []string{a}},
		"dups drop":  {req("G", a, "", b, a), "G", []string{a, b}},
		"order kept": {req("G", b, a), "G", []string{b, a}},
	}
	for k, c := range good {
		name, ms, err := parseGroupCreate(c.req)
		if err != nil || name != c.name || len(ms) != len(c.want) {
			t.Errorf("%s: %q %v %v", k, name, ms, err)
			continue
		}
		for i, m := range ms {
			if m.String() != c.want[i] {
				t.Errorf("%s: member %d is %s", k, i, m)
			}
		}
	}
	bad := map[string]json.RawMessage{
		"empty name":   req("   ", a),
		"101 runes":    req(strings.Repeat("è", 101), a),
		"no one":       req("G"),
		"only empties": req("G", "", " "),
		"bad jid":      req("G", "1:x@s.whatsapp.net"),
		"not json":     json.RawMessage(`[1]`),
	}
	for k, r := range bad {
		if _, _, err := parseGroupCreate(r); err == nil {
			t.Errorf("%s accepted", k)
		} else if e, ok := err.(*Error); !ok || e.Code != "bad_request" {
			t.Errorf("%s: %v", k, err)
		}
	}
}
