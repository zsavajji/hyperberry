package main

import (
	"fmt"
	"time"
)

// simChat is one chat of the simulated account, with its last message.
type simChat struct {
	jid, name string // name "" when unknown
	sender    string // groups: who sent the last message
	text      string // "" on a text message: deleted
	kind      int    // the last message's
	out       bool
	state     int // ours only
	ago       time.Duration
	unread    int
	muted     int // hours from now; -1 for always
	pinned    bool
	archived  bool
}

const (
	hour = time.Hour
	day  = 24 * time.Hour
)

// simChats cover what the chat list draws: names long, short, with emoji and unknown; groups;
// every tick state and a failed send; media previews; pinned, muted and archived chats; unread
// counts; and days from today back to last year. Every person, group and number is made up.
var simChats = append([]simChat{
	{jid: "393330000001@s.whatsapp.net", name: "Anna Rossi", text: "See you at 8 then! 🍕", out: true, state: stateRead, ago: 3 * time.Minute},
	{jid: "120363000000000001@g.us", name: "Family 🏠", sender: "Mum", text: "Who's bringing dessert on Sunday?", ago: 9 * time.Minute, unread: 12, pinned: true},
	{jid: "393330000002@s.whatsapp.net", name: "Marco", text: "Did you see the match last night? Unbelievable ending", ago: 25 * time.Minute, unread: 3},
	{jid: "393330000003@s.whatsapp.net", name: "Giulia Bianchi-Ferraris della Valle Lunga", text: "A name long enough to be cut off", ago: 40 * time.Minute},
	{jid: "120363000000000002@g.us", name: "🎸 Band practice", sender: "Luca Verdi", kind: msgVoice, ago: hour, unread: 1},
	{jid: "393330000004@s.whatsapp.net", name: "Sara", text: "Sending…", out: true, state: stateSending, ago: 2 * hour},
	{jid: "393330000005@s.whatsapp.net", name: "Paolo", text: "This one didn't go", out: true, state: stateFailed, ago: 3 * hour},
	{jid: "393330000006@s.whatsapp.net", name: "Elena", text: "Delivered, not read yet", out: true, state: stateDelivered, ago: 4 * hour},
	{jid: "393330000007@s.whatsapp.net", name: "", text: "Hi, it's the plumber", ago: 5 * hour, unread: 1},
	{jid: "123456789012345@lid", name: "", text: "Someone whose number we don't know", ago: 6 * hour},
	{jid: "120363000000000003@g.us", name: "", sender: "Tom", text: "A group without a subject yet", ago: 7 * hour},
	{jid: "120363000000000004@g.us", name: "Condominium", sender: "Admin", text: "Water off tomorrow 9-12", ago: 20 * hour, unread: 41, muted: -1},
	{jid: "393330000008@s.whatsapp.net", name: "Davide", kind: msgPhoto, text: "The view from up here", ago: day + 2*hour},
	{jid: "393330000009@s.whatsapp.net", name: "Chiara", text: "", ago: day + 5*hour},
	{jid: "393330000010@s.whatsapp.net", name: "Office", text: "Sent, only one tick", out: true, state: stateSent, ago: 2*day + 1*hour, muted: 8},
	{jid: "120363000000000005@g.us", name: "Weekend trip", sender: "Francesca Neri", kind: msgDoc, text: "itinerary.pdf", ago: 3 * day},
	{jid: "393330000011@s.whatsapp.net", name: "Roberto", kind: msgSticker, ago: 5 * day},
	{jid: "393330000012@s.whatsapp.net", name: "Old friend", text: "Pinned, from a while ago", ago: 40 * day, pinned: true},
	{jid: "393330000013@s.whatsapp.net", name: "Archived chat", text: "This one never shows in the list", ago: 2 * day, archived: true},
	{jid: "393330000014@s.whatsapp.net", name: "Emoji 🫨🪿🫎", text: "New emoji: 🫫 🫪 🫩 🫨 🪿 and old ones: 😀 👍", ago: 10 * day},
	{jid: "393330000015@s.whatsapp.net", name: "Last year", text: "Happy new year!", out: true, state: stateRead, ago: 300 * day},
}, simFiller()...)

// simFiller adds plain chats spread over the last months, so the list scrolls.
func simFiller() []simChat {
	var c []simChat
	for i := 0; i < 20; i++ {
		c = append(c, simChat{
			jid:  fmt.Sprintf("39333100%04d@s.whatsapp.net", i),
			name: fmt.Sprintf("Contact %d", i+1),
			text: fmt.Sprintf("Message number %d", i+1),
			out:  i%3 == 0, state: stateRead,
			ago: time.Duration(12+i*6) * day,
		})
	}
	return c
}
