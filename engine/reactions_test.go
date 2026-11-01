package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/proto/waCommon"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/proto/waHistorySync"
	"github.com/polymorfa/hypermeow/proto/waWeb"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"
)

// reactionOf reads one reaction: "" when there is none.
func reactionOf(t *testing.T, tx *storeTx, chat, msg, sender string) (emoji string, ts int64) {
	t.Helper()
	tx.tx.QueryRow("SELECT emoji, ts FROM reactions WHERE chat = ? AND msg = ? AND sender = ?", chat, msg, sender).Scan(&emoji, &ts)
	return
}

// took reports whether the message was named as changed, and forgets it.
func took(tx *storeTx, chat, msg string) bool {
	b := tx.msgs[chat][msg]
	delete(tx.msgs, chat)
	return b
}

func previewOf(tx *storeTx, chat string) (p string) {
	tx.tx.QueryRow("SELECT COALESCE(last_preview, '') FROM chats WHERE jid = ?", chat).Scan(&p)
	return
}

func TestSetReaction(t *testing.T) {
	tx, _ := newTestTx(t)
	g := "g@g.us"
	check := func(what, wantEmoji string, wantTS int64, wantChanged bool) {
		t.Helper()
		if e, ts := reactionOf(t, tx, g, "m1", "bob"); e != wantEmoji || ts != wantTS {
			t.Errorf("%s: %q at %d, want %q at %d", what, e, ts, wantEmoji, wantTS)
		}
		if got := took(tx, g, "m1"); got != wantChanged {
			t.Errorf("%s: named as changed %v, want %v", what, got, wantChanged)
		}
	}
	tx.setReaction(g, "m1", "bob", "Bob", "👍", 10)
	check("first", "👍", 10, true)
	tx.setReaction(g, "m1", "bob", "Bob", "👍", 10)
	check("the same again", "👍", 10, false)
	tx.setReaction(g, "m1", "bob", "Bob", "❤️", 20)
	check("newer", "❤️", 20, true)
	tx.setReaction(g, "m1", "bob", "Bob", "😂", 15) // out of order
	check("older", "❤️", 20, false)
	tx.setReaction(g, "m1", "bob", "", "😂", 20) // a tie goes to the later arrival
	check("same time", "😂", 20, true)
	var name string
	tx.tx.QueryRow("SELECT sender_name FROM reactions WHERE sender = 'bob'").Scan(&name)
	if name != "Bob" {
		t.Errorf("an update with no name lost it: %q", name)
	}

	tx.setReaction(g, "m1", "bob", "", "", 5) // a removal older than the reaction
	check("older removal", "😂", 20, false)
	tx.setReaction(g, "m1", "bob", "", "", 20)
	check("removal", "", 0, true)
	tx.setReaction(g, "m1", "bob", "", "", 30)
	check("removal of nothing", "", 0, false)

	// Ours has sender "" and no name; each reactor has a row of their own.
	tx.setReaction(g, "m1", "", "", "🙏", 40)
	tx.setReaction(g, "m1", "bob", "Bob", "👍", 41)
	tx.setReaction(g, "m2", "bob", "Bob", "👍", 42)
	if n := count(t, tx, "SELECT count(*) FROM reactions WHERE msg = 'm1'"); n != 2 {
		t.Errorf("%d reactions on m1, want 2", n)
	}
	if n := count(t, tx, "SELECT count(*) FROM reactions WHERE sender = '' AND sender_name IS NULL AND emoji = '🙏'"); n != 1 {
		t.Error("ours: want sender '' and no name")
	}
}

// reactMsg is a reaction message as the event handler gets it.
func reactMsg(chat, sender, alt types.JID, fromMe bool, target, emoji string, ms int64) *events.Message {
	return &events.Message{
		Info: types.MessageInfo{
			ID: "R" + target + emoji,
			MessageSource: types.MessageSource{Chat: chat, Sender: sender, SenderAlt: alt, IsFromMe: fromMe,
				IsGroup: chat.Server == types.GroupServer},
			PushName:  "Bob Lee",
			Timestamp: time.Unix(1700000100, 0),
		},
		Message: &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{
			Key: &waCommon.MessageKey{ID: proto.String(target)}, Text: proto.String(emoji), SenderTimestampMS: proto.Int64(ms)}},
	}
}

