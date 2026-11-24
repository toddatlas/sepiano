package piano

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"sepiano/edit"
	"sepiano/lut"
	"sepiano/se"
)

// Corrections put right what this piano's sensors get wrong when it
// records. The recorder writes what each key's sensors measured, with no
// per-key correction (docs/SUITE-PROGRAMS.md): a key whose hammer sensor
// reads slow records too soft, and one whose release sensor fires early
// lets go too soon. Corrections are applied when a recording made on this
// piano plays, or saved into a corrected copy. A recording itself is never
// changed, so the Key check always sees what the sensors measured.
// docs/RECORDING-EDITOR.md.
type Corrections struct {
	On bool `json:"on"` // applied when recordings play
	// FillZero gives each press with no hammer reading (velocity 0, which
	// the player plays as a silent press) the loudness of the notes around
	// it, so a note whose hammer sensor missed it sounds again.
	FillZero bool                  `json:"fill_zero"`
	Keys     map[int]KeyCorrection `json:"keys"`
	Updated  time.Time             `json:"updated"`
}

// KeyCorrection is one key's correction.
type KeyCorrection struct {
	// Speed is a change of hammer speed (128000 / velocity), in percent:
	// +35 plays the key's notes 35% faster, that is louder.
	Speed int `json:"speed"`
	// Release moves the key's releases, in milliseconds: positive is later.
	Release int `json:"release_ms"`
}

// Limits of a correction.
const (
	MinSpeed   = -50
	MaxSpeed   = 100
	MaxRelease = 1000 // ms, either way
)

// FillWindow is how near (in ticks, either side) the notes are whose
// loudness a press with no hammer reading takes.
const FillWindow = se.TicksPerSecond

// Empty reports whether there is nothing to correct.
func (c Corrections) Empty() bool {
	for _, k := range c.Keys {
		if k != (KeyCorrection{}) {
			return false
		}
	}
	return !c.FillZero
}

// Active reports whether recordings are corrected when they play.
func (c Corrections) Active() bool { return c.On && !c.Empty() }

// Clean checks the corrections. A key left at zero stays listed (it does
// nothing) until it is removed, so a row does not vanish while it is set.
func (c Corrections) Clean() (Corrections, error) {
	out := Corrections{On: c.On, FillZero: c.FillZero, Keys: map[int]KeyCorrection{}, Updated: c.Updated}
	for n, k := range c.Keys {
		switch {
		case n < edit.Lowest || n > edit.Highest:
			return out, fmt.Errorf("key %d is not on the keyboard", n)
		case k.Speed < MinSpeed || k.Speed > MaxSpeed:
			return out, fmt.Errorf("%s: loudness %+d%% is out of range (%d to %+d)", lut.NoteName(n), k.Speed, MinSpeed, MaxSpeed)
		case k.Release < -MaxRelease || k.Release > MaxRelease:
			return out, fmt.Errorf("%s: release %+d ms is out of range (±%d)", lut.NoteName(n), k.Release, MaxRelease)
		}
		out.Keys[n] = k
	}
	return out, nil
}

// Notes returns the corrected keys in order.
func (c Corrections) Notes() []int {
	var ns []int
	for n := range c.Keys {
		ns = append(ns, n)
	}
	sort.Ints(ns)
	return ns
}

// Summary says in a line what the corrections do.
func (c Corrections) Summary() string {
	var parts []string
	for _, n := range c.Notes() {
		k := c.Keys[n]
		var what []string
		if k.Speed != 0 {
			what = append(what, fmt.Sprintf("loudness %+d%%", k.Speed))
		}
		if k.Release != 0 {
			what = append(what, fmt.Sprintf("release %+d ms", k.Release))
		}
		if len(what) > 0 {
			parts = append(parts, lut.NoteName(n)+" "+strings.Join(what, ", "))
		}
	}
	if c.FillZero {
		parts = append(parts, "notes with no hammer reading filled in")
	}
	if len(parts) == 0 {
		return "no corrections"
	}
	return strings.Join(parts, "; ")
}

func clampVel(v float64) uint16 {
	return uint16(max(4, min(se.MaxVelocity, math.Round(v))))
}

