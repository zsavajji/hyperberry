package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waCommon"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/proto/waHistorySync"
	"github.com/polymorfa/hypermeow/proto/waWeb"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"
)

var pollNames = []string{"Yes", "No", "Maybe"}

// pollMessage is a poll with the options above.
func pollMessage(q string) *waE2E.Message {
	var opts []*waE2E.PollCreationMessage_Option
	for _, n := range pollNames {
		opts = append(opts, &waE2E.PollCreationMessage_Option{OptionName: proto.String(n)})
	}
	return &waE2E.Message{PollCreationMessage: &waE2E.PollCreationMessage{Name: proto.String(q), Options: opts}}
}

// voteOf reads one vote: "" when there is none.
func voteOf(t *testing.T, tx *storeTx, chat, msg, sender string) (options string, ts int64) {
	t.Helper()
	tx.tx.QueryRow("SELECT options, ts FROM poll_votes WHERE chat = ? AND msg = ? AND sender = ?", chat, msg, sender).Scan(&options, &ts)
	return
}

func TestSetVote(t *testing.T) {
	tx, _ := newTestTx(t)
	g := "g@g.us"
	check := func(what, wantOptions string, wantTS int64, wantChanged bool) {
		t.Helper()
		if o, ts := voteOf(t, tx, g, "p1", "bob"); o != wantOptions || ts != wantTS {
			t.Errorf("%s: %q at %d, want %q at %d", what, o, ts, wantOptions, wantTS)
		}
		if got := took(tx, g, "p1"); got != wantChanged {
			t.Errorf("%s: named as changed %v, want %v", what, got, wantChanged)
		}
	}
	tx.setVote(g, "p1", "bob", "Bob", "0,2", 10)
	check("first", "0,2", 10, true)
	tx.setVote(g, "p1", "bob", "Bob", "0,2", 10)
	check("the same again", "0,2", 10, false)
	tx.setVote(g, "p1", "bob", "Bob", "1", 20)
	check("newer", "1", 20, true)
	tx.setVote(g, "p1", "bob", "Bob", "0", 15) // out of order
	check("older", "1", 20, false)
	tx.setVote(g, "p1", "bob", "", "2", 20) // a tie goes to the later arrival
	check("same time", "2", 20, true)
	var name string
	tx.tx.QueryRow("SELECT sender_name FROM poll_votes WHERE sender = 'bob'").Scan(&name)
	if name != "Bob" {
		t.Errorf("an update with no name lost it: %q", name)
	}

	tx.setVote(g, "p1", "bob", "", "", 5) // taking the vote back, older than the vote
	check("older removal", "2", 20, false)
	tx.setVote(g, "p1", "bob", "", "", 20)
	check("removal", "", 0, true)
	tx.setVote(g, "p1", "bob", "", "", 30)
	check("removal of nothing", "", 0, false)

	// Ours has sender "" and no name; each voter and each poll has a row of its own.
	tx.setVote(g, "p1", "", "", "0", 40)
	tx.setVote(g, "p1", "bob", "Bob", "1", 41)
	tx.setVote(g, "p2", "bob", "Bob", "1", 42)
	if n := count(t, tx, "SELECT count(*) FROM poll_votes WHERE msg = 'p1'"); n != 2 {
		t.Errorf("%d votes on p1, want 2", n)
	}
	if n := count(t, tx, "SELECT count(*) FROM poll_votes WHERE sender = '' AND sender_name IS NULL AND options = '0'"); n != 1 {
		t.Error("ours: want sender '' and no name")
	}
}

