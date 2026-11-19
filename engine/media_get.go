package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/polymorfa/hypermeow"
	waLog "github.com/polymorfa/hypermeow/util/log"
)

// Photos, videos, audio files and documents (step 6.1): media.get downloads one when it isn't
// here and answers where it is. Voice notes keep voice.play, and media.get gives their OGG.

// photoPath is where a photo lives: in the sandbox, for our own viewer.
func photoPath(dir, id string) string { return filepath.Join(dir, "media", id+".jpg") }

// stickerPath is where a sticker lives: WebP, which the UI decodes itself (step 6.5b).
func stickerPath(dir, id string) string { return filepath.Join(dir, "media", id+".webp") }

// mediaExts are the extensions of what WhatsApp sends, since the phone has no mime database;
// anything else is asked of mime, then falls back by kind.
var mediaExts = map[string]string{
	"audio/mpeg": ".mp3", "audio/mp4": ".m4a", "audio/aac": ".aac", "audio/amr": ".amr", "audio/ogg": ".ogg",
	"audio/wav": ".wav", "video/mp4": ".mp4", "video/3gpp": ".3gp", "video/quicktime": ".mov",
}

func mediaExt(mimeType string, kind int) string {
	typ, _, _ := mime.ParseMediaType(mimeType)
	if e, ok := mediaExts[typ]; ok {
		return e
	}
	if exts, _ := mime.ExtensionsByType(typ); len(exts) > 0 {
		return exts[0]
	}
	switch kind {
	case msgVideo:
		return ".mp4"
	case msgAudio:
		return ".m4a"
	}
	return ".bin"
}

// mediaTarget is the file a download of row is written to: a photo or sticker in media/, a voice note as
// voice.play has it, the rest in Downloads under the document's own name or <id>.<ext>. A name
// already taken there gets the message id in front.
func mediaTarget(dir string, row msgRow, mimeType string) string {
	switch row.Kind {
	case msgPhoto:
		return photoPath(dir, row.ID)
	case msgSticker:
		return stickerPath(dir, row.ID)
	case msgVoice:
		return oggPath(dir, row.ID)
	}
	dl, name := downloadsDir(dir), ""
	if row.Kind == msgDoc { // the sender chose it: fileSafe takes the separators, this the dot names
		name = strings.TrimSpace(fileSafe.Replace(row.Text))
	}
	if name == "" || name == "." || name == ".." {
		return filepath.Join(dl, row.ID+mediaExt(mimeType, row.Kind))
	}
	if _, err := os.Stat(filepath.Join(dl, name)); err == nil {
		name = row.ID + "-" + name
	}
	return filepath.Join(dl, name)
}

// mediaHere is the path of row's downloaded file, or "" when it isn't here.
func mediaHere(dir string, row msgRow) string {
	p := row.Path
	if row.Kind == msgVoice {
		p = oggPath(dir, row.ID)
	}
	if row.MediaState == 2 && p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// memUsage is the engine's memory, for the log: the phone caps it at about 30 MB (phase6.md 6.3).
func memUsage() string {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return fmt.Sprintf("heap %d MB, from the OS %d MB", ms.HeapAlloc>>20, ms.Sys>>20)
}

// logTransfer says what a download or an upload (who: "media <id>") moved and what it cost.
func logTransfer(log waLog.Logger, who string, n int64, since time.Time) {
	log.Infof("%s: %d bytes in %v, %s", who, n, time.Since(since).Round(time.Millisecond), memUsage())
}

// downloadTo has get write a download into the temporary file tmp, and once it is whole (synced
// and closed) moves it to target(mimeType), the mimetype being what get answers. A failed
// download leaves nothing behind, and a half file never has the final name. tmp must be in the
// directory the target will be in.
func downloadTo(tmp string, get func(f *os.File) (string, error), target func(mimeType string) string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(tmp), 0o755); err != nil {
		return "", err
	}
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	mimeType, err := get(f)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	path := ""
	if err == nil {
		path = target(mimeType)
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
		return "", err
	}
	return path, nil
}

// progressInterval is how often a download's progress goes to the UI.
const progressInterval = 200 * time.Millisecond

// progressFile is the file a download writes into, counting the bytes: hypermeow copies the response into it with
// io.Copy, which would give the embedded *os.File's own ReadFrom the whole copy, so ReadFrom goes through Write here.
// emit gets the count, at most every progressInterval and never above size (what is written is the encrypted file, a
// few bytes more than the decrypted one). A retry after a lost connection writes from the start again, and the count
// goes on from where it was, so the ring jumps to full and waits.
type progressFile struct {
	*os.File
	size, done int64
	last       time.Time
	emit       func(done, size int64)
}

func (p *progressFile) Write(b []byte) (int, error) {
	n, err := p.File.Write(b)
	p.done += int64(n)
	if now := time.Now(); now.Sub(p.last) >= progressInterval {
		p.last = now
		p.emit(min(p.done, p.size), p.size)
	}
	return n, err
}

func (p *progressFile) ReadFrom(r io.Reader) (int64, error) {
	return io.Copy(struct{ io.Writer }{p}, r)
}

