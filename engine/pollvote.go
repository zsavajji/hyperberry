package main

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/types"
)

// Voting in polls (step 6.5e): poll.vote sends our vote, then writes it to poll_votes.

type voteReq struct {
	Chat, ID string
	Options  []int
}

func parseVote(req json.RawMessage) (voteReq, error) {
	var r voteReq
	// No options takes the vote back, missing or null as well as []: the UI's JSON writer
	// (JsonDataAccess) may not keep an empty list as one.
	if err := json.Unmarshal(req, &r); err != nil || r.Chat == "" || r.ID == "" {
		return r, &Error{"bad_request", "poll.vote needs chat, id and options"}
	}
	return r, nil
}

// pollChoice checks the chosen indices against the poll row. It gives the chosen options' names
// (what BuildPollVote hashes) and the indices as poll_votes keeps them, sorted: "0,2", "" for none.
func pollChoice(row msgRow, chosen []int) ([]string, string, error) {
	if row.Kind != msgPoll {
		return nil, "", &Error{"bad_request", "not a poll"}
	}
	var x struct {
		Options []string
		Max     int
	}
	json.Unmarshal([]byte(row.Extra), &x) // not JSON: no options, so every index is out of range
	idx := slices.Sorted(slices.Values(chosen))
	if x.Max > 0 && len(idx) > x.Max {
		return nil, "", &Error{"bad_request", "the poll allows " + strconv.Itoa(x.Max) + " choices at most"}
	}
	names, parts := make([]string, len(idx)), make([]string, len(idx))
	for i, n := range idx {
		if n < 0 || n >= len(x.Options) {
			return nil, "", &Error{"bad_request", "no option " + strconv.Itoa(n)}
		}
		if i > 0 && n == idx[i-1] {
			return nil, "", &Error{"bad_request", "option " + strconv.Itoa(n) + " chosen twice"}
		}
		names[i], parts[i] = x.Options[n], strconv.Itoa(n)
	}
	return names, strings.Join(parts, ","), nil
}

// pollInfo is the MessageInfo BuildPollVote wants for the poll row in the chat jid, whose author
// is sender (pollSender).
func pollInfo(jid types.JID, row msgRow, sender types.JID) *types.MessageInfo {
	return &types.MessageInfo{ID: row.ID, MessageSource: types.MessageSource{
		Chat: jid, Sender: sender, IsFromMe: row.Out, IsGroup: chatKind(jid) == chatGroup}}
}

// pollSender is the poll author's JID for EncryptPollVote. It looks the poll's message secret up
// with it, and signs our vote as our LID, or as our number if the author is one. The vote must be
// signed as SendMessage sends it, since the receiver builds the key from the stanza's sender:
//   - a 1:1 chat: SendMessage always sends as our LID, so the author must not be a number: the
//     chat itself for theirs (reactSender), our LID for ours.
//   - a group: as our LID in a LID-addressed group, as our number in the others. The poll's secret
//     is stored under the author in that form (ours: the identity SendMessage sent it as; theirs:
//     the message's sender), so ask the store, which matches a LID and its number alike, who it
//     holds the secret under.
//
// ponytail: a 1:1 chat still keyed by phone number, with no LID known, signs the vote with our
// number while SendMessage sends from our LID; chatKey makes the chat a LID one as soon as the
// mapping is known. Look the LID up here if it shows.
func pollSender(ctx context.Context, cli *whatsmeow.Client, jid types.JID, row msgRow) (types.JID, error) {
	var who []types.JID // the forms of the author to ask the store with
	if row.Out {
		who = []types.JID{cli.Store.GetLID().ToNonAD(), cli.Store.GetJID().ToNonAD()}
	} else {
		s, err := reactSender(row, jid)
		if err != nil {
			return s, err
		}
		who = []types.JID{s}
	}
	who = slices.DeleteFunc(who, types.JID.IsEmpty) // never all of them: we are linked
	if chatKind(jid) != chatGroup {
		return who[0], nil
	}
	for _, w := range who {
		if secret, held, err := cli.Store.MsgSecrets.GetMessageSecret(ctx, jid, w, row.ID); err == nil && secret != nil {
			return held, nil
		}
	}
	return who[0], nil // no secret: BuildPollVote says so
}

// handlePollVote is poll.vote: it sends our vote (no options takes it back), then writes it here,
// since WhatsApp doesn't echo our own sends to this device.
func (s *Session) handlePollVote(req json.RawMessage) (any, error) {
	r, err := parseVote(req)
	if err != nil {
		return nil, err
	}
	cli, jid, err := s.linked(r.Chat)
	if err != nil {
		return nil, err
	}
	row, err := s.store.reactable(r.Chat, r.ID)
	if err != nil {
		return nil, err
	}
	names, idx, err := pollChoice(row, r.Options)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	sender, err := pollSender(ctx, cli, jid, row)
	if err != nil {
		return nil, err
	}
	msg, err := cli.BuildPollVote(ctx, pollInfo(jid, row, sender), names)
	if errors.Is(err, whatsmeow.ErrOriginalMessageSecretNotFound) {
		return nil, &Error{"not_found", "this poll's secret isn't here, so it can't be voted on"}
	} else if err != nil {
		return nil, err
	}
	if _, err := cli.SendMessage(ctx, jid, msg); err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	s.store.ops <- func(t *storeTx) { t.setVote(r.Chat, r.ID, "", "", idx, now) }
	return nil, nil
}
