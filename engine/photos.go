package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/types"
)

// Chat pictures. Every person's, group's and channel's chat gets WhatsApp's small preview,
// asked for one chat at a time, streamed to data/photos/<key>-<picture id>.jpg; chats.photo
// holds the path, and the UI draws it over the initials tile. The picture's ID is in the file
// name, so the ID to ask "has it changed?" with is read back from the column: no migration.
// The info pages ask for the full-size picture themselves (full), kept in data/photos/full/.
// The engine never decodes a picture (phase6.md 6.3): the bytes only stream to disk.
// A group's members without a chat of their own get theirs for the group page (members), kept
// in data/photos/members/ and asked again at most every memberPhotoAge.
//
// LIDs: hypermeow passes the JID it is given straight into the query (its only LID handling is
// for the privacy token it attaches), so a chat keyed by LID is asked by LID, as WhatsApp Web
// does. Whether the server answers it is unverified until the real link.
// ponytail: if it doesn't, ask the number (cli.Store.LIDs.GetPNForLID) when the LID says "not set".

// photoPause is the wait between two requests of a round.
// ponytail: fixed, since WhatsApp's limit isn't known; a 429 ends the round. Back off by the
// error instead if the real link hits it.
var photoPause = 300 * time.Millisecond

// photoRoundGap keeps a flapping connection from starting a round at every Connected.
const photoRoundGap = 30 * time.Minute

// memberPrefix marks a queue entry as a group member without a chat: it shares the chats' queue,
// so their pacing and their 429 stop.
const memberPrefix = "member:"

// memberPhotoAge is how long a member's picture, or the lack of one, is trusted: it is asked
// again at most this often, besides when WhatsApp says it changed. The cache is the files in
// members/ (the picture, or <key>.none for "has none"): their newest mtime is the last check, so
// it survives a restart and opening a group again asks no one checked lately.
// ponytail: a big group queues every unchecked member on its first open; a 429 ends the round
// and the rest wait for the next open. Members who left keep their files until unlink.
const memberPhotoAge = 7 * 24 * time.Hour

// photoFetch asks WhatsApp for jid's picture (the preview, or the full size) unless it is still
// existing (an ID, "" for none), and streams it to dst(its ID). It returns the file's path, or
// "" when the picture is the existing one. The not-set and hidden errors come back as they are.
type photoFetch func(ctx context.Context, jid types.JID, preview bool, existing string, dst func(id string) string) (string, error)

// clientFetch is photoFetch on a hypermeow client.
func clientFetch(cli *whatsmeow.Client) photoFetch {
	return func(ctx context.Context, jid types.JID, preview bool, existing string, dst func(string) string) (string, error) {
		if jid.Server == types.NewsletterServer {
			return channelFetch(ctx, cli, jid, preview, existing, dst)
		}
		info, err := cli.GetProfilePictureInfo(ctx, jid, &whatsmeow.GetProfilePictureParams{Preview: preview, ExistingID: existing})
		if err != nil || info == nil || info.URL == "" {
			return "", err // nil, nil: unchanged
		}
		path := dst(info.ID)
		return downloadTo(strings.TrimSuffix(path, ".jpg")+".tmp", streamURL(ctx, info.URL), func(string) string { return path })
	}
}

// streamURL is a downloadTo getter that copies what url serves into the file: the picture is
// never held whole.
// ponytail: hypermeow's media HTTP client isn't exported; the default one does for a CDN URL.
func streamURL(ctx context.Context, url string) func(f *os.File) (string, error) {
	return func(f *os.File) (string, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return "", err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("picture: HTTP %d", resp.StatusCode)
		}
		_, err = io.Copy(f, resp.Body)
		return "", err
	}
}

// noPicture: the chat has none, or hides it from us.
func noPicture(err error) bool {
	return errors.Is(err, whatsmeow.ErrProfilePictureNotSet) || errors.Is(err, whatsmeow.ErrProfilePictureUnauthorized)
}

