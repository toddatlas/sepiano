package piano

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"

	"sepiano/se"
)

// A Fit adjusts one collection's pieces to this piano as they play
// (docs/LIBRARIES.md). Files from other pianos and sources come out a
// little "all over the place" here: a Bösendorfer's hammer speeds, or a
// Disklavier's MIDI loudness and pedal depth, are not this Steinway's. A
// fit is set by ear once per collection; the files never change, and it
// can be changed again at any time.
type Fit struct {
	// Speed is the loudness, in % hammer speed: +10 plays 10% faster
	// hammers (louder), −10 slower. Silent presses stay silent and audible
	// notes stay audible (at most velocity 639).
	Speed int `json:"speed"`
	// Damper and Soft scale the pedals' recorded depth, in %: 100 is as
	// recorded, 80 lifts the dampers 20% less, 120 more (up to fully).
	Damper int `json:"damper"`
	Soft   int `json:"soft"`
}

// Limits of a fit.
const (
	MinFitSpeed, MaxFitSpeed = -50, 100
	MinFitPedal, MaxFitPedal = 25, 200
)

// normal fills in an unset pedal scale as 100.
func (f Fit) normal() Fit {
	if f.Damper == 0 {
		f.Damper = 100
	}
	if f.Soft == 0 {
		f.Soft = 100
	}
	return f
}

// Active says whether the fit changes anything.
func (f Fit) Active() bool {
	f = f.normal()
	return f.Speed != 0 || f.Damper != 100 || f.Soft != 100
}

// Check checks a fit's ranges.
func (f Fit) Check() error {
	f = f.normal()
	if f.Speed < MinFitSpeed || f.Speed > MaxFitSpeed {
		return fmt.Errorf("loudness must be between %d%% and %+d%%", MinFitSpeed, MaxFitSpeed)
	}
	if f.Damper < MinFitPedal || f.Damper > MaxFitPedal || f.Soft < MinFitPedal || f.Soft > MaxFitPedal {
		return fmt.Errorf("pedal depth must be between %d%% and %d%%", MinFitPedal, MaxFitPedal)
	}
	return nil
}

// Apply fits p in place and returns how many notes and pedal samples it
// changed.
func (f Fit) Apply(p *se.Performance) int {
	f = f.normal()
	if !f.Active() {
		return 0
	}
	n := 0
	if f.Speed != 0 {
		for i := range p.KB.Events {
			e := &p.KB.Events[i]
			if !e.On || e.Key == 0 || e.Filler() || e.Vel == 0 || e.Vel >= SilentVelocity {
				continue
			}
			if v := clampVel(float64(e.Vel) * 100 / float64(100+f.Speed)); v != e.Vel {
				e.Vel = v
				n++
			}
		}
	}
	scale := func(ped *se.Pedal, pct int) {
		if ped == nil || pct == 100 {
			return
		}
		for i, s := range ped.Samples {
			if v := byte(math.Min(255, math.Round(float64(s)*float64(pct)/100))); v != s {
				ped.Samples[i] = v
				n++
			}
		}
	}
	scale(p.Damper, f.Damper)
	scale(p.Soft, f.Soft)
	return n
}

// Fits are the fits by collection name (library.Piece.Collection).
type Fits struct {
	On          bool           `json:"on"`
	Collections map[string]Fit `json:"collections"`
}

// For returns a collection's fit, if there is an active one.
func (fs Fits) For(collection string) (Fit, bool) {
	if !fs.On || collection == "" {
		return Fit{}, false
	}
	f, ok := fs.Collections[collection]
	if !ok || !f.Active() {
		return Fit{}, false
	}
	return f.normal(), true
}

// Active says whether any fit changes anything.
func (fs Fits) Active() bool {
	if !fs.On {
		return false
	}
	for _, f := range fs.Collections {
		if f.Active() {
			return true
		}
	}
	return false
}

// FitStore keeps the fits in a JSON file.
type FitStore struct {
	Path string
	mu   sync.Mutex
}

// Load returns the saved fits, or none (switched on, so the first fit set
// is heard).
func (s *FitStore) Load() (Fits, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fs := Fits{On: true, Collections: map[string]Fit{}}
	b, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return fs, nil
	}
	if err != nil {
		return fs, err
	}
	if err := json.Unmarshal(b, &fs); err != nil {
		return Fits{On: true, Collections: map[string]Fit{}}, fmt.Errorf("%s: %w", s.Path, err)
	}
	if fs.Collections == nil {
		fs.Collections = map[string]Fit{}
	}
	return fs, nil
}

// Save checks and stores the fits, dropping those that change nothing.
func (s *FitStore) Save(fs Fits) (Fits, error) {
	out := Fits{On: fs.On, Collections: map[string]Fit{}}
	for name, f := range fs.Collections {
		if name == "" {
			return fs, errors.New("a fit needs a collection")
		}
		if err := f.Check(); err != nil {
			return fs, fmt.Errorf("%s: %w", name, err)
		}
		if f = f.normal(); f.Active() {
			out.Collections[name] = f
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fs, err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return fs, err
	}
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fs, err
	}
	return out, os.Rename(tmp, s.Path)
}

// Pedal speed limits at playback (docs/LIBRARIES.md, "Pedals"). This is a
// vintage acoustic piano, not a MIDI instrument: a sudden full press or
// release is a hard knock for its pedal mechanism.
const (
	// PedalStep: no pedal moves more than this per sample (10 ms for the
	// damper, 20 ms for the soft pedal). It is the fastest move in native
	// SE recordings: the owner's 350 Zenph files never exceed it, so they
	// play exactly as recorded.
	PedalStep = 60
	// OnOffPedalStep: a pedal that is only ever fully up or fully down (an
	// on/off MIDI pedal: rolls, transcriptions) carries no speed at all, so
	// it moves more gently, a full stroke over about 80 ms (damper) or
	// 160 ms (soft), about a pianist's usual pace.
	OnOffPedalStep = 32
)

// SafePedals slows a performance's pedals to those limits and returns how
// many samples it changed. Every piece goes through it as it plays.
func SafePedals(p *se.Performance) int {
	n := 0
	for _, ped := range []*se.Pedal{p.Damper, p.Soft} {
		if ped == nil {
			continue
		}
		step := PedalStep
		if ped.OnOff() {
			step = OnOffPedalStep
		}
		n += ped.Limit(step)
	}
	return n
}
