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

// encodeOpus makes an OGG Opus voice note from mono PCM at rate, one of libopus's rates
// (8, 12, 16, 24 or 48 kHz), in 20 ms frames.
func encodeOpus(pcm []int16, rate int) ([]byte, error) {
	var lookahead, cerr C.int
	enc := C.hb_encoder(C.int(rate), &lookahead, &cerr)
	if enc == nil {
		return nil, fmt.Errorf("opus encoder: %s", C.GoString(C.opus_strerror(cerr)))
	}
	defer C.opus_encoder_destroy(enc)

	// Granules count at 48 kHz whatever the input rate (RFC 7845). The decoder drops the
	// encoder's lookahead (pre-skip) at the start and everything past the last page's granule
	// at the end, so the input runs on in zeros until the lookahead is flushed.
	preSkip := int64(lookahead) * 48000 / int64(rate)
	end := preSkip + int64(len(pcm))*48000/int64(rate)
	frame := rate / 50
	frames := int((end + 959) / 960)
	in := make([]int16, frames*frame)
	copy(in, pcm)

	var o oggWriter
	head := []byte("OpusHead\x01\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	binary.LittleEndian.PutUint16(head[10:], uint16(preSkip))
	binary.LittleEndian.PutUint32(head[12:], uint32(rate))
	o.page([][]byte{head}, 0, oggFirst)
	vendor := "HyperBerry, " + C.GoString(C.opus_get_version_string())
	tags := binary.LittleEndian.AppendUint32([]byte("OpusTags"), uint32(len(vendor)))
	tags = binary.LittleEndian.AppendUint32(append(tags, vendor...), 0)
	o.page([][]byte{tags}, 0, 0)

	// About a second of packets to a page.
	var page [][]byte
	lacing := 0
	out := make([]byte, 1500)
	for i := 0; i < frames; i++ {
		n := C.opus_encode(enc, (*C.opus_int16)(unsafe.Pointer(&in[i*frame])), C.int(frame),
			(*C.uchar)(unsafe.Pointer(&out[0])), C.opus_int32(len(out)))
		if n < 0 {
			return nil, fmt.Errorf("opus encode: %s", C.GoString(C.opus_strerror(n)))
		}
		packet := append([]byte(nil), out[:n]...)
		page = append(page, packet)
		lacing += lacingSize(packet)
		if i == frames-1 {
			o.page(page, end, oggLast)
		} else if len(page) == 50 || lacing > 255-6 { // 6: an Opus packet's most (1275 bytes)
			o.page(page, int64(i+1)*960, 0)
			page, lacing = nil, 0
		}
	}
	return o.buf, nil
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
