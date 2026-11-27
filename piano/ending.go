package piano

import (
	"sepiano/se"
)

// A gentle ending for the pedals (the owner, 2026-11-27). MIDI recordings
// often end with the sustain pedal dropped in one step as the recording
// stopped (the Disklavier's reset), or with a pedal still down, which the
// piano then lets go all at once when the piece ends. Both are loud and hard
// on the mechanism; the Zenph files almost never do either.
//
// From just before the last note's release, a pedal comes up no faster
// than a full stroke in EndingRelease, and no pedal is left down at the
// end: the piece is made longer by what the release needs.

const (
	// EndingRelease is the time a full pedal stroke takes to come up at
	// the end of a piece.
	EndingRelease = 1.5 // seconds
	// endingWindow: the ending starts this long before the last release, to
	// catch a pedal dropped together with the last notes.
	endingWindow = 0.25 // seconds
	// pedalUp: at or below this a pedal counts as up.
	pedalUp = 10
	// abruptFall: a fall of more than this in one sample is a jump no foot
	// makes: the Zenph files never move a pedal more than PedalStep (60)
	// per sample. Only an ending with such a jump, or with a pedal left
	// down, is changed, so the Zenph endings stay as made.
	abruptFall = PedalStep
)

// GentleEnding slows the pedals' release at the end of p, where needed,
// and returns how many pedal samples it changed. It may make p longer.
func GentleEnding(p *se.Performance) int {
	if p == nil || p.KB == nil {
		return 0
	}
	lastRelease := uint32(0)
	for _, e := range p.KB.Events {
		if !e.Filler() {
			lastRelease = max(lastRelease, e.Tick)
		}
	}
	from := int64(lastRelease) - int64(endingWindow*se.TicksPerSecond)
	end := p.KB.End
	changed := 0
	for _, pd := range []struct {
		ped    *se.Pedal
		period uint32
	}{{p.Damper, se.DamperPeriod}, {p.Soft, se.SoftPeriod}} {
		if pd.ped == nil || len(pd.ped.Samples) == 0 {
			continue
		}
		n, newEnd := gentle(pd.ped, pd.period, from)
		changed += n
		end = max(end, newEnd)
	}
	if end > p.KB.End {
		p.KB.End = end
		// Both pedals reach the new end: holding their last values (up).
		for _, pd := range []struct {
			ped    *se.Pedal
			period uint32
		}{{p.Damper, se.DamperPeriod}, {p.Soft, se.SoftPeriod}} {
			if pd.ped == nil {
				continue
			}
			want := int(end/pd.period) + 2
			for len(pd.ped.Samples) < want {
				var last byte
				if n := len(pd.ped.Samples); n > 0 {
					last = pd.ped.Samples[n-1]
				}
				pd.ped.Samples = append(pd.ped.Samples, last)
			}
		}
	}
	return changed
}

// gentle applies the ending to one pedal from tick `from`: if it is left
// down at the end, or falls abruptly in the ending, its fall is limited to
// the ending's rate from there on, and it is taken up to 0 after its last
// sample. It returns the samples changed and the tick where the pedal is
// up.
func gentle(ped *se.Pedal, period uint32, from int64) (int, uint32) {
	s := ped.Samples
	i0 := max(0, int(from/int64(period)))
	if i0 >= len(s) {
		i0 = len(s) - 1
	}
	needed := s[len(s)-1] > pedalUp
	for i := i0 + 1; i < len(s) && !needed; i++ {
		if int(s[i-1])-int(s[i]) > abruptFall {
			needed = true
		}
	}
	if !needed {
		return 0, 0
	}
	rate := 255 / (EndingRelease * se.TicksPerSecond / float64(period)) // per sample
	changed := 0
	level := float64(s[i0])
	for i := i0 + 1; i < len(s); i++ {
		want := float64(s[i])
		if want < level-rate {
			want = level - rate
		}
		level = want
		if v := byte(want + 0.5); v != s[i] {
			s[i] = v
			changed++
		}
	}
	// Still down at the end: carry on down to 0.
	for level > 0 {
		level = max(0, level-rate)
		s = append(s, byte(level+0.5))
		changed++
	}
	ped.Samples = s
	return changed, uint32(len(s)) * period
}
