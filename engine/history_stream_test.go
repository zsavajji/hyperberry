package main

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waAdv"
	"github.com/polymorfa/hypermeow/proto/waCommon"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/proto/waHistorySync"
	"github.com/polymorfa/hypermeow/proto/waWeb"
	"github.com/polymorfa/hypermeow/store/sqlstore"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	waLog "github.com/polymorfa/hypermeow/util/log"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

const (
	testConvs    = 160
	testMsgs     = 400
	testPushname = 500
)

func testConvID(i int) string { return fmt.Sprintf("39333%07d@s.whatsapp.net", i) }

func testMsgID(i, j int) string { return fmt.Sprintf("3EB0%08X%08X%012X", i, j, i*testMsgs+j) }

// testText is 120 bytes.
func testText(i, j int) string {
	return fmt.Sprintf("Message %d of chat %d: %s", j, i, strings.Repeat("lorem ipsum ", 12))[:120]
}

// testConv is a chat of testMsgs text messages.
func testConv(i int) *waHistorySync.Conversation {
	c := &waHistorySync.Conversation{ID: proto.String(testConvID(i)), UnreadCount: proto.Uint32(2)}
	for j := 0; j < testMsgs; j++ {
		c.Messages = append(c.Messages, &waHistorySync.HistorySyncMsg{Message: &waWeb.WebMessageInfo{
			Key:              &waCommon.MessageKey{RemoteJID: proto.String(testConvID(i)), ID: proto.String(testMsgID(i, j)), FromMe: proto.Bool(j%3 == 0)},
			Message:          &waE2E.Message{Conversation: proto.String(testText(i, j))},
			MessageTimestamp: proto.Uint64(uint64(1700000000 + i*1000 + j)),
			PushName:         proto.String("Giorgia Conti"),
			Participant:      proto.String(testConvID(i)),
		}})
	}
	return c
}

// testBlob writes a FULL history sync of testConvs chats and testPushname push names to a zlib
// file, a chat at a time, so the test's own heap stays small. It returns the file and the size
// of the blob inflated.
func testBlob(t *testing.T) (string, int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "h.z")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zlib.NewWriter(f)
	n := 0
	put := func(m *waHistorySync.HistorySync) {
		b, err := proto.MarshalOptions{AllowPartial: true}.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		zw.Write(b)
		n += len(b)
	}
	// Fields in number order, as proto.Marshal writes them.
	put(&waHistorySync.HistorySync{SyncType: waHistorySync.HistorySync_FULL.Enum()})
	for i := 0; i < testConvs; i++ {
		put(&waHistorySync.HistorySync{Conversations: []*waHistorySync.Conversation{testConv(i)}})
	}
	rest := &waHistorySync.HistorySync{ChunkOrder: proto.Uint32(3), Progress: proto.Uint32(40)}
	for i := 0; i < testPushname; i++ {
		rest.Pushnames = append(rest.Pushnames, &waHistorySync.Pushname{ID: proto.String(testConvID(i)), Pushname: proto.String(fmt.Sprintf("Person %d", i))})
	}
	put(rest)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return path, n
}

func heapInuse() uint64 {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ms.HeapInuse
}

// decodeChat does what the engine does with one chat's raw bytes: compress them with the header
// as a blob of their own and have them parsed, as hypermeow's DownloadHistorySync does.
func decodeChat(header, raw []byte) (*waHistorySync.HistorySync, error) {
	zr, err := zlib.NewReader(bytes.NewReader(zlibBytes(header, raw)))
	if err != nil {
		return nil, err
	}
	b, err := io.ReadAll(zr)
	if err != nil {
		return nil, err
	}
	hs := new(waHistorySync.HistorySync)
	return hs, proto.Unmarshal(b, hs)
}

