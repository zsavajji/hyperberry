package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"
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
		// A photo comes at once, for its tile; a video waits for story.get. Never in the writer: it is a network call.
		if row.Kind == msgPhoto && row.MediaState == 1 && !st.holdLive(row) {
			go st.liveStory(cli, row)
		}
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
	t.dropStoryFiles("poster = ? AND id = ?", poster, id)
	if t.exec("DELETE FROM stories WHERE poster = ? AND id = ?", poster, id) > 0 {
		t.stories = true
	}
}

// expireStories drops the stories 24 hours old, with their files. The UI hides them by their time as
// well, so a long run between two sweeps shows none.
func (t *storeTx) expireStories(now int64) {
	t.dropStoryFiles("ts <= ?", now-storyLife)
	if t.exec("DELETE FROM stories WHERE ts <= ?", now-storyLife) > 0 {
		t.stories = true
	}
}

// dropStoryFiles deletes the downloaded files of the stories the condition picks, before their rows go.
func (t *storeTx) dropStoryFiles(where string, args ...any) {
	rows, err := t.tx.Query("SELECT path FROM stories WHERE path IS NOT NULL AND "+where, args...)
	if err != nil {
		t.log.Warnf("story files: %v", err)
		return
	}
	var paths []string
	for rows.Next() {
		var p string
		if rows.Scan(&p) == nil {
			paths = append(paths, p)
		}
	}
	rows.Close()
	for _, p := range paths {
		os.Remove(p)
	}
}

// storyByID reads what a download needs of one story.
func (t *storeTx) storyByID(poster, id string) (msgRow, bool) {
	r := msgRow{Chat: poster, ID: id}
	var media, path sql.NullString
	if t.tx.QueryRow("SELECT kind, media, media_state, path FROM stories WHERE poster = ? AND id = ?",
		poster, id).Scan(&r.Kind, &media, &r.MediaState, &path) != nil {
		return msgRow{}, false
	}
	r.Media, r.Path = media.String, path.String
	return r, true
}

// setStoryFile records a download's end: media_state 2 and the path, or 3. It reports whether the
// story was still there (it may have expired meanwhile).
func (t *storeTx) setStoryFile(poster, id string, state int, path string) bool {
	if t.exec("UPDATE stories SET media_state = ?, path = COALESCE(NULLIF(?, ''), path) WHERE poster = ? AND id = ?",
		state, path, poster, id) > 0 {
		t.stories = true
		return true
	}
	return false
}

// storyRow reads one story through the writer.
func (st *Store) storyRow(poster, id string) (msgRow, bool) {
	type got struct {
		r  msgRow
		ok bool
	}
	c := make(chan got, 1)
	st.ops <- func(t *storeTx) {
		r, ok := t.storyByID(poster, id)
		c <- got{r, ok}
	}
	g := <-c
	return g.r, g.ok
}

// storyTarget is where a story's file goes: data/stories/<id>.jpg for a photo, <id>.<ext> for a
// video (the extension from its mimetype). Not Downloads: a story isn't the user's to keep.
func storyTarget(dir string, row msgRow, mimeType string) string {
	ext := ".jpg"
	if row.Kind == msgVideo {
		ext = mediaExt(mimeType, msgVideo)
	}
	return filepath.Join(dir, "stories", row.ID+ext)
}

// fetchStory is fetchMedia for a story: get writes the file into f and says what it is; one
// download of a story at a time, so a tap during the live photo's download waits and finds the file.
func (st *Store) fetchStory(dir string, row msgRow, get func(f *os.File) (string, error)) (string, error) {
	mu, _ := st.dl.LoadOrStore("story/"+row.Chat+"/"+row.ID, new(sync.Mutex))
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()
	cur, ok := st.storyRow(row.Chat, row.ID)
	if !ok {
		return "", errors.New("the story is gone")
	}
	if p := mediaHere(dir, cur); p != "" {
		return p, nil
	}
	t0 := time.Now()
	tmp := filepath.Join(dir, "stories", row.ID+".tmp")
	path, err := downloadTo(tmp, get, func(mimeType string) string { return storyTarget(dir, row, mimeType) })
	state := 2
	if err != nil {
		state, path = 3, ""
	} else if fi, serr := os.Stat(path); serr == nil {
		logTransfer(st.log, "story "+row.ID, fi.Size(), t0)
	}
	st.ops <- func(t *storeTx) {
		if !t.setStoryFile(row.Chat, row.ID, state, path) && path != "" {
			os.Remove(path) // it expired or was deleted while it downloaded
		}
	}
	return path, err
}

