package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// The sim's stories, for the list of who viewed ours: sim.story is a text story of ours, sim.storyview
// someone viewing the newest one, and story.delete takes one back as the viewer's trash can asks.
// ponytail: text stories only and no story.post, story.get or story.seen; add them when the sim has to
// show photos, videos or other people's stories.

func (s *Sim) registerSimStories() {
	s.srv.Handle("sim.story", s.handleSimStory)
	s.srv.Handle("sim.storyview", s.handleSimStoryView)
	s.srv.Handle("story.delete", s.handleSimStoryDelete)
}

// handleSimStory is sim.story: a text story of ours, born seen, as story.post writes one.
func (s *Sim) handleSimStory(req json.RawMessage) (any, error) {
	var r struct{ Text string }
	json.Unmarshal(req, &r)
	if err := s.ready(); err != nil {
		return nil, err
	}
	now := time.Now()
	if strings.TrimSpace(r.Text) == "" {
		r.Text = "A story at " + now.Format("15:04:05")
	}
	extra, _ := json.Marshal(map[string]any{"bg": uint32(0xFF075E54), "fg": uint32(0xFFFFFFFF)})
	row := msgRow{Chat: ownStory, ID: simID(now), TS: now.Unix(), Kind: msgText, Text: strings.TrimSpace(r.Text), Extra: string(extra)}
	if err := s.store.runOp(func(t *storeTx) { t.insertStory(row, true) }); err != nil {
		return nil, err
	}
	return map[string]string{"id": row.ID}, nil
}

// handleSimStoryView is sim.storyview: chat n's person views our newest story. A group can't.
func (s *Sim) handleSimStoryView(req json.RawMessage) (any, error) {
	var r struct{ N int }
	json.Unmarshal(req, &r)
	if err := s.ready(); err != nil {
		return nil, err
	}
	if r.N < 0 || r.N >= len(simChats) || strings.HasSuffix(simChats[r.N].jid, "@g.us") {
		return nil, &Error{"bad_request", fmt.Sprintf("n is a person's chat, 0 to %d", len(simChats)-1)}
	}
	c, found := simChats[r.N], false
	err := s.store.runOp(func(t *storeTx) {
		var id string
		if t.tx.QueryRow("SELECT id FROM stories WHERE poster = ? ORDER BY ts DESC, rowid DESC LIMIT 1", ownStory).Scan(&id) == nil {
			found = true
			t.addStoryView(id, c.jid, c.name, time.Now().Unix())
		}
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, &Error{"not_found", "no story of ours: sim.story first"}
	}
	return nil, nil
}

// handleSimStoryDelete is story.delete: the row goes with nothing sent, and storyop says so.
func (s *Sim) handleSimStoryDelete(req json.RawMessage) (any, error) {
	var r struct{ ID string }
	if err := json.Unmarshal(req, &r); err != nil || r.ID == "" {
		return nil, &Error{"bad_request", "story.delete needs id"}
	}
	if err := s.store.runOp(func(t *storeTx) { t.deleteStory(ownStory, r.ID) }); err != nil {
		return nil, err
	}
	s.srv.Emit("storyop", map[string]any{"cmd": "delete", "id": r.ID, "ok": true})
	return nil, nil
}
