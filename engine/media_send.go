package main

import (
	"context"
	"encoding/json"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"google.golang.org/protobuf/proto"
)

// Sending photos, videos, audio files and documents (step 6.2): media.send. The kind comes
// from the file's extension; the row is in as sending before the reply, the upload and the
// send follow it.

// maxSendSize is the biggest file media.send takes.
// ponytail: a fixed cap, picked for a phone's upload time; the file streams now, so memory is
// not why. Raise it if it gets in the way.
const maxSendSize = 100 << 20

// maxThumb is the biggest thumbnail media.send takes, decoded: WhatsApp's are a few KB.
const maxThumb = 16 << 10

// mediaTimeout bounds a media upload or download: a video of tens of MB takes minutes on a
// phone's data connection, past sendTimeout's minute.
const mediaTimeout = 10 * time.Minute

// sendMimes are the mimetypes of the extensions we send by name: mediaExts reversed (it has the
// extensions of what WhatsApp sends), and JPEG.
var sendMimes = func() map[string]string {
	m := map[string]string{".jpg": "image/jpeg", ".jpeg": "image/jpeg"}
	for typ, ext := range mediaExts {
		m[ext] = typ
	}
	return m
}()

// sendKind says what a file goes as, by its extension (any case), and its mimetype.
// ponytail: PNG and GIF go as documents rather than being re-encoded; a re-encode to JPEG (and
// the loss of GIF's frames) if people expect them as pictures.
func sendKind(path string) (kind int, mimeType string) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".jpg", ".jpeg":
		kind = msgPhoto
	case ".mp4", ".3gp", ".mov":
		kind = msgVideo
	case ".mp3", ".m4a", ".aac", ".amr", ".ogg", ".wav":
		kind = msgAudio
	default:
		kind = msgDoc
	}
	if mimeType = sendMimes[ext]; mimeType == "" {
		mimeType = mime.TypeByExtension(ext)
	}
	if typ, _, err := mime.ParseMediaType(mimeType); err == nil { // "text/html; charset=utf-8" goes as text/html
		return kind, typ
	}
	return kind, "application/octet-stream"
}

// mediaSendReq is media.send. Thumb (a JPEG, base64 in the JSON), Width and Height are a
// photo's, made by the UI: the engine never decodes a picture, its memory is too small.
// Caption goes with a photo or a video only; the UI sends the other kinds' as a text message.
type mediaSendReq struct {
	Chat, Path, Quote, Caption string
	ID                         string // the id the caller chose for the message, if it did (beginSend)
	Original                   string // the file Path is a copy of, if it is one (the UI outbox's): extra.original records it, not Path
	Thumb                      []byte
	File                       bool // as a document whatever the extension: the staged bar's As file on a JPEG
	Width, Height              int
	size                       int64 // from the stat of parseMediaSend
}

func parseMediaSend(req json.RawMessage) (mediaSendReq, error) {
	var r mediaSendReq
	if err := json.Unmarshal(req, &r); err != nil || !filepath.IsAbs(r.Path) || (r.Original != "" && !filepath.IsAbs(r.Original)) {
		return r, &Error{"bad_request", "media.send needs chat and an absolute path (thumb is base64, original is absolute too)"}
	}
	if len(r.Thumb) > maxThumb || r.Width < 0 || r.Height < 0 {
		return r, &Error{"bad_request", "media.send: thumb is at most 16 KB, width and height aren't negative"}
	}
	fi, err := os.Stat(r.Path)
	switch {
	case err != nil || !fi.Mode().IsRegular():
		return r, &Error{"bad_request", "not a file: " + r.Path}
	case fi.Size() > maxSendSize:
		return r, &Error{"bad_request", "over 100 MB: " + r.Path}
	}
	r.size = fi.Size()
	return r, nil
}

// mediaOut is what the upload needs once the row is in.
type mediaOut struct {
	row     msgRow // as queued
	mime    string
	quote   msgRow
	created bool // the chat was new
	// poster is a video's thumbnail as the message carries it: the UI draws one with a play mark, so the
	// phones that get it show a video and not a grey box. Our own row keeps none (the UI marks a video by itself); the
	// send keeps it in outbox-engine/<id>.poster (posterPath) until it is over, for a rebuild after a restart.
	poster []byte
}

// kind is what the file goes as: by its extension, or a document when the caller says so (File).
func (r mediaSendReq) kind() (kind int, mimeType string) {
	kind, mimeType = sendKind(r.Path)
	if r.File {
		kind = msgDoc
	}
	return kind, mimeType
}

// queueMedia puts our row in as sending, and is the one place both accounts build it. A
// photo's thumbnail and size are the UI's, and a photo's or a video's caption is its text; the
// other kinds ignore them.
func (st *Store) queueMedia(jid types.JID, r mediaSendReq, id string) *mediaOut {
	kind, mimeType := r.kind()
	o := &mediaOut{mime: mimeType, row: msgRow{Chat: r.Chat, ID: id, TS: time.Now().Unix(), Out: true,
		State: stateSending, Kind: kind, Size: r.size, Path: r.Path, MediaState: 2}}
	switch kind {
	case msgPhoto:
		o.row.Thumb, o.row.Width, o.row.Height, o.row.Text = r.Thumb, r.Width, r.Height, r.Caption
	case msgVideo:
		o.row.Text, o.poster = r.Caption, r.Thumb
	case msgDoc:
		o.row.Text = filepath.Base(r.Path)
	}
	if r.Original != "" { // the file the row's path stands for, which it goes back to once the send is over (sendfiles.go)
		o.row.Extra = withOriginal(o.row.Extra, r.Original)
	}
	o.quote, o.created = st.queueRow(jid, r.Chat, r.Quote, o.row)
	return o
}

