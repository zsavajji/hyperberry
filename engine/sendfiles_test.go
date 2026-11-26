package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// settle waits until what the store was given has committed and its after-commit work (file removals) has run.
func settle(st *Store) {
	done := make(chan struct{})
	st.afterCommit(func() { close(done) })
	<-done
}

func writeTo(p string) string {
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte("x"), 0o644)
	return p
}

// A media send keeps its file when it is queued, connection or not: the engine's own link to it (a copy where it can't link),
// as the row's path, with the file it stands for in extra.original: the one the caller gave, else the path it was sent with.
func TestMediaSendKeepsItsFile(t *testing.T) {
	s, st := offlineSession(t)
	chat, dir, tmp := retryChat, s.srv.dir, t.TempDir()
	st.runOp(func(tx *storeTx) { tx.ensureChat(chat, chatUser) })
	orig, uiOrig := writeTo(filepath.Join(tmp, "p.jpg")), writeTo(filepath.Join(tmp, "ui.jpg"))
	uiCopy := writeTo(filepath.Join(dir, "outbox", "U1", "ui.jpg"))
	for _, body := range []string{
		`{"chat":"` + chat + `","id":"E1","path":"` + orig + `"}`,
		`{"chat":"` + chat + `","id":"U1","path":"` + uiCopy + `","original":"` + uiOrig + `"}`,
	} {
		if _, err := s.handleMediaSend(json.RawMessage(body)); err != nil {
			t.Fatal(err)
		}
	}
	for id, want := range map[string]string{"E1": orig, "U1": uiOrig} {
		row, _ := st.row(chat, id)
		var x struct{ Original string }
		json.Unmarshal([]byte(row.Extra), &x)
		if row.Path != filepath.Join(outboxDir(dir), id+".jpg") || x.Original != want || !exists(row.Path) {
			t.Errorf("%s: path %q, extra %q; want the kept file and the original %q", id, row.Path, row.Extra, want)
		}
	}
	// A link where the file system has them: the kept file is the original's inode, so no space is spent on it.
	if a, b := fileInfo(t, orig), fileInfo(t, filepath.Join(outboxDir(dir), "E1.jpg")); !os.SameFile(a, b) {
		t.Error("the kept file is a copy, not a link")
	}
	// Where a link can't be made (a kept file is there already, or the files are on two file systems) the file is copied.
	os.WriteFile(orig, []byte("data"), 0o644)
	writeTo(filepath.Join(outboxDir(dir), "L1.jpg"))
	f := s.files()
	got, err := f.keep("L1", orig, []byte{7})
	if data, _ := os.ReadFile(got); err != nil || string(data) != "data" || os.SameFile(fileInfo(t, orig), fileInfo(t, got)) || !exists(posterPath(dir, "L1")) {
		t.Errorf("keep over a leftover: %q %q, %v", got, data, err)
	}
	if _, err := f.keep("L2", filepath.Join(tmp, "nope.jpg"), nil); err == nil {
		t.Error("keep of a file that isn't there")
	}
	if _, err := s.handleMediaSend(json.RawMessage(`{"chat":"` + chat + `","path":"/x","original":"rel"}`)); code(err) != "bad_request" {
		t.Errorf("a relative original: %v", err)
	}
}

func fileInfo(t *testing.T, p string) os.FileInfo {
	t.Helper()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi
}

// released(sent): with the original still there the row's path goes back to it, its key goes, and the kept file and the
// poster go, and a forward made while the send waited, which had the kept path, follows; with the original gone, in the
// cache, or not recorded (a row from before it was kept) the kept file stays, for the bubble. Unsendable, or sent from a file
// that isn't ours, only the poster goes.
func TestReleased(t *testing.T) {
	s, st := offlineSession(t)
	chat, other, dir, tmp := retryChat, "4915700000008@s.whatsapp.net", s.srv.dir, t.TempDir()
	st.runOp(func(tx *storeTx) { tx.ensureChat(chat, chatUser); tx.ensureChat(other, chatUser) })
	for _, c := range []struct {
		id   string
		orig string // "here", "gone", "cache" (here, under cache/), or "" for a row that has none
		sent bool
		back bool // the path is the original afterwards, and the kept file is gone
	}{
		{"A", "here", true, true},
		{"B", "gone", true, false},
		{"C", "cache", true, false},
		{"D", "", true, false},
		{"E", "here", false, false}, // unsendable: the file stays for the bubble
	} {
		orig := filepath.Join(tmp, c.id+".jpg")
		if c.orig == "cache" {
			orig = filepath.Join(dir, "cache", "convert", c.id+".jpg")
		}
		if c.orig == "here" || c.orig == "cache" {
			writeTo(orig)
		}
		kept := writeTo(filepath.Join(outboxDir(dir), c.id+".jpg"))
		poster := writeTo(posterPath(dir, c.id))
		row := msgRow{Chat: chat, ID: c.id, TS: 1, Out: true, State: stateSending, Kind: msgPhoto, Path: kept}
		if c.orig != "" {
			row.Extra = withOriginal(`{"gif":true}`, orig)
		}
		st.runOp(func(tx *storeTx) {
			tx.insertMessage(row)
			tx.insertMessage(msgRow{Chat: other, ID: "FW" + c.id, TS: 1, Out: true, State: stateSending, Kind: msgPhoto, Path: kept}) // a forward of it
			s.files().released(tx, row, c.sent)
		})
		settle(st)
		got, _ := st.row(chat, c.id)
		fw, _ := st.row(other, "FW"+c.id)
		if c.back {
			if got.Path != orig || fw.Path != orig || got.Extra != `{"gif":true}` || exists(kept) || !exists(orig) {
				t.Errorf("%s: path %q, forward %q, extra %q, kept file there %v", c.id, got.Path, fw.Path, got.Extra, exists(kept))
			}
		} else if got.Path != kept || fw.Path != kept || !exists(kept) || (c.orig != "" && !strings.Contains(got.Extra, orig)) {
			t.Errorf("%s: path %q, forward %q, extra %q, kept file there %v; want it all left", c.id, got.Path, fw.Path, got.Extra, exists(kept))
		}
		if exists(poster) {
			t.Errorf("%s: the poster is still there", c.id)
		}
		if c.orig == "here" && !exists(orig) {
			t.Errorf("%s: the original went", c.id)
		}
	}

	// A path that isn't the engine's (a forward's, a user's file) is never removed, whatever extra says.
	user := writeTo(filepath.Join(tmp, "mine.jpg"))
	row := msgRow{Chat: chat, ID: "U", TS: 1, Out: true, Kind: msgPhoto, Path: user, Extra: withOriginal("", user)}
	st.runOp(func(tx *storeTx) { tx.insertMessage(row); s.files().released(tx, row, true) })
	settle(st)
	if !exists(user) {
		t.Error("the user's own file went")
	}
}

