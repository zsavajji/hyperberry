package main

import (
	"encoding/json"
	"strings"
	"time"
)

// simProfile is the simulated account's name and about. ponytail: in memory, so an engine
// restart puts them back; me.jpg is a file and stays.
type simProfile struct{ name, about string }

func (s *Sim) registerProfile() {
	s.prof = simProfile{name: "Sim", about: "Available"}
	s.srv.Handle("profile.get", s.handleProfileGet)
	s.srv.Handle("profile.set", s.handleProfileSet)
}

// handleProfileGet answers profile.get from what the sim keeps. No delay, as contacts.list.
func (s *Sim) handleProfileGet(json.RawMessage) (any, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	phone, _, _ := strings.Cut(simMe, "@")
	return profileInfo{Name: s.prof.name, About: s.prof.about, Phone: "+" + phone, Picture: profilePicture(s.srv.dir)}, nil
}

// handleProfileSet checks as the real one does, then keeps the change after a round trip.
func (s *Sim) handleProfileSet(req json.RawMessage) (any, error) {
	r, err := parseProfileSet(req)
	if err != nil {
		return nil, err
	}
	if err := s.ready(); err != nil {
		return nil, err
	}
	time.Sleep(simDelay)
	if r.photo {
		profileMu.Lock()
		err := setProfilePhoto(s.srv.dir, r.jpeg)
		profileMu.Unlock()
		if err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.name != nil {
		s.prof.name = *r.name
	}
	if r.about != nil {
		s.prof.about = *r.about
	}
	return nil, nil
}
