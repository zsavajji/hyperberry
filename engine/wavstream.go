package main

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// wavStream reads a WAV that is still being written, a read at a time. The sizes in its header
// are 0 or 0xffffffff until the recorder finishes, so the data chunk runs to wherever the file
// ends. Bytes of an unfinished header, or of half a frame (an odd byte, half a stereo frame),
// wait for the next Feed.
type wavStream struct {
	buf  []byte
	info wavInfo
	riff bool // the RIFF header is past
	skip int  // bytes of a chunk we don't read
	data bool // in the data chunk
}

// Feed takes the next bytes of the file and returns the mono PCM they complete.
func (w *wavStream) Feed(b []byte) ([]int16, error) {
	w.buf = append(w.buf, b...)
	for !w.data {
		if w.skip > 0 {
			n := min(w.skip, len(w.buf))
			w.buf, w.skip = w.buf[n:], w.skip-n
			if w.skip > 0 {
				return nil, nil
			}
		}
		if !w.riff {
			if len(w.buf) < 12 {
				return nil, nil
			}
			if string(w.buf[:4]) != "RIFF" || string(w.buf[8:12]) != "WAVE" {
				return nil, errors.New("wav: not RIFF/WAVE")
			}
			w.buf, w.riff = w.buf[12:], true
		}
		if len(w.buf) < 8 {
			return nil, nil
		}
		id, size := string(w.buf[:4]), int(binary.LittleEndian.Uint32(w.buf[4:]))
		switch {
		case id == "fmt ":
			if size < 16 {
				return nil, errors.New("wav: short fmt chunk")
			}
			if len(w.buf) < 8+size {
				return nil, nil
			}
			w.info = parseFmt(w.buf[8 : 8+size])
			w.buf, w.skip = w.buf[8+size:], size&1 // chunks are padded to an even size
		case id == "data":
			if w.info.format != 1 || w.info.bits != 16 || w.info.channels < 1 {
				return nil, fmt.Errorf("wav: unsupported %v", w.info)
			}
			w.buf, w.data = w.buf[8:], true
		default:
			if size < 0 { // a size past 2 GiB on a 32-bit int
				return nil, errors.New("wav: bad chunk size")
			}
			w.buf, w.skip = w.buf[8:], size+size&1
		}
	}
	n := len(w.buf) / (2 * w.info.channels) * (2 * w.info.channels)
	pcm := mixMono(w.buf[:n], w.info.channels)
	w.buf = append(w.buf[:0], w.buf[n:]...)
	return pcm, nil
}
