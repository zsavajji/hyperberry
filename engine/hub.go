package main

import (
	"encoding/hex"
	"regexp"

	waLog "github.com/polymorfa/hypermeow/util/log"
)

// The BlackBerry Hub account (step 5.7). The item MIME and the actions are written out again in
// hub_qnx.go's C and in bar-descriptor.xml: keep them in step.
const (
	hubAccount    = 1212305497 // "HBRY"
	hubMarkRead   = "com.zsavajji.hyperberry.MARKREAD"
	hubMarkUnread = "com.zsavajji.hyperberry.MARKUNREAD"
	hubDelete     = "com.zsavajji.hyperberry.DELETE"
)

// invocation is what the navigator delivers to the engine: a Hub item's action.
type invocation struct {
	action, mime, uri string
	data              []byte
}

// hubItem is one chat's item in the Hub's inbox: ts in milliseconds, alert for sound, LED and
// the instant preview.
type hubItem struct {
	id, title, body string
	unread          int
	ts              int64
	alert           bool
}

// hubItemID is the UDS source id of a chat's item: the chat key in hex, so the id carries
// nothing the Hub or a shell could trip on.
func hubItemID(chat string) string { return "chat_" + hex.EncodeToString([]byte(chat)) }

var hubChatRE = regexp.MustCompile(`chat_([0-9a-f]+)`)

// hubChat finds a chat's item id in s, wherever the Hub put it (the URI or the data of an
// invocation), and gives back the chat key; "" when there is none.
func hubChat(s string) string {
	m := hubChatRE.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	b, err := hex.DecodeString(m[1])
	if err != nil {
		return ""
	}
	return string(b)
}

// hubActions turns the Hub items' Mark as read and Mark as unread into chat.set, as if the UI
// had asked, and its Delete into remove: the item goes, the chat stays. The invocation is
// logged whole: where the item id rides is for the Q10 to show.
func hubActions(srv *Server, in <-chan invocation, remove func(chat string), log waLog.Logger) {
	for inv := range in {
		if inv.action != hubMarkRead && inv.action != hubMarkUnread && inv.action != hubDelete {
			continue // START and the like need nothing
		}
		log.Infof("%s mime %q uri %q data %q", inv.action, inv.mime, inv.uri, inv.data)
		chat := hubChat(inv.uri + " " + string(inv.data))
		if chat == "" {
			log.Warnf("%s: no chat in the invocation", inv.action)
			continue
		}
		if inv.action == hubDelete {
			remove(chat)
			continue
		}
		req := map[string]any{"chat": chat, "unread": inv.action == hubMarkUnread}
		if _, err := srv.Call("chat.set", req); err != nil {
			log.Warnf("%s %s: %v", inv.action, chat, err)
		}
	}
}
