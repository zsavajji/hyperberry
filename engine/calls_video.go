package main

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"sync/atomic"
	"time"

	"github.com/purpshell/meowcaller"
)

// Video in calls (step 7c.2, docs/protocol2.md "Video in calls"): meowcaller carries H.264 access
// units; the camera, the encoder, the decoder and the screen are the UI's. The video link is the
// audio link's twin: a loopback connection with its own hello, here carrying 16-byte-headed records.

const (
	vHeader = 16
	// Record types.
	vAU     = 1 // an access unit, Annex-B
	vKeyReq = 2 // a keyframe request
	// videoQueue is how many of the peer's records wait for the writer when the UI doesn't read.
	videoQueue = 30
	// videoMax is the largest access unit taken from the UI; a bigger one is a broken link.
	videoMax = 1 << 20
	// frameStep is the duration of a frame with no earlier one to measure from (15 fps).
	frameStep = time.Second / 15
)

// vfields is what the `call` event says about video.
type vfields struct {
	video  bool // offered or placed as video (sticky)
	ours   bool // we send
	theirs bool // they send
	asked  bool // the peer asks us to turn ours on
	orient int  // the peer's: quarter turns to rotate their frames upright
}

// videoState is the video part of callState. f and sent are guarded by Calls.mu.
type videoState struct {
	f, sent    vfields
	voiceOnly  bool // answered a video call with voice only: stop our video once active
	peerOrient atomic.Int32
	t0         time.Time // the peer's timestamps count from here
	link       atomic.Pointer[audioLink]
}

// read refreshes f from the call. Call it with Calls.mu held.
func (v *videoState) read(cs *callState) vfields {
	if cs.call != nil {
		v.f.ours, v.f.theirs = cs.call.IsSendingVideo(), cs.call.IsReceivingVideo()
		v.f.video = v.f.video || cs.call.IsVideo()
	}
	v.f.video = v.f.video || v.f.ours || v.f.theirs
	v.f.orient = int(v.peerOrient.Load())
	return v.f
}

// videoBody adds the video fields to the `call` event. Call it with Calls.mu held.
func (cs *callState) videoBody(m map[string]any) {
	f := cs.vid.read(cs)
	cs.vid.sent = f
	m["video"], m["videoOurs"], m["videoTheirs"], m["videoAsked"], m["orientation"] = f.video, f.ours, f.theirs, f.asked, f.orient
}

// kind is the row's text for a call that had video: call -> video_call, missed_call -> missed_video_call.
func (cs *callState) kind(base string) string {
	if cs.vid.f.video {
		if base == "call" {
			return "video_call"
		}
		return "missed_video_call"
	}
	return base
}

// videoChanged sends the `call` event again if its video fields changed.
func (c *Calls) videoChanged(cs *callState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cs.ended || c.cur != cs || cs.state == "" || cs.call == nil || cs.vid.read(cs) == cs.vid.sent {
		return
	}
	c.srv.Emit("call", cs.body(""))
}

// hookVideo puts the video callbacks on the call. Call it with Calls.mu held.
func (c *Calls) hookVideo(cs *callState) {
	cs.vid.t0 = time.Now()
	cs.vid.read(cs)
	cs.call.ReceiveVideo(videoSink{c, cs})
	cs.call.OnVideoState(func(v meowcaller.VideoState) { c.peerVideo(cs, v) })
	cs.call.OnVideoKeyframeRequest(func() {
		if l := cs.vid.link.Load(); l != nil {
			l.sendNewest(vRecord(vKeyReq, 0, 0, 0, nil))
		}
	})
}

// peerVideo follows the peer's <video> stanzas. The peer turning its video on is accepted at once,
// ours staying off (the user's call, 2026-11-08: see theirs, start ours by hand); WhatsApp also
// withdraws a request nobody answers within ~5 s (state 0, on the Q10). Only when that accept
// fails does the UI ask ("asked"); any other state clears it (cancelled, rejected, or on already).
func (c *Calls) peerVideo(cs *callState, v meowcaller.VideoState) {
	c.log.Infof("call %s: peer video state %d (active %v, upgrade %v, orientation %d)", cs.id, v.Raw, v.Active, v.Upgrade, v.Orientation)
	if v.Upgrade {
		go c.acceptPeerVideo(cs) // off meowcaller's stanza handler: AcceptVideo sends two stanzas
		return
	}
	c.mu.Lock()
	cs.vid.f.asked = false
	c.mu.Unlock()
	if v.Active && v.Orientation >= 0 && v.Orientation <= 3 {
		cs.vid.peerOrient.Store(int32(v.Orientation))
	}
	c.videoChanged(cs)
}

// acceptPeerVideo takes the peer's video with ours off (meowcaller sends "stopped", then the accept).
func (c *Calls) acceptPeerVideo(cs *callState) {
	err := cs.call.AcceptVideo()
	if err == nil {
		c.log.Infof("call %s: the peer's video accepted, ours stays off", cs.id)
		return
	}
	c.log.Warnf("call %s: accepting the peer's video: %v; the UI asks", cs.id, err)
	c.mu.Lock()
	cs.vid.f.asked = true
	c.mu.Unlock()
	c.videoChanged(cs)
}

// videoSink is the call's VideoSink (and VideoOrientationSink): the peer's access units to the
// UI's video link, if there is one.
type videoSink struct {
	c  *Calls
	cs *callState
}

