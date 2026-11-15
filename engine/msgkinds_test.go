package main

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/proto/waCommon"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"
)

// sameJSON compares two JSON documents whatever the order of their keys; "" is no document.
func sameJSON(a, b string) bool {
	if a == "" || b == "" {
		return a == b
	}
	var x, y any
	return json.Unmarshal([]byte(a), &x) == nil && json.Unmarshal([]byte(b), &y) == nil && reflect.DeepEqual(x, y)
}

const annVCard = "BEGIN:VCARD\nVERSION:3.0\nN:;Anna;;;\nFN:Anna Rossi\nitem1.TEL;type=CELL;waid=393331234567:+39 333 123 4567\nitem1.X-ABLabel:Mobile\nTEL;TYPE=HOME:  02 1234 \nTEL:\nEND:VCARD"

func TestMoreKinds(t *testing.T) {
	pin := func(typ waE2E.PinInChatMessage_Type) *waE2E.Message {
		return &waE2E.Message{PinInChatMessage: &waE2E.PinInChatMessage{Type: typ.Enum(), Key: &waCommon.MessageKey{ID: proto.String("PINNED")}}}
	}
	// A pin lasts as long as the main phone set, in the message's context (the test's clock is 100).
	lasting := func(m *waE2E.Message, secs uint32) *waE2E.Message {
		m.MessageContextInfo = &waE2E.MessageContextInfo{MessageAddOnDurationInSecs: proto.Uint32(secs)}
		return m
	}
	call := func(outcome waE2E.CallLogMessage_CallOutcome, video bool, secs int64) *waE2E.Message {
		return &waE2E.Message{CallLogMesssage: &waE2E.CallLogMessage{CallOutcome: outcome.Enum(), IsVideo: proto.Bool(video), DurationSecs: proto.Int64(secs)}}
	}
	poll := func() *waE2E.PollCreationMessage {
		return &waE2E.PollCreationMessage{Name: proto.String("Pizza?"), SelectableOptionsCount: proto.Uint32(2),
			Options: []*waE2E.PollCreationMessage_Option{{OptionName: proto.String("Yes")}, {OptionName: proto.String("No")}}}
	}
	const pollExtra = `{"options":["Yes","No"],"max":2}`
	tests := []struct {
		name  string
		m     *waE2E.Message
		kind  int
		text  string
		prev  string // chat preview
		extra string
		once  bool // the message came in a view-once wrapper
	}{
		{"location", &waE2E.Message{LocationMessage: &waE2E.LocationMessage{Name: proto.String("Duomo"), Address: proto.String("Piazza Duomo"),
			DegreesLatitude: proto.Float64(45.464), DegreesLongitude: proto.Float64(9.19), JPEGThumbnail: []byte{1}}},
			msgLocation, "Duomo\nPiazza Duomo", "Location Duomo", `{"lat":45.464,"lon":9.19,"live":false}`, false},
		{"location, address only", &waE2E.Message{LocationMessage: &waE2E.LocationMessage{Address: proto.String("Via Roma 1")}},
			msgLocation, "Via Roma 1", "Location Via Roma 1", `{"lat":0,"lon":0,"live":false}`, false},
		{"location, comment only", &waE2E.Message{LocationMessage: &waE2E.LocationMessage{Comment: proto.String("here")}},
			msgLocation, "here", "Location here", `{"lat":0,"lon":0,"live":false}`, false},
		{"location, nothing", &waE2E.Message{LocationMessage: &waE2E.LocationMessage{}},
			msgLocation, "", "Location", `{"lat":0,"lon":0,"live":false}`, false},
		{"live location", &waE2E.Message{LiveLocationMessage: &waE2E.LiveLocationMessage{Caption: proto.String("on my way"),
			DegreesLatitude: proto.Float64(1.5), DegreesLongitude: proto.Float64(-2.5)}},
			msgLocation, "on my way", "Location on my way", `{"lat":1.5,"lon":-2.5,"live":true}`, false},
		{"contact", &waE2E.Message{ContactMessage: &waE2E.ContactMessage{DisplayName: proto.String("Anna"), Vcard: proto.String(annVCard)}},
			msgContact, "Anna", "Contact: Anna",
			`{"contacts":[{"name":"Anna","tel":["+39 333 123 4567","02 1234"],"vcard":` + jsonStr(annVCard) + `}]}`, false},
		{"contact, name from the vCard", &waE2E.Message{ContactMessage: &waE2E.ContactMessage{Vcard: proto.String("FN:Bob\nTEL:123")}},
			msgContact, "Bob", "Contact: Bob", `{"contacts":[{"name":"Bob","tel":["123"],"vcard":"FN:Bob\nTEL:123"}]}`, false},
		{"contacts", &waE2E.Message{ContactsArrayMessage: &waE2E.ContactsArrayMessage{DisplayName: proto.String("2 contacts"),
			Contacts: []*waE2E.ContactMessage{{DisplayName: proto.String("Anna"), Vcard: proto.String("TEL:1")}, {DisplayName: proto.String("Bob")}}}},
			msgContact, "Anna, Bob", "Contact: Anna, Bob",
			`{"contacts":[{"name":"Anna","tel":["1"],"vcard":"TEL:1"},{"name":"Bob","tel":[],"vcard":""}]}`, false},
		{"contacts, no list", &waE2E.Message{ContactsArrayMessage: &waE2E.ContactsArrayMessage{DisplayName: proto.String("2 contacts")}},
			msgContact, "2 contacts", "Contact: 2 contacts", `{"contacts":[]}`, false},
		{"poll", &waE2E.Message{PollCreationMessage: poll()}, msgPoll, "Pizza?", "Poll: Pizza?", pollExtra, false},
		{"poll v2", &waE2E.Message{PollCreationMessageV2: poll()}, msgPoll, "Pizza?", "Poll: Pizza?", pollExtra, false},
		{"poll v3", &waE2E.Message{PollCreationMessageV3: poll()}, msgPoll, "Pizza?", "Poll: Pizza?", pollExtra, false},
		{"poll v5", &waE2E.Message{PollCreationMessageV5: poll()}, msgPoll, "Pizza?", "Poll: Pizza?", pollExtra, false},
		{"poll v6", &waE2E.Message{PollCreationMessageV6: poll()}, msgPoll, "Pizza?", "Poll: Pizza?", pollExtra, false},
		{"poll v4, wrapped", &waE2E.Message{PollCreationMessageV4: &waE2E.FutureProofMessage{Message: &waE2E.Message{PollCreationMessage: poll()}}},
			msgPoll, "Pizza?", "Poll: Pizza?", pollExtra, false},
		{"poll, any number of choices", &waE2E.Message{PollCreationMessage: &waE2E.PollCreationMessage{Name: proto.String("Q"),
			Options: []*waE2E.PollCreationMessage_Option{{OptionName: proto.String("A")}}}},
			msgPoll, "Q", "Poll: Q", `{"options":["A"],"max":0}`, false},
		{"event", &waE2E.Message{EventMessage: &waE2E.EventMessage{Name: proto.String("Party"), Description: proto.String("Bring cake\nand wine"),
			StartTime: proto.Int64(1800000000), EndTime: proto.Int64(1800003600), JoinLink: proto.String("https://call.whatsapp.com/x"),
			Location: &waE2E.LocationMessage{Name: proto.String("My place"), Address: proto.String("Via Roma 1")}}},
			msgEvent, "Party\nBring cake\nand wine", "Event: Party",
			`{"start":1800000000,"end":1800003600,"place":"My place","link":"https://call.whatsapp.com/x"}`, false},
		{"event, canceled, place by address", &waE2E.Message{EventMessage: &waE2E.EventMessage{Name: proto.String("Party"), IsCanceled: proto.Bool(true),
			Location: &waE2E.LocationMessage{Address: proto.String("Via Roma 1")}}},
			msgEvent, "Party", "Event: Party", `{"place":"Via Roma 1","canceled":true}`, false},
		{"event, name only", &waE2E.Message{EventMessage: &waE2E.EventMessage{Name: proto.String("Party")}},
			msgEvent, "Party", "Event: Party", "", false},
		{"invite", &waE2E.Message{GroupInviteMessage: &waE2E.GroupInviteMessage{GroupName: proto.String("Family"), Caption: proto.String("Join us"),
			GroupJID: proto.String("120363@g.us"), InviteCode: proto.String("ABC"), InviteExpiration: proto.Int64(1800000000), JPEGThumbnail: []byte{1}}},
			msgInvite, "Family\nJoin us", "Group invite: Family", `{"group":"120363@g.us","code":"ABC","expires":1800000000}`, false},
		{"pin", pin(waE2E.PinInChatMessage_PIN_FOR_ALL), msgNotice, "pin", "Pinned a message", `{"msg":"PINNED"}`, false},
		{"unpin", pin(waE2E.PinInChatMessage_UNPIN_FOR_ALL), msgNotice, "unpin", "Unpinned a message", `{"msg":"PINNED"}`, false},
		{"pin for a week", lasting(pin(waE2E.PinInChatMessage_PIN_FOR_ALL), 604800), msgNotice, "pin", "Pinned a message", `{"msg":"PINNED","until":604900}`, false},
		{"pin of no length", lasting(pin(waE2E.PinInChatMessage_PIN_FOR_ALL), 0), msgNotice, "pin", "Pinned a message", `{"msg":"PINNED"}`, false},
		{"unpin with a length", lasting(pin(waE2E.PinInChatMessage_UNPIN_FOR_ALL), 604800), msgNotice, "unpin", "Unpinned a message", `{"msg":"PINNED"}`, false},
		{"call", call(waE2E.CallLogMessage_CONNECTED, false, 65), msgNotice, "call", "Voice call", `{"secs":65}`, false},
		{"video call", call(waE2E.CallLogMessage_CONNECTED, true, 0), msgNotice, "video_call", "Video call", "", false},
		{"call answered elsewhere", call(waE2E.CallLogMessage_ACCEPTED_ELSEWHERE, false, 0), msgNotice, "call", "Voice call", "", false},
		{"missed call", call(waE2E.CallLogMessage_MISSED, false, 0), msgNotice, "missed_call", "Missed voice call", "", false},
		{"missed video call", call(waE2E.CallLogMessage_MISSED, true, 0), msgNotice, "missed_video_call", "Missed video call", "", false},
		{"rejected call", call(waE2E.CallLogMessage_REJECTED, false, 0), msgNotice, "missed_call", "Missed voice call", "", false},
		{"call silenced by DND", call(waE2E.CallLogMessage_SILENCED_BY_DND, true, 0), msgNotice, "missed_video_call", "Missed video call", "", false},
		{"call silenced, unknown caller", call(waE2E.CallLogMessage_SILENCED_UNKNOWN_CALLER, false, 0), msgNotice, "missed_call", "Missed voice call", "", false},
		{"on the phone", &waE2E.Message{PlaceholderMessage: &waE2E.PlaceholderMessage{Type: waE2E.PlaceholderMessage_MASK_LINKED_DEVICES.Enum()}},
			msgNotice, "on_phone", "Message on your phone", "", false},
		{"view once photo", &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("secret"), JPEGThumbnail: []byte{1},
			DirectPath: proto.String("/p"), Width: proto.Uint32(10)}}, msgNotice, "view_once", "View once message", "", true},
		{"view once voice", &waE2E.Message{AudioMessage: &waE2E.AudioMessage{PTT: proto.Bool(true), DirectPath: proto.String("/a"), Seconds: proto.Uint32(5)}},
			msgNotice, "view_once", "View once message", "", true},
		{"link preview", &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("look https://x.test"),
			MatchedText: proto.String("https://x.test"), Title: proto.String("X"), Description: proto.String("An X"), JPEGThumbnail: []byte{1, 2}}},
			msgText, "look https://x.test", "look https://x.test", `{"title":"X","desc":"An X","url":"https://x.test"}`, false},
		{"text, no preview", &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("hi"), MatchedText: proto.String("x")}},
			msgText, "hi", "hi", "", false},
		{"gif", &waE2E.Message{VideoMessage: &waE2E.VideoMessage{GifPlayback: proto.Bool(true), Seconds: proto.Uint32(3)}},
			msgVideo, "", "Video", `{"gif":true}`, false},
		{"video", &waE2E.Message{VideoMessage: &waE2E.VideoMessage{Seconds: proto.Uint32(3)}}, msgVideo, "", "Video", "", false},
		// The premade thumbnail's keys (liveVideoThumb fetches it): the bytes are base64 in the JSON.
		{"video with a premade thumbnail", &waE2E.Message{VideoMessage: &waE2E.VideoMessage{Seconds: proto.Uint32(3), ThumbnailDirectPath: proto.String("/v/t/p"),
			ThumbnailSHA256: []byte{1, 2, 3}, ThumbnailEncSHA256: []byte{4, 5, 6}}},
			msgVideo, "", "Video", `{"thumbPath":"/v/t/p","thumbSha":"AQID","thumbEncSha":"BAUG"}`, false},
		{"gif with a premade thumbnail", &waE2E.Message{VideoMessage: &waE2E.VideoMessage{GifPlayback: proto.Bool(true), ThumbnailDirectPath: proto.String("/v/t/p"),
			ThumbnailSHA256: []byte{1, 2, 3}, ThumbnailEncSHA256: []byte{4, 5, 6}}},
			msgVideo, "", "Video", `{"gif":true,"thumbPath":"/v/t/p","thumbSha":"AQID","thumbEncSha":"BAUG"}`, false},
		{"round video note", &waE2E.Message{PtvMessage: &waE2E.VideoMessage{Seconds: proto.Uint32(3)}}, msgVideo, "", "Video", "", false},
		{"instant photo", &waE2E.Message{InstantImageMessage: &waE2E.ImageMessage{Caption: proto.String("now")}}, msgPhoto, "now", "Photo now", "", false},
		{"spoiler", &waE2E.Message{SpoilerMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("boo")}}}},
			msgPhoto, "boo", "Photo boo", "", false},
		{"sticker", &waE2E.Message{StickerMessage: &waE2E.StickerMessage{}}, msgSticker, "", "Sticker", "", false},
		{"animated sticker", &waE2E.Message{StickerMessage: &waE2E.StickerMessage{IsAnimated: proto.Bool(true)}}, msgSticker, "", "Sticker", `{"animated":true}`, false},
		{"payment stays unknown", &waE2E.Message{SendPaymentMessage: &waE2E.SendPaymentMessage{}}, msgOther, unknownText, "Message", "", false},
	}
	// Every kind again as a forward: they now come wrapped (seen on the Q10 2026-11-15).
	for _, tc := range tests {
		tc.name, tc.m = "forwarded "+tc.name, &waE2E.Message{BotForwardedMessage: &waE2E.FutureProofMessage{Message: tc.m}}
		tests = append(tests, tc)
	}
	for _, tc := range tests {
		e := &events.Message{Message: tc.m, IsViewOnce: tc.once, Info: types.MessageInfo{ID: "id", Timestamp: time.Unix(100, 0)}}
		r, ok := rowFromMessage("c", e, "")
		if !ok {
			t.Errorf("%s: not ok", tc.name)
			continue
		}
		if r.Kind != tc.kind || r.Text != tc.text || chatPreview(r) != tc.prev || !sameJSON(r.Extra, tc.extra) {
			t.Errorf("%s: kind %d text %q preview %q extra %s", tc.name, r.Kind, r.Text, chatPreview(r), r.Extra)
		}
		if tc.once && (r.Media != "" || r.MediaState != 0 || r.Thumb != nil || r.Width != 0 || r.Waveform != nil || r.Duration != 0) {
			t.Errorf("%s: a view-once row kept media: %+v", tc.name, r)
		}
	}
}

func jsonStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// What a row keeps beside its extra: the thumbnails, and the sticker's download details.
func TestKindThumbs(t *testing.T) {
	row := func(m *waE2E.Message) msgRow {
		t.Helper()
		r, ok := rowFromMessage("c", &events.Message{Message: m, Info: types.MessageInfo{ID: "id"}}, "")
		if !ok {
			t.Fatalf("not ok: %v", m)
		}
		return r
	}
	for name, tc := range map[string]struct {
		m    *waE2E.Message
		want []byte
	}{
		"link preview": {&waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("a"), Title: proto.String("T"), JPEGThumbnail: []byte{1, 2}}}, []byte{1, 2}},
		"location":     {&waE2E.Message{LocationMessage: &waE2E.LocationMessage{JPEGThumbnail: []byte{3}}}, []byte{3}},
		"live":         {&waE2E.Message{LiveLocationMessage: &waE2E.LiveLocationMessage{JPEGThumbnail: []byte{4}}}, []byte{4}},
		"invite":       {&waE2E.Message{GroupInviteMessage: &waE2E.GroupInviteMessage{JPEGThumbnail: []byte{5}}}, []byte{5}},
		"sticker":      {&waE2E.Message{StickerMessage: &waE2E.StickerMessage{PngThumbnail: []byte{6}}}, []byte{6}},
		"no preview":   {&waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("a"), JPEGThumbnail: []byte{7}}}, nil},
	} {
		if r := row(tc.m); !reflect.DeepEqual(r.Thumb, tc.want) {
			t.Errorf("%s: thumb %v, want %v", name, r.Thumb, tc.want)
		}
	}

	s := row(&waE2E.Message{StickerMessage: &waE2E.StickerMessage{DirectPath: proto.String("/s"), MediaKey: []byte{1}, FileLength: proto.Uint64(900),
		Mimetype: proto.String("image/webp"), Width: proto.Uint32(512), Height: proto.Uint32(400)}})
	var info mediaInfo
	if err := json.Unmarshal([]byte(s.Media), &info); err != nil || info.DirectPath != "/s" || info.Mimetype != "image/webp" ||
		s.MediaState != 1 || s.Width != 512 || s.Height != 400 || s.Size != 900 {
		t.Errorf("sticker: %+v, %v", s, err)
	}
	if got := info.message(msgSticker).(*waE2E.StickerMessage); got.GetDirectPath() != "/s" || string(got.GetMediaKey()) != "\x01" {
		t.Errorf("a sticker downloads as a StickerMessage: %v", got)
	}
	if got, want := mediaTarget("/d", s, "image/webp"), "/d/media/id.webp"; got != want {
		t.Errorf("sticker path %s, want %s", got, want)
	}
	if got := mediaTarget("/d", msgRow{Kind: msgPhoto, ID: "p"}, ""); got != "/d/media/p.jpg" {
		t.Errorf("photo path %s", got)
	}
}

