package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	whatsmeow "github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waCommon"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	waLog "github.com/polymorfa/hypermeow/util/log"
	"google.golang.org/protobuf/proto"
)

// status is a message of status@broadcast from poster, sent age ago.
func status(poster types.JID, id string, age time.Duration, m *waE2E.Message) *events.Message {
	return &events.Message{Message: m, Info: types.MessageInfo{ID: id, PushName: "Pia",
		Timestamp:     time.Now().Add(-age),
		MessageSource: types.MessageSource{Chat: types.StatusBroadcastJID, Sender: poster, IsGroup: true}}}
}

func TestStories(t *testing.T) {
	st, cli := historyFixture(t)
	pia := types.NewJID("4915700000007", types.DefaultUserServer)
	k := pia.String()
	feed := func(e any) { st.handle(cli, e); st.runOp(func(*storeTx) {}) }
	n := func(where string, args ...any) int {
		return rows(st, "SELECT count(*) FROM stories WHERE "+where, args...)
	}

	photo := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("sunset"),
		JPEGThumbnail: []byte{1, 2, 3}, DirectPath: proto.String("/v/t/x"), MediaKey: []byte{4}, FileLength: proto.Uint64(900),
		Width: proto.Uint32(720), Height: proto.Uint32(1280), Mimetype: proto.String("image/jpeg")}}
	feed(status(pia, "S1", time.Hour, photo))
	if n("poster = ? AND id = 'S1' AND kind = ? AND text = 'sunset' AND name = 'Pia' AND media_state IN (1, 3) AND width = 720 AND seen = 0 AND thumb IS NOT NULL",
		k, msgPhoto) != 1 {
		t.Error("the photo story's row is wrong or missing")
	}
	// A story is no chat and no message: nothing in the list, nothing unread.
	if c, m := rows(st, "SELECT count(*) FROM chats"), rows(st, "SELECT count(*) FROM messages"); c+m != 0 {
		t.Errorf("%d chats and %d messages from a story, want none", c, m)
	}
	feed(status(pia, "S1", time.Hour, photo))
	if n("1") != 1 {
		t.Errorf("the same story twice: %d rows, want 1", n("1"))
	}

	video := &waE2E.Message{VideoMessage: &waE2E.VideoMessage{Seconds: proto.Uint32(12), DirectPath: proto.String("/v/t/y"), MediaKey: []byte{5}}}
	feed(status(pia, "S2", time.Minute, video))
	if n("id = 'S2' AND kind = ? AND duration = 12", msgVideo) != 1 {
		t.Error("the video story's row is wrong or missing")
	}

	text := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("hello"),
		BackgroundArgb: proto.Uint32(0xff112233), TextArgb: proto.Uint32(0xffffffff)}}
	feed(status(pia, "S3", 0, text))
	var extra string
	st.db.QueryRow("SELECT extra FROM stories WHERE id = 'S3' AND kind = ? AND text = 'hello'", msgText).Scan(&extra)
	if extra != `{"bg":4279312947,"fg":4294967295}` {
		t.Errorf("the text story's extra is %q", extra)
	}

	// Seen: news the first time only, and nothing for a story that isn't there.
	if !st.markStorySeen(k, "S3") || st.markStorySeen(k, "S3") || st.markStorySeen(k, "nope") || n("id = 'S3' AND seen = 1") != 1 {
		t.Error("marking a story seen: not once, or not stored")
	}

	// Ours, from our phone, are kept as "me" and born seen; one a day old and what is no text, photo or video are not.
	own := status(pia, "S4", 0, text)
	own.Info.IsFromMe = true
	feed(own)
	feed(status(pia, "S5", storyLife*time.Second+time.Minute, photo))
	feed(status(pia, "S6", 0, &waE2E.Message{StickerMessage: &waE2E.StickerMessage{}}))
	if n("poster = ? AND id = 'S4' AND seen = 1 AND name IS NULL", ownStory) != 1 || n("1") != 4 {
		t.Errorf("%d stories after the ones to drop, want 4 with ours as %q, seen", n("1"), ownStory)
	}

	// A video story is fetched into data/stories, once, and the file goes with its row.
	dir := t.TempDir()
	writes := 0
	get := func(f *os.File) (string, error) { writes++; _, err := f.WriteString("mp4!"); return "video/mp4", err }
	row, _ := st.storyRow(k, "S2")
	p, err := st.fetchStory(dir, row, get)
	if want := filepath.Join(dir, "stories", "S2.mp4"); err != nil || p != want {
		t.Fatalf("fetchStory: %q, %v, want %q", p, err, want)
	}
	if again, err := st.fetchStory(dir, row, get); err != nil || again != p || writes != 1 {
		t.Errorf("a second fetch: %q, %v after %d downloads, want the file again and 1 download", again, err, writes)
	}
	// No barrier here: fetchStory has to return with the row already saying where the file is, since its reply is
	// what the UI reads the row on.
	if b, _ := os.ReadFile(p); string(b) != "mp4!" || n("id = 'S2' AND media_state = 2 AND path = ?", p) != 1 {
		t.Errorf("the file holds %q, or its row doesn't say where", b)
	}

	// The poster deletes one.
	feed(status(pia, "R1", 0, &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Type: waE2E.ProtocolMessage_REVOKE.Enum(), Key: &waCommon.MessageKey{ID: proto.String("S2")}}}))
	if _, err := os.Stat(p); n("id = 'S2'") != 0 || n("1") != 3 || !os.IsNotExist(err) {
		t.Error("the revoked story is still there, or its file")
	}

	// One more video, posted 2 h ago, whose file the sweep takes too.
	feed(status(pia, "S7", 2*time.Hour, video))
	row7, _ := st.storyRow(k, "S7")
	p7, err := st.fetchStory(dir, row7, get)
	if err != nil {
		t.Fatal(err)
	}
	st.runOp(func(*storeTx) {})

	// A sweep an hour on drops the one posted an hour ago, 23 hours into its day, and keeps the others.
	st.runOp(func(tx *storeTx) { tx.expireStories(time.Now().Unix() + 23*3600 + 60) })
	if _, err := os.Stat(p7); n("id = 'S1'") != 0 || n("id = 'S7'") != 0 || n("id = 'S3'") != 1 || !os.IsNotExist(err) {
		t.Errorf("after the sweep: S1 %d, S7 %d, S3 %d, want 0, 0 and 1, and S7's file gone", n("id = 'S1'"), n("id = 'S7'"), n("id = 'S3'"))
	}

	// Unlinking drops them with the rest.
	st.clear()
	st.runOp(func(*storeTx) {})
	if n("1") != 0 {
		t.Errorf("%d stories after clear, want 0", n("1"))
	}
}

