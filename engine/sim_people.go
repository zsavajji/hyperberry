package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/polymorfa/hypermeow/types"
)

// handleGroupInfo answers group.info with made-up members, after a round trip.
func (s *Sim) handleGroupInfo(req json.RawMessage) (any, error) {
	var r struct{ Chat string }
	json.Unmarshal(req, &r)
	jid, err := s.chat(r.Chat)
	if err != nil {
		return nil, err
	}
	if jid.Server != types.GroupServer {
		return nil, &Error{"bad_chat", "not a group: " + r.Chat}
	}
	time.Sleep(simDelay)
	out := groupInfo{Participants: []participant{{JID: simMe, Name: "Sim", Me: true}}}
	add := func(name string, admin bool) {
		out.Participants = append(out.Participants, participant{JID: simMember(name), Name: name, Admin: admin})
	}
	switch i := simIndex(r.Chat); {
	case i == 1:
		out.Description = "Sunday lunches and everything else 🍝"
		out.MembersAdd = true // Sim isn't an admin here, yet may add
		for _, n := range []string{"Mum", "Dad", "Luca", "Giorgia", "Marco"} {
			add(n, n == "Mum" || n == "Dad")
		}
	case i > 1:
		c := simChats[i]
		names := simPeople
		if c.sender != "" {
			names = append([]string{c.sender}, names...)
		}
		for k, n := range names {
			if k > 0 && n == names[0] {
				continue // the sender is also one of simPeople
			}
			add(n, k == 0)
		}
	}
	if i := simIndex(r.Chat); i >= 0 {
		out.Subject = simChats[i].name
	}
	sortParticipants(out.Participants)
	return out, nil
}

// handleGroupParticipants accepts any change after a round trip.
// ponytail: the sim's members are made up per group.info, so the list doesn't change; keep
// per-group members if the sim must show adds and removes.
func (s *Sim) handleGroupParticipants(req json.RawMessage) (any, error) {
	if _, _, _, err := participantReq(req); err != nil {
		return nil, err
	}
	time.Sleep(simDelay)
	return nil, nil
}

// handleContactInfo answers contact.info after a round trip: Anna is in every group, Marco in
// the band's, the rest in none.
func (s *Sim) handleContactInfo(req json.RawMessage) (any, error) {
	var r struct{ Chat string }
	json.Unmarshal(req, &r)
	jid, err := s.chat(r.Chat)
	if err != nil {
		return nil, err
	}
	if !isPerson(jid) {
		return nil, &Error{"bad_chat", "not a person: " + r.Chat}
	}
	time.Sleep(simDelay)
	out := contactInfo{About: "Hey there! I am using WhatsApp.", Groups: []groupRef{}}
	s.mu.Lock()
	out.Blocked = s.blocked[r.Chat]
	s.mu.Unlock()
	if jid.Server == types.DefaultUserServer {
		out.Phone = "+" + jid.User
	} else {
		out.About = ""
	}
	i := simIndex(r.Chat)
	if i == 0 {
		out.About = "Out for pizza 🍕"
	}
	if i == 8 { // the plumber is a business: hours Monday to Friday, a split Saturday, open all Sunday
		out.Business = &businessInfo{Name: "Idraulica Ferri", Category: "Home Improvement",
			Description: "Pipes, boilers, emergencies.", Address: "Via Roma 12, Vicenza",
			Email: "info@example.com", Websites: []string{"https://example.com"}}
		for d := 1; d <= 5; d++ {
			out.Business.Hours = append(out.Business.Hours, bizHour{Day: d, Mode: "specific_hours", Open: 8 * 60, Close: 18 * 60})
		}
		out.Business.Hours = append(out.Business.Hours, bizHour{6, "specific_hours", 9 * 60, 12 * 60},
			bizHour{6, "specific_hours", 15 * 60, 19 * 60}, bizHour{0, "open_24h", 0, 0})
	}
	for _, c := range simChats {
		if strings.HasSuffix(c.jid, "@g.us") && (i == 0 || i == 2 && c.name == "🎸 Band practice") {
			out.Groups = append(out.Groups, groupRef{c.jid, c.name})
		}
	}
	return out, nil
}

// simStrangers are in the address book but have no chat yet.
var simStrangers = []string{"Alessandro Romano", "Beatrice Conti", "Federico Marino", "Ilaria Costa",
	"Matteo Ricci", "Noemi Bruno", "Valentina Moretti", "Zia Rosa 🌹"}

// simBook is the made-up address book: every named 1:1 chat, and the strangers.
func simBook() []contact {
	book := []contact{}
	for _, c := range simChats {
		if c.name != "" && !strings.HasSuffix(c.jid, "@g.us") {
			book = append(book, contact{JID: c.jid, Name: c.name})
		}
	}
	for i, n := range simStrangers {
		book = append(book, contact{JID: fmt.Sprintf("39333200000%d@s.whatsapp.net", i+1), Name: n})
	}
	return book
}

// simBookName is a contact's name, "" when the book doesn't have the key.
func simBookName(jid string) string {
	for _, c := range simBook() {
		if c.JID == jid {
			return c.Name
		}
	}
	return ""
}

// handleContacts answers contacts.list. No delay: the real one reads a local store.
func (s *Sim) handleContacts(json.RawMessage) (any, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	book := simBook()
	sortContacts(book)
	return book, nil
}

// handleBook answers contacts.book with ok and keeps nothing: the sim's people have made-up
// numbers. Not handleLogged, which would put the whole address book in the log.
func (s *Sim) handleBook(json.RawMessage) (any, error) { return nil, nil }
