package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"
)

const retryChat = "4915700000007@s.whatsapp.net"

// Every kind of send, made while the connection is down as its handler writes the row, is rebuilt from that row alone: the
// wire messages of the plain kinds equal the first ones, the files the rows name are the engine's own copies, and a restart's
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

	// A media send that waited works from the engine's copy, which is the row's path; msg.cancel removes it.
	for id, name := range map[string]string{"P1": "p.jpg", "V1": "v.mp4", "A1": "a.mp3", "D1": "d.pdf", "F1": "f.jpg"} {
		row, _ := st.row(chat, id)
		got, err := os.ReadFile(row.Path)
		if row.Path != filepath.Join(outboxDir(s.srv.dir), id+filepath.Ext(name)) || err != nil || string(got) != "data of "+name {
			t.Errorf("%s: path %q, %q, %v", id, row.Path, got, err)
		}
	}
	d, _ := st.row(chat, "D1")
	if _, err := s.handleCancel(json.RawMessage(`{"chat":"` + chat + `","id":"D1"}`)); err != nil {
		t.Fatal(err)
	}
	waitGone(t, d.Path) // deleteMessage's, once its transaction has committed
	if _, err := os.Stat(wavPath); !os.IsNotExist(err) {
		t.Errorf("the WAV of the voice note is still there: %v", err)
	}

	// The engine restarts: a session with nothing queued flushes the rows. All 11 are sent again (down: queued, in order).
	s2 := &Session{cli: s.cli, store: st, srv: st.srv, log: s.log}
	var ids []string
	for _, row := range st.queuedRows() {
		if r := s2.flushRow(cli, row); !strings.HasPrefix(r, "down again") {
			t.Errorf("%s: %q", row.ID, r)
		}
		ids = append(ids, row.ID)
	}
	if len(ids) != 11 || len(s2.queue.items) != 11 {
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
	if len(s3.queue.items) != 9 {
		t.Errorf("%d queued, want the 9 that could be rebuilt", len(s3.queue.items))
	}

	// A busy id (a handler has it) is left to its handler.
	s3.sending.Store("T1", true)
	row, _ := st.row(chat, "T1")
	if r := s3.flushRow(cli, row); r != "" {
		t.Errorf("a held row: %q", r)
	}
}

// msg.retry: a failed row of ours goes back to sending and is sent again; the others are refused.
func TestRetry(t *testing.T) {
	s, st := offlineSession(t)
	chat := retryChat
	st.runOp(func(tx *storeTx) {
		tx.ensureChat(chat, chatUser)
		for _, m := range []msgRow{
			{Chat: chat, ID: "BAD", TS: 1, Out: true, State: stateFailed, Kind: msgText, Text: "failed text"},
			{Chat: chat, ID: "GONE", TS: 2, Out: true, State: stateFailed, Kind: msgPhoto, Path: "/nonexistent/p.jpg"},
			{Chat: chat, ID: "SENT", TS: 3, Out: true, State: stateSent, Kind: msgText, Text: "sent"},
			{Chat: chat, ID: "THEIRS", TS: 4, State: stateFailed, Kind: msgText, Text: "theirs"},
		} {
			tx.insertMessage(m)
		}
		tx.setLast(chat, 2, "x", true, stateFailed)
	})
	retry := func(id string) (any, error) {
		return s.handleRetry(json.RawMessage(`{"chat":"` + chat + `","id":"` + id + `"}`))
	}
	for id, want := range map[string]string{"NOPE": "not_found", "SENT": "not_failed", "THEIRS": "not_failed", "GONE": "cannot_retry"} {
		if _, err := retry(id); code(err) != want {
			t.Errorf("retry %s: %v, want %s", id, err, want)
		}
	}
	if _, err := s.handleRetry(json.RawMessage(`{"chat":"` + chat + `"}`)); code(err) != "bad_request" {
		t.Errorf("no id: %v", err)
	}
	if g, _ := st.row(chat, "GONE"); g.State != stateFailed {
		t.Errorf("an unrebuildable row is %d, want it left failed", g.State)
	}

	r, err := retry("BAD")
	if err != nil || r.(map[string]string)["id"] != "BAD" || r.(map[string]string)["queued"] != "true" {
		t.Fatalf("retry of a failed text with no connection: %v, %v", r, err)
	}
	waitQueued(t, s, 1)
	if b, _ := st.row(chat, "BAD"); b.State != stateSending || b.Text != "failed text" {
		t.Errorf("the row after retry: %+v", b)
	}
	if _, held := s.sending.Load("BAD"); !held {
		t.Error("its id isn't held")
	}
	if _, err := retry("BAD"); code(err) != "not_failed" { // it is sending now
		t.Errorf("a second retry: %v", err)
	}
}

