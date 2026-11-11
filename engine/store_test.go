package main

import (
	"testing"
)

func TestInsertAndLast(t *testing.T) {
	tx, _ := newTestTx(t)
	tx.ensureChat("a@s.whatsapp.net", chatUser)
	m := msgRow{Chat: "a@s.whatsapp.net", ID: "1", TS: 10, Kind: msgText, Text: "hi"}
	if !tx.insertMessage(m) || tx.insertMessage(m) {
		t.Error("insert twice: want new, then not new")
	}
	if n := count(t, tx, "SELECT count(*) FROM messages"); n != 1 {
		t.Errorf("%d rows", n)
	}
	tx.setLast("a@s.whatsapp.net", 10, "new", false, stateRead)
	tx.setLast("a@s.whatsapp.net", 5, "older", false, stateRead)
	tx.setLast("a@s.whatsapp.net", 10, "same time", false, stateRead)
	var p string
	tx.tx.QueryRow("SELECT last_preview FROM chats").Scan(&p)
	if p != "same time" {
		t.Errorf("preview %q", p)
	}
	tx.setLast("a@s.whatsapp.net", 5, "older", false, stateRead)
	tx.tx.QueryRow("SELECT last_preview FROM chats").Scan(&p)
	if p != "same time" {
		t.Errorf("last moved backwards: %q", p)
	}
}

func TestEditRevoke(t *testing.T) {
	tx, _ := newTestTx(t)
	c := "g@g.us"
	tx.ensureChat(c, chatGroup)
	tx.insertMessage(msgRow{Chat: c, ID: "1", TS: 1, Kind: msgText, Text: "first", SenderName: "Anna Lee"})
	tx.insertMessage(msgRow{Chat: c, ID: "2", TS: 2, Kind: msgText, Text: "second", SenderName: "Anna Lee"})
	tx.setLast(c, 2, "Anna: second", false, stateRead)
	preview := func() (p string) {
		tx.tx.QueryRow("SELECT last_preview FROM chats").Scan(&p)
		return
	}

	tx.editMessage(c, "1", "first!") // not the newest: the preview stays
	if preview() != "Anna: second" {
		t.Errorf("preview %q", preview())
	}
	tx.editMessage(c, "2", "second!")
	if preview() != "Anna: second!" || count(t, tx, "SELECT count(*) FROM messages WHERE edited = 1") != 2 {
		t.Errorf("after edit: %q", preview())
	}
	tx.revokeMessage(c, "2")
	if preview() != "This message was deleted" {
		t.Errorf("after revoke: %q", preview())
	}
	if count(t, tx, "SELECT count(*) FROM messages WHERE id = '2' AND deleted = 1 AND text IS NULL") != 1 {
		t.Error("revoked row")
	}
}

func TestStoreEvents(t *testing.T) {
	ts := newTestServer(t)
	c := ts.login(t)
	db, err := openChats(t.TempDir() + "/c.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	st := newStore(db, ts.Server, ts.Server.log)
	st.ops <- func(tx *storeTx) {
		tx.ensureChat("a@s.whatsapp.net", chatUser)
		tx.insertMessage(msgRow{Chat: "a@s.whatsapp.net", ID: "1", TS: 1, Kind: msgText, Text: "x"})
		tx.insertMessage(msgRow{Chat: "a@s.whatsapp.net", ID: "2", TS: 2, Kind: msgText, Text: "y"})
	}
	got := map[string]map[string]any{}
	for i := 0; i < 2; i++ {
		m := c.read(t)
		got[m["ev"].(string)] = m
	}
	if j := got["chats"]["jids"].([]any); len(j) != 1 || j[0] != "a@s.whatsapp.net" {
		t.Errorf("chats event: %v", got["chats"])
	}
	if m := got["msgs"]; m["chat"] != "a@s.whatsapp.net" || len(m["ids"].([]any)) != 2 {
		t.Errorf("msgs event: %v", m)
	}

	st.clear()
	st.close()
	var n int
	db.QueryRow("SELECT (SELECT count(*) FROM chats) + (SELECT count(*) FROM messages)").Scan(&n)
	if n != 0 {
		t.Errorf("%d rows after clear", n)
	}
}

func TestDropEmptyGroups(t *testing.T) {
	tx, _ := newTestTx(t)
	tx.ensureChat("empty@g.us", chatGroup)  // what syncGroups added: dropped
	tx.ensureChat("joined@g.us", chatGroup) // stamped (JoinedGroup, history): kept
	tx.stampChat("joined@g.us", 10)
	tx.ensureChat("talk@g.us", chatGroup) // holds a message: kept
	tx.insertMessage(msgRow{Chat: "talk@g.us", ID: "1", TS: 1, Kind: msgText, Text: "hi"})
	tx.ensureChat("a@s.whatsapp.net", chatUser) // not a group: kept
	tx.dropEmptyGroups()
	if n := count(t, tx, "SELECT count(*) FROM chats"); n != 3 || count(t, tx, "SELECT count(*) FROM chats WHERE jid = 'empty@g.us'") != 0 {
		t.Errorf("%d chats left", n)
	}
}
