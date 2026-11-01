package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
)

func TestExportText(t *testing.T) {
	loc := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = loc })
	const t0 = 1700000000 // 14/11/23, 22:13 UTC

	one := exportText("393330000001@s.whatsapp.net", "Anna", false, []msgRow{
		{TS: t0, Out: true, Kind: msgText, Text: "Hi"},
		{TS: t0 + 60, Kind: msgText, Text: "Hello\nsecond line"},
		{TS: t0 + 120, Kind: msgPhoto, Text: "a caption"},
		{TS: t0 + 180, Out: true, Kind: msgVoice},
		{TS: t0 + 240, Kind: msgText, Deleted: true},
		{TS: t0 + 300, Kind: msgService, Text: "Messages are end-to-end encrypted"},
	})
	want := "14/11/23, 22:13 - You: Hi\n" +
		"14/11/23, 22:14 - Anna: Hello\nsecond line\n" +
		"14/11/23, 22:15 - Anna: <Media omitted>\n" +
		"14/11/23, 22:16 - You: <Media omitted>\n" +
		"14/11/23, 22:17 - Anna: This message was deleted\n" +
		"14/11/23, 22:18 - Messages are end-to-end encrypted\n"
	if one != want {
		t.Errorf("1:1:\n%s\nwant:\n%s", one, want)
	}

	group := exportText("120363000000000001@g.us", "Family", true, []msgRow{
		{TS: t0, Out: true, Kind: msgText, Text: "Hi all"},
		{TS: t0 + 60, Sender: "111@lid", SenderName: "Mum", Kind: msgText, Text: "Dessert?"},
		{TS: t0 + 120, Sender: "222@lid", Kind: msgSticker}, // no name: the number
		{TS: t0 + 180, Sender: "111@lid", SenderName: "Mum", Kind: msgDoc, Text: "menu.pdf"},
		{TS: t0 + 240, Kind: msgService, Text: "Luca added Marco"},
		{TS: t0 + 300, Sender: "111@lid", SenderName: "Mum", Kind: msgVideo, Deleted: true},
	})
	want = "14/11/23, 22:13 - You: Hi all\n" +
		"14/11/23, 22:14 - Mum: Dessert?\n" +
		"14/11/23, 22:15 - 222: <Media omitted>\n" +
		"14/11/23, 22:16 - Mum: <Media omitted>\n" +
		"14/11/23, 22:17 - Luca added Marco\n" +
		"14/11/23, 22:18 - Mum: This message was deleted\n"
	if group != want {
		t.Errorf("group:\n%s\nwant:\n%s", group, want)
	}

	// No name known: the chat key's user part.
	if got := exportText("393330000007@s.whatsapp.net", "", false, []msgRow{{TS: t0, Kind: msgText, Text: "x"}}); got != "14/11/23, 22:13 - 393330000007: x\n" {
		t.Errorf("unnamed: %q", got)
	}
}

func TestClearAndDeleteChat(t *testing.T) {
	tx, _ := newTestTx(t)
	a, b := "a@s.whatsapp.net", "b@s.whatsapp.net"
	for _, c := range []string{a, b} {
		tx.ensureChat(c, chatUser)
		tx.insertMessage(msgRow{Chat: c, ID: c + "1", TS: 10, Kind: msgText, Text: "hi"})
		tx.insertMessage(msgRow{Chat: c, ID: c + "2", TS: 20, Kind: msgVoice, Duration: 3})
		tx.setLast(c, 20, "🎤 0:03", false, stateRead)
		tx.addUnread(c, 2)
	}
	tx.chats, tx.msgs = map[string]bool{}, map[string]map[string]bool{}

	tx.clearChat(a, false)
	if n := count(t, tx, "SELECT count(*) FROM messages WHERE chat = ?", a); n != 0 {
		t.Errorf("%d rows left in the cleared chat", n)
	}
	if n := count(t, tx, "SELECT count(*) FROM messages WHERE chat = ?", b); n != 2 {
		t.Errorf("the other chat has %d rows", n)
	}
	var ts, unread int
	var preview *string
	if err := tx.tx.QueryRow("SELECT last_ts, last_preview, unread FROM chats WHERE jid = ?", a).Scan(&ts, &preview, &unread); err != nil {
		t.Fatalf("the cleared chat's row: %v", err)
	}
	if ts != 20 || preview != nil || unread != 0 {
		t.Errorf("cleared row: last_ts %d, preview %v, unread %d", ts, preview, unread)
	}
	if !tx.chats[a] || len(tx.msgs[a]) != 2 || !tx.msgs[a][a+"1"] || !tx.msgs[a][a+"2"] {
		t.Errorf("events: chats %v, msgs %v", tx.chats, tx.msgs)
	}

	tx.clearChat(a, true)
	tx.clearChat(b, true)
	if n := count(t, tx, "SELECT (SELECT count(*) FROM chats) + (SELECT count(*) FROM messages)"); n != 0 {
		t.Errorf("%d rows after delete", n)
	}
	if !tx.chats[b] {
		t.Error("no chats event for the deleted chat")
	}
}