// A video's poster (the UI's thumbnail, which the row doesn't keep) waits in outbox-engine/<id>.poster, and a rebuild from
// the row carries it; it goes with the send being sent, cancelled, or unable to be rebuilt.
func TestVideoPosterKept(t *testing.T) {
	s, st := offlineSession(t)
	chat := retryChat
	mp4 := filepath.Join(t.TempDir(), "v.mp4")
	os.WriteFile(mp4, []byte("video"), 0o644)
	st.runOp(func(tx *storeTx) { tx.ensureChat(chat, chatUser) })
	for _, id := range []string{"V1", "V2", "V4"} {
		body := `{"chat":"` + chat + `","id":"` + id + `","path":"` + mp4 + `","thumb":"Bw=="}`
		if _, err := s.handleMediaSend(json.RawMessage(body)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.handleMediaSend(json.RawMessage(`{"chat":"` + chat + `","id":"P1","path":"` + mp4 + `","file":true,"thumb":"Bw=="}`)); err != nil {
		t.Fatal(err)
	}
	waitQueued(t, s, 4)
	if _, err := os.Stat(posterPath(s.srv.dir, "P1")); err == nil {
		t.Error("a document kept a poster")
	}
	row, _ := st.row(chat, "V1")
	o := s.mediaOf(row)
	if got, _ := os.ReadFile(posterPath(s.srv.dir, "V1")); string(got) != "\x07" || string(o.poster) != "\x07" ||
		string(mediaMessage(o, whatsmeow.UploadResponse{}, nil).GetVideoMessage().GetJPEGThumbnail()) != "\x07" {
		t.Errorf("the poster: file %q, rebuilt %q", got, o.poster)
	}
	if row.Thumb != nil {
		t.Errorf("the row keeps a thumb: %v", row.Thumb)
	}

	// Sent: the op that records it takes the poster.
	st.runOp(func(tx *storeTx) { s.dropSendFiles(tx, o, o.row.Path) })
	waitGone(t, posterPath(s.srv.dir, "V1"))
	// Cancelled.
	if _, err := s.handleCancel(json.RawMessage(`{"chat":"` + chat + `","id":"V2"}`)); err != nil {
		t.Fatal(err)
	}
	waitGone(t, posterPath(s.srv.dir, "V2"))
	// Unable to be rebuilt (its file is gone): a restart's flush fails the row and drops the poster.
	row, _ = st.row(chat, "V4")
	os.Remove(row.Path)
	s2 := &Session{cli: s.cli, store: st, srv: st.srv, log: s.log}
	if r := s2.flushRow(s.client(), row); !strings.HasPrefix(r, "failed, can't be rebuilt") {
		t.Errorf("flush: %q", r)
	}
	waitGone(t, posterPath(s.srv.dir, "V4"))
}

// Once a media send is sent, the row's path goes back to the original and the copies go (the engine's, the UI outbox's, or
// both: a copy of a copy), while the original is there; without it the copy stays, as the only file the bubble can show.
func TestSentSendDropsItsCopies(t *testing.T) {
	s, st := offlineSession(t)
	chat, dir := retryChat, s.srv.dir
	tmp := t.TempDir()
	st.runOp(func(tx *storeTx) { tx.ensureChat(chat, chatUser) })
	write := func(p string) string {
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("x"), 0o644)
		return p
	}
	for _, c := range []struct {
		id      string
		ui, eng bool   // the UI outbox's copy, the engine's (made from the UI's if both)
		orig    string // the original: "here", "gone", "cache" (here, under cache/), or "" for a rebuild, which doesn't know it
		kept    bool   // the copies are still there afterwards
	}{
		{"A", false, true, "here", false}, // a send that waited
		{"B", true, true, "here", false},  // the UI's copy, then the engine's
		{"C", true, false, "here", false}, // the UI's copy, sent at once
		{"D", true, true, "gone", true},
		{"E", false, true, "", true},
		{"F", false, false, "here", true}, // sent at once from the original: nothing to drop
		{"G", true, true, "cache", true},  // the cache doesn't last: the copy stays
	} {
		orig := filepath.Join(tmp, c.id+".jpg")
		if c.orig == "cache" {
			orig = filepath.Join(dir, "cache", "convert", c.id+".jpg")
		}
		o := &mediaOut{row: msgRow{Chat: chat, ID: c.id, Out: true, State: stateSent, Kind: msgPhoto, Path: orig}}
		if c.orig != "" {
			o.original = orig
		}
		if c.orig == "here" || c.orig == "cache" {
			write(orig)
		}
		from, copies := orig, []string{}
		if c.ui {
			from = write(filepath.Join(dir, "outbox", c.id, "p.jpg"))
			o.row.Path, copies = from, append(copies, from)
		}
		if c.eng {
			from = write(filepath.Join(outboxDir(dir), c.id+".jpg"))
			copies = append(copies, from)
		}
		st.runOp(func(tx *storeTx) {
			tx.insertMessage(msgRow{Chat: chat, ID: c.id, TS: 1, Out: true, State: stateSending, Kind: msgPhoto, Path: from})
			s.dropSendFiles(tx, o, from)
		})
		done := make(chan struct{})
		st.afterCommit(func() { close(done) })
		<-done
		row, _ := st.row(chat, c.id)
		if want := map[bool]string{true: from, false: orig}[c.kept]; row.Path != want {
			t.Errorf("%s: path %q, want %q", c.id, row.Path, want)
		}
		for _, p := range copies {
			if exists(p) != c.kept {
				t.Errorf("%s: %s is there: %v", c.id, p, !c.kept)
			}
		}
		if c.ui && !c.kept && exists(filepath.Join(dir, "outbox", c.id)) {
			t.Errorf("%s: the UI's directory is still there", c.id)
		}
		if (c.orig == "here" || c.orig == "cache") && !exists(orig) {
			t.Errorf("%s: the original went", c.id)
		}
	}
	if _, err := s.handleMediaSend(json.RawMessage(`{"chat":"` + chat + `","path":"/x","original":"rel"}`)); code(err) != "bad_request" {
		t.Errorf("a relative original: %v", err)
	}
}

// A delivery or read receipt for one of our rows that is still sending, or failed, says the message went: the row moves to
// delivered or read, and isSending (which every attempt asks right before sending) turns false, so nothing sends it again.
func TestReceiptMovesUnsentRows(t *testing.T) {
	st, cli := historyFixture(t)
	chat := retryChat
	jid, _ := types.ParseJID(chat)
	st.runOp(func(tx *storeTx) {
		tx.ensureChat(chat, chatUser)
		for i, m := range []msgRow{
			{ID: "SND", Out: true, State: stateSending, Kind: msgText, Text: "sending"},
			{ID: "FLD", Out: true, State: stateFailed, Kind: msgText, Text: "failed"},
			{ID: "FLD2", Out: true, State: stateFailed, Kind: msgText, Text: "failed, read"},
			{ID: "FLD3", Out: true, State: stateFailed, Kind: msgText, Text: "failed, sent on the phone"},
			{ID: "CAN", Out: true, State: stateSending, Kind: msgText, Text: "cancelled"},
			{ID: "REV", Out: true, State: stateSending, Kind: msgText, Text: "revoked", Deleted: true},
			{ID: "THEIRS", State: stateSending, Kind: msgText, Text: "theirs"},
		} {
			m.Chat, m.TS = chat, int64(10+i)
			tx.insertMessage(m)
		}
	})
	for _, id := range []string{"SND", "CAN"} {
		if !st.isSending(chat, id) {
			t.Errorf("%s is not sending", id)
		}
	}
	for _, id := range []string{"FLD", "REV", "THEIRS", "GONE"} {
		if st.isSending(chat, id) {
			t.Errorf("%s is sending", id)
		}
	}
	feed := func(typ types.ReceiptType, ids ...string) {
		st.handle(cli, &events.Receipt{MessageSource: types.MessageSource{Chat: jid, Sender: jid}, MessageIDs: ids, Type: typ})
		st.runOp(func(*storeTx) {})
	}
	feed(types.ReceiptTypeDelivered, "SND", "FLD")
	feed(types.ReceiptTypeRead, "FLD2")
	st.runOp(func(tx *storeTx) { tx.setState(chat, "FLD3", stateSent) }) // the phone's history says sent: not enough
	st.runOp(func(tx *storeTx) { tx.deleteMessage(chat, "CAN") })       // msg.cancel
	for id, want := range map[string]int{"SND": stateDelivered, "FLD": stateDelivered, "FLD2": stateRead, "FLD3": stateFailed} {
		if row, _ := st.row(chat, id); row.State != want {
			t.Errorf("%s is %d, want %d", id, row.State, want)
		}
	}
	for _, id := range []string{"SND", "CAN"} {
		if st.isSending(chat, id) {
			t.Errorf("%s is still to be sent: a receipt moved it, or it was cancelled", id)
		}
	}
}

// settle waits until what the store was given has committed and its after-commit work (file removals) has run.
func settle(st *Store) {
	done := make(chan struct{})
	st.afterCommit(func() { close(done) })
	<-done
}

// A send made with an original keeps it in the row's extra, next to what else is there, so a row rebuilt after a restart
// (mediaOf, from chats.db alone) drops its copies once it is sent, and the path goes back to the original. The key goes
// when the path is the original again.
func TestRebuiltSendDropsItsCopies(t *testing.T) {
	s, st := offlineSession(t)
	chat, dir, tmp := retryChat, s.srv.dir, t.TempDir()
	st.runOp(func(tx *storeTx) { tx.ensureChat(chat, chatUser) })
	write := func(p string) string {
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("x"), 0o644)
		return p
	}
	if got := withOriginal(`{"gif":true}`, `/a "b"/c.jpg`); got != `{"gif":true,"original":"/a \"b\"/c.jpg"}` {
		t.Errorf("withOriginal keeps the other keys: %s", got)
	}
	// A send from the UI outbox (its copy, and the file it came from), one from a file the engine copies itself.
	uiOrig, engOrig := write(filepath.Join(tmp, "ui.jpg")), write(filepath.Join(tmp, "eng.jpg"))
	uiCopy := write(filepath.Join(dir, "outbox", "U1", "ui.jpg"))
	for _, body := range []string{
		`{"chat":"` + chat + `","id":"U1","path":"` + uiCopy + `","original":"` + uiOrig + `"}`,
		`{"chat":"` + chat + `","id":"E1","path":"` + engOrig + `"}`,
	} {
		if _, err := s.handleMediaSend(json.RawMessage(body)); err != nil {
			t.Fatal(err)
		}
	}
	waitQueued(t, s, 2)
	settle(st)
	for id, orig := range map[string]string{"U1": uiOrig, "E1": engOrig} {
		row, _ := st.row(chat, id)
		var x struct{ Original string }
		json.Unmarshal([]byte(row.Extra), &x)
		if row.Path != filepath.Join(outboxDir(dir), id+".jpg") || x.Original != orig {
			t.Errorf("%s: path %q, extra %q; want the engine's copy and the original %q", id, row.Path, row.Extra, orig)
		}
	}

	// After a restart: a session that has only the rows. The send went; the copies go, the path is the original again.
	s2 := &Session{cli: s.cli, store: st, srv: st.srv, log: s.log}
	for id, orig := range map[string]string{"U1": uiOrig, "E1": engOrig} {
		row, _ := st.row(chat, id)
		o := s2.mediaOf(row)
		if o.original != orig {
			t.Fatalf("%s: rebuilt original %q, want %q", id, o.original, orig)
		}
		st.runOp(func(tx *storeTx) { s2.dropSendFiles(tx, o, row.Path) })
		settle(st)
		after, _ := st.row(chat, id)
		if after.Path != orig || strings.Contains(after.Extra, "original") {
			t.Errorf("%s: path %q, extra %q after the send", id, after.Path, after.Extra)
		}
		if exists(row.Path) || exists(filepath.Join(dir, "outbox", id)) || !exists(orig) {
			t.Errorf("%s: the engine's copy %v, the UI's directory %v, the original %v", id, exists(row.Path),
				exists(filepath.Join(dir, "outbox", id)), exists(orig))
		}
	}

	// With the original gone the copy stays, and so does the key: it is still what the row is a copy of.
	gone := write(filepath.Join(tmp, "gone.jpg"))
	if _, err := s.handleMediaSend(json.RawMessage(`{"chat":"` + chat + `","id":"G1","path":"` + gone + `"}`)); err != nil {
		t.Fatal(err)
	}
	waitQueued(t, s, 3)
	settle(st)
	os.Remove(gone)
	row, _ := st.row(chat, "G1")
	o := s2.mediaOf(row) // not in the op: it reads the quoted row through the writer
	st.runOp(func(tx *storeTx) { s2.dropSendFiles(tx, o, row.Path) })
	settle(st)
	if after, _ := st.row(chat, "G1"); after.Path != row.Path || !exists(row.Path) || !strings.Contains(after.Extra, gone) {
		t.Errorf("original gone: path %q, extra %q, the copy %v", after.Path, after.Extra, exists(row.Path))
	}
}

