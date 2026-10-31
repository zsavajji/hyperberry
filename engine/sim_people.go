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

// simStrangers are in the address book but have no chat yet.
var simStrangers = []string{"Alessandro Romano", "Beatrice Conti", "Federico Marino", "Ilaria Costa",
	"Matteo Ricci", "Noemi Bruno", "Valentina Moretti", "Zia Rosa 🌹"}

// simBook is the made-up address book: every named 1:1 chat, and the strangers.
func simBook() []contact {
	book := []contact{}
	for _, c := range simChats {
		if c.name != "" && !strings.HasSuffix(c.jid, "@g.us") {
			book = append(book, contact{c.jid, c.name})
		}
	}
	for i, n := range simStrangers {
		book = append(book, contact{fmt.Sprintf("39333200000%d@s.whatsapp.net", i+1), n})
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