// photoStops: asking more would only fail (WhatsApp says slow down, or the link is down).
func photoStops(err error) bool {
	return errors.Is(err, whatsmeow.ErrIQRateOverLimit) || errors.Is(err, whatsmeow.ErrNotConnected)
}

// pictureID is the ID in a picture file's name ("<key>-<id>.jpg"; keys hold dashes, IDs don't),
// "" for a path that isn't one.
func pictureID(path string) string {
	b := strings.TrimSuffix(filepath.Base(path), ".jpg")
	if i := strings.LastIndex(b, "-"); i >= 0 {
		return b[i+1:]
	}
	return ""
}

// photoQueue is the one worker. A nil queue (a Session built bare in a test) does nothing.
type photoQueue struct {
	st    *Store
	fetch func() photoFetch // the current client's: a logout replaces it
	dir   string            // data/photos, absolute

	mu      sync.Mutex
	todo    []string  // chat keys, once each
	running bool      // a drain goroutine is alive
	last    time.Time // when the last round started

	fullMu sync.Mutex // full: two pages don't write one file
}

func newPhotoQueue(st *Store, fetch func() photoFetch) *photoQueue {
	dir := filepath.Join(st.srv.dir, "photos")
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs // the UI reads the paths from another working directory
	}
	return &photoQueue{st: st, fetch: fetch, dir: dir}
}

func (q *photoQueue) path(sub, key, id string) string {
	return filepath.Join(q.dir, sub, fileSafe.Replace(key)+"-"+id+".jpg")
}

// connected starts a round, unless one started lately: Connected comes at every reconnect.
func (q *photoQueue) connected() {
	if q == nil {
		return
	}
	q.mu.Lock()
	recent := time.Since(q.last) < photoRoundGap
	q.mu.Unlock()
	if !recent {
		q.round()
	}
}

// round queues every person's, group's and channel's chat, newest first, so the ones on screen are done first.
// ponytail: a chat that appears after the round, or whose request failed, waits for the next
// round or for its chat.open.
func (q *photoQueue) round() {
	if q == nil {
		return
	}
	q.mu.Lock()
	q.last = time.Now()
	q.mu.Unlock()
	go q.roundNow() // the writer may be busy: not on the caller's goroutine
}

func (q *photoQueue) roundNow() { q.add(q.chatKeys()...) }

func (q *photoQueue) chatKeys() []string {
	got := make(chan []string, 1)
	q.st.ops <- func(t *storeTx) {
		var keys []string
		defer func() { got <- keys }()
		rows, err := t.tx.Query("SELECT jid FROM chats WHERE kind IN (?, ?, ?) ORDER BY last_ts DESC", chatUser, chatGroup, chatNewsletter)
		if err != nil {
			t.log.Warnf("photos: %v", err)
			return
		}
		defer rows.Close()
		for rows.Next() {
			var j string
			if rows.Scan(&j) == nil {
				keys = append(keys, j)
			}
		}
	}
	return <-got
}

// changed queues the chat of a jid whose picture changed (it may be a number for a chat keyed by LID).
func (q *photoQueue) changed(cli *whatsmeow.Client, jid types.JID) {
	if q == nil {
		return
	}
	q.st.ops <- func(t *storeTx) {
		key := chatKey(context.Background(), t, cli.Store.LIDs, jid, types.EmptyJID)
		if t.hasChat(key) {
			q.add(key)
		} else if _, seen := q.memberCache(key); !seen.IsZero() {
			q.add(memberPrefix + key) // a member we've cached: the new picture now, not at the age
		}
	}
}

// add queues chats not queued yet and starts the worker if it isn't running. A key the worker
// has already taken is queued again: it was asked before the news.
// ponytail: a chat opened during a round waits its turn; put it first if that shows.
func (q *photoQueue) add(keys ...string) {
	if q == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, k := range keys {
		if !slices.Contains(q.todo, k) {
			q.todo = append(q.todo, k)
		}
	}
	if !q.running && len(q.todo) > 0 {
		q.running = true
		go q.drain()
	}
}

