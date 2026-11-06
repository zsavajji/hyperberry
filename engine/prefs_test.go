package main

import (
	"os"
	"path/filepath"
	"testing"

	waLog "github.com/polymorfa/hypermeow/util/log"
)

func TestPrefsOldFile(t *testing.T) {
	t.Cleanup(func() { setLanguage("") })
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "prefs.json"), []byte(`{"notifyPreview":false}`), 0o600)
	n := newNotifier(dir, waLog.Noop)
	defer n.close()
	if n.notifyPreview() || !n.mediaOnMobile() {
		t.Errorf("old file: preview %v, mobile %v", n.notifyPreview(), n.mediaOnMobile())
	}
}

func TestPrefsMerge(t *testing.T) {
	t.Cleanup(func() { setLanguage("") })
	dir := t.TempDir()
	n := newNotifier(dir, waLog.Noop)
	if ch, err := n.setPrefs([]byte(`{"language":"it_IT","notifyPreview":false}`)); err != nil || !ch {
		t.Fatalf("first language: %v %v", ch, err)
	}
	if ch, err := n.setPrefs([]byte(`{"language":"it_IT"}`)); err != nil || ch {
		t.Errorf("same language: %v %v", ch, err)
	}
	if ch, err := n.setPrefs([]byte(`{"mediaOnMobile":false}`)); err != nil || ch {
		t.Errorf("another field: %v %v", ch, err)
	}
	if ch, err := n.setPrefs([]byte(`{"language":"en_US"}`)); err != nil || !ch {
		t.Errorf("a new language: %v %v", ch, err)
	}
	n.close()
	n = newNotifier(dir, waLog.Noop) // all of it kept, language included
	defer n.close()
	if n.notifyPreview() || n.mediaOnMobile() || *n.prefs.Language != "en_US" {
		t.Errorf("reloaded %+v", n.prefs)
	}
	setLanguage("")
	n.setPrefs([]byte(`{"language":"it_IT"}`))
	if word("Photo") != "Foto" {
		t.Error("setPrefs didn't change the language")
	}
}

func TestRewritePreviews(t *testing.T) {
	t.Cleanup(func() { setLanguage("") })
	tx, _ := newTestTx(t)
	c := "a@s.whatsapp.net"
	tx.ensureChat(c, chatUser)
	tx.insertMessage(msgRow{Chat: c, ID: "1", TS: 1, Out: true, Kind: msgPhoto})
	tx.setLast(c, 1, "You: Photo", true, stateSent)
	tx.ensureChat("b@s.whatsapp.net", chatUser) // no message: no preview
	setLanguage("it_IT")
	tx.rewritePreviews()
	var p string
	tx.tx.QueryRow("SELECT last_preview FROM chats WHERE jid = ?", c).Scan(&p)
	if p != "Tu: Foto" {
		t.Errorf("preview %q", p)
	}
	if !tx.chats[c] {
		t.Error("the chat isn't marked changed")
	}
}