// downloadStory fetches a story's file from WhatsApp, streamed to disk as media is.
func (st *Store) downloadStory(ctx context.Context, cli *whatsmeow.Client, dir string, row msgRow) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	return st.fetchStory(dir, row, func(f *os.File) (string, error) {
		var info mediaInfo
		if err := json.Unmarshal([]byte(row.Media), &info); err != nil {
			return "", err
		}
		return info.Mimetype, cli.DownloadToFile(ctx, info.message(row.Kind), f)
	})
}

// liveStory downloads a photo story that just arrived.
func (st *Store) liveStory(cli *whatsmeow.Client, row msgRow) {
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	if _, err := st.downloadStory(ctx, cli, st.srv.dir, row); err != nil {
		st.log.Warnf("story %s: %v", row.ID, err)
	}
}

type storyReq struct{ Poster, ID string }

func parseStoryReq(req json.RawMessage, cmd string) (storyReq, error) {
	var r storyReq
	if err := json.Unmarshal(req, &r); err != nil || r.Poster == "" || r.ID == "" {
		return r, &Error{"bad_request", cmd + " needs poster and id"}
	}
	return r, nil
}

// storyGet is story.get for both accounts: the path at once when the file is here, else fetch
// downloads it first.
func (st *Store) storyGet(dir string, r storyReq, fetch func(msgRow) (string, error)) (any, error) {
	row, ok := st.storyRow(r.Poster, r.ID)
	if !ok || row.MediaState == 0 {
		return nil, &Error{"not_found", "no such story media"}
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	p := mediaHere(dir, row)
	if p == "" {
		if p, err = fetch(row); err != nil {
			return nil, &Error{"download_failed", err.Error()}
		}
	}
	return map[string]string{"path": p}, nil
}

func (s *Session) handleStoryGet(req json.RawMessage) (any, error) {
	r, err := parseStoryReq(req, "story.get")
	if err != nil {
		return nil, err
	}
	cli, _, err := s.linked(r.Poster)
	if err != nil {
		return nil, err
	}
	fetch := func(row msgRow) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), mediaTimeout)
		defer cancel()
		return s.store.downloadStory(ctx, cli, s.srv.dir, row)
	}
	return s.store.storyGet(s.srv.dir, r, fetch)
}

// setStorySeen marks a story seen. It reports whether that was news: only the first time is.
func (t *storeTx) setStorySeen(poster, id string) bool {
	if t.exec("UPDATE stories SET seen = 1 WHERE poster = ? AND id = ? AND seen = 0", poster, id) > 0 {
		t.stories = true
		return true
	}
	return false
}

// markStorySeen is story.seen's side of the store: it reports whether the story was news.
func (st *Store) markStorySeen(poster, id string) bool {
	fresh := false
	st.runOp(func(t *storeTx) { fresh = t.setStorySeen(poster, id) })
	return fresh
}

// handleStorySeen is story.seen: the viewer showed a story. It is marked seen, and the first time the
// poster gets the read receipt, as WhatsApp's own apps send one: hypermeow sends it as "read-self", to
// our own devices only, when our read receipts are off.
func (s *Session) handleStorySeen(req json.RawMessage) (any, error) {
	r, err := parseStoryReq(req, "story.seen")
	if err != nil {
		return nil, err
	}
	cli, poster, err := s.linked(r.Poster)
	if err != nil {
		return nil, err
	}
	if s.store.markStorySeen(r.Poster, r.ID) {
		go func() { // a network call, and the viewer needn't wait for it
			ctx, cancel := context.WithTimeout(context.Background(), netTimeout)
			defer cancel()
			if err := cli.MarkRead(ctx, []types.MessageID{r.ID}, time.Now(), types.StatusBroadcastJID, poster); err != nil {
				s.store.log.Warnf("story %s: read receipt: %v", r.ID, err)
			}
		}()
	}
	return nil, nil
}