// drain asks for one chat at a time, with a pause between, until the queue is empty or WhatsApp
// says to stop (the rest wait for the next round).
func (q *photoQueue) drain() {
	for {
		q.mu.Lock()
		if len(q.todo) == 0 {
			q.running = false
			q.mu.Unlock()
			return
		}
		key := q.todo[0]
		q.todo = q.todo[1:]
		q.mu.Unlock()
		if err := q.refresh(key); err != nil {
			q.st.log.Warnf("photos: %s: %v", key, err)
			if photoStops(err) {
				q.mu.Lock()
				q.todo, q.running = nil, false
				q.mu.Unlock()
				return
			}
		}
		time.Sleep(photoPause)
	}
}

// current is the chat's photo column ("" when none) and whether the chat exists.
func (q *photoQueue) current(key string) (string, bool) {
	type res struct {
		path string
		ok   bool
	}
	got := make(chan res, 1)
	q.st.ops <- func(t *storeTx) {
		var p sql.NullString
		err := t.tx.QueryRow("SELECT photo FROM chats WHERE jid = ?", key).Scan(&p)
		got <- res{p.String, err == nil}
	}
	r := <-got
	return r.path, r.ok
}

// refresh brings one chat's preview up to date.
func (q *photoQueue) refresh(key string) error {
	if k, ok := strings.CutPrefix(key, memberPrefix); ok {
		return q.member(k)
	}
	jid, err := types.ParseJID(key)
	if err != nil || !isPerson(jid) && jid.Server != types.GroupServer && jid.Server != types.NewsletterServer {
		return nil
	}
	cur, ok := q.current(key)
	if !ok {
		return nil // the chat is gone
	}
	id := ""
	if _, err := os.Stat(cur); err == nil {
		id = pictureID(cur)
	}
	ctx, cancel := context.WithTimeout(context.Background(), netTimeout)
	defer cancel()
	path, err := q.fetch()(ctx, jid, true, id, func(id string) string { return q.path("", key, id) })
	switch {
	case noPicture(err):
		path = ""
	case err != nil:
		return err
	case path == "":
		return nil // unchanged
	}
	if path != cur {
		q.set(key, path)
	}
	return nil
}

// set makes path the chat's photo ("" for none) and, once that has committed, removes the file
// it replaces. It returns when the op has run.
func (q *photoQueue) set(key, path string) {
	done := make(chan struct{})
	q.st.ops <- func(t *storeTx) {
		defer close(done)
		drop := func(p string) {
			if p != "" {
				t.after = append(t.after, func() { os.Remove(p) })
			}
		}
		var old sql.NullString
		if t.tx.QueryRow("SELECT photo FROM chats WHERE jid = ?", key).Scan(&old) != nil {
			drop(path) // the chat went while the picture came
			return
		}
		t.touch(key, "UPDATE chats SET photo = NULLIF(?1, '') WHERE photo IS NOT NULLIF(?1, '') AND jid = ?2", path)
		if old.String != path {
			// ponytail: a chat merged into its LID's leaves the other's file behind until the next unlink.
			drop(old.String)
		}
	}
	<-done
}

// full is jid's full-size picture's path, "" when it has none (or hides it). The info pages
// ask each time: with the stored picture's ID an unchanged one costs no download. A failure
// is logged and the stored picture, if any, is the answer.
func (q *photoQueue) full(jid types.JID) string {
	if q == nil {
		return ""
	}
	q.fullMu.Lock()
	defer q.fullMu.Unlock()
	have, _ := filepath.Glob(q.path("full", jid.String(), "*"))
	cur := ""
	if len(have) > 0 {
		cur = have[len(have)-1]
	}
	ctx, cancel := context.WithTimeout(context.Background(), netTimeout)
	defer cancel()
	path, err := q.fetch()(ctx, jid, false, pictureID(cur), func(id string) string { return q.path("full", jid.String(), id) })
	switch {
	case noPicture(err):
		path = ""
	case err != nil:
		q.st.log.Warnf("photos: %s: %v", jid, err)
		return cur
	case path == "":
		return cur
	}
	for _, f := range have {
		if f != path {
			os.Remove(f)
		}
	}
	return path
}

