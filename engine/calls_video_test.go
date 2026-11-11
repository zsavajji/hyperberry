package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/purpshell/meowcaller"
)

type sentAU struct {
	au []byte
	d  time.Duration
}

// The fake call's video side. A call offered as video starts with both directions on, as
// meowcaller's does.
func (f *fakeCall) IsVideo() bool          { f.mu.Lock(); defer f.mu.Unlock(); return f.ours || f.theirs }
func (f *fakeCall) IsSendingVideo() bool   { f.mu.Lock(); defer f.mu.Unlock(); return f.ours }
func (f *fakeCall) IsReceivingVideo() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.theirs }
func (f *fakeCall) StartVideo() error {
	f.did("startvideo")
	f.mu.Lock()
	f.ours = true
	f.mu.Unlock()
	return nil
}

// As meowcaller's: the peer's video accepted, ours left as it is.
func (f *fakeCall) AcceptVideo() error {
	f.did("acceptvideo")
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.acceptErr
}
func (f *fakeCall) StopVideo() error {
	f.did("stopvideo")
	f.mu.Lock()
	f.ours = false
	f.mu.Unlock()
	return nil
}
func (f *fakeCall) SetVideoOrientation(o int) error {
	f.mu.Lock()
	f.orients = append(f.orients, o)
	f.mu.Unlock()
	return nil
}
func (f *fakeCall) ReceiveVideo(s meowcaller.VideoSink)         { f.vsink = s }
func (f *fakeCall) OnVideoState(fn func(meowcaller.VideoState)) { f.onVS = fn }
func (f *fakeCall) OnVideoKeyframeRequest(fn func())            { f.onKey = fn }
func (f *fakeCall) SendVideoWithDuration(au []byte, d time.Duration) error {
	f.mu.Lock()
	f.sent = append(f.sent, sentAU{au, d})
	f.mu.Unlock()
	return nil
}
func (f *fakeCall) nsent() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.sent) }

func (f *fakeCall) videoOffer() *fakeCall { f.ours, f.theirs = true, true; return f }

func (r *callRig) raw(cmd, body string) error {
	_, err := r.ts.handlers[cmd]([]byte(body))
	return err
}

func wantVideo(t *testing.T, m map[string]any, video, ours, theirs, asked bool, orient float64) {
	t.Helper()
	if m["video"] != video || m["videoOurs"] != ours || m["videoTheirs"] != theirs || m["videoAsked"] != asked || m["orientation"] != orient {
		t.Fatalf("video fields: %v; want %v %v %v %v %v", m, video, ours, theirs, asked, orient)
	}
}

func TestVideoEvents(t *testing.T) {
	r := newCallRig(t)

	// An incoming video offer.
	f := r.fake("V1").videoOffer()
	r.c.offer(f)
	wantVideo(t, r.want(t, "ringing", ""), true, true, true, false, 0)
	// Answered with voice only: our video is stopped once the call is active.
	if err := r.raw("call.answer", `{"id":"V1","video":false}`); err != nil {
		t.Fatal(err)
	}
	r.want(t, "connecting", "")
	if f.count("stopvideo") != 0 {
		t.Fatal("stopped video before the call was active")
	}
	f.setPhase(meowcaller.CallPhaseActive)
	r.want(t, "active", "")
	m := r.ev(t) // our video went off
	wantVideo(t, m, true, false, true, false, 0)
	// The peer's orientation (from the RTP packets) and its video off and on.
	f.vsink.(meowcaller.VideoOrientationSink).SetOrientation(1)
	wantVideo(t, r.ev(t), true, false, true, false, 1)
	f.mu.Lock()
	f.theirs = false
	f.mu.Unlock()
	f.onVS(meowcaller.VideoState{Raw: 6})
	wantVideo(t, r.ev(t), true, false, false, false, 1)
	// The peer turns its video on: accepted by the engine at once, ours stays off, nobody is asked.
	f.onVS(meowcaller.VideoState{Upgrade: true, Raw: 11})
	for i := 0; f.count("acceptvideo") == 0; i++ {
		if i == 200 {
			t.Fatal("the peer's video was not accepted")
		}
		time.Sleep(5 * time.Millisecond)
	}
	f.mu.Lock()
	f.theirs = true
	f.mu.Unlock()
	f.onVS(meowcaller.VideoState{Active: true, Raw: 1, Orientation: 1})
	wantVideo(t, r.ev(t), true, false, true, false, 1)
	// A failed accept leaves it to the UI: asked, and call.video on accepts.
	f.mu.Lock()
	f.acceptErr = errors.New("no pending peer video upgrade")
	f.mu.Unlock()
	f.onVS(meowcaller.VideoState{Upgrade: true, Raw: 11})
	wantVideo(t, r.ev(t), true, false, true, true, 1)
	f.mu.Lock()
	f.acceptErr = nil
	f.mu.Unlock()
	if err := r.raw("call.video", `{"id":"V1","on":true}`); err != nil {
		t.Fatal(err)
	}
	wantVideo(t, r.ev(t), true, false, true, false, 1)
	if f.count("acceptvideo") != 3 || f.count("startvideo") != 0 {
		t.Errorf("calls: %v", f.calls)
	}
	// Ours on by our own start, off, and on again.
	r.raw("call.video", `{"id":"V1","on":true}`)
	wantVideo(t, r.ev(t), true, true, true, false, 1)
	r.raw("call.video", `{"id":"V1","on":false}`)
	wantVideo(t, r.ev(t), true, false, true, false, 1)
	r.raw("call.video", `{"id":"V1","on":true}`)
	wantVideo(t, r.ev(t), true, true, true, false, 1)
	if f.count("startvideo") != 2 {
		t.Errorf("calls: %v", f.calls)
	}
	if err := r.raw("call.video", `{"id":"nope","on":true}`); err == nil || err.(*Error).Code != "not_found" {
		t.Errorf("wrong id: %v", err)
	}
	// A late UI gets the fields with the event after its hello.
	late := r.ts.login(t)
	if m := late.read(t); m["ev"] != "call" || m["videoOurs"] != true || m["orientation"] != 1.0 {
		t.Errorf("hello event: %v", m)
	}
	f.end("hangup")
	r.want(t, "ended", "hangup")
	if text, _, _, _, _, ok := r.row(t, "V1"); !ok || text != "video_call" {
		t.Errorf("answered video call row: %q %v", text, ok)
	}
}

