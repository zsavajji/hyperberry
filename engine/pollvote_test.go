package main

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
)

const voteExtra = `{"options":["Yes","No","Maybe"],"max":0}`

func TestParseVote(t *testing.T) {
	for body, ok := range map[string]bool{
		`{"chat":"a@s.whatsapp.net","id":"X","options":[0,2]}`: true,
		`{"chat":"a@s.whatsapp.net","id":"X","options":[]}`:    true, // taking the vote back
		`{"chat":"a@s.whatsapp.net","id":"X"}`:                 true, // so is no list at all
		`{"chat":"a@s.whatsapp.net","id":"X","options":null}`:  true,
		`{"chat":"a@s.whatsapp.net","id":"X","options":"1"}`:   false,
		`{"chat":"a@s.whatsapp.net","id":"X","options":[1.5]}`: false,
		`{"id":"X","options":[0]}`:                             false,
		`{"chat":"a@s.whatsapp.net","options":[0]}`:            false,
		`not json`: false,
	} {
		_, err := parseVote(json.RawMessage(body))
		if (err == nil) != ok || (err != nil && code(err) != "bad_request") {
			t.Errorf("%s: %v", body, err)
		}
	}
}

func TestPollChoice(t *testing.T) {
	poll := msgRow{Kind: msgPoll, Extra: voteExtra}
	single := msgRow{Kind: msgPoll, Extra: `{"options":["Yes","No","Maybe"],"max":1}`}
	for _, c := range []struct {
		what   string
		row    msgRow
		chosen []int
		names  []string
		idx    string // "" and no error: taking the vote back
		fails  bool
	}{
		{"one", poll, []int{1}, []string{"No"}, "1", false},
		{"two, in any order", poll, []int{2, 0}, []string{"Yes", "Maybe"}, "0,2", false},
		{"all", poll, []int{0, 1, 2}, []string{"Yes", "No", "Maybe"}, "0,1,2", false},
		{"none: taking it back", poll, []int{}, []string{}, "", false},
		{"none, in a single-choice poll", single, []int{}, []string{}, "", false},
		{"one, in a single-choice poll", single, []int{2}, []string{"Maybe"}, "2", false},
		{"two, in a single-choice poll", single, []int{0, 1}, nil, "", true},
		{"past the end", poll, []int{3}, nil, "", true},
		{"below zero", poll, []int{-1}, nil, "", true},
		{"twice", poll, []int{1, 1}, nil, "", true},
		{"a good one and a bad one", poll, []int{0, 7}, nil, "", true},
		{"not a poll", msgRow{Kind: msgText, Extra: voteExtra}, []int{0}, nil, "", true},
		{"a poll with no options", msgRow{Kind: msgPoll}, []int{0}, nil, "", true},
		{"taking back a vote on a poll with no options", msgRow{Kind: msgPoll}, []int{}, []string{}, "", false},
	} {
		names, idx, err := pollChoice(c.row, c.chosen)
		if c.fails {
			if code(err) != "bad_request" {
				t.Errorf("%s: %v, want bad_request", c.what, err)
			}
			continue
		}
		if err != nil || idx != c.idx || !slices.Equal(names, c.names) {
			t.Errorf("%s: %q %q %v, want %q %q", c.what, names, idx, err, c.names, c.idx)
		}
	}
}

func TestPollInfo(t *testing.T) {
	bob := types.NewJID("200000000000002", types.HiddenUserServer)
	group := types.NewJID("120363000000000001", types.GroupServer)
	for _, c := range []struct {
		what  string
		chat  types.JID
		out   bool
		group bool
	}{
		{"theirs, 1:1", bob, false, false},
		{"ours, 1:1", bob, true, false},
		{"theirs, group", group, false, true},
		{"ours, group", group, true, true},
	} {
		in := pollInfo(c.chat, msgRow{ID: "P1", Out: c.out}, bob)
		if in.ID != "P1" || in.Chat != c.chat || in.Sender != bob || in.IsFromMe != c.out || in.IsGroup != c.group {
			t.Errorf("%s: %+v", c.what, in)
		}
	}
}

