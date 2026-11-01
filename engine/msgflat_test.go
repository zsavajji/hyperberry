package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"
)

// psaVideo is the shape of the message the official account sent on the first real link: a
// video with a title, a text and one native-flow button.
func psaVideo() *waE2E.Message {
	return &waE2E.Message{InteractiveMessage: &waE2E.InteractiveMessage{
		Header: &waE2E.InteractiveMessage_Header{
			Title: proto.String("Sincronizza i tuoi contatti"),
			Media: &waE2E.InteractiveMessage_Header_VideoMessage{VideoMessage: &waE2E.VideoMessage{
				DirectPath: proto.String("/v/psa"), Seconds: proto.Uint32(12), Mimetype: proto.String("video/mp4")}},
		},
		Body: &waE2E.InteractiveMessage_Body{Text: proto.String("per trovare i tuoi preferiti")},
		InteractiveMessage: &waE2E.InteractiveMessage_NativeFlowMessage_{NativeFlowMessage: &waE2E.InteractiveMessage_NativeFlowMessage{
			Buttons: []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{
				{Name: proto.String("cta_url"), ButtonParamsJSON: proto.String(`{"display_text":"Inizia","url":"x"}`)},
				{Name: proto.String("junk"), ButtonParamsJSON: proto.String(`not json`)},
			}}},
	}}
}

func TestFlatten(t *testing.T) {
	const psaText = "Sincronizza i tuoi contatti\nper trovare i tuoi preferiti\n\nInizia"

	in := psaVideo()
	v := flatten(in).GetVideoMessage()
	if v.GetCaption() != psaText || v.GetDirectPath() != "/v/psa" || v.GetSeconds() != 12 {
		t.Errorf("interactive: caption %q, %v", v.GetCaption(), v)
	}
	if in.GetInteractiveMessage().GetHeader().GetVideoMessage().Caption != nil {
		t.Error("the event's own message was changed")
	}

	tpl := &waE2E.HydratedTemplateButton{}
	qr := &waE2E.HydratedTemplateButton{HydratedButton: &waE2E.HydratedTemplateButton_QuickReplyButton{
		QuickReplyButton: &waE2E.HydratedTemplateButton_HydratedQuickReplyButton{DisplayText: proto.String("Yes")}}}
	url := &waE2E.HydratedTemplateButton{HydratedButton: &waE2E.HydratedTemplateButton_UrlButton{
		UrlButton: &waE2E.HydratedTemplateButton_HydratedURLButton{DisplayText: proto.String("Open")}}}
	call := &waE2E.HydratedTemplateButton{HydratedButton: &waE2E.HydratedTemplateButton_CallButton{
		CallButton: &waE2E.HydratedTemplateButton_HydratedCallButton{DisplayText: proto.String("Call us")}}}
	hydrated := &waE2E.TemplateMessage_HydratedFourRowTemplate{
		Title:               &waE2E.TemplateMessage_HydratedFourRowTemplate_ImageMessage{ImageMessage: &waE2E.ImageMessage{DirectPath: proto.String("/i/1")}},
		HydratedContentText: proto.String("Your order"), HydratedFooterText: proto.String("Shop"),
		HydratedButtons: []*waE2E.HydratedTemplateButton{qr, url, call, tpl},
	}
	const tplText = "Your order\nShop\n\nYes\nOpen\nCall us"
	for name, m := range map[string]*waE2E.Message{
		"hydratedTemplate":        {TemplateMessage: &waE2E.TemplateMessage{HydratedTemplate: hydrated}},
		"hydratedFourRowTemplate": {TemplateMessage: &waE2E.TemplateMessage{Format: &waE2E.TemplateMessage_HydratedFourRowTemplate_{HydratedFourRowTemplate: hydrated}}},
	} {
		if i := flatten(m).GetImageMessage(); i.GetCaption() != tplText || i.GetDirectPath() != "/i/1" {
			t.Errorf("%s: %v", name, i)
		}
	}

	btn := func(s string) *waE2E.ButtonsMessage_Button {
		return &waE2E.ButtonsMessage_Button{ButtonText: &waE2E.ButtonsMessage_Button_ButtonText{DisplayText: proto.String(s)}}
	}
	ci := &waE2E.ContextInfo{StanzaID: proto.String("q1")}
	bm := &waE2E.Message{ButtonsMessage: &waE2E.ButtonsMessage{
		Header:      &waE2E.ButtonsMessage_Text{Text: "  Hello  "},
		ContentText: proto.String("Pick one"), Buttons: []*waE2E.ButtonsMessage_Button{btn("A"), btn("B")}, ContextInfo: ci}}
	if x := flatten(bm).GetExtendedTextMessage(); x.GetText() != "Hello\nPick one\n\nA\nB" || x.GetContextInfo() != ci {
		t.Errorf("buttons, text only: %v", x)
	}

	// A document keeps its name; one with none shows the text. The thumbnail alone is a photo.
	doc := func(name string) *waE2E.Message {
		return &waE2E.Message{ButtonsMessage: &waE2E.ButtonsMessage{
			Header:      &waE2E.ButtonsMessage_DocumentMessage{DocumentMessage: &waE2E.DocumentMessage{FileName: proto.String(name)}},
			ContentText: proto.String("Invoice")}}
	}
	if _, text, _ := content(doc("a.pdf")); text != "a.pdf" {
		t.Errorf("named document: %q", text)
	}
	if kind, text, _ := content(doc("")); kind != msgDoc || text != "Invoice" {
		t.Errorf("unnamed document: %d %q", kind, text)
	}
	thumb := &waE2E.Message{InteractiveMessage: &waE2E.InteractiveMessage{
		Header: &waE2E.InteractiveMessage_Header{Media: &waE2E.InteractiveMessage_Header_JPEGThumbnail{JPEGThumbnail: []byte{1, 2}}},
		Body:   &waE2E.InteractiveMessage_Body{Text: proto.String("Look")}}}
	if i := flatten(thumb).GetImageMessage(); i.GetCaption() != "Look" || len(i.GetJPEGThumbnail()) != 2 {
		t.Errorf("thumbnail: %v", i)
	}
	loc := &waE2E.Message{InteractiveMessage: &waE2E.InteractiveMessage{
		Header: &waE2E.InteractiveMessage_Header{Media: &waE2E.InteractiveMessage_Header_LocationMessage{
			LocationMessage: &waE2E.LocationMessage{Name: proto.String("Shop")}}}}}
	if kind, text, _ := content(loc); kind != msgOther || text != "Location" {
		t.Errorf("location: %d %q", kind, text)
	}

	// Nothing to show, or not a business message: as it was.
	for name, m := range map[string]*waE2E.Message{
		"empty interactive": {InteractiveMessage: &waE2E.InteractiveMessage{}},
		"four-row template": {TemplateMessage: &waE2E.TemplateMessage{Format: &waE2E.TemplateMessage_FourRowTemplate_{FourRowTemplate: &waE2E.TemplateMessage_FourRowTemplate{}}}},
		"text":              {Conversation: proto.String("hi")},
		"nil":               nil,
	} {
		if flatten(m) != m {
			t.Errorf("%s: changed", name)
		}
	}
	if kind, text, ok := content(&waE2E.Message{InteractiveMessage: &waE2E.InteractiveMessage{}}); !ok || kind != msgOther || text != unknownText {
		t.Errorf("empty interactive: %d %q %v", kind, text, ok)
	}

	// A reply to one previews as the video does.
	reply := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("ok"),
		ContextInfo: &waE2E.ContextInfo{StanzaID: proto.String("q"), QuotedMessage: psaVideo()}}}
	if id, prev := quoteOf(reply); id != "q" || !strings.HasPrefix(prev, "Video Sincronizza") {
		t.Errorf("quote: %q %q", id, prev)
	}
}

