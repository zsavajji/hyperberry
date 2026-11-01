package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image/jpeg"
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
// ponytail: the file is read whole, as hypermeow's Upload takes bytes; a stream if 100 MB
// proves too much for the Q10's memory.
const maxSendSize = 100 << 20

// mediaTimeout bounds a media upload or download: a video of tens of MB takes minutes on a
// phone's data connection, past sendTimeout's minute.
const mediaTimeout = 10 * time.Minute

// thumbSide and thumbQuality are the thumbnail inside an image message: WhatsApp's are small,
// and the 320 px preview could swell the message.
const (
	thumbSide    = 96
	thumbQuality = 60
)

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

type mediaSendReq struct {
	Chat, Path, Quote string
	size              int64 // from the stat of parseMediaSend
}

func parseMediaSend(req json.RawMessage) (mediaSendReq, error) {
	var r mediaSendReq
	if err := json.Unmarshal(req, &r); err != nil || !filepath.IsAbs(r.Path) {
		return r, &Error{"bad_request", "media.send needs chat and an absolute path"}
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
	data    []byte // the file, read for a photo's preview; nil otherwise, and the upload reads it
	quote   msgRow
	created bool // the chat was new
}

// queueMedia puts our row in as sending, with a photo's 320 px preview, and is the one place
// both accounts build it. A file that won't decode only logs: the row has no thumbnail.
func (st *Store) queueMedia(jid types.JID, r mediaSendReq, id string) (*mediaOut, error) {
	kind, mimeType := sendKind(r.Path)
	o := &mediaOut{mime: mimeType, row: msgRow{Chat: r.Chat, ID: id, TS: time.Now().Unix(), Out: true,
		State: stateSending, Kind: kind, Size: r.size, Path: r.Path, MediaState: 2}}
	switch kind {
	case msgPhoto:
		var err error
		if o.data, err = os.ReadFile(r.Path); err != nil {
			return nil, &Error{"bad_request", err.Error()}
		}
		var perr error
		if o.row.Thumb, o.row.Width, o.row.Height, perr = previewOf(o.data); perr != nil {
			st.log.Warnf("preview of %s: %v", r.Path, perr)
		}
	case msgDoc:
		o.row.Text = filepath.Base(r.Path)
	}
	o.quote, o.created = st.queueRow(jid, r.Chat, r.Quote, o.row)
	return o, nil
}

// smallThumb is the preview shrunk to the thumbnail of an image message, or nil if it won't.
// Made from the 320 px preview, so the photo isn't decoded twice.
func smallThumb(preview []byte) []byte {
	img, err := jpeg.Decode(bytes.NewReader(preview))
	if err != nil {
		return nil
	}
	var buf bytes.Buffer
	if jpeg.Encode(&buf, boxScale(img, thumbSide), &jpeg.Options{Quality: thumbQuality}) != nil {
		return nil
	}
	return buf.Bytes()
}

var uploadTypes = map[int]whatsmeow.MediaType{msgPhoto: whatsmeow.MediaImage, msgVideo: whatsmeow.MediaVideo,
	msgAudio: whatsmeow.MediaAudio, msgDoc: whatsmeow.MediaDocument}

// mediaMessage is the message of an uploaded file; ci quotes, or is nil.
// ponytail: a video has no thumbnail and no length, so the phone shows a grey box until it
// downloads; reading the MP4's headers can come later.
func mediaMessage(o *mediaOut, up whatsmeow.UploadResponse, ci *waE2E.ContextInfo) *waE2E.Message {
	r := o.row
	switch r.Kind {
	case msgPhoto:
		return &waE2E.Message{ImageMessage: &waE2E.ImageMessage{URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: &up.FileLength, Mimetype: &o.mime,
			JPEGThumbnail: smallThumb(r.Thumb), Width: proto.Uint32(uint32(r.Width)), Height: proto.Uint32(uint32(r.Height)), ContextInfo: ci}}
	case msgVideo:
		return &waE2E.Message{VideoMessage: &waE2E.VideoMessage{URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: &up.FileLength, Mimetype: &o.mime, ContextInfo: ci}}
	case msgAudio:
		return &waE2E.Message{AudioMessage: &waE2E.AudioMessage{URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: &up.FileLength, Mimetype: &o.mime,
			PTT: proto.Bool(false), ContextInfo: ci}}
	}
	return &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey,
		FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: &up.FileLength, Mimetype: &o.mime,
		FileName: &r.Text, Title: &r.Text, ContextInfo: ci}}
}

// handleMediaSend is media.send: the row is in as sending before the reply; the upload and the
// send run after it.
func (s *Session) handleMediaSend(req json.RawMessage) (any, error) {
	r, err := parseMediaSend(req)
	if err != nil {
		return nil, err
	}
	cli, jid, err := s.linked(r.Chat)
	if err != nil {
		return nil, err
	}
	id := cli.GenerateMessageID()
	o, err := s.store.queueMedia(jid, r, id)
	if err != nil {
		return nil, err
	}
	go s.uploadMedia(cli, jid, r.Path, o)
	return map[string]string{"id": id}, nil
}

func (s *Session) uploadMedia(cli *whatsmeow.Client, jid types.JID, path string, o *mediaOut) {
	chat, id := o.row.Chat, o.row.ID
	s.nameNew(cli, jid, chat, o.created)
	ctx, cancel := context.WithTimeout(context.Background(), mediaTimeout)
	defer cancel()
	data, err := o.data, error(nil)
	if data == nil {
		data, err = os.ReadFile(path)
	}
	if err == nil {
		var up whatsmeow.UploadResponse
		if up, err = cli.Upload(ctx, data, uploadTypes[o.row.Kind]); err == nil {
			var ci *waE2E.ContextInfo
			if o.quote.ID != "" {
				ci = quoteInfo(cli, chat, o.quote)
			}
			var resp whatsmeow.SendResponse
			if resp, err = cli.SendMessage(ctx, jid, mediaMessage(o, up, ci), whatsmeow.SendRequestExtra{ID: id}); err == nil {
				s.store.ops <- func(t *storeTx) { t.sentAs(chat, id, resp.Timestamp.Unix(), stateSent) }
				return
			}
		}
	}
	s.log.Warnf("media %s: %v", id, err)
	s.store.ops <- func(t *storeTx) { t.sentAs(chat, id, 0, stateFailed) }
}
