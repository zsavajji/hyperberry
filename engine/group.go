package main

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/types"
)

type participant struct {
	JID   string `json:"jid"`
	Name  string `json:"name"`
	Admin bool   `json:"admin"`
	Me    bool   `json:"me"`
	Photo string `json:"photo,omitempty"` // the small picture's path: the member's chat's, else the one fetched for group pages; ours for our own row
}

type groupInfo struct {
	Subject      string        `json:"subject"`
	Description  string        `json:"description"`
	Participants []participant `json:"participants"`
	Picture      string        `json:"picture"`    // the full-size picture's path, "" when none
	MembersAdd   bool          `json:"membersAdd"` // every member may add people, not only admins
}

type groupRef struct {
	JID  string `json:"jid"`
	Name string `json:"name"`
}

type contactInfo struct {
	Phone    string        `json:"phone"`
	About    string        `json:"about"`
	Groups   []groupRef    `json:"groups"`
	Blocked  bool          `json:"blocked"`
	Picture  string        `json:"picture"`            // as groupInfo's
	Business *businessInfo `json:"business,omitempty"` // a WhatsApp Business account's (business.go)
}

func registerGroup(srv *Server, s *Session) {
	srv.Handle("group.info", s.handleGroupInfo)
	srv.Handle("group.participants", s.handleGroupParticipants)
	srv.Handle("group.create", s.handleGroupCreate)
	srv.Handle("contact.info", s.handleContactInfo)
}

// participantReq is group.participants' request, its action checked.
func participantReq(req json.RawMessage) (chat, jid string, action whatsmeow.ParticipantChange, err error) {
	var r struct{ Chat, JID, Action string }
	json.Unmarshal(req, &r)
	switch a := whatsmeow.ParticipantChange(r.Action); a {
	case whatsmeow.ParticipantChangeAdd, whatsmeow.ParticipantChangeRemove,
		whatsmeow.ParticipantChangePromote, whatsmeow.ParticipantChangeDemote:
		return r.Chat, r.JID, a, nil
	}
	return "", "", "", &Error{"bad_request", "action is add, remove, promote or demote: " + r.Action}
}

// handleGroupParticipants adds, removes, promotes or demotes one member. WhatsApp answers per
// member: a refusal (403 their privacy, 409 already in, and the like) is the error "refused"
// with the number as its message.
func (s *Session) handleGroupParticipants(req json.RawMessage) (any, error) {
	chat, who, action, err := participantReq(req)
	if err != nil {
		return nil, err
	}
	cli, jid, err := s.linked(chat)
	if err != nil {
		return nil, err
	}
	member, err := types.ParseJID(who)
	if jid.Server != types.GroupServer || err != nil || member.IsEmpty() {
		return nil, &Error{"bad_chat", "not a group and a member: " + chat + ", " + who}
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	res, err := cli.UpdateGroupParticipants(ctx, jid, []types.JID{member}, action)
	if err != nil {
		return nil, err
	}
	for _, p := range res {
		if p.Error != 0 {
			return nil, &Error{"refused", strconv.Itoa(p.Error)}
		}
	}
	return nil, nil
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
	out := groupInfo{Subject: info.Name, Description: info.Topic, Participants: []participant{},
		MembersAdd: info.MemberAddMode == types.GroupMemberAddModeAllMember}
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
	s.photos.members(out.Participants) // fills Photo, queues the fetches and returns at once
	out.Picture = s.photos.full(jid)
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
	if !isPerson(jid) {
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
		if i.VerifiedName != nil { // a business: its profile too, a failure only logs
			p, err := cli.GetBusinessProfile(ctx, ask)
			if err != nil {
				s.log.Warnf("contact.info: business profile: %v", err)
			}
			out.Business = businessFrom(i.VerifiedName, p)
		}
	}
	// ponytail: every group's members on each open; cache them if it's slow with many groups.
	groups, err := cli.GetJoinedGroups(ctx)
	if err != nil {
		return nil, err
	}
	out.Groups = commonGroups(groups, jid.User, pn.User)
	bl, err := cli.GetBlocklist(ctx, "")
	if err != nil {
		return nil, err
	}
	out.Blocked = isBlocked(bl, jid.User, pn.User)
	out.Picture = s.photos.full(jid)
	return out, nil
}

// maxGroupName is WhatsApp's own limit. hypermeow's doc still says 25; the apps take 100, and a
// name the server won't take comes back as a 406, which handleGroupCreate maps.
const maxGroupName = 100

// parseGroupCreate reads group.create's request: the trimmed name and the members, in order,
// without empties or repeats.
func parseGroupCreate(req json.RawMessage) (name string, members []types.JID, err error) {
	var r struct {
		Name         string
		Participants []string
	}
	if err := json.Unmarshal(req, &r); err != nil {
		return "", nil, &Error{"bad_request", "group.create needs name and participants"}
	}
	name = strings.TrimSpace(r.Name)
	if n := utf8.RuneCountInString(name); n < 1 || n > maxGroupName {
		return "", nil, &Error{"bad_request", "a group name is 1 to 100 characters"}
	}
	seen := map[types.JID]bool{}
	for _, p := range r.Participants {
		if p = strings.TrimSpace(p); p == "" {
			continue
		}
		j, err := types.ParseJID(p)
		if err != nil || j.IsEmpty() {
			return "", nil, &Error{"bad_request", "not a JID: " + p}
		}
		if !seen[j] {
			seen[j] = true
			members = append(members, j)
		}
	}
	if len(members) == 0 {
		return "", nil, &Error{"bad_request", "a group needs someone in it"}
	}
	return name, members, nil
}

// handleGroupCreate makes the group, then its chat row, so the UI finds the chat when it opens
// it. refused counts the people whose privacy settings kept them out.
func (s *Session) handleGroupCreate(req json.RawMessage) (any, error) {
	name, members, err := parseGroupCreate(req)
	if err != nil {
		return nil, err
	}
	cli, err := s.ready()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	info, err := cli.CreateGroup(ctx, whatsmeow.ReqCreateGroup{Name: name, Participants: members})
	if errors.Is(err, whatsmeow.ErrIQNotAcceptable) {
		return nil, &Error{"name_too_long", err.Error()}
	}
	if err != nil {
		return nil, err
	}
	refused := 0
	for _, p := range info.Participants {
		if p.Error != 0 {
			refused++
		}
	}
	s.store.madeChat(info.JID.String(), chatGroup, name)
	return map[string]any{"jid": info.JID.String(), "refused": refused}, nil
}
