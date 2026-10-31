package main

import (
	"encoding/json"
	"fmt"
	"time"
)

// simPlayed is how long after read the sim's note counts as played. Tests shorten it.
var simPlayed = 5 * time.Second

func (s *Sim) registerVoiceSend() {
	s.srv.Handle("voice.start", s.handleVoiceStart)
	s.srv.Handle("voice.send", s.handleVoiceSend)
	s.srv.Handle("voice.cancel", s.handleVoiceCancel)
}

// handleVoiceStart is voice.start: the same polling and encoding, with "recording" as a log line.
func (s *Sim) handleVoiceStart(req json.RawMessage) (any, error) {
	r, err := parseVoicePath(req, "voice.start")
	if err != nil {
		return nil, err
	}
	if _, err := s.chat(r.Chat); err != nil {
		return nil, err
	}
	if _, err := s.rec.start(s.srv.dir, r.Chat, r.Path, s.log); err != nil {
		return nil, err
	}
	s.log.Infof("sim: voice.start %s: recording", req)
	return nil, nil
}

// handleVoiceSend is voice.send: encoded and queued before the reply, then sent, delivered and
// read at simTicks as msg.send's are, and played simPlayed after read.
func (s *Sim) handleVoiceSend(req json.RawMessage) (any, error) {
	r, err := parseVoicePath(req, "voice.send")
	if err != nil {
		return nil, err
	}
	jid, err := s.chat(r.Chat)
	if err != nil {
		return nil, err
	}
	id, ops := fmt.Sprintf("SIMOUT%d", time.Now().UnixNano()), s.store.ops
	n, err := s.store.queueVoice(&s.rec, s.srv.dir, jid, r.Chat, r.Path, r.Quote, id)
	if err != nil {
		return nil, err
	}
	if n.created && chatKind(jid) == chatUser {
		name := simBookName(r.Chat)
		ops <- func(t *storeTx) { t.setName(r.Chat, name) }
	}
	s.log.Infof("sim: voice.send %s: not recording", req)
	ticks, played := simTicks, simPlayed // copied: tests restore them while this may still sleep
	go func() {
		time.Sleep(ticks[0])
		ops <- func(t *storeTx) { t.sentAs(r.Chat, id, 0, stateSent) }
		time.Sleep(ticks[1] - ticks[0])
		ops <- func(t *storeTx) { t.setState(r.Chat, id, stateDelivered) }
		time.Sleep(ticks[2] - ticks[1])
		ops <- func(t *storeTx) { t.setState(r.Chat, id, stateRead) }
		time.Sleep(played)
		ops <- func(t *storeTx) { t.setPlayed(r.Chat, id) }
	}()
	return map[string]string{"id": id}, nil
}

func (s *Sim) handleVoiceCancel(req json.RawMessage) (any, error) {
	r, err := parseVoicePath(req, "voice.cancel")
	if err != nil {
		return nil, err
	}
	if _, err := s.rec.cancel(s.srv.dir, r.Path); err != nil {
		return nil, err
	}
	s.log.Infof("sim: voice.cancel %s: not recording", req)
	return nil, nil
}