func TestLiveReactionGroup(t *testing.T) {
	st, cli := historyFixture(t)
	tx, _ := newTestTx(t)
	ctx := context.Background()
	group := types.NewJID("120363000000000001", types.GroupServer)
	bob := types.NewJID("4915700000002", types.DefaultUserServer)
	me := *cli.Store.ID

	st.message(ctx, tx, cli, reactMsg(group, bob, types.EmptyJID, false, "M1", "👍", 1700000090000))
	if e, ts := reactionOf(t, tx, group.String(), "M1", bob.String()); e != "👍" || ts != 1700000090 {
		t.Errorf("theirs: %q at %d", e, ts)
	}
	var name string
	tx.tx.QueryRow("SELECT sender_name FROM reactions").Scan(&name)
	if name != "Bob Lee" || !took(tx, group.String(), "M1") {
		t.Errorf("name %q, or the message wasn't named as changed", name)
	}
	if p := previewOf(tx, group.String()); p != "Bob reacted 👍" {
		t.Errorf("preview %q", p)
	}

	// Ours, from another device: sender "" and our own preview. No millisecond stamp: the
	// message's time stands in for it.
	st.message(ctx, tx, cli, reactMsg(group, me, types.EmptyJID, true, "M1", "❤️", 0))
	if e, ts := reactionOf(t, tx, group.String(), "M1", ""); e != "❤️" || ts != 1700000100 {
		t.Errorf("ours: %q at %d", e, ts)
	}
	var out, state int
	tx.tx.QueryRow("SELECT last_out, last_state FROM chats").Scan(&out, &state)
	if p := previewOf(tx, group.String()); p != "You reacted ❤️" || out != 1 || state != stateSent {
		t.Errorf("preview %q, out %d, state %d", p, out, state)
	}

	// Removals (empty text) reach the table and leave the preview alone.
	st.message(ctx, tx, cli, reactMsg(group, bob, types.EmptyJID, false, "M1", "", 1700000095000))
	if e, _ := reactionOf(t, tx, group.String(), "M1", bob.String()); e != "" {
		t.Errorf("theirs after the removal: %q", e)
	}
	if p := previewOf(tx, group.String()); p != "You reacted ❤️" {
		t.Errorf("a removal changed the preview: %q", p)
	}
	st.message(ctx, tx, cli, reactMsg(group, me, types.EmptyJID, true, "M1", "", 1700000101000))
	if n := count(t, tx, "SELECT count(*) FROM reactions"); n != 0 {
		t.Errorf("%d reactions left", n)
	}
}

// In a 1:1 chat the reactor's key is the chat's own, whichever way the message addressed them:
// by phone number with the LID beside it, as history does it (the number alone), or with nothing
// known about a LID.
func TestLiveReactionOneToOne(t *testing.T) {
	st, cli := historyFixture(t)
	tx, _ := newTestTx(t)
	ctx := context.Background()
	lids := cli.Store.LIDs
	pn := types.NewJID("4915700000001", types.DefaultUserServer)
	lid := types.NewJID("99887766", types.HiddenUserServer)
	if err := lids.PutLIDMapping(ctx, lid, pn); err != nil {
		t.Fatal(err)
	}

	sender := func(chat, msg string) string {
		t.Helper()
		var s string
		if err := tx.tx.QueryRow("SELECT sender FROM reactions WHERE chat = ? AND msg = ?", chat, msg).Scan(&s); err != nil {
			t.Fatalf("no reaction on %s in %s: %v", msg, chat, err)
		}
		return s
	}
	// The number with the LID beside it.
	e := reactMsg(pn, pn, lid, false, "M1", "👍", 1700000090000)
	st.message(ctx, tx, cli, e)
	if got, want := sender(lid.String(), "M1"), chatKey(ctx, tx, lids, e.Info.Sender, e.Info.SenderAlt); got != lid.String() || got != want {
		t.Errorf("reactor %s; the chat is %s, chatKey(Sender, SenderAlt) is %s", got, lid, want)
	}
	// The number alone, the LID known from before.
	st.message(ctx, tx, cli, reactMsg(pn, pn, types.EmptyJID, false, "M2", "👍", 1700000090000))
	if got := sender(lid.String(), "M2"); got != lid.String() {
		t.Errorf("number alone: reactor %s, want %s", got, lid)
	}
	// Nobody known by a LID: the number is the chat.
	other := types.NewJID("4915700000003", types.DefaultUserServer)
	st.message(ctx, tx, cli, reactMsg(other, other, types.EmptyJID, false, "M3", "😂", 1700000090000))
	if got := sender(other.String(), "M3"); got != other.String() {
		t.Errorf("no LID: reactor %s, want %s", got, other)
	}
	// Ours from another device, to the number whose LID we know: the same chat, sender "".
	st.message(ctx, tx, cli, reactMsg(pn, *cli.Store.ID, types.EmptyJID, true, "M1", "🙏", 1700000091000))
	if e, _ := reactionOf(t, tx, lid.String(), "M1", ""); e != "🙏" {
		t.Errorf("ours: %q", e)
	}
	if n := count(t, tx, "SELECT count(*) FROM chats"); n != 2 {
		t.Errorf("%d chats, want 2", n)
	}
}

