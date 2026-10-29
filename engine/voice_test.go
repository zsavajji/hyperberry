package main

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

func tone(hz float64, rate, n int) []int16 {
	pcm := make([]int16, n)
	for i := range pcm {
		pcm[i] = int16(8000 * math.Sin(2*math.Pi*hz*float64(i)/float64(rate)))
	}
	return pcm
}

// A known tone through the encoder and back out of the decoder (PLAN.md §7): the muxing is the
// part most likely to be wrong.
func TestOpusRoundTrip(t *testing.T) {
	in := tone(440, 16000, 16000) // 1 s
	ogg, err := encodeOpus(in, 16000)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := encodeOpus(in, 16000)
	if !bytes.Equal(ogg, again) {
		t.Error("the same PCM gave different bytes")
	}

	packets, err := oggPackets(ogg)
	if err != nil {
		t.Fatal(err)
	}
	head := packets[0].data
	if string(head[:8]) != "OpusHead" || len(head) != 19 || head[9] != 1 {
		t.Fatalf("OpusHead: % x", head)
	}
	if string(packets[1].data[:8]) != "OpusTags" {
		t.Fatal("no OpusTags")
	}
	if ogg[5] != oggFirst || ogg[len(ogg)-lastPageSize(ogg)+5] != oggLast {
		t.Error("beginning or end of stream not flagged")
	}
	preSkip := int64(binary.LittleEndian.Uint16(head[10:]))
	if g := packets[len(packets)-1].granule; g != preSkip+48000 {
		t.Errorf("last granule %d, want pre-skip %d + 48000", g, preSkip)
	}
	if len(ogg) > 4000 { // 20 kbps is 2500 bytes a second, plus the pages
		t.Errorf("%d bytes for 1 s", len(ogg))
	}

	out, err := decodeOpus(ogg)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 48000 {
		t.Fatalf("decoded %d samples, want 48000", len(out))
	}
	// The middle half: its pitch by zero crossings, and its level against the input's.
	mid := out[12000:36000]
	crossings, sum := 0, 0.0
	for i := 1; i < len(mid); i++ {
		if (mid[i-1] < 0) != (mid[i] < 0) {
			crossings++
		}
		sum += float64(mid[i]) * float64(mid[i])
	}
	if hz := float64(crossings) / 2 / 0.5; math.Abs(hz-440) > 10 {
		t.Errorf("decoded pitch %.0f Hz, want 440", hz)
	}
	if rms := math.Sqrt(sum / float64(len(mid))); rms < 0.7*5657 || rms > 1.3*5657 {
		t.Errorf("decoded level %.0f, want about 5657", rms)
	}
}

func lastPageSize(b []byte) int {
	size := 0
	for len(b) > 0 {
		n := int(b[26])
		size = 27 + n
		for _, l := range b[27 : 27+n] {
			size += int(l)
		}
		b = b[size:]
	}
	return size
}

// A packet continued across pages, as other encoders write them, is joined back.
func TestOggContinuedPacket(t *testing.T) {
	packet := bytes.Repeat([]byte{7}, 300)
	raw := func(lacing, body []byte, flags byte, seq uint32) []byte {
		h := make([]byte, 27)
		copy(h, "OggS")
		h[5] = flags
		binary.LittleEndian.PutUint32(h[18:], seq)
		h[26] = byte(len(lacing))
		h = append(append(h, lacing...), body...)
		binary.LittleEndian.PutUint32(h[22:], oggCRC(h))
		return h
	}
	stream := append(raw([]byte{255}, packet[:255], oggFirst, 0), raw([]byte{45}, packet[255:], oggContinued, 1)...)
	got, err := oggPackets(stream)
	if err != nil || len(got) != 1 || !bytes.Equal(got[0].data, packet) {
		t.Fatalf("got %d packets, %v", len(got), err)
	}
	stream[30] ^= 1
	if _, err := oggPackets(stream); err == nil {
		t.Error("a corrupted page passed its checksum")
	}
}

func TestWAV(t *testing.T) {
	in := tone(300, 16000, 1001)
	pcm, info, err := readWAV(writeWAV(in, 16000))
	if err != nil || info.rate != 16000 || info.channels != 1 || !slicesEqual(pcm, in) {
		t.Fatalf("round trip: %v, %v", info, err)
	}

	// Stereo, an extra chunk before the data, and a data size left at 0xffffffff.
	b := writeWAV([]int16{100, 300, -100, -300}, 8000)
	binary.LittleEndian.PutUint16(b[22:], 2)
	b = append(b[:36], append([]byte("LIST\x03\x00\x00\x00abc\x00"), b[36:]...)...)
	binary.LittleEndian.PutUint32(b[52:], 0xffffffff)
	pcm, info, err = readWAV(b)
	if err != nil || info.channels != 2 || !slicesEqual(pcm, []int16{200, -200}) {
		t.Fatalf("stereo: %v %v %v", pcm, info, err)
	}
}

func slicesEqual(a, b []int16) bool {
	return len(a) == len(b) && bytes.Equal(writeWAV(a, 1), writeWAV(b, 1))
}

func TestOpusRateAndWaveform(t *testing.T) {
	if out, rate := opusRate(tone(440, 44100, 44100), 44100); rate != 48000 || len(out) != 48000 {
		t.Errorf("44.1 kHz gave %d samples at %d", len(out), rate)
	}
	pcm := append(make([]int16, 500), tone(100, 8000, 500)...)
	w := waveform(pcm)
	if len(w) != 64 || w[0] != 0 || w[63] < 90 {
		t.Errorf("waveform %v", w)
	}
}
