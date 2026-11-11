package main

import (
	"encoding/json"
	"time"

	"github.com/polymorfa/hypermeow/types"
	"github.com/purpshell/meowcaller"
)

// A group video call's tiles (phase 7e, V2; docs/protocol2.md "Video in calls"): each participant's
// frames come apart by sender, and the ones in a tile (up to groupTiles) go on the video link with the
// tile in the flags' high byte. Calls.mu guards all of it.

const groupTiles = 4

// videoStale: a sender with no frame for this long is no longer sending (and leaves a tile it got by
// itself); tileSweep is how often the tiles are checked for that. Vars for the tests, read once per
// call, when it is hooked.
var (
	videoStale = 3 * time.Second
	tileSweep  = time.Second
)

// pliEvery is the least time between two keyframe requests for one tile.
const pliEvery = time.Second

type sender struct {
	ssrc   uint32
	orient int
	last   time.Time // of their last frame; zero once their video state went off
}

type tileState struct {
	stale   time.Duration         // videoStale, as the call began
	senders map[types.JID]*sender // by sender, .ToNonAD()
	tiles   [groupTiles]types.JID // empty JID = free
	manual  [groupTiles]bool      // set by call.tiles: kept until changed or the sender leaves the call
	waitIDR [groupTiles]bool      // the tile's sender changed: drop until their IDR
	pli     [groupTiles]time.Time // last keyframe request for the tile
}

// sending is whether a sender had a frame lately.
func (ts *tileState) sending(j types.JID) bool {
	s := ts.senders[j]
	return s != nil && time.Since(s.last) < ts.stale
}

// tileOf is the tile holding j, or -1.
func (ts *tileState) tileOf(j types.JID) int {
	if !j.IsEmpty() {
		for i, t := range ts.tiles {
			if t == j {
				return i
			}
		}
	}
	return -1
}

// release frees a tile.
func (ts *tileState) release(i int) {
	ts.tiles[i], ts.manual[i], ts.waitIDR[i] = types.EmptyJID, false, false
}

// askKeyframe sends a picture-loss request to one sender, off the lock (and off meowcaller's goroutine).
func (c *Calls) askKeyframe(cs *callState, ssrc uint32) {
	go func() {
		if err := cs.call.RequestParticipantKeyframe(ssrc); err != nil {
			c.log.Warnf("call %s: keyframe request for %d: %v", cs.id, ssrc, err)
		}
	}()
}

// groupFrame is one participant's access unit (meowcaller's OnParticipantVideoFrame). A sender with no
// tile takes the first free one; a tile's frames wait for the sender's IDR, asking for it.
func (c *Calls) groupFrame(cs *callState, f meowcaller.ParticipantVideoFrame) {
	if f.Sender.IsEmpty() {
		return
	}
	key, idr, now := f.Sender.ToNonAD(), hasIDR(f.AccessUnit), time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if cs.ended || c.cur != cs {
		return
	}
	ts := &cs.tiles
	s := ts.senders[key]
	if s == nil {
		s = &sender{}
		ts.senders[key] = s
	}
	changed := !ts.sending(key) // the people's `video` flips
	s.ssrc, s.last = f.SSRC, now
	if f.Orientation >= 0 && f.Orientation <= 3 {
		s.orient = f.Orientation
	}
	t := ts.tileOf(key)
	if t < 0 {
		for i, j := range ts.tiles {
			if j.IsEmpty() {
				t = i
				break
			}
		}
		if t >= 0 {
			ts.tiles[t], ts.manual[t], ts.waitIDR[t], ts.pli[t] = key, false, true, time.Time{}
			changed = true
		}
	}
	if t >= 0 {
		switch {
		case ts.waitIDR[t] && !idr:
			if now.Sub(ts.pli[t]) > pliEvery {
				ts.pli[t] = now
				c.askKeyframe(cs, s.ssrc)
			}
		default:
			ts.waitIDR[t] = false
			if l := cs.vid.link.Load(); l != nil {
				flags := uint16(t) << 8
				if idr {
					flags |= 1
				}
				l.sendNewest(vRecord(vAU, byte(s.orient), flags, time.Since(cs.vid.t0).Microseconds(), f.AccessUnit))
			}
		}
	}
	if changed {
		c.sendPeople(cs)
	}
}

