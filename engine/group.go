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

type groupRef struct {
	JID  string `json:"jid"`
	Name string `json:"name"`
}

type contactInfo struct {
	Phone  string     `json:"phone"`
	About  string     `json:"about"`
	Groups []groupRef `json:"groups"`
}

func registerGroup(srv *Server, s *Session) {
	srv.Handle("group.info", s.handleGroupInfo)
	srv.Handle("contact.info", s.handleContactInfo)
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

// commonGroups are the groups with one of users (user parts, empty ones skipped) as a
// participant by JID, LID or phone number, by name ignoring case. Never nil.
func commonGroups(groups []*types.GroupInfo, users ...string) []groupRef {
	out := []groupRef{}
	for _, g := range groups {
	members:
		for _, p := range g.Participants {
			for _, a := range []types.JID{p.JID, p.LID, p.PhoneNumber} {
				for _, u := range users {
					if u != "" && a.User == u {
						out = append(out, groupRef{g.JID.String(), g.Name})
						break members
					}
				}
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func (s *Session) handleContactInfo(req json.RawMessage) (any, error) {
	var r struct{ Chat string }
	json.Unmarshal(req, &r)
	cli, jid, err := s.linked(r.Chat)
	if err != nil {
		return nil, err
	}
	if jid.Server != types.DefaultUserServer && jid.Server != types.HiddenUserServer {
		return nil, &Error{"bad_chat", "not a person: " + r.Chat}
	}
	ctx, cancel := context.WithTimeout(context.Background(), netTimeout)
	defer cancel()
	pn := jid
	if jid.Server == types.HiddenUserServer {
		pn, _ = cli.Store.LIDs.GetPNForLID(ctx, jid) // empty when the number isn't known
	}
	out := contactInfo{}
	ask := jid
	if !pn.IsEmpty() {
		out.Phone, ask = "+"+pn.User, pn
	}
	infos, err := cli.GetUserInfo(ctx, []types.JID{ask})
	if err != nil {
		return nil, err
	}
	for _, i := range infos {
		out.About = i.Status
	}
	// ponytail: every group's members on each open; cache them if it's slow with many groups.
	groups, err := cli.GetJoinedGroups(ctx)
	if err != nil {
		return nil, err
	}
	out.Groups = commonGroups(groups, jid.User, pn.User)
	return out, nil
}
