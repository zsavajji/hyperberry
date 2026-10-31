package main

import (
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"sort"
	"time"

	"github.com/polymorfa/hypermeow/types"
)

// simMember is a made-up person's key for a group member: the same name gives the same key on
// every run.
func simMember(name string) string {
	h := fnv.New32a()
	h.Write([]byte(name))
	return fmt.Sprintf("39334%07d@s.whatsapp.net", h.Sum32()%10000000)
}

// simPhrases are what the made-up people say: old and new emoji, one paragraph that wraps
// several lines, one text with a newline, and short ones.
var simPhrases = []string{
	"ok", "👍", "Thanks!", "Haha 😀", "On my way", "See you later", "Can you call me?", "Yes",
	"No problem 🫎", "Not sure yet, I'll tell you tonight", "Did you get my message?", "😀😀😀",
	"Look at this 🪿", "Let me check and get back to you", "Good morning!", "Good night 🫨",
	"I'll be there in ten minutes", "What time works for you?", "Perfect, see you then",
	"Sorry, I missed your call", "Can we move it to tomorrow?", "Happy birthday! 🎂",
	"Sure, why not", "Send me the address", "Running late, start without me",
	"Nice one 👍", "I don't think so", "Wait, what?",
	"Two things:\n1. bring the charger\n2. don't forget the keys",
	"So I finally went to see the place we talked about last month, and it is bigger than the " +
		"photos made it look. The garden is lovely, the kitchen needs work, and the neighbours " +
		"seem nice. Let's talk about it properly this weekend, with a coffee.",
}

// simPeople are the group members besides the one who sent the chat's last message.
var simPeople = []string{"Giorgia Conti", "Luca Verdi", "Marco Gallo", "Sofia Greco", "Paolo Fabbri"}

// simStep is one message of the Family script. who "" is us; kind msgService has no sender.
type simStep struct {
	who         string
	kind        int
	text        string
	quote       int // index of an earlier step, or 0 for none (step 0 is never quoted)
	edited, del bool
}

// simFamily is chat 1's hand-written talk: five senders, replies, an edited and a deleted
// message, every media kind and a service message. The last 11 are theirs (unread is 12).
var simFamily = []simStep{
	{who: "Mum", text: "Morning everyone! Who is free on Sunday?"},
	{who: "Dad", text: "I am"},
	{text: "Me too 👍"},
	{who: "Luca", text: "Count me in"},
	{who: "Giorgia", kind: msgPhoto, text: "Look what I found in the attic 😀"},
	{who: "Mum", text: "Great, lunch at one then", quote: 1},
	{text: "Where was that taken?", quote: 4},
	{who: "Giorgia", text: "In grandma's old box", edited: true},
	{kind: msgService, text: "Luca added Marco"},
	{who: "Marco", text: "Hi all, thanks for adding me!"},
	{who: "Mum", text: "Welcome Marco 🫶"},
	{who: "Dad", kind: msgVoice},
	{who: "Luca", kind: msgDoc, text: "recipe.pdf"},
	{text: "Thanks Luca"},
	{who: "Marco", del: true},
	{who: "Giorgia", kind: msgSticker},
	{who: "Luca", text: "Grandma's, of course"},
	{who: "Dad", text: "Who's bringing the wine?"},
	{who: "Marco", text: "I can bring some 🍷"},
	{who: "Mum", text: "Marco, thank you!", quote: 18},
	{who: "Luca", text: "Can I bring the chairs?"},
	{who: "Giorgia", text: "Yes please"},
	{who: "Dad", text: "Parking is tight, come early"},
	{who: "Marco", text: "See you there"},
	{who: "Mum", text: "Don't forget the dessert 🍰"},
}

// simHistory is chat i's messages before its last one (which seed writes), oldest first. A
// generator of its own per chat means adding a chat leaves the others' history as it was; only
// the times depend on now.
func simHistory(i int, now time.Time) []msgRow {
	c := simChats[i]
	j, _ := types.ParseJID(c.jid)
	group := chatKind(j) == chatGroup
	end := now.Add(-c.ago).Unix() - 60 // strictly before the last message
	rng := rand.New(rand.NewPCG(uint64(i), 1))
	id := func(k int) string { return fmt.Sprintf("SIMH%d.%d", i, k) }
	var rows []msgRow
	quote := func(r *msgRow, k int) {
		q := rows[k]
		r.QuoteID, r.QuotePreview = q.ID, clip(preview(q.Kind, q.Text), 80)
	}
	if i == 1 {
		for k, s := range simFamily {
			r := msgRow{Chat: c.jid, ID: id(k), TS: end - int64(len(simFamily)-1-k)*9000, State: stateRead,
				Kind: s.kind, Text: s.text, Edited: s.edited, Deleted: s.del}
			switch s.who {
			case "":
				r.Out = s.kind != msgService
			default:
				r.Sender, r.SenderName = simMember(s.who), s.who
			}
			if s.quote > 0 {
				quote(&r, s.quote)
			}
			rows = append(rows, r)
		}
		return rows
	}

	n, span := 16+rng.IntN(9), 3*day
	if i == 0 {
		n, span = 599, 90*day
	}
	n = max(n, c.unread-1) // a count of unread needs that many of theirs
	offs := make([]int, n)
	for k := range offs {
		offs[k] = rng.IntN(int(span / time.Second))
	}
	sort.Ints(offs)
	people := append([]string{c.sender}, simPeople...)
	if c.sender == "" {
		people = simPeople
	}
	for k := 0; k < n; k++ {
		r := msgRow{Chat: c.jid, ID: id(k), TS: end - int64(offs[n-1-k]), State: stateRead, Kind: msgText,
			Text: simPhrases[rng.IntN(len(simPhrases))]}
		r.Out = rng.IntN(2) == 0 && k < n-(c.unread-1)
		if !r.Out && group {
			r.SenderName = people[rng.IntN(len(people))]
			r.Sender = simMember(r.SenderName)
		}
		if k > 0 && rng.IntN(10) == 0 {
			quote(&r, rng.IntN(k))
		}
		rows = append(rows, r)
	}
	return rows
}