// Apply corrects the notes of d and returns how many it changed. Each
// key's velocities and releases are corrected first; then, with FillZero,
// each press with no hammer reading takes the median velocity of the
// corrected notes within FillWindow of it (silent presses left out).
func (c Corrections) Apply(d *edit.Doc) int {
	changed := make([]bool, len(d.Notes))
	for i := range d.Notes {
		n := &d.Notes[i]
		k, ok := c.Keys[int(n.Key)]
		if !ok {
			continue
		}
		if k.Speed != 0 && n.Vel > 0 {
			if v := clampVel(float64(n.Vel) * 100 / float64(100+k.Speed)); v != n.Vel {
				n.Vel, changed[i] = v, true
			}
		}
		if k.Release != 0 {
			off := int64(n.Off) + int64(k.Release)*se.TicksPerSecond/1000
			off = max(off, int64(n.On)+4)
			if uint32(off) != n.Off {
				n.Off, changed[i] = uint32(off), true
			}
		}
	}
	if c.FillZero {
		var all []int
		for _, n := range d.Notes {
			if n.Vel > 0 && n.Vel < SilentVelocity {
				all = append(all, int(n.Vel))
			}
		}
		fallback := 200
		if len(all) > 0 {
			fallback = median(all)
		}
		order := make([]int, len(d.Notes)) // note indices by start
		for i := range order {
			order[i] = i
		}
		sort.SliceStable(order, func(a, b int) bool { return d.Notes[order[a]].On < d.Notes[order[b]].On })
		fills := map[int]uint16{} // assigned after, so a filled note is no one's neighbour
		lo := 0
		for _, i := range order {
			n := d.Notes[i]
			if n.Vel != 0 {
				continue
			}
			for lo < len(order) && d.Notes[order[lo]].On+FillWindow < n.On {
				lo++
			}
			var near []int
			for j := lo; j < len(order) && d.Notes[order[j]].On <= n.On+FillWindow; j++ {
				if v := d.Notes[order[j]].Vel; v > 0 && v < SilentVelocity {
					near = append(near, int(v))
				}
			}
			v := fallback
			if len(near) > 0 {
				v = median(near)
			}
			fills[i] = uint16(v)
		}
		for i, v := range fills {
			d.Notes[i].Vel, changed[i] = v, true
		}
	}
	count := 0
	for _, ch := range changed {
		if ch {
			count++
		}
	}
	return count
}

// SilentVelocity: this velocity or more is a silent press (section 11's
// word 28 on this piano).
const SilentVelocity = 640

// ApplyTo corrects a performance, returning the corrected copy and how
// many notes changed.
func (c Corrections) ApplyTo(p *se.Performance) (*se.Performance, int, error) {
	d := edit.FromPerformance(p)
	n := c.Apply(&d)
	out, err := d.Performance()
	return out, n, err
}

// Suggestion is a correction the Key check suggests for one key, from one
// recording, and why. It replaces the key's correction, not adds to it:
// the Key check reads the recording as the sensors measured it.
type Suggestion struct {
	Note    int    `json:"note"`
	Name    string `json:"name"`
	Speed   int    `json:"speed"`
	Release int    `json:"release_ms"`
	Why     string `json:"why"`
}

// Suggest turns the keys that stand out in a recording into corrections:
// a key that records d% off its neighbours in hammer speed gets the speed
// that brings it back, and a key that lets go early or late in its chords
// gets its typical difference the other way, rounded to 5 ms.
func Suggest(kc KeyCheck, rc ReleaseCheck) []Suggestion {
	by := map[int]*Suggestion{}
	get := func(n int, name string) *Suggestion {
		if by[n] == nil {
			by[n] = &Suggestion{Note: n, Name: name}
		}
		return by[n]
	}
	for _, k := range kc.Outliers {
		speed := int(math.Round((1/(1+k.Deviation/100) - 1) * 100))
		speed = max(MinSpeed, min(MaxSpeed, speed))
		s := get(k.Note, k.Name)
		s.Speed = speed
		word := "softer"
		if k.Deviation > 0 {
			word = "louder"
		}
		s.Why = fmt.Sprintf("records %.0f%% %s than its neighbours (%d notes)", math.Abs(k.Deviation), word, k.Count)
	}
	for _, k := range rc.Outliers {
		ms := -int(math.Round(float64(k.Median)/5)) * 5
		ms = max(-MaxRelease, min(MaxRelease, ms))
		s := get(k.Note, k.Name)
		s.Release = ms
		word := "early"
		if k.Median > 0 {
			word = "late"
		}
		why := fmt.Sprintf("lets go %d ms %s in its chords (%d chords)", abs(k.Median), word, k.Count)
		if s.Why != "" {
			s.Why += "; " + why
		} else {
			s.Why = why
		}
	}
	out := make([]Suggestion, 0, len(by))
	for _, s := range by {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Note < out[j].Note })
	return out
}

// CorrectionsStore keeps the corrections in a JSON file.
type CorrectionsStore struct {
	Path string
	mu   sync.Mutex
}

// Load reads the corrections; with no file there are none, switched on so
// that the first one added takes effect.
func (s *CorrectionsStore) Load() (Corrections, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := Corrections{On: true, Keys: map[int]KeyCorrection{}}
	b, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return Corrections{Keys: map[int]KeyCorrection{}}, fmt.Errorf("%s: %w", s.Path, err)
	}
	if c.Keys == nil {
		c.Keys = map[int]KeyCorrection{}
	}
	return c, nil
}

// Save checks and stores corrections, and returns what was stored.
func (s *CorrectionsStore) Save(c Corrections, now time.Time) (Corrections, error) {
	c, err := c.Clean()
	if err != nil {
		return c, err
	}
	c.Updated = now.UTC().Truncate(time.Second)
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return c, err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return c, err
	}
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return c, err
	}
	return c, os.Rename(tmp, s.Path)
}
