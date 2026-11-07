package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/types"
	"github.com/purpshell/meowcaller"
)

func TestPCMFraming(t *testing.T) {
	pcm := make([]byte, frameBytes)
	for i := 0; i < meowcaller.FrameSamples; i++ {
		binary.LittleEndian.PutUint16(pcm[2*i:], uint16(int16(i*60-28000)))
	}
	f := pcmToFloat(pcm)
	if len(f) != meowcaller.FrameSamples || f[0] != -28000.0/32768 || f[1] != -27940.0/32768 {
		t.Fatalf("pcmToFloat: %d samples, %v %v", len(f), f[0], f[1])
	}
	if string(floatToPCM(f)) != string(pcm) {
		t.Error("s16le -> float32 -> s16le is not the same")
	}
	// Out of range clips instead of wrapping.
	b := floatToPCM([]float32{2, -2})
	if int16(binary.LittleEndian.Uint16(b)) != 32767 || int16(binary.LittleEndian.Uint16(b[2:])) != -32768 {
		t.Errorf("clip: %v", b)
	}
}

func TestMicQueue(t *testing.T) {
	m := &micSource{}
	f, err := m.ReadFrame()
	if err != nil || len(f) != meowcaller.FrameSamples || f[0] != 0 {
		t.Fatalf("empty queue: %d samples, %v", len(f), err)
	}
	for i := 1; i <= 5; i++ { // the UI is ahead: only the last 3 are kept
		m.push([]float32{float32(i)})
	}
	for _, want := range []float32{3, 4, 5} {
		if f, err := m.ReadFrame(); err != nil || f[0] != want {
			t.Fatalf("want %v, got %v %v", want, f, err)
		}
	}
	if f, err := m.ReadFrame(); err != nil || len(f) != meowcaller.FrameSamples {
		t.Errorf("silence again, never io.EOF: %d %v", len(f), err)
	}
}

// audioHello opens an audio link and gives the reply.
func (r *callRig) audioHello(t *testing.T, token, id string) (*conn, map[string]any) {
	t.Helper()
	c := r.ts.dial(t)
	c.send(t, fmt.Sprintf(`{"@":"a","cmd":"hello","token":%q,"proto":1,"audio":%q}`, token, id))
	return c, c.read(t)
}

func TestAudioLink(t *testing.T) {
	r := newCallRig(t)
	old := audioGrace
	audioGrace = 150 * time.Millisecond
	defer func() { audioGrace = old }()

	// No call yet: not_found, and closed.
	c, m := r.audioHello(t, r.ts.token, "L1")
	if errCode(m) != "not_found" {
		t.Errorf("no call: %v", m)
	}
	c.wantEOF(t, "no call")

	f := r.fake("L1")
	r.c.offer(f)
	r.want(t, "ringing", "")
	if c, m := r.audioHello(t, "wrong", "L1"); errCode(m) != "auth" {
		t.Errorf("bad token: %v", m)
	} else {
		c.wantEOF(t, "bad token")
	}
	if c, m := r.audioHello(t, r.ts.token, "other"); errCode(m) != "not_found" {
		t.Errorf("wrong id: %v", m)
	} else {
		c.wantEOF(t, "wrong id")
	}

	link, m := r.audioHello(t, r.ts.token, "L1")
	if m["ok"] != true {
		t.Fatalf("good audio hello: %v", m)
	}
	// UI -> engine: the microphone, right behind the hello (nothing was read ahead).
	pcm := make([]byte, frameBytes)
	binary.LittleEndian.PutUint16(pcm, uint16(int16(16384)))
	link.Write(pcm)
	var got []float32
	for i := 0; i < 200 && got == nil || len(got) > 0 && got[0] == 0 && i < 200; i++ {
		time.Sleep(5 * time.Millisecond)
		got, _ = f.src.ReadFrame()
	}
	if got[0] != 0.5 {
		t.Errorf("microphone frame: %v", got[0])
	}
	// engine -> UI: the peer.
	peer := make([]float32, meowcaller.FrameSamples)
	peer[0] = -0.5
	f.sink.WriteFrame(peer)
	buf := make([]byte, frameBytes)
	link.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(link.r, buf); err != nil || int16(binary.LittleEndian.Uint16(buf)) != -16384 {
		t.Errorf("peer frame: %v %v", err, buf[:2])
	}

	// A new link replaces the old one, which closes; the call goes on.
	link2, m := r.audioHello(t, r.ts.token, "L1")
	if m["ok"] != true {
		t.Fatalf("second link: %v", m)
	}
	link.wantEOF(t, "replaced link")
	time.Sleep(3 * audioGrace)
	if f.count("hangup") != 0 {
		t.Fatal("the call hung up though a link replaced the old")
	}

	// The link closing with no other within the grace: hung up.
	link2.Close()
	for i := 0; f.count("hangup") == 0 && i < 200; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if f.count("hangup") != 1 {
		t.Fatal("the call wasn't hung up")
	}
	r.want(t, "ended", "hangup")
}

func TestAudioLinkClosesWithTheCall(t *testing.T) {
	r := newCallRig(t)
	f := r.fake("L2")
	r.c.offer(f)
	r.want(t, "ringing", "")
	link, m := r.audioHello(t, r.ts.token, "L2")
	if m["ok"] != true {
		t.Fatal(m)
	}
	f.end("")
	link.wantEOF(t, "link at the end")
}

func TestCallRowsStayOffWhatsApp(t *testing.T) {
	r := newCallRig(t)
	f := r.fake("W1")
	r.c.offer(f)
	r.want(t, "ringing", "")
	f.end("")
	r.row(t, "W1") // flushed
	jid := types.NewJID("111", types.HiddenUserServer)

	if _, err := r.st.reactable(annaKey, "call-W1"); err == nil || err.(*Error).Code != "not_found" {
		t.Errorf("reactable (react, resend, vote): %v", err)
	}
	// Even a sent outgoing call row isn't deletable for everyone.
	r.st.ops <- func(tx *storeTx) {
		tx.insertMessage(msgRow{Chat: annaKey, ID: "call-W2", TS: 5, Out: true, State: stateSent, Kind: msgNotice, Text: "call"})
		tx.insertMessage(msgRow{Chat: annaKey, ID: "real", TS: 4, Out: false, State: stateRead, Kind: msgText, Text: "hi"})
	}
	if _, err := r.st.deletable(annaKey, []string{"call-W2"}, true); err == nil || err.(*Error).Code != "not_ours" {
		t.Errorf("delete for everyone: %v", err)
	}
	if rows, err := r.st.deletable(annaKey, []string{"call-W2"}, false); err != nil || len(rows) != 1 {
		t.Errorf("delete for me stays local: %v %v", rows, err)
	}

	// chat.read: the receipt names no call row, whatever the unread count.
	var unread []unreadMsg
	r.st.runOp(func(tx *storeTx) { unread = tx.unreadIncoming(annaKey) })
	for _, u := range unread {
		if isCallID(u.ID) {
			t.Errorf("read receipt for %s", u.ID)
		}
	}
	// The app-state patches of archive and mark-unread end at a real message.
	if ts, key := r.st.lastKey(annaKey); key == nil || key.GetID() != "real" || ts.Unix() != 4 {
		t.Errorf("lastKey: %v %v", ts, key)
	}
	// A reply to a call row goes out unquoted.
	q, _ := r.st.queueRow(jid, annaKey, "call-W2", msgRow{Chat: annaKey, ID: "m1", TS: 6, Out: true, Kind: msgText, Text: "x"})
	if q.ID != "" {
		t.Errorf("quoted a call row: %v", q)
	}
}
