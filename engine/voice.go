package main

/*
#cgo LDFLAGS: -lopus -lm
#include <opus/opus.h>

// opus_encoder_ctl is variadic, which cgo can't call. A voice note's encoder (PLAN.md §7):
// VOIP, voice, 20 kbps (more costs upload time and a phone microphone gains nothing),
// complexity 5 (about half the CPU of 10, no audible difference on speech).
static OpusEncoder *hb_encoder(int rate, int *lookahead, int *err) {
	OpusEncoder *e = opus_encoder_create(rate, 1, OPUS_APPLICATION_VOIP, err);
	if (!e)
		return NULL;
	opus_encoder_ctl(e, OPUS_SET_BITRATE(20000));
	opus_encoder_ctl(e, OPUS_SET_COMPLEXITY(5));
	opus_encoder_ctl(e, OPUS_SET_SIGNAL(OPUS_SIGNAL_VOICE));
	opus_encoder_ctl(e, OPUS_GET_LOOKAHEAD(lookahead));
	return e;
}
*/
import "C"

import (
	"encoding/binary"
	"errors"
	"fmt"
	"unsafe"
)

// opusEncoder makes an OGG Opus voice note from mono PCM at rate, one of libopus's rates
// (8, 12, 16, 24 or 48 kHz), in 20 ms frames. PCM goes in at any chunk size; a page is
// written when the next packet wouldn't fit, never when a chunk ends, so the bytes depend on
// the PCM alone.
type opusEncoder struct {
	enc     *C.OpusEncoder
	rate    int
	frame   int     // samples in 20 ms
	preSkip int64   // at 48 kHz
	pending []int16 // under one frame, for the next Write
	total   int64   // samples in
	packets int
	o       oggWriter
	page    [][]byte
	lacing  int
	out     [1500]byte
}

