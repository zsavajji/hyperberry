package main

import (
	"bytes"
	"errors"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"
)

// A download lands under its final name only when whole; whatever way it ends, no .tmp stays.
func TestDownloadTo(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "sub", "X.tmp")
	path, err := downloadTo(tmp, func(f *os.File) (string, error) {
		f.WriteString("hello")
		return "video/mp4", nil
	}, func(m string) string { return filepath.Join(dir, "sub", "X."+strings.TrimPrefix(m, "video/")) })
	if want := filepath.Join(dir, "sub", "X.mp4"); err != nil || path != want {
		t.Fatalf("path %q, %v, want %s", path, err, want)
	}
	if b, _ := os.ReadFile(path); string(b) != "hello" {
		t.Errorf("file: %q", b)
	}
	if _, err := os.Stat(tmp); err == nil {
		t.Error("the .tmp is still there after a good download")
	}

	// Half the bytes, then an error: nothing at all remains.
	failed := filepath.Join(dir, "sub", "Y.mp4")
	_, err = downloadTo(filepath.Join(dir, "sub", "Y.tmp"), func(f *os.File) (string, error) {
		f.WriteString("hal")
		return "", errors.New("connection lost")
	}, func(string) string { return failed })
	if err == nil {
		t.Fatal("the failure was swallowed")
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "sub", "Y*")); len(left) != 0 {
		t.Errorf("a failed download left %v", left)
	}
}

// The sim's photo is drawn in YCbCr planes, not RGBA: 2.9 MB where RGBA took 7.7.
func TestSimGradientSize(t *testing.T) {
	img := simGradient(simPhotoW, simPhotoH, "SIMH1.4")
	if n := len(img.Y) + len(img.Cb) + len(img.Cr); n < 2_800_000 || n > 3_000_000 {
		t.Errorf("%d bytes of pixels", n)
	}
	a, b := simGradient(80, 60, "a").YCbCrAt(0, 0), simGradient(80, 60, "b").YCbCrAt(0, 0)
	if a == b {
		t.Error("two messages, one colour")
	}
}

func TestMediaMessageKinds(t *testing.T) {
	info := mediaInfo{DirectPath: "/p", MediaKey: []byte{1}, FileEncSHA256: []byte{2}, FileSHA256: []byte{3}, FileLength: 9, Mimetype: "x/y"}
	for kind, want := range map[int]whatsmeow.MediaType{msgPhoto: whatsmeow.MediaImage, msgVideo: whatsmeow.MediaVideo,
		msgDoc: whatsmeow.MediaDocument, msgAudio: whatsmeow.MediaAudio, msgVoice: whatsmeow.MediaAudio} {
		m := info.message(kind)
		if got := whatsmeow.GetMediaType(m); got != want {
			t.Errorf("kind %d: %s, want %s", kind, got, want)
		}
		if m.GetDirectPath() != "/p" || m.GetMediaKey()[0] != 1 || m.GetFileEncSHA256()[0] != 2 || m.GetFileSHA256()[0] != 3 {
			t.Errorf("kind %d: fields lost", kind)
		}
	}
}