// downloadMedia fetches row's file and moves its media_state to 2 with its path, or to 3 on a
// failure. A voice note goes through downloadVoice.
func (st *Store) downloadMedia(ctx context.Context, cli *whatsmeow.Client, dir string, row msgRow) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if row.Kind == msgVoice {
		return oggPath(dir, row.ID), st.downloadVoice(ctx, cli, dir, row)
	}
	// The file streams to disk, decrypted as it comes: the engine never holds it whole.
	return st.fetchMedia(dir, row, func(f *os.File) (string, error) {
		var info mediaInfo
		if err := json.Unmarshal([]byte(row.Media), &info); err != nil {
			return "", err
		}
		var file whatsmeow.File = f
		if st.srv != nil && info.FileLength > 0 {
			file = &progressFile{File: f, size: int64(info.FileLength), emit: func(done, size int64) {
				st.srv.Emit("media.progress", map[string]any{"chat": row.Chat, "id": row.ID, "done": done, "size": size})
			}}
		}
		return info.Mimetype, cli.DownloadToFile(ctx, info.message(row.Kind), file)
	})
}

// fetchMedia is the download of the real and the simulated account: get writes the file into f
// and says what it is (the real one knows from the row, the sim makes it up), the rest is
// shared. One message downloads at a time, so a tap during a live photo's download waits and
// finds the file.
func (st *Store) fetchMedia(dir string, row msgRow, get func(f *os.File) (string, error)) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	mu, _ := st.dl.LoadOrStore(row.Chat+"/"+row.ID, new(sync.Mutex))
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()
	cur, ok := st.row(row.Chat, row.ID)
	if !ok {
		return "", errors.New("the message is gone")
	}
	if p := mediaHere(dir, cur); p != "" {
		return p, nil
	}
	// The name of a video or audio file depends on the mimetype, a document's on what is taken
	// in Downloads: so the file is named after the download, from its own <id>.tmp.
	t0 := time.Now()
	tmp := filepath.Join(filepath.Dir(mediaTarget(dir, row, "")), row.ID+".tmp")
	path, err := downloadTo(tmp, get, func(mimeType string) string { return mediaTarget(dir, row, mimeType) })
	if errors.Is(err, context.Canceled) {
		return "", err // the user's cancel, not a failure: the row stays as it was, and a tap fetches it again
	}
	state := 2
	if err != nil {
		state, path = 3, ""
	} else if fi, serr := os.Stat(path); serr == nil {
		logTransfer(st.log, "media "+row.ID, fi.Size(), t0)
	}
	st.ops <- func(t *storeTx) { t.setFile(row.Chat, row.ID, state, path) }
	return path, err
}

type mediaReq struct{ Chat, ID string }

func parseMediaGet(req json.RawMessage) (mediaReq, error) {
	var r mediaReq
	if err := json.Unmarshal(req, &r); err != nil || r.Chat == "" || r.ID == "" {
		return r, &Error{"bad_request", "media.get needs chat and id"}
	}
	return r, nil
}

// mediaGet is media.get for both accounts: the path at once when the file is here, else fetch
// downloads it first.
func (st *Store) mediaGet(dir string, r mediaReq, fetch func(msgRow) (string, error)) (any, error) {
	row, ok := st.row(r.Chat, r.ID)
	if !ok || row.MediaState == 0 {
		return nil, &Error{"not_found", "no such media"}
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	p := mediaHere(dir, row)
	if p == "" {
		if p, err = fetch(row); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil, &Error{"cancelled", "download cancelled"}
			}
			return nil, &Error{"download_failed", err.Error()}
		}
	}
	return map[string]string{"path": p}, nil
}

func (s *Session) handleMediaGet(req json.RawMessage) (any, error) {
	r, err := parseMediaGet(req)
	if err != nil {
		return nil, err
	}
	cli, _, err := s.linked(r.Chat)
	if err != nil {
		return nil, err
	}
	fetch := func(row msgRow) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), mediaTimeout)
		defer cancel()
		// media.cancel finds the cancel here; a newer media.get of the same message replaces it, and this one's end then
		// leaves the newer one's alone.
		stop := context.CancelFunc(cancel)
		key := r.Chat + "/" + r.ID
		s.cancels.Store(key, &stop)
		defer s.cancels.CompareAndDelete(key, &stop)
		return s.store.downloadMedia(ctx, cli, s.srv.dir, row)
	}
	return s.store.mediaGet(s.srv.dir, r, fetch)
}

// handleMediaCancel is media.cancel: the media.get of that message, if one is running, ends with "cancelled". It
// answers ok either way: a download that just finished is not an error.
func (s *Session) handleMediaCancel(req json.RawMessage) (any, error) {
	var r mediaReq
	if err := json.Unmarshal(req, &r); err != nil || r.Chat == "" || r.ID == "" {
		return nil, &Error{"bad_request", "media.cancel needs chat and id"}
	}
	if stop, ok := s.cancels.Load(r.Chat + "/" + r.ID); ok {
		(*stop.(*context.CancelFunc))()
	}
	return nil, nil
}