func TestVideoCallStartAndRows(t *testing.T) {
	r := newCallRig(t)
	var asked bool
	inner := r.c.dial
	r.c.dial = func(ctx context.Context, target string, video bool, members []string) (liveCall, error) {
		asked = video
		return inner(ctx, target, video, members)
	}
	f := r.fake("V2").videoOffer()
	f.phase = meowcaller.CallPhaseCalling
	r.next = f
	if err := r.raw("call.start", fmt.Sprintf(`{"chat":%q,"video":true}`, annaKey)); err != nil {
		t.Fatal(err)
	}
	if !asked {
		t.Error("dial wasn't told it is a video call")
	}
	wantVideo(t, r.want(t, "calling", ""), true, true, true, false, 0)
	f.end("rejected")
	r.want(t, "ended", "declined")
	if text, _, _, _, _, ok := r.row(t, "V2"); !ok || text != "video_call" {
		t.Errorf("unanswered video call out: %q %v", text, ok)
	}

	// Theirs and never answered: a missed video call, unread.
	f = r.fake("V3").videoOffer()
	r.c.offer(f)
	r.want(t, "ringing", "")
	f.end("")
	r.want(t, "ended", "hangup")
	if text, _, _, _, unread, ok := r.row(t, "V3"); !ok || text != "missed_video_call" || unread != 1 {
		t.Errorf("missed video call: %q unread=%d %v", text, unread, ok)
	}
	// A voice call still says call.
	f = r.fake("V4")
	r.c.offer(f)
	r.want(t, "ringing", "")
	f.end("")
	r.want(t, "ended", "hangup")
	if text, _, _, _, _, _ := r.row(t, "V4"); text != "missed_call" {
		t.Errorf("voice: %q", text)
	}
}

// videoHello opens a video link.
func (r *callRig) videoHello(t *testing.T, token, id string) (*conn, map[string]any) {
	t.Helper()
	c := r.ts.dial(t)
	c.send(t, fmt.Sprintf(`{"@":"v","cmd":"hello","token":%q,"proto":1,"video":%q}`, token, id))
	return c, c.read(t)
}

type vrec struct {
	typ, orient byte
	flags       uint16
	ts          int64
	payload     []byte
}

func readRec(t *testing.T, c *conn) vrec {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	h := make([]byte, vHeader)
	if _, err := io.ReadFull(c.r, h); err != nil {
		t.Fatal("record header:", err)
	}
	p := make([]byte, binary.LittleEndian.Uint32(h))
	if _, err := io.ReadFull(c.r, p); err != nil {
		t.Fatal("record payload:", err)
	}
	return vrec{h[4], h[5], binary.LittleEndian.Uint16(h[6:]), int64(binary.LittleEndian.Uint64(h[8:])), p}
}