// Deleting one of our rows (for me, clear chat, delete chat, msg.cancel) takes the files of its send that are ours alone: the
// copy in outbox-engine/, the UI outbox's outbox/<id>/ (whether or not the row's path is in it), the video's poster, a voice
// note's OGG. A file elsewhere is the user's and stays, and so does one another row has too.
func TestDeleteDropsSendFiles(t *testing.T) {
	s, st := offlineSession(t)
	chat, other, dir, tmp := retryChat, "4915700000008@s.whatsapp.net", s.srv.dir, t.TempDir()
	write := func(p string) string {
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("x"), 0o644)
		return p
	}
	user := write(filepath.Join(tmp, "mine.jpg"))
	ui := func(id string) string { return filepath.Join(dir, "outbox", id) }
	type row struct {
		id, path string
		kind     int
		out      bool
	}
	rowsOf := []row{
		{"F1", filepath.Join(outboxDir(dir), "F1.mp4"), msgVideo, true},  // the engine's copy, the UI's too, a poster
		{"F2", filepath.Join(ui("F2"), "p.jpg"), msgPhoto, true},         // the UI's copy
		{"F3", user, msgVideo, true},                                     // the user's own file, and a poster
		{"F4", "", msgVoice, true},                                       // an OGG
		{"F5", filepath.Join(outboxDir(dir), "F5.jpg"), msgPhoto, true},  // a forward in another chat has it too
		{"F6", filepath.Join(outboxDir(dir), "F6.jpg"), msgPhoto, false}, // not ours
		{"F7", filepath.Join(ui("F8"), "x.jpg"), msgPhoto, true},         // another message's directory
	}
	ours := []string{rowsOf[0].path, ui("F1"), posterPath(dir, "F1"), ui("F2"), posterPath(dir, "F3"), oggPath(dir, "F4")} // all gone
	kept := []string{user, rowsOf[4].path, rowsOf[5].path, rowsOf[6].path}
	build := func() {
		for _, r := range rowsOf {
			if r.path != "" && r.path != user {
				write(r.path)
			}
		}
		write(filepath.Join(ui("F1"), "p.mp4"))
		write(posterPath(dir, "F1"))
		write(posterPath(dir, "F3"))
		write(oggPath(dir, "F4"))
		st.runOp(func(tx *storeTx) {
			tx.ensureChat(chat, chatUser)
			tx.ensureChat(other, chatUser)
			for i, r := range rowsOf {
				tx.insertMessage(msgRow{Chat: chat, ID: r.id, TS: int64(i + 1), Out: r.out, State: stateFailed, Kind: r.kind, Path: r.path})
			}
			tx.insertMessage(msgRow{Chat: other, ID: "FW5", TS: 1, Out: true, State: stateSent, Kind: msgPhoto, Path: rowsOf[4].path})
		})
	}
	check := func(t *testing.T, how string) {
		t.Helper()
		settle(st)
		for _, p := range ours {
			if exists(p) {
				t.Errorf("%s: %s is still there", how, p)
			}
		}
		for _, p := range kept {
			if !exists(p) {
				t.Errorf("%s: %s went, and isn't ours alone", how, p)
			}
		}
		if n := rows(st, "SELECT count(*) FROM messages WHERE chat = ?", chat); n != 0 {
			t.Errorf("%s: %d rows left", how, n)
		}
	}

	// Delete for me: the handler tells our other devices too, which fails offline and is only logged.
	build()
	var ids []string
	for _, r := range rowsOf {
		ids = append(ids, `"`+r.id+`"`)
	}
	if _, err := s.handleDelete(json.RawMessage(`{"chat":"` + chat + `","ids":[` + strings.Join(ids, ",") + `]}`)); err != nil {
		t.Fatal(err)
	}
	check(t, "delete for me")

	// Clear chat and delete chat.
	for _, drop := range []bool{false, true} {
		build()
		st.runOp(func(tx *storeTx) { tx.clearChat(chat, drop) })
		check(t, map[bool]string{false: "clear chat", true: "delete chat"}[drop])
	}
}