var uploadTypes = map[int]whatsmeow.MediaType{msgPhoto: whatsmeow.MediaImage, msgVideo: whatsmeow.MediaVideo,
	msgAudio: whatsmeow.MediaAudio, msgDoc: whatsmeow.MediaDocument}

// mediaMessage is the message of an uploaded file; ci quotes, or is nil. A photo carries the
// thumbnail and size the UI gave, if it gave them; a photo and a video carry the row's text as
// their caption.
// ponytail: a video has no length, and its thumbnail is the UI's poster (a play mark on grey), not a frame of
// it: nothing here can decode one.
func mediaMessage(o *mediaOut, up whatsmeow.UploadResponse, ci *waE2E.ContextInfo) *waE2E.Message {
	r := o.row
	switch r.Kind {
	case msgPhoto:
		img := &waE2E.ImageMessage{URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: &up.FileLength, Mimetype: &o.mime,
			JPEGThumbnail: r.Thumb, ContextInfo: ci}
		if r.Text != "" {
			img.Caption = &r.Text
		}
		if r.Width > 0 && r.Height > 0 {
			img.Width, img.Height = proto.Uint32(uint32(r.Width)), proto.Uint32(uint32(r.Height))
		}
		return &waE2E.Message{ImageMessage: img}
	case msgVideo:
		vid := &waE2E.VideoMessage{URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: &up.FileLength, Mimetype: &o.mime,
			JPEGThumbnail: o.poster, ContextInfo: ci}
		if r.Text != "" {
			vid.Caption = &r.Text
		}
		return &waE2E.Message{VideoMessage: vid}
	case msgAudio:
		return &waE2E.Message{AudioMessage: &waE2E.AudioMessage{URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: &up.FileLength, Mimetype: &o.mime,
			PTT: proto.Bool(false), ContextInfo: ci}}
	}
	return &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey,
		FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: &up.FileLength, Mimetype: &o.mime,
		FileName: &r.Text, Title: &r.Text, ContextInfo: ci}}
}

// handleMediaSend is media.send: the file is kept and the row is in as sending before the reply; the upload and the
// send follow it, in the chat's worker (outgoing.go).
func (s *Session) handleMediaSend(req json.RawMessage) (any, error) {
	r, err := parseMediaSend(req)
	if err != nil {
		return nil, err
	}
	cli, jid, id, dup, err := s.beginSend(r.Chat, r.ID)
	if err != nil {
		return nil, err
	}
	if dup {
		return sendReply(cli, id), nil
	}
	// The row's path is the kept file, and the file it was made from (the caller's original, else Path) its extra.original.
	var poster []byte
	if kind, _ := r.kind(); kind == msgVideo {
		poster = r.Thumb
	}
	kept, err := s.files().keep(id, r.Path, poster)
	if err != nil {
		return nil, &Error{"copy_failed", err.Error()}
	}
	if r.Original == "" {
		r.Original = r.Path
	}
	r.Path = kept
	o := s.store.queueMedia(jid, r, id)
	go func() {
		s.nameNew(cli, jid, r.Chat, o.created)
		s.kick(r.Chat)
	}()
	return sendReply(cli, id), nil
}

// uploadTemp makes the file the upload encrypts into, under <data dir>/cache/upload/, and the
// func that closes and removes it. A crash leaves the file there until the cache goes.
// pruneCache sweeps the stale ones after a day.
func uploadTemp(dir string) (*os.File, func(), error) {
	d := filepath.Join(dir, "cache", "upload")
	if err := os.MkdirAll(d, 0o755); err != nil {
		return nil, nil, err
	}
	f, err := os.CreateTemp(d, "up-*")
	if err != nil {
		return nil, nil, err
	}
	return f, func() { f.Close(); os.Remove(f.Name()) }, nil
}

// uploadFile streams the file at path through UploadReader: read, encrypted into a temporary
// file, sent. The engine never holds the file.
func (s *Session) uploadFile(ctx context.Context, cli *whatsmeow.Client, path string, typ whatsmeow.MediaType) (whatsmeow.UploadResponse, error) {
	f, err := os.Open(path)
	if err != nil {
		return whatsmeow.UploadResponse{}, err
	}
	defer f.Close()
	tmp, done, err := uploadTemp(s.srv.dir)
	if err != nil {
		return whatsmeow.UploadResponse{}, err
	}
	defer done()
	return cli.UploadReader(ctx, f, tmp, typ)
}

// uploadMedia is one attempt at a media send: the upload, then the message. It returns the server's time and the upload.
func (s *Session) uploadMedia(cli *whatsmeow.Client, jid types.JID, o *mediaOut) (sendDone, error) {
	chat, id := o.row.Chat, o.row.ID
	ctx, cancel := context.WithTimeout(context.Background(), mediaTimeout)
	defer cancel()
	up, err := s.uploadFile(ctx, cli, o.row.Path, uploadTypes[o.row.Kind])
	if err == nil {
		var ci *waE2E.ContextInfo
		if o.quote.ID != "" {
			ci = quoteInfo(cli, chat, o.quote)
		}
		var resp whatsmeow.SendResponse
		if resp, err = cli.SendMessage(ctx, jid, mediaMessage(o, up, ci), whatsmeow.SendRequestExtra{ID: id}); err == nil {
			// The upload, as a received file's: a caption edit sends the message again (editContent).
			info, _ := json.Marshal(mediaInfo{up.DirectPath, up.MediaKey, up.FileEncSHA256, up.FileSHA256, up.FileLength, o.mime})
			return sendDone{resp.Timestamp.Unix(), string(info)}, nil
		}
	}
	return sendDone{}, err
}