func webReaction(fromMe bool, participant, emoji string, ms int64) *waWeb.Reaction {
	k := &waCommon.MessageKey{FromMe: proto.Bool(fromMe)}
	if participant != "" {
		k.Participant = proto.String(participant)
	}
	return &waWeb.Reaction{Key: k, Text: proto.String(emoji), SenderTimestampMS: proto.Int64(ms)}
}

func webMsg(chat, id, from, text string, fromMe bool, ts uint64, rs ...*waWeb.Reaction) *waHistorySync.HistorySyncMsg {
	m := &waWeb.WebMessageInfo{
		Key:              &waCommon.MessageKey{RemoteJID: proto.String(chat), ID: proto.String(id), FromMe: proto.Bool(fromMe)},
		Message:          &waE2E.Message{Conversation: proto.String(text)},
		MessageTimestamp: proto.Uint64(ts),
		Reactions:        rs,
	}
	if from != "" {
		m.Participant = proto.String(from)
	}
	return &waHistorySync.HistorySyncMsg{Message: m}
}

func TestHistoryReactions(t *testing.T) {
	st, cli := historyFixture(t)
	tx, _ := newTestTx(t)
	ctx := context.Background()
	group := types.NewJID("120363000000000001", types.GroupServer).String()
	bob, carol := "4915700000002@s.whatsapp.net", "4915700000004@s.whatsapp.net"
	pn := types.NewJID("4915700000001", types.DefaultUserServer)
	lid := types.NewJID("99887766", types.HiddenUserServer)

	hs := &waHistorySync.HistorySync{Conversations: []*waHistorySync.Conversation{
		{ID: proto.String(group), Name: proto.String("Family"), Messages: []*waHistorySync.HistorySyncMsg{
			webMsg(group, "G1", bob, "hello", false, 1700000000,
				webReaction(false, carol, "😂", 1700000050000),
				webReaction(true, "", "👍", 1700000060000),
				webReaction(false, "", "?", 1700000070000)), // a group reaction with no reactor: skipped
		}},
		{ID: proto.String(pn.String()), LidJID: proto.String(lid.String()), Messages: []*waHistorySync.HistorySyncMsg{
			webMsg(pn.String(), "P1", "", "mine", true, 1700000000,
				webReaction(false, "", "❤️", 1700000050000)),
			webMsg(pn.String(), "P2", "", "theirs", false, 1700000010,
				webReaction(true, "", "🙏", 1700000050000)),
		}},
	}}
	st.history(ctx, tx, cli, &events.HistorySync{Data: hs})

	for _, c := range []struct {
		chat, msg, sender, emoji, name string
		ts                             int64
	}{
		{group, "G1", carol, "😂", "+4915700000004", 1700000050},
		{group, "G1", "", "👍", "", 1700000060},
		{lid.String(), "P1", lid.String(), "❤️", "+4915700000001", 1700000050}, // the chat's own key, not the number
		{lid.String(), "P2", "", "🙏", "", 1700000050},
	} {
		var emoji, name string
		var ts int64
		err := tx.tx.QueryRow("SELECT emoji, COALESCE(sender_name, ''), ts FROM reactions WHERE chat = ? AND msg = ? AND sender = ?",
			c.chat, c.msg, c.sender).Scan(&emoji, &name, &ts)
		if err != nil || emoji != c.emoji || name != c.name || ts != c.ts {
			t.Errorf("%+v: got %q %q at %d: %v", c, emoji, name, ts, err)
		}
	}
	if n := count(t, tx, "SELECT count(*) FROM reactions"); n != 4 {
		t.Errorf("%d reactions, want 4", n)
	}
	// The chat's preview is its newest message's; reactions don't touch it.
	if p := previewOf(tx, group); !strings.HasSuffix(p, ": hello") {
		t.Errorf("group preview %q", p)
	}
	if p := previewOf(tx, lid.String()); p != "theirs" {
		t.Errorf("1:1 preview %q", p)
	}
	// Syncing the same history again changes nothing: no reaction row, no re-read.
	tx.msgs = map[string]map[string]bool{}
	st.history(ctx, tx, cli, &events.HistorySync{Data: hs})
	if n := count(t, tx, "SELECT count(*) FROM reactions"); n != 4 || len(tx.msgs) != 0 {
		t.Errorf("a second sync: %d reactions, changed %v", n, tx.msgs)
	}
}

