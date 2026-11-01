package main

import (
	"bytes"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"github.com/polymorfa/hypermeow"
)

func TestSendKind(t *testing.T) {
	for path, want := range map[string]struct {
		kind int
		mime string
	}{
		"/a/p.jpg": {msgPhoto, "image/jpeg"}, "/a/P.JPEG": {msgPhoto, "image/jpeg"},
		"/a/v.mp4": {msgVideo, "video/mp4"}, "/a/v.3gp": {msgVideo, "video/3gpp"}, "/a/V.MOV": {msgVideo, "video/quicktime"},
		"/a/s.mp3": {msgAudio, "audio/mpeg"}, "/a/s.m4a": {msgAudio, "audio/mp4"}, "/a/s.aac": {msgAudio, "audio/aac"},
		"/a/s.amr": {msgAudio, "audio/amr"}, "/a/s.ogg": {msgAudio, "audio/ogg"}, "/a/S.WAV": {msgAudio, "audio/wav"},
		"/a/d.pdf": {msgDoc, "application/pdf"}, "/a/d.PDF": {msgDoc, "application/pdf"},
		"/a/p.png": {msgDoc, "image/png"}, "/a/g.gif": {msgDoc, "image/gif"}, // as documents, not re-encoded
		"/a/x.zzzunknown": {msgDoc, "application/octet-stream"}, "/a/README": {msgDoc, "application/octet-stream"},
		"/a/v.mp4.json": {msgDoc, "application/json"}, "/a.jpg/notes": {msgDoc, "application/octet-stream"},
	} {
		if kind, mime := sendKind(path); kind != want.kind || mime != want.mime {
			t.Errorf("%s: kind %d, %q; want %d, %q", path, kind, mime, want.kind, want.mime)
		}
	}
}

