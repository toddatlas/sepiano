// Package lut reads the SE instrument's lookup table: the EEPROM image that
// getlut.exe retrieves and putlut.exe installs. docs/SUITE-PROGRAMS.md
// describes the layout, as the firmware itself uses it.
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

const nameOffset = 0x22

// Table is a parsed lookup table.
type Table struct {
	Raw      []byte  // the whole EEPROM image
	Version  uint16  // word 0
	Length   int     // word 1: the table's length in bytes
	Bottom   int     // byte 4: the MIDI note of the first key (21, A0, on a Steinway)
	Top      int     // byte 5: the MIDI note of the last key (108, C8)
	Sections [14]int // offsets of the sections, from the directory at 6
	Name     string  // the model's name, at 22H
	TrimAt   int     // the word after the name: where section 9's trims start (their on/off flag)
}

// Parse checks the header and reads the directory, as the firmware's
// table check (FW#66) does.
func Parse(b []byte) (*Table, error) {
	if len(b) < 0x40 {
		return nil, errors.New("lut: too short for a table header")
	}
	t := &Table{Raw: b}
	w := func(i int) int { return int(binary.LittleEndian.Uint16(b[i:])) }
	t.Version, t.Length, t.Bottom, t.Top = uint16(w(0)), w(2), int(b[4]), int(b[5])
	if t.Length > len(b) || t.Length < 0x40 {
		return nil, fmt.Errorf("lut: length %d does not fit the %d-byte image", t.Length, len(b))
	}
	if t.Top < t.Bottom || t.Top-t.Bottom+1 > 255 {
		return nil, fmt.Errorf("lut: key range %d-%d", t.Bottom, t.Top)
	}
	prev := nameOffset
	for i := range t.Sections {
		t.Sections[i] = w(6 + 2*i)
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
	if end+2 < t.Sections[0] {
		t.TrimAt = w(end + 1)
	}
	return t, nil
}

// Keys is the number of keys the per-key sections hold, bottom note first.
func (t *Table) Keys() int { return t.Top - t.Bottom + 1 }

// Section returns section i's bytes: from its offset to the next section,
// or to the end of the table.
func (t *Table) Section(i int) []byte {
	end := t.Length
	if i+1 < len(t.Sections) {
		end = t.Sections[i+1]
	}
	return t.Raw[t.Sections[i]:end]
}

// SectionOf says which section holds byte offset off, -1 for the header,
// or len(Sections) past the table.
func (t *Table) SectionOf(off int) int {
	if off >= t.Length {
		return len(t.Sections)
	}
	s := -1
	for i, o := range t.Sections {
		if off >= o {
			s = i
		}
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

// Sections 10 to 13 are length-prefixed blocks the programs copy into
// memory (FW#78 to FW#81); setup.exe's fields are bytes in 10, 12 and 13.
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

// Breakpoints is section 0: SE velocities, ending in 0. Sections 1 and 2
// have one row per breakpoint.
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

// Grid returns a section as rows of Keys values (row-major), or nil when
// its size isn't a multiple of Keys.
func (t *Table) Grid(i int) [][]byte {
	s, k := t.Section(i), t.Keys()
	if len(s) == 0 || len(s)%k != 0 {
		return nil
	}
	var rows [][]byte
	for r := 0; r < len(s); r += k {
		rows = append(rows, s[r:r+k])
	}
	return rows
}

// Trims returns section 9's per-key velocity trims (signed) and whether
// the firmware applies them (their flag byte is not 0).
func (t *Table) Trims() (on bool, trims []int8) {
	if t.TrimAt == 0 || t.TrimAt+t.Keys() >= t.Length {
		return false, nil
	}
	on = t.Raw[t.TrimAt] != 0
	for k := 0; k < t.Keys(); k++ {
		trims = append(trims, int8(t.Raw[t.TrimAt+1+k]))
	}
	return on, trims
}

// Lookup computes what the firmware's FW#71 gives a player for a key and
// an SE velocity, before any trim: the lead time from section 1 (the ticks
// from the strike command to the hammer reaching the sensor) and the strike
// level from section 2 (the actuator value; smaller is harder), each
// interpolated linearly between the two breakpoints around the velocity,
// or extrapolated from the end ones. Like the firmware it never lets a row
// fall between breakpoints, rounds to nearest, and fails above 255.
func (t *Table) Lookup(note, velocity int) (lead, level int, err error) {
	k := note - t.Bottom
	if k < 0 || k >= t.Keys() {
		return 0, 0, fmt.Errorf("lut: note %d is outside %d-%d", note, t.Bottom, t.Top)
	}
	bp := t.Breakpoints()
	if len(bp) < 2 {
		return 0, 0, errors.New("lut: fewer than two breakpoints")
	}
	i := 0
	for i+1 < len(bp) && velocity >= bp[i+1] {
		i++
	}
	if i == len(bp)-1 {
		i-- // past the last breakpoint: extend the last interval
	}
	width := bp[i+1] - bp[i]
	dv, below := velocity-bp[i], false
	if dv < 0 {
		dv, below = -dv, true
	}
	one := func(sec int) (int, error) {
		g := t.Grid(sec)
		if len(g) <= i+1 {
			return 0, fmt.Errorf("lut: section %d has %d rows", sec, len(g))
		}
		a, b := int(g[i][k]), int(g[i+1][k])
		rise := max(b-a, 0)
		step := (2*dv*rise + width) / (2 * width) // rounded to nearest
		if step > 255 {
			return 0, fmt.Errorf("lut: step %d overflows (the firmware stops with an error)", step)
		}
		if below {
			return max(a-step, 0), nil
		}
		return min(a+step, 255), nil
	}
	if lead, err = one(1); err != nil {
		return 0, 0, err
	}
	level, err = one(2)
	return lead, level, err
}

var noteNames = [12]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}

// NoteName names a MIDI note: 21 is A0, 60 is C4, 108 is C8.
func NoteName(n int) string {
	return fmt.Sprintf("%s%d", noteNames[n%12], n/12-1)
}

// TrimVelocity applies a section 9 trim the way FW#71 does before its
// lookup. The SE velocity becomes a hammer speed, 128000 / velocity, and
// each trim step adds 5 to it: a positive trim is a faster hammer, so a
// louder note, with more effect on soft notes than on loud ones. The speed
// then becomes a velocity again. A speed under 126 gives 1023, the softest.
// Divisions round to nearest, as in the firmware.
func TrimVelocity(velocity, trim int) int {
	if trim == 0 || velocity < 2 {
		return velocity // the firmware does not trim 0; it stops on 1
	}
	speed := divRound(128000, velocity) + 5*trim
	if speed < 126 {
		return 1023
	}
	return divRound(128000, speed)
}

func divRound(a, b int) int { return (2*a + b) / (2 * b) }
