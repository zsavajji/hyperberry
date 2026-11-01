package main

import (
	"bytes"
	"encoding/base64"
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
	// A thumbnail of 16 KB goes on to the link check; one byte more, bad base64 or a negative size don't.
	b64 := func(n int) string { return base64.StdEncoding.EncodeToString(make([]byte, n)) }
	for name, c := range map[string]struct{ extra, want string }{
		"thumb 16 KB":    {`"thumb":"` + b64(maxThumb) + `"`, "unlinked"},
		"thumb over":     {`"thumb":"` + b64(maxThumb+1) + `"`, "bad_request"},
		"not base64":     {`"thumb":"@@@"`, "bad_request"},
		"width < 0":      {`"width":-1`, "bad_request"},
		"height < 0":     {`"height":-5`, "bad_request"},
		"size, no thumb": {`"width":800,"height":600`, "unlinked"},
	} {
		if _, err := call(s.handleMediaSend, `{"chat":"a@s.whatsapp.net","path":%q,%s}`, file, c.extra); code(err) != c.want {
			t.Errorf("%s: %v, want %s", name, err, c.want)
		}
	}
}

// The upload's temporary file is made under cache/upload and is gone once done is called.
func TestUploadTemp(t *testing.T) {
	dir := t.TempDir()
	f, done, err := uploadTemp(dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(f.Name()) != filepath.Join(dir, "cache", "upload") {
		t.Errorf("temp file at %s", f.Name())
	}
	f.WriteString("encrypted")
	if _, err := os.Stat(f.Name()); err != nil {
		t.Fatal(err)
	}
	done()
	if left, _ := os.ReadDir(filepath.Join(dir, "cache", "upload")); len(left) != 0 {
		t.Errorf("left behind: %v", left)
	}
}

func TestSimMediaSend(t *testing.T) {
	s, st, _ := linkedSim(t)
	dir, chat := t.TempDir(), simChats[2].jid
	// extra is more JSON fields, with a comma in front.
	send := func(chat, path, quote, extra string) (string, error) {
		r, err := call(s.handleMediaSend, `{"chat":%q,"path":%q,"quote":%q%s}`, chat, path, quote, extra)
		if err != nil {
			return "", err
		}
		return r.(map[string]string)["id"], nil
	}

	// A JPEG with the UI's thumbnail and size: a picture row with them, sending at once, then sent.
	photo := filepath.Join(dir, "IMG_1.JPG")
	data := simJPEG(simGradient(800, 600, "p"), 85)
	os.WriteFile(photo, data, 0o644)
	thumb := simJPEG(simGradient(96, 72, "p"), 60)
	var quoted string
	st.db.QueryRow("SELECT id FROM messages WHERE chat = ? LIMIT 1", chat).Scan(&quoted)
	id, err := send(chat, photo, quoted, `,"thumb":"`+base64.StdEncoding.EncodeToString(thumb)+`","width":800,"height":600`)
	if err != nil {
		t.Fatal(err)
	}
	r, ok := st.row(chat, id)
	if !ok || r.Kind != msgPhoto || !r.Out || r.State != stateSending || r.Text != "" || r.Size != int64(len(data)) || r.Path != photo ||
		r.MediaState != 2 || r.Width != 800 || r.Height != 600 || !bytes.Equal(r.Thumb, thumb) || r.QuoteID != quoted {
		t.Fatalf("photo row: %+v", r)
	}
	eventually(t, st, 1, "SELECT count(*) FROM chats WHERE jid = ? AND last_preview = 'You: Photo'", chat)
	for _, q := range []string{"state = 1", "state = 2", "state = 3"} {
		eventually(t, st, 1, "SELECT count(*) FROM messages WHERE chat = ? AND id = ? AND "+q, chat, id)
	}

	// The same without them: a picture with no thumbnail and no size. The engine decodes nothing.
	if id, err = send(chat, photo, "", ""); err != nil {
		t.Fatal(err)
	}
	if r, _ = st.row(chat, id); r.Kind != msgPhoto || r.Thumb != nil || r.Width != 0 || r.Height != 0 || r.Size != int64(len(data)) {
		t.Errorf("photo row without a thumbnail: %+v", r)
	}

	// A caption is a photo's or a video's text; a document's text stays its file name,
	// an audio file has none.
	for name, c := range map[string]struct {
		kind int
		text string
	}{"IMG_2.jpg": {msgPhoto, "sunset"}, "clip2.mp4": {msgVideo, "sunset"}, "Menu.pdf": {msgDoc, "Menu.pdf"}, "song2.mp3": {msgAudio, ""}} {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte("x"), 0o644)
		if id, err = send(chat, p, "", `,"caption":"sunset"`); err != nil {
			t.Fatal(err)
		}
		if r, _ = st.row(chat, id); r.Kind != c.kind || r.Text != c.text {
			t.Errorf("%s with a caption: kind %d, text %q; want %d, %q", name, r.Kind, r.Text, c.kind, c.text)
		}
	}
	// A PDF: a document row named for the file, with no thumbnail.
	pdf := filepath.Join(dir, "Recipe.pdf")
	os.WriteFile(pdf, simPDF, 0o644)
	if id, err = send(chat, pdf, "", ""); err != nil {
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
		if id, err = send(chat, p, "", `,"thumb":"/9j/","width":10,"height":10`); err != nil { // not a photo: ignored
			t.Fatal(err)
		}
		if r, _ = st.row(chat, id); r.Kind != kind || r.Text != "" || r.Thumb != nil || r.Width != 0 || r.Height != 0 || r.MediaState != 2 {
			t.Errorf("%s row: %+v", name, r)
		}
	}

	for name, path := range map[string]string{"relative": "Recipe.pdf", "missing": filepath.Join(dir, "nope.jpg"), "directory": dir} {
		if _, err := send(chat, path, "", ""); code(err) != "bad_request" {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := send("not a jid@@@", pdf, "", ""); code(err) != "bad_chat" {
		t.Errorf("bad chat: %v", err)
	}
	if _, err := send(chat, photo, "", `,"thumb":"`+base64.StdEncoding.EncodeToString(make([]byte, maxThumb+1))+`"`); code(err) != "bad_request" {
		t.Errorf("a thumbnail over 16 KB: %v", err)
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
	thumb := simJPEG(simGradient(96, 72, "p"), 60)
	for kind, name := range map[int]string{msgPhoto: "a.jpg", msgVideo: "a.mp4", msgAudio: "a.mp3", msgDoc: "a.pdf"} {
		o := &mediaOut{mime: "x/y", row: msgRow{Kind: kind, Text: "", Thumb: thumb, Width: 800, Height: 600}}
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
	// A caption goes in a photo's and a video's message, and a message without one has none.
	caption := func(kind int, text string) string {
		m := mediaMessage(&mediaOut{mime: "x/y", row: msgRow{Kind: kind, Text: text}}, up, nil)
		if kind == msgVideo {
			return m.GetVideoMessage().GetCaption()
		}
		return m.GetImageMessage().GetCaption()
	}
	for _, kind := range []int{msgPhoto, msgVideo} {
		if got, none := caption(kind, "sunset"), caption(kind, ""); got != "sunset" || none != "" {
			t.Errorf("kind %d: caption %q, without %q", kind, got, none)
		}
	}
	// A photo carries the thumbnail and size it was given as they are, and nothing if it had none.
	i := mediaMessage(&mediaOut{mime: "image/jpeg", row: msgRow{Kind: msgPhoto, Thumb: thumb, Width: 800, Height: 600}}, up, nil).GetImageMessage()
	if !bytes.Equal(i.GetJPEGThumbnail(), thumb) || i.GetWidth() != 800 || i.GetHeight() != 600 {
		t.Errorf("photo: thumbnail %d bytes, %dx%d", len(i.GetJPEGThumbnail()), i.GetWidth(), i.GetHeight())
	}
	i = mediaMessage(&mediaOut{mime: "image/jpeg", row: msgRow{Kind: msgPhoto}}, up, nil).GetImageMessage()
	if i.JPEGThumbnail != nil || i.Width != nil || i.Height != nil {
		t.Errorf("photo without them: %v", i)
	}
}
