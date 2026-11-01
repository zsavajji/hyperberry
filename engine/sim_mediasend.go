package main

import (
	"encoding/json"
	"fmt"
	"time"
)

// handleMediaSend is the sim's media.send: the same validation and row as the real one's, then
// sent, delivered and read at simTicks as msg.send's are. Nothing is uploaded.
func (s *Sim) handleMediaSend(req json.RawMessage) (any, error) {
	r, err := parseMediaSend(req)
	if err != nil {
		return nil, err
	}
	jid, err := s.chat(r.Chat)
	if err != nil {
		return nil, err
	}
	id, ops := fmt.Sprintf("SIMOUT%d", time.Now().UnixNano()), s.store.ops
	o, err := s.store.queueMedia(jid, r, id)
	if err != nil {
		return nil, err
	}
	if o.created && chatKind(jid) == chatUser {
		name := simBookName(r.Chat)
		ops <- func(t *storeTx) { t.setName(r.Chat, name) }
	}
	s.log.Infof("sim: media.send %s: not uploaded", req)
	ticks := simTicks // copied: tests restore them while this may still sleep
	go func() {
		time.Sleep(ticks[0])
		ops <- func(t *storeTx) { t.sentAs(r.Chat, id, 0, stateSent) }
		time.Sleep(ticks[1] - ticks[0])
		ops <- func(t *storeTx) { t.setState(r.Chat, id, stateDelivered) }
		time.Sleep(ticks[2] - ticks[1])
		ops <- func(t *storeTx) { t.setState(r.Chat, id, stateRead) }
	}()
	return map[string]string{"id": id}, nil
}
