package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/polymorfa/hypermeow/types"
)

// The sim's reactions (step 5.11): msg.react writes ours as the real one does, sim.react is
// someone reacting, and the seed has a few already.

// simFamilyMembers are chat 1's members, as its hand-written talk and group.info have them.
var simFamilyMembers = []string{"Mum", "Dad", "Luca", "Giorgia", "Marco"}

func (s *Sim) registerSimReact() {
	s.srv.Handle("msg.react", s.handleReact)
	s.srv.Handle("sim.react", s.handleSimReact)
}

// handleReact is msg.react: the same rows as the real one writes, after a round trip, with
// nothing sent.
func (s *Sim) handleReact(req json.RawMessage) (any, error) {
	r, err := parseReact(req)
	if err != nil {
		return nil, err
	}
	if _, err := s.chat(r.Chat); err != nil {
		return nil, err
	}
	if _, err := s.store.reactable(r.Chat, r.ID); err != nil {
		return nil, err
	}
	time.Sleep(simDelay)
	now := time.Now().Unix()
	s.store.ops <- func(t *storeTx) { t.reactedByUs(r.Chat, r.ID, r.Emoji, now) }
	return nil, nil
}

// handleSimReact is sim.react: chat n reacts to our newest message in it (to its newest if we
// have none), 👍 by default. In a group a member does, the next one on each call. emoji "" takes
// back the newest reaction that is theirs.
func (s *Sim) handleSimReact(req json.RawMessage) (any, error) {
	var r struct {
		N     int
		Emoji *string // nil when absent: "" is a removal
	}
	json.Unmarshal(req, &r)
	if err := s.ready(); err != nil {
		return nil, err
	}
	if r.N < 0 || r.N >= len(simChats) {
		return nil, &Error{"bad_request", fmt.Sprintf("n is 0 to %d", len(simChats)-1)}
	}
	emoji := "👍"
	if r.Emoji != nil {
		emoji = *r.Emoji
	}
	found, now := false, time.Now().Unix()
	if err := s.store.runOp(func(t *storeTx) { found = simReact(t, r.N, emoji, now) }); err != nil {
		return nil, err
	}
	if !found {
		return nil, &Error{"not_found", "the chat has no message to react to"}
	}
	return nil, nil
}

// simReact writes chat n's reaction as a live incoming one, preview included. It reports
// whether the chat had a message.
func simReact(t *storeTx, n int, emoji string, ts int64) bool {
	c := simChats[n]
	var id string
	if t.tx.QueryRow(`SELECT id FROM messages WHERE chat = ? AND deleted = 0 AND kind != ?
ORDER BY out DESC, ts DESC, rowid DESC LIMIT 1`, c.jid, msgService).Scan(&id) != nil {
		return false
	}
	var sender, name string
	if emoji == "" {
		if t.tx.QueryRow(`SELECT sender, COALESCE(sender_name, '') FROM reactions
WHERE chat = ? AND msg = ? AND sender != '' ORDER BY ts DESC, rowid DESC LIMIT 1`, c.jid, id).Scan(&sender, &name) == nil {
			t.setReaction(c.jid, id, sender, name, "", ts)
		}
		return true
	}
	j, _ := types.ParseJID(c.jid)
	if j.Server == types.GroupServer {
		names, had := simPeople, 0
		if n == 1 {
			names = simFamilyMembers
		}
		t.tx.QueryRow("SELECT count(*) FROM reactions WHERE chat = ? AND msg = ? AND sender != ''", c.jid, id).Scan(&had)
		name = names[had%len(names)]
		sender = simMember(name)
	} else {
		sender, name = c.jid, c.name // in a 1:1 chat the chat's key stands for them
		if name == "" {
			name = "+" + j.User
		}
	}
	t.setReaction(c.jid, id, sender, name, emoji, ts)
	t.reactPreview(c.jid, ts, firstWord(name), emoji, false)
	return true
}

// seedReactions is the reactions in the sim's history: two Family messages (one ours, one with
// our own reaction beside a member's) and Anna's reply to our newest. Their ids are the history's.
func seedReactions(t *storeTx, now time.Time) {
	ts, family, anna := now.Add(-time.Minute).Unix(), simChats[1].jid, simChats[0]
	for _, r := range []struct{ chat, msg, who, emoji string }{
		{family, "SIMH1.2", "Mum", "❤️"}, // "Me too 👍", ours
		{family, "SIMH1.2", "Dad", "👍"},
		{family, "SIMH1.18", "Luca", "😂"}, // Marco's "I can bring some 🍷"
		{family, "SIMH1.18", "", "👍"},
		{anna.jid, "SIMSEED0", anna.name, "❤️"},
	} {
		sender, name := "", r.who
		switch {
		case r.chat == anna.jid:
			sender = anna.jid
		case r.who != "":
			sender = simMember(r.who)
		}
		t.setReaction(r.chat, r.msg, sender, name, r.emoji, ts)
	}
}
