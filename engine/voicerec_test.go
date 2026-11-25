package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// stereoWAV is n seconds at 48 kHz, 16-bit stereo, as AudioRecorder writes it, with the sizes
// of a recording that isn't finished (0), and the mono mix of what it holds.
func stereoWAV(seconds int) (wav []byte, mono []int16) {
	n := seconds * 48000
	wav = make([]byte, 44, 44+4*n)
	copy(wav, "RIFF")
	copy(wav[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(wav[16:], 16)
	binary.LittleEndian.PutUint16(wav[20:], 1)
	binary.LittleEndian.PutUint16(wav[22:], 2)
	binary.LittleEndian.PutUint32(wav[24:], 48000)
	binary.LittleEndian.PutUint32(wav[28:], 48000*4)
	binary.LittleEndian.PutUint16(wav[32:], 4)
	binary.LittleEndian.PutUint16(wav[34:], 16)
	copy(wav[36:], "data")
	for i := 0; i < n; i++ {
		env := math.Abs(math.Sin(float64(i) / 20000))
		l := int16(9000 * env * math.Sin(2*math.Pi*220*float64(i)/48000))
		r := int16(7000 * math.Sin(2*math.Pi*330*float64(i)/48000))
		wav = binary.LittleEndian.AppendUint16(wav, uint16(l))
		wav = binary.LittleEndian.AppendUint16(wav, uint16(r))
		mono = append(mono, int16((int(l)+int(r))/2))
	}
	return
}

// recordTo appends wav to path in chunks of the given sizes, over and over, pausing between.
func recordTo(t *testing.T, path string, wav []byte, sizes []int, pause time.Duration) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for i := 0; len(wav) > 0; i++ {
		n := min(sizes[i%len(sizes)], len(wav))
		if _, err := f.Write(wav[:n]); err != nil {
			t.Fatal(err)
		}
		wav = wav[n:]
		time.Sleep(pause)
	}
}

// voiceSim is a linked sim with a fast poll and fast ticks.
func voiceSim(t *testing.T) (*Sim, *Store, string) {
	t.Helper()
	poll, played := voicePoll, simPlayed
	voicePoll, simPlayed = 5*time.Millisecond, 100*time.Millisecond
	t.Cleanup(func() { voicePoll, simPlayed = poll, played })
	s, st, _ := linkedSim(t)
	return s, st, s.srv.dir
}

func call(h Handler, format string, args ...any) (any, error) {
	return h([]byte(fmt.Sprintf(format, args...)))
}

func TestVoiceChunkedEqualsWhole(t *testing.T) {
	s, st, dir := voiceSim(t)
	chat, path := simChats[2].jid, filepath.Join(dir, "rec-1.wav")
	wav, mono := stereoWAV(10)
	want, err := encodeOpus(mono, 48000)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := call(s.handleVoiceStart, `{"chat":%q,"path":%q}`, chat, path); err != nil {
		t.Fatal(err)
	}
	// Splits inside the header, inside a stereo frame and on odd byte counts.
	recordTo(t, path, wav, []int{30, 14, 1001, 7, 40999, 333, 60001, 1, 12345}, 2*time.Millisecond)
	r, err := call(s.handleVoiceSend, `{"chat":%q,"path":%q}`, chat, path)
	if err != nil {
		t.Fatal(err)
	}
	id := r.(map[string]string)["id"]

	got, err := os.ReadFile(oggPath(dir, id))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("the chunked OGG differs from the whole one (%d and %d bytes): %v", len(got), len(want), err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("the WAV is still here")
	}
	row, ok := st.row(chat, id)
	if !ok || row.Kind != msgVoice || !row.Out || row.Duration != 10 || row.MediaState != 2 ||
		!bytes.Equal(row.Waveform, waveform(mono)) {
		t.Fatalf("row: %+v", row)
	}
	if n := rows(st, "SELECT count(*) FROM chats WHERE jid = ? AND last_preview = 'You: 🎤 0:10'", chat); n != 1 {
		t.Error("the chat's preview isn't 'You: 🎤 0:10'")
	}

	// The states, in order, then played.
	for _, q := range []string{"state = 0", "state = 1", "state = 2", "state = 3", "state = 3 AND played = 1"} {
		eventually(t, st, 1, "SELECT count(*) FROM messages WHERE chat = ? AND id = ? AND "+q, chat, id)
	}
}

// A WAV that was complete before voice.start, as when the recorder writes only at the end.
func TestVoiceFallbackWholeFile(t *testing.T) {
	s, _, dir := voiceSim(t)
	chat, path := simChats[2].jid, filepath.Join(dir, "rec-2.wav")
	wav, mono := stereoWAV(3)
	os.WriteFile(path, wav, 0o644)
	call(s.handleVoiceStart, `{"chat":%q,"path":%q}`, chat, path)
	r, err := call(s.handleVoiceSend, `{"chat":%q,"path":%q}`, chat, path)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := encodeOpus(mono, 48000)
	if got, _ := os.ReadFile(oggPath(dir, r.(map[string]string)["id"])); !bytes.Equal(got, want) {
		t.Error("the OGG differs from encodeOpus on the whole")
	}
}

// voice.send of a WAV no voice.start was made for, as when the UI recorded while the engine was down: the complete file is
// read whole, and the OGG, the length and the waveform are the same as from one followed as it grew.
func TestVoiceSendWithoutStart(t *testing.T) {
	s, st, dir := voiceSim(t)
	chat, path := simChats[2].jid, filepath.Join(dir, "rec-down.wav")
	wav, mono := stereoWAV(3)
	os.WriteFile(path, wav, 0o644)
	r, err := call(s.handleVoiceSend, `{"chat":%q,"path":%q}`, chat, path)
	if err != nil {
		t.Fatal(err)
	}
	id := r.(map[string]string)["id"]
	want, _ := encodeOpus(mono, 48000)
	if got, _ := os.ReadFile(oggPath(dir, id)); !bytes.Equal(got, want) {
		t.Error("the OGG differs from encodeOpus on the whole")
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("the WAV is still here")
	}
	if row, ok := st.row(chat, id); !ok || row.Kind != msgVoice || row.Duration != 3 || !bytes.Equal(row.Waveform, waveform(mono)) {
		t.Errorf("row: %+v", row)
	}
	// With another file's recording running, a complete WAV is still its own, and the running one is left be.
	other, mine := filepath.Join(dir, "rec-run.wav"), filepath.Join(dir, "rec-down2.wav")
	call(s.handleVoiceStart, `{"chat":%q,"path":%q}`, chat, other)
	os.WriteFile(mine, wav, 0o644)
	if _, err := call(s.handleVoiceSend, `{"chat":%q,"path":%q}`, chat, mine); err != nil {
		t.Fatal(err)
	}
	if cur := s.rec.cur; cur == nil || cur.path != other {
		t.Errorf("the running recording was taken: %+v", cur)
	}
}

func TestVoiceSendIsQuick(t *testing.T) {
	s, _, dir := voiceSim(t)
	chat, path := simChats[2].jid, filepath.Join(dir, "rec-3.wav")
	wav, _ := stereoWAV(60)
	call(s.handleVoiceStart, `{"chat":%q,"path":%q}`, chat, path)
	// A second of audio every 50 ms: faster than real time, as the file is written; what
	// counts is the time after the last write.
	recordTo(t, path, wav, []int{48000 * 4}, 50*time.Millisecond)
	start := time.Now()
	if _, err := call(s.handleVoiceSend, `{"chat":%q,"path":%q}`, chat, path); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("voice.send took %v", d)
	}
}

