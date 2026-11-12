package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types/events"
)

// Stories (the Stories tab, docs/plan/stories.md): WhatsApp's status posts, kept in their own table
// so that status@broadcast is no chat in the list, and with no unread count and no alert.

// storyLife is how long a story lives: WhatsApp's 24 hours, in seconds.
const storyLife = 24 * 60 * 60

// story handles a message of status@broadcast. It keeps other people's text, photo and video
// posts, and drops a story its poster deletes; our own posts (from our phone) have no tile, the
// tab is for viewing.
// ponytail: the status conversation of a history sync is skipped (history.go), so a fresh link shows
// only what arrives after it; ingest it there if that is missed.
func (st *Store) story(ctx context.Context, t *storeTx, cli *whatsmeow.Client, e *events.Message) {
	if e.Info.IsFromMe {
		return
	}
	poster := chatKey(ctx, t, cli.Store.LIDs, e.Info.Sender, e.Info.SenderAlt)
	m := flatten(e.Message)
	if pm := m.GetProtocolMessage(); pm != nil {
		if pm.GetType() == waE2E.ProtocolMessage_REVOKE {
			t.deleteStory(poster, pm.GetKey().GetID())
		}
		return
	}
	row, ok := rowFromMessage(poster, e, "")
	if !ok || row.Kind != msgText && row.Kind != msgPhoto && row.Kind != msgVideo {
		return
	}
	now := time.Now().Unix()
	if now-row.TS >= storyLife { // one that came late, from a backlog
		return
	}
	row.SenderName = resolveName(ctx, cli, e.Info.Sender, e.Info.SenderAlt, e.Info.PushName)
	if row.Kind == msgText { // the post is its colours: a link preview's thumbnail is not a picture of it
		x := map[string]any{}
		tx := m.GetExtendedTextMessage()
		keep(x, "bg", tx.GetBackgroundArgb())
		keep(x, "fg", tx.GetTextArgb())
		keep(x, "font", int(tx.GetFont()))
		row.Extra, row.Thumb = "", nil
		if b, err := json.Marshal(x); err == nil && len(x) > 0 {
			row.Extra = string(b)
		}
	}
	if t.insertStory(row) {
		t.expireStories(now)
	}
}

// insertStory is INSERT OR IGNORE: the same post can arrive twice. s is a msgRow with Chat the
// poster's key and SenderName their name. It reports whether the story was new.
func (t *storeTx) insertStory(s msgRow) bool {
	if t.exec(`INSERT OR IGNORE INTO stories(poster, id, ts, name, kind, text, duration, width, height, size,
thumb, media, media_state, extra)
VALUES(?, ?, ?, NULLIF(?, ''), ?, NULLIF(?, ''), NULLIF(?, 0), NULLIF(?, 0), NULLIF(?, 0), NULLIF(?, 0),
?, NULLIF(?, ''), ?, NULLIF(?, ''))`,
		s.Chat, s.ID, s.TS, s.SenderName, s.Kind, s.Text, s.Duration, s.Width, s.Height, s.Size,
		blob(s.Thumb), s.Media, s.MediaState, s.Extra) == 0 {
		return false
	}
	t.stories = true
	return true
}

func (t *storeTx) deleteStory(poster, id string) {
	if t.exec("DELETE FROM stories WHERE poster = ? AND id = ?", poster, id) > 0 {
		t.stories = true
	}
}

// expireStories drops the stories 24 hours old. The UI hides them by their time as well, so a long
// run between two sweeps shows none.
func (t *storeTx) expireStories(now int64) {
	if t.exec("DELETE FROM stories WHERE ts <= ?", now-storyLife) > 0 {
		t.stories = true
	}
}
