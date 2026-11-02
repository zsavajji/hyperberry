package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// unknownText is the text of a message content() has no kind for.
const unknownText = "Message"

// msgRow is one row of messages; the empty string stands for NULL in the optional columns.
type msgRow struct {
	Chat, ID              string
	TS                    int64
	Out                   bool
	State                 int
	Sender, SenderName    string
	Kind                  int
	Text                  string
	QuoteID, QuotePreview string
	Edited, Deleted       bool
	// Voice notes: seconds, the waveform's 64 values (0-100), download details as JSON.
	Duration   int
	Waveform   []byte
	Media      string
	MediaState int
	Played     bool
	// Photos, videos, audio files and documents (step 6.1): a JPEG thumbnail, pixels, bytes, and
	// the downloaded file once there is one.
	Thumb         []byte
	Width, Height int
	Size          int64
	Path          string
	// Step 6.5: each kind's own details as JSON (msgkinds.go), "" for none.
	Extra string
}

// content says what a message shows: its kind and text. ok is false for messages with nothing
// to show (protocol messages, reactions, key distribution and the like). A business message
// shows as what it wraps (msgflat.go), so a quote of one previews right too.
func content(m *waE2E.Message) (kind int, text string, ok bool) {
	m = flatten(m)
	switch {
	case m == nil || m.GetProtocolMessage() != nil || m.GetReactionMessage() != nil:
		return 0, "", false
	case m.GetConversation() != "":
		return msgText, m.GetConversation(), true
	case m.GetExtendedTextMessage() != nil:
		t := m.GetExtendedTextMessage().GetText()
		return msgText, t, t != ""
	case m.GetImageMessage() != nil:
		return msgPhoto, m.GetImageMessage().GetCaption(), true
	case m.GetVideoMessage() != nil:
		return msgVideo, m.GetVideoMessage().GetCaption(), true
	case m.GetAudioMessage() != nil:
		if m.GetAudioMessage().GetPTT() {
			return msgVoice, "", true
		}
		return msgAudio, "", true
	case m.GetDocumentMessage() != nil:
		d := m.GetDocumentMessage()
		if d.GetFileName() != "" {
			return msgDoc, d.GetFileName(), true
		}
		return msgDoc, d.GetTitle(), true
	case m.GetStickerMessage() != nil:
		return msgSticker, "", true
	}
	if k, t, ok, done := moreKinds(m); done {
		return k, t, ok
	}
	c := proto.Clone(m).(*waE2E.Message)
	c.MessageContextInfo, c.SenderKeyDistributionMessage = nil, nil
	if proto.Size(c) == 0 {
		return 0, "", false
	}
	return msgOther, unknownText, true
}

// fieldNames lists the fields a message has set, by name: what the log says of a message
// content() has no kind for, so the next format to handle can be told from the log.
func fieldNames(m *waE2E.Message) string {
	var names []string
	m.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, _ protoreflect.Value) bool {
		names = append(names, string(fd.Name()))
		return true
	})
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// quoteOf returns the id and a one-line preview of the message a reply answers.
func quoteOf(m *waE2E.Message) (id, prev string) {
	// Whichever field is set carries the ContextInfo: any kind can be a reply.
	var ci *waE2E.ContextInfo
	flatten(m).ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.Message() != nil && !fd.IsList() && !fd.IsMap() {
			if g, ok := v.Message().Interface().(interface{ GetContextInfo() *waE2E.ContextInfo }); ok {
				ci = g.GetContextInfo()
			}
		}
		return ci == nil
	})
	if ci.GetStanzaID() == "" {
		return "", ""
	}
	if k, t, ok := content(ci.GetQuotedMessage()); ok {
		prev = clip(preview(k, t, int(ci.GetQuotedMessage().GetAudioMessage().GetSeconds())), 80)
	}
	return ci.GetStanzaID(), prev
}

// rowFromMessage turns a message event into its row. It leaves Sender to the caller, which
// has to ask identity.go for the key. senderName is used in groups only.
func rowFromMessage(key string, e *events.Message, senderName string) (msgRow, bool) {
	kind, text, ok := content(e.Message)
	if !ok {
		return msgRow{}, false
	}
	r := msgRow{Chat: key, ID: e.Info.ID, TS: e.Info.Timestamp.Unix(), Out: e.Info.IsFromMe,
		State: stateRead, Kind: kind, Text: text}
	if r.Out {
		r.State = stateSent
	}
	if e.Info.IsGroup {
		r.SenderName = senderName
	}
	m := flatten(e.Message)
	r.QuoteID, r.QuotePreview = quoteOf(m)
	if e.IsViewOnce { // WhatsApp's companions don't show view-once media either
		r.Kind, r.Text = msgNotice, "view_once"
		return r, true
	}
	if a := m.GetAudioMessage(); a != nil && a.GetPTT() {
		r.Duration, r.Waveform = int(a.GetSeconds()), a.GetWaveform()
		if a.GetDirectPath() != "" {
			info := mediaInfo{a.GetDirectPath(), a.GetMediaKey(), a.GetFileEncSHA256(), a.GetFileSHA256(),
				a.GetFileLength(), a.GetMimetype()}
			if b, err := json.Marshal(info); err == nil {
				r.Media, r.MediaState = string(b), 1
			}
		}
	}
	fillMedia(&r, m)
	fillExtra(&r, m)
	return r, true
}

