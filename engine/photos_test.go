package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"context"
	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/types"
)

// photoFake stands in for the client: reply decides each call (n counts from 1), calls
// records "<jid>[<existing id>]", and max is the most requests that ever ran at once.
type photoFake struct {
	mu       sync.Mutex
	calls    []string
	cur, max int
	reply    func(n int, jid types.JID, existing string, dst func(string) string) (string, error)
}

func (f *photoFake) fetch(_ context.Context, jid types.JID, preview bool, existing string, dst func(string) string) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, fmt.Sprintf("%s[%s]", jid, existing))
	n := len(f.calls)
	if f.cur++; f.cur > f.max {
		f.max = f.cur
	}
	f.mu.Unlock()
	defer func() { f.mu.Lock(); f.cur--; f.mu.Unlock() }()
	return f.reply(n, jid, existing, dst)
}

func (f *photoFake) got() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// newPic is what a download does: the file of picture id at dst(id).
func newPic(dst func(string) string, id string) string {
	p := dst(id)
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte{0xFF, 0xD8, 0xFF, 1}, 0o644)
	return p
}

func newPhotoTest(t *testing.T) (*photoQueue, *Store, *photoFake, *conn) {
	t.Helper()
	pause := photoPause
	photoPause = 0
	t.Cleanup(func() { photoPause = pause })
	ts := newTestServer(t)
	db, err := openChats(filepath.Join(t.TempDir(), "chats.db"))
	if err != nil {
		t.Fatal(err)
	}
	st := newStore(db, ts.Server, ts.Server.log)
	t.Cleanup(func() { st.close(); db.Close() })
	f := &photoFake{reply: func(int, types.JID, string, func(string) string) (string, error) { return "", nil }}
	return newPhotoQueue(st, func() photoFetch { return f.fetch }), st, f, ts.login(t)
}

// addChat writes a chat, with last_ts ts, and returns when the op has run.
func addChat(st *Store, key string, kind int, ts int64) {
	done := make(chan struct{})
	st.ops <- func(t *storeTx) {
		defer close(done)
		t.ensureChat(key, kind)
		t.setLast(key, ts, "x", false, stateRead)
	}
	<-done
}

func photoOf(st *Store, key string) (p string) {
	var s *string
	st.db.QueryRow("SELECT photo FROM chats WHERE jid = ?", key).Scan(&s)
	if s != nil {
		p = *s
	}
	return
}

func waitIdle(t *testing.T, q *photoQueue) {
	t.Helper()
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		q.mu.Lock()
		idle := !q.running && len(q.todo) == 0
		q.mu.Unlock()
		if idle {
			return
		}
	}
	t.Fatal("the photo worker is still busy")
}

func waitCalls(t *testing.T, f *photoFake, n int) {
	t.Helper()
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if len(f.got()) >= n {
			return
		}
	}
	t.Fatalf("%d requests, want %d", len(f.got()), n)
}

func waitGone(t *testing.T, path string) {
	t.Helper()
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return
		}
	}
	t.Fatalf("%s is still there", path)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func wantCalls(t *testing.T, f *photoFake, want ...string) {
	t.Helper()
	if got := f.got(); !reflect.DeepEqual(got, want) {
		t.Fatalf("requests %q, want %q", got, want)
	}
}