// The vote is made as the other end will read it: whatever form the poll's secret is stored
// under, our vote decrypts when it is signed as SendMessage sends it (our LID in a 1:1 chat or a
// LID group, our number in a number group), and not when it is signed as the other.
func TestPollVoteRoundTrip(t *testing.T) {
	_, cli := historyFixture(t)
	ctx := context.Background()
	ourPN := cli.Store.GetJID().ToNonAD()
	ourLID := types.NewJID("100000000000009", types.HiddenUserServer)
	cli.Store.LID = ourLID
	bobPN := types.NewJID("4915700000002", types.DefaultUserServer)
	bobLID := types.NewJID("200000000000002", types.HiddenUserServer)
	if err := cli.Store.LIDs.PutLIDMapping(ctx, bobLID, bobPN); err != nil {
		t.Fatal(err)
	}
	lidGroup := types.NewJID("120363000000000001", types.GroupServer)
	pnGroup := types.NewJID("120363000000000002", types.GroupServer)

	for i, c := range []struct {
		what     string
		chat     types.JID
		out      bool
		sender   string    // the row's sender: the chat key of the author
		storedAs types.JID // who the poll's secret is stored under
		signedAs types.JID // who SendMessage signs our vote as
	}{
		{"theirs, 1:1", bobLID, false, "", bobLID, ourLID},
		{"theirs, 1:1, secret stored under their number", bobLID, false, "", bobPN, ourLID},
		{"ours, 1:1", bobLID, true, "", ourLID, ourLID},
		{"theirs, LID group", lidGroup, false, bobLID.String(), bobLID, ourLID},
		{"ours, LID group", lidGroup, true, "", ourLID, ourLID},
		{"theirs, number group", pnGroup, false, bobLID.String(), bobPN, ourPN},
		{"ours, number group", pnGroup, true, "", ourPN, ourPN},
	} {
		id := "POLL" + string(rune('A'+i))
		if err := cli.Store.MsgSecrets.PutMessageSecret(ctx, c.chat, c.storedAs, id, []byte(strings.Repeat(string(rune('a'+i)), 32))); err != nil {
			t.Fatal(err)
		}
		row := msgRow{Chat: c.chat.String(), ID: id, Out: c.out, Sender: c.sender, Kind: msgPoll, Extra: voteExtra}
		sender, err := pollSender(ctx, cli, c.chat, row)
		if err != nil {
			t.Errorf("%s: %v", c.what, err)
			continue
		}
		names, idx, _ := pollChoice(row, []int{2, 0})
		msg, err := cli.BuildPollVote(ctx, pollInfo(c.chat, row, sender), names)
		if err != nil {
			t.Errorf("%s: %v", c.what, err)
			continue
		}
		k := msg.GetPollUpdateMessage().GetPollCreationMessageKey()
		participant := ""
		if chatKind(c.chat) == chatGroup {
			participant = c.storedAs.String()
		}
		if k.GetID() != id || k.GetRemoteJID() != c.chat.String() || k.GetFromMe() != c.out || k.GetParticipant() != participant {
			t.Errorf("%s: key %v", c.what, k)
		}

		arrives := func(as types.JID) *events.Message {
			return &events.Message{Message: msg, Info: types.MessageInfo{
				MessageSource: types.MessageSource{Chat: c.chat, Sender: as, IsFromMe: true}}}
		}
		v, err := cli.DecryptPollVote(ctx, arrives(c.signedAs))
		if err != nil {
			t.Errorf("%s: %v", c.what, err)
			continue
		}
		if got := voteIndices(pollNames, v.GetSelectedOptions()); got != idx {
			t.Errorf("%s: the vote reads %q, want %q", c.what, got, idx)
		}
		other := ourPN
		if c.signedAs == ourPN {
			other = ourLID
		}
		if _, err := cli.DecryptPollVote(ctx, arrives(other)); err == nil {
			t.Errorf("%s: the vote also decrypts as %s", c.what, other)
		}

		// Taking the vote back is a vote for nothing.
		msg, err = cli.BuildPollVote(ctx, pollInfo(c.chat, row, sender), nil)
		if err != nil {
			t.Errorf("%s, taking it back: %v", c.what, err)
			continue
		}
		if v, err = cli.DecryptPollVote(ctx, arrives(c.signedAs)); err != nil || len(v.GetSelectedOptions()) != 0 {
			t.Errorf("%s, taking it back: %v %v", c.what, v, err)
		}
	}
}

