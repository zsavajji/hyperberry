package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// decodedSecs is how long an OGG on disk plays, by decoding it.
func decodedSecs(t *testing.T, path string) float64 {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pcm, err := decodeOpus(b)
	if err != nil {
		t.Fatal(err)
	}
	return float64(len(pcm)) / 48000
}

func TestSimSeededVoiceNotes(t *testing.T) {
	s, st, _ := linkedSim(t)
	for _, c := range []struct {
		chat, id string
		secs     int
	}{{simChats[1].jid, "SIMH1.11", 7}, {simChats[4].jid, "SIMSEED4", 23}} {
		row, ok := st.row(c.chat, c.id)
		if !ok || row.Kind != msgVoice || row.Duration != c.secs || len(row.Waveform) != 64 || row.MediaState != 2 || row.Played {
			t.Fatalf("%s: %+v", c.id, row)
		}
		if got := decodedSecs(t, oggPath(s.srv.dir, c.id)); got < float64(c.secs)-0.1 || got > float64(c.secs)+0.1 {
			t.Errorf("%s decodes to %.2f s, want %d", c.id, got, c.secs)
		}
	}
	if n := rows(st, "SELECT count(*) FROM chats WHERE jid = ? AND last_preview = 'Luca: 🎤 0:23'", simChats[4].jid); n != 1 {
		t.Error("Band practice's preview isn't 'Luca: 🎤 0:23'")
	}
}

func TestSimVoicePlay(t *testing.T) {
	s, st, _ := linkedSim(t)
	chat, play := simChats[1].jid, `{"chat":"`+simChats[1].jid+`","id":"SIMH1.11"}`
	r, err := s.handleVoicePlay([]byte(play))
	if err != nil {
		t.Fatal(err)
	}
	path := r.(map[string]string)["path"]
	if !filepath.IsAbs(path) || filepath.Ext(path) != ".wav" {
		t.Fatalf("path %q", path)
	}
	b, _ := os.ReadFile(path)
	pcm, info, err := readWAV(b)
	if err != nil || info.rate != 48000 || len(pcm) < 48000*69/10 || len(pcm) > 48000*71/10 {
		t.Fatalf("wav: %d samples, %v, %v", len(pcm), info, err)
	}
	eventually(t, st, 1, "SELECT played FROM messages WHERE chat = ? AND id = 'SIMH1.11'", chat)

	// A second play reuses the file.
	old := time.Unix(1e9, 0)
	os.Chtimes(path, old, old)
	if _, err := s.handleVoicePlay([]byte(play)); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); !fi.ModTime().Equal(old) {
		t.Error("the second play wrote the WAV again")
	}

	if _, err := s.handleVoicePlay([]byte(`{"chat":"` + chat + `","id":"nope"}`)); code(err) != "not_found" {
		t.Errorf("unknown id: %v", err)
	}
	if _, err := s.handleVoicePlay([]byte(`{"chat":"` + chat + `","id":"SIMH1.0"}`)); code(err) != "not_found" {
		t.Errorf("a text message: %v", err)
	}
	if _, err := s.handleVoicePlay([]byte(`{"chat":"` + chat + `"}`)); code(err) != "bad_request" {
		t.Errorf("no id: %v", err)
	}
}

func TestSimVoiceLive(t *testing.T) {
	s, st, _ := linkedSim(t)
	d := simDownload
	simDownload = 300 * time.Millisecond
	t.Cleanup(func() { simDownload = d })
	chat := simChats[2].jid
	if _, err := s.handleVoice([]byte(`{"n":2,"seconds":3}`)); err != nil {
		t.Fatal(err)
	}
	q := "SELECT count(*) FROM messages WHERE chat = ? AND kind = 3 AND duration = 3 AND length(waveform) = 64 AND media_state = ?"
	eventually(t, st, 1, q, chat, 1)
	var id string
	st.db.QueryRow("SELECT id FROM messages WHERE chat = ? AND kind = 3 AND duration = 3", chat).Scan(&id)
	if _, err := os.Stat(oggPath(s.srv.dir, id)); err == nil {
		t.Error("the OGG is here before the download")
	}
	eventually(t, st, 1, q, chat, 2)
	if got := decodedSecs(t, oggPath(s.srv.dir, id)); got < 2.9 || got > 3.1 {
		t.Errorf("decodes to %.2f s", got)
	}
}