// Deleting one of our rows (for me, clear chat, delete chat, msg.cancel) takes the files of its send that are ours alone: the
// copy in outbox-engine/, the UI outbox's outbox/<id>/ (whether or not the row's path is in it), the video's poster, a voice
// note's OGG. A file elsewhere is the user's and stays, and so does one another row has too.
func TestDeleteDropsSendFiles(t *testing.T) {
	s, st := offlineSession(t)
	chat, other, dir, tmp := retryChat, "4915700000008@s.whatsapp.net", s.srv.dir, t.TempDir()
	write := func(p string) string {
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("x"), 0o644)
		return p
	}
	user := write(filepath.Join(tmp, "mine.jpg"))
	ui := func(id string) string { return filepath.Join(dir, "outbox", id) }
	type row struct {
		id, path string
		kind     int
		out      bool
	}
	rowsOf := []row{
		{"F1", filepath.Join(outboxDir(dir), "F1.mp4"), msgVideo, true},  // the engine's copy, the UI's too, a poster
		{"F2", filepath.Join(ui("F2"), "p.jpg"), msgPhoto, true},         // the UI's copy
		{"F3", user, msgVideo, true},                                     // the user's own file, and a poster
		{"F4", "", msgVoice, true},                                       // an OGG
		{"F5", filepath.Join(outboxDir(dir), "F5.jpg"), msgPhoto, true},  // a forward in another chat has it too
		{"F6", filepath.Join(outboxDir(dir), "F6.jpg"), msgPhoto, false}, // not ours
		{"F7", filepath.Join(ui("F8"), "x.jpg"), msgPhoto, true},         // another message's directory
	}
	ours := []string{rowsOf[0].path, ui("F1"), posterPath(dir, "F1"), ui("F2"), posterPath(dir, "F3"), oggPath(dir, "F4")} // all gone
	kept := []string{user, rowsOf[4].path, rowsOf[5].path, rowsOf[6].path}
	build := func() {
		for _, r := range rowsOf {
			if r.path != "" && r.path != user {
				write(r.path)
			}
		}
		write(filepath.Join(ui("F1"), "p.mp4"))
		write(posterPath(dir, "F1"))
		write(posterPath(dir, "F3"))
		write(oggPath(dir, "F4"))
		st.runOp(func(tx *storeTx) {
			tx.ensureChat(chat, chatUser)
			tx.ensureChat(other, chatUser)
			for i, r := range rowsOf {
				tx.insertMessage(msgRow{Chat: chat, ID: r.id, TS: int64(i + 1), Out: r.out, State: stateFailed, Kind: r.kind, Path: r.path})
			}
			tx.insertMessage(msgRow{Chat: other, ID: "FW5", TS: 1, Out: true, State: stateSent, Kind: msgPhoto, Path: rowsOf[4].path})
		})
	}
	check := func(t *testing.T, how string) {
		t.Helper()
		settle(st)
		for _, p := range ours {
			if exists(p) {
				t.Errorf("%s: %s is still there", how, p)
			}
		}
		for _, p := range kept {
			if !exists(p) {
				t.Errorf("%s: %s went, and isn't ours alone", how, p)
			}
		}
		if n := rows(st, "SELECT count(*) FROM messages WHERE chat = ?", chat); n != 0 {
			t.Errorf("%s: %d rows left", how, n)
		}
	}

	// Delete for me: the handler tells our other devices too, which fails offline and is only logged.
	build()
	var ids []string
	for _, r := range rowsOf {
		ids = append(ids, `"`+r.id+`"`)
	}
	if _, err := s.handleDelete(json.RawMessage(`{"chat":"` + chat + `","ids":[` + strings.Join(ids, ",") + `]}`)); err != nil {
		t.Fatal(err)
	}
	check(t, "delete for me")

	// Clear chat and delete chat.
	for _, drop := range []bool{false, true} {
		build()
		st.runOp(func(tx *storeTx) { tx.clearChat(chat, drop) })
		check(t, map[bool]string{false: "clear chat", true: "delete chat"}[drop])
	}
}
