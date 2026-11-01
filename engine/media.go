package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
)

// mediaInfo is what Download needs of a voice note, kept in messages.media. The byte slices
// are base64 in the JSON.
type mediaInfo struct {
	DirectPath    string `json:"directPath"`
	MediaKey      []byte `json:"mediaKey"`
	FileEncSHA256 []byte `json:"fileEncSha256"`
	FileSHA256    []byte `json:"fileSha256"`
	FileLength    uint64 `json:"fileLength"`
	Mimetype      string `json:"mimetype"`
}

// message rebuilds the part of the AudioMessage that hypermeow's Download reads.
func (m mediaInfo) message() *waE2E.AudioMessage {
	return &waE2E.AudioMessage{DirectPath: &m.DirectPath, MediaKey: m.MediaKey, FileEncSHA256: m.FileEncSHA256,
		FileSHA256: m.FileSHA256, FileLength: &m.FileLength, Mimetype: &m.Mimetype}
}

// writeFile writes through a temporary name, so a crash never leaves a half file that looks done.
func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path+".tmp", data, 0o644); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

func oggPath(dir, id string) string { return filepath.Join(dir, "voice", id+".ogg") }

// cachedWAV is the decoded copy voice.play plays.
func cachedWAV(dir, id string) string { return filepath.Join(dir, "cache", "voice", id+".wav") }

// downloadVoice fetches a note, decrypted, into voice/<id>.ogg and moves its media_state to 2,
// or to 3 on a failure. A note with no waveform gets one from the decoded audio.
func (st *Store) downloadVoice(ctx context.Context, cli *whatsmeow.Client, dir string, row msgRow) error {
	var wf []byte
	err := func() error {
		var info mediaInfo
		if err := json.Unmarshal([]byte(row.Media), &info); err != nil {
			return err
		}
		data, err := cli.Download(ctx, info.message())
		if err != nil {
			return err
		}
		if len(row.Waveform) == 0 {
			if pcm, err := decodeOpus(data); err == nil && len(pcm) > 0 {
				wf = waveform(pcm)
			}
		}
		return writeFile(oggPath(dir, row.ID), data)
	}()
	state := 2
	if err != nil {
		state = 3
	}
	st.ops <- func(t *storeTx) { t.setMedia(row.Chat, row.ID, state, wf) }
	return err
}

// liveVoice downloads a note that just arrived.
func (st *Store) liveVoice(cli *whatsmeow.Client, row msgRow) {
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	if err := st.downloadVoice(ctx, cli, st.srv.dir, row); err != nil {
		st.log.Warnf("voice note %s: %v", row.ID, err)
	}
}

// row reads one message through the writer.
func (st *Store) row(chat, id string) (msgRow, bool) {
	type got struct {
		r  msgRow
		ok bool
	}
	c := make(chan got, 1)
	st.ops <- func(t *storeTx) {
		r, ok := t.messageByID(chat, id)
		c <- got{r, ok}
	}
	g := <-c
	return g.r, g.ok
}

type voiceReq struct{ Chat, ID string }

func parseVoice(req json.RawMessage) (voiceReq, error) {
	var r voiceReq
	if err := json.Unmarshal(req, &r); err != nil || r.Chat == "" || r.ID == "" {
		return r, &Error{"bad_request", "voice.play needs chat and id"}
	}
	return r, nil
}

// playVoice is voice.play for both the real and the simulated account: fetch gets the OGG when
// it isn't here, notify sends the played receipt the first time one of theirs is played.
// ponytail: the cache of WAVs is never pruned; Phase 8 caps it.
func (st *Store) playVoice(dir string, r voiceReq, fetch, notify func(msgRow) error) (any, error) {
	row, ok := st.row(r.Chat, r.ID)
	if !ok || row.Kind != msgVoice {
		return nil, &Error{"not_found", "no such voice note"}
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	ogg, wav := oggPath(dir, r.ID), cachedWAV(dir, r.ID)
	if _, err := os.Stat(ogg); err != nil {
		if err := fetch(row); err != nil {
			return nil, &Error{"download_failed", err.Error()}
		}
	}
	if _, err := os.Stat(wav); err != nil {
		data, err := os.ReadFile(ogg)
		if err != nil {
			return nil, err
		}
		pcm, err := decodeOpus(data)
		if err != nil {
			return nil, err
		}
		if err := writeFile(wav, writeWAV(pcm, 48000)); err != nil {
			return nil, err
		}
	}
	if !row.Out && !row.Played {
		if err := notify(row); err != nil {
			st.log.Warnf("played receipt: %v", err) // the phone still shows it unplayed; nothing to retry
		}
		st.ops <- func(t *storeTx) { t.setPlayed(r.Chat, r.ID) }
	}
	return map[string]string{"path": wav}, nil
}

func registerMedia(srv *Server, s *Session) {
	srv.Handle("voice.play", s.handleVoicePlay)
}

func (s *Session) handleVoicePlay(req json.RawMessage) (any, error) {
	r, err := parseVoice(req)
	if err != nil {
		return nil, err
	}
	cli, jid, err := s.linked(r.Chat)
	if err != nil {
		return nil, err
	}
	fetch := func(row msgRow) error {
		ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
		defer cancel()
		return s.store.downloadVoice(ctx, cli, s.srv.dir, row)
	}
	notify := func(row msgRow) error {
		ctx, cancel := context.WithTimeout(context.Background(), netTimeout)
		defer cancel()
		sender := types.EmptyJID // MarkRead wants it in groups only
		if row.Sender != "" {
			sender, _ = types.ParseJID(row.Sender)
		}
		return cli.MarkRead(ctx, []types.MessageID{row.ID}, time.Now(), jid, sender, types.ReceiptTypePlayed)
	}
	return s.store.playVoice(s.srv.dir, r, fetch, notify)
}
