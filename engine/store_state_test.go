package main

import "testing"

const tc = "a@s.whatsapp.net"

func stateOf(t *testing.T, tx *storeTx, id string) (s int, ts int64) {
	t.Helper()
	if err := tx.tx.QueryRow("SELECT state, ts FROM messages WHERE chat = ? AND id = ?", tc, id).Scan(&s, &ts); err != nil {
		t.Fatal(err)
	}
	return
}

func TestSetStateOnlyRaises(t *testing.T) {
	tx, _ := newTestTx(t)
	tx.ensureChat(tc, chatUser)
	tx.insertMessage(msgRow{Chat: tc, ID: "1", TS: 10, Out: true, State: stateSending, Kind: msgText, Text: "hi"})
	tx.insertMessage(msgRow{Chat: tc, ID: "in", TS: 5, Kind: msgText, State: stateRead})
	tx.setLast(tc, 10, "You: hi", true, stateSending)
	tx.setState(tc, "1", stateDelivered)
	tx.sentAs(tc, "1", 12, stateSent) // late: keeps delivered, takes the server time
	if s, ts := stateOf(t, tx, "1"); s != stateDelivered || ts != 12 {
		t.Errorf("state %d ts %d", s, ts)
	}
	tx.sentAs(tc, "1", 0, stateFailed)
	tx.setState(tc, "in", stateDelivered) // not ours
	if s, _ := stateOf(t, tx, "1"); s != stateDelivered {
		t.Errorf("failure lowered it to %d", s)
	}
	if s, _ := stateOf(t, tx, "in"); s != stateRead {
		t.Errorf("incoming changed to %d", s)
	}
}

func TestSentAsAndLastState(t *testing.T) {
	tx, _ := newTestTx(t)
	tx.ensureChat(tc, chatUser)
	tx.insertMessage(msgRow{Chat: tc, ID: "old", TS: 1, Out: true, Kind: msgText})
	tx.insertMessage(msgRow{Chat: tc, ID: "1", TS: 10, Out: true, Kind: msgText})
	tx.setLast(tc, 10, "You: x", true, stateSending)
	tx.sentAs(tc, "1", 11, stateSent)
	if s, ts := stateOf(t, tx, "1"); s != stateSent || ts != 11 {
		t.Errorf("state %d ts %d", s, ts)
	}
	last := func() (st int, ts int64) {
		tx.tx.QueryRow("SELECT last_state, last_ts FROM chats WHERE jid = ?", tc).Scan(&st, &ts)
		return
	}
	if st, ts := last(); st != stateSent || ts != 11 {
		t.Errorf("last %d %d", st, ts)
	}
	tx.setState(tc, "old", stateRead) // not the newest: the chat row stays
	tx.setState(tc, "1", stateRead)
	if st, _ := last(); st != stateRead {
		t.Errorf("last_state %d", st)
	}
	f := msgRow{Chat: tc, ID: "2", TS: 20, Out: true, Kind: msgText}
	tx.insertMessage(f)
	tx.sentAs(tc, "2", 0, stateFailed)
	if s, ts := stateOf(t, tx, "2"); s != stateFailed || ts != 20 {
		t.Errorf("failed: %d %d", s, ts)
	}
}

func TestUnreadIncoming(t *testing.T) {
	tx, _ := newTestTx(t)
	tx.ensureChat(tc, chatUser)
	for i, id := range []string{"a", "b", "c", "d"} {
		tx.insertMessage(msgRow{Chat: tc, ID: id, TS: int64(i + 1), Kind: msgText, Sender: "s" + id})
	}
	tx.insertMessage(msgRow{Chat: tc, ID: "mine", TS: 9, Out: true, Kind: msgText})
	tx.setUnread(tc, 2)
	got := tx.unreadIncoming(tc)
	if len(got) != 2 || got[0].ID != "d" || got[1].ID != "c" || got[0].Sender != "sd" {
		t.Errorf("got %v", got)
	}
	tx.setUnread(tc, 0)
	if n := count(t, tx, "SELECT unread FROM chats WHERE jid = ?", tc); n != 0 {
		t.Errorf("unread %d", n)
	}
	if got := tx.unreadIncoming(tc); len(got) != 1 || got[0].ID != "d" {
		t.Errorf("at least one: %v", got)
	}
	if m, ok := tx.messageByID(tc, "a"); !ok || m.Sender != "sa" {
		t.Errorf("messageByID %v %v", m, ok)
	}
	if _, ok := tx.messageByID(tc, "zz"); ok {
		t.Error("found a missing message")
	}
}