func TestRowFromMedia(t *testing.T) {
	thumb := []byte{0xff, 0xd8, 1}
	tests := []struct {
		name  string
		m     *waE2E.Message
		kind  int
		text  string
		secs  int
		w, h  int
		thumb bool
		state int
		prev  string
	}{
		{"photo", &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("hi"), JPEGThumbnail: thumb, Width: proto.Uint32(800),
			Height: proto.Uint32(600), FileLength: proto.Uint64(5000), DirectPath: proto.String("/i"), Mimetype: proto.String("image/jpeg")}},
			msgPhoto, "hi", 0, 800, 600, true, 1, "Photo hi"},
		{"video", &waE2E.Message{VideoMessage: &waE2E.VideoMessage{JPEGThumbnail: thumb, Width: proto.Uint32(640), Height: proto.Uint32(360),
			Seconds: proto.Uint32(12), FileLength: proto.Uint64(5000), DirectPath: proto.String("/v")}},
			msgVideo, "", 12, 640, 360, true, 1, "Video"},
		{"document", &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{FileName: proto.String("a.pdf"), JPEGThumbnail: thumb,
			FileLength: proto.Uint64(5000), DirectPath: proto.String("/d"), Mimetype: proto.String("application/pdf")}},
			msgDoc, "a.pdf", 0, 0, 0, true, 1, "Document: a.pdf"},
		{"audio", &waE2E.Message{AudioMessage: &waE2E.AudioMessage{Seconds: proto.Uint32(31), FileLength: proto.Uint64(5000),
			DirectPath: proto.String("/a"), Mimetype: proto.String("audio/mpeg")}},
			msgAudio, "", 31, 0, 0, false, 1, "Audio"},
		{"photo, no path", &waE2E.Message{ImageMessage: &waE2E.ImageMessage{JPEGThumbnail: thumb, FileLength: proto.Uint64(5000)}},
			msgPhoto, "", 0, 0, 0, true, 0, "Photo"},
	}
	for _, tc := range tests {
		e := &events.Message{Message: tc.m, Info: types.MessageInfo{ID: "id", Timestamp: time.Unix(100, 0)}}
		r, ok := rowFromMessage("c", e, "")
		if !ok || r.Kind != tc.kind || r.Text != tc.text || r.Duration != tc.secs || r.Width != tc.w || r.Height != tc.h ||
			bytes.Equal(r.Thumb, thumb) != tc.thumb || r.Size != 5000 || r.MediaState != tc.state || chatPreview(r) != tc.prev {
			t.Errorf("%s: %+v", tc.name, r)
		}
		if (r.Media != "") != (tc.state == 1) || r.Path != "" {
			t.Errorf("%s: media %q, path %q", tc.name, r.Media, r.Path)
		}
	}
	// A voice note stays as it was: no thumbnail, no size.
	v := &events.Message{Message: &waE2E.Message{AudioMessage: &waE2E.AudioMessage{PTT: proto.Bool(true), Seconds: proto.Uint32(7),
		FileLength: proto.Uint64(5000), DirectPath: proto.String("/a")}}, Info: types.MessageInfo{ID: "v"}}
	if r, _ := rowFromMessage("c", v, ""); r.Kind != msgVoice || r.Duration != 7 || r.MediaState != 1 || r.Thumb != nil || r.Size != 0 {
		t.Errorf("voice: %+v", r)
	}
}

func TestMediaTarget(t *testing.T) {
	dir := t.TempDir()
	dl := downloadsDir(dir)
	doc := msgRow{ID: "M1", Kind: msgDoc, Text: "a/b.pdf"}
	if got := mediaTarget(dir, doc, ""); got != filepath.Join(dl, "a_b.pdf") {
		t.Errorf("document: %s", got)
	}
	writeFile(filepath.Join(dl, "a_b.pdf"), nil)
	if got := mediaTarget(dir, doc, ""); got != filepath.Join(dl, "M1-a_b.pdf") {
		t.Errorf("taken name: %s", got)
	}
	for text, want := range map[string]string{"": "M1.bin", "..": "M1.bin", " . ": "M1.bin"} {
		if got := mediaTarget(dir, msgRow{ID: "M1", Kind: msgDoc, Text: text}, "application/octet-stream"); got != filepath.Join(dl, want) {
			t.Errorf("document %q: %s", text, got)
		}
	}
	for _, tc := range []struct {
		kind int
		mime string
		want string
	}{{msgVideo, "video/mp4", "M1.mp4"}, {msgVideo, "", "M1.mp4"}, {msgAudio, "audio/mpeg", "M1.mp3"},
		{msgAudio, "audio/ogg; codecs=opus", "M1.ogg"}, {msgAudio, "weird/type", "M1.m4a"}, {msgVideo, "video/quicktime", "M1.mov"}} {
		if got := mediaTarget(dir, msgRow{ID: "M1", Kind: tc.kind}, tc.mime); got != filepath.Join(dl, tc.want) {
			t.Errorf("%d %q: %s, want %s", tc.kind, tc.mime, got, tc.want)
		}
	}
	if got := mediaTarget(dir, msgRow{ID: "P", Kind: msgPhoto}, "image/jpeg"); got != filepath.Join(dir, "media", "P.jpg") {
		t.Errorf("photo: %s", got)
	}
}

func TestMediaGetRejects(t *testing.T) {
	s := newUnlinked(t)
	if _, err := call(s.handleMediaGet, `{"chat":"a@s.whatsapp.net"}`); code(err) != "bad_request" {
		t.Errorf("no id: %v", err)
	}
	if _, err := call(s.handleMediaGet, `{"chat":"a@s.whatsapp.net","id":"X"}`); code(err) != "unlinked" {
		t.Errorf("unlinked: %v", err)
	}
}

// mediaOf reads the columns media.get changes.
func mediaOf(t *testing.T, st *Store, chat, id string) msgRow {
	t.Helper()
	r, ok := st.row(chat, id)
	if !ok {
		t.Fatalf("no row %s", id)
	}
	return r
}