func TestPhotoPreview(t *testing.T) {
	q, st, f, c := newPhotoTest(t)
	key := "111@lid"
	addChat(st, key, chatUser, 1)
	next(t, c, "chats") // the chat's own

	// A new picture: its file, the column, and a chats event.
	f.reply = func(_ int, _ types.JID, _ string, dst func(string) string) (string, error) {
		return newPic(dst, "7"), nil
	}
	q.add(key)
	waitIdle(t, q)
	p7 := q.path("", key, "7")
	if got := photoOf(st, key); got != p7 || !exists(p7) || !filepath.IsAbs(got) {
		t.Fatalf("after a new picture: column %q, file %v", got, exists(p7))
	}
	if m := next(t, c, "chats"); !reflect.DeepEqual(m["jids"], []any{key}) {
		t.Errorf("chats event: %v", m)
	}

	// Unchanged: it is asked with the stored ID, and nothing moves.
	f.reply = func(int, types.JID, string, func(string) string) (string, error) { return "", nil }
	q.add(key)
	waitIdle(t, q)
	wantCalls(t, f, key+"[]", key+"[7]")
	if photoOf(st, key) != p7 || !exists(p7) {
		t.Error("an unchanged picture moved")
	}

	// A new one replaces the file and the old one goes.
	f.reply = func(_ int, _ types.JID, _ string, dst func(string) string) (string, error) {
		return newPic(dst, "8"), nil
	}
	q.add(key)
	waitIdle(t, q)
	p8 := q.path("", key, "8")
	if photoOf(st, key) != p8 || !exists(p8) {
		t.Fatalf("after a changed picture: column %q", photoOf(st, key))
	}
	waitGone(t, p7)

	// A file that went missing is asked for from scratch.
	os.Remove(p8)
	q.add(key)
	waitIdle(t, q)
	if got := f.got(); got[len(got)-1] != key+"[]" || !exists(p8) {
		t.Errorf("after the file went: %q, file %v", got, exists(p8))
	}

	// Hidden: the column clears and the file goes. So does not set, even wrapped.
	f.reply = func(int, types.JID, string, func(string) string) (string, error) {
		return "", fmt.Errorf("query: %w", whatsmeow.ErrProfilePictureUnauthorized)
	}
	q.add(key)
	waitIdle(t, q)
	if photoOf(st, key) != "" {
		t.Errorf("hidden: column %q", photoOf(st, key))
	}
	waitGone(t, p8)
	f.reply = func(int, types.JID, string, func(string) string) (string, error) {
		return "", whatsmeow.ErrProfilePictureNotSet
	}
	q.add(key)
	waitIdle(t, q)
	if photoOf(st, key) != "" {
		t.Errorf("not set: column %q", photoOf(st, key))
	}
	if n, ok := q.current(key); n != "" || !ok {
		t.Errorf("current: %q %v", n, ok)
	}
}

func TestPhotoMembers(t *testing.T) {
	q, st, f, c := newPhotoTest(t)
	addChat(st, "1@lid", chatUser, 1)
	q.set("1@lid", newPic(func(id string) string { return q.path("", "1@lid", id) }, "3"))
	chatPic := photoOf(st, "1@lid")
	f.reply = func(_ int, jid types.JID, _ string, dst func(string) string) (string, error) {
		if jid.User == "3" {
			return "", whatsmeow.ErrProfilePictureNotSet
		}
		return newPic(dst, "9"), nil
	}
	group := func() []participant {
		return []participant{{JID: "1@lid"}, {JID: "2@lid"}, {JID: "3@lid"}, {JID: "4@lid", Me: true}}
	}

	// A member with a chat has the chat's picture and is not asked; ours is the profile's. The
	// others are asked once each, in order, and a picture is announced, a lack of one is not.
	me := newPic(func(string) string { return profilePath(st.srv.dir) }, "")
	ps := group()
	q.members(ps)
	waitIdle(t, q)
	wantCalls(t, f, "2@lid[]", "3@lid[]")
	p9 := q.path("members", "2@lid", "9")
	if m := next(t, c, "photo"); m["jid"] != "2@lid" || m["photo"] != p9 || !exists(p9) {
		t.Errorf("photo event: %v, file %v", m, exists(p9))
	}
	if ps[0].Photo != chatPic || ps[3].Photo != me || chatPic == "" {
		t.Errorf("chat's %q, ours %q", ps[0].Photo, ps[3].Photo)
	}
	if !exists(q.memberNone("3@lid")) || exists(q.memberNone("2@lid")) {
		t.Error("the none mark: for the member with no picture only")
	}

	// Again right away: the files answer, nobody is asked.
	ps = group()
	q.members(ps)
	waitIdle(t, q)
	wantCalls(t, f, "2@lid[]", "3@lid[]")
	if ps[1].Photo != p9 || ps[2].Photo != "" {
		t.Errorf("cached: %q, %q", ps[1].Photo, ps[2].Photo)
	}

	// A week on they are asked again, with the stored ID; an unchanged picture is renewed, not announced.
	old := time.Now().Add(-memberPhotoAge - time.Hour)
	os.Chtimes(p9, old, old)
	os.Chtimes(q.memberNone("3@lid"), old, old)
	f.reply = func(_ int, jid types.JID, _ string, _ func(string) string) (string, error) {
		if jid.User == "3" {
			return "", whatsmeow.ErrProfilePictureNotSet
		}
		return "", nil
	}
	q.members(group())
	waitIdle(t, q)
	wantCalls(t, f, "2@lid[]", "3@lid[]", "2@lid[9]", "3@lid[]")
	if _, seen := q.memberCache("2@lid"); time.Since(seen) > time.Minute || !exists(p9) {
		t.Error("the unchanged picture wasn't renewed")
	}
	if _, seen := q.memberCache("3@lid"); time.Since(seen) > time.Minute {
		t.Error("the none mark wasn't renewed")
	}
	(*photoQueue)(nil).members(group()) // a Session built bare has none
}