func TestVideoLink(t *testing.T) {
	r := newCallRig(t)
	if c, m := r.videoHello(t, r.ts.token, "V9"); errCode(m) != "not_found" {
		t.Errorf("no call: %v", m)
	} else {
		c.wantEOF(t, "no call")
	}
	f := r.fake("V9").videoOffer()
	r.c.offer(f)
	r.want(t, "ringing", "")
	if c, m := r.videoHello(t, "wrong", "V9"); errCode(m) != "auth" {
		t.Errorf("bad token: %v", m)
	} else {
		c.wantEOF(t, "bad token")
	}
	if c, m := r.videoHello(t, r.ts.token, "other"); errCode(m) != "not_found" {
		t.Errorf("wrong id: %v", m)
	} else {
		c.wantEOF(t, "wrong id")
	}
	link, m := r.videoHello(t, r.ts.token, "V9")
	if m["ok"] != true {
		t.Fatalf("hello: %v", m)
	}

	// UI -> engine, right behind the hello: durations from the timestamps, orientation on change.
	for _, x := range []struct {
		ts int64
		o  byte
	}{{1000, 1}, {34000, 1}, {34000, 2}, {5_000_000, 2}} {
		link.Write(vRecord(vAU, x.o, 0, x.ts, []byte{0, 0, 1, 0x65, byte(x.ts)}))
	}
	link.Write(vRecord(vKeyReq, 0, 0, 0, nil)) // logged, nothing else
	for i := 0; f.nsent() < 4 && i < 200; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	f.mu.Lock()
	var got []time.Duration
	for _, s := range f.sent {
		got = append(got, s.d)
	}
	orients := f.orients
	f.mu.Unlock()
	want := []time.Duration{frameStep, 33 * time.Millisecond, frameStep, frameStep}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("durations %v; want %v", got, want)
	}
	if fmt.Sprint(orients) != "[1 2]" {
		t.Errorf("orientations %v", orients)
	}

	// engine -> UI: the peer's frames carry its orientation, a keyframe flag and a timestamp.
	f.vsink.(meowcaller.VideoOrientationSink).SetOrientation(3)
	idr := []byte{0, 0, 0, 1, 0x65, 9}
	f.vsink.WriteVideo(idr)
	f.vsink.WriteVideo([]byte{0, 0, 1, 0x41, 7})
	a := readRec(t, link)
	b := readRec(t, link)
	if a.typ != vAU || a.orient != 3 || a.flags != 1 || string(a.payload) != string(idr) {
		t.Errorf("first record %+v", a)
	}
	if b.typ != vAU || b.flags != 0 || b.ts < a.ts || len(b.payload) != 5 {
		t.Errorf("second record %+v", b)
	}
	// A keyframe request from the peer.
	f.onKey()
	if k := readRec(t, link); k.typ != vKeyReq || len(k.payload) != 0 {
		t.Errorf("keyframe request %+v", k)
	}
	// A new link replaces the old; the call goes on, picture or not.
	link2, m := r.videoHello(t, r.ts.token, "V9")
	if m["ok"] != true {
		t.Fatalf("second link: %v", m)
	}
	link.wantEOF(t, "replaced link")
	link2.Close()
	time.Sleep(100 * time.Millisecond)
	f.vsink.WriteVideo(idr) // nobody listens: no panic, no hang-up
	if f.count("hangup") != 0 {
		t.Error("the call hung up for want of a video link")
	}
	link3, _ := r.videoHello(t, r.ts.token, "V9")
	f.end("hangup")
	link3.wantEOF(t, "link at the end of the call")
}

func TestVideoQueueDropsOldest(t *testing.T) {
	l := &audioLink{out: make(chan []byte, videoQueue)}
	for i := 0; i < videoQueue+5; i++ {
		l.sendNewest([]byte{byte(i)})
	}
	if len(l.out) != videoQueue {
		t.Fatalf("queue holds %d", len(l.out))
	}
	if first := <-l.out; first[0] != 5 {
		t.Errorf("oldest kept is %d; want 5", first[0])
	}
}

func TestVideoRecordAndIDR(t *testing.T) {
	b := vRecord(vAU, 2, 1, -5, []byte{1, 2, 3})
	if len(b) != vHeader+3 || binary.LittleEndian.Uint32(b) != 3 || b[4] != 1 || b[5] != 2 ||
		binary.LittleEndian.Uint16(b[6:]) != 1 || int64(binary.LittleEndian.Uint64(b[8:])) != -5 {
		t.Errorf("record %v", b)
	}
	if !hasIDR([]byte{0, 0, 0, 1, 0x67, 1, 0, 0, 1, 0x65, 2}) || hasIDR([]byte{0, 0, 1, 0x41, 5}) || hasIDR(nil) {
		t.Error("hasIDR")
	}
}
