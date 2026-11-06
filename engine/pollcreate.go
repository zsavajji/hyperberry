// Creating polls (step 6.6e): poll.create sends one and writes its row, an ordinary poll row, so its votes and our own vote go as a received poll's do.
package main

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/polymorfa/hypermeow"
)

// WhatsApp's limits on a poll.
const (
	maxPollQuestion = 255
	maxPollOption   = 100
	minPollOptions  = 2
	maxPollOptions  = 12
)

type pollReq struct {
	Chat, Question string
	Options        []string
	Multiple       bool
}

func parsePollCreate(req json.RawMessage) (pollReq, error) {
	var r pollReq
	if err := json.Unmarshal(req, &r); err != nil {
		return r, &Error{"bad_request", "poll.create needs chat, question and options"}
	}
	r.Question = strings.TrimSpace(r.Question)
	var opts []string
	for _, o := range r.Options {
		// WhatsApp hashes options by name, so two the same could never be told apart.
		if o = strings.TrimSpace(o); o != "" && !slices.Contains(opts, o) {
			opts = append(opts, o)
		}
	}
	r.Options = opts
	bad := func(why string) (pollReq, error) { return r, &Error{"bad_request", "poll.create: " + why} }
	switch {
	case r.Chat == "":
		return bad("no chat")
	case r.Question == "" || utf8.RuneCountInString(r.Question) > maxPollQuestion:
		return bad("the question needs 1 to 255 characters")
	case len(opts) < minPollOptions || len(opts) > maxPollOptions:
		return bad("a poll needs 2 to 12 different options")
	}
	for _, o := range opts {
		if utf8.RuneCountInString(o) > maxPollOption {
			return bad("an option has at most 100 characters")
		}
	}
	return r, nil
}

// pollExtra is the row's extra, with the keys fillExtra writes for a poll.
func pollExtra(options []string, max int) string {
	b, _ := json.Marshal(map[string]any{"options": options, "max": max})
	return string(b)
}

func (s *Session) handlePollCreate(req json.RawMessage) (any, error) {
	r, err := parsePollCreate(req)
	if err != nil {
		return nil, err
	}
	cli, jid, err := s.linked(r.Chat)
	if err != nil {
		return nil, err
	}
	if k := chatKind(jid); k != chatUser && k != chatGroup {
		return nil, &Error{"bad_chat", "polls go to people and groups"}
	}
	max := 1
	if r.Multiple {
		max = 0 // any number, as BuildPollCreation and the rows have it
	}
	id, now := cli.GenerateMessageID(), time.Now().Unix()
	_, created := s.store.queueRow(jid, r.Chat, "", msgRow{Chat: r.Chat, ID: id, TS: now, Out: true, State: stateSending, Kind: msgPoll, Text: r.Question, Extra: pollExtra(r.Options, max)})
	s.nameNew(cli, jid, r.Chat, created)
	// hypermeow stores the poll's secret as it sends, which is what lets us vote on it.
	msg := cli.BuildPollCreation(r.Question, r.Options, max)

	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	resp, err := cli.SendMessage(ctx, jid, msg, whatsmeow.SendRequestExtra{ID: id})
	if err != nil {
		s.store.ops <- func(t *storeTx) { t.sentAs(r.Chat, id, 0, stateFailed) }
		return nil, err
	}
	// The server's clock, not the phone's: it drifts.
	s.store.ops <- func(t *storeTx) { t.sentAs(r.Chat, id, resp.Timestamp.Unix(), stateSent) }
	return map[string]string{"id": id}, nil
}