func (s videoSink) WriteVideo(au []byte) error {
	if l := s.cs.vid.link.Load(); l != nil {
		var flags uint16
		if hasIDR(au) {
			flags = 1
		}
		l.sendNewest(vRecord(vAU, byte(s.cs.vid.peerOrient.Load()), flags, time.Since(s.cs.vid.t0).Microseconds(), au))
	}
	return nil
}

// SetOrientation is meowcaller reading the peer's orientation off its RTP packets, ahead of the
// frame they belong to.
func (s videoSink) SetOrientation(o int) {
	if o >= 0 && o <= 3 && s.cs.vid.peerOrient.Swap(int32(o)) != int32(o) {
		s.c.videoChanged(s.cs)
	}
}

func (s videoSink) Close() error { return nil }

// vRecord is one record: u32 payload length, u8 type, u8 orientation, u16 flags, i64 µs.
func vRecord(typ, orient byte, flags uint16, ts int64, payload []byte) []byte {
	b := make([]byte, vHeader+len(payload))
	binary.LittleEndian.PutUint32(b, uint32(len(payload)))
	b[4], b[5] = typ, orient
	binary.LittleEndian.PutUint16(b[6:], flags)
	binary.LittleEndian.PutUint64(b[8:], uint64(ts))
	copy(b[vHeader:], payload)
	return b
}

// hasIDR is whether an Annex-B access unit holds an IDR slice (NAL type 5).
func hasIDR(au []byte) bool {
	for i := 0; i+3 < len(au); i++ {
		if au[i] == 0 && au[i+1] == 0 && au[i+2] == 1 {
			if au[i+3]&0x1f == 5 {
				return true
			}
			i += 2
		}
	}
	return false
}

// sendNewest queues a record for the UI; when the queue is full the oldest goes. A frame dropped is
// better than a delay, and the UI is told nothing: its decoder asks for a keyframe by itself.
func (l *audioLink) sendNewest(b []byte) {
	for {
		select {
		case l.out <- b:
			return
		default:
		}
		select {
		case <-l.out:
		default:
		}
	}
}

// frameDuration is the time a frame lasts, from the timestamps (µs) of it and the one before.
// ponytail: a gap over 1 s (the camera paused) or a step back counts as one frame step.
func frameDuration(prev, ts int64, have bool) time.Duration {
	if !have || ts <= prev || ts-prev > 1e6 {
		return frameStep
	}
	return time.Duration(ts-prev) * time.Microsecond
}

// attachVideo is Server.video: the hello of a video link for call id.
func (c *Calls) attachVideo(id string, conn net.Conn, ok []byte) bool {
	return c.attachLink(id, conn, ok, videoQueue, func(cs *callState) *atomic.Pointer[audioLink] { return &cs.vid.link }, c.readVideo)
}

// readVideo takes the UI's records until the link closes. No grace and no hang-up when it does:
// a call without its video link just has no picture.
func (c *Calls) readVideo(cs *callState, l *audioLink) {
	hdr := make([]byte, vHeader)
	var prev int64
	have, orient, failed := false, -1, false
	for {
		if _, err := io.ReadFull(l.conn, hdr); err != nil {
			break
		}
		n := binary.LittleEndian.Uint32(hdr)
		if n > videoMax {
			c.log.Warnf("call %s: video record of %d bytes, closing the link", cs.id, n)
			break
		}
		au := make([]byte, n)
		if _, err := io.ReadFull(l.conn, au); err != nil {
			break
		}
		switch hdr[4] {
		case vKeyReq:
			// meowcaller exports no way to ask the peer for a keyframe: it sends its own picture-loss
			// request when RTP packets go missing, and that is all there is.
			c.log.Infof("call %s: the UI's decoder asks for a keyframe (meowcaller can't pass it on)", cs.id)
		case vAU:
			ts := int64(binary.LittleEndian.Uint64(hdr[8:]))
			if o := int(hdr[5] & 3); o != orient {
				if err := cs.call.SetVideoOrientation(o); err != nil {
					c.log.Warnf("call %s: video orientation: %v", cs.id, err)
				}
				orient = o
			}
			d := frameDuration(prev, ts, have)
			prev, have = ts, true
			if err := cs.call.SendVideoWithDuration(au, d); err != nil && !failed {
				failed = true // while the video isn't up on the call this fails for every frame
				c.log.Infof("call %s: sending video: %v", cs.id, err)
			}
		}
	}
	l.close()
	cs.vid.link.CompareAndSwap(l, nil)
}

func (c *Calls) handleVideo(req json.RawMessage) (any, error) {
	cs, err := c.get(req)
	if err != nil {
		return nil, err
	}
	var r struct{ On bool }
	json.Unmarshal(req, &r)
	c.mu.Lock()
	asked := cs.vid.f.asked
	c.mu.Unlock()
	switch {
	case !r.On:
		err = cs.call.StopVideo()
	case asked:
		err = cs.call.AcceptVideo()
	default:
		err = cs.call.StartVideo()
	}
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	cs.vid.f.asked = false
	c.mu.Unlock()
	c.videoChanged(cs)
	return nil, nil
}

// voiceOnly stops our video on a video call answered with voice only, once it is up.
func (c *Calls) voiceOnly(cs *callState) {
	if err := cs.call.StopVideo(); err != nil {
		c.log.Warnf("call %s: stopping video: %v", cs.id, err)
	}
	c.videoChanged(cs)
}
