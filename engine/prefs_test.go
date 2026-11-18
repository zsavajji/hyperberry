package main

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestPrefsAutoDownload(t *testing.T) {
	dir := t.TempDir()
	n := newNotifier(dir, waLog.Noop)
	kinds := []int{msgPhoto, msgVoice, msgAudio, msgVideo, msgDoc, msgSticker, msgText, msgLocation}
	check := func(when string, want ...bool) {
		t.Helper()
		for i, k := range kinds {
			if got := n.autoDownload(k); got != want[i] {
				t.Errorf("%s: kind %d downloads %v", when, k, got)
			}
		}
	}
	check("defaults", true, true, true, false, false, true, false, false)
	// Only the new fields are a request of their own, and each moves its own category.
	if _, err := n.setPrefs([]byte(`{"autoVideo":true,"autoDoc":true}`)); err != nil {
		t.Fatal(err)
	}
	check("video and doc on", true, true, true, true, true, true, false, false)
	if _, err := n.setPrefs([]byte(`{"autoPhoto":false,"autoAudio":false,"autoVideo":false}`)); err != nil {
		t.Fatal(err)
	}
	check("photo, audio and video off", false, false, false, false, true, true, false, false)
	if _, err := n.setPrefs([]byte(`{"autoPhoto":true}`)); err != nil {
		t.Fatal(err)
	}
	check("photo on again", true, false, false, false, true, true, false, false)
	n.close()
	again := newNotifier(dir, waLog.Noop) // what a restart reads
	defer again.close()
	if !again.autoDownload(msgPhoto) || again.autoDownload(msgVoice) || !again.autoDownload(msgDoc) {
		t.Errorf("after a restart: %+v", again.prefs)
	}
	if _, err := again.setPrefs([]byte(`{}`)); err == nil {
		t.Error("an empty prefs.set: no error")
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

func TestPrefsCacheLimit(t *testing.T) {
	dir := t.TempDir()
	n := newNotifier(dir, waLog.Noop)
	defer n.close()
	if got := n.cacheLimit(); got != 64<<20 {
		t.Errorf("default: %d", got)
	}
	if _, err := n.setPrefs([]byte(`{"cacheMB":512}`)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"63", "513", "0", "-1"} {
		if _, err := n.setPrefs([]byte(`{"cacheMB":` + bad + `}`)); err == nil {
			t.Errorf("cacheMB %s: no error", bad)
		}
	}
	if got := n.cacheLimit(); got != 512<<20 {
		t.Errorf("after refusals: %d", got)
	}
	again := newNotifier(dir, waLog.Noop) // what a restart reads
	defer again.close()
	if got := again.cacheLimit(); got != 512<<20 {
		t.Errorf("after a restart: %d", got)
	}
}

// A limit that comes down takes effect at once, with no decode or restart to wait for.
func TestPrefsLowerLimitPrunes(t *testing.T) {
	_, st, _, _ := hubSim(t)
	big := filepath.Join(st.srv.dir, "cache", "voice", "big.wav")
	if err := os.MkdirAll(filepath.Dir(big), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	f.Truncate(65 << 20) // sparse: only its size matters
	f.Close()
	if _, err := st.handlePrefs([]byte(`{"cacheMB":512}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.handlePrefs([]byte(`{"cacheMB":64}`)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		if !exists(big) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("the file over the new limit is still there")
}

func TestPrefsCloudflareDNS(t *testing.T) {
	t.Cleanup(func() { useCloudflareDNS(false) })
	dir := t.TempDir()
	n := newNotifier(dir, waLog.Noop)
	if net.DefaultResolver != systemResolver {
		t.Error("Cloudflare before it was set")
	}
	n.setPrefs([]byte(`{"cloudflareDNS":true}`))
	if net.DefaultResolver != cloudflareResolver {
		t.Error("prefs.set didn't switch to Cloudflare")
	}
	n.close()
	useCloudflareDNS(false)
	n = newNotifier(dir, waLog.Noop) // the stored choice at start
	defer n.close()
	if net.DefaultResolver != cloudflareResolver {
		t.Error("not Cloudflare after a restart")
	}
	n.setPrefs([]byte(`{"cloudflareDNS":false}`))
	if net.DefaultResolver != systemResolver {
		t.Error("prefs.set didn't switch back")
	}
}
