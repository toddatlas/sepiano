package piano

import (
	"math"
	"sort"

	"sepiano/edit"
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

	// NoReading counts, by key, the presses with velocity 0: the key went
	// down but the recorder measured no hammer speed. The player plays such
	// a note as a silent press (play.exe 1044H), so if the key sounded when
	// it was recorded, the note is lost on playback.
	NoReading []KeyCount `json:"no_reading"`
}

// KeyCount is a number of notes on one key.
type KeyCount struct {
	Note  int    `json:"note"`
	Name  string `json:"name"`
	Count int    `json:"count"`
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
	zero := map[int]int{}
	var all []int
	for _, e := range p.KB.Events {
		if !e.On || e.Filler() {
			continue
		}
		if e.Vel == 0 {
			zero[int(e.Key)]++
			continue
		}
		byKey[int(e.Key)] = append(byKey[int(e.Key)], int(e.Vel))
		all = append(all, int(e.Vel))
	}
	c := KeyCheck{Notes: len(all), Threshold: CheckThreshold, Keys: []KeyStat{}, Outliers: []KeyStat{}, NoReading: []KeyCount{}}
	for n := 0; n < 128; n++ {
		if zero[n] > 0 {
			c.NoReading = append(c.NoReading, KeyCount{Note: n, Name: lut.NoteName(n), Count: zero[n]})
		}
	}
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

// ReleaseCheck compares when each key lets go with the other keys of the
// chords it is played in. Its use is a recording made to check the release
// sensors: chords pressed and released together, several times. A key whose
// releases come regularly before or after the rest of its chords has a
// release sensor that fires early or late; one played that way by hand
// varies from chord to chord instead.
type ReleaseCheck struct {
	Chords    int           `json:"chords"`       // chords of three or more keys
	Keys      []ReleaseStat `json:"keys"`         // keys in those chords, low to high
	Outliers  []ReleaseStat `json:"outliers"`     // keys regularly early or late, worst first
	Threshold int           `json:"threshold_ms"` // how early or late counts
	Window    int           `json:"window_ms"`    // presses this close together make a chord
}

// ReleaseStat is one key's releases against the median release of each
// chord it is in, in milliseconds: negative is early.
type ReleaseStat struct {
	Note   int    `json:"note"`
	Name   string `json:"name"`
	Count  int    `json:"count"`
	Median int    `json:"median_ms"`
	Min    int    `json:"min_ms"`
	Max    int    `json:"max_ms"`
}

// Release check limits: presses within ChordWindow ticks (70 ms) of a
// chord's first make one chord, and a key whose median release is
// ReleaseThreshold ms or more from its chords' counts as early or late.
const (
	ChordWindow      = 56
	ReleaseThreshold = 50
)

// CheckReleases finds the chords in p and how each key lets go in them.
func CheckReleases(p *se.Performance) ReleaseCheck {
	notes := edit.FromPerformance(p).Notes
	c := ReleaseCheck{Threshold: ReleaseThreshold, Window: ChordWindow * 1000 / se.TicksPerSecond,
		Keys: []ReleaseStat{}, Outliers: []ReleaseStat{}}
	byKey := map[int][]int{}
	for i := 0; i < len(notes); {
		j := i + 1
		for j < len(notes) && notes[j].On-notes[i].On <= ChordWindow {
			j++
		}
		chord := notes[i:j]
		i = j
		seen := map[uint8]bool{}
		var offs []int
		var keys []int
		for _, n := range chord {
			if !seen[n.Key] {
				seen[n.Key] = true
				offs = append(offs, int(n.Off))
				keys = append(keys, int(n.Key))
			}
		}
		if len(offs) < 3 {
			continue
		}
		c.Chords++
		ref := median(offs)
		for k, off := range offs {
			byKey[keys[k]] = append(byKey[keys[k]], (off-ref)*1000/se.TicksPerSecond)
		}
	}
	var keys []int
	for n := range byKey {
		keys = append(keys, n)
	}
	sort.Ints(keys)
	for _, n := range keys {
		d := byKey[n]
		sort.Ints(d)
		mid := d[len(d)/2]
		if len(d)%2 == 0 {
			mid = (d[len(d)/2-1] + mid) / 2
		}
		s := ReleaseStat{Note: n, Name: lut.NoteName(n), Count: len(d), Median: mid, Min: d[0], Max: d[len(d)-1]}
		c.Keys = append(c.Keys, s)
		// Early or late, and so in at least two thirds of its chords: a key
		// early once and late once is the hand, not the sensor.
		early, late := 0, 0
		for _, x := range d {
			if x <= -ReleaseThreshold/2 {
				early++
			} else if x >= ReleaseThreshold/2 {
				late++
			}
		}
		if s.Count >= 2 && ((s.Median <= -ReleaseThreshold && 3*early >= 2*s.Count) || (s.Median >= ReleaseThreshold && 3*late >= 2*s.Count)) {
			c.Outliers = append(c.Outliers, s)
		}
	}
	sort.Slice(c.Outliers, func(a, b int) bool {
		return abs(c.Outliers[a].Median) > abs(c.Outliers[b].Median)
	})
	return c
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