// A reply to a story: no session with the phone here, so the send fails, but our row is in the poster's chat
// first, quoting the story, and the quote that goes out names the status and carries its thumbnail.
func TestStoryReply(t *testing.T) {
	st, cli := historyFixture(t)
	s := &Session{cli: cli, store: st}
	pia := types.NewJID("4915700000007", types.DefaultUserServer)
	k := pia.String()
	st.handle(cli, status(pia, "S1", time.Minute, &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("sunset"),
		JPEGThumbnail: []byte{1, 2, 3}, DirectPath: proto.String("/v/t/x"), MediaKey: []byte{4}}}))
	st.runOp(func(*storeTx) {})

	bad := func(cmd func(json.RawMessage) (any, error), body, want string) {
		t.Helper()
		if _, err := cmd(json.RawMessage(body)); code(err) != want {
			t.Errorf("%s: %v, want %s", body, err, want)
		}
	}
	bad(s.handleStoryReply, `{"poster":"`+k+`","id":"S1","text":"  "}`, "bad_request")
	bad(s.handleStoryReply, `{"poster":"`+k+`","id":"nope","text":"hi"}`, "not_found")
	bad(s.handleStoryReact, `{"poster":"`+k+`","id":"S1"}`, "bad_request")
	bad(s.handleStoryReact, `{"poster":"`+k+`","id":"nope","emoji":"❤️"}`, "not_found")

	if _, err := s.handleStoryReply(json.RawMessage(`{"poster":"` + k + `","id":"S1","text":"nice one"}`)); err == nil {
		t.Fatal("a send with no connection went through")
	}
	var text, quote, preview string
	var state int
	st.db.QueryRow("SELECT text, quote_id, quote_preview, state FROM messages WHERE chat = ? AND out = 1", k).Scan(&text, &quote, &preview, &state)
	if text != "nice one" || quote != "S1" || preview != "Photo sunset" || state != stateFailed {
		t.Errorf("our row: %q quoting %q (%q), state %d", text, quote, preview, state)
	}

	m := quotedStory(msgPhoto, "sunset", []byte{1, 2, 3})
	if m.GetImageMessage().GetCaption() != "sunset" || len(m.GetImageMessage().GetJPEGThumbnail()) != 3 {
		t.Error("the quoted photo lacks its caption or thumbnail")
	}
	if quotedStory(msgText, "hello", nil).GetConversation() != "hello" || quotedStory(msgVideo, "", nil).GetVideoMessage() == nil {
		t.Error("the quoted text or video is wrong")
	}
}

