package main

import (
	"cmp"
	"encoding/json"
	"strings"

	"github.com/polymorfa/hypermeow/proto/waE2E"
)

// The message kinds of step 6.5: what content() says of them, and what fillExtra keeps of each
// in messages.extra (docs/protocol.md has the contract).

// noticeWords are the English forms of a notice's codes, for previews; the UI words the
// notice itself in the phone's language.
var noticeWords = map[string]string{
	"call": "Voice call", "video_call": "Video call",
	"missed_call": "Missed voice call", "missed_video_call": "Missed video call",
	"pin": "Pinned a message", "unpin": "Unpinned a message",
	"view_once": "View once message", "on_phone": "Message on your phone",
}

// joinLines joins the parts that aren't empty, one to a line.
func joinLines(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "\n")
}

// pollOf is the poll a message creates: WhatsApp has sent it in six fields over the years (the
// fourth, a wrapper, is opened by flatten).
func pollOf(m *waE2E.Message) *waE2E.PollCreationMessage {
	return cmp.Or(m.GetPollCreationMessage(), m.GetPollCreationMessageV2(), m.GetPollCreationMessageV3(),
		m.GetPollCreationMessageV5(), m.GetPollCreationMessageV6())
}

// silent lists the messages with nothing to show, one by one so that content() still logs a
// type it hasn't heard of: the votes (polls.go) and event replies, encrypted reactions, an
// album's parent (its photos arrive as messages of their own), and the key and history traffic.
func silent(m *waE2E.Message) bool {
	return m.GetPollUpdateMessage() != nil || m.GetEncEventResponseMessage() != nil ||
		m.GetEncReactionMessage() != nil || m.GetKeepInChatMessage() != nil || m.GetAlbumMessage() != nil ||
		m.GetMessageHistoryNotice() != nil || m.GetMessageHistoryBundle() != nil ||
		m.GetStickerSyncRmrMessage() != nil || m.GetGroupRootKeyShare() != nil ||
		m.GetRootSecretDistributeMessage() != nil || m.GetPollAddOptionMessage() != nil ||
		m.GetSecretEncryptedMessage() != nil
}

// moreKinds is content() for the kinds of step 6.5. done is false when m is none of them or the
// silent ones; ok is false when it is one with nothing to show.
func moreKinds(m *waE2E.Message) (kind int, text string, ok, done bool) {
	switch {
	case m.GetLocationMessage() != nil:
		l := m.GetLocationMessage()
		return msgLocation, cmp.Or(joinLines(l.GetName(), l.GetAddress()), strings.TrimSpace(l.GetComment())), true, true
	case m.GetLiveLocationMessage() != nil:
		return msgLocation, strings.TrimSpace(m.GetLiveLocationMessage().GetCaption()), true, true
	case m.GetContactMessage() != nil:
		return msgContact, contactName(m.GetContactMessage()), true, true
	case m.GetContactsArrayMessage() != nil:
		a := m.GetContactsArrayMessage()
		var names []string
		for _, c := range a.GetContacts() {
			if n := contactName(c); n != "" {
				names = append(names, n)
			}
		}
		return msgContact, cmp.Or(strings.Join(names, ", "), a.GetDisplayName()), true, true
	case pollOf(m) != nil:
		return msgPoll, pollOf(m).GetName(), true, true
	case m.GetEventMessage() != nil:
		e := m.GetEventMessage()
		return msgEvent, joinLines(e.GetName(), e.GetDescription()), true, true
	case m.GetGroupInviteMessage() != nil:
		g := m.GetGroupInviteMessage()
		return msgInvite, joinLines(g.GetGroupName(), g.GetCaption()), true, true
	case m.GetPinInChatMessage() != nil:
		switch m.GetPinInChatMessage().GetType() {
		case waE2E.PinInChatMessage_PIN_FOR_ALL:
			return msgNotice, "pin", true, true
		case waE2E.PinInChatMessage_UNPIN_FOR_ALL:
			return msgNotice, "unpin", true, true
		}
		return 0, "", false, true
	case m.GetCallLogMesssage() != nil: // sic, hypermeow's spelling
		c := m.GetCallLogMesssage()
		code := "call"
		if c.GetIsVideo() {
			code = "video_call"
		}
		switch c.GetCallOutcome() {
		case waE2E.CallLogMessage_MISSED, waE2E.CallLogMessage_SILENCED_BY_DND,
			waE2E.CallLogMessage_SILENCED_UNKNOWN_CALLER, waE2E.CallLogMessage_REJECTED:
			code = "missed_" + code
		}
		return msgNotice, code, true, true
	case m.GetPlaceholderMessage() != nil && m.GetPlaceholderMessage().GetType() == waE2E.PlaceholderMessage_MASK_LINKED_DEVICES:
		return msgNotice, "on_phone", true, true
	case silent(m):
		return 0, "", false, true
	}
	return 0, "", false, false
}