func TestSimMediaGet(t *testing.T) {
	s, st, c := linkedSim(t)
	dir, _ := filepath.Abs(s.srv.dir)
	g := simChats[1].jid
	get := func(chat, id string) (string, error) {
		r, err := s.handleMediaGet([]byte(`{"chat":"` + chat + `","id":"` + id + `"}`))
		if err != nil {
			return "", err
		}
		return r.(map[string]string)["path"], nil
	}

	// Giorgia's photo: a thumbnail and no file, then the file; the row keeps its thumbnail.
	before := mediaOf(t, st, g, "SIMH1.4")
	if before.Kind != msgPhoto || before.MediaState != 1 || before.Width != 1600 || before.Height != 1200 || before.Size == 0 || len(before.Thumb) == 0 || before.Path != "" {
		t.Fatalf("seeded photo: %+v", before)
	}
	p, err := get(g, "SIMH1.4")
	if err != nil || p != filepath.Join(dir, "media", "SIMH1.4.jpg") {
		t.Fatalf("photo: %q, %v", p, err)
	}
	if img, err := jpeg.DecodeConfig(mustOpen(t, p)); err != nil || img.Width != 1600 || img.Height != 1200 {
		t.Errorf("photo file: %v, %v", img, err)
	}
	eventually(t, st, 1, "SELECT count(*) FROM messages WHERE chat = ? AND id = 'SIMH1.4' AND media_state = 2 AND path = ?", g, p)
	after := mediaOf(t, st, g, "SIMH1.4")
	if !bytes.Equal(after.Thumb, before.Thumb) || after.Width != 1600 || after.Height != 1200 {
		t.Errorf("the engine changed the thumbnail or the size: %+v", after)
	}
	for ok := false; !ok; { // the change reached the UI
		m := next(t, c, "msgs")
		for _, id := range m["ids"].([]any) {
			ok = ok || (m["chat"] == g && id == "SIMH1.4")
		}
	}
	// Here already: the same path, and no new download.
	old := time.Unix(1e9, 0)
	os.Chtimes(p, old, old)
	if p2, err := get(g, "SIMH1.4"); err != nil || p2 != p {
		t.Fatalf("second get: %q, %v", p2, err)
	}
	if fi, _ := os.Stat(p); !fi.ModTime().Equal(old) {
		t.Error("the second get downloaded again")
	}

	// Luca's recipe.pdf goes to Downloads under its own name.
	p, err = get(g, "SIMH1.12")
	if want := filepath.Join(dir, "downloads", "recipe.pdf"); err != nil || p != want {
		t.Fatalf("pdf: %q, %v, want %s", p, err, want)
	}
	if b, _ := os.ReadFile(p); !bytes.HasPrefix(b, []byte("%PDF-1.4")) || !bytes.Contains(b, []byte("(HyperBerry test) Tj")) || !bytes.HasSuffix(b, []byte("%%EOF\n")) {
		t.Errorf("pdf: %q", b)
	}
	if i := strings.LastIndex(string(simPDF), "startxref\n"); i < 0 || !strings.HasPrefix(string(simPDF[atoi(t, string(simPDF[i+10:])):]), "xref\n0 6\n") {
		t.Error("the PDF's startxref doesn't point at the xref table")
	}

	// The audio file in Elena's history: 20 s of WAV.
	e := simChats[simAudioChat].jid
	var aid string
	st.db.QueryRow("SELECT id FROM messages WHERE chat = ? AND kind = ?", e, msgAudio).Scan(&aid)
	a := mediaOf(t, st, e, aid)
	if a.Duration != 20 || a.MediaState != 1 || a.Out || a.Text != "" || a.Size != 640044 || chatPreview(a) != "Audio" {
		t.Fatalf("seeded audio: %+v", a)
	}
	p, err = get(e, aid)
	if want := filepath.Join(dir, "downloads", aid+".wav"); err != nil || p != want {
		t.Fatalf("audio: %q, %v, want %s", p, err, want)
	}
	b, _ := os.ReadFile(p)
	if pcm, info, err := readWAV(b); err != nil || info.rate != simRate || len(pcm) != 20*simRate || int64(len(b)) != a.Size {
		t.Errorf("wav: %d samples, %v, %v", len(pcm), info, err)
	}

	// A voice note answers with its OGG; what the sim can't make fails and says so.
	if p, err := get(g, "SIMH1.11"); err != nil || p != filepath.Join(dir, "voice", "SIMH1.11.ogg") {
		t.Errorf("voice: %q, %v", p, err)
	}
	st.ops <- func(t *storeTx) {
		t.insertMessage(msgRow{Chat: g, ID: "VID", TS: time.Now().Unix(), Kind: msgVideo, MediaState: 1})
	}
	eventually(t, st, 1, "SELECT count(*) FROM messages WHERE id = 'VID'")
	if _, err := get(g, "VID"); code(err) != "download_failed" {
		t.Errorf("video: %v", err)
	}
	eventually(t, st, 1, "SELECT count(*) FROM messages WHERE id = 'VID' AND media_state = 3")
	if left, _ := filepath.Glob(filepath.Join(downloadsDir(dir), "*.tmp")); len(left) != 0 {
		t.Errorf("a failed download left %v", left)
	}
	for id, want := range map[string]string{"nope": "not_found", "SIMH1.0": "not_found", "SIMH1.15": "not_found"} { // none; text; sticker
		if _, err := get(g, id); code(err) != want {
			t.Errorf("%s: %v, want %s", id, err, want)
		}
	}
	if _, err := s.handleMediaGet([]byte(`{"chat":"` + g + `"}`)); code(err) != "bad_request" {
		t.Errorf("no id: %v", err)
	}

	// Clearing the chat takes the photo, not Downloads' files; unlinking takes media/ whole.
	if _, err := s.handleChatClear([]byte(`{"chat":"` + g + `"}`)); err != nil {
		t.Fatal(err)
	}
	eventually(t, st, 0, "SELECT count(*) FROM messages WHERE chat = ?", g)
	for end := time.Now().Add(2 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if _, err := os.Stat(filepath.Join(dir, "media", "SIMH1.4.jpg")); os.IsNotExist(err) {
			break
		} else if time.Now().After(end) {
			t.Fatal("the cleared chat's photo is still there")
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "downloads", "recipe.pdf")); err != nil {
		t.Errorf("clearing the chat took a file out of Downloads: %v", err)
	}
	if _, err := get(simChats[8].jid, "x"); code(err) != "not_found" {
		t.Errorf("unknown row: %v", err)
	}
	writeFile(filepath.Join(dir, "media", "other.jpg"), []byte("x"))
	s.handleRemoved(nil)
	for end := time.Now().Add(2 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if _, err := os.Stat(filepath.Join(dir, "media")); os.IsNotExist(err) {
			break
		} else if time.Now().After(end) {
			t.Fatal("data/media is still there after unlinking")
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "downloads", "recipe.pdf")); err != nil {
		t.Errorf("unlinking took a file out of Downloads: %v", err)
	}
}

