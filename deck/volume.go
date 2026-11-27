package deck

import (
	"io"
	"math"
	"os"

	"sepiano/se"
)

// The level on the Pi (Config.PlayerLevel false). The player's own level
// command is never used: the Pi makes each note softer as it sends the key
// stream, with the same 0 to −16 steps the app always showed. The piano
// reads a few seconds ahead, so a change is heard after that.
const (
	// MinAppLevel is the lowest step.
	MinAppLevel = -16
	// appLevelStep: each step down makes the hammers 6% slower (an SE
	// velocity, a time, 6% bigger). −16 is 2.54 times the velocity.
	appLevelStep = 1.06
	// SoftLimit: a note made softer on the Pi goes no softer than this, or
	// than it was recorded. 400 is the softest velocity this piano's
	// calibration covers (section 0's last breakpoint), so quiet notes
	// still sound instead of fading below what the piano can play.
	SoftLimit = 400
	// silentVelocity: 0, or this or more, is a silent press (play.exe
	// 1044H). Those are left alone, so they stay silent.
	silentVelocity = 640
)

// Subdue is what velocity v becomes at the given level (0 to MinAppLevel).
func Subdue(v uint16, level int) uint16 {
	if level >= 0 || v == 0 || v >= silentVelocity {
		return v
	}
	level = max(level, MinAppLevel)
	s := math.Round(float64(v) * math.Pow(appLevelStep, float64(-level)))
	return uint16(min(s, max(float64(v), SoftLimit)))
}

// openPerf gives the player a performance file. The key stream (.kb) is
// encoded as it is read, each note with the level of that moment, so a
// change reaches everything not yet sent. A .kb that cannot be parsed is
// sent as it is, and so are the pedal files.
func (d *Deck) openPerf(i int, path string) (io.ReadCloser, error) {
	if i != 0 {
		return os.Open(path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	kb, err := se.ParseKB(data)
	if err != nil {
		d.logf("deck: %s: %v; sent without the level", path, err)
		return os.Open(path)
	}
	limits := d.strike.Load()
	r, err := kb.Reader(true, func(e se.Event) uint16 {
		v := Subdue(e.Vel, int(d.vol.Load()))
		// Never louder than the key can strike (SetStrikeLimits).
		if limits != nil && v > 0 && v < silentVelocity && int(e.Key) < len(limits) {
			if m := limits[e.Key]; m > 0 && v < m {
				v = m
				d.atLimit.Add(1)
			}
		}
		return v
	})
	if err != nil {
		d.logf("deck: %s: %v; sent without the level", path, err)
		return os.Open(path)
	}
	return io.NopCloser(r), nil
}

// SetStrikeLimits sets, per key, the loudest SE velocity the piano can
// strike (piano.StrikeLimits; nil: none). A louder note asks the firmware for
// a strike level of 0, and the player stops with "PROGRAM ERROR AT ADDRESS
// BEDA"; every note the player is sent is kept within the limit. It applies
// from the next piece.
func (d *Deck) SetStrikeLimits(l *[128]uint16) { d.strike.Store(l) }

// applyMethod puts the chosen level method (SetLevelMethod) into effect.
// The run loop calls it only when no piece is playing: at the start of a
// piece and after one, so a piece never changes method halfway.
func (d *Deck) applyMethod() {
	d.st.LevelMethodPending = false
	if d.playerLevel == d.wantPlayerLevel {
		return
	}
	d.playerLevel = d.wantPlayerLevel
	if d.playerLevel {
		// The player's own: its lowest level is found as it is used.
		d.st.LevelOnPi, d.st.MinLevel = false, nil
		d.vol.Store(0)
		return
	}
	m := MinAppLevel
	d.st.LevelOnPi, d.st.MinLevel = true, &m
	d.setTarget(min(max(d.st.TargetLevel, MinAppLevel), 0))
	d.st.Level = d.st.TargetLevel
	d.vol.Store(int32(d.st.TargetLevel))
}
