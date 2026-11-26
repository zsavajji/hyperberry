package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
)

const retryChat = "4915700000007@s.whatsapp.net"

// Every kind of send, made while the connection is down as its handler writes the row, is rebuilt from that row alone: the
// wire messages of the plain kinds equal the first ones, and a restart's
// flush (a session with an empty queue) sends them all again, failing the ones that can't be rebuilt.
func TestResendEveryKind(t *testing.T) {
	s, st := offlineSession(t)
	dir := t.TempDir()
	chat := retryChat
	src := mediaInfo{DirectPath: "/v/x", MediaKey: []byte{1}, FileLength: 9, Mimetype: "image/jpeg"}
	media, _ := json.Marshal(src)
	st.runOp(func(tx *storeTx) {
		tx.ensureChat(chat, chatUser)
		tx.insertMessage(msgRow{Chat: chat, ID: "Q1", TS: 10, Kind: msgText, Text: "the question", State: stateRead})
		tx.insertMessage(msgRow{Chat: chat, ID: "SRC", TS: 11, Out: true, State: stateSent, Kind: msgPhoto, Text: "cap", Media: string(media), Thumb: []byte{7}})
		tx.insertStory(msgRow{Chat: chat, ID: "S1", TS: 12, Kind: msgText, Text: "a story"}, false)
		tx.setFave(hashOf(7), faveOf(7, "image/webp", true), 1)
	})
	file := func(name string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("data of "+name), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	wav, _ := stereoWAV(1)
	wavPath := filepath.Join(s.srv.dir, "rec.wav")
	os.WriteFile(wavPath, wav, 0o644)
	jpg, mp4, mp3, pdf, asFile := file("p.jpg"), file("v.mp4"), file("a.mp3"), file("d.pdf"), file("f.jpg")
	for cmd, h := range map[string]struct {
		do   func(json.RawMessage) (any, error)
		body string
	}{
		"text":    {s.handleSend, `{"chat":"%c","id":"T1","text":"hi","quote":"Q1"}`},
		"mention": {s.handleSend, `{"chat":"%c","id":"M1","text":"hi @Bo","mentions":[{"jid":"4915700000008@s.whatsapp.net","name":"Bo"}]}`},
		"photo":   {s.handleMediaSend, `{"chat":"%c","id":"P1","path":"` + jpg + `","caption":"look","width":8,"height":6,"thumb":"Bw=="}`},
		"video":   {s.handleMediaSend, `{"chat":"%c","id":"V1","path":"` + mp4 + `","caption":"clip","thumb":"Bw=="}`},
		"audio":   {s.handleMediaSend, `{"chat":"%c","id":"A1","path":"` + mp3 + `"}`},
		"doc":     {s.handleMediaSend, `{"chat":"%c","id":"D1","path":"` + pdf + `"}`},
		"asfile":  {s.handleMediaSend, `{"chat":"%c","id":"F1","path":"` + asFile + `","file":true}`},
		"voice":   {s.handleVoiceSend, `{"chat":"%c","id":"VN1","path":"` + wavPath + `"}`},
		"sticker": {s.handleStickerSend, `{"chat":"%c","id":"ST1","hash":"` + hashOf(7) + `","quote":"Q1"}`},
		"poll":    {s.handlePollCreate, `{"chat":"%c","id":"PO1","question":"Pizza?","options":["yes","no"],"multiple":true}`},
		"forward": {s.handleForward, `{"chat":"%c","id":"SRC","to":"%c","newId":"FW1"}`},
		"story":   {s.handleStoryReply, `{"poster":"%c","id":"S1","text":"nice","newId":"SR1"}`},
	} {
		if _, err := h.do(json.RawMessage(strings.ReplaceAll(h.body, "%c", chat))); err != nil {
			t.Fatalf("%s: %v", cmd, err)
		}
	}
	waitQueued(t, s, 12)
	for _, id := range []string{"T1", "M1", "P1", "V1", "A1", "D1", "F1", "VN1", "ST1", "PO1", "FW1", "SR1"} {
		row, ok := st.row(chat, id)
		if !ok || row.State != stateSending {
			t.Fatalf("%s: %+v", id, row)
		}
		if attempt, err := s.resendAttempt(row); err != nil || attempt == nil {
			t.Errorf("%s can't be rebuilt: %v", id, err)
		}
	}

	// The plain kinds' messages are the ones the paths build.
	cli := s.client()
	text, _ := st.row(chat, "T1")
	q, _ := st.row(chat, "Q1")
	if m, err := s.resendMessage(cli, text); err != nil || !proto.Equal(m, textMessage(cli, chat, "hi", q, nil)) || m.GetExtendedTextMessage().GetContextInfo().GetStanzaID() != "Q1" {
		t.Errorf("text with a quote: %v, %v", m, err)
	}
	men, _ := st.row(chat, "M1")
	ms := []mention{{"4915700000008@s.whatsapp.net", "Bo"}}
	if m, err := s.resendMessage(cli, men); err != nil || !proto.Equal(m, textMessage(cli, chat, "hi @Bo", msgRow{}, ms)) || m.GetExtendedTextMessage().GetText() != "hi @4915700000008" {
		t.Errorf("text with a mention: %v, %v", m, err)
	}
	fw, _ := st.row(chat, "FW1")
	want, _, _ := st.forwardCopy(forwardReq{Chat: chat, ID: "SRC", To: chat}, "FW1", 0)
	if m, err := s.resendMessage(cli, fw); err != nil || !proto.Equal(m, want) || m.GetImageMessage().GetContextInfo().GetForwardingScore() != 1 {
		t.Errorf("forward: %v, %v want %v", m, err, want)
	}
	sr, _ := st.row(chat, "SR1")
	if m, err := s.resendMessage(cli, sr); err != nil || !proto.Equal(m, storyReplyMessage(chat, "S1", "nice", quotedStory(msgText, "a story", nil))) {
		t.Errorf("story reply: %v, %v", m, err)
	}
	sr.QuoteID = "GONE" // the story went after 24 hours: the row's own line of it quotes
	sr.QuotePreview = "a story"
	if m, err := s.resendMessage(cli, sr); err != nil || m.GetExtendedTextMessage().GetContextInfo().GetQuotedMessage().GetConversation() != "a story" ||
		m.GetExtendedTextMessage().GetContextInfo().GetStanzaID() != "GONE" {
		t.Errorf("story reply, story gone: %v, %v", m, err)
	}
	po, _ := st.row(chat, "PO1")
	if m, err := s.resendMessage(cli, po); err != nil || m.GetPollCreationMessage().GetName() != "Pizza?" || len(m.GetPollCreationMessage().GetOptions()) != 2 ||
		m.GetPollCreationMessage().GetSelectableOptionsCount() != 0 || len(m.GetMessageContextInfo().GetMessageSecret()) != 32 {
		t.Errorf("poll: %v, %v", m, err)
	}

	// The engine restarts: a session with nothing queued flushes the rows. All 12 are sent again (down: queued, in order).
	s2 := &Session{cli: s.cli, store: st, srv: st.srv, log: s.log}
	var ids []string
	for _, row := range st.queuedRows() {
		if r := s2.flushRow(cli, row); !strings.HasPrefix(r, "down again") {
			t.Errorf("%s: %q", row.ID, r)
		}
		ids = append(ids, row.ID)
	}
	if len(ids) != 12 || len(s2.queue.items) != 12 {
		t.Fatalf("%d rows read, %d queued: %v", len(ids), len(s2.queue.items), ids)
	}
	if n := rows(st, "SELECT count(*) FROM messages WHERE out = 1 AND state = ?", stateFailed); n != 0 {
		t.Errorf("%d failed", n)
	}

	// What can't be rebuilt fails: a file that is gone, a recording that is gone, a kind we don't send. A sending row nobody
	// holds (not in s3's memory) is the flush's.
	p, _ := st.row(chat, "P1")
	os.Remove(p.Path)
	os.Remove(oggPath(s.srv.dir, "VN1"))
	st.runOp(func(tx *storeTx) {
		tx.insertMessage(msgRow{Chat: chat, ID: "LOC", TS: 99, Out: true, State: stateSending, Kind: msgLocation, Text: "here"})
	})
	s3 := &Session{cli: s.cli, store: st, srv: st.srv, log: s.log}
	for _, row := range st.queuedRows() {
		r := s3.flushRow(cli, row)
		failed := row.ID == "P1" || row.ID == "VN1" || row.ID == "LOC"
		if failed != strings.HasPrefix(r, "failed, can't be rebuilt") {
			t.Errorf("%s: %q", row.ID, r)
		}
	}
	for _, id := range []string{"P1", "VN1", "LOC"} {
		if row, _ := st.row(chat, id); row.State != stateFailed {
			t.Errorf("%s is %d, want failed", id, row.State)
		}
		if _, held := s3.sending.Load(id); held {
			t.Errorf("%s is still held", id)
		}
	}
	if len(s3.queue.items) != 10 {
		t.Errorf("%d queued, want the 10 that could be rebuilt", len(s3.queue.items))
	}

	// A busy id (a handler has it) is left to its handler.
	s3.sending.Store("T1", true)
	row, _ := st.row(chat, "T1")
	if r := s3.flushRow(cli, row); r != "" {
		t.Errorf("a held row: %q", r)
	}
}
