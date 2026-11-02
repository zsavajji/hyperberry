package main

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
)

// Poll votes (step 6.5a): the poll_votes table, written from live votes and from history. sender
// is the voter's key as reactions have it, "" for us; options is the chosen options' indices.

// setVote is the one writer of poll_votes, ordered as setReaction is: the newest by ts wins, a
// removal (options "") only takes a vote no newer than itself, and the poll is named as changed
// only when a row did change.
// ponytail: no tombstones either, so a removal that comes before the vote it removes lets that
// one stay; keep removed rows if it shows.
func (t *storeTx) setVote(chat, msg, sender, name, options string, ts int64) {
	var n int64
	if options == "" {
		n = t.exec("DELETE FROM poll_votes WHERE chat = ? AND msg = ? AND sender = ? AND ts <= ?", chat, msg, sender, ts)
	} else {
		n = t.exec(`INSERT INTO poll_votes(chat, msg, sender, sender_name, options, ts)
VALUES(?1, ?2, ?3, NULLIF(?4, ''), ?5, ?6)
ON CONFLICT(chat, msg, sender) DO UPDATE SET options = excluded.options,
  sender_name = COALESCE(excluded.sender_name, poll_votes.sender_name), ts = excluded.ts
WHERE excluded.ts >= poll_votes.ts AND (excluded.options != poll_votes.options OR excluded.ts > poll_votes.ts)`,
			chat, msg, sender, name, options, ts)
	}
	if n > 0 {
		t.changed(chat, msg)
	}
}

// pollOptions is the option names of a poll row, from its extra.
func pollOptions(extra string) []string {
	var x struct{ Options []string }
	json.Unmarshal([]byte(extra), &x) // not JSON: no options, and every vote is dropped
	return x.Options
}

// voteIndices turns the SHA-256 hashes a vote carries into the indices of the options they name,
// "0,2". A hash that names no option is ignored.
func voteIndices(options []string, hashes [][]byte) string {
	want := whatsmeow.HashPollOptions(options)
	var idx []int
	for _, h := range hashes {
		if i := slices.IndexFunc(want, func(w []byte) bool { return slices.Equal(w, h) }); i >= 0 && !slices.Contains(idx, i) {
			idx = append(idx, i)
		}
	}
	slices.Sort(idx)
	s := make([]string, len(idx))
	for i, n := range idx {
		s[i] = strconv.Itoa(n)
	}
	return strings.Join(s, ",")
}

// vote stores one vote on a poll row. A vote whose hashes name none of the options (the poll was
// edited, or isn't the one we know) is dropped rather than read as taking the vote back.
func (t *storeTx) vote(poll msgRow, sender, name string, hashes [][]byte, ts int64) {
	idx := voteIndices(pollOptions(poll.Extra), hashes)
	if idx == "" && len(hashes) > 0 {
		t.log.Debugf("poll %s: a vote for none of its options", poll.ID)
		return
	}
	t.setVote(poll.Chat, poll.ID, sender, name, idx, ts)
}

// liveVote stores a live poll vote: it is decrypted with the poll's secret, and leaves no row,
// no unread and no preview. A vote for a poll we don't hold, or one that can't be decrypted, is
// dropped.
func (st *Store) liveVote(ctx context.Context, t *storeTx, cli *whatsmeow.Client, key string, e *events.Message, name string) {
	u := e.Message.GetPollUpdateMessage()
	poll, ok := t.messageByID(key, u.GetPollCreationMessageKey().GetID())
	if !ok || poll.Kind != msgPoll {
		st.log.Debugf("poll vote %s: no such poll %s", e.Info.ID, u.GetPollCreationMessageKey().GetID())
		return
	}
	v, err := cli.DecryptPollVote(ctx, e)
	if err != nil {
		st.log.Warnf("poll vote %s: %v", e.Info.ID, err)
		return
	}
	ts := u.GetSenderTimestampMS() / 1000
	if ts == 0 {
		ts = e.Info.Timestamp.Unix()
	}
	t.vote(poll, actorKey(ctx, t, cli, key, e), name, v.GetSelectedOptions(), ts)
}

// historyVotes stores the votes a history poll carries, already decrypted. The chat preview is
// not touched.
func historyVotes(ctx context.Context, t *storeTx, cli *whatsmeow.Client, key string, chat, alt types.JID, ev *events.Message) {
	updates := ev.SourceWebMsg.GetPollUpdates()
	if len(updates) == 0 {
		return
	}
	poll, ok := t.messageByID(key, ev.Info.ID)
	if !ok || poll.Kind != msgPoll {
		return
	}
	for _, u := range updates {
		ts := u.GetSenderTimestampMS() / 1000
		if ts == 0 {
			ts = ev.Info.Timestamp.Unix()
		}
		if sender, name, ok := historyActor(ctx, t, cli, key, chat, alt, u.GetPollUpdateMessageKey()); ok {
			t.vote(poll, sender, name, u.GetVote().GetSelectedOptions(), ts)
		}
	}
}

// movePollVotes takes the phone-number chat's votes to the LID chat as moveReactions does its
// reactions: in a 1:1 chat their votes are keyed by the chat itself, so that key moves too.
func movePollVotes(tx *storeTx, from, to string) {
	tx.exec("UPDATE OR IGNORE poll_votes SET chat = ? WHERE chat = ?", to, from)
	tx.exec("DELETE FROM poll_votes WHERE chat = ?", from)
	tx.exec("UPDATE OR IGNORE poll_votes SET sender = ?1 WHERE chat = ?2 AND sender = ?3", to, to, from)
	tx.exec("DELETE FROM poll_votes WHERE chat = ? AND sender = ?", to, from)
}
