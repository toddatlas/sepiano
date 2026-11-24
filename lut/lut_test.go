package lut

import (
	"encoding/binary"
	"testing"
)

const keys = 88

// fake builds a table with the Steinway B-SE layout and made-up values.
func fake() []byte {
	b := make([]byte, 2048)
	for i := range b {
		b[i] = 0xFF
	}
	le := func(i, v int) { binary.LittleEndian.PutUint16(b[i:], uint16(v)) }
	offs := []int{0x34, 0x44, 0x2AC, 0x514, 0x516, 0x56E, 0x574, 0x624, 0x67C, 0x67D, 0x6D7, 0x6E0, 0x73B, 0x748}
	length := 0x751
	le(0, Version)
	le(2, length)
	b[4], b[5] = 21, 108
	for i, o := range offs {
		le(6+2*i, o)
	}
	copy(b[0x22:], "Test B-SE\r\n\x7F") // 12 bytes: the trim pointer follows at 2EH
	le(0x2E, 0x067E)
	for i := 0x34; i < length; i++ {
		b[i] = 0
	}
	for i, v := range []int{30, 40, 64, 100, 160, 256, 400} {
		le(0x34+2*i, v)
	}
	sec1 := []int{28, 36, 48, 64, 80, 108, 136}
	sec2 := []int{18, 30, 48, 68, 100, 128, 160}
	for r := 0; r < 7; r++ {
		for k := 0; k < keys; k++ {
			b[0x44+r*keys+k] = byte(sec1[r])
			b[0x2AC+r*keys+k] = byte(sec2[r])
		}
	}
	b[0x2AC+6*keys+0] = 110                        // key A0: section 2 falls from row 5 (128) to row 6
	b[0x67D], b[0x67E] = 100, 0xFF                 // section 9: a byte, then the trims' flag
	b[0x67F+34] = 7                                // G3 trimmed by +7
	b[0x6D7+1], b[0x6D7+2], b[0x6D7+4] = 28, 28, 1 // calibration range E1–E1, calibrated
	b[0x73B+2] = 0x08                              // serial
	b[0x748+1], b[0x748+2] = 0xFF, 0               // discard silent, no German names
	return b
}

func TestParse(t *testing.T) {
	tb, err := Parse(fake())
	if err != nil {
		t.Fatal(err)
	}
	if tb.Version != Version || tb.Length != 0x751 || tb.Name != "Test B-SE" || tb.Bottom != 21 || tb.Top != 108 || tb.Keys() != 88 {
		t.Errorf("header: %+v", tb)
	}
	if tb.TrimAt != 0x067E {
		t.Errorf("trim pointer %#x", tb.TrimAt)
	}
	if got := len(tb.Section(1)); got != 7*keys {
		t.Errorf("section 1: %d bytes", got)
	}
	if g := tb.Grid(1); len(g) != 7 || g[0][5] != 28 || g[6][87] != 136 {
		t.Errorf("grid: %d rows", len(g))
	}
	if g := tb.Grid(9); g != nil {
		t.Error("section 9 (a byte, a flag, 88 trims) is not a grid")
	}
	if got := tb.Breakpoints(); len(got) != 7 || got[0] != 30 || got[6] != 400 {
		t.Errorf("breakpoints %v", got)
	}
	want := Settings{Serial: 8, DiscardSilent: true, FirstNote: 28, LastNote: 28}
	if s := tb.Settings(); s != want {
		t.Errorf("settings %+v, want %+v", s, want)
	}
	on, trims := tb.Trims()
	if !on || len(trims) != keys || trims[34] != 7 || trims[0] != 0 {
		t.Errorf("trims on=%v %v", on, trims)
	}
	if s := tb.SectionOf(0x10); s != -1 {
		t.Errorf("SectionOf header = %d", s)
	}
	if s := tb.SectionOf(0x2AC); s != 2 {
		t.Errorf("SectionOf 2ACH = %d", s)
	}
}

func TestLookup(t *testing.T) {
	tb, err := Parse(fake())
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		note, vel, lead, drive int
	}{
		{60, 30, 28, 18},    // on the first breakpoint
		{60, 40, 36, 30},    // on the second
		{60, 35, 32, 24},    // halfway: 28+4, 18+6
		{60, 52, 42, 39},    // halfway between 40 and 64
		{60, 400, 136, 160}, // the last breakpoint
		{60, 544, 164, 192}, // past it: extend 256..400 (+28, +32 per 144)
		{60, 20, 20, 6},     // below the first: extend 30..40 downwards
		{60, 0, 4, 0},       // clamped at 0
		{21, 300, 117, 128}, // A0: lead 108 + 44×28/144; drive stays at row 5, which row 6 is below
	}
	for _, c := range cases {
		lead, drive, err := tb.Lookup(c.note, c.vel)
		if err != nil || lead != c.lead || drive != c.drive {
			t.Errorf("Lookup(%d, %d) = %d, %d, %v; want %d, %d", c.note, c.vel, lead, drive, err, c.lead, c.drive)
		}
	}
	if _, _, err := tb.Lookup(20, 100); err == nil {
		t.Error("accepted a note below the keyboard")
	}
}

func TestParseRejects(t *testing.T) {
	b := fake()
	binary.LittleEndian.PutUint16(b[2:], 4000)
	if _, err := Parse(b); err == nil {
		t.Error("accepted a length past the image")
	}
	b = fake()
	binary.LittleEndian.PutUint16(b[8:], 0x30) // section 1 before section 0
	if _, err := Parse(b); err == nil {
		t.Error("accepted sections out of order")
	}
	b = fake()
	b[4], b[5] = 108, 21
	if _, err := Parse(b); err == nil {
		t.Error("accepted a top note below the bottom")
	}
}

func TestNoteName(t *testing.T) {
	for n, want := range map[int]string{21: "A0", 28: "E1", 60: "C4", 61: "C#4", 108: "C8"} {
		if got := NoteName(n); got != want {
			t.Errorf("NoteName(%d) = %s, want %s", n, got, want)
		}
	}
}

func TestTrimVelocity(t *testing.T) {
	cases := []struct{ v, trim, want int }{
		{100, 0, 100},
		{100, 7, 97},     // speed 1280 + 35 = 1315 → 97.3
		{256, 7, 239},    // 500 + 35 = 535 → 239.3
		{400, 7, 361},    // 320 + 35 = 355 → 360.6
		{100, -7, 103},   // 1280 − 35 = 1245 → 102.8
		{30, 10, 30},     // 4267 + 50 = 4317 → 29.65
		{1000, -1, 1023}, // 128 − 5 = 123, under 126
	}
	for _, c := range cases {
		if got := TrimVelocity(c.v, c.trim); got != c.want {
			t.Errorf("TrimVelocity(%d, %+d) = %d, want %d", c.v, c.trim, got, c.want)
		}
	}
}