func TestFieldNames(t *testing.T) {
	m := &waE2E.Message{InteractiveMessage: &waE2E.InteractiveMessage{}, MessageContextInfo: &waE2E.MessageContextInfo{}}
	if got := fieldNames(m); got != "interactiveMessage, messageContextInfo" {
		t.Errorf("%q", got)
	}
}

// A business message goes into chats.db as what it wraps, and what content() can't place is
// logged by field name.
func TestBusinessMessageStored(t *testing.T) {
	st, cli := historyFixture(t)
	var logged bytes.Buffer
	st.log = newLogger(&logged, "INFO")
	msg := func(id string, m *waE2E.Message) *events.Message {
		return &events.Message{Message: m, Info: types.MessageInfo{ID: id, Timestamp: time.Unix(1700000000, 0),
			MessageSource: types.MessageSource{Chat: types.PSAJID, Sender: types.PSAJID}}}
	}
	st.handle(cli, msg("PSA1", psaVideo()))
	st.handle(cli, msg("PSA2", &waE2E.Message{InteractiveMessage: &waE2E.InteractiveMessage{}, MessageContextInfo: &waE2E.MessageContextInfo{}}))
	if err := st.runOp(func(*storeTx) {}); err != nil {
		t.Fatal(err)
	}
	var kind, dur, state int
	var text string
	st.db.QueryRow("SELECT kind, text, duration, media_state FROM messages WHERE id = 'PSA1'").Scan(&kind, &text, &dur, &state)
	if kind != msgVideo || text != "Sincronizza i tuoi contatti\nper trovare i tuoi preferiti\n\nInizia" || dur != 12 || state != 1 {
		t.Errorf("stored: kind %d, %q, %d s, media_state %d", kind, text, dur, state)
	}
	var name string
	st.db.QueryRow("SELECT name FROM chats WHERE jid = ?", types.PSAJID.String()).Scan(&name)
	if name != "WhatsApp" {
		t.Errorf("chat named %q", name)
	}
	if got := logged.String(); strings.Count(got, "unknown message type: interactiveMessage, messageContextInfo") != 1 {
		t.Errorf("log: %q", got)
	}
}