// A 15 MB history comes out chat by chat, each whole and in order, with the rest apart, and the
// heap never holds more than a chat or two beyond what it did before.
func TestHistoryStream(t *testing.T) {
	path, inflated := testBlob(t)
	if inflated < 15<<20 {
		t.Fatalf("the test blob is %d bytes inflated, want 15 MB or more", inflated)
	}
	fi, _ := os.Stat(path)
	t.Logf("blob: %d bytes inflated, %d compressed", inflated, fi.Size())

	// With the default pacing the heap may double before a collection, which the test would
	// measure as the stream's: a low percentage shows what is held, as the engine's memory limit does.
	defer debug.SetGCPercent(debug.SetGCPercent(20))
	runtime.GC()
	base := heapInuse()

	// Pass 1.
	var rest, header []byte
	err := scanHistory(path, func(n protowire.Number) bool { return n != hsConversations }, func(n protowire.Number, raw []byte) error {
		rest = append(rest, raw...)
		if n == hsSyncType || n == hsChunkOrder || n == hsProgress {
			header = append(header, raw...)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	hs0 := new(waHistorySync.HistorySync)
	if err := proto.Unmarshal(rest, hs0); err != nil {
		t.Fatal(err)
	}
	if len(hs0.Conversations) != 0 || len(hs0.Pushnames) != testPushname || hs0.GetSyncType() != waHistorySync.HistorySync_FULL ||
		hs0.GetChunkOrder() != 3 || hs0.GetProgress() != 40 || hs0.Pushnames[7].GetPushname() != "Person 7" {
		t.Errorf("the rest: %d chats, %d push names, %v chunk %d progress %d", len(hs0.Conversations), len(hs0.Pushnames),
			hs0.GetSyncType(), hs0.GetChunkOrder(), hs0.GetProgress())
	}
	hs0 = nil

	// Pass 2.
	peak, chats := uint64(0), 0
	err = scanHistory(path, func(n protowire.Number) bool { return n == hsConversations }, func(_ protowire.Number, raw []byte) error {
		hs, err := decodeChat(header, raw)
		if err != nil {
			return err
		}
		if h := heapInuse(); h > peak {
			peak = h
		}
		convs := hs.GetConversations()
		if len(convs) != 1 || convs[0].GetID() != testConvID(chats) || len(convs[0].Messages) != testMsgs {
			return fmt.Errorf("chat %d: wrong chat, or %d of them", chats, len(convs))
		}
		last := convs[0].Messages[testMsgs-1].GetMessage()
		if last.GetKey().GetID() != testMsgID(chats, testMsgs-1) || last.GetMessage().GetConversation() != testText(chats, testMsgs-1) {
			return fmt.Errorf("chat %d: last message %v", chats, last)
		}
		if hs.GetSyncType() != waHistorySync.HistorySync_FULL || hs.GetChunkOrder() != 3 || hs.GetProgress() != 40 {
			return fmt.Errorf("chat %d: header %v %d %d", chats, hs.GetSyncType(), hs.GetChunkOrder(), hs.GetProgress())
		}
		chats++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if chats != testConvs {
		t.Fatalf("%d chats, want %d", chats, testConvs)
	}
	t.Logf("per chat: baseline heap %d KB, peak %d KB, %d KB over", base>>10, peak>>10, (peak-base)>>10)
	if peak > base+6<<20 {
		t.Errorf("the heap peaked %d KB over the baseline, want under 6144", (peak-base)>>10)
	}

	// For comparison: the whole blob inflated and decoded at once, as hypermeow does.
	runtime.GC()
	base = heapInuse()
	f, _ := os.Open(path)
	zr, _ := zlib.NewReader(f)
	all, err := io.ReadAll(zr)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	whole := new(waHistorySync.HistorySync)
	if err := proto.Unmarshal(all, whole); err != nil {
		t.Fatal(err)
	}
	both := heapInuse() - base
	all = nil
	runtime.GC()
	t.Logf("whole: %d KB over the baseline with the blob and the decoded, %d KB with only the decoded", both>>10, (heapInuse()-base)>>10)
	runtime.KeepAlive(whole)
}

// zfile writes b as a zlib file.
func zfile(t *testing.T, b []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "x.z")
	if err := os.WriteFile(path, zlibBytes(b), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestScanHistoryEdges(t *testing.T) {
	all := func(protowire.Number) bool { return true }
	scan := func(path string) (nums []protowire.Number, sizes []int, err error) {
		err = scanHistory(path, all, func(n protowire.Number, raw []byte) error {
			nums, sizes = append(nums, n), append(sizes, len(raw))
			return nil
		})
		return
	}
	// One of each wire type, read exactly as it is.
	var b []byte
	b = protowire.AppendTag(b, 1, protowire.VarintType)
	b = protowire.AppendVarint(b, 300)
	b = protowire.AppendTag(b, 4, protowire.Fixed32Type)
	b = protowire.AppendFixed32(b, 7)
	b = protowire.AppendTag(b, 5, protowire.Fixed64Type)
	b = protowire.AppendFixed64(b, 7)
	b = protowire.AppendTag(b, 2, protowire.BytesType)
	b = protowire.AppendString(b, "abc")
	nums, sizes, err := scan(zfile(t, b))
	if fmt.Sprint(nums, sizes) != "[1 4 5 2] [3 5 9 5]" || err != nil {
		t.Errorf("fields %v sizes %v: %v", nums, sizes, err)
	}
	// An empty blob has no fields.
	if nums, _, err := scan(zfile(t, nil)); len(nums) != 0 || err != nil {
		t.Errorf("empty: %v, %v", nums, err)
	}
	// A group, and a blob cut inside a field, are errors, not a half answer.
	if _, _, err := scan(zfile(t, protowire.AppendTag(nil, 1, protowire.StartGroupType))); err == nil {
		t.Error("a group was taken")
	}
	if _, _, err := scan(zfile(t, b[:len(b)-1])); err == nil {
		t.Error("a field cut short was taken")
	}
	// The zlib stream cut short: the fields before it come, then the error.
	z := zlibBytes(b)
	cut := filepath.Join(t.TempDir(), "cut.z")
	os.WriteFile(cut, z[:len(z)-6], 0o600)
	if _, _, err := scan(cut); err == nil {
		t.Error("a cut zlib stream was taken")
	}
	// A chat over the cap is skipped unread and reported as nil; the field after it comes.
	defer func(m int) { histFieldMax = m }(histFieldMax)
	histFieldMax = 4
	big := protowire.AppendTag(nil, 2, protowire.BytesType)
	big = protowire.AppendString(big, "0123456789")
	big = append(big, b[:3]...)
	var got []string
	err = scanHistory(zfile(t, big), all, func(n protowire.Number, raw []byte) error {
		got = append(got, fmt.Sprint(n, raw == nil))
		return nil
	})
	if fmt.Sprint(got) != "[2 true 1 false]" || err != nil {
		t.Errorf("over the cap: %v, %v", got, err)
	}
}

// historyFixture is a Store with a linked-looking client, so a chunk can go through the queue.
// Nothing connects: a receipt fails and is only logged.
func historyFixture(t *testing.T) (*Store, *whatsmeow.Client) {
	t.Helper()
	ts := newTestServer(t)
	db, err := openChats(filepath.Join(t.TempDir(), "chats.db"))
	if err != nil {
		t.Fatal(err)
	}
	st := newStore(db, ts.Server, waLog.Noop)
	t.Cleanup(func() { st.close(); db.Close() })
	container, err := sqlstore.New(context.Background(), "sqlite3", "file:"+t.TempDir()+"/s.db?_foreign_keys=on", waLog.Noop)
	if err != nil {
		t.Fatal(err)
	}
	dev := container.NewDevice()
	me := types.NewJID("4915700000009", types.DefaultUserServer)
	// The device table wants these columns filled, and of the right length.
	dev.ID, dev.Account = &me, &waAdv.ADVSignedDeviceIdentity{Details: []byte{1}, AccountSignature: make([]byte, 64),
		AccountSignatureKey: make([]byte, 32), DeviceSignature: make([]byte, 64)}
	if err := dev.Save(context.Background()); err != nil {
		t.Fatal(err)
	}
	return st, whatsmeow.NewClient(dev, waLog.Noop)
}

// quiet waits until the queue has nothing queued or being worked.
func (q *historyQueue) quiet(t *testing.T) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		q.mu.Lock()
		n := len(q.pending)
		q.mu.Unlock()
		if n == 0 {
			return
		}
	}
	t.Fatal("the history queue is still busy")
}

func (q *historyQueue) files(t *testing.T) string {
	t.Helper()
	ents, _ := os.ReadDir(q.dir)
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	return fmt.Sprint(names)
}

// A chunk goes in chat by chat, tells the session its recent chats are in, and leaves no file.
func TestHistoryChunk(t *testing.T) {
	st, cli := historyFixture(t)
	in := make(chan struct{}, 4)
	st.historyIn = func() { in <- struct{}{} }
	hs := &waHistorySync.HistorySync{SyncType: waHistorySync.HistorySync_RECENT.Enum(), ChunkOrder: proto.Uint32(1), Progress: proto.Uint32(50)}
	for i := 0; i < 3; i++ {
		c := testConv(i)
		c.Messages = c.Messages[:5]
		hs.Conversations = append(hs.Conversations, c)
	}
	// Ours (j % 3 == 0) carry the phone's ticks: read, delivered.
	hs.Conversations[0].Messages[0].Message.Status = waWeb.WebMessageInfo_READ.Enum()
	hs.Conversations[0].Messages[3].Message.Status = waWeb.WebMessageInfo_DELIVERY_ACK.Enum()
	b, err := proto.Marshal(hs)
	if err != nil {
		t.Fatal(err)
	}
	notif := &waE2E.HistorySyncNotification{InitialHistBootstrapInlinePayload: zlibBytes(b)}
	// As hypermeow dispatches it: a message from our own phone, in the chat with ourselves.
	me := types.NewJID("4915700000009", types.DefaultUserServer)
	st.handle(cli, &events.Message{
		Info:    types.MessageInfo{ID: "CHUNK1", MessageSource: types.MessageSource{Chat: me, Sender: me, IsFromMe: true}},
		Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{HistorySyncNotification: notif}},
	})
	select {
	case <-in:
	case <-time.After(5 * time.Second):
		t.Fatal("historyIn never came")
	}
	st.hist.quiet(t)
	for i := 0; i < 3; i++ {
		eventually(t, st, 5, "SELECT count(*) FROM messages WHERE chat = ?", testConvID(i))
	}
	for j, want := range map[int]int{0: stateRead, 3: stateDelivered} {
		if got := rows(st, "SELECT state FROM messages WHERE chat = ? AND id = ?", testConvID(0), testMsgID(0, j)); got != want {
			t.Errorf("message %d: state %d, want %d", j, got, want)
		}
	}
	if f := st.hist.files(t); f != "[]" {
		t.Errorf("files left: %s", f)
	}
	// The notification wrote nothing itself: no chat with ourselves.
	if n := rows(st, "SELECT count(*) FROM chats"); n != 3 {
		t.Errorf("%d chats, want 3", n)
	}
}

// A chunk that fails is tried again on the next Connected, once; and when it is queued already
// a reconnect doesn't count as a try.
func TestHistoryResume(t *testing.T) {
	st, cli := historyFixture(t)
	q := st.hist
	// No inline payload and no path to download from: the chunk cannot succeed.
	q.add(cli, "BAD", &waE2E.HistorySyncNotification{FileLength: proto.Uint64(9)}, false)
	q.quiet(t)
	if f := q.files(t); f != "[BAD.notif]" {
		t.Fatalf("after the failure: %s", f)
	}
	q.resume(cli) // the retry
	q.quiet(t)
	if f := q.files(t); f != "[BAD.notif BAD.retry]" {
		t.Fatalf("after the retry: %s", f)
	}
	q.resume(cli) // dropped
	q.quiet(t)
	if f := q.files(t); f != "[]" {
		t.Fatalf("after the second retry: %s", f)
	}
	// A file that isn't a notification is dropped too.
	os.MkdirAll(q.dir, 0o700)
	os.WriteFile(q.path("JUNK", ".notif"), []byte{0xff, 0xff}, 0o600)
	q.resume(cli)
	if f := q.files(t); f != "[]" {
		t.Errorf("a junk notification: %s", f)
	}
}