func TestVoteIndices(t *testing.T) {
	h := func(names ...string) [][]byte { return whatsmeow.HashPollOptions(names) }
	for _, tc := range []struct {
		name   string
		hashes [][]byte
		want   string
	}{
		{"one", h("No"), "1"},
		{"two, in the order of the options", h("Maybe", "Yes"), "0,2"},
		{"all", h("Yes", "No", "Maybe"), "0,1,2"},
		{"the same twice", h("No", "No"), "1"},
		{"one that isn't an option", h("Yes", "Never"), "0"},
		{"none of them", h("Never"), ""},
		{"nothing chosen", nil, ""},
		{"a hash that isn't one", [][]byte{{1, 2, 3}}, ""},
	} {
		if got := voteIndices(pollNames, tc.hashes); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
	if got := pollOptions(`{"options":["A","B"],"max":1}`); len(got) != 2 || got[1] != "B" {
		t.Errorf("options of an extra: %q", got)
	}
	if got := pollOptions(""); len(got) != 0 {
		t.Errorf("options of no extra: %q", got)
	}
}

// A vote that names none of the options is dropped; one that names nothing takes the vote back.
func TestVoteSelection(t *testing.T) {
	tx, _ := newTestTx(t)
	poll, _ := rowFromMessage("c", &events.Message{Message: pollMessage("Q?"), Info: types.MessageInfo{ID: "p1"}}, "")
	h := func(names ...string) [][]byte { return whatsmeow.HashPollOptions(names) }
	tx.vote(poll, "bob", "Bob", h("Yes", "Maybe"), 10)
	tx.vote(poll, "bob", "Bob", h("Never"), 20)
	if o, ts := voteOf(t, tx, "c", "p1", "bob"); o != "0,2" || ts != 10 {
		t.Errorf("a vote for nothing we know: %q at %d", o, ts)
	}
	tx.vote(poll, "bob", "Bob", nil, 30)
	if o, _ := voteOf(t, tx, "c", "p1", "bob"); o != "" {
		t.Errorf("an empty vote left %q", o)
	}
}

// A v4 database, as shipped before the message types, keeps its rows and gets extra and poll_votes.
func TestMigratePolls(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chats.db")
	old, err := sql.Open("sqlite3", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range append(migrations[:4:4], "PRAGMA user_version = 4",
		`INSERT INTO chats(jid, kind) VALUES('a@lid', 0)`,
		`INSERT INTO messages(chat, id, ts, out, state, kind, text) VALUES('a@lid', 'm1', 1, 0, 3, 7, 'Poll: Pizza?')`) {
		if _, err := old.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	old.Close()
	db, err := openChats(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var v int
	var kind int
	var text string
	var extra sql.NullString
	err = db.QueryRow("SELECT (SELECT user_version FROM pragma_user_version), kind, text, extra FROM messages WHERE id = 'm1'").Scan(&v, &kind, &text, &extra)
	if err != nil || v != len(migrations) || kind != msgOther || text != "Poll: Pizza?" || extra.Valid {
		t.Fatalf("version %d, kind %d, text %q, extra %v: %v", v, kind, text, extra, err)
	}
	ins := `INSERT INTO poll_votes(chat, msg, sender, sender_name, options, ts) VALUES('a@lid', 'm1', ?, NULL, '0,2', 5)`
	for _, sender := range []string{"", "b@lid"} {
		if _, err := db.Exec(ins, sender); err != nil {
			t.Fatalf("vote by %q: %v", sender, err)
		}
	}
	if _, err := db.Exec(ins, ""); err == nil {
		t.Error("two votes by the same voter on one poll")
	}
}

// A message row keeps its extra through insert and read.
func TestExtraRoundTrip(t *testing.T) {
	tx, _ := newTestTx(t)
	tx.ensureChat("c", chatUser)
	tx.insertMessage(msgRow{Chat: "c", ID: "m1", TS: 1, Kind: msgPoll, Text: "Q", Extra: `{"options":["A"],"max":0}`})
	tx.insertMessage(msgRow{Chat: "c", ID: "m2", TS: 2, Kind: msgText, Text: "hi"})
	if r, _ := tx.messageByID("c", "m1"); r.Extra != `{"options":["A"],"max":0}` {
		t.Errorf("extra %q", r.Extra)
	}
	if r, _ := tx.messageByID("c", "m2"); r.Extra != "" {
		t.Errorf("no extra: %q", r.Extra)
	}
	if n := count(t, tx, "SELECT count(*) FROM messages WHERE extra IS NULL"); n != 1 {
		t.Errorf("%d rows with no extra, want 1 (NULL, not '')", n)
	}
}

// webPoll is a poll of the history sync with the votes it carries.
func webPoll(chat, id, from string, fromMe bool, ts uint64, ups ...*waWeb.PollUpdate) *waHistorySync.HistorySyncMsg {
	m := webMsg(chat, id, from, "", fromMe, ts)
	m.Message.Message, m.Message.PollUpdates = pollMessage("Pizza?"), ups
	return m
}

func webVote(fromMe bool, participant string, ms int64, names ...string) *waWeb.PollUpdate {
	k := &waCommon.MessageKey{FromMe: proto.Bool(fromMe)}
	if participant != "" {
		k.Participant = proto.String(participant)
	}
	return &waWeb.PollUpdate{PollUpdateMessageKey: k, SenderTimestampMS: proto.Int64(ms),
		Vote: &waE2E.PollVoteMessage{SelectedOptions: whatsmeow.HashPollOptions(names)}}
}

func TestHistoryVotes(t *testing.T) {
	st, cli := historyFixture(t)
	tx, _ := newTestTx(t)
	ctx := context.Background()
	group := types.NewJID("120363000000000001", types.GroupServer).String()
	bob, carol := "4915700000002@s.whatsapp.net", "4915700000004@s.whatsapp.net"
	pn := types.NewJID("4915700000001", types.DefaultUserServer)
	lid := types.NewJID("99887766", types.HiddenUserServer)

	hs := &waHistorySync.HistorySync{Conversations: []*waHistorySync.Conversation{
		{ID: proto.String(group), Name: proto.String("Family"), Messages: []*waHistorySync.HistorySyncMsg{
			webPoll(group, "G1", bob, false, 1700000000,
				webVote(false, carol, 1700000050000, "Yes", "Maybe"),
				webVote(true, "", 1700000060000, "No"),
				webVote(false, carol, 1700000040000, "No"),  // older than carol's other: loses
				webVote(false, "", 1700000070000, "Yes"),    // a group vote with no voter: skipped
				webVote(false, bob, 1700000080000, "Never"), // none of the options: dropped
				webVote(false, bob, 1700000095000)),         // nothing chosen, nothing to take back
			webMsg(group, "G2", bob, "not a poll", false, 1700000010,
				webReaction(false, carol, "😂", 1700000050000)),
		}},
		{ID: proto.String(pn.String()), LidJID: proto.String(lid.String()), Messages: []*waHistorySync.HistorySyncMsg{
			webPoll(pn.String(), "P1", "", true, 1700000000,
				webVote(false, "", 1700000050000, "Maybe"),
				webVote(true, "", 1700000051000, "Yes")),
			webPoll(pn.String(), "P2", "", false, 1700000001), // no votes
		}},
	}}
	st.history(ctx, tx, cli, &events.HistorySync{Data: hs})

	for _, c := range []struct {
		chat, msg, sender, options, name string
		ts                               int64
	}{
		{group, "G1", carol, "0,2", "+4915700000004", 1700000050},
		{group, "G1", "", "1", "", 1700000060},
		{lid.String(), "P1", lid.String(), "2", "+4915700000001", 1700000050}, // the chat's own key, not the number
		{lid.String(), "P1", "", "0", "", 1700000051},
	} {
		var options, name string
		var ts int64
		err := tx.tx.QueryRow("SELECT options, COALESCE(sender_name, ''), ts FROM poll_votes WHERE chat = ? AND msg = ? AND sender = ?",
			c.chat, c.msg, c.sender).Scan(&options, &name, &ts)
		if err != nil || options != c.options || name != c.name || ts != c.ts {
			t.Errorf("%+v: got %q %q at %d: %v", c, options, name, ts, err)
		}
	}
	if n := count(t, tx, "SELECT count(*) FROM poll_votes"); n != 4 {
		t.Errorf("%d votes, want 4", n)
	}
	if n := count(t, tx, "SELECT count(*) FROM reactions"); n != 1 {
		t.Errorf("%d reactions, want 1 (G2's)", n)
	}
	// A vote leaves no row of its own and no preview: the chat's is its newest message's.
	if n := count(t, tx, "SELECT count(*) FROM messages"); n != 4 {
		t.Errorf("%d messages, want 4", n)
	}
	if p := previewOf(tx, lid.String()); p != "Poll: Pizza?" {
		t.Errorf("1:1 preview %q", p)
	}
	// The same history again changes nothing.
	tx.msgs = map[string]map[string]bool{}
	st.history(ctx, tx, cli, &events.HistorySync{Data: hs})
	if n := count(t, tx, "SELECT count(*) FROM poll_votes"); n != 4 || len(tx.msgs) != 0 {
		t.Errorf("a second sync: %d votes, changed %v", n, tx.msgs)
	}
}

// A live vote is decrypted with the poll's secret, matched to the options, and leaves no row, no
// unread and no preview. Ours, from another device, is made with hypermeow's own encryption.
func TestLiveVote(t *testing.T) {
	st, cli := historyFixture(t)
	tx, _ := newTestTx(t)
	ctx := context.Background()
	bob := types.NewJID("4915700000002", types.DefaultUserServer)
	chat := bob.String()
	secret := make([]byte, 32)
	if err := cli.Store.MsgSecrets.PutMessageSecret(ctx, bob, bob, "POLL1", secret); err != nil {
		t.Fatal(err)
	}
	tx.ensureChat(chat, chatUser)
	poll, _ := rowFromMessage(chat, &events.Message{Message: pollMessage("Pizza?"), Info: types.MessageInfo{ID: "POLL1", Timestamp: time.Unix(1700000000, 0)}}, "")
	tx.insertMessage(poll)
	tx.setLast(chat, poll.TS, chatPreview(poll), false, stateRead)
	tx.addUnread(chat, 1)
	tx.msgs = map[string]map[string]bool{}

	pollInfo := &types.MessageInfo{MessageSource: types.MessageSource{Chat: bob, Sender: bob}, ID: "POLL1"}
	vote := func(chosen ...string) *events.Message {
		t.Helper()
		pu, err := cli.EncryptPollVote(ctx, pollInfo, &waE2E.PollVoteMessage{SelectedOptions: whatsmeow.HashPollOptions(chosen)})
		if err != nil {
			t.Fatal(err)
		}
		pu.SenderTimestampMS = proto.Int64(1700000100000)
		return &events.Message{Message: &waE2E.Message{PollUpdateMessage: pu}, Info: types.MessageInfo{
			ID:            "V" + time.Now().String(),
			MessageSource: types.MessageSource{Chat: bob, Sender: cli.Store.ID.ToNonAD(), IsFromMe: true},
			Timestamp:     time.Unix(1700000100, 0)}}
	}
	quiet := func(what string) {
		t.Helper()
		if n := count(t, tx, "SELECT count(*) FROM messages"); n != 1 {
			t.Errorf("%s: %d messages", what, n)
		}
		if n := count(t, tx, "SELECT unread FROM chats WHERE jid = ?", chat); n != 1 {
			t.Errorf("%s: %d unread", what, n)
		}
		if p := previewOf(tx, chat); p != "Poll: Pizza?" {
			t.Errorf("%s: preview %q", what, p)
		}
	}

	e := vote("Yes", "Maybe")
	st.message(ctx, tx, cli, e)
	if o, ts := voteOf(t, tx, chat, "POLL1", ""); o != "0,2" || ts != e.Message.GetPollUpdateMessage().GetSenderTimestampMS()/1000 {
		t.Errorf("our vote: %q at %d", o, ts)
	}
	if !took(tx, chat, "POLL1") {
		t.Error("the poll wasn't named as changed")
	}
	quiet("after a vote")

	// Without a millisecond stamp the message's time stands in (the same second here, which the
	// later arrival wins); a vote for nothing takes ours back.
	e = vote()
	e.Message.PollUpdateMessage.SenderTimestampMS = nil
	st.message(ctx, tx, cli, e)
	if o, _ := voteOf(t, tx, chat, "POLL1", ""); o != "" {
		t.Errorf("after the vote was taken back: %q", o)
	}
	quiet("after taking it back")

	// A vote for a poll we don't hold, a payload that doesn't decrypt, and one for a message that isn't a poll are dropped.
	e = vote("No")
	e.Message.PollUpdateMessage.PollCreationMessageKey.ID = proto.String("NOPE")
	st.message(ctx, tx, cli, e)
	e = vote("No")
	e.Message.PollUpdateMessage.Vote.EncPayload = []byte("not a vote, but long enough to try ........")
	st.message(ctx, tx, cli, e)
	tx.insertMessage(msgRow{Chat: chat, ID: "TXT", TS: 5, Kind: msgText, Text: "x"})
	e = vote("No")
	e.Message.PollUpdateMessage.PollCreationMessageKey.ID = proto.String("TXT")
	st.message(ctx, tx, cli, e)
	if n := count(t, tx, "SELECT count(*) FROM poll_votes"); n != 0 {
		t.Errorf("%d votes after the dropped ones", n)
	}
}

// poll_votes goes where reactions go: with its message, with its chat, and to the LID chat.
func TestPollVotesGoWithTheirPoll(t *testing.T) {
	tx, _ := newTestTx(t)
	c, d := "g@g.us", "h@g.us"
	tx.ensureChat(c, chatGroup)
	tx.ensureChat(d, chatGroup)
	for _, chat := range []string{c, d} {
		for _, id := range []string{"1", "2", "3"} {
			tx.insertMessage(msgRow{Chat: chat, ID: id, TS: 5, Kind: msgPoll, Text: "x"})
			tx.setVote(chat, id, "bob", "Bob", "0", 10)
			tx.setVote(chat, id, "", "", "1", 10)
		}
	}
	left := func(chat string) int { return count(t, tx, "SELECT count(*) FROM poll_votes WHERE chat = ?", chat) }

	tx.deleteMessage(c, "1")
	if left(c) != 4 || left(d) != 6 {
		t.Errorf("after delete for me: %d and %d votes", left(c), left(d))
	}
	tx.revokeMessage(c, "2")
	if left(c) != 2 || left(d) != 6 {
		t.Errorf("after revoke: %d and %d votes", left(c), left(d))
	}
	tx.clearChat(c, false)
	if left(c) != 0 || left(d) != 6 {
		t.Errorf("after clear: %d and %d votes", left(c), left(d))
	}
	tx.clearChat(d, true)
	if left(d) != 0 {
		t.Errorf("after delete chat: %d votes", left(d))
	}
}

func TestMergeChatMovesPollVotes(t *testing.T) {
	tx, _ := newTestTx(t)
	pn, lid := "4915700000001@s.whatsapp.net", "99887766@lid"
	tx.ensureChat(pn, chatUser)
	tx.ensureChat(lid, chatUser)
	for _, m := range []msgRow{{Chat: pn, ID: "m1", TS: 1}, {Chat: pn, ID: "m2", TS: 2}, {Chat: lid, ID: "m1", TS: 1}} {
		m.Kind = msgPoll
		tx.insertMessage(m)
	}
	tx.setVote(pn, "m1", pn, "Bob", "0", 10) // theirs, keyed by the number
	tx.setVote(pn, "m1", "", "", "1", 10)    // ours
	tx.setVote(pn, "m2", pn, "Bob", "2", 10)
	tx.setVote(lid, "m1", lid, "Bob", "0,1", 20) // the LID chat has theirs on m1 already
	mergeChat(tx, pn, lid)
	if n := count(t, tx, "SELECT count(*) FROM poll_votes WHERE chat = ? OR sender = ?", pn, pn); n != 0 {
		t.Errorf("%d votes left under the number", n)
	}
	for _, c := range []struct{ msg, sender, options string }{
		{"m1", lid, "0,1"}, {"m1", "", "1"}, {"m2", lid, "2"},
	} {
		if o, _ := voteOf(t, tx, lid, c.msg, c.sender); o != c.options {
			t.Errorf("%s by %q: %q, want %q", c.msg, c.sender, o, c.options)
		}
	}
	if n := count(t, tx, "SELECT count(*) FROM poll_votes"); n != 3 {
		t.Errorf("%d votes, want 3", n)
	}
}

func TestClearTakesPollVotes(t *testing.T) {
	st, _ := historyFixture(t)
	st.runOp(func(tx *storeTx) {
		tx.ensureChat("g@g.us", chatGroup)
		tx.insertMessage(msgRow{Chat: "g@g.us", ID: "1", TS: 5, Kind: msgPoll, Text: "x"})
		tx.setVote("g@g.us", "1", "bob", "Bob", "0", 10)
	})
	eventually(t, st, 1, "SELECT count(*) FROM poll_votes")
	st.clear()
	eventually(t, st, 0, "SELECT count(*) FROM poll_votes")
}
