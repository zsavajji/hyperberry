package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestForwardContent(t *testing.T) {
	info, _ := json.Marshal(mediaInfo{DirectPath: "/v/x", MediaKey: []byte{1}, FileLength: 9, Mimetype: "image/jpeg"})

	m, score, err := forwardContent(msgRow{Kind: msgText, Text: "hi", Extra: `{"forwarded":3}`})
	ci := m.GetExtendedTextMessage().GetContextInfo()
	if err != nil || score != 4 || m.GetExtendedTextMessage().GetText() != "hi" || !ci.GetIsForwarded() || ci.GetForwardingScore() != 4 {
		t.Errorf("text: %v score %d %v", m, score, err)
	}
	if _, score, _ := forwardContent(msgRow{Kind: msgText, Text: "hi"}); score != 1 {
		t.Errorf("never forwarded: score %d, want 1", score)
	}

	m, _, err = forwardContent(msgRow{Kind: msgPhoto, Text: "look", Media: string(info), Thumb: []byte{7}, Width: 8, Height: 6})
	img := m.GetImageMessage()
	if err != nil || img.GetDirectPath() != "/v/x" || img.GetMediaKey()[0] != 1 || img.GetCaption() != "look" ||
		!img.GetContextInfo().GetIsForwarded() || img.GetWidth() != 8 || img.GetJPEGThumbnail()[0] != 7 || img.URL != nil {
		t.Errorf("photo: %v %v", m, err)
	}

	wave := bytes.Repeat([]byte{5}, 64)
	m, _, err = forwardContent(msgRow{Kind: msgVoice, Media: string(info), Duration: 12, Waveform: wave})
	voice := m.GetAudioMessage()
	if err != nil || !voice.GetPTT() || voice.GetSeconds() != 12 || !bytes.Equal(voice.GetWaveform(), wave) || !voice.GetContextInfo().GetIsForwarded() {
		t.Errorf("voice: %v %v", m, err)
	}

	for name, row := range map[string]msgRow{
		"poll":             {Kind: msgPoll, Text: "Pizza?"},
		"deleted":          {Kind: msgText, Deleted: true},
		"photo, no upload": {Kind: msgPhoto},
		"photo, bad media": {Kind: msgPhoto, Media: "{}"},
	} {
		if m, _, err := forwardContent(row); err == nil || m != nil {
			t.Errorf("%s: %v %v, want an error", name, m, err)
		}
	}
}

func TestSimForward(t *testing.T) {
	s, st, _ := linkedSim(t)
	from, to := simChats[2].jid, simChats[5].jid
	if _, err := s.handleForward([]byte(`{"chat":"` + from + `","id":"SIMSEED2","to":"` + to + `"}`)); err != nil {
		t.Fatal(err)
	}
	eventually(t, st, 1, `SELECT count(*) FROM messages WHERE chat = ? AND out = 1 AND state = ? AND extra = '{"forwarded":1}'
AND text = (SELECT text FROM messages WHERE chat = ? AND id = 'SIMSEED2')`, to, stateSent, from)

	for req, want := range map[string]string{
		`{"chat":"` + from + `","id":"NOPE","to":"` + to + `"}`: "not_found",
		`{"chat":"` + from + `","id":"SIMSEED2"}`:               "bad_request",
	} {
		if _, err := s.handleForward([]byte(req)); code(err) != want {
			t.Errorf("%s: %v, want %s", req, err, want)
		}
	}
}
