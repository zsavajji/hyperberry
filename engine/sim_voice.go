package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"time"
)

// simDownload stands for the download of a live voice note. Tests may shorten it.
var simDownload = 500 * time.Millisecond

const simRate = 16000

// simSpeech is seconds of made-up speech, the same every run: tone bursts of 150 to 400 ms with
// a quick attack and a decay, each with its own pitch and loudness, and short gaps, so the
// waveform varies.
func simSpeech(seconds int) []int16 {
	pcm := make([]int16, seconds*simRate)
	rng := rand.New(rand.NewPCG(uint64(seconds), 7))
	for at := 0; at < len(pcm); {
		n := (150 + rng.IntN(250)) * simRate / 1000
		f, amp := 110+140*rng.Float64(), 0.15+0.85*rng.Float64()
		for i := 0; i < n && at+i < len(pcm); i++ {
			t := float64(i) / simRate
			env := math.Min(t/0.02, 1) * math.Exp(-3*float64(i)/float64(n))
			p := 2 * math.Pi * f * t
			pcm[at+i] = int16(9000 * amp * env * (math.Sin(p) + 0.5*math.Sin(2*p) + 0.25*math.Sin(3*p)))
		}
		gap := 20 + rng.IntN(100)
		if rng.IntN(6) == 0 {
			gap = 300 // a breath between phrases
		}
		at += n + gap*simRate/1000
	}
	return pcm
}

// simNote is a made-up voice note: its OGG and waveform.
func simNote(seconds int) (ogg, wf []byte, err error) {
	pcm := simSpeech(seconds)
	ogg, err = encodeOpus(pcm, simRate)
	return ogg, waveform(pcm), err
}

// voiceAudio gives a seeded voice note its audio, as if downloaded: the OGG on disk, the
// waveform, media_state 2.
func (s *Sim) voiceAudio(r *msgRow) {
	if r.Kind != msgVoice || r.Duration == 0 {
		return
	}
	ogg, wf, err := simNote(r.Duration)
	if err == nil {
		err = writeFile(oggPath(s.srv.dir, r.ID), ogg)
	}
	if err != nil {
		s.log.Warnf("sim voice %s: %v", r.ID, err)
		return
	}
	r.Waveform, r.MediaState = wf, 2
}

// handleVoice is sim.voice: an incoming voice note in chat n, now. The row comes first, on the
// server; its OGG arrives after simDownload, as a live note's does.
func (s *Sim) handleVoice(req json.RawMessage) (any, error) {
	var r struct{ N, Seconds int }
	json.Unmarshal(req, &r)
	if err := s.ready(); err != nil {
		return nil, err
	}
	if r.N < 0 || r.N >= len(simChats) {
		return nil, &Error{"bad_request", fmt.Sprintf("n is 0 to %d", len(simChats)-1)}
	}
	if r.Seconds <= 0 {
		r.Seconds = 5
	}
	ogg, wf, err := simNote(r.Seconds)
	if err != nil {
		return nil, err
	}
	c, dir := simChats[r.N], s.srv.dir
	id := s.deliver(c, time.Now(), msgRow{Kind: msgVoice, Duration: r.Seconds, Waveform: wf, MediaState: 1})
	time.AfterFunc(simDownload, func() {
		state := 2
		if err := writeFile(oggPath(dir, id), ogg); err != nil {
			s.log.Warnf("sim voice %s: %v", id, err)
			state = 3
		}
		s.store.ops <- func(t *storeTx) { t.setMedia(c.jid, id, state, nil) }
	})
	return nil, nil
}

// handleVoicePlay is voice.play: the OGG is always here, so nothing is fetched, and the played
// receipt is a log line.
func (s *Sim) handleVoicePlay(req json.RawMessage) (any, error) {
	r, err := parseVoice(req)
	if err != nil {
		return nil, err
	}
	if _, err := s.chat(r.Chat); err != nil {
		return nil, err
	}
	fetch := func(msgRow) error { return errors.New("the simulated note isn't here") }
	notify := func(msgRow) error {
		s.log.Infof("sim: voice.play %s: played", req)
		return nil
	}
	return s.store.playVoice(s.srv.dir, r, fetch, notify)
}