// Posting: what the request refuses, and that a post which can't be sent (no connection here) leaves nothing: the
// row and the copy of the file go again, and a storyop event is the way the UI hears of it.
func TestStoryPost(t *testing.T) {
	st, cli := historyFixture(t)
	s := &Session{cli: cli, store: st, srv: st.srv, log: waLog.Noop}
	n := func(where string, args ...any) int {
		return rows(st, "SELECT count(*) FROM stories WHERE "+where, args...)
	}
	dir := t.TempDir()
	jpg := filepath.Join(dir, "a.jpg")
	os.WriteFile(jpg, []byte("not really a jpeg"), 0o644)
	pdf := filepath.Join(dir, "a.pdf")
	os.WriteFile(pdf, []byte("x"), 0o644)

	for body, want := range map[string]string{
		`{}`:                                   "bad_request", // neither
		`{"path":"` + jpg + `","text":"both"}`: "bad_request",
		`{"text":"  "}`:                        "bad_request",
		`{"path":"a.jpg"}`:                     "bad_request", // not absolute
		`{"path":"` + pdf + `"}`:               "bad_request", // not a photo or a video
		`{"path":"` + dir + `/none.jpg"}`:      "bad_request",
	} {
		if _, err := s.handleStoryPost(json.RawMessage(body)); code(err) != want {
			t.Errorf("%s: %v, want %s", body, err, want)
		}
	}

	// A photo: it is in at once with its own copy of the file, seen, as ours...
	res, err := s.handleStoryPost(json.RawMessage(`{"path":"` + jpg + `","caption":" hi ","width":4,"height":3}`))
	if err != nil {
		t.Fatal(err)
	}
	id := res.(map[string]string)["id"]
	copyPath := filepath.Join(st.srv.dir, "stories", id+".jpg")
	// ...and out again once the send has failed, copy and all.
	eventually(t, st, 0, "SELECT count(*) FROM stories WHERE id = ?", id)
	if _, err := os.Stat(copyPath); !os.IsNotExist(err) {
		t.Errorf("the copy of a story that wasn't posted is still there: %v", err)
	}

	// A text story the same way.
	res, err = s.handleStoryPost(json.RawMessage(`{"text":"hello","bg":"#112233","fg":"#FFFFFF"}`))
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, st, 0, "SELECT count(*) FROM stories WHERE id = ?", res.(map[string]string)["id"])

	// Delete: not ours, or not there, is refused; with no connection the revoke fails and the row stays.
	if _, err := s.handleStoryDelete(json.RawMessage(`{"id":"nope"}`)); code(err) != "not_found" {
		t.Errorf("deleting a story that isn't there: %v", err)
	}
	st.runOp(func(tx *storeTx) {
		tx.insertStory(msgRow{Chat: ownStory, ID: "O1", TS: time.Now().Unix(), Kind: msgText, Text: "mine"}, true)
	})
	if _, err := s.handleStoryDelete(json.RawMessage(`{"id":"O1"}`)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond) // the revoke is tried in the background and fails
	if n("id = 'O1'") != 1 {
		t.Error("a delete that was never sent took the row")
	}
}

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "a.bin"), filepath.Join(dir, "sub", "b.bin")
	os.WriteFile(src, []byte("bytes"), 0o644)
	if err := copyFile(src, dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "bytes" {
		t.Errorf("the copy holds %q", b)
	}
	if _, err := os.Stat(dst + ".tmp"); !os.IsNotExist(err) {
		t.Error("the temporary name is left behind")
	}
	if err := copyFile(filepath.Join(dir, "none"), dst+"2"); err == nil {
		t.Error("copying a file that isn't there worked")
	}
}