// fillMedia adds what a photo, video, sticker, document or audio file row carries (step 6.1),
// as the voice note's code above does for its own. Anything else, voice notes included, is left
// alone.
func fillMedia(r *msgRow, m *waE2E.Message) {
	var info mediaInfo
	switch {
	case m.GetImageMessage() != nil:
		i := m.GetImageMessage()
		r.Thumb, r.Width, r.Height = i.GetJPEGThumbnail(), int(i.GetWidth()), int(i.GetHeight())
		info = mediaInfo{i.GetDirectPath(), i.GetMediaKey(), i.GetFileEncSHA256(), i.GetFileSHA256(), i.GetFileLength(), i.GetMimetype()}
	case m.GetVideoMessage() != nil:
		v := m.GetVideoMessage()
		r.Thumb, r.Width, r.Height, r.Duration = v.GetJPEGThumbnail(), int(v.GetWidth()), int(v.GetHeight()), int(v.GetSeconds())
		info = mediaInfo{v.GetDirectPath(), v.GetMediaKey(), v.GetFileEncSHA256(), v.GetFileSHA256(), v.GetFileLength(), v.GetMimetype()}
	case m.GetStickerMessage() != nil: // the thumbnail is a PNG
		s := m.GetStickerMessage()
		r.Thumb, r.Width, r.Height = s.GetPngThumbnail(), int(s.GetWidth()), int(s.GetHeight())
		info = mediaInfo{s.GetDirectPath(), s.GetMediaKey(), s.GetFileEncSHA256(), s.GetFileSHA256(), s.GetFileLength(), s.GetMimetype()}
	case m.GetDocumentMessage() != nil:
		d := m.GetDocumentMessage()
		r.Thumb = d.GetJPEGThumbnail()
		info = mediaInfo{d.GetDirectPath(), d.GetMediaKey(), d.GetFileEncSHA256(), d.GetFileSHA256(), d.GetFileLength(), d.GetMimetype()}
	case m.GetAudioMessage() != nil && !m.GetAudioMessage().GetPTT():
		a := m.GetAudioMessage()
		r.Duration = int(a.GetSeconds())
		info = mediaInfo{a.GetDirectPath(), a.GetMediaKey(), a.GetFileEncSHA256(), a.GetFileSHA256(), a.GetFileLength(), a.GetMimetype()}
	default:
		return
	}
	r.Size = int64(info.FileLength)
	if info.DirectPath != "" {
		if b, err := json.Marshal(info); err == nil {
			r.Media, r.MediaState = string(b), 1
		}
	}
}

// preview is the one-line form of a message, for chat rows and quotes.
// A voice note shows its length when known.
func preview(kind int, text string, secs int) string {
	line := clip(firstLine(text), 100)
	label, sep := "", " "
	switch kind {
	case msgPhoto:
		label = "Photo"
	case msgVideo:
		label = "Video"
	case msgLocation:
		label = "Location"
	case msgContact:
		label, sep = "Contact", ": "
	case msgPoll:
		label, sep = "Poll", ": "
	case msgEvent:
		label, sep = "Event", ": "
	case msgInvite:
		label, sep = "Group invite", ": "
	case msgNotice:
		return cmp.Or(noticeWords[text], unknownText)
	case msgVoice:
		if secs > 0 {
			return fmt.Sprintf("🎤 %d:%02d", secs/60, secs%60)
		}
		return "Voice message"
	case msgAudio:
		return "Audio"
	case msgSticker:
		return "Sticker"
	case msgDoc:
		if line == "" {
			return "Document"
		}
		return "Document: " + line
	default:
		return line
	}
	if line == "" {
		return label
	}
	return label + sep + line
}

// chatPreview is the chat list line: "You: hi", "Anna: Photo".
func chatPreview(r msgRow) string {
	p := preview(r.Kind, r.Text, r.Duration)
	switch {
	case r.Out:
		return "You: " + p
	case r.SenderName != "":
		return firstWord(r.SenderName) + ": " + p
	}
	return p
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(s)
}

func firstWord(s string) string {
	w, _, _ := strings.Cut(strings.TrimSpace(s), " ")
	return w
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