func TestSilentMessages(t *testing.T) {
	for name, m := range map[string]*waE2E.Message{
		"poll vote":              {PollUpdateMessage: &waE2E.PollUpdateMessage{}},
		"event reply":            {EncEventResponseMessage: &waE2E.EncEventResponseMessage{}},
		"encrypted reaction":     {EncReactionMessage: &waE2E.EncReactionMessage{}},
		"keep in chat":           {KeepInChatMessage: &waE2E.KeepInChatMessage{}},
		"album":                  {AlbumMessage: &waE2E.AlbumMessage{}},
		"history notice":         {MessageHistoryNotice: &waE2E.MessageHistoryNotice{}},
		"history bundle":         {MessageHistoryBundle: &waE2E.MessageHistoryBundle{}},
		"sticker sync":           {StickerSyncRmrMessage: &waE2E.StickerSyncRMRMessage{}},
		"group root key":         {GroupRootKeyShare: &waE2E.GroupRootKeyShare{}},
		"root secret":            {RootSecretDistributeMessage: &waE2E.RootSecretDistributeMessage{}},
		"poll option added":      {PollAddOptionMessage: &waE2E.PollAddOptionMessage{}},
		"secret encrypted":       {SecretEncryptedMessage: &waE2E.SecretEncryptedMessage{}},
		"pin of an unknown type": {PinInChatMessage: &waE2E.PinInChatMessage{}},
		"vote with a context":    {PollUpdateMessage: &waE2E.PollUpdateMessage{}, MessageContextInfo: &waE2E.MessageContextInfo{}},
	} {
		if _, _, ok := content(m); ok {
			t.Errorf("%s: has something to show", name)
		}
		if _, ok := rowFromMessage("c", &events.Message{Message: m, Info: types.MessageInfo{ID: "id"}}, ""); ok {
			t.Errorf("%s: made a row", name)
		}
	}
	// What no list names stays "Message", so the log hears of it.
	if k, tx, ok := content(&waE2E.Message{SendPaymentMessage: &waE2E.SendPaymentMessage{}}); !ok || k != msgOther || tx != unknownText {
		t.Errorf("unknown: %d %q %v", k, tx, ok)
	}
}

