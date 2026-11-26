package piano

import (
	"sort"

	"sepiano/se"
)

// Work is how hard a performance works the piano's key solenoids
// (docs/ROADMAP.md, idea 2: a stand-in for heat and wear). Each key the
// player holds down is a solenoid kept powered, so the figures are about
// holding: how long, how many at once, and for how long at a stretch.
// Silent presses count too: they hold a key without sounding it.
type Work struct {
	Notes     int     `json:"notes"`
	DurationS float64 `json:"duration_s"`
	// HoldS is the time all keys spend held down, added up (key-seconds).
	HoldS float64 `json:"hold_s"`
	// AvgHeld is the average number of keys held at once (HoldS over the
	// piece's length).
	AvgHeld float64 `json:"avg_held"`
	// PeakHeld is the most keys held at once; Held1s the most held
	// together for at least a second, Held10s for at least ten.
	PeakHeld int `json:"peak_held"`
	Held1s   int `json:"held_1s"`
	Held10s  int `json:"held_10s"`
	// LongestHoldS is the longest a single key stays down, and LongestKey
	// that key (MIDI number).
	LongestHoldS float64 `json:"longest_hold_s"`
	LongestKey   int     `json:"longest_key"`
	// LongHoldS is the holding beyond LongHold in each note, added up: the
	// sustained part, where heat builds.
	LongHoldS float64 `json:"long_hold_s"`
	// PedalFreeS is the holding that changes nothing in the sound: a key
	// still held while the damper pedal is fully down until it is let go
	// (the pedal holds its damper up anyway), after StrikeTime. The room a
	// "cool-running" playback could save; not used yet.
	PedalFreeS float64 `json:"pedal_free_s"`
	// Level sums it up for people: "light", "moderate", "heavy" or "very
	// heavy" (WorkLevel).
	Level string `json:"level"`
}

const (
	// LongHold: holding beyond this, per note, counts as sustained.
	LongHold = 4.0 // seconds
	// FullPedal: a damper sample at or above this (90%) counts as fully
	// down, a cautious choice: the owner's recordings sit there a third of
	// the time the pedal is pressed.
	FullPedal = 230
	// StrikeTime: how long a key must stay down to strike before it could be
	// let go under a full pedal.
	StrikeTime = 0.15 // seconds
)

// MeasureWork measures a performance.
func MeasureWork(p *se.Performance) Work {
	sec := func(t uint32) float64 { return float64(t) / se.TicksPerSecond }
	type hold struct{ from, to uint32 }
	var holds []hold
	var keys []int
	down := map[uint8]uint32{}
	var end uint32
	for _, e := range p.KB.Events {
		end = max(end, e.Tick)
		if e.Filler() {
			continue
		}
		if t, ok := down[e.Key]; ok {
			// A release, or a re-strike without one: the hold ends here.
			holds = append(holds, hold{t, e.Tick})
			keys = append(keys, int(e.Key))
			delete(down, e.Key)
		}
		if e.On {
			down[e.Key] = e.Tick
		}
	}
	end = max(end, p.KB.End)
	for k, t := range down { // still down at the end
		holds = append(holds, hold{t, end})
		keys = append(keys, int(k))
	}
	w := Work{DurationS: sec(end)}
	for _, e := range p.KB.Events {
		if e.On && !e.Filler() {
			w.Notes++
		}
	}
	// The damper's last not-fully-down sample at or before each moment.
	var damper []byte
	if p.Damper != nil {
		damper = p.Damper.Samples
	}
	lastUp := make([]int, len(damper)) // index of the last sample < FullPedal, or -1
	prev := -1
	for i, v := range damper {
		if v < FullPedal {
			prev = i
		}
		lastUp[i] = prev
	}
	type edge struct {
		t  uint32
		up int
	}
	var edges []edge
	for i, h := range holds {
		d := sec(h.to - h.from)
		w.HoldS += d
		if d > w.LongestHoldS {
			w.LongestHoldS, w.LongestKey = d, keys[i]
		}
		if d > LongHold {
			w.LongHoldS += d - LongHold
		}
		// Under a full pedal from some point until the release?
		if n := len(damper); n > 0 && h.to > h.from {
			at := min(int((h.to-1)/se.DamperPeriod), n-1)
			if damper[at] >= FullPedal {
				from := sec(h.from) + StrikeTime
				if lu := lastUp[at]; lu >= 0 {
					from = max(from, float64((lu+1)*se.DamperPeriod)/se.TicksPerSecond)
				}
				if s := sec(h.to) - from; s > 0 {
					w.PedalFreeS += s
				}
			}
		}
		edges = append(edges, edge{h.from, 1}, edge{h.to, -1})
	}
	if w.DurationS > 0 {
		w.AvgHeld = w.HoldS / w.DurationS
	}
	// How many keys are held at once, and for how long at each count.
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].t != edges[j].t {
			return edges[i].t < edges[j].t
		}
		return edges[i].up < edges[j].up // releases first at the same tick
	})
	// runs[n] is the longest stretch with at least n keys held.
	held := 0
	since := map[int]uint32{}
	runs := map[int]float64{}
	for _, e := range edges {
		if e.up > 0 {
			held++
			since[held] = e.t
			w.PeakHeld = max(w.PeakHeld, held)
		} else {
			if s, ok := since[held]; ok {
				runs[held] = max(runs[held], sec(e.t-s))
				delete(since, held)
			}
			held--
		}
	}
	for n := w.PeakHeld; n >= 1; n-- {
		// At least n held for a stretch means at least n−1 for as long.
		if runs[n+1] > runs[n] {
			runs[n] = runs[n+1]
		}
		if w.Held1s == 0 && runs[n] >= 1 {
			w.Held1s = n
		}
		if w.Held10s == 0 && runs[n] >= 10 {
			w.Held10s = n
		}
	}
	w.Level = WorkLevel(w)
	return w
}

// WorkLevel sums up a piece's work against what this piano usually plays:
// the owner's 350 Zenph SE files (docs/LIBRARIES.md, "How hard a piece
// works the piano"). In those, 90% hold under 2.8 keys at once on average
// (the most is 4.1), at most one key held for ten seconds, and no key past
// 10 s (the longest is 19 s).
//   - light: within what they usually do;
//   - moderate: up to the most they ever do;
//   - heavy: up to about twice that;
//   - very heavy: beyond.
func WorkLevel(w Work) string {
	switch {
	case w.AvgHeld >= 8 || w.Held10s > 10 || w.LongestHoldS >= 40:
		return "very heavy"
	case w.AvgHeld >= 4.2 || w.Held10s > 5 || w.LongestHoldS >= 20:
		return "heavy"
	case w.AvgHeld >= 2.8 || w.Held10s > 1 || w.LongestHoldS >= 10:
		return "moderate"
	}
	return "light"
}
