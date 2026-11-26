package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// The files of our sends have one owner. A media send keeps its file in <data dir>/outbox-engine/<id><ext> from the moment
// it is queued: a hard link to the file it was given (a copy where a link can't be made), so the send doesn't depend on the
// original staying, and a rebuild after a restart finds it. The row's path is the kept file, the bubble's picture until the
// send is over; extra.original is the file it stands for. The files go in two places only: released, when the send is over,
// and dropped, when the row is deleted.

// sendFiles is the owner: dir is the data dir.
type sendFiles struct{ dir string }

func (s *Session) files() sendFiles { return sendFiles{s.srv.dir} }

// outboxDir is where the engine keeps the files its sends wait with.
func outboxDir(dir string) string {
	d, _ := filepath.Abs(filepath.Join(dir, "outbox-engine"))
	return d
}

// posterPath is where a video's poster waits with the send, for a rebuild after a restart: the row keeps none.
func posterPath(dir, id string) string { return filepath.Join(outboxDir(dir), id+".poster") }

// uiOutboxDir is where the UI outbox keeps the copy of the file of its media.send: <data dir>/outbox/<id>/.
func uiOutboxDir(dir, id string) string {
	return filepath.Join(filepath.Dir(outboxDir(dir)), "outbox", id)
}

// withOriginal sets the file a media row's path stands for in its extra ("original"), keeping what else is there. Its
// readers look for their own keys by name, so it doesn't change what they see.
func withOriginal(extra, original string) string {
	b, err := json.Marshal(original)
	if err != nil {
		return extra
	}
	return withKey(extra, "original", string(b))
}

// keep makes the send's file: id's link to src, or a copy of it, and a video's poster beside it. It returns the path of the
// kept file, which the row takes. A poster that can't be written is a video that goes without one.
func (f sendFiles) keep(id, src string, poster []byte) (path string, err error) {
	path = filepath.Join(outboxDir(f.dir), id+filepath.Ext(src))
	if err = os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if os.Link(src, path) != nil { // another file system, or none that links
		if err = copyFile(src, path); err != nil {
			return "", err
		}
	}
	if len(poster) > 0 {
		writeFile(posterPath(f.dir, id), poster)
	}
	return path, nil
}

// released runs in the op that ends a send, with the row as it was read: sent, or unsendable (it can't be rebuilt). Sent,
// the poster goes, and with the original still there (and not in the cache, which is cleared and its names reused: as good
// as gone) the row's path goes back to it and the kept file goes; a forward of this message made while it waited carries the
// kept path too, and follows. Without the original the kept file stays, as the only one the bubble can show. Unsendable,
// the poster goes and the file stays for the bubble. A send that failed keeps both: msg.retry sends it again.
func (f sendFiles) released(t *storeTx, row msgRow, sent bool) {
	gone := []string{posterPath(f.dir, row.ID)}
	var x struct{ Original string }
	json.Unmarshal([]byte(row.Extra), &x) // "" or not ours: no original
	cache := filepath.Join(filepath.Dir(outboxDir(f.dir)), "cache") + string(filepath.Separator)
	if _, err := os.Stat(x.Original); sent && err == nil && x.Original != "" && !strings.HasPrefix(x.Original, cache) && filepath.Dir(row.Path) == outboxDir(f.dir) {
		t.exec("UPDATE messages SET path = ? WHERE path = ?", x.Original, row.Path)
		t.exec("UPDATE messages SET extra = NULLIF(json_remove(extra, '$.original'), '{}') WHERE chat = ? AND id = ? AND json_valid(extra)",
			row.Chat, row.ID) // the path is the original again: nothing stands for anything
		gone = append(gone, row.Path)
	}
	t.after = append(t.after, func() {
		for _, p := range gone {
			os.Remove(p)
		}
	})
}

// dropped runs where our row is deleted (deleteMessage, clearChat): once the transaction has committed, the files of its send
// that are ours alone go. Those are its path when it lies in outbox-engine/, the UI outbox's outbox/<id>/ with its copy
// (which the row's path need not point to: the engine's copy replaced it), a video's poster, and a voice note's OGG; never a
// file elsewhere, which is the user's own photo. What another row has too (a forward of this message carries its path) stays.
func (f sendFiles) dropped(t *storeTx, row msgRow) {
	if !row.Out {
		return
	}
	held := func(prefix string) bool { // a row still points there
		var n int
		return t.tx.QueryRow("SELECT 1 FROM messages WHERE instr(path, ?) = 1 LIMIT 1", prefix).Scan(&n) == nil
	}
	gone := []string{posterPath(f.dir, row.ID)}
	if row.Kind == msgVoice {
		gone = append(gone, oggPath(f.dir, row.ID))
	}
	if row.Path != "" && filepath.Dir(row.Path) == outboxDir(f.dir) && !held(row.Path) {
		gone = append(gone, row.Path)
	}
	ui := uiOutboxDir(f.dir, row.ID)
	if _, err := os.Stat(ui); err != nil || held(ui+string(filepath.Separator)) { // held scans the table: only for a directory that is there
		ui = ""
	}
	t.after = append(t.after, func() {
		for _, p := range gone {
			os.Remove(p)
		}
		if ui != "" {
			os.RemoveAll(ui)
		}
	})
}