func TestVcardValues(t *testing.T) {
	for _, tc := range []struct {
		name, vcard, prop string
		want              []string
	}{
		{"plain", "BEGIN:VCARD\nTEL:+39 333\nEND:VCARD", "TEL", []string{"+39 333"}},
		{"group and parameters", annVCard, "TEL", []string{"+39 333 123 4567", "02 1234"}},
		{"crlf", "FN:Anna\r\nTEL;TYPE=CELL:123\r\n", "TEL", []string{"123"}},
		{"lower case", "tel;type=cell:123", "TEL", []string{"123"}},
		{"colon in the value", "TEL:tel:123", "TEL", []string{"tel:123"}},
		{"not TEL", "X-TEL:1\nTELEX:2\nFN:Anna", "TEL", []string{}},
		{"FN", annVCard, "FN", []string{"Anna Rossi"}},
		{"no vcard", "", "TEL", []string{}},
	} {
		if got := vcardValues(tc.vcard, tc.prop); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestFlattenMore(t *testing.T) {
	ci := &waE2E.ContextInfo{StanzaID: proto.String("q1"), QuotedMessage: &waE2E.Message{Conversation: proto.String("old")}}
	str := proto.String
	prodPhoto := &waE2E.Message{ProductMessage: &waE2E.ProductMessage{
		Product: &waE2E.ProductMessage_ProductSnapshot{Title: str("Mug"), CurrencyCode: str("EUR"),
			ProductImage: &waE2E.ImageMessage{DirectPath: str("/img"), JPEGThumbnail: []byte{1}}}, ContextInfo: ci}}
	orderPic := &waE2E.Message{OrderMessage: &waE2E.OrderMessage{OrderTitle: str("Order #2"), ItemCount: proto.Int32(1),
		Thumbnail: []byte{9}, ContextInfo: ci}}
	tests := []struct {
		name string
		m    *waE2E.Message
		kind int
		text string
	}{
		{"list", &waE2E.Message{ListMessage: &waE2E.ListMessage{Title: str("Menu"), Description: str("Pick one"), FooterText: str("Thanks"),
			Sections:    []*waE2E.ListMessage_Section{{Title: str("S1"), Rows: []*waE2E.ListMessage_Row{{Title: str("Pasta")}, {Title: str("Pizza")}}}, {Rows: []*waE2E.ListMessage_Row{{Title: str("Salad")}}}},
			ContextInfo: ci}}, msgText, "Menu\nPick one\nThanks\n\nPasta\nPizza\nSalad"},
		{"list response", &waE2E.Message{ListResponseMessage: &waE2E.ListResponseMessage{Title: str("Pizza"), Description: str("with cheese"), ContextInfo: ci}},
			msgText, "Pizza\nwith cheese"},
		{"buttons response", &waE2E.Message{ButtonsResponseMessage: &waE2E.ButtonsResponseMessage{Response: &waE2E.ButtonsResponseMessage_SelectedDisplayText{SelectedDisplayText: "Yes"}, ContextInfo: ci}}, msgText, "Yes"},
		{"template reply", &waE2E.Message{TemplateButtonReplyMessage: &waE2E.TemplateButtonReplyMessage{SelectedDisplayText: str("Later"), ContextInfo: ci}}, msgText, "Later"},
		{"interactive response", &waE2E.Message{InteractiveResponseMessage: &waE2E.InteractiveResponseMessage{
			Body: &waE2E.InteractiveResponseMessage_Body{Text: str("Done")}, ContextInfo: ci}}, msgText, "Done"},
		{"product, text", &waE2E.Message{ProductMessage: &waE2E.ProductMessage{
			Product: &waE2E.ProductMessage_ProductSnapshot{Title: str("Mug"), Description: str("Blue"), CurrencyCode: str("EUR"), PriceAmount1000: proto.Int64(12500)},
			Body:    str("Nice"), Footer: str("Shop"), ContextInfo: ci}}, msgText, "Mug\nBlue\n12.50 EUR\nNice\nShop"},
		{"product, photo", prodPhoto, msgPhoto, "Mug"},
		{"product, no price", &waE2E.Message{ProductMessage: &waE2E.ProductMessage{Product: &waE2E.ProductMessage_ProductSnapshot{Title: str("Mug"), CurrencyCode: str("EUR")}, ContextInfo: ci}},
			msgText, "Mug"},
		{"order", &waE2E.Message{OrderMessage: &waE2E.OrderMessage{OrderTitle: str("Order #1"), Message: str("Thanks!"), ItemCount: proto.Int32(3), ContextInfo: ci}},
			msgText, "Order #1\nThanks!\n3 items"},
		{"order, one item, picture", orderPic, msgPhoto, "Order #2\n1 item"},
		{"audio sticker", &waE2E.Message{AudioStickerMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{StickerMessage: &waE2E.StickerMessage{}}}}, msgSticker, ""},
		{"question", &waE2E.Message{QuestionMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{Conversation: str("Ask me")}}}, msgText, "Ask me"},
		{"question reply", &waE2E.Message{QuestionReplyMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{Conversation: str("Fine")}}}, msgText, "Fine"},
		{"poll with an option image", &waE2E.Message{PollCreationOptionImageMessage: &waE2E.FutureProofMessage{
			Message: &waE2E.Message{PollCreationMessageV3: &waE2E.PollCreationMessage{Name: str("Which?")}}}}, msgPoll, "Which?"},
	}
	for _, tc := range tests {
		if k, text, ok := content(tc.m); !ok || k != tc.kind || text != tc.text {
			t.Errorf("%s: %d %q %v", tc.name, k, text, ok)
		}
		// The replies keep their quote (the wrapped ones have none of their own).
		if id, prev := quoteOf(tc.m); id != "" && (id != "q1" || prev != "old") {
			t.Errorf("%s: quote %q %q", tc.name, id, prev)
		}
	}
	for _, m := range []*waE2E.Message{tests[0].m, tests[1].m, tests[2].m, tests[3].m, tests[4].m, prodPhoto, orderPic} {
		if id, _ := quoteOf(m); id != "q1" {
			t.Errorf("%v lost its quote", m)
		}
	}

	// The product's picture keeps its download details, on a clone: the event's message is left alone.
	img := flatten(prodPhoto).GetImageMessage()
	if img.GetDirectPath() != "/img" || img.GetCaption() != "Mug" || prodPhoto.GetProductMessage().GetProduct().GetProductImage().Caption != nil {
		t.Errorf("product picture: %v", img)
	}
	if o := flatten(orderPic).GetImageMessage(); len(o.GetJPEGThumbnail()) != 1 || o.GetDirectPath() != "" {
		t.Errorf("order picture: %v", o)
	}

	// A round video note is a video, an instant photo a photo; both keep their quotes.
	ptv := &waE2E.Message{PtvMessage: &waE2E.VideoMessage{DirectPath: str("/v"), Seconds: proto.Uint32(7), ContextInfo: ci}}
	if v := flatten(ptv).GetVideoMessage(); v.GetDirectPath() != "/v" || v.GetSeconds() != 7 {
		t.Errorf("ptv: %v", v)
	}
	instant := &waE2E.Message{InstantImageMessage: &waE2E.ImageMessage{DirectPath: str("/i"), ContextInfo: ci}}
	if i := flatten(instant).GetImageMessage(); i.GetDirectPath() != "/i" {
		t.Errorf("instant image: %v", i)
	}
	for _, m := range []*waE2E.Message{ptv, instant} {
		if id, prev := quoteOf(m); id != "q1" || prev != "old" {
			t.Errorf("quote of %v: %q %q", m, id, prev)
		}
	}
	// A wrapper with nothing in it, and a list with nothing to show, come back as they were.
	for name, m := range map[string]*waE2E.Message{
		"empty spoiler": {SpoilerMessage: &waE2E.FutureProofMessage{}},
		"empty list":    {ListMessage: &waE2E.ListMessage{}},
		"empty product": {ProductMessage: &waE2E.ProductMessage{}},
		"empty order":   {OrderMessage: &waE2E.OrderMessage{}},
	} {
		if flatten(m) != m {
			t.Errorf("%s: changed", name)
		}
	}
}