// contactName is the name a shared contact shows: the sender's own, else the vCard's.
func contactName(c *waE2E.ContactMessage) string {
	if n := strings.TrimSpace(c.GetDisplayName()); n != "" {
		return n
	}
	if fn := vcardValues(c.GetVcard(), "FN"); len(fn) > 0 {
		return fn[0]
	}
	return ""
}

// vcardValues lists the values of a vCard's lines of one property, in order, trimmed and not
// empty. A line is "name;parameters:value", the name possibly behind a group ("item1.TEL"), and
// the value is what follows the first colon.
// ponytail: no unfolding of continued lines or unescaping; WhatsApp's vCards don't fold.
func vcardValues(vcard, prop string) []string {
	vals := []string{}
	for _, line := range strings.Split(vcard, "\n") {
		head, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		name, _, _ := strings.Cut(head, ";")
		if i := strings.LastIndexByte(name, '.'); i >= 0 {
			name = name[i+1:]
		}
		if v := strings.TrimSpace(val); v != "" && strings.EqualFold(strings.TrimSpace(name), prop) {
			vals = append(vals, v)
		}
	}
	return vals
}

type sharedContact struct {
	Name  string   `json:"name"`
	Tel   []string `json:"tel"`
	VCard string   `json:"vcard"`
}

// keep sets x[k] unless v is empty or zero: the extras leave out what they don't have.
func keep[T comparable](x map[string]any, k string, v T) {
	var zero T
	if v != zero {
		x[k] = v
	}
}

// fillExtra adds what a message keeps of its kind: messages.extra as JSON, and the thumbnail
// of a link preview, a location or an invite. Kinds with nothing to keep leave Extra "". m is
// flattened, and r already has the kind content() gave it.
func fillExtra(r *msgRow, m *waE2E.Message) {
	x := map[string]any{}
	switch {
	case m.GetExtendedTextMessage().GetTitle() != "":
		t := m.GetExtendedTextMessage()
		x["title"], x["desc"], x["url"] = t.GetTitle(), t.GetDescription(), t.GetMatchedText()
		r.Thumb = t.GetJPEGThumbnail()
	case m.GetVideoMessage().GetGifPlayback():
		x["gif"] = true
	case m.GetStickerMessage().GetIsAnimated():
		x["animated"] = true
	case m.GetLocationMessage() != nil:
		l := m.GetLocationMessage()
		x["lat"], x["lon"], x["live"] = l.GetDegreesLatitude(), l.GetDegreesLongitude(), l.GetIsLive()
		r.Thumb = l.GetJPEGThumbnail()
	case m.GetLiveLocationMessage() != nil:
		l := m.GetLiveLocationMessage()
		x["lat"], x["lon"], x["live"] = l.GetDegreesLatitude(), l.GetDegreesLongitude(), true
		r.Thumb = l.GetJPEGThumbnail()
	case m.GetContactMessage() != nil || m.GetContactsArrayMessage() != nil:
		cs := m.GetContactsArrayMessage().GetContacts()
		if c := m.GetContactMessage(); c != nil {
			cs = []*waE2E.ContactMessage{c}
		}
		list := []sharedContact{}
		for _, c := range cs {
			list = append(list, sharedContact{contactName(c), vcardValues(c.GetVcard(), "TEL"), c.GetVcard()})
		}
		x["contacts"] = list
	case pollOf(m) != nil:
		p := pollOf(m)
		names := []string{}
		for _, o := range p.GetOptions() {
			names = append(names, o.GetOptionName())
		}
		x["options"], x["max"] = names, p.GetSelectableOptionsCount()
	case m.GetEventMessage() != nil:
		e := m.GetEventMessage()
		keep(x, "start", e.GetStartTime())
		keep(x, "end", e.GetEndTime())
		keep(x, "place", cmp.Or(e.GetLocation().GetName(), e.GetLocation().GetAddress()))
		keep(x, "link", e.GetJoinLink())
		keep(x, "canceled", e.GetIsCanceled())
	case m.GetGroupInviteMessage() != nil:
		g := m.GetGroupInviteMessage()
		keep(x, "group", g.GetGroupJID())
		keep(x, "code", g.GetInviteCode())
		keep(x, "expires", g.GetInviteExpiration())
		r.Thumb = g.GetJPEGThumbnail()
	case m.GetPinInChatMessage() != nil:
		keep(x, "msg", m.GetPinInChatMessage().GetKey().GetID())
	case m.GetCallLogMesssage() != nil:
		keep(x, "secs", m.GetCallLogMesssage().GetDurationSecs())
	}
	if len(x) == 0 {
		return
	}
	if b, err := json.Marshal(x); err == nil {
		r.Extra = string(b)
	}
}
