package piano

import (
	"sepiano/lut"
	"sepiano/se"
)

// StrikeLimits are, for each key, the loudest SE velocity this piano can
// strike (SE velocity: smaller is louder). FW#71 turns a note's velocity
// into a strike level from the piano's table (section 2; smaller is
// harder), extending the table's line below its loudest breakpoint. For a
// loud enough note that reaches 0, and FW#17 then stops the player with
// "PROGRAM ERROR AT ADDRESS BEDA": a MAESTRO piece stopped that way on
// 2026-11-27, at a low E1 of velocity 21 where E1's limit is 25. MIDI
// sources often hold such notes (about 400 of MAESTRO's 1,276 pieces);
// the owner's 350 Zenph files never do (their loudest note is 24, and every
// note keeps a step of margin). A note louder than its key's limit plays
// at the limit. 0 means no limit.
type StrikeLimits [128]uint16

// MinStrikeLevel is the hardest strike level a note may ask for: one step of
// margin above the firmware's refused 0.
const MinStrikeLevel = 2

// NewStrikeLimits works the limits out from the piano's table, trims
// included, as FW#71 does.
func NewStrikeLimits(t *lut.Table) StrikeLimits {
	var l StrikeLimits
	on, trims := t.Trims()
	for note := t.Bottom; note <= t.Top; note++ {
		if note < 0 || note >= len(l) {
			continue
		}
		for v := 2; v < SilentVelocity; v++ {
			tv := v
			if on && note-t.Bottom < len(trims) {
				tv = lut.TrimVelocity(v, int(trims[note-t.Bottom]))
			}
			if _, level, err := t.Lookup(note, tv); err == nil && level >= MinStrikeLevel {
				l[note] = uint16(v)
				break
			}
		}
	}
	return l
}

// Velocity is vel, or the key's limit when vel is louder. Silent presses (0,
// or SilentVelocity and more) stay as they are.
func (l *StrikeLimits) Velocity(key uint8, vel uint16) uint16 {
	if l == nil || vel == 0 || vel >= SilentVelocity || int(key) >= len(l) {
		return vel
	}
	if m := l[key]; m > 0 && vel < m {
		return m
	}
	return vel
}

// TooLoud counts the notes of p louder than their key's limit.
func (l *StrikeLimits) TooLoud(p *se.Performance) int {
	n := 0
	for _, e := range p.KB.Events {
		if e.On && !e.Filler() && l.Velocity(e.Key, e.Vel) != e.Vel {
			n++
		}
	}
	return n
}
