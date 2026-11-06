package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestParsePollCreate(t *testing.T) {
	long := func(n int) string { return strings.Repeat("é", n) }
	cases := []struct {
		name, req string
		ok        bool
		want      []string
		multiple  bool
	}{
		{"trims, drops empties and duplicates", `{"chat":"c","question":" Q ","options":[" a ","","b","a"," b "]}`, true, []string{"a", "b"}, false},
		{"multiple", `{"chat":"c","question":"Q","options":["a","b"],"multiple":true}`, true, []string{"a", "b"}, true},
		{"one option left", `{"chat":"c","question":"Q","options":["a","a"," "]}`, false, nil, false},
		{"13 options", `{"chat":"c","question":"Q","options":["1","2","3","4","5","6","7","8","9","10","11","12","13"]}`, false, nil, false},
		{"12 options", `{"chat":"c","question":"Q","options":["1","2","3","4","5","6","7","8","9","10","11","12"]}`, true, nil, false},
		{"empty question", `{"chat":"c","question":"  ","options":["a","b"]}`, false, nil, false},
		{"no chat", `{"question":"Q","options":["a","b"]}`, false, nil, false},
		{"question 256", `{"chat":"c","question":"` + long(256) + `","options":["a","b"]}`, false, nil, false},
		{"question 255", `{"chat":"c","question":"` + long(255) + `","options":["a","b"]}`, true, nil, false},
		{"option 101", `{"chat":"c","question":"Q","options":["a","` + long(101) + `"]}`, false, nil, false},
		{"option 100", `{"chat":"c","question":"Q","options":["a","` + long(100) + `"]}`, true, nil, false},
		{"not JSON", `[`, false, nil, false},
	}
	for _, c := range cases {
		r, err := parsePollCreate(json.RawMessage(c.req))
		if (err == nil) != c.ok {
			t.Errorf("%s: err = %v", c.name, err)
			continue
		}
		if err != nil {
			if e, isErr := err.(*Error); !isErr || e.Code != "bad_request" {
				t.Errorf("%s: err = %v, want bad_request", c.name, err)
			}
			continue
		}
		if c.want != nil && !slices.Equal(r.Options, c.want) {
			t.Errorf("%s: options = %q, want %q", c.name, r.Options, c.want)
		}
		if r.Multiple != c.multiple {
			t.Errorf("%s: multiple = %v", c.name, r.Multiple)
		}
	}
}

func TestPollExtraReadBack(t *testing.T) {
	opts := []string{"yes", "no", "maybe"}
	if got := pollOptions(pollExtra(opts, 1)); !slices.Equal(got, opts) {
		t.Fatalf("pollOptions = %q", got)
	}
	single := msgRow{Kind: msgPoll, Extra: pollExtra(opts, 1)}
	if _, _, err := pollChoice(single, []int{0, 1}); err == nil {
		t.Error("single choice accepted two")
	}
	if _, _, err := pollChoice(single, []int{2}); err != nil {
		t.Errorf("single choice refused one: %v", err)
	}
	multi := msgRow{Kind: msgPoll, Extra: pollExtra(opts, 0)}
	names, _, err := pollChoice(multi, []int{0, 2})
	if err != nil || !slices.Equal(names, []string{"yes", "maybe"}) {
		t.Errorf("multiple: %q, %v", names, err)
	}
}