// The main phone's own clear and delete come through dispatch, except those of a full sync.
func TestClearDeleteEvents(t *testing.T) {
	tx, _ := newTestTx(t)
	cli := newUnlinked(t).cli
	g, _ := types.ParseJID("120363000000000001@g.us")
	tx.ensureChat(g.String(), chatGroup)
	tx.insertMessage(msgRow{Chat: g.String(), ID: "1", TS: 10, Kind: msgText, Text: "hi"})
	tx.setLast(g.String(), 10, "hi", false, stateRead)
	st := &Store{}
	st.dispatch(tx, cli, &events.ClearChat{JID: g, FromFullSync: true})
	st.dispatch(tx, cli, &events.DeleteChat{JID: g, FromFullSync: true})
	if n := count(t, tx, "SELECT count(*) FROM messages"); n != 1 {
		t.Fatalf("a full sync's clear took the history: %d rows", n)
	}
	st.dispatch(tx, cli, &events.ClearChat{JID: g})
	if n := count(t, tx, "SELECT (SELECT count(*) FROM messages) + (SELECT count(*) FROM chats WHERE last_preview IS NULL)"); n != 1 {
		t.Errorf("after the clear: %d", n)
	}
	st.dispatch(tx, cli, &events.DeleteChat{JID: g})
	if n := count(t, tx, "SELECT count(*) FROM chats"); n != 0 {
		t.Errorf("%d chats after the delete", n)
	}
}

func TestClearChatPatch(t *testing.T) {
	chat := types.NewJID("4915700000001", types.DefaultUserServer)
	p := clearChatPatch(chat, time.Unix(1700000000, 0))
	m := p.Mutations[0]
	if want := []string{"clearChat", chat.String(), "1", "0"}; strings.Join(m.Index, "|") != strings.Join(want, "|") {
		t.Errorf("index %v, want %v", m.Index, want)
	}
	if m.Version != 6 || m.Value.ClearChatAction.MessageRange.GetLastMessageTimestamp() != 1700000000 {
		t.Errorf("version %d, action %v", m.Version, m.Value.ClearChatAction)
	}
	if ts := clearChatPatch(chat, time.Time{}).Mutations[0].Value.ClearChatAction.MessageRange.GetLastMessageTimestamp(); ts < 1700000000 {
		t.Errorf("no message: timestamp %d is not now", ts)
	}
}

func TestIsBlocked(t *testing.T) {
	bl := &types.Blocklist{Items: []types.BlocklistItem{
		{LID: types.NewJID("111", types.HiddenUserServer), PN: types.NewJID("393330000001", types.DefaultUserServer)},
		{LID: types.NewJID("222", types.HiddenUserServer)}, // no number known
	}}
	for _, c := range []struct {
		users []string
		want  bool
	}{
		{[]string{"111", ""}, true},
		{[]string{"999", "393330000001"}, true},
		{[]string{"222", ""}, true},
		{[]string{"", ""}, false}, // an empty number never matches an empty one
		{[]string{"333", "393330000002"}, false},
	} {
		if got := isBlocked(bl, c.users...); got != c.want {
			t.Errorf("%v: %v", c.users, got)
		}
	}
}

func TestChatOpsReject(t *testing.T) {
	s := newUnlinked(t)
	for _, c := range []struct {
		h    Handler
		body string
		want string
	}{
		{s.handleContactBlock, `{"chat":"a@s.whatsapp.net"}`, "bad_request"},
		{s.handleContactBlock, `{"chat":"a@s.whatsapp.net","block":"yes"}`, "bad_request"},
		{s.handleContactBlock, `{"chat":"a@s.whatsapp.net","block":true}`, "unlinked"},
		{s.handleChatClear, `{"chat":"a@s.whatsapp.net"}`, "unlinked"},
		{s.handleChatDelete, `{"chat":"a@s.whatsapp.net"}`, "unlinked"},
		{s.handleChatClear, `[]`, "bad_request"},
		{s.handleChatDelete, `[]`, "bad_request"},
		{s.store.handleExport, `[]`, "bad_request"},
	} {
		if _, err := c.h([]byte(c.body)); code(err) != c.want {
			t.Errorf("%s: %v, want %s", c.body, err, c.want)
		}
	}
	me := types.NewJID("4915700000009", types.DefaultUserServer)
	s.cli.Store.ID = &me
	if _, err := s.handleContactBlock([]byte(`{"chat":"1@g.us","block":true}`)); code(err) != "bad_chat" {
		t.Errorf("block a group: %v", err)
	}
	if _, err := s.handleChatClear([]byte(`{"chat":"not a jid@@@"}`)); code(err) != "bad_chat" {
		t.Errorf("clear, bad key: %v", err)
	}
}

