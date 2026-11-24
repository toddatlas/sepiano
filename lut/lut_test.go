package lut

import (
	"encoding/binary"
	"testing"
)

// fake builds a table with the prototype tables' layout, made-up values.
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
	le(4, 0x1234)
	for i, o := range offs {
		le(6+2*i, o)
	}
	copy(b[0x22:], "Test B-SE\r\n\x7F")
	for i := 0x34; i < length; i++ {
		b[i] = 0
	}
	for i, v := range []int{25, 40, 64, 100, 160, 256, 400} {
		le(0x34+2*i, v)
	}
	for k := 0; k < Keys; k++ {
		b[0x44+k] = byte(k)          // section 1, row 0
		b[0x44+Keys*6+k] = byte(200) // section 1, row 6
	}
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
	if tb.Version != Version || tb.Length != 0x751 || tb.Name != "Test B-SE" || tb.Word2 != 0x1234 {
		t.Errorf("header: %+v", tb)
	}
	if got := len(tb.Section(1)); got != 7*Keys {
		t.Errorf("section 1: %d bytes", got)
	}
	if g := tb.Grid(1); len(g) != 7 || g[0][5] != 5 || g[6][87] != 200 {
		t.Errorf("grid: %d rows", len(g))
	}
	if g := tb.Grid(3); g != nil {
		t.Error("a 2-byte section is not a grid")
	}
	if got := tb.Breakpoints(); len(got) != 7 || got[0] != 25 || got[6] != 400 {
		t.Errorf("breakpoints %v", got)
	}
	want := Settings{Serial: 8, DiscardSilent: true, FirstNote: 28, LastNote: 28}
	if s := tb.Settings(); s != want {
		t.Errorf("settings %+v, want %+v", s, want)
	}
	if s := tb.SectionOf(0x10); s != -1 {
		t.Errorf("SectionOf header = %d", s)
	}
	if s := tb.SectionOf(0x2AC); s != 2 {
		t.Errorf("SectionOf 2ACH = %d", s)
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
}

func TestNoteName(t *testing.T) {
	for n, want := range map[int]string{21: "A0", 28: "E1", 60: "C4", 61: "C#4", 108: "C8"} {
		if got := NoteName(n); got != want {
			t.Errorf("NoteName(%d) = %s, want %s", n, got, want)
		}
	}
}
