package main

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// wavInfo describes a WAV as found, for the log: what the phone's recorder writes is
// measured, not assumed (PLAN.md §7).
type wavInfo struct {
	format, channels, rate, bits int
}

func (w wavInfo) String() string {
	return fmt.Sprintf("format %#x, %d Hz, %d ch, %d bit", w.format, w.rate, w.channels, w.bits)
}

// readWAV returns a WAV's 16-bit PCM mixed down to mono. A data chunk whose size is missing
// or too big (a recording that wasn't finalised) runs to the end of the file, and an odd
// trailing byte is dropped.
func readWAV(b []byte) ([]int16, wavInfo, error) {
	var info wavInfo
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, info, errors.New("wav: not RIFF/WAVE")
	}
	for b = b[12:]; len(b) >= 8; {
		id, size := string(b[:4]), int(binary.LittleEndian.Uint32(b[4:]))
		b = b[8:]
		if size < 0 || size > len(b) {
			size = len(b)
		}
		switch id {
		case "fmt ":
			if size < 16 {
				return nil, info, errors.New("wav: short fmt chunk")
			}
			info = parseFmt(b[:size])
		case "data":
			if info.format != 1 || info.bits != 16 || info.channels < 1 {
				return nil, info, fmt.Errorf("wav: unsupported %v", info)
			}
			pcm := mixMono(b[:size], info.channels)
			return pcm, info, nil
		}
		b = b[min(size+size&1, len(b)):] // chunks are padded to an even size
	}
	return nil, info, errors.New("wav: no data chunk")
}

// parseFmt reads a fmt chunk's body of 16 bytes or more.
func parseFmt(b []byte) (info wavInfo) {
	info.format = int(binary.LittleEndian.Uint16(b))
	info.channels = int(binary.LittleEndian.Uint16(b[2:]))
	info.rate = int(binary.LittleEndian.Uint32(b[4:]))
	info.bits = int(binary.LittleEndian.Uint16(b[14:]))
	if info.format == 0xfffe && len(b) >= 26 { // extensible: the real format is the subformat's
		info.format = int(binary.LittleEndian.Uint16(b[24:]))
	}
	return
}

// mixMono mixes whole 16-bit frames of PCM down to mono; a partial frame at the end is ignored.
func mixMono(b []byte, channels int) []int16 {
	pcm := make([]int16, len(b)/(2*channels))
	for i := range pcm {
		sum := 0
		for c := 0; c < channels; c++ {
			sum += int(int16(binary.LittleEndian.Uint16(b[2*(i*channels+c):])))
		}
		pcm[i] = int16(sum / channels)
	}
	return pcm
}

// writeWAV makes a mono 16-bit PCM WAV.
func writeWAV(pcm []int16, rate int) []byte {
	b := append(make([]byte, 0, 44+2*len(pcm)), wavHeader(len(pcm), rate)...)
	for _, s := range pcm {
		b = binary.LittleEndian.AppendUint16(b, uint16(s))
	}
	return b
}

// wavHeader is the 44 bytes before a mono 16-bit WAV's samples.
func wavHeader(samples, rate int) []byte {
	b := make([]byte, 44)
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(36+2*samples))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], uint32(rate))
	binary.LittleEndian.PutUint32(b[28:], uint32(2*rate))
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(2*samples))
	return b
}

// opusRate returns PCM at a rate libopus takes: as it is, or resampled to 48 kHz.
// ponytail: linear interpolation, fine for speech going up to 48 kHz; a windowed-sinc
// resampler if a recorder ever hands over a rate above 48 kHz.
func opusRate(pcm []int16, rate int) ([]int16, int) {
	switch rate {
	case 8000, 12000, 16000, 24000, 48000:
		return pcm, rate
	}
	if len(pcm) == 0 || rate <= 0 {
		return nil, 48000
	}
	out := make([]int16, int64(len(pcm))*48000/int64(rate))
	for i := range out {
		pos := float64(i) * float64(rate) / 48000
		j := int(pos)
		if j+1 >= len(pcm) {
			out[i] = pcm[len(pcm)-1]
			continue
		}
		f := pos - float64(j)
		out[i] = int16(float64(pcm[j])*(1-f) + float64(pcm[j+1])*f)
	}
	return out, 48000
}

// waveFrame is the peak window: 10 ms at 48 kHz.
const waveFrame = 480

// waveTracker keeps the peak of each frame as PCM goes by, so a long note isn't held as PCM.
// ponytail: 6,000 ints a minute at 48 kHz (an hour is 3 MB); fold the frames into fewer
// buckets as they come if notes ever run that long.
type waveTracker struct {
	peaks []int
	n     int // samples in the last frame
}

func (w *waveTracker) Add(pcm []int16) {
	for _, s := range pcm {
		if w.n == 0 {
			w.peaks = append(w.peaks, 0)
		}
		last := len(w.peaks) - 1
		w.peaks[last] = max(w.peaks[last], abs(int(s)))
		w.n = (w.n + 1) % waveFrame
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// Result is the 64 values of a voice note, 0 to 100 against the loudest (to verify against the
// main phone's bubble in step 4.3). Fewer than 64 frames are stretched over them.
func (w *waveTracker) Result() []byte {
	most, n := 1, len(w.peaks)
	for _, p := range w.peaks {
		most = max(most, p)
	}
	out := make([]byte, 64)
	for k := range out {
		lo := k * n / 64
		peak := 0
		for _, p := range w.peaks[lo:min(max((k+1)*n/64, lo+1), n)] {
			peak = max(peak, p)
		}
		out[k] = byte(peak * 100 / most)
	}
	return out
}

func waveform(pcm []int16) []byte {
	var w waveTracker
	w.Add(pcm)
	return w.Result()
}