func TestPrice(t *testing.T) {
	for _, tc := range []struct {
		amount   int64
		currency string
		want     string
	}{{12500, "EUR", "12.50 EUR"}, {1000, "USD", "1.00 USD"}, {12505, "EUR", "12.51 EUR"}, {990, "", "0.99"}, {0, "EUR", ""}} {
		if got := price(tc.amount, tc.currency); got != tc.want {
			t.Errorf("price(%d, %q) = %q, want %q", tc.amount, tc.currency, got, tc.want)
		}
	}
}

// A reply to, or with, any kind quotes: the ContextInfo is read from whichever field is set.
func TestQuoteOfKinds(t *testing.T) {
	str := proto.String
	reply := func(quoted *waE2E.Message) *waE2E.ContextInfo {
		return &waE2E.ContextInfo{StanzaID: str("q1"), QuotedMessage: quoted}
	}
	loc := &waE2E.Message{LocationMessage: &waE2E.LocationMessage{Name: str("Duomo")}}
	for _, tc := range []struct {
		name, prev string
		m          *waE2E.Message
	}{
		{"location reply", "old", &waE2E.Message{LocationMessage: &waE2E.LocationMessage{Name: str("Duomo"), ContextInfo: reply(&waE2E.Message{Conversation: str("old")})}}},
		{"live location reply", "old", &waE2E.Message{LiveLocationMessage: &waE2E.LiveLocationMessage{ContextInfo: reply(&waE2E.Message{Conversation: str("old")})}}},
		{"contact reply", "old", &waE2E.Message{ContactMessage: &waE2E.ContactMessage{ContextInfo: reply(&waE2E.Message{Conversation: str("old")})}}},
		{"contacts reply", "old", &waE2E.Message{ContactsArrayMessage: &waE2E.ContactsArrayMessage{ContextInfo: reply(&waE2E.Message{Conversation: str("old")})}}},
		{"poll reply", "old", &waE2E.Message{PollCreationMessage: &waE2E.PollCreationMessage{ContextInfo: reply(&waE2E.Message{Conversation: str("old")})}}},
		{"event reply", "old", &waE2E.Message{EventMessage: &waE2E.EventMessage{ContextInfo: reply(&waE2E.Message{Conversation: str("old")})}}},
		{"invite reply", "old", &waE2E.Message{GroupInviteMessage: &waE2E.GroupInviteMessage{ContextInfo: reply(&waE2E.Message{Conversation: str("old")})}}},
		{"video reply", "old", &waE2E.Message{VideoMessage: &waE2E.VideoMessage{ContextInfo: reply(&waE2E.Message{Conversation: str("old")})}}},
		{"sticker reply", "old", &waE2E.Message{StickerMessage: &waE2E.StickerMessage{ContextInfo: reply(&waE2E.Message{Conversation: str("old")})}}},
		{"text reply to a location", "Location Duomo", &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: str("nice"), ContextInfo: reply(loc)}}},
		{"reply to a contact", "Contact: Anna", &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{ContextInfo: reply(
			&waE2E.Message{ContactMessage: &waE2E.ContactMessage{DisplayName: str("Anna")}})}}},
		{"reply to a poll", "Poll: Pizza?", &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{ContextInfo: reply(
			&waE2E.Message{PollCreationMessage: &waE2E.PollCreationMessage{Name: str("Pizza?")}})}}},
		{"reply to a call", "Missed voice call", &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{ContextInfo: reply(
			&waE2E.Message{CallLogMesssage: &waE2E.CallLogMessage{CallOutcome: waE2E.CallLogMessage_MISSED.Enum()}})}}},
	} {
		if id, prev := quoteOf(tc.m); id != "q1" || prev != tc.prev {
			t.Errorf("%s: %q %q, want q1 %q", tc.name, id, prev, tc.prev)
		}
	}
	for name, m := range map[string]*waE2E.Message{
		"nothing": nil, "plain text": {Conversation: str("hi")}, "a location that replies to nothing": loc,
		"a context without a quote": {ExtendedTextMessage: &waE2E.ExtendedTextMessage{ContextInfo: &waE2E.ContextInfo{}}},
	} {
		if id, prev := quoteOf(m); id != "" || prev != "" {
			t.Errorf("%s: %q %q", name, id, prev)
		}
	}
}

// A notice in a chat export reads in English, not as its code.
func TestExportNotice(t *testing.T) {
	got := exportText("a@s.whatsapp.net", "Anna", false, []msgRow{{TS: 0, Kind: msgNotice, Text: "missed_call"}})
	if want := "Anna: Missed voice call\n"; got[len(got)-len(want):] != want {
		t.Errorf("export: %q", got)
	}
}
