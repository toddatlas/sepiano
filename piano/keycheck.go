package piano

import (
	"math"
	"sort"

	"sepiano/lut"
	"sepiano/se"
)

// KeyCheck describes the velocities each key has in a performance: for a
// recording, what the key's hammer sensor measured. To check the sensors,
// record each key played several times at the same loudness: a key whose
// typical velocity stands apart from its neighbours records too loud or
// too soft. The recorder applies no per-key correction, so this is the
// sensor itself (docs/SUITE-PROGRAMS.md).
type KeyCheck struct {
	Notes     int       `json:"notes"`     // note-ons with a velocity
	Median    int       `json:"median"`    // over all of them
	Keys      []KeyStat `json:"keys"`      // keys that play, low to high
	Outliers  []KeyStat `json:"outliers"`  // keys that stand apart, worst first
	Threshold float64   `json:"threshold"` // the hammer-speed difference counted as standing apart (%)
}

// KeyStat is one key's velocities. Velocity is the SE scale (bigger is
// softer); Deviation compares the key's median hammer speed (128000 /
// velocity) with its neighbours': positive means it records louder.
type KeyStat struct {
	Note      int     `json:"note"`
	Name      string  `json:"name"`
	Count     int     `json:"count"`
	Min       int     `json:"min"`
	Max       int     `json:"max"`
	Median    int     `json:"median"`
	Spread    float64 `json:"spread"`    // the middle half's range in hammer speed, % of the median
	Deviation float64 `json:"deviation"` // % hammer speed against the neighbours' median
}

// CheckThreshold is the hammer-speed difference from the neighbours (%)
// that marks a key as standing apart.
const CheckThreshold = 12.0

// neighbours is how many keys on each side a key is compared with.
const neighbours = 4

// CheckKeys gathers the velocities of every note-on in p.
func CheckKeys(p *se.Performance) KeyCheck {
	byKey := map[int][]int{}
	var all []int
	for _, e := range p.KB.Events {
		if !e.On || e.Filler() || e.Vel == 0 {
			continue
		}
		byKey[int(e.Key)] = append(byKey[int(e.Key)], int(e.Vel))
		all = append(all, int(e.Vel))
	}
	c := KeyCheck{Notes: len(all), Threshold: CheckThreshold, Keys: []KeyStat{}, Outliers: []KeyStat{}}
	if len(all) == 0 {
		return c
	}
	c.Median = median(all)
	var notes []int
	for n := range byKey {
		notes = append(notes, n)
	}
	sort.Ints(notes)
	for _, n := range notes {
		v := byKey[n]
		sort.Ints(v)
		s := KeyStat{Note: n, Name: lut.NoteName(n), Count: len(v), Min: v[0], Max: v[len(v)-1], Median: median(v)}
		if len(v) >= 4 {
			lo, hi := v[len(v)/4], v[len(v)*3/4]
			s.Spread = round1((speed(lo) - speed(hi)) / speed(s.Median) * 100)
		}
		c.Keys = append(c.Keys, s)
	}
	// Each key against the median of its neighbours (the keys within
	// `neighbours` places on each side that play), so a gradual change
	// across the keyboard is not counted against any one key.
	for i := range c.Keys {
		var near []int
		for j := max(0, i-neighbours); j <= min(len(c.Keys)-1, i+neighbours); j++ {
			if j != i {
				near = append(near, c.Keys[j].Median)
			}
		}
		if len(near) < 2 {
			continue
		}
		ref := median(near)
		c.Keys[i].Deviation = round1((speed(c.Keys[i].Median)/speed(ref) - 1) * 100)
		if math.Abs(c.Keys[i].Deviation) >= CheckThreshold && c.Keys[i].Count >= 2 {
			c.Outliers = append(c.Outliers, c.Keys[i])
		}
	}
	sort.Slice(c.Outliers, func(a, b int) bool {
		return math.Abs(c.Outliers[a].Deviation) > math.Abs(c.Outliers[b].Deviation)
	})
	return c
}

func speed(v int) float64 { return 128000 / float64(max(v, 1)) }

func median(v []int) int {
	s := append([]int(nil), v...)
	sort.Ints(s)
	return s[len(s)/2]
}

func round1(x float64) float64 { return math.Round(x*10) / 10 }
