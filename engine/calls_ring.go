package main

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"
)

// The ringing of an incoming call (step 7.2): a Hub item and the ringtone, both until the call
// stops ringing.

const (
	// ringMax ends the ringtone whatever happens: WhatsApp stops ringing sooner.
	ringMax = 60 * time.Second
	// fallbackRing exists on the Q10 (2026-11-07).
	fallbackRing = "/usr/share/sounds/notification_phone_ringer.wav"
)

// ringPPS is the phone app's notification settings, whose tone is the user's ringtone; notifyStatus
// and notifyGlobal say whether the phone's mode lets a call ring. All readable by apps (Q10,
// 2026-11-07).
var (
	ringPPS      = "/pps/services/notify/settings/cat.phone"
	notifyStatus = "/pps/services/notify/status"
	notifyGlobal = "/pps/services/notify/settings/global"
)

// ringStart plays the file until stop is called; ringtone_qnx.go, or a no-op elsewhere.
var ringStart = startRing

// ppsValue is key's value in a PPS object ("key:type:value" lines, the type often empty), "" when
// the object or the key is missing.
func ppsValue(path, key string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		if k, rest, ok := strings.Cut(line, ":"); ok && k == key {
			if _, v, ok := strings.Cut(rest, ":"); ok {
				return v
			}
		}
	}
	return ""
}

// ringFile is the phone's own ringtone, as its Phone app's settings name it, "" when they don't.
// It isn't checked here: the engine can't see into the tones' folder, mm-renderer can (Q10,
// 0.0.1.185 fell back to the stock tone for that), and refuses one that isn't there.
func ringFile(pps string) string { return ppsValue(pps, "tone") }

// ringAllowed is whether the phone's notification mode lets a call ring (the user's call,
// 2026-11-07: Silent must not ring). The notify service's status says sound::off in Silent,
// Vibrate, Alerts off and the like; Phone only still rings calls. Unreadable, it rings. The Hub
// item's own alert follows the mode by itself, vibration included.
func ringAllowed(status, global string) bool {
	return ppsValue(global, "modeid") == "phoneonly" || ppsValue(status, "sound") != "off"
}

// startRing begins the ringtone and the Hub item. Call it with c.mu held.
func (c *Calls) startRing(cs *callState) {
	cs.ring = make(chan struct{})
	cs.item = true
	c.st.nt.callRing(cs.chat, cs.name, cs.offer, cs.group)
	// Read here: the goroutine may outlive a test's swap.
	start, pps, status, global := ringStart, ringPPS, notifyStatus, notifyGlobal
	go func(done <-chan struct{}) {
		if !ringAllowed(status, global) {
			c.log.Infof("ringtone: off, the phone's notification mode has no sound")
			return
		}
		file := ringFile(pps)
		var stop func()
		err := errors.New("the phone names no ringtone")
		if file != "" {
			stop, err = start(file)
		}
		if err != nil {
			c.log.Warnf("ringtone %q: %v; the stock one instead", file, err)
			file = fallbackRing
			if stop, err = start(file); err != nil {
				c.log.Warnf("ringtone %s: %v", file, err)
				return
			}
		}
		c.log.Infof("ringtone %s", file)
		select {
		case <-done:
		case <-time.After(ringMax):
		}
		stop()
	}(cs.ring)
}

// silenceRing ends the ringtone only: the Hub item and the ringing state stay (call.silence).
// Call it with c.mu held.
func (c *Calls) silenceRing(cs *callState) {
	if cs.ring != nil {
		close(cs.ring)
		cs.ring = nil
	}
}

// stopRing ends the ringtone and takes the Hub item out. Call it with c.mu held.
func (c *Calls) stopRing(cs *callState) {
	c.silenceRing(cs)
	if cs.item {
		cs.item = false
		c.st.nt.callEnd(cs.chat)
	}
}

func (c *Calls) handleSilence(req json.RawMessage) (any, error) {
	cs, err := c.get(req)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if cs.state == "ringing" {
		c.silenceRing(cs)
	}
	return nil, nil
}
