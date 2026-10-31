package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/polymorfa/hypermeow/types"
	waLog "github.com/polymorfa/hypermeow/util/log"
)

// voicePoll is how often a growing WAV is read. Tests shorten it.
var voicePoll = 100 * time.Millisecond

// recording is a WAV being written by the UI, read, mixed down and encoded as it grows, so
// that at the end only the tail is left to do.
type recording struct {
	chat, path string
	log        waLog.Logger
	poll       time.Duration // voicePoll at the start: tests restore it while this may still run

	off    int64 // bytes of the file read so far
	stream wavStream
	enc    *opusEncoder // made once the header has told the rate
	wave   waveTracker
	err    error

	stop chan struct{}
	done chan struct{}
	once sync.Once
}

func (r *recording) run() {
	defer close(r.done)
	t := time.NewTicker(r.poll)
	defer t.Stop()
	for {
		select {
		case <-r.stop:
			return
		case <-t.C:
			if r.err = r.pump(); r.err != nil {
				return
			}
		}
	}
}

// halt stops the polling and waits for it, so the caller owns the recording.
func (r *recording) halt() {
	r.once.Do(func() { close(r.stop) })
	<-r.done
}

// drop is halt and the end of the encoder, for a recording that isn't sent.
func (r *recording) drop() {
	r.halt()
	if r.enc != nil {
		r.enc.destroy()
	}
}

// pump reads what the file has grown by. A file that isn't there yet is not an error.
func (r *recording) pump() error {
	f, err := os.Open(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	if _, err := f.Seek(r.off, io.SeekStart); err != nil {
		return err
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	r.off += int64(len(b))
	pcm, err := r.stream.Feed(b)
	if err != nil {
		return err
	}
	if r.enc == nil && r.stream.data {
		if r.enc, err = newOpusEncoder(r.stream.info.rate); err != nil {
			return err
		}
	}
	if len(pcm) == 0 {
		return nil
	}
	r.wave.Add(pcm)
	return r.enc.Write(pcm)
}

// finish reads the tail and returns the OGG, the waveform and the length in whole seconds, at
// least 1.
func (r *recording) finish() (ogg, wave []byte, secs int, err error) {
	r.halt()
	if r.err != nil {
		r.drop()
		return nil, nil, 0, r.err
	}
	if r.off == 0 {
		// AudioRecorder may write only at the end; the Q10 test will tell.
		r.log.Warnf("voice: the WAV didn't grow while recording; encoded at the end")
	}
	if err := r.pump(); err != nil {
		r.drop()
		return nil, nil, 0, err
	}
	if r.enc == nil || r.enc.total == 0 {
		r.drop()
		return nil, nil, 0, &Error{"bad_request", "the recording has no audio"}
	}
	rate := int64(r.enc.rate)
	secs = max(1, int((r.enc.total+rate/2)/rate))
	ogg, err = r.enc.Close()
	return ogg, r.wave.Result(), secs, err
}

// voiceRec holds the one recording that may run at a time.
type voiceRec struct {
	mu  sync.Mutex
	cur *recording
}

// wavPath checks that path is a WAV inside the data dir and returns it absolute.
func wavPath(dir, path string) (string, error) {
	d, err := filepath.Abs(dir)
	if err == nil {
		path, err = filepath.Abs(path)
	}
	if err == nil {
		var rel string
		if rel, err = filepath.Rel(d, path); err == nil && (strings.HasPrefix(rel, "..") || rel == "." || filepath.Ext(path) != ".wav") {
			err = fmt.Errorf("%s is not a .wav inside the data dir", path)
		}
	}
	if err != nil {
		return "", &Error{"bad_request", err.Error()}
	}
	return path, nil
}

// start begins reading path, and cancels the recording before it. It returns the chat of the
// one cancelled ("" if none), whose "recording" state is still on.
func (v *voiceRec) start(dir, chat, path string, log waLog.Logger) (string, error) {
	path, err := wavPath(dir, path)
	if err != nil {
		return "", err
	}
	r := &recording{chat: chat, path: path, log: log, poll: voicePoll, stop: make(chan struct{}), done: make(chan struct{})}
	go r.run()
	v.mu.Lock()
	old := v.cur
	v.cur = r
	v.mu.Unlock()
	if old == nil {
		return "", nil
	}
	old.drop()
	if old.path != path { // the same file again starts over, and is still being written
		os.Remove(old.path)
	}
	return old.chat, nil
}

// take removes the recording of path from the registry, for voice.send.
func (v *voiceRec) take(dir, path string) (*recording, error) {
	path, err := wavPath(dir, path)
	if err != nil {
		return nil, err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	r := v.cur
	if r == nil || r.path != path {
		return nil, &Error{"bad_request", "no recording for " + path}
	}
	v.cur = nil
	return r, nil
}

// cancel drops the recording of path, if one runs, and deletes the WAV. It returns the chat
// whose "recording" state to stop.
func (v *voiceRec) cancel(dir, path string) (string, error) {
	path, err := wavPath(dir, path)
	if err != nil {
		return "", err
	}
	v.mu.Lock()
	r := v.cur
	if r != nil && r.path == path {
		v.cur = nil
	} else {
		r = nil
	}
	v.mu.Unlock()
	os.Remove(path)
	if r == nil {
		return "", nil
	}
	r.drop()
	return r.chat, nil
}

// voiceNote is a recording ended: what the upload needs, and what queueing it found.
type voiceNote struct {
	ogg, wave []byte
	secs      int
	quote     msgRow // empty when there is no quote
	created   bool   // the chat was new
}

// queueVoice ends the recording of path and puts our voice note in as sending: its OGG on
// disk, the row with the length and waveform, the chat's preview. The WAV goes.
func (st *Store) queueVoice(v *voiceRec, dir string, jid types.JID, chat, path, quote, id string) (*voiceNote, error) {
	rec, err := v.take(dir, path)
	if err != nil {
		return nil, err
	}
	n := &voiceNote{}
	n.ogg, n.wave, n.secs, err = rec.finish()
	if err == nil {
		err = writeFile(oggPath(dir, id), n.ogg)
	}
	os.Remove(rec.path)
	if err != nil {
		return nil, err
	}
	n.quote, n.created = st.queueRow(jid, chat, quote, msgRow{Chat: chat, ID: id, TS: time.Now().Unix(), Out: true,
		State: stateSending, Kind: msgVoice, Duration: n.secs, Waveform: n.wave, MediaState: 2})
	return n, nil
}
