package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/polymorfa/hypermeow/types"
	waLog "github.com/polymorfa/hypermeow/util/log"
)

// useBook gives the test an empty phoneBook and puts the old one back after.
func useBook(t *testing.T) *addressBook {
	t.Helper()
	old := phoneBook
	phoneBook = &addressBook{}
	t.Cleanup(func() { phoneBook = old })
	return phoneBook
}

func TestBookName(t *testing.T) {
	const it = "393201112222"
	for _, c := range []struct{ what, own, saved, user, want string }{
		{"international, plus", it, "+39 333 123 4567", "393331234567", "Anna"},
		{"international, 00", it, "0039 3331234567", "393331234567", "Anna"},
		{"national", it, "333 1234567", "393331234567", "Anna"},
		{"national landline keeps its 0", it, "041 1234567", "390411234567", "Anna"},
		{"national, another country", it, "7700900123", "447700900123", ""},
		{"national, own country unknown", "", "333 1234567", "393331234567", ""},
		{"UK trunk zero", "447000000000", "07700 900123", "447700900123", "Anna"},
		{"US", "14155550000", "(415) 555-2671", "14155552671", "Anna"},
		{"short code", it, "112", "39112", ""},
		{"nobody", it, "333 1234567", "393339999999", ""},
	} {
		b := &addressBook{}
		b.set([]bookEntry{{c.saved, "Anna"}})
		if got := b.name(c.user, c.own); got != c.want {
			t.Errorf("%s: %q, want %q", c.what, got, c.want)
		}
	}
}

func TestCallingCode(t *testing.T) {
	for number, want := range map[string]string{
		"14155552671": "1", "79991234567": "7", "201001234567": "20", "393331234567": "39",
		"447700900123": "44", "212612345678": "212", "351912345678": "351", "971501234567": "971",
		"": "", "21": "", "3": "",
	} {
		if got := callingCode(number); got != want {
			t.Errorf("%q: %q, want %q", number, got, want)
		}
	}
}

func TestBookPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.json")
	entries := []bookEntry{{"+39 333 123 4567", "Anna"}, {"+44 7700 900123", "Bob"}, {"112", "Police"}, {"+39 333 000 0000", ""}}
	a := &addressBook{}
	a.load(path) // no file yet
	if a.name("393331234567", "") != "" {
		t.Error("an unread book has names")
	}
	if !a.set(entries) {
		t.Error("the first set changed nothing")
	}
	if a.set(entries) {
		t.Error("the same book again was a change")
	}
	if st, err := os.Stat(path); err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("book file: %v, %v", st, err)
	}
	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Error("temp file left behind")
	}
	b := &addressBook{}
	b.load(path)
	if b.name("393331234567", "") != "Anna" || b.name("447700900123", "") != "Bob" || len(b.byNumber) != 2 {
		t.Errorf("after a reload: %v", b.byNumber)
	}
	if b.set(entries) {
		t.Error("the loaded book took the same entries as a change")
	}
	if !b.set(nil) { // the UI's decision: an empty book
		t.Error("emptying the book was no change")
	}
	c := &addressBook{}
	c.load(path)
	if len(c.byNumber) != 0 {
		t.Errorf("emptied book came back with %v", c.byNumber)
	}

	if err := os.WriteFile(path, []byte(`{"393331234567": "Anna"`), 0o600); err != nil {
		t.Fatal(err)
	}
	d := &addressBook{}
	d.load(path)
	if len(d.byNumber) != 0 {
		t.Errorf("damaged file gave %v", d.byNumber)
	}
	if !d.set(entries) { // and the next book replaces the damage
		t.Error("set after a damaged load changed nothing")
	}
}

// contacts.book works unlinked, rejects bad JSON, and queues a rename only for a linked
// client and a changed book.
func TestHandleBook(t *testing.T) {
	useBook(t)
	s := &Session{cli: savedClient(t, "393201112222"), log: waLog.Noop, store: &Store{ops: make(chan func(*storeTx), 4)}}
	book := `{"entries":[{"number":"333 1234567","name":"Anna"}]}`
	if _, err := s.handleBook([]byte(`{"entries":"x"}`)); code(err) != "bad_request" {
		t.Errorf("bad entries: %v", err)
	}
	s.cli.Store.ID = nil
	if _, err := s.handleBook([]byte(book)); err != nil || len(s.store.ops) != 0 {
		t.Errorf("unlinked: %v, %d ops", err, len(s.store.ops))
	}
	if phoneBook.name("393331234567", "393201112222") != "Anna" {
		t.Error("an unlinked engine dropped the book")
	}
	me := types.NewJID("393201112222", types.DefaultUserServer)
	s.cli.Store.ID = &me
	if _, err := s.handleBook([]byte(book)); err != nil || len(s.store.ops) != 0 {
		t.Errorf("the same book, linked: %v, %d ops", err, len(s.store.ops))
	}
	if _, err := s.handleBook([]byte(`{"entries":[{"number":"333 1234567","name":"Anna B"}]}`)); err != nil || len(s.store.ops) != 1 {
		t.Errorf("a changed book, linked: %v, %d ops", err, len(s.store.ops))
	}
}

