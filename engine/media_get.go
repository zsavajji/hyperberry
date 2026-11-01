package main

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/polymorfa/hypermeow"
)

// Photos, videos, audio files and documents (step 6.1): media.get downloads one when it isn't
// here and answers where it is. Voice notes keep voice.play, and media.get gives their OGG.

// photoPath is where a photo lives: in the sandbox, for our own viewer.
func photoPath(dir, id string) string { return filepath.Join(dir, "media", id+".jpg") }

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

// mediaTarget is the file a download of row is written to: a photo in media/, a voice note as
// voice.play has it, the rest in Downloads under the document's own name or <id>.<ext>. A name
// already taken there gets the message id in front.
func mediaTarget(dir string, row msgRow, mimeType string) string {
	switch row.Kind {
	case msgPhoto:
		return photoPath(dir, row.ID)
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

// downloadMedia fetches row's file and moves its media_state to 2 with its path (a photo also
// gets its preview), or to 3 on a failure. A voice note goes through downloadVoice.
func (st *Store) downloadMedia(ctx context.Context, cli *whatsmeow.Client, dir string, row msgRow) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if row.Kind == msgVoice {
		return oggPath(dir, row.ID), st.downloadVoice(ctx, cli, dir, row)
	}
	// ponytail: the whole file is held in memory (hypermeow's Download); a streamed download if
	// videos of tens of MB turn out to be a problem on the Q10.
	return st.fetchMedia(dir, row, func() ([]byte, string, error) {
		var info mediaInfo
		if err := json.Unmarshal([]byte(row.Media), &info); err != nil {
			return nil, "", err
		}
		data, err := cli.Download(ctx, info.message(row.Kind))
		return data, info.Mimetype, err
	})
}

// fetchMedia is the download of the real and the simulated account: get makes the bytes and
// says what they are (the sim makes them up), the rest is shared. One message downloads at a
// time, so a tap during a live photo's download waits and finds the file.
func (st *Store) fetchMedia(dir string, row msgRow, get func() ([]byte, string, error)) (string, error) {
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
	var thumb []byte
	var w, h int
	path, err := func() (string, error) {
		data, mimeType, err := get()
		if err != nil {
			return "", err
		}
		p := mediaTarget(dir, row, mimeType)
		if err := writeFile(p, data); err != nil {
			return "", err
		}
		if row.Kind == msgPhoto { // the inline thumbnail stays if the photo won't decode
			var perr error
			if thumb, w, h, perr = previewOf(data); perr != nil {
				st.log.Warnf("preview of %s: %v", row.ID, perr)
			}
		}
		return p, nil
	}()
	state := 2
	if err != nil {
		state, path = 3, ""
	}
	st.ops <- func(t *storeTx) { t.setFile(row.Chat, row.ID, state, path, thumb, w, h) }
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
		return s.store.downloadMedia(ctx, cli, s.srv.dir, row)
	}
	return s.store.mediaGet(s.srv.dir, r, fetch)
}