// With no secret to ask for, the sender is the first form of the author, and BuildPollVote says
// the secret is missing. A group message of unknown sender has no author at all.
func TestPollSenderWithoutSecret(t *testing.T) {
	_, cli := historyFixture(t)
	ctx := context.Background()
	group := types.NewJID("120363000000000001", types.GroupServer)
	ourPN := cli.Store.GetJID().ToNonAD()
	row := msgRow{Chat: group.String(), ID: "P1", Out: true, Kind: msgPoll, Extra: voteExtra}

	if s, _ := pollSender(ctx, cli, group, row); s != ourPN {
		t.Errorf("ours, no LID known: %s, want %s", s, ourPN)
	}
	ourLID := types.NewJID("100000000000009", types.HiddenUserServer)
	cli.Store.LID = ourLID
	s, _ := pollSender(ctx, cli, group, row)
	if s != ourLID {
		t.Errorf("ours, no secret: %s, want %s", s, ourLID)
	}
	if _, err := cli.BuildPollVote(ctx, pollInfo(group, row, s), []string{"Yes"}); err == nil {
		t.Error("a vote with no secret: want an error")
	}
	if _, err := pollSender(ctx, cli, group, msgRow{ID: "P2"}); code(err) != "not_found" {
		t.Errorf("a group message of unknown sender: %v", err)
	}
}

func TestPollVoteRequest(t *testing.T) {
	s := newUnlinked(t)
	call := func(body string) error {
		_, err := s.handlePollVote(json.RawMessage(body))
		return err
	}
	for body, want := range map[string]string{
		`{"chat":"a@s.whatsapp.net","id":"X"}`: "unlinked", // no options: a take-back, so it gets as far
		`{"id":"X","options":[0]}`:             "bad_request",
		`{}`:                                   "bad_request",
		`{"chat":"a@s.whatsapp.net","id":"X","options":[0]}`:     "unlinked",
		`{"chat":"a@s.whatsapp.net","id":"X","options":[]}`:      "unlinked",
		`{"chat":"a@s.whatsapp.net","id":"X","options":[0,"1"]}`: "bad_request",
	} {
		if c := code(call(body)); c != want {
			t.Errorf("%s: %s, want %s", body, c, want)
		}
	}

	st, cli := historyFixture(t)
	ctx := context.Background()
	cli.Store.LID = types.NewJID("100000000000009", types.HiddenUserServer)
	s = &Session{cli: cli, store: st}
	g := types.NewJID("120363000000000001", types.GroupServer)
	gk := g.String()
	st.runOp(func(tx *storeTx) {
		tx.ensureChat(gk, chatGroup)
		tx.insertMessage(msgRow{Chat: gk, ID: "text", TS: 5, Kind: msgText, Text: "x"})
		tx.insertMessage(msgRow{Chat: gk, ID: "single", TS: 6, Out: true, State: stateSent, Kind: msgPoll, Text: "Q?",
			Extra: `{"options":["Yes","No","Maybe"],"max":1}`})
		tx.insertMessage(msgRow{Chat: gk, ID: "nosecret", TS: 7, Out: true, State: stateSent, Kind: msgPoll, Text: "Q?", Extra: voteExtra})
	})
	if err := cli.Store.MsgSecrets.PutMessageSecret(ctx, g, cli.Store.GetLID(), "single", make([]byte, 32)); err != nil {
		t.Fatal(err)
	}
	vote := func(id, options string) error {
		return call(`{"chat":"` + gk + `","id":"` + id + `","options":` + options + `}`)
	}
	for _, c := range []struct{ what, id, options, want string }{
		{"no such message", "nope", "[0]", "not_found"},
		{"not a poll", "text", "[0]", "bad_request"},
		{"past the end", "single", "[3]", "bad_request"},
		{"twice", "single", "[1,1]", "bad_request"},
		{"more than the poll allows", "single", "[0,1]", "bad_request"},
		{"no secret", "nosecret", "[0]", "not_found"},
		// These are good: they get as far as sending, which fails with no connection.
		{"a choice", "single", "[1]", "?"},
		{"taking it back", "single", "[]", "?"},
	} {
		if got := code(vote(c.id, c.options)); got != c.want {
			t.Errorf("%s: %s, want %s", c.what, got, c.want)
		}
	}
	if err := vote("nosecret", "[0]"); !strings.Contains(err.Error(), "secret") {
		t.Errorf("no secret: %v", err)
	}
	// Nothing is written as if the send had gone.
	time.Sleep(2 * batchTime)
	if n := rows(st, "SELECT count(*) FROM poll_votes"); n != 0 {
		t.Errorf("%d votes after the failed sends", n)
	}
}
