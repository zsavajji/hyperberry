package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/proto/waCommon"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
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

	// Not kept: our own posts, one a day old, and what is no text, photo or video.
	own := status(pia, "S4", 0, text)
	own.Info.IsFromMe = true
	feed(own)
	feed(status(pia, "S5", storyLife*time.Second+time.Minute, photo))
	feed(status(pia, "S6", 0, &waE2E.Message{StickerMessage: &waE2E.StickerMessage{}}))
	if n("1") != 3 {
		t.Errorf("%d stories after the ones to drop, want 3", n("1"))
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
	st.runOp(func(*storeTx) {})
	if b, _ := os.ReadFile(p); string(b) != "mp4!" || n("id = 'S2' AND media_state = 2 AND path = ?", p) != 1 {
		t.Errorf("the file holds %q, or its row doesn't say where", b)
	}

	// The poster deletes one.
	feed(status(pia, "R1", 0, &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Type: waE2E.ProtocolMessage_REVOKE.Enum(), Key: &waCommon.MessageKey{ID: proto.String("S2")}}}))
	if _, err := os.Stat(p); n("id = 'S2'") != 0 || n("1") != 2 || !os.IsNotExist(err) {
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