func TestSimBlock(t *testing.T) {
	s, _, _ := linkedSim(t)
	marco := simChats[2].jid
	info := func() contactInfo {
		t.Helper()
		r, err := s.handleContactInfo([]byte(`{"chat":"` + marco + `"}`))
		if err != nil {
			t.Fatal(err)
		}
		return r.(contactInfo)
	}
	if info().Blocked {
		t.Fatal("blocked from the start")
	}
	for _, block := range []string{"true", "false"} {
		if _, err := s.handleContactBlock([]byte(`{"chat":"` + marco + `","block":` + block + `}`)); err != nil {
			t.Fatal(err)
		}
		if got := info().Blocked; got != (block == "true") {
			t.Errorf("block %s: contact.info says %v", block, got)
		}
	}
	if _, err := s.handleContactBlock([]byte(`{"chat":"` + simChats[1].jid + `","block":true}`)); code(err) != "bad_chat" {
		t.Errorf("block a group: %v", err)
	}
}

func TestSimClearDeleteChat(t *testing.T) {
	s, st, c := linkedSim(t)
	family, marco := simChats[1].jid, simChats[2].jid
	var voice string
	st.db.QueryRow("SELECT id FROM messages WHERE chat = ? AND kind = ? LIMIT 1", family, msgVoice).Scan(&voice)
	if voice == "" {
		t.Fatal("no voice note in the family group")
	}
	ogg, wav := oggPath(s.srv.dir, voice), cachedWAV(s.srv.dir, voice)
	for _, f := range []string{ogg, wav} {
		if err := writeFile(f, []byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	n := rows(st, "SELECT count(*) FROM messages WHERE chat = ?", family)

	if _, err := s.handleChatClear([]byte(`{"chat":"` + family + `"}`)); err != nil {
		t.Fatal(err)
	}
	if m := next(t, c, "chats"); m["jids"].([]any)[0] != family {
		t.Errorf("chats event: %v", m)
	}
	if m := next(t, c, "msgs"); m["chat"] != family || len(m["ids"].([]any)) != n {
		t.Errorf("msgs event: %v, want %d ids", m, n)
	}
	eventually(t, st, 0, "SELECT count(*) FROM messages WHERE chat = ?", family)
	eventually(t, st, 1, "SELECT count(*) FROM chats WHERE jid = ? AND last_preview IS NULL AND unread = 0 AND last_ts IS NOT NULL", family)
	for _, f := range []string{ogg, wav} { // removed once the batch has committed
		for end := time.Now().Add(2 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
			if _, err := os.Stat(f); err != nil {
				break
			}
		}
		if _, err := os.Stat(f); err == nil {
			t.Errorf("%s is still there", f)
		}
	}

	if _, err := s.handleChatDelete([]byte(`{"chat":"` + marco + `"}`)); err != nil {
		t.Fatal(err)
	}
	eventually(t, st, 0, "SELECT (SELECT count(*) FROM messages WHERE chat = ?1) + (SELECT count(*) FROM chats WHERE jid = ?1)", marco)
	if _, err := s.handleChatClear([]byte(`{"chat":"not a jid@@@"}`)); code(err) != "bad_chat" {
		t.Errorf("bad key: %v", err)
	}
}

func TestSimExport(t *testing.T) {
	shared := sharedDownloads
	sharedDownloads = filepath.Join(t.TempDir(), "none")
	t.Cleanup(func() { sharedDownloads = shared })
	s, _, _ := linkedSim(t)
	r, err := s.store.handleExport([]byte(`{"chat":"` + simChats[2].jid + `"}`))
	if err != nil {
		t.Fatal(err)
	}
	path := r.(map[string]string)["path"]
	if want := filepath.Join(s.srv.dir, "downloads", "WhatsApp Chat with Marco.txt"); path != want {
		t.Errorf("path %s, want %s", path, want)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), " - Marco: Did you see the match last night? Unbelievable ending\n") {
		t.Errorf("export:\n%s", b)
	}
	if _, err := s.store.handleExport([]byte(`{"chat":"nobody@s.whatsapp.net"}`)); code(err) != "bad_chat" {
		t.Errorf("unknown chat: %v", err)
	}
	// A group's name can have characters a file name can't.
	if got := fileSafe.Replace(`a/b\c:d*e?f"g<h>i|j`); got != "a_b_c_d_e_f_g_h_i_j" {
		t.Errorf("file name %q", got)
	}
}