func TestStoryColour(t *testing.T) {
	for in, want := range map[string]uint32{"": 7, "#112233": 0xFF112233, "FFFFFF": 0xFFFFFFFF} {
		if got, ok := storyColour(in, 7); !ok || got != want {
			t.Errorf("%q: %x, %v, want %x", in, got, ok, want)
		}
	}
	for _, in := range []string{"blue", "#12345", "#1234567", "#GGGGGG"} {
		if _, ok := storyColour(in, 0); ok {
			t.Errorf("%q was taken as a colour", in)
		}
	}
}

// A video goes out with the UI's poster as its thumbnail (so the phones that get it show a video, not a grey box), a photo
// with its own thumbnail; our own row never keeps a video's poster.
func TestVideoPoster(t *testing.T) {
	up := whatsmeow.UploadResponse{}
	poster := []byte{9, 9, 9}
	v := mediaMessage(&mediaOut{row: msgRow{Kind: msgVideo, Text: "clip"}, mime: "video/mp4", poster: poster}, up, nil)
	if string(v.GetVideoMessage().GetJPEGThumbnail()) != string(poster) || v.GetVideoMessage().GetCaption() != "clip" {
		t.Error("the video message lacks the poster or its caption")
	}
	if p := mediaMessage(&mediaOut{row: msgRow{Kind: msgVideo}, mime: "video/mp4"}, up, nil); len(p.GetVideoMessage().GetJPEGThumbnail()) != 0 {
		t.Error("a video with no poster got a thumbnail")
	}
	img := mediaMessage(&mediaOut{row: msgRow{Kind: msgPhoto, Thumb: []byte{1}}, mime: "image/jpeg", poster: poster}, up, nil)
	if string(img.GetImageMessage().GetJPEGThumbnail()) != "\x01" {
		t.Error("a photo took the poster")
	}

	// media.send: the poster rides the message, our row has no thumbnail.
	st, _ := historyFixture(t)
	jid := types.NewJID("4915700000007", types.DefaultUserServer)
	o := st.queueMedia(jid, mediaSendReq{Chat: jid.String(), Path: "/x/clip.mp4", Thumb: poster}, "V1")
	var thumb []byte
	st.db.QueryRow("SELECT thumb FROM messages WHERE chat = ? AND id = 'V1'", jid.String()).Scan(&thumb)
	if string(o.poster) != string(poster) || len(thumb) != 0 {
		t.Errorf("poster %v, our row's thumbnail %v: want the poster on the message and none on the row", o.poster, thumb)
	}
}

