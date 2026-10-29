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
			info.format = int(binary.LittleEndian.Uint16(b))
			info.channels = int(binary.LittleEndian.Uint16(b[2:]))
			info.rate = int(binary.LittleEndian.Uint32(b[4:]))
			info.bits = int(binary.LittleEndian.Uint16(b[14:]))
			if info.format == 0xfffe && size >= 26 { // extensible: the real format is the subformat's
				info.format = int(binary.LittleEndian.Uint16(b[24:]))
			}
		case "data":
			if info.format != 1 || info.bits != 16 || info.channels < 1 {
				return nil, info, fmt.Errorf("wav: unsupported %v", info)
			}
			frames := size / (2 * info.channels)
			pcm := make([]int16, frames)
			for i := range pcm {
				sum := 0
				for c := 0; c < info.channels; c++ {
					sum += int(int16(binary.LittleEndian.Uint16(b[2*(i*info.channels+c):])))
				}
				pcm[i] = int16(sum / info.channels)
			}
			return pcm, info, nil
		}
		b = b[min(size+size&1, len(b)):] // chunks are padded to an even size
	}
	return nil, info, errors.New("wav: no data chunk")
}

// writeWAV makes a mono 16-bit PCM WAV.
func writeWAV(pcm []int16, rate int) []byte {
	b := make([]byte, 44, 44+2*len(pcm))
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(36+2*len(pcm)))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], uint32(rate))
	binary.LittleEndian.PutUint32(b[28:], uint32(2*rate))
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(2*len(pcm)))
	for _, s := range pcm {
		b = binary.LittleEndian.AppendUint16(b, uint16(s))
	}
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

// waveform is the 64 peaks of a voice note, 0 to 100 against the loudest (to verify against
// the main phone's bubble in step 4.3).
func waveform(pcm []int16) []byte {
	peaks := make([]int, 64)
	most := 1
	for i, s := range pcm {
		v := int(s)
		if v < 0 {
			v = -v
		}
		k := i * 64 / len(pcm)
		peaks[k] = max(peaks[k], v)
		most = max(most, v)
	}
	out := make([]byte, 64)
	for i, p := range peaks {
		out[i] = byte(p * 100 / most)
	}
	return out
}