// The phone's book beats WhatsApp's saved name, which stays for whoever the book doesn't have;
// a LID reaches the book through its phone number; nothing panics while unlinked.
func TestBookPrecedence(t *testing.T) {
	useBook(t)
	ctx := context.Background()
	cli := savedClient(t, "393201112222")
	anna := types.NewJID("393331234567", types.DefaultUserServer)
	bob := types.NewJID("393339999999", types.DefaultUserServer)
	annaLID := types.NewJID("99887766", types.HiddenUserServer)
	for j, name := range map[types.JID]string{anna: "WA Anna", bob: "WA Bob"} {
		if err := cli.Store.Contacts.PutContactName(ctx, j, name, name); err != nil {
			t.Fatal(err)
		}
	}
	if err := cli.Store.LIDs.PutLIDMapping(ctx, annaLID, anna); err != nil {
		t.Fatal(err)
	}
	names := func() [4]string {
		return [4]string{
			displayName(ctx, cli, anna, types.EmptyJID), displayName(ctx, cli, bob, types.EmptyJID),
			displayName(ctx, cli, annaLID, types.EmptyJID), resolveName(ctx, cli, anna, types.EmptyJID, "push"),
		}
	}
	if got, want := names(), [4]string{"WA Anna", "WA Bob", "WA Anna", "WA Anna"}; got != want {
		t.Fatalf("before the book: %q, want %q", got, want)
	}
	phoneBook.set([]bookEntry{{"333 1234567", "Book Anna"}})
	if got, want := names(), [4]string{"Book Anna", "WA Bob", "Book Anna", "Book Anna"}; got != want {
		t.Errorf("with the book: %q, want %q", got, want)
	}
	phoneBook.set(nil)
	if got, want := names(), [4]string{"WA Anna", "WA Bob", "WA Anna", "WA Anna"}; got != want {
		t.Errorf("book emptied: %q, want %q", got, want)
	}

	// Unlinked there is no number of ours: international entries still match, national ones don't.
	cli.Store.ID = nil
	phoneBook.set([]bookEntry{{"333 1234567", "Book Anna"}, {"+39 333 999 9999", "Book Bob"}})
	if got, want := names(), [4]string{"WA Anna", "Book Bob", "WA Anna", "WA Anna"}; got != want {
		t.Errorf("unlinked: %q, want %q", got, want)
	}
}

// renameAll gives a chat and a group sender the book's name, including a person WhatsApp has no
// contact card of, whose push name was all there was.
func TestRenameAllBook(t *testing.T) {
	useBook(t)
	cli := savedClient(t, "393201112222")
	tx, _ := newTestTx(t)
	chat, group := "393331234567@s.whatsapp.net", "g@g.us"
	tx.ensureChat(chat, chatUser)
	tx.setName(chat, "push name")
	tx.ensureChat(group, chatGroup)
	tx.insertMessage(msgRow{Chat: group, ID: "1", TS: 1, Kind: msgText, Text: "a", Sender: chat, SenderName: "push name"})

	new(Store).renameAll(context.Background(), tx, cli)
	if n := count(t, tx, "SELECT count(*) FROM chats WHERE name = 'push name'"); n != 1 {
		t.Fatalf("no book yet, but the chat was renamed (%d with its push name)", n)
	}

	phoneBook.set([]bookEntry{{"333 1234567", "Book Anna"}})
	new(Store).renameAll(context.Background(), tx, cli)
	if n := count(t, tx, "SELECT count(*) FROM chats WHERE jid = ? AND name = 'Book Anna'", chat); n != 1 {
		t.Error("the chat didn't take the book name")
	}
	if n := count(t, tx, "SELECT count(*) FROM messages WHERE sender = ? AND sender_name = 'Book Anna'", chat); n != 1 {
		t.Error("the group sender didn't take the book name")
	}
}
