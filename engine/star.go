package main

import (
	"encoding/json"

	"github.com/polymorfa/hypermeow/appstate"
	"github.com/polymorfa/hypermeow/types"
)

// starReq is msg.star; star false takes the star off.
type starReq struct {
	Chat, ID string
	Star     *bool
}

func parseStar(req json.RawMessage) (starReq, error) {
	var r starReq
	if err := json.Unmarshal(req, &r); err != nil || r.Chat == "" || r.ID == "" || r.Star == nil {
		return r, &Error{"bad_request", "msg.star needs chat, id and star"}
	}
	return r, nil
}

// setStarred stars or unstars one message, as the phone's app state or the user did.
func (t *storeTx) setStarred(chat, id string, v bool) {
	if t.exec("UPDATE messages SET starred = ?1 WHERE starred != ?1 AND chat = ?2 AND id = ?3", v, chat, id) > 0 {
		t.changed(chat, id)
	}
}

// applyStar changes the row at once and hands the message back; WhatsApp doesn't echo our own patch to this device.
func (st *Store) applyStar(r starReq) (msgRow, error) {
	row, err := st.reactable(r.Chat, r.ID)
	if err != nil {
		return row, err
	}
	st.ops <- func(t *storeTx) { t.setStarred(r.Chat, r.ID, *r.Star) }
	return row, nil
}

// starPatch is the app-state patch for starring row in chat: in a group it names the sender of a message that
// isn't ours; BuildStar writes "0" where that is the chat itself, or none (a 1:1 chat, ours).
func starPatch(chat types.JID, row msgRow, star bool) appstate.PatchInfo {
	sender := chat
	if row.Sender != "" && !row.Out {
		if j, err := types.ParseJID(row.Sender); err == nil {
			sender = j
		}
	}
	return appstate.BuildStar(chat, sender, row.ID, row.Out, star)
}

// handleStar is msg.star: the row changes here, then the main phone is told.
func (s *Session) handleStar(req json.RawMessage) (any, error) {
	r, err := parseStar(req)
	if err != nil {
		return nil, err
	}
	cli, jid, err := s.linked(r.Chat)
	if err != nil {
		return nil, err
	}
	row, err := s.store.applyStar(r)
	if err != nil {
		return nil, err
	}
	// Local only: WhatsApp's app state has no channels.
	if jid.Server == types.NewsletterServer {
		return nil, nil
	}
	s.sendPatch(cli, "msg.star", starPatch(jid, row, *r.Star))
	return nil, nil
}
