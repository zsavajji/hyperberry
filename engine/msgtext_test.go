package main

import (
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"
)

func TestContent(t *testing.T) {
	quoted := &waE2E.ContextInfo{StanzaID: proto.String("q1"),
		QuotedMessage: &waE2E.Message{Conversation: proto.String("old\nsecond line")}}
	tests := []struct {
		name  string
		m     *waE2E.Message
		kind  int
		text  string
		quote string
		prev  string // chat preview
		ok    bool
	}{
		{"text", &waE2E.Message{Conversation: proto.String("hi\nthere")}, msgText, "hi\nthere", "", "hi", true},
		{"reply", &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("yes"), ContextInfo: quoted}},
			msgText, "yes", "q1", "yes", true},
		{"photo", &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("sunset")}}, msgPhoto, "sunset", "", "Photo sunset", true},
		{"photo, no caption", &waE2E.Message{ImageMessage: &waE2E.ImageMessage{}}, msgPhoto, "", "", "Photo", true},
		{"voice", &waE2E.Message{AudioMessage: &waE2E.AudioMessage{PTT: proto.Bool(true)}}, msgVoice, "", "", "Voice message", true},
		{"audio", &waE2E.Message{AudioMessage: &waE2E.AudioMessage{}}, msgOther, "Audio", "", "Audio", true},
		{"document", &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{FileName: proto.String("a.pdf")}}, msgDoc, "a.pdf", "", "Document: a.pdf", true},
		{"sticker", &waE2E.Message{StickerMessage: &waE2E.StickerMessage{}}, msgSticker, "", "", "Sticker", true},
		{"poll", &waE2E.Message{PollCreationMessage: &waE2E.PollCreationMessage{Name: proto.String("Pizza?")}}, msgOther, "Poll: Pizza?", "", "Poll: Pizza?", true},
		{"reaction", &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Text: proto.String("x")}}, 0, "", "", "", false},
		{"empty", &waE2E.Message{MessageContextInfo: &waE2E.MessageContextInfo{}}, 0, "", "", "", false},
	}
	for _, tc := range tests {
		e := &events.Message{Message: tc.m, Info: types.MessageInfo{ID: "id", Timestamp: time.Unix(100, 0)}}
		r, ok := rowFromMessage("c", e, "")
		if ok != tc.ok {
			t.Errorf("%s: ok = %v", tc.name, ok)
			continue
		}
		if !ok {
			continue
		}
		if r.Kind != tc.kind || r.Text != tc.text || r.QuoteID != tc.quote || chatPreview(r) != tc.prev || r.TS != 100 || r.State != stateRead {
			t.Errorf("%s: %+v, preview %q", tc.name, r, chatPreview(r))
		}
		if tc.quote != "" && r.QuotePreview != "old" {
			t.Errorf("%s: quote preview %q", tc.name, r.QuotePreview)
		}
	}
}

func TestChatPreviewSender(t *testing.T) {
	r := msgRow{Kind: msgPhoto, Text: "x", SenderName: "Anna Lee"}
	if p := chatPreview(r); p != "Anna: Photo x" {
		t.Error(p)
	}
	r.Out = true
	if p := chatPreview(r); p != "You: Photo x" {
		t.Error(p)
	}
	if p := preview(msgText, string(make([]rune, 0))+"é"+string(make([]byte, 0))); p != "é" {
		t.Error(p)
	}
	long := ""
	for i := 0; i < 150; i++ {
		long += "é"
	}
	if n := len([]rune(preview(msgText, long))); n != 100 {
		t.Errorf("clipped to %d runes", n)
	}
}