func TestMediaSendRejects(t *testing.T) {
	s := newUnlinked(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "a.pdf")
	os.WriteFile(file, []byte("x"), 0o644)
	big := filepath.Join(dir, "big.mp4")
	f, _ := os.Create(big)
	f.Truncate(maxSendSize + 1) // sparse: the size is all that is looked at
	f.Close()
	for name, path := range map[string]string{"relative": "a.pdf", "empty": "", "missing": filepath.Join(dir, "nope.pdf"),
		"directory": dir, "over 100 MB": big} {
		if _, err := call(s.handleMediaSend, `{"chat":"a@s.whatsapp.net","path":%q}`, path); code(err) != "bad_request" {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := call(s.handleMediaSend, `{"chat":"a@s.whatsapp.net","path":%q}`, file); code(err) != "unlinked" {
		t.Errorf("unlinked: %v", err)
	}
}

func TestSimMediaSend(t *testing.T) {
	s, st, _ := linkedSim(t)
	dir, chat := t.TempDir(), simChats[2].jid
	send := func(chat, path, quote string) (string, error) {
		r, err := call(s.handleMediaSend, `{"chat":%q,"path":%q,"quote":%q}`, chat, path, quote)
		if err != nil {
			return "", err
		}
		return r.(map[string]string)["id"], nil
	}

	// A JPEG: a picture row with its preview, sending at once, then sent.
	photo := filepath.Join(dir, "IMG_1.JPG")
	data := simJPEG(simGradient(800, 600, "p"), 85)
	os.WriteFile(photo, data, 0o644)
	var quoted string
	st.db.QueryRow("SELECT id FROM messages WHERE chat = ? LIMIT 1", chat).Scan(&quoted)
	id, err := send(chat, photo, quoted)
	if err != nil {
		t.Fatal(err)
	}
	r, ok := st.row(chat, id)
	cfg, cerr := jpeg.DecodeConfig(bytes.NewReader(r.Thumb))
	if !ok || r.Kind != msgPhoto || !r.Out || r.State != stateSending || r.Text != "" || r.Size != int64(len(data)) || r.Path != photo ||
		r.MediaState != 2 || r.Width != 800 || r.Height != 600 || cerr != nil || cfg.Width != 320 || cfg.Height != 240 || r.QuoteID != quoted {
		t.Fatalf("photo row: %+v (%v)", r, cerr)
	}
	eventually(t, st, 1, "SELECT count(*) FROM chats WHERE jid = ? AND last_preview = 'You: Photo'", chat)
	for _, q := range []string{"state = 1", "state = 2", "state = 3"} {
		eventually(t, st, 1, "SELECT count(*) FROM messages WHERE chat = ? AND id = ? AND "+q, chat, id)
	}

	// A PDF: a document row named for the file, with no thumbnail.
	pdf := filepath.Join(dir, "Recipe.pdf")
	os.WriteFile(pdf, simPDF, 0o644)
	if id, err = send(chat, pdf, ""); err != nil {
		t.Fatal(err)
	}
	if r, ok = st.row(chat, id); !ok || r.Kind != msgDoc || r.Text != "Recipe.pdf" || r.Thumb != nil || r.State != stateSending ||
		r.Size != int64(len(simPDF)) || r.Path != pdf || r.MediaState != 2 || r.Width != 0 || r.QuoteID != "" {
		t.Fatalf("pdf row: %+v", r)
	}
	eventually(t, st, 1, "SELECT count(*) FROM chats WHERE jid = ? AND last_preview = 'You: Document: Recipe.pdf'", chat)
	eventually(t, st, 1, "SELECT count(*) FROM messages WHERE chat = ? AND id = ? AND state = 1", chat, id)

	// A video and an audio file are text-less rows of their own kinds.
	for name, kind := range map[string]int{"clip.mp4": msgVideo, "song.mp3": msgAudio} {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte("x"), 0o644)
		if id, err = send(chat, p, ""); err != nil {
			t.Fatal(err)
		}
		if r, _ = st.row(chat, id); r.Kind != kind || r.Text != "" || r.Thumb != nil || r.MediaState != 2 {
			t.Errorf("%s row: %+v", name, r)
		}
	}

	for name, path := range map[string]string{"relative": "Recipe.pdf", "missing": filepath.Join(dir, "nope.jpg"), "directory": dir} {
		if _, err := send(chat, path, ""); code(err) != "bad_request" {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := send("not a jid@@@", pdf, ""); code(err) != "bad_chat" {
		t.Errorf("bad chat: %v", err)
	}
}

// uploaded is what the four media messages share.
type uploaded interface {
	whatsmeow.DownloadableMessage
	GetURL() string
	GetMimetype() string
	GetFileLength() uint64
}

func TestMediaMessage(t *testing.T) {
	up := whatsmeow.UploadResponse{URL: "u", DirectPath: "/d", MediaKey: []byte{1}, FileEncSHA256: []byte{2}, FileSHA256: []byte{3}, FileLength: 9}
	preview, _, _, err := previewOf(simJPEG(simGradient(800, 600, "p"), 85))
	if err != nil {
		t.Fatal(err)
	}
	for kind, name := range map[int]string{msgPhoto: "a.jpg", msgVideo: "a.mp4", msgAudio: "a.mp3", msgDoc: "a.pdf"} {
		o := &mediaOut{mime: "x/y", row: msgRow{Kind: kind, Text: "", Thumb: preview, Width: 800, Height: 600}}
		if kind == msgDoc {
			o.row.Text = name
		}
		m := mediaMessage(o, up, nil)
		var inner uploaded
		switch kind {
		case msgPhoto:
			inner = m.GetImageMessage()
		case msgVideo:
			inner = m.GetVideoMessage()
		case msgAudio:
			inner = m.GetAudioMessage()
		default:
			inner = m.GetDocumentMessage()
		}
		if got := whatsmeow.GetMediaType(inner); got != uploadTypes[kind] {
			t.Errorf("%s: media type %s, want %s", name, got, uploadTypes[kind])
		}
		if inner.GetDirectPath() != "/d" || inner.GetMediaKey()[0] != 1 || inner.GetFileEncSHA256()[0] != 2 || inner.GetFileSHA256()[0] != 3 ||
			inner.GetMimetype() != "x/y" || inner.GetFileLength() != 9 || inner.GetURL() != "u" {
			t.Errorf("%s: upload fields lost", name)
		}
		if kind == msgAudio && m.GetAudioMessage().GetPTT() {
			t.Error("an audio file went as a voice note")
		}
		if kind == msgDoc && (m.GetDocumentMessage().GetFileName() != name || m.GetDocumentMessage().GetTitle() != name) {
			t.Errorf("document: %v", m.GetDocumentMessage())
		}
		if kind == msgVideo && (m.GetVideoMessage().GetJPEGThumbnail() != nil || m.GetVideoMessage().GetSeconds() != 0) {
			t.Error("video has a thumbnail or a length")
		}
	}
	i := mediaMessage(&mediaOut{mime: "image/jpeg", row: msgRow{Kind: msgPhoto, Thumb: preview, Width: 800, Height: 600}}, up, nil).GetImageMessage()
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(i.GetJPEGThumbnail()))
	if err != nil || cfg.Width != 96 || cfg.Height != 72 || i.GetWidth() != 800 || i.GetHeight() != 600 || len(i.GetJPEGThumbnail()) >= len(preview) {
		t.Errorf("photo: thumbnail %v (%d bytes of %d), %v; %dx%d", cfg, len(i.GetJPEGThumbnail()), len(preview), err, i.GetWidth(), i.GetHeight())
	}
	if smallThumb([]byte("not a jpeg")) != nil || smallThumb(nil) != nil {
		t.Error("a thumbnail from nothing")
	}
}
