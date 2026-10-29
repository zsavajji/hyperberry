package main

import (
	"encoding/binary"
	"errors"
)

// The Ogg container of a voice note (PLAN.md §7): one logical stream, pages of whole packets.
// Writing never splits a packet across pages; reading accepts any valid stream.

var oggCRCTable = func() (t [256]uint32) {
	for i := range t {
		r := uint32(i) << 24
		for j := 0; j < 8; j++ {
			if r&0x80000000 != 0 {
				r = r<<1 ^ 0x04c11db7 // Ogg's CRC: not reflected, no final xor
			} else {
				r <<= 1
			}
		}
		t[i] = r
	}
	return
}()

func oggCRC(b []byte) uint32 {
	var c uint32
	for _, x := range b {
		c = c<<8 ^ oggCRCTable[byte(c>>24)^x]
	}
	return c
}

const (
	oggContinued = 1
	oggFirst     = 2
	oggLast      = 4
)

// oggWriter appends pages to buf. The serial is fixed, so the same input always gives the same
// bytes.
type oggWriter struct {
	buf []byte
	seq uint32
}

// page writes packets as one page. They must fit in its 255 lacing values.
func (o *oggWriter) page(packets [][]byte, granule int64, flags byte) {
	var lacing, body []byte
	for _, p := range packets {
		for n := len(p); ; n -= 255 {
			if n < 255 {
				lacing = append(lacing, byte(n))
				break
			}
			lacing = append(lacing, 255)
		}
		body = append(body, p...)
	}
	h := make([]byte, 27, 27+len(lacing)+len(body))
	copy(h, "OggS")
	h[5] = flags
	binary.LittleEndian.PutUint64(h[6:], uint64(granule))
	binary.LittleEndian.PutUint32(h[14:], 0x48425652) // serial "HBVR"
	binary.LittleEndian.PutUint32(h[18:], o.seq)
	h[26] = byte(len(lacing))
	h = append(append(h, lacing...), body...)
	binary.LittleEndian.PutUint32(h[22:], oggCRC(h))
	o.buf = append(o.buf, h...)
	o.seq++
}

// lacingSize is how many lacing values a packet takes.
func lacingSize(p []byte) int { return len(p)/255 + 1 }

type oggPacket struct {
	data    []byte
	granule int64 // of the page the packet ends on
}

// oggPackets splits a stream into its packets, joining those continued across pages.
func oggPackets(b []byte) ([]oggPacket, error) {
	var out []oggPacket
	var partial []byte
	for len(b) > 0 {
		if len(b) < 27 || string(b[:4]) != "OggS" {
			return nil, errors.New("ogg: bad page")
		}
		n := int(b[26])
		if len(b) < 27+n {
			return nil, errors.New("ogg: short page")
		}
		lacing := b[27 : 27+n]
		size := 27 + n
		for _, l := range lacing {
			size += int(l)
		}
		if len(b) < size {
			return nil, errors.New("ogg: short page")
		}
		page := append([]byte(nil), b[:size]...)
		binary.LittleEndian.PutUint32(page[22:], 0)
		if oggCRC(page) != binary.LittleEndian.Uint32(b[22:]) {
			return nil, errors.New("ogg: bad checksum")
		}
		granule := int64(binary.LittleEndian.Uint64(b[6:]))
		body := b[27+n : size]
		for _, l := range lacing {
			partial = append(partial, body[:l]...)
			body = body[l:]
			if l < 255 {
				out = append(out, oggPacket{partial, granule})
				partial = nil
			}
		}
		b = b[size:]
	}
	return out, nil
}
