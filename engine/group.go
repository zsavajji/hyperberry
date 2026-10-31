package main

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/polymorfa/hypermeow/types"
)

type participant struct {
	JID   string `json:"jid"`
	Name  string `json:"name"`
	Admin bool   `json:"admin"`
	Me    bool   `json:"me"`
}

type groupInfo struct {
	Subject      string        `json:"subject"`
	Description  string        `json:"description"`
	Participants []participant `json:"participants"`
}

func registerGroup(srv *Server, s *Session) {
	srv.Handle("group.info", s.handleGroupInfo)
}

// sortParticipants puts us first, then the admins, then everyone by name, ignoring case.
func sortParticipants(p []participant) {
	sort.SliceStable(p, func(i, j int) bool {
		a, b := p[i], p[j]
		if a.Me != b.Me {
			return a.Me
		}
		if a.Admin != b.Admin {
			return a.Admin
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
}

// isUs: one of the addresses has our user part (id is our PN or our LID).
func isUs(id *types.JID, addrs ...types.JID) bool {
	if id == nil || id.User == "" {
		return false
	}
	for _, a := range addrs {
		if a.User == id.User {
			return true
		}
	}
	return false
}

func (s *Session) handleGroupInfo(req json.RawMessage) (any, error) {
	var r struct{ Chat string }
	json.Unmarshal(req, &r)
	cli, jid, err := s.linked(r.Chat)
	if err != nil {
		return nil, err
	}
	if jid.Server != types.GroupServer {
		return nil, &Error{"bad_chat", "not a group: " + r.Chat}
	}
	ctx, cancel := context.WithTimeout(context.Background(), netTimeout)
	defer cancel()
	info, err := cli.GetGroupInfo(ctx, jid)
	if err != nil {
		return nil, err
	}
	out := groupInfo{Subject: info.Name, Description: info.Topic, Participants: []participant{}}
	for _, p := range info.Participants {
		// LID first: the chat's sender keys are, so the colours match.
		key, alt := p.JID, p.PhoneNumber
		if !p.LID.IsEmpty() {
			key = p.LID
		}
		if alt.IsEmpty() || alt.User == p.JID.User {
			alt = p.LID
		}
		me := isUs(cli.Store.ID, p.JID, p.PhoneNumber) || isUs(&cli.Store.LID, p.JID, p.LID)
		out.Participants = append(out.Participants, participant{
			JID:   key.String(),
			Name:  displayName(ctx, cli, p.JID, alt),
			Admin: p.IsAdmin || p.IsSuperAdmin,
			Me:    me,
		})
	}
	sortParticipants(out.Participants)
	return out, nil
}