// The people a status goes to: LID keys as phone numbers when we know them, each once, only persons.
func TestStatusRecipients(t *testing.T) {
	_, cli := historyFixture(t)
	ctx := context.Background()
	lid := types.NewJID("266396202233898", types.HiddenUserServer)
	pn := types.NewJID("4915700000007", types.DefaultUserServer)
	other := types.NewJID("4915700000008", types.DefaultUserServer)
	if err := cli.Store.LIDs.PutLIDMapping(ctx, lid, pn); err != nil {
		t.Fatal(err)
	}
	got, err := statusRecipients(ctx, cli, []string{lid.String(), pn.String(), other.String(), "4915700000009:12@s.whatsapp.net"})
	if err != nil || len(got) != 3 || got[0] != pn || got[1] != other || got[2] != types.NewJID("4915700000009", types.DefaultUserServer) {
		t.Errorf("got %v, %v, want the number once, the other and the third without its device", got, err)
	}
	unknown := types.NewJID("111", types.HiddenUserServer)
	if got, _ := statusRecipients(ctx, cli, []string{unknown.String()}); len(got) != 1 || got[0] != unknown {
		t.Errorf("a LID with no number: %v, want it as it is", got)
	}
	for _, bad := range []string{"", "nonsense", "120363431046871673@g.us", "status@broadcast"} {
		if _, err := statusRecipients(ctx, cli, []string{bad}); code(err) != "bad_request" {
			t.Errorf("%q: %v, want bad_request", bad, err)
		}
	}
}

// A reaction to a story of ours becomes a message from them quoting it; a reply to a story carries the story as its quote.
func TestStoryQuotes(t *testing.T) {
	st, cli := historyFixture(t)
	pia := types.NewJID("4915700000007", types.DefaultUserServer)
	k := pia.String()
	feed := func(e any) { st.handle(cli, e); st.runOp(func(*storeTx) {}) }
	st.runOp(func(tx *storeTx) {
		tx.insertStory(msgRow{Chat: ownStory, ID: "O1", TS: time.Now().Unix(), Kind: msgPhoto, Text: "sunset", Thumb: []byte{1, 2, 3}}, true)
	})
	react := func(id, storyID, emoji string) *events.Message {
		return status(pia, id, 0, &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{
			Key: &waCommon.MessageKey{ID: proto.String(storyID)}, Text: proto.String(emoji)}})
	}
	feed(react("RX1", "O1", "❤️"))
	var text, quote, qprev, extra string
	st.db.QueryRow("SELECT text, quote_id, quote_preview, extra FROM messages WHERE chat = ? AND id = 'RX1' AND out = 0", k).Scan(&text, &quote, &qprev, &extra)
	if text != "❤️" || quote != "O1" || qprev != "Photo sunset" || !strings.Contains(extra, `"sposter":"me"`) || !strings.Contains(extra, `"story":true`) ||
		!strings.Contains(extra, `"qthumb":"AQID"`) || rows(st, "SELECT unread FROM chats WHERE jid = ?", k) != 1 {
		t.Errorf("reaction row: %q quoting %q (%q) extra %s", text, quote, qprev, extra)
	}
	feed(react("RX2", "nope", "❤️")) // not one of ours
	feed(react("RX3", "O1", ""))     // a reaction taken back
	if n := rows(st, "SELECT count(*) FROM messages WHERE chat = ?", k); n != 1 {
		t.Errorf("%d messages after reactions that make none, want 1", n)
	}

	// A reply from them to our story, and ours to theirs.
	ci := &waE2E.ContextInfo{StanzaID: proto.String("O1"), RemoteJID: proto.String(types.StatusBroadcastJID.String()),
		QuotedMessage: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{JPEGThumbnail: []byte{4, 5}}}}
	feed(inbound(pia, "RP1", &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("nice"), ContextInfo: ci}}, ""))
	st.db.QueryRow("SELECT extra FROM messages WHERE chat = ? AND id = 'RP1'", k).Scan(&extra)
	if !strings.Contains(extra, `"sposter":"me"`) || !strings.Contains(extra, `"qthumb":"BAU="`) {
		t.Errorf("a reply to our story: extra %s", extra)
	}
	mine := inbound(pia, "RP2", &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("thanks"), ContextInfo: ci}}, "")
	mine.Info.IsFromMe = true
	feed(mine)
	st.db.QueryRow("SELECT extra FROM messages WHERE chat = ? AND id = 'RP2'", k).Scan(&extra)
	if !strings.Contains(extra, `"sposter":"`+k+`"`) {
		t.Errorf("our reply to their story: extra %s", extra)
	}
	if got := withStoryQuote("", "x", make([]byte, 30<<10)); strings.Contains(got, "qthumb") {
		t.Error("a thumbnail over 24 KB was kept")
	}
}
