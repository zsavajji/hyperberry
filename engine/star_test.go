package main

import (
	"context"
	"testing"

	"github.com/polymorfa/hypermeow/proto/waHistorySync"
	"github.com/polymorfa/hypermeow/proto/waSyncAction"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"
)

func TestSimMsgStar(t *testing.T) {
	s, st, c := linkedSim(t)
	anna := simChats[0].jid
	star := func(body string) error {
		_, err := s.handleStar([]byte(`{"chat":"` + anna + `",` + body + `}`))
		return err
	}
	if err := star(`"id":"SIMSEED0","star":true`); err != nil {
		t.Fatal(err)
	}
	msgsNamed(t, c, anna, "SIMSEED0") // the open chat redraws the card
	eventually(t, st, 1, "SELECT starred FROM messages WHERE chat = ? AND id = 'SIMSEED0'", anna)
	if err := star(`"id":"SIMSEED0","star":false`); err != nil {
		t.Fatal(err)
	}
	msgsNamed(t, c, anna, "SIMSEED0")
	eventually(t, st, 0, "SELECT starred FROM messages WHERE chat = ? AND id = 'SIMSEED0'", anna)

	for body, want := range map[string]string{
		`"id":"SIMSEED0"`:           "bad_request", // star is missing, not false
		`"star":true`:               "bad_request",
		`"id":"NOPE","star":true`:   "not_found",
		`"id":"call-X","star":true`: "not_found",
	} {
		if err := star(body); code(err) != want {
			t.Errorf("%s: %v, want %s", body, err, want)
		}
	}
}

// A Star event from the phone sets or clears the flag, and tells the open chat only when it changed.
func TestStarEvent(t *testing.T) {
	st, cli := historyFixture(t)
	tx, _ := newTestTx(t)
	bob := types.NewJID("4915700000002", types.DefaultUserServer)
	key := bob.String()
	tx.ensureChat(key, chatUser)
	tx.insertMessage(msgRow{Chat: key, ID: "M1", TS: 1, Kind: msgText, Text: "hi"})
	star := func(v bool) {
		tx.msgs = map[string]map[string]bool{}
		st.dispatch(tx, cli, &events.Star{ChatJID: bob, MessageID: "M1", Action: &waSyncAction.StarAction{Starred: proto.Bool(v)}})
	}
	starred := func() int { return count(t, tx, "SELECT starred FROM messages WHERE chat = ? AND id = 'M1'", key) }
	star(true)
	if starred() != 1 || !tx.msgs[key]["M1"] {
		t.Errorf("a star: starred %d, told %v", starred(), tx.msgs)
	}
	star(true)
	if len(tx.msgs) != 0 {
		t.Errorf("the same star again told the chat: %v", tx.msgs)
	}
	star(false)
	if starred() != 0 {
		t.Error("an unstar left the star")
	}
}

// The patch names a group message's sender, and "0" where the sender is the chat itself or ours.
func TestStarPatch(t *testing.T) {
	group := types.NewJID("120363000000000001", types.GroupServer)
	user := types.NewJID("4915700000002", types.DefaultUserServer)
	for _, tc := range []struct {
		chat types.JID
		row  msgRow
		want []string
	}{
		{group, msgRow{ID: "A", Sender: "99887766@lid"}, []string{"star", group.String(), "A", "0", "99887766@lid"}},
		{group, msgRow{ID: "B", Out: true}, []string{"star", group.String(), "B", "1", "0"}},
		{user, msgRow{ID: "C"}, []string{"star", user.String(), "C", "0", "0"}},
		{user, msgRow{ID: "D", Out: true}, []string{"star", user.String(), "D", "1", "0"}},
	} {
		got := starPatch(tc.chat, tc.row, true).Mutations[0].Index
		if len(got) != len(tc.want) {
			t.Fatalf("%+v: index %v", tc.row, got)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%+v: index %v, want %v", tc.row, got, tc.want)
				break
			}
		}
	}
}

// A history sync's message carries the star it has on the phone.
func TestHistoryStarred(t *testing.T) {
	st, cli := historyFixture(t)
	tx, _ := newTestTx(t)
	bob := "4915700000002@s.whatsapp.net"
	starred, plain := webMsg(bob, "S1", "", "keep", false, 1700000000), webMsg(bob, "S2", "", "meh", false, 1700000010)
	starred.Message.Starred = proto.Bool(true)
	st.history(context.Background(), tx, cli, &events.HistorySync{Data: &waHistorySync.HistorySync{
		Conversations: []*waHistorySync.Conversation{{ID: proto.String(bob), Messages: []*waHistorySync.HistorySyncMsg{starred, plain}}}}})
	if n := count(t, tx, "SELECT count(*) FROM messages WHERE starred = 1 AND id = 'S1'"); n != 1 {
		t.Error("the starred message is not starred")
	}
	if n := count(t, tx, "SELECT count(*) FROM messages WHERE starred = 1"); n != 1 {
		t.Errorf("%d starred, want 1", n)
	}
}