func TestSimPhotoLive(t *testing.T) {
	s, st, c := linkedSim(t)
	chat := simChats[2].jid
	eventually(t, st, 3, "SELECT unread FROM chats WHERE jid = ?", chat)
	if _, err := s.handlePhoto([]byte(`{"n":2}`)); err != nil {
		t.Fatal(err)
	}
	q := "SELECT count(*) FROM messages WHERE chat = ? AND kind = 1 AND text = 'A photo from the sim 📷' AND out = 0 AND size > 0 AND length(thumb) > 0 AND width = 1600 AND media_state = ?"
	eventually(t, st, 1, q, chat, 2)
	eventually(t, st, 4, "SELECT unread FROM chats WHERE jid = ?", chat)
	eventually(t, st, 1, "SELECT count(*) FROM chats WHERE jid = ? AND last_preview = 'Photo A photo from the sim 📷'", chat)
	var id, path string
	st.db.QueryRow("SELECT id, path FROM messages WHERE chat = ? AND kind = 1 AND text LIKE 'A photo%'", chat).Scan(&id, &path)
	if want, _ := filepath.Abs(photoPath(s.srv.dir, id)); path != want {
		t.Errorf("path %q, want %q", path, want)
	}
	if cfg, err := jpeg.DecodeConfig(mustOpen(t, path)); err != nil || cfg.Width != 1600 {
		t.Errorf("file: %v, %v", cfg, err)
	}
	if m := next(t, c, "msgs"); m["chat"] != chat {
		t.Errorf("msgs: %v", m)
	}
	if _, err := s.handlePhoto([]byte(`{"n":999}`)); code(err) != "bad_request" {
		t.Errorf("past the chats: %v", err)
	}
}

func mustOpen(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func atoi(t *testing.T, s string) int {
	t.Helper()
	n := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			break
		}
		n = n*10 + int(ch-'0')
	}
	return n
}
