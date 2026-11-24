package lut

import "encoding/binary"

// Size is the EEPROM's size: a table image is always this long.
const Size = 2048

// Example returns a made-up 2048-byte table with the layout of an 88-key
// table (the Steinway B-SE's), for the simulator and tests. Its calibration
// values are generated, not taken from any real table; the player constants
// in section 11 are the documented ones (docs/SUITE-PROGRAMS.md).
func Example() []byte {
	const keys = 88
	b := make([]byte, Size)
	for i := range b {
		b[i] = 0xFF
	}
	le := func(at, v int) { binary.LittleEndian.PutUint16(b[at:], uint16(v)) }

	// A small deterministic generator for per-key variation.
	seed := uint32(20261123)
	jitter := func(n int) int {
		seed = seed*1664525 + 1013904223
		return int(seed>>16)%(2*n+1) - n
	}

	name := "Simulated SE\r\n\x7F"    // 15 bytes, so the sections start at 34H
	pos := nameOffset + len(name) + 2 // the trim pointer word follows the name
	pos += pos % 2
	sizes := []int{16, 7 * keys, 7 * keys, 2, keys, 6, 2 * keys, keys, 1, 2 + keys, 9, 91, 13, 9}
	offs := make([]int, len(sizes))
	for i, n := range sizes {
		offs[i] = pos
		pos += n
	}
	le(0, Version)
	le(2, pos)
	b[4], b[5] = 21, 108
	for i, o := range offs {
		le(6+2*i, o)
	}
	copy(b[nameOffset:], name)
	le(nameOffset+len(name), offs[9]+1) // the trims start after section 9's first byte
	for i := offs[0]; i < pos; i++ {
		b[i] = 0
	}

	for i, v := range []int{30, 40, 64, 100, 160, 256, 400} {
		le(offs[0]+2*i, v)
	}
	lead := []int{25, 32, 45, 58, 76, 97, 124}
	level := []int{17, 29, 52, 75, 101, 127, 155}
	for k := 0; k < keys; k++ {
		dl, dv := jitter(3), jitter(4)
		for r := 0; r < 7; r++ {
			b[offs[1]+r*keys+k] = byte(lead[r] + dl + r/2)
			b[offs[2]+r*keys+k] = byte(level[r] + dv + r)
		}
		b[offs[4]+k] = byte(28 + jitter(1))
		b[offs[6]+k] = byte(39 + jitter(4))
		b[offs[6]+keys+k] = byte(33 + jitter(2))
		b[offs[7]+k] = byte(33 + jitter(5))
	}
	le(offs[3], 288)
	le(offs[5], 60)
	le(offs[5]+2, 120)
	b[offs[8]] = 0xFF
	b[offs[9]], b[offs[9]+1] = 100, 0xFF // the hold level; trims on
	b[offs[9]+2+62-21] = 4               // D4 trimmed by +4

	s10 := offs[10]
	b[s10], b[s10+1], b[s10+2], b[s10+3], b[s10+4] = 8, 21, 108, 24, 1
	le(s10+5, 192)
	le(s10+7, 384)

	s11 := offs[11]
	b[s11] = 90
	for i, v := range []int{800, 22, 120, 80, 16, 208, 16, 176, 256, 30, 64, 32, 38, 32, 640, 128, 224, 8, 512, 2400, 4800} {
		le(s11+1+2*i, v)
	}
	copy(b[s11+1+58:], []byte{0x40, 0x11, 0x10, 0x07, 0x05, 0x02, 0x01, 0x00})
	for i, v := range []int{240, 256, 256, 256, 256, 256, 288, 480} {
		le(s11+1+74+2*i, v)
	}

	// Section 12: the recording type and serial number (the recorder refuses
	// to record when either is 0), then the recorder's parameters.
	copy(b[offs[12]:], []byte{12, 0x07, 0x01, 0xE0, 0x00, 0x00, 0x01, 0x20, 0x00, 0xA0, 0x00, 0xC8, 0x00})
	b[offs[13]] = 8
	return b
}