// storyQuote reads what a reply or a reaction needs of a story: its kind, caption and inline thumbnail.
func (st *Store) storyQuote(poster, id string) (kind int, text string, thumb []byte, ok bool) {
	st.runOp(func(t *storeTx) {
		var txt sql.NullString
		ok = t.tx.QueryRow("SELECT kind, text, thumb FROM stories WHERE poster = ? AND id = ?", poster, id).Scan(&kind, &txt, &thumb) == nil
		text = txt.String
	})
	return
}

// quotedStory is the post as a reply quotes it: its content and thumbnail, which is what WhatsApp's own apps
// quote, so the poster sees "replied to your status" with the picture.
func quotedStory(kind int, text string, thumb []byte) *waE2E.Message {
	switch kind {
	case msgPhoto:
		return &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String(text), JPEGThumbnail: thumb}}
	case msgVideo:
		return &waE2E.Message{VideoMessage: &waE2E.VideoMessage{Caption: proto.String(text), JPEGThumbnail: thumb}}
	}
	return &waE2E.Message{Conversation: proto.String(text)}
}

// handleStoryReply is story.reply: our text to the poster's 1:1 chat, quoting the story (the quote names
// status@broadcast and the poster, as a reply to a status does). Our row goes in the chat at once, as any
// message of ours, with the story's one-line preview as its quote.
// (verify on the real link): that the poster's phone shows it as a reply to their status.
func (s *Session) handleStoryReply(req json.RawMessage) (any, error) {
	var r struct{ Poster, ID, Text string }
	if err := json.Unmarshal(req, &r); err != nil || r.Poster == "" || r.ID == "" || strings.TrimSpace(r.Text) == "" {
		return nil, &Error{"bad_request", "story.reply needs poster, id and non-empty text"}
	}
	cli, jid, err := s.linked(r.Poster)
	if err != nil {
		return nil, err
	}
	kind, text, thumb, ok := s.store.storyQuote(r.Poster, r.ID)
	if !ok {
		return nil, &Error{"not_found", "no such story"}
	}
	id, now := cli.GenerateMessageID(), time.Now().Unix()
	_, created := s.store.queueRow(jid, r.Poster, "", msgRow{Chat: r.Poster, ID: id, TS: now, Out: true, State: stateSending,
		Kind: msgText, Text: r.Text, QuoteID: r.ID, QuotePreview: clip(preview(kind, text, 0), 80)})
	s.nameNew(cli, jid, r.Poster, created)
	msg := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: &r.Text, ContextInfo: &waE2E.ContextInfo{
		StanzaID: &r.ID, Participant: &r.Poster, RemoteJID: proto.String(types.StatusBroadcastJID.String()),
		QuotedMessage: quotedStory(kind, text, thumb)}}}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	resp, err := cli.SendMessage(ctx, jid, msg, whatsmeow.SendRequestExtra{ID: id})
	if err != nil {
		s.store.ops <- func(t *storeTx) { t.sentAs(r.Poster, id, 0, stateFailed) }
		return nil, err
	}
	s.store.ops <- func(t *storeTx) { t.sentAs(r.Poster, id, resp.Timestamp.Unix(), stateSent) }
	return map[string]string{"id": id}, nil
}

// handleStoryReact is story.react: an emoji (the heart) on a story, sent to the poster as a reaction to their
// status. Nothing is stored: the reaction belongs to no chat of ours.
// (verify on the real link): that WhatsApp takes it as a reaction to the status.
func (s *Session) handleStoryReact(req json.RawMessage) (any, error) {
	var r struct{ Poster, ID, Emoji string }
	if err := json.Unmarshal(req, &r); err != nil || r.Poster == "" || r.ID == "" || r.Emoji == "" {
		return nil, &Error{"bad_request", "story.react needs poster, id and emoji"}
	}
	cli, jid, err := s.linked(r.Poster)
	if err != nil {
		return nil, err
	}
	if _, _, _, ok := s.store.storyQuote(r.Poster, r.ID); !ok {
		return nil, &Error{"not_found", "no such story"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	if _, err := cli.SendMessage(ctx, jid, cli.BuildReaction(types.StatusBroadcastJID, jid, r.ID, r.Emoji)); err != nil {
		return nil, err
	}
	return nil, nil
}
