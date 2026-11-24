// Package lut reads the SE instrument's lookup table: the EEPROM image that
// getlut.exe retrieves and putlut.exe installs (docs/SUITE-PROGRAMS.md).
//
// The table holds the piano's calibration and settings. Its header is
// understood; the meaning of most sections is still being worked out, so
// they are exposed as bytes with their shapes.
package lut

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// Version is the only table version play.exe V50.45 accepts. With any
// other, it overwrites the table's first 256 bytes.
const Version = 0x3017

// Keys is the number of keys the per-key sections cover, bottom note first.
const Keys = 88

// BottomNote is the MIDI note of the first key (A0). The prototype tables'
// calibration range runs from it to 108 (C8).
const BottomNote = 21

const nameOffset = 0x22

// Table is a parsed lookup table.
type Table struct {
	Raw      []byte  // the whole EEPROM image
	Version  uint16  // word 0
	Length   int     // word 1: the table's length in bytes
	Word2    uint16  // word 2: not understood yet
	Sections [14]int // offsets of the sections, from the directory at 6
	Name     string  // the model's name, at 22H
}

// Parse checks the header and reads the directory.
func Parse(b []byte) (*Table, error) {
	if len(b) < 0x40 {
		return nil, errors.New("lut: too short for a table header")
	}
	t := &Table{Raw: b}
	w := func(i int) uint16 { return binary.LittleEndian.Uint16(b[i:]) }
	t.Version, t.Length, t.Word2 = w(0), int(w(2)), w(4)
	if t.Length > len(b) || t.Length < 0x40 {
		return nil, fmt.Errorf("lut: length %d does not fit the %d-byte image", t.Length, len(b))
	}
	prev := nameOffset
	for i := range t.Sections {
		t.Sections[i] = int(w(6 + 2*i))
		if t.Sections[i] < prev || t.Sections[i] > t.Length {
			return nil, fmt.Errorf("lut: section %d offset %#x out of order", i, t.Sections[i])
		}
		prev = t.Sections[i]
	}
	end := nameOffset
	for end < t.Sections[0] && b[end] != 0x7F {
		end++
	}
	t.Name = strings.TrimRight(string(b[nameOffset:end]), "\r\n")
	return t, nil
}

// Section returns section i's bytes: from its offset to the next section,
// or to the end of the table.
func (t *Table) Section(i int) []byte {
	end := t.Length
	if i+1 < len(t.Sections) {
		end = t.Sections[i+1]
	}
	return t.Raw[t.Sections[i]:end]
}

// SectionOf says which section holds byte offset off, or -1 for the header.
func (t *Table) SectionOf(off int) int {
	s := -1
	for i, o := range t.Sections {
		if off >= o {
			s = i
		}
	}
	if off >= t.Length {
		return len(t.Sections)
	}
	return s
}

// Settings are the six values setup.exe shows and edits.
type Settings struct {
	Serial           byte // setup prints it as two hex digits
	DiscardSilent    bool
	GermanNames      bool
	FirstNote        byte // first adjustment/calibration note (MIDI number)
	LastNote         byte // last adjustment/calibration note
	FirstCalibration bool // true until the first calibration has been done
}

// The firmware gives setup.exe its fields as offsets from sections 10, 12
// and 13 (firmware entries 61, 63 and 64).
const (
	secRange   = 10
	secSerial  = 12
	secSwitch  = 13
	offFirst   = 1
	offLast    = 2
	offFirstCa = 4
)

// Settings reads setup.exe's fields.
func (t *Table) Settings() Settings {
	r := t.Section(secRange)
	return Settings{
		Serial:           t.Section(secSerial)[2],
		DiscardSilent:    t.Section(secSwitch)[1] != 0,
		GermanNames:      t.Section(secSwitch)[2] != 0,
		FirstNote:        r[offFirst],
		LastNote:         r[offLast],
		FirstCalibration: r[offFirstCa] == 0,
	}
}

// Breakpoints is section 0: a list of words ending in 0. There are as many
// as the rows of the 7×88 sections.
func (t *Table) Breakpoints() []int {
	s := t.Section(0)
	var out []int
	for i := 0; i+1 < len(s); i += 2 {
		v := int(binary.LittleEndian.Uint16(s[i:]))
		if v == 0 {
			break
		}
		out = append(out, v)
	}
	return out
}

// Grid returns a section as rows of Keys values (row-major, one row per
// breakpoint or per quantity), or nil when its size isn't a multiple of
// Keys.
func (t *Table) Grid(i int) [][]byte {
	s := t.Section(i)
	if len(s) == 0 || len(s)%Keys != 0 {
		return nil
	}
	var rows [][]byte
	for r := 0; r < len(s); r += Keys {
		rows = append(rows, s[r:r+Keys])
	}
	return rows
}

var noteNames = [12]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}

// NoteName names a MIDI note: 21 is A0, 60 is C4, 108 is C8.
func NoteName(n int) string {
	return fmt.Sprintf("%s%d", noteNames[n%12], n/12-1)
}