func TestMergeChatMovesReactions(t *testing.T) {
	tx, _ := newTestTx(t)
	pn, lid := "4915700000001@s.whatsapp.net", "99887766@lid"
	tx.ensureChat(pn, chatUser)
	tx.ensureChat(lid, chatUser)
	for _, m := range []msgRow{{Chat: pn, ID: "m1", TS: 1}, {Chat: pn, ID: "m2", TS: 2}, {Chat: lid, ID: "m1", TS: 1}} {
		m.Kind = msgText
		tx.insertMessage(m)
	}
	tx.setReaction(pn, "m1", pn, "Bob", "👍", 10) // theirs, keyed by the number
	tx.setReaction(pn, "m1", "", "", "🙏", 10)    // ours
	tx.setReaction(pn, "m2", pn, "Bob", "😂", 10)
	tx.setReaction(lid, "m1", lid, "Bob", "❤️", 20) // the LID chat has theirs on m1 already
	mergeChat(tx, pn, lid)
	if n := count(t, tx, "SELECT count(*) FROM reactions WHERE chat = ?", pn); n != 0 {
		t.Errorf("%d reactions left under the number", n)
	}
	if n := count(t, tx, "SELECT count(*) FROM reactions WHERE sender = ?", pn); n != 0 {
		t.Errorf("%d reactions still by the number", n)
	}
	for _, c := range []struct{ msg, sender, emoji string }{
		{"m1", lid, "❤️"}, {"m1", "", "🙏"}, {"m2", lid, "😂"},
	} {
		if e, _ := reactionOf(t, tx, lid, c.msg, c.sender); e != c.emoji {
			t.Errorf("%s by %q: %q, want %q", c.msg, c.sender, e, c.emoji)
		}
	}
	if n := count(t, tx, "SELECT count(*) FROM reactions"); n != 3 {
		t.Errorf("%d reactions, want 3", n)
	}
}

func TestReactionsGoWithTheirMessage(t *testing.T) {
	tx, _ := newTestTx(t)
	c, d := "g@g.us", "h@g.us"
	tx.ensureChat(c, chatGroup)
	tx.ensureChat(d, chatGroup)
	fill := func() {
		for _, chat := range []string{c, d} {
			for _, id := range []string{"1", "2", "3"} {
				tx.insertMessage(msgRow{Chat: chat, ID: id, TS: 5, Kind: msgText, Text: "x"})
				tx.setReaction(chat, id, "bob", "Bob", "👍", 10)
				tx.setReaction(chat, id, "", "", "🙏", 10)
			}
		}
		tx.msgs = map[string]map[string]bool{}
	}
	left := func(chat string) int { return count(t, tx, "SELECT count(*) FROM reactions WHERE chat = ?", chat) }

	fill()
	tx.deleteMessage(c, "1") // delete for me
	if left(c) != 4 || left(d) != 6 || count(t, tx, "SELECT count(*) FROM reactions WHERE chat = ? AND msg = '1'", c) != 0 {
		t.Errorf("after delete for me: %d and %d reactions", left(c), left(d))
	}
	tx.revokeMessage(c, "2") // delete for everyone: the row stays, its reactions go
	if left(c) != 2 || left(d) != 6 {
		t.Errorf("after revoke: %d and %d reactions", left(c), left(d))
	}
	if !tx.msgs[c]["2"] {
		t.Error("the revoked message wasn't named as changed")
	}
	tx.clearChat(c, false)
	if left(c) != 0 || left(d) != 6 {
		t.Errorf("after clear: %d and %d reactions", left(c), left(d))
	}
	tx.clearChat(d, true)
	if left(d) != 0 {
		t.Errorf("after delete chat: %d reactions", left(d))
	}
}