// sweepTiles frees the tiles of senders that went quiet (the ones nobody set by hand) and of senders
// that left the call, every `every`, until the call ends. It also sends the people again, which is
// how `video` going false reaches the UI.
func (c *Calls) sweepTiles(cs *callState, every time.Duration) {
	tk := time.NewTicker(every)
	defer tk.Stop()
	for range tk.C {
		c.mu.Lock()
		if cs.ended || c.cur != cs {
			c.mu.Unlock()
			return
		}
		ts := &cs.tiles
		in := map[types.JID]bool{}
		for _, p := range cs.roster {
			in[p.JID.ToNonAD()] = true
		}
		for i, j := range ts.tiles {
			// ponytail: until the first roster comes, nobody is "gone".
			if !j.IsEmpty() && (!ts.manual[i] && !ts.sending(j) || len(in) > 0 && !in[j]) {
				ts.release(i)
			}
		}
		c.sendPeople(cs)
		c.mu.Unlock()
	}
}

// senderOff is a sender's video state going off: they are not sending, and leave a tile they got by
// themselves.
func (c *Calls) senderOff(cs *callState, who types.JID) {
	if who.IsEmpty() {
		return
	}
	who = who.ToNonAD()
	c.mu.Lock()
	defer c.mu.Unlock()
	if cs.ended || c.cur != cs {
		return
	}
	ts := &cs.tiles
	if s := ts.senders[who]; s != nil {
		s.last = time.Time{}
	}
	if i := ts.tileOf(who); i >= 0 && !ts.manual[i] {
		ts.release(i)
	}
	c.sendPeople(cs)
}

// handleTiles is `call.tiles {id, jids}`: the tiles set by hand, in order; "" keeps one free. A tile
// whose sender changes waits for their IDR, which is asked of them at once.
func (c *Calls) handleTiles(req json.RawMessage) (any, error) {
	cs, err := c.get(req)
	if err != nil {
		return nil, err
	}
	var r struct{ Jids []string }
	json.Unmarshal(req, &r)
	if !cs.group {
		return nil, &Error{"bad_request", "tiles are for a group call"}
	}
	if len(r.Jids) > groupTiles {
		return nil, &Error{"bad_request", "a group call has 4 tiles at most"}
	}
	var want [groupTiles]types.JID
	for i, s := range r.Jids {
		if s == "" {
			continue
		}
		j, err := types.ParseJID(s)
		if err != nil {
			return nil, &Error{"bad_request", "not a jid: " + s}
		}
		want[i] = j.ToNonAD()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if cs.ended || c.cur != cs {
		return nil, &Error{"not_found", "no such live call"}
	}
	ts := &cs.tiles
	for i, j := range want {
		for k := 0; k < i; k++ { // in two tiles: the first keeps it
			if !j.IsEmpty() && want[k] == j {
				j = types.EmptyJID
			}
		}
		switch {
		case j.IsEmpty():
			ts.release(i)
		case j != ts.tiles[i]:
			ts.tiles[i], ts.manual[i], ts.waitIDR[i] = j, true, true
			if s := ts.senders[j]; s != nil && s.ssrc != 0 {
				ts.pli[i] = time.Now()
				c.askKeyframe(cs, s.ssrc)
			}
		}
		want[i] = j
	}
	c.sendPeople(cs)
	return nil, nil
}

// tileKeyframe is the UI's decoder for tile t asking for a keyframe: its sender is asked, at most
// once every pliEvery.
func (c *Calls) tileKeyframe(cs *callState, t int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t < 0 || t >= groupTiles || cs.ended || c.cur != cs {
		return
	}
	ts := &cs.tiles
	s := ts.senders[ts.tiles[t]]
	if s == nil || s.ssrc == 0 || time.Since(ts.pli[t]) <= pliEvery {
		return
	}
	ts.pli[t] = time.Now()
	c.askKeyframe(cs, s.ssrc)
}