// memberNone is the mark that a member has no picture, or hides it.
func (q *photoQueue) memberNone(key string) string {
	return filepath.Join(q.dir, "members", fileSafe.Replace(key)+".none")
}

// memberCache is a member's pictures in members/ (one, but for a moment) and when they were last
// checked: the newest mtime of those and the none mark, zero when never.
func (q *photoQueue) memberCache(key string) (jpgs []string, seen time.Time) {
	jpgs, _ = filepath.Glob(q.path("members", key, "*"))
	for _, f := range append([]string{q.memberNone(key)}, jpgs...) {
		if fi, err := os.Stat(f); err == nil && fi.ModTime().After(seen) {
			seen = fi.ModTime()
		}
	}
	return
}

// members fills in the Photo of a group's participants and queues the ones to ask: ours is the
// profile's; one with a chat has the chat's (the chat rounds keep it fresh); the rest have the
// file kept in members/, if any, and are queued unless checked within memberPhotoAge. It never
// waits for the network.
func (q *photoQueue) members(ps []participant) {
	if q == nil {
		return
	}
	got := make(chan map[string]string, 1)
	q.st.ops <- func(t *storeTx) {
		chats := map[string]string{}
		defer func() { got <- chats }()
		for _, p := range ps {
			var photo sql.NullString
			if !p.Me && t.tx.QueryRow("SELECT photo FROM chats WHERE jid = ?", p.JID).Scan(&photo) == nil {
				chats[p.JID] = photo.String
			}
		}
	}
	chats := <-got
	var todo []string
	for i := range ps {
		p := &ps[i]
		if photo, ok := chats[p.JID]; ok {
			p.Photo = photo
			continue
		}
		if p.Me {
			p.Photo = profilePicture(q.st.srv.dir)
			continue
		}
		jpgs, seen := q.memberCache(p.JID)
		if len(jpgs) > 0 {
			p.Photo = jpgs[len(jpgs)-1]
		}
		if time.Since(seen) > memberPhotoAge {
			todo = append(todo, memberPrefix+p.JID)
		}
	}
	q.add(todo...)
}

// member brings one member's preview up to date, as refresh does a chat's: the file in members/,
// the none mark when they have no picture, and a photo event when it is not what it was.
func (q *photoQueue) member(key string) error {
	jid, err := types.ParseJID(key)
	if err != nil || !isPerson(jid) {
		return nil
	}
	have, _ := q.memberCache(key)
	cur := ""
	if len(have) > 0 {
		cur = have[len(have)-1]
	}
	none := q.memberNone(key)
	ctx, cancel := context.WithTimeout(context.Background(), netTimeout)
	defer cancel()
	path, err := q.fetch()(ctx, jid, true, pictureID(cur), func(id string) string { return q.path("members", key, id) })
	switch {
	case noPicture(err), err == nil && path == "" && cur == "":
		path = "" // none; a nil answer to an unasked ID is the same
		for _, f := range have {
			os.Remove(f)
		}
		os.MkdirAll(filepath.Dir(none), 0o755)
		os.WriteFile(none, nil, 0o644) // makes it, or renews its mtime
	case err != nil:
		return err
	case path == "":
		now := time.Now()
		os.Chtimes(cur, now, now) // unchanged: checked just now
		return nil
	default:
		for _, f := range have {
			if f != path {
				os.Remove(f)
			}
		}
		os.Remove(none)
	}
	if path != cur {
		q.st.srv.Emit("photo", map[string]any{"jid": key, "photo": path})
	}
	return nil
}
