package main

import (
	"context"
	"encoding/json"
)

// handleResend is msg.resend: it asks our main phone to send a message again, for one that came
// out as "Message" or that this device could not decrypt. The reply only says the request went;
// the message comes back as an ordinary message event, which replaces the row (Store.apply).
func (s *Session) handleResend(req json.RawMessage) (any, error) {
	var r struct{ Chat, ID string }
	if err := json.Unmarshal(req, &r); err != nil || r.Chat == "" || r.ID == "" {
		return nil, &Error{"bad_request", "msg.resend needs chat and id"}
	}
	cli, jid, err := s.linked(r.Chat)
	if err != nil {
		return nil, err
	}
	row, err := s.store.reactable(r.Chat, r.ID) // not_found, as for a reaction
	if err != nil {
		return nil, err
	}
	sender, err := reactSender(row, jid) // the same key as a reaction's: ours, the chat's, or the group member's
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	_, err = cli.SendPeerMessage(ctx, cli.BuildUnavailableMessageRequest(jid, sender, r.ID))
	return nil, err
}