func TestVoiceCancelAndBadRequests(t *testing.T) {
	s, st, dir := voiceSim(t)
	chat, path := simChats[2].jid, filepath.Join(dir, "rec-4.wav")
	wav, _ := stereoWAV(1)
	call(s.handleVoiceStart, `{"chat":%q,"path":%q}`, chat, path)
	recordTo(t, path, wav, []int{50000}, 10*time.Millisecond)
	if _, err := call(s.handleVoiceCancel, `{"path":%q}`, path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("cancel left the WAV")
	}
	if _, err := call(s.handleVoiceSend, `{"chat":%q,"path":%q}`, chat, path); code(err) != "bad_request" {
		t.Errorf("send after cancel: %v", err)
	}
	if n := rows(st, "SELECT count(*) FROM messages WHERE out = 1 AND kind = ?", msgVoice); n != 0 {
		t.Errorf("%d voice rows after a cancel", n)
	}
	if _, err := call(s.handleVoiceCancel, `{"path":%q}`, path); err != nil {
		t.Errorf("cancel with nothing running: %v", err)
	}

	outside := filepath.Join(t.TempDir(), "x.wav")
	os.WriteFile(outside, wav, 0o644)
	for _, h := range []Handler{s.handleVoiceStart, s.handleVoiceSend, s.handleVoiceCancel} {
		for _, p := range []string{outside, filepath.Join(dir, "..", "x.wav"), filepath.Join(dir, "chats.db")} {
			if _, err := call(h, `{"chat":%q,"path":%q}`, chat, p); code(err) != "bad_request" {
				t.Errorf("path %s: %v", p, err)
			}
		}
	}
	if _, err := os.Stat(outside); err != nil {
		t.Error("a path outside the data dir was deleted")
	}
	if _, err := call(s.handleVoiceStart, `{"chat":"not a jid@@@","path":%q}`, path); code(err) != "bad_chat" {
		t.Errorf("bad chat: %v", err)
	}
}

// A second start cancels the first and deletes its WAV.
func TestVoiceStartCancelsTheOld(t *testing.T) {
	s, _, dir := voiceSim(t)
	chat, a, b := simChats[2].jid, filepath.Join(dir, "a.wav"), filepath.Join(dir, "b.wav")
	os.WriteFile(a, []byte("RIFF"), 0o644)
	call(s.handleVoiceStart, `{"chat":%q,"path":%q}`, chat, a)
	call(s.handleVoiceStart, `{"chat":%q,"path":%q}`, chat, b)
	if _, err := os.Stat(a); err == nil {
		t.Error("the old WAV is still here")
	}
	if _, err := call(s.handleVoiceSend, `{"chat":%q,"path":%q}`, chat, a); code(err) != "bad_request" {
		t.Errorf("send of the old one: %v", err)
	}
}
