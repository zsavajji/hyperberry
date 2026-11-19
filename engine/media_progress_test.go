package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	waLog "github.com/polymorfa/hypermeow/util/log"
)

// The bytes of a download are counted through io.Copy (hypermeow's way in, which goes by ReadFrom on a bare *os.File),
// and what is reported never goes above the size.
func TestProgressFile(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "x.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var got [][2]int64
	p := &progressFile{File: f, size: 100, emit: func(done, size int64) { got = append(got, [2]int64{done, size}) }}
	// 150 bytes against a size of 100: the encrypted file is a little longer than the decrypted one.
	if n, err := io.Copy(p, struct{ io.Reader }{bytes.NewReader(make([]byte, 150))}); n != 150 || err != nil {
		t.Fatalf("copied %d, %v", n, err)
	}
	if p.done != 150 {
		t.Errorf("counted %d bytes, want 150", p.done)
	}
	if len(got) == 0 {
		t.Fatal("nothing was reported")
	}
	for _, g := range got {
		if g[0] > 100 || g[1] != 100 {
			t.Errorf("reported %v of a size of 100", g)
		}
	}
	if st, _ := f.Stat(); st.Size() != 150 {
		t.Errorf("file holds %d bytes", st.Size())
	}
}

// A cancelled fetch is "cancelled", not "download_failed", and leaves the row's media_state at 1: a tap fetches it again.
func TestMediaGetCancelled(t *testing.T) {
	ts := newTestServer(t)
	db, err := openChats(filepath.Join(t.TempDir(), "chats.db"))
	if err != nil {
		t.Fatal(err)
	}
	st := newStore(db, ts.Server, waLog.Noop)
	t.Cleanup(func() { st.close(); db.Close() })
	st.ops <- func(tx *storeTx) {
		tx.ensureChat("a@s.whatsapp.net", chatUser)
		tx.insertMessage(msgRow{Chat: "a@s.whatsapp.net", ID: "1", TS: 1, Kind: msgVideo, MediaState: 1, Media: "{}"})
	}
	dir := t.TempDir()
	r := mediaReq{"a@s.whatsapp.net", "1"}

	_, err = st.mediaGet(dir, r, func(row msgRow) (string, error) {
		return st.fetchMedia(dir, row, func(f *os.File) (string, error) {
			return "", fmt.Errorf("download: %w", context.Canceled)
		})
	})
	var e *Error
	if !errors.As(err, &e) || e.Code != "cancelled" {
		t.Errorf("error %v, want code cancelled", err)
	}
	if row, _ := st.row(r.Chat, r.ID); row.MediaState != 1 {
		t.Errorf("media_state %d after a cancel, want 1", row.MediaState)
	}

	// Any other failure is download_failed, and the row is marked failed.
	_, err = st.mediaGet(dir, r, func(row msgRow) (string, error) {
		return st.fetchMedia(dir, row, func(f *os.File) (string, error) { return "", errors.New("lost") })
	})
	if !errors.As(err, &e) || e.Code != "download_failed" {
		t.Errorf("error %v, want code download_failed", err)
	}
	if row, _ := st.row(r.Chat, r.ID); row.MediaState != 3 {
		t.Errorf("media_state %d after a failure, want 3", row.MediaState)
	}
}

// An async handler doesn't hold up the requests behind it: a second one is answered while the first still waits.
func TestHandleAsync(t *testing.T) {
	ts := newTestServer(t)
	release := make(chan struct{})
	ts.HandleAsync("slow", func(json.RawMessage) (any, error) { <-release; return "done", nil })
	c := ts.login(t)
	c.send(t, `{"@":"1","cmd":"slow"}`)
	c.send(t, `{"@":"2","cmd":"echo","x":"fast"}`)
	if m := c.read(t); m["@"] != "2" || m["result"] != "fast" {
		t.Errorf("the request behind a running async one: %v", m)
	}
	close(release)
	if m := c.read(t); m["@"] != "1" || m["result"] != "done" {
		t.Errorf("the async reply: %v", m)
	}
}