func TestPhotoQueue(t *testing.T) {
	q, st, f, _ := newPhotoTest(t)
	for k, c := range map[string]struct {
		kind int
		ts   int64
	}{
		"a@lid": {chatUser, 10}, "b@s.whatsapp.net": {chatUser, 30}, "g-1@g.us": {chatGroup, 20},
		"status@broadcast": {chatBroadcast, 40}, "c@newsletter": {chatNewsletter, 50},
	} {
		addChat(st, k, c.kind, c.ts)
	}

	// Queued jids are asked once, one request at a time; chats that don't exist, or that
	// aren't a person, a group or a channel, are never asked.
	gate := make(chan struct{})
	f.reply = func(n int, _ types.JID, _ string, _ func(string) string) (string, error) {
		if n == 1 {
			<-gate
		}
		return "", nil
	}
	q.add("a@lid")
	waitCalls(t, f, 1)
	q.add("b@s.whatsapp.net", "g-1@g.us", "b@s.whatsapp.net", "ghost@lid", "g-1@g.us", "status@broadcast", "c@newsletter")
	q.mu.Lock()
	queued := len(q.todo)
	q.mu.Unlock()
	if queued != 5 {
		t.Errorf("%d queued, want 5 (b, g, ghost, status, newsletter)", queued)
	}
	close(gate)
	waitIdle(t, q)
	wantCalls(t, f, "a@lid[]", "b@s.whatsapp.net[]", "g-1@g.us[]", "c@newsletter[]")
	if f.max != 1 {
		t.Errorf("%d requests ran at once", f.max)
	}

	// A round asks the people, groups and channels, the newest chat first.
	f.mu.Lock()
	f.calls = nil
	f.mu.Unlock()
	q.roundNow()
	waitIdle(t, q)
	wantCalls(t, f, "c@newsletter[]", "b@s.whatsapp.net[]", "g-1@g.us[]", "a@lid[]")
}

func TestPhotoRateLimit(t *testing.T) {
	q, st, f, _ := newPhotoTest(t)
	for i, k := range []string{"a@lid", "b@lid", "c@lid", "d@lid"} {
		addChat(st, k, chatUser, int64(10-i))
	}
	f.reply = func(n int, _ types.JID, _ string, dst func(string) string) (string, error) {
		switch n {
		case 2:
			return "", fmt.Errorf("boom") // one chat's failure doesn't end the round
		case 3:
			return "", &whatsmeow.IQError{Code: 429, Text: "rate-overlimit"}
		}
		return "", nil
	}
	q.roundNow()
	waitIdle(t, q)
	wantCalls(t, f, "a@lid[]", "b@lid[]", "c@lid[]") // d waits for the next round
	f.reply = func(int, types.JID, string, func(string) string) (string, error) { return "", nil }
	q.add("d@lid")
	waitIdle(t, q)
	wantCalls(t, f, "a@lid[]", "b@lid[]", "c@lid[]", "d@lid[]")
}