func TestClearTakesReactions(t *testing.T) {
	st, _ := historyFixture(t)
	st.runOp(func(tx *storeTx) {
		tx.ensureChat("g@g.us", chatGroup)
		tx.insertMessage(msgRow{Chat: "g@g.us", ID: "1", TS: 5, Kind: msgText, Text: "x"})
		tx.setReaction("g@g.us", "1", "bob", "Bob", "👍", 10)
	})
	eventually(t, st, 1, "SELECT count(*) FROM reactions")
	st.clear()
	eventually(t, st, 0, "SELECT count(*) FROM reactions")
}

func TestReactRequest(t *testing.T) {
	s := newUnlinked(t)
	call := func(body string) error {
		_, err := s.handleReact(json.RawMessage(body))
		return err
	}
	for body, want := range map[string]string{
		`{"chat":"a@s.whatsapp.net"}`: "bad_request",
		`{"id":"X","emoji":"👍"}`:      "bad_request",
		`{}`:                          "bad_request",
		`{"chat":"a@s.whatsapp.net","id":"X","emoji":"👍"}`: "unlinked",
		`{"chat":"a@s.whatsapp.net","id":"X"}`:             "unlinked",
	} {
		if c := code(call(body)); c != want {
			t.Errorf("%s: %s, want %s", body, c, want)
		}
	}

	st, cli := historyFixture(t)
	s = &Session{cli: cli, store: st}
	if c := code(call(`{"chat":"not a jid@@@","id":"X"}`)); c != "bad_chat" {
		t.Errorf("bad chat: %s", c)
	}
	if c := code(call(`{"chat":"a@s.whatsapp.net","id":"nope","emoji":"👍"}`)); c != "not_found" {
		t.Errorf("no such message: %s", c)
	}
	g := "120363000000000001@g.us"
	st.runOp(func(tx *storeTx) {
		tx.ensureChat(g, chatGroup)
		tx.insertMessage(msgRow{Chat: g, ID: "nosender", TS: 5, Kind: msgText, Text: "x"}) // theirs, in a group, no sender
		tx.insertMessage(msgRow{Chat: g, ID: "mine", TS: 6, Out: true, State: stateSent, Kind: msgText, Text: "x"})
	})
	if c := code(call(`{"chat":"` + g + `","id":"nosender","emoji":"👍"}`)); c != "not_found" {
		t.Errorf("a group message of unknown sender: %s", c)
	}
	// The client isn't connected: the send fails, and nothing is written as if it had gone.
	if c := call(`{"chat":"` + g + `","id":"mine","emoji":"👍"}`); c == nil {
		t.Error("a send with no connection: want an error")
	}
	time.Sleep(2 * batchTime)
	if n := rows(st, "SELECT count(*) FROM reactions"); n != 0 {
		t.Errorf("%d reactions after a failed send", n)
	}
}

// The key a reaction carries names the message's sender the way WhatsApp wants it.
func TestReactSender(t *testing.T) {
	_, cli := historyFixture(t)
	pn := types.NewJID("4915700000001", types.DefaultUserServer)
	group := types.NewJID("120363000000000001", types.GroupServer)
	bob := "4915700000002@s.whatsapp.net"
	for _, c := range []struct {
		what    string
		row     msgRow
		chat    types.JID
		fromMe  bool
		partici string
		fails   bool
	}{
		{"ours, 1:1", msgRow{Out: true}, pn, true, "", false},
		{"theirs, 1:1", msgRow{}, pn, false, "", false},
		{"ours, group", msgRow{Out: true}, group, true, "", false},
		{"theirs, group", msgRow{Sender: bob}, group, false, bob, false},
		{"theirs, group, no sender", msgRow{}, group, false, "", true},
	} {
		sender, err := reactSender(c.row, c.chat)
		if (err != nil) != c.fails {
			t.Errorf("%s: error %v", c.what, err)
			continue
		}
		if err != nil {
			continue
		}
		k := cli.BuildReaction(c.chat, sender, "X", "👍").GetReactionMessage().GetKey()
		if k.GetFromMe() != c.fromMe || k.GetParticipant() != c.partici || k.GetRemoteJID() != c.chat.String() || k.GetID() != "X" {
			t.Errorf("%s: key %v", c.what, k)
		}
	}
}
