package main

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/types"
	"github.com/purpshell/meowcaller"
)

func lidJID(u string) types.JID { return types.NewJID(u, types.HiddenUserServer) }

func idrAU(b byte) []byte { return []byte{0, 0, 0, 1, 0x65, b} }
func pAU(b byte) []byte   { return []byte{0, 0, 1, 0x41, b} }

// pvf is meowcaller handing the engine one participant's access unit.
func (f *fakeCall) pvf(who string, ssrc uint32, orient int, au []byte) {
	f.mu.Lock()
	fn := f.onPVF
	f.mu.Unlock()
	fn(meowcaller.ParticipantVideoFrame{Sender: lidJID(who), SSRC: ssrc, Orientation: orient, AccessUnit: au})
}

// keyframes waits for n keyframe requests (they go out on a goroutine) and gives them all.
func (f *fakeCall) waitKeyframes(t *testing.T, n int) []uint32 {
	t.Helper()
	for i := 0; ; i++ {
		f.mu.Lock()
		got := append([]uint32(nil), f.keyframes...)
		f.mu.Unlock()
		if len(got) >= n {
			return got
		}
		if i == 200 {
			t.Fatalf("keyframe requests %v; want %d", got, n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// groupVideo is a ringing group video call with 1@lid to 5@lid in its roster and a video link.
func (r *callRig) groupVideo(t *testing.T) (*fakeCall, *conn) {
	t.Helper()
	f := r.groupFake("GV1").videoOffer()
	for _, u := range []string{"1", "2", "3", "4", "5"} {
		f.group.Participants = append(f.group.Participants, meowcaller.GroupCallParticipant{JID: lidJID(u), State: "connected"})
	}
	r.c.offer(f)
	r.want(t, "ringing", "")
	link, m := r.videoHello(t, r.ts.token, f.id)
	if m["ok"] != true {
		t.Fatalf("video hello: %v", m)
	}
	for i := 0; ; i++ { // OnParticipantVideoFrame is registered on a goroutine
		f.mu.Lock()
		ok := f.onPVF != nil
		f.mu.Unlock()
		if ok {
			return f, link
		}
		if i == 200 {
			t.Fatal("no OnParticipantVideoFrame")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// people is the event's people by jid.
type people map[string]map[string]any

// until reads call events until the people are as ok says, and gives them.
func (r *callRig) until(t *testing.T, ok func(people) bool) people {
	t.Helper()
	for i := 0; i < 20; i++ {
		by := people{}
		ps, _ := r.ev(t)["people"].([]any)
		for _, p := range ps {
			p, _ := p.(map[string]any)
			by[p["jid"].(string)] = p
		}
		if ok(by) {
			return by
		}
	}
	t.Fatal("no event with the people as wanted")
	return nil
}

// wantEnded reads call events up to the one that says the call ended.
func (r *callRig) wantEnded(t *testing.T) {
	t.Helper()
	for i := 0; i < 20; i++ {
		if r.ev(t)["state"] == "ended" {
			return
		}
	}
	t.Fatal("the call did not end")
}

// is: jid sends video (or not) and is in tile (-1: none).
func is(jid string, video bool, tile float64) func(people) bool {
	return func(by people) bool { return by[jid]["video"] == video && by[jid]["tile"] == tile }
}

func both(a, b func(people) bool) func(people) bool {
	return func(by people) bool { return a(by) && b(by) }
}

// wantRec is what the next record on the link must be: a picture in tile t, an IDR or not, upright by orient.
func wantRec(t *testing.T, link *conn, tile int, idr bool, orient byte, au []byte) {
	t.Helper()
	flags := uint16(tile) << 8
	if idr {
		flags |= 1
	}
	if got := readRec(t, link); got.typ != vAU || got.flags != flags || got.orient != orient || string(got.payload) != string(au) {
		t.Fatalf("record %+v; want tile %d idr %v orient %d % x", got, tile, idr, orient, au)
	}
}

func TestGroupTilesRouting(t *testing.T) {
	r := newCallRig(t)
	f, link := r.groupVideo(t)
	if f.vsink != nil {
		t.Error("a group call took the single video sink")
	}

	// A, first: tile 0. Its first frame is an IDR, so nothing is asked.
	f.pvf("1", 101, 1, idrAU(1))
	wantRec(t, link, 0, true, 1, idrAU(1))
	f.pvf("1", 101, 1, pAU(2))
	wantRec(t, link, 0, false, 1, pAU(2))
	r.until(t, is("1@lid", true, 0))
	// B takes the next tile; its frames wait for its IDR, which is asked for (once a second).
	f.pvf("2", 202, 3, pAU(3))
	f.pvf("2", 202, 3, pAU(4))
	if got := f.waitKeyframes(t, 1); !reflect.DeepEqual(got, []uint32{202}) {
		t.Fatalf("keyframe requests %v", got)
	}
	f.pvf("2", 202, 3, idrAU(5))
	wantRec(t, link, 1, true, 3, idrAU(5)) // the two before it were dropped
	f.pvf("1", 101, 1, pAU(6))
	wantRec(t, link, 0, false, 1, pAU(6))
	r.until(t, is("2@lid", true, 1))
	if n := len(f.waitKeyframes(t, 1)); n != 1 {
		t.Errorf("%d keyframe requests", n)
	}

	// Four tiles: a fifth sender gets none, and its frames are dropped.
	f.pvf("3", 303, 0, idrAU(7))
	f.pvf("4", 404, 0, idrAU(8))
	wantRec(t, link, 2, true, 0, idrAU(7))
	wantRec(t, link, 3, true, 0, idrAU(8))
	f.pvf("5", 505, 0, idrAU(9))
	f.pvf("1", 101, 1, pAU(10))
	wantRec(t, link, 0, false, 1, pAU(10))
	r.until(t, is("5@lid", true, -1))
	f.end("hangup")
	r.wantEnded(t)
}

func TestGroupTilesSwap(t *testing.T) {
	r := newCallRig(t)
	f, link := r.groupVideo(t)
	f.pvf("1", 101, 0, idrAU(1))
	f.pvf("2", 202, 0, pAU(9)) // dropped: it waits for an IDR
	f.waitKeyframes(t, 1)
	f.pvf("2", 202, 0, idrAU(2))
	wantRec(t, link, 0, true, 0, idrAU(1))
	wantRec(t, link, 1, true, 0, idrAU(2))

	if err := r.raw("call.tiles", fmt.Sprintf(`{"id":%q,"jids":["2@lid","1@lid"]}`, f.id)); err != nil {
		t.Fatal(err)
	}
	r.until(t, both(is("2@lid", true, 0), is("1@lid", true, 1)))
	// Both tiles changed hands: each sender is asked for a keyframe at once.
	got := f.waitKeyframes(t, 3)
	if rest := got[1:]; !(rest[0] == 202 && rest[1] == 101 || rest[0] == 101 && rest[1] == 202) {
		t.Fatalf("keyframe requests %v", got)
	}
	f.pvf("1", 101, 0, pAU(3)) // not yet its IDR
	f.pvf("1", 101, 0, idrAU(4))
	wantRec(t, link, 1, true, 0, idrAU(4))
	f.pvf("2", 202, 0, idrAU(5))
	wantRec(t, link, 0, true, 0, idrAU(5))
	f.pvf("2", 202, 0, pAU(6))
	wantRec(t, link, 0, false, 0, pAU(6))

	// Tile 1 is freed by a list with one jid: B takes the first free tile again, by itself.
	if err := r.raw("call.tiles", fmt.Sprintf(`{"id":%q,"jids":["1@lid"]}`, f.id)); err != nil {
		t.Fatal(err)
	}
	r.until(t, is("2@lid", true, -1))
	f.pvf("2", 202, 0, idrAU(7))
	wantRec(t, link, 1, true, 0, idrAU(7))
	f.end("hangup")
	r.wantEnded(t)
}

// A sender that stops leaves the tile it took by itself, and shows no video; one placed by hand keeps
// its tile.
func TestGroupTilesStale(t *testing.T) {
	oldStale, oldSweep := videoStale, tileSweep
	videoStale, tileSweep = 200*time.Millisecond, 20*time.Millisecond
	r := newCallRig(t)
	f, link := r.groupVideo(t)
	defer func() { f.end("hangup"); videoStale, tileSweep = oldStale, oldSweep }()

	if err := r.raw("call.tiles", fmt.Sprintf(`{"id":%q,"jids":["","2@lid"]}`, f.id)); err != nil {
		t.Fatal(err)
	}
	f.pvf("1", 101, 0, idrAU(1))
	wantRec(t, link, 0, true, 0, idrAU(1))
	r.until(t, is("1@lid", true, 0))
	// No more frames: A is out of its tile, B stays in the one set by hand.
	r.until(t, both(is("1@lid", false, -1), is("2@lid", false, 1)))
	// It comes back and takes the first free tile again.
	f.pvf("1", 101, 0, idrAU(2))
	wantRec(t, link, 0, true, 0, idrAU(2))
}

// A sender's video state going off frees its tile at once, whatever its last frame.
func TestGroupTilesVideoOff(t *testing.T) {
	r := newCallRig(t)
	f, link := r.groupVideo(t)
	f.pvf("1", 101, 0, idrAU(1))
	wantRec(t, link, 0, true, 0, idrAU(1))
	r.until(t, is("1@lid", true, 0))
	f.onVS(meowcaller.VideoState{Raw: 6, Sender: lidJID("1")})
	r.until(t, is("1@lid", false, -1))
	r.c.mu.Lock()
	o := r.c.cur.vid.peerOrient.Load()
	r.c.mu.Unlock()
	if o != 0 {
		t.Errorf("a group's video state set the 1:1 orientation: %d", o)
	}
	f.end("hangup")
	r.wantEnded(t)
}

func TestGroupTilesRequests(t *testing.T) {
	r := newCallRig(t)
	// A group video call is placed with video, and the roster is for it to know.
	var video bool
	inner := r.c.dial
	r.c.dial = func(ctx context.Context, target string, v bool, members []string) (liveCall, error) {
		video = v
		return inner(ctx, target, v, members)
	}
	f := r.groupFake("GV2").videoOffer()
	f.phase = meowcaller.CallPhaseCalling
	r.next = f
	if err := r.raw("call.start", fmt.Sprintf(`{"chat":%q,"members":["1@lid","2@lid"],"video":true}`, teamKey)); err != nil {
		t.Fatal(err)
	}
	if !video {
		t.Error("dial wasn't told it is a video call")
	}
	wantVideo(t, r.want(t, "calling", ""), true, true, true, false, 0)

	for name, body := range map[string]string{
		"five":    `{"id":"GV2","jids":["1@lid","2@lid","3@lid","4@lid","5@lid"]}`,
		"not jid": `{"id":"GV2","jids":["1@lid","1:x@lid"]}`,
	} {
		if err := r.raw("call.tiles", body); err == nil || err.(*Error).Code != "bad_request" {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := r.raw("call.tiles", `{"id":"nope","jids":[]}`); err == nil || err.(*Error).Code != "not_found" {
		t.Errorf("wrong id: %v", err)
	}
	if err := r.raw("call.tiles", `{"id":"GV2","jids":["1@lid","1@lid"]}`); err != nil { // the second is freed
		t.Fatal(err)
	}
	r.c.mu.Lock()
	tiles := r.c.cur.tiles.tiles
	r.c.mu.Unlock()
	if tiles != [groupTiles]types.JID{lidJID("1")} {
		t.Errorf("a jid in two tiles: %v", tiles)
	}
	f.end("hangup")
	r.wantEnded(t)

	// Not a group call.
	g := r.fake("P1")
	r.c.offer(g)
	r.want(t, "ringing", "")
	if err := r.raw("call.tiles", `{"id":"P1","jids":[]}`); err == nil || err.(*Error).Code != "bad_request" {
		t.Errorf("a 1:1 call: %v", err)
	}
}

// The UI's decoder for a tile asks for a keyframe: the tile's sender is asked, at most once a second.
func TestGroupTileKeyframeFromUI(t *testing.T) {
	r := newCallRig(t)
	f, link := r.groupVideo(t)
	f.pvf("1", 101, 0, idrAU(1))
	wantRec(t, link, 0, true, 0, idrAU(1))
	f.pvf("2", 202, 0, idrAU(2))
	wantRec(t, link, 1, true, 0, idrAU(2))
	link.Write(vRecord(vKeyReq, 0, 1<<8, 0, nil)) // tile 1
	if got := f.waitKeyframes(t, 1); !reflect.DeepEqual(got, []uint32{202}) {
		t.Fatalf("keyframe requests %v; want [202]", got)
	}
	link.Write(vRecord(vKeyReq, 0, 1<<8, 0, nil)) // again at once: held back
	link.Write(vRecord(vKeyReq, 0, 3<<8, 0, nil)) // an empty tile: nobody to ask
	time.Sleep(50 * time.Millisecond)
	if n := len(f.waitKeyframes(t, 1)); n != 1 {
		t.Errorf("%d keyframe requests; want 1", n)
	}
	f.end("hangup")
	r.wantEnded(t)
}
