package main

import (
	"cmp"
	"encoding/json"
	"strings"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

// bizMsg is what a business message says, whichever of its three formats carried it: at most
// one of the media, the lines of text in order, and the labels of the buttons.
type bizMsg struct {
	doc     *waE2E.DocumentMessage
	img     *waE2E.ImageMessage
	vid     *waE2E.VideoMessage
	loc     *waE2E.LocationMessage
	thumb   []byte
	lines   []string
	buttons []string
	ci      *waE2E.ContextInfo
}

// flatten turns a business message into a plain one: the official account and businesses send
// interactive, template and buttons messages (a video with a title, a text and a button), and
// the Q10 can't act on their buttons (they open the phone's own settings or a business's
// flows). So they show as what they wrap, a photo, video, document or text, with the title, the
// text, the footer and the button labels as its caption. Lists, their replies, products and
// orders do the same (msgbiz.go), which also unwraps round video notes and the future-proof
// wrappers. Anything else, and a business message with nothing to show, comes back as it was.
func flatten(m *waE2E.Message) *waE2E.Message {
	var b bizMsg
	switch {
	case m.GetInteractiveMessage() != nil:
		b = fromInteractive(m.GetInteractiveMessage())
	case m.GetTemplateMessage() != nil:
		b = fromTemplate(m.GetTemplateMessage())
	case m.GetButtonsMessage() != nil:
		b = fromButtons(m.GetButtonsMessage())
	default:
		var ok bool
		if b, ok = moreBiz(m); !ok {
			return unwrap(m) // msgbiz.go
		}
	}
	if out := b.message(); out != nil {
		return out
	}
	return m
}

func fromInteractive(i *waE2E.InteractiveMessage) bizMsg {
	h := i.GetHeader()
	b := bizMsg{doc: h.GetDocumentMessage(), img: h.GetImageMessage(), vid: h.GetVideoMessage(),
		loc: h.GetLocationMessage(), thumb: h.GetJPEGThumbnail(), ci: i.GetContextInfo(),
		lines: []string{h.GetTitle(), h.GetSubtitle(), i.GetBody().GetText(), i.GetFooter().GetText()}}
	for _, nb := range i.GetNativeFlowMessage().GetButtons() {
		var p struct {
			DisplayText string `json:"display_text"`
		}
		json.Unmarshal([]byte(nb.GetButtonParamsJSON()), &p) // not JSON: no label
		b.buttons = append(b.buttons, p.DisplayText)
	}
	return b
}

// fromTemplate reads the hydrated forms, the two places WhatsApp puts one. The unhydrated
// four-row template is made of message templates the Q10 can't fill in: it stays "Message".
func fromTemplate(t *waE2E.TemplateMessage) bizMsg {
	h := cmp.Or(t.GetHydratedTemplate(), t.GetHydratedFourRowTemplate())
	b := bizMsg{doc: h.GetDocumentMessage(), img: h.GetImageMessage(), vid: h.GetVideoMessage(),
		loc: h.GetLocationMessage(), ci: t.GetContextInfo(),
		lines: []string{h.GetHydratedTitleText(), h.GetHydratedContentText(), h.GetHydratedFooterText()}}
	for _, hb := range h.GetHydratedButtons() {
		b.buttons = append(b.buttons, cmp.Or(hb.GetQuickReplyButton().GetDisplayText(),
			hb.GetUrlButton().GetDisplayText(), hb.GetCallButton().GetDisplayText()))
	}
	return b
}

func fromButtons(m *waE2E.ButtonsMessage) bizMsg {
	b := bizMsg{doc: m.GetDocumentMessage(), img: m.GetImageMessage(), vid: m.GetVideoMessage(),
		loc: m.GetLocationMessage(), ci: m.GetContextInfo(),
		lines: []string{m.GetText(), m.GetContentText(), m.GetFooterText()}}
	for _, mb := range m.GetButtons() {
		b.buttons = append(b.buttons, mb.GetButtonText().GetDisplayText())
	}
	return b
}

// text is the lines, then a blank line and the button labels one to a line; empty parts go.
func (b bizMsg) text() string {
	var out, labels []string
	for _, l := range b.lines {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	for _, l := range b.buttons {
		if l = strings.TrimSpace(l); l != "" {
			labels = append(labels, l)
		}
	}
	text := strings.Join(out, "\n")
	if len(labels) > 0 {
		if text != "" {
			text += "\n\n"
		}
		text += strings.Join(labels, "\n")
	}
	return text
}

// message is the plain message for b, or nil when there is neither media nor text. The media
// is cloned: the event's message isn't ours to change.
func (b bizMsg) message() *waE2E.Message {
	text := b.text()
	switch {
	case b.img != nil:
		i := proto.Clone(b.img).(*waE2E.ImageMessage)
		i.Caption, i.ContextInfo = &text, cmp.Or(i.ContextInfo, b.ci)
		return &waE2E.Message{ImageMessage: i}
	case b.vid != nil:
		v := proto.Clone(b.vid).(*waE2E.VideoMessage)
		v.Caption, v.ContextInfo = &text, cmp.Or(v.ContextInfo, b.ci)
		return &waE2E.Message{VideoMessage: v}
	case b.doc != nil:
		// ponytail: a document shows (and is saved under) its file name, so the text shows only
		// for one with no name. A caption column would show both.
		d := proto.Clone(b.doc).(*waE2E.DocumentMessage)
		d.Caption, d.ContextInfo = &text, cmp.Or(d.ContextInfo, b.ci)
		if d.GetFileName() == "" {
			d.Title = &text
		}
		return &waE2E.Message{DocumentMessage: d}
	case b.loc != nil:
		// ponytail: a location shows its own name and address, so the business's text is dropped.
		l := proto.Clone(b.loc).(*waE2E.LocationMessage)
		l.ContextInfo = cmp.Or(l.ContextInfo, b.ci)
		return &waE2E.Message{LocationMessage: l}
	case len(b.thumb) > 0: // a picture with no file behind it: the row keeps the thumbnail
		return &waE2E.Message{ImageMessage: &waE2E.ImageMessage{JPEGThumbnail: b.thumb, Caption: &text, ContextInfo: b.ci}}
	case text != "":
		return &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: &text, ContextInfo: b.ci}}
	}
	return nil
}
