package main

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

// forwardScore is how many times a row's message was forwarded, from its extra; 0 when never.
func forwardScore(extra string) int {
	var x struct{ Forwarded int }
	json.Unmarshal([]byte(extra), &x) // not JSON, or no such key: never forwarded
	return x.Forwarded
}

// forwardContent is the message that sends m again, marked forwarded, and its new forwarding score,
// the row's plus one. A media message points at the upload already on the server, as editContent's
// does, so nothing is downloaded or uploaded again; mediaInfo has no URL and DirectPath is enough.
func forwardContent(m msgRow) (*waE2E.Message, int, error) {
	if m.Deleted {
		return nil, 0, &Error{"not_found", "that message was deleted"}
	}
	score := forwardScore(m.Extra) + 1
	ci := &waE2E.ContextInfo{IsForwarded: proto.Bool(true), ForwardingScore: proto.Uint32(uint32(score))}
	switch m.Kind {
	case msgText:
		return &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: &m.Text, ContextInfo: ci}}, score, nil
	case msgPhoto, msgVideo, msgDoc, msgAudio, msgVoice, msgSticker:
	default: // a location, contact, poll, event, invite, or a line the app words
		return nil, 0, &Error{"bad_request", "this kind of message can't be forwarded"}
	}
	var info mediaInfo
	if json.Unmarshal([]byte(m.Media), &info) != nil || info.DirectPath == "" { // ours still sending, or failed
		return nil, 0, &Error{"bad_request", "this message's file isn't on WhatsApp's servers"}
	}
	msg := &waE2E.Message{}
	switch d := info.message(m.Kind).(type) {
	case *waE2E.ImageMessage:
		d.JPEGThumbnail, d.ContextInfo = m.Thumb, ci
		if m.Text != "" {
			d.Caption = &m.Text
		}
		if m.Width > 0 && m.Height > 0 {
			d.Width, d.Height = proto.Uint32(uint32(m.Width)), proto.Uint32(uint32(m.Height))
		}
		msg.ImageMessage = d
	case *waE2E.VideoMessage:
		d.JPEGThumbnail, d.ContextInfo = m.Thumb, ci
		if m.Text != "" {
			d.Caption = &m.Text
		}
		msg.VideoMessage = d
	case *waE2E.DocumentMessage:
		d.FileName, d.Title, d.ContextInfo = &m.Text, &m.Text, ci
		msg.DocumentMessage = d
	case *waE2E.StickerMessage:
		d.ContextInfo = ci
		msg.StickerMessage = d
		if info.lottie() { // wrapped, as WhatsApp sends one (stickerOut)
			d.IsLottie, d.IsAnimated = proto.Bool(true), proto.Bool(true)
			msg = &waE2E.Message{LottieStickerMessage: &waE2E.FutureProofMessage{Message: msg}}
		}
	case *waE2E.AudioMessage:
		d.PTT, d.ContextInfo = proto.Bool(m.Kind == msgVoice), ci
		if m.Kind == msgVoice {
			d.Seconds, d.Waveform = proto.Uint32(uint32(m.Duration)), m.Waveform
		}
		msg.AudioMessage = d
	}
	return msg, score, nil
}

// forwardReq is msg.forward: ID is the message to forward, NewID the id the caller chose for the copy, if it did (outID).
type forwardReq struct {
	Chat, ID, To string
	NewID        string `json:"newId"`
}

func parseForward(req json.RawMessage) (forwardReq, error) {
	var r forwardReq
	if err := json.Unmarshal(req, &r); err != nil || r.Chat == "" || r.ID == "" || r.To == "" {
		return r, &Error{"bad_request", "msg.forward needs chat, id and to"}
	}
	return r, nil
}

// forwardCopy reads the message a forward names and returns what goes on the wire and our row of it
// in the target chat, as sending: the real account and the simulated one both build it here.
func (st *Store) forwardCopy(r forwardReq, id string, now int64) (*waE2E.Message, msgRow, error) {
	rows, _ := st.deletable(r.Chat, []string{r.ID}, false)
	if len(rows) == 0 {
		return nil, msgRow{}, &Error{"not_found", "no such message"}
	}
	m := rows[0]
	msg, score, err := forwardContent(m)
	if err != nil {
		return nil, msgRow{}, err
	}
	return msg, msgRow{Chat: r.To, ID: id, TS: now, Out: true, State: stateSending, Kind: m.Kind, Text: m.Text,
		Thumb: m.Thumb, Width: m.Width, Height: m.Height, Size: m.Size, Path: m.Path, Media: m.Media,
		MediaState: m.MediaState, Duration: m.Duration, Waveform: m.Waveform, Sticker: m.Sticker,
		Extra: withKey("", "forwarded", strconv.Itoa(score))}, nil
}

// handleForward is msg.forward: one message goes to one chat, as handleSend sends a text.
func (s *Session) handleForward(req json.RawMessage) (any, error) {
	r, err := parseForward(req)
	if err != nil {
		return nil, err
	}
	cli, jid, err := s.linked(r.To)
	if err != nil {
		return nil, err
	}
	id, dup, err := s.outID(cli, r.To, r.NewID)
	if err != nil {
		return nil, err
	}
	if dup {
		return map[string]string{"id": id}, nil
	}
	msg, row, err := s.store.forwardCopy(r, id, time.Now().Unix())
	if err != nil {
		s.sending.Delete(id)
		return nil, err
	}
	_, created := s.store.queueRow(jid, r.To, "", row)
	s.nameNew(cli, jid, r.To, created)
	if _, err := s.sendOut(cli, r.To, id, func(c *whatsmeow.Client) error { return s.sendMsg(c, jid, r.To, id, msg) }); err != nil {
		return nil, err
	}
	return map[string]string{"id": id}, nil
}
