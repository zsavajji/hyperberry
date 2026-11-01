package main

import (
	"encoding/json"
	"time"
)

// handleContactBlock is contact.block: the flag contact.info reports, after a round trip.
func (s *Sim) handleContactBlock(req json.RawMessage) (any, error) {
	chat, block, err := parseBlock(req)
	if err != nil {
		return nil, err
	}
	jid, err := s.chat(chat)
	if err != nil {
		return nil, err
	}
	if !isPerson(jid) {
		return nil, &Error{"bad_chat", "not a person: " + chat}
	}
	time.Sleep(simDelay)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.blocked == nil {
		s.blocked = map[string]bool{}
	}
	s.blocked[chat] = block
	return nil, nil
}

func (s *Sim) handleChatClear(req json.RawMessage) (any, error) {
	return s.clearChat(req, "chat.clear", false)
}

func (s *Sim) handleChatDelete(req json.RawMessage) (any, error) {
	return s.clearChat(req, "chat.delete", true)
}

// clearChat is chat.clear and chat.delete: the same rows change as with the real one, nothing is sent.
func (s *Sim) clearChat(req json.RawMessage, cmd string, drop bool) (any, error) {
	chat, err := parseChatReq(req, cmd)
	if err != nil {
		return nil, err
	}
	if _, err := s.chat(chat); err != nil {
		return nil, err
	}
	time.Sleep(simDelay)
	s.store.applyClear(chat, drop)
	return nil, nil
}
