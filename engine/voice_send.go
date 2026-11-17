package main

import (
	"context"
	"encoding/json"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
)

func registerVoiceSend(srv *Server, s *Session) {
	srv.Handle("voice.start", s.handleVoiceStart)
	srv.Handle("voice.send", s.handleVoiceSend)
	srv.Handle("voice.cancel", s.handleVoiceCancel)
}

type voicePathReq struct{ Chat, Path, Quote string }

func parseVoicePath(req json.RawMessage, cmd string) (voicePathReq, error) {
	var r voicePathReq
	if err := json.Unmarshal(req, &r); err != nil || r.Path == "" {
		return r, &Error{"bad_request", cmd + " needs a path"}
	}
	return r, nil
}

// stopRecording pauses our "recording…" in a chat, if there is one and we are linked.
func (s *Session) stopRecording(chat string) {
	cli := s.client()
	jid, err := types.ParseJID(chat)
	if chat == "" || cli.Store.ID == nil || err != nil {
		return
	}
	if err := chatState(cli, jid, false, true); err != nil {
		s.log.Warnf("voice: pausing the recording state: %v", err)
	}
}

// handleVoiceStart is voice.start: the engine starts reading the growing WAV, and tells the
// chat we are recording.
// ponytail: "recording…" is sent once; the main phone may let it lapse after a few seconds.
// Re-send it from the poll loop if the Q10 test shows that.
func (s *Session) handleVoiceStart(req json.RawMessage) (any, error) {
	r, err := parseVoicePath(req, "voice.start")
	if err != nil {
		return nil, err
	}
	cli, jid, err := s.linked(r.Chat)
	if err != nil {
		return nil, err
	}
	old, err := s.rec.start(s.srv.dir, r.Chat, r.Path, s.log)
	if err != nil {
		return nil, err
	}
	if old != "" && old != r.Chat {
		s.stopRecording(old)
	}
	return nil, chatState(cli, jid, true, true)
}

// handleVoiceSend is voice.send: the tail is encoded and the row is in as sending before the
// reply; the upload and the send run after it.
func (s *Session) handleVoiceSend(req json.RawMessage) (any, error) {
	r, err := parseVoicePath(req, "voice.send")
	if err != nil {
		return nil, err
	}
	cli, jid, err := s.linked(r.Chat)
	if err != nil {
		return nil, err
	}
	id := cli.GenerateMessageID()
	n, err := s.store.queueVoice(&s.rec, s.srv.dir, jid, r.Chat, r.Path, r.Quote, id)
	if err != nil {
		return nil, err
	}
	go s.uploadVoice(cli, jid, r.Chat, id, n)
	return map[string]string{"id": id}, nil
}

func (s *Session) uploadVoice(cli *whatsmeow.Client, jid types.JID, chat, id string, n *voiceNote) {
	s.stopRecording(chat)
	s.nameNew(cli, jid, chat, n.created)
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	up, err := cli.Upload(ctx, n.ogg, whatsmeow.MediaAudio)
	if err == nil {
		mime, secs, ptt := "audio/ogg; codecs=opus", uint32(n.secs), true
		am := &waE2E.AudioMessage{URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: &up.FileLength,
			Mimetype: &mime, Seconds: &secs, PTT: &ptt, Waveform: n.wave}
		if n.quote.ID != "" {
			am.ContextInfo = quoteInfo(cli, chat, n.quote)
		}
		var resp whatsmeow.SendResponse
		if resp, err = cli.SendMessage(ctx, jid, &waE2E.Message{AudioMessage: am}, whatsmeow.SendRequestExtra{ID: id}); err == nil {
			// The upload, as uploadMedia keeps a file's: a forward sends it again (forward.go).
			info, _ := json.Marshal(mediaInfo{up.DirectPath, up.MediaKey, up.FileEncSHA256, up.FileSHA256, up.FileLength, mime})
			s.store.ops <- func(t *storeTx) {
				t.sentAs(chat, id, resp.Timestamp.Unix(), stateSent)
				t.exec("UPDATE messages SET media = ? WHERE chat = ? AND id = ?", string(info), chat, id)
			}
			return
		}
	}
	s.log.Warnf("voice note %s: %v", id, err)
	s.store.ops <- func(t *storeTx) { t.sentAs(chat, id, 0, stateFailed) }
}

func (s *Session) handleVoiceCancel(req json.RawMessage) (any, error) {
	r, err := parseVoicePath(req, "voice.cancel")
	if err != nil {
		return nil, err
	}
	chat, err := s.rec.cancel(s.srv.dir, r.Path)
	if err != nil {
		return nil, err
	}
	s.stopRecording(chat)
	return nil, nil
}