func newOpusEncoder(rate int) (*opusEncoder, error) {
	var lookahead, cerr C.int
	enc := C.hb_encoder(C.int(rate), &lookahead, &cerr)
	if enc == nil {
		return nil, fmt.Errorf("opus encoder: %s", C.GoString(C.opus_strerror(cerr)))
	}
	// Granules count at 48 kHz whatever the input rate (RFC 7845). The decoder drops the
	// encoder's lookahead (pre-skip) at the start and everything past the last page's granule
	// at the end, so Close runs the input on in zeros until the lookahead is flushed.
	e := &opusEncoder{enc: enc, rate: rate, frame: rate / 50, preSkip: int64(lookahead) * 48000 / int64(rate)}
	head := []byte("OpusHead\x01\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	binary.LittleEndian.PutUint16(head[10:], uint16(e.preSkip))
	binary.LittleEndian.PutUint32(head[12:], uint32(rate))
	e.o.page([][]byte{head}, 0, oggFirst)
	vendor := "HyperBerry, " + C.GoString(C.opus_get_version_string())
	tags := binary.LittleEndian.AppendUint32([]byte("OpusTags"), uint32(len(vendor)))
	tags = binary.LittleEndian.AppendUint32(append(tags, vendor...), 0)
	e.o.page([][]byte{tags}, 0, 0)
	return e, nil
}

// destroy frees libopus's encoder; Close does it too, so a cancelled note calls only this.
func (e *opusEncoder) destroy() {
	if e.enc != nil {
		C.opus_encoder_destroy(e.enc)
		e.enc = nil
	}
}

// Write encodes whole frames and keeps the rest.
func (e *opusEncoder) Write(pcm []int16) error {
	e.total += int64(len(pcm))
	buf := append(e.pending, pcm...)
	n := len(buf) / e.frame * e.frame
	for i := 0; i < n; i += e.frame {
		if err := e.encode(buf[i : i+e.frame]); err != nil {
			return err
		}
	}
	e.pending = append(e.pending[:0], buf[n:]...)
	return nil
}

func (e *opusEncoder) encode(frame []int16) error {
	n := C.opus_encode(e.enc, (*C.opus_int16)(unsafe.Pointer(&frame[0])), C.int(e.frame),
		(*C.uchar)(unsafe.Pointer(&e.out[0])), C.opus_int32(len(e.out)))
	if n < 0 {
		return fmt.Errorf("opus encode: %s", C.GoString(C.opus_strerror(n)))
	}
	packet := append([]byte(nil), e.out[:n]...)
	// About a second of packets to a page; the lacing values of a page are 255 at most.
	if len(e.page) == 50 || e.lacing+lacingSize(packet) > 255 {
		e.o.page(e.page, int64(e.packets)*960, 0)
		e.page, e.lacing = nil, 0
	}
	e.page = append(e.page, packet)
	e.lacing += lacingSize(packet)
	e.packets++
	return nil
}

// Close flushes the lookahead and returns the whole OGG.
func (e *opusEncoder) Close() ([]byte, error) {
	defer e.destroy()
	end := e.preSkip + e.total*48000/int64(e.rate)
	frames := int((end + 959) / 960)
	zeros := make([]int16, e.frame)
	if len(e.pending) > 0 {
		if err := e.encode(append(e.pending, zeros[:e.frame-len(e.pending)]...)); err != nil {
			return nil, err
		}
	}
	for e.packets < frames {
		if err := e.encode(zeros); err != nil {
			return nil, err
		}
	}
	e.o.page(e.page, end, oggLast)
	return e.o.buf, nil
}

func encodeOpus(pcm []int16, rate int) ([]byte, error) {
	e, err := newOpusEncoder(rate)
	if err != nil {
		return nil, err
	}
	if err := e.Write(pcm); err != nil {
		e.destroy()
		return nil, err
	}
	return e.Close()
}

// decodeOpus decodes an OGG Opus stream to mono PCM at 48 kHz, the format's own rate.
func decodeOpus(data []byte) ([]int16, error) {
	packets, err := oggPackets(data)
	if err != nil {
		return nil, err
	}
	if len(packets) < 2 || len(packets[0].data) < 19 || string(packets[0].data[:8]) != "OpusHead" {
		return nil, errors.New("opus: no OpusHead")
	}
	channels := int(packets[0].data[9])
	preSkip := int(binary.LittleEndian.Uint16(packets[0].data[10:]))
	if channels < 1 || channels > 2 {
		return nil, fmt.Errorf("opus: %d channels", channels)
	}
	var cerr C.int
	dec := C.opus_decoder_create(48000, C.int(channels), &cerr)
	if dec == nil {
		return nil, fmt.Errorf("opus decoder: %s", C.GoString(C.opus_strerror(cerr)))
	}
	defer C.opus_decoder_destroy(dec)

	const maxFrame = 5760 // 120 ms at 48 kHz, the longest an Opus packet holds
	buf := make([]int16, maxFrame*channels)
	var pcm []int16
	for _, p := range packets[2:] {
		if len(p.data) == 0 {
			continue
		}
		n := C.opus_decode(dec, (*C.uchar)(unsafe.Pointer(&p.data[0])), C.opus_int32(len(p.data)),
			(*C.opus_int16)(unsafe.Pointer(&buf[0])), maxFrame, 0)
		if n < 0 {
			return nil, fmt.Errorf("opus decode: %s", C.GoString(C.opus_strerror(n)))
		}
		for i := 0; i < int(n); i++ {
			if channels == 2 {
				pcm = append(pcm, int16((int32(buf[2*i])+int32(buf[2*i+1]))/2))
			} else {
				pcm = append(pcm, buf[i])
			}
		}
	}
	end := int(packets[len(packets)-1].granule)
	if end > len(pcm) || end < preSkip {
		end = len(pcm)
	}
	if preSkip > end {
		preSkip = end
	}
	return pcm[preSkip:end], nil
}
