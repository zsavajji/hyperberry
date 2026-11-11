package main

import (
	"encoding/hex"
	"strings"
)

// callItemID is the Hub id of the item for a ringing call from chat: apart from the chat's own
// item, which a missed call later updates through its row. hubChat finds the chat in both.
func callItemID(chat string) string { return "call_" + hex.EncodeToString([]byte(chat)) }

// callRing puts the "Incoming voice call" item (a group's: "Incoming group voice call") in the
// Hub, alerting. Like the chat items it lives on the notifier's thread; unlike them it isn't kept
// in hub.json: it lasts as long as the ringing. ponytail: an engine that dies while ringing leaves the item behind; it goes with the
// account on the next unlink.
func (n *Notifier) callRing(chat, title string, ts int64, group bool) {
	n.queue(func() {
		n.ensureAccount()
		if !n.acct {
			return
		}
		if title == "" {
			title, _, _ = strings.Cut(chat, "@")
		}
		if title == "" {
			title = "Unknown"
		}
		text := word("Incoming voice call")
		if group {
			text = word("Incoming group voice call")
		}
		it := hubItem{callItemID(chat), title, text, 1, ts * 1000, true}
		// The Hub may still hold one from a call that ended while the Hub was away: update then.
		err := n.item(it, true)
		if err != nil {
			err = n.item(it, false)
		}
		if err != nil {
			n.log.Warnf("hub: call item %s: %v", chat, err)
			return
		}
		n.log.Infof("hub: ringing %s", chat)
	}, "the call item")
}

// callEnd takes the ringing call's item out.
func (n *Notifier) callEnd(chat string) {
	n.queue(func() {
		if !n.opened() {
			return
		}
		if err := n.remove(callItemID(chat)); err != nil {
			n.log.Debugf("hub: remove call item %s: %v", chat, err) // normally it was never added
		}
	}, "the call item's removal")
}