func TestPhotoConnectedGap(t *testing.T) {
	q, st, f, _ := newPhotoTest(t)
	addChat(st, "a@lid", chatUser, 1)
	q.connected()
	waitCalls(t, f, 1)
	waitIdle(t, q)
	q.connected() // too soon after the last
	time.Sleep(100 * time.Millisecond)
	waitIdle(t, q)
	wantCalls(t, f, "a@lid[]")
	q.mu.Lock()
	q.last = time.Now().Add(-photoRoundGap - time.Second)
	q.mu.Unlock()
	q.connected()
	waitCalls(t, f, 2)
	waitIdle(t, q)
	(*photoQueue)(nil).connected() // a Session built bare has none
	(*photoQueue)(nil).add("x")
}

func TestPhotoFull(t *testing.T) {
	q, _, f, _ := newPhotoTest(t)
	jid, _ := types.ParseJID("1203-5678@g.us")
	pic := func(id string) func(int, types.JID, string, func(string) string) (string, error) {
		return func(_ int, _ types.JID, _ string, dst func(string) string) (string, error) {
			return newPic(dst, id), nil
		}
	}
	f.reply = pic("5")
	p5 := q.full(jid)
	if want := filepath.Join(q.dir, "full", "1203-5678@g.us-5.jpg"); p5 != want || !exists(p5) {
		t.Fatalf("first: %q, want %q", p5, want)
	}
	f.reply = func(int, types.JID, string, func(string) string) (string, error) { return "", nil }
	if got := q.full(jid); got != p5 {
		t.Errorf("unchanged: %q", got)
	}
	f.reply = pic("6")
	p6 := q.full(jid)
	if p6 == p5 || !exists(p6) || exists(p5) {
		t.Errorf("changed: %q, old file %v", p6, exists(p5))
	}
	f.reply = func(int, types.JID, string, func(string) string) (string, error) { return "", fmt.Errorf("offline") }
	if got := q.full(jid); got != p6 || !exists(p6) {
		t.Errorf("a failure keeps the stored picture: %q", got)
	}
	f.reply = func(int, types.JID, string, func(string) string) (string, error) {
		return "", whatsmeow.ErrProfilePictureNotSet
	}
	if got := q.full(jid); got != "" || exists(p6) {
		t.Errorf("not set: %q, file %v", got, exists(p6))
	}
	wantCalls(t, f, "1203-5678@g.us[]", "1203-5678@g.us[5]", "1203-5678@g.us[5]", "1203-5678@g.us[6]", "1203-5678@g.us[6]")
	if got := (*photoQueue)(nil).full(jid); got != "" {
		t.Errorf("no queue: %q", got)
	}
}

func TestPictureID(t *testing.T) {
	for path, want := range map[string]string{
		"/d/photos/111@lid-7.jpg":              "7",
		"/d/photos/full/1203-5678@g.us-99.jpg": "99",
		"/d/photos/x.jpg":                      "",
		"":                                     "",
	} {
		if got := pictureID(path); got != want {
			t.Errorf("pictureID(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestUnlinkWipesPhotos(t *testing.T) {
	q, st, _, _ := newPhotoTest(t)
	newPic(func(id string) string { return q.path("", "a@lid", id) }, "1")
	newPic(func(id string) string { return q.path("full", "a@lid", id) }, "1")
	st.clear()
	waitGone(t, q.dir)
}

// The address book's rows carry the picture a member would have: the chat's, else the fetched one, and a missing one is
// fetched and announced.
func TestContactPhotos(t *testing.T) {
	q, st, f, c := newPhotoTest(t)
	addChat(st, "1@lid", chatUser, 1)
	q.set("1@lid", newPic(func(id string) string { return q.path("", "1@lid", id) }, "3"))
	chatPic := photoOf(st, "1@lid")
	f.reply = func(_ int, _ types.JID, _ string, dst func(string) string) (string, error) {
		return newPic(dst, "9"), nil
	}
	out := []contact{{JID: "1@lid", Name: "A"}, {JID: "2@lid", Name: "B"}}
	withPhotos(q, out)
	waitIdle(t, q)
	if out[0].Photo != chatPic || chatPic == "" || out[1].Photo != "" {
		t.Errorf("rows %+v, want the chat's picture for the first and none yet for the second", out)
	}
	if m := next(t, c, "photo"); m["jid"] != "2@lid" {
		t.Errorf("photo event %v, want one for the contact with no chat", m)
	}
	withPhotos(nil, out) // a bare Session has no queue
}
