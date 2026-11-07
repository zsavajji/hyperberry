package main

import (
	"bufio"
	"encoding/json"
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

// ringPPS is the notification settings of the phone app, whose tone is the user's ringtone.
var ringPPS = "/pps/services/notify/settings/cat.phone"

// ringStart plays the file until stop is called; ringtone_qnx.go, or a no-op elsewhere.
var ringStart = startRing

// ringFile is the phone's own ringtone if PPS names one that exists, else the stock one.
func ringFile(pps string) string {
	f, err := os.Open(pps)
	if err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if tone, ok := strings.CutPrefix(sc.Text(), "tone::"); ok {
				if _, err := os.Stat(tone); err == nil {
					return tone
				}
			}
		}
	}
	return fallbackRing
}

// startRing begins the ringtone and the Hub item. Call it with c.mu held.
func (c *Calls) startRing(cs *callState) {
	cs.ring = make(chan struct{})
	cs.item = true
	c.st.nt.callRing(cs.chat, cs.name, cs.offer)
	start, pps := ringStart, ringPPS // read here: the goroutine may outlive a test's swap
	go func(done <-chan struct{}) {
		file := ringFile(pps)
		stop, err := start(file)
		if err != nil {
			c.log.Warnf("ringtone %s: %v", file, err)
			return
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
