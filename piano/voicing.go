package piano

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"sepiano/lut"
	"sepiano/se"
)

// Voicing is software voicing: per-key trims and a whole-piano offset that
// sepianod applies to a performance's velocities as it serves it to the
// player, with the firmware's own trim formula (lut.TrimVelocity). Nothing
// is written to the piano. Trims are in the same steps as the table's
// section 9 trims: +1 adds 5 to the hammer speed (128000 / velocity).
type Voicing struct {
	On     bool        `json:"on"`
	Offset int         `json:"offset"` // steps for every key
	Trims  map[int]int `json:"trims"`  // MIDI note → steps
}

// MaxTrim bounds software trims and the offset. The table itself holds
// -128..127; this keeps experiments within sensible reach.
const MaxTrim = 60

// Active says whether the voicing changes anything.
func (v Voicing) Active() bool {
	if !v.On {
		return false
	}
	if v.Offset != 0 {
		return true
	}
	for _, t := range v.Trims {
		if t != 0 {
			return true
		}
	}
	return false
}

// TrimFor is the steps applied to a note: its own trim plus the offset.
func (v Voicing) TrimFor(note int) int { return v.Offset + v.Trims[note] }

// Clean checks the ranges and drops zero trims.
func (v Voicing) Clean() (Voicing, error) {
	if v.Offset < -MaxTrim || v.Offset > MaxTrim {
		return v, fmt.Errorf("the whole-piano offset must be between %d and %d", -MaxTrim, MaxTrim)
	}
	out := Voicing{On: v.On, Offset: v.Offset, Trims: map[int]int{}}
	for note, t := range v.Trims {
		if note < 0 || note > 127 {
			return v, fmt.Errorf("no such note %d", note)
		}
		if t < -MaxTrim || t > MaxTrim {
			return v, fmt.Errorf("a trim must be between %d and %d", -MaxTrim, MaxTrim)
		}
		if t != 0 {
			out.Trims[note] = t
		}
	}
	return out, nil
}

// Velocity is what a note-on velocity becomes. 0 (which the format allows)
// is left alone, and results stay within 4..1023.
func (v Voicing) Velocity(note int, vel uint16) uint16 {
	t := v.TrimFor(note)
	if t == 0 || vel == 0 {
		return vel
	}
	return uint16(min(max(lut.TrimVelocity(int(vel), t), 4), se.MaxVelocity))
}

// Apply changes p's note-on velocities in place and says how many changed.
// Silent presses (0, or 640 and more) are left alone: the player decides
// a press is silent before it trims (play.exe 1044H), so a trim never
// makes one sound.
func (v Voicing) Apply(p *se.Performance) int {
	if !v.Active() {
		return 0
	}
	n := 0
	for i := range p.KB.Events {
		e := &p.KB.Events[i]
		if !e.On || e.Filler() || e.Vel == 0 || e.Vel >= SilentVelocity {
			continue
		}
		if nv := v.Velocity(int(e.Key), e.Vel); nv != e.Vel {
			e.Vel = nv
			n++
		}
	}
	return n
}

// TrimmedNotes lists the notes with a trim, low to high.
func (v Voicing) TrimmedNotes() []int {
	var out []int
	for n, t := range v.Trims {
		if t != 0 {
			out = append(out, n)
		}
	}
	sort.Ints(out)
	return out
}

// VoicingStore keeps the voicing in a JSON file.
type VoicingStore struct {
	Path string
	mu   sync.Mutex
}

// Load returns the saved voicing, or an empty one that is switched on (so
// the first trim tried is heard).
func (s *VoicingStore) Load() (Voicing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := Voicing{On: true, Trims: map[int]int{}}
	b, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return v, nil
	}
	if err != nil {
		return v, err
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return Voicing{Trims: map[int]int{}}, fmt.Errorf("%s: %w", s.Path, err)
	}
	if v.Trims == nil {
		v.Trims = map[int]int{}
	}
	return v, nil
}

// Save checks and stores a voicing, and returns what was stored.
func (s *VoicingStore) Save(v Voicing) (Voicing, error) {
	v, err := v.Clean()
	if err != nil {
		return v, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return v, err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return v, err
	}
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return v, err
	}
	return v, os.Rename(tmp, s.Path)
}

// TestDynamics are the velocities a test phrase plays, loud to very soft.
var TestDynamics = []int{64, 100, 256, 400}

// TestPhrase builds a short performance for hearing a trim on one key: at
// each of TestDynamics, the note as recorded, then with the trim, a second
// apart. It starts after a second, so even the softest strike's lead time
// fits before it.
func TestPhrase(note, trim int) *se.Performance {
	kb := &se.KB{Format: 0x0F, Model: 0x11,
		Text: fmt.Sprintf("Voicing test: %s, as recorded then %+d\r\n", lut.NoteName(note), trim)}
	v := Voicing{On: true, Trims: map[int]int{note: trim}}
	const gap, hold = se.TicksPerSecond, se.TicksPerSecond / 2
	t := uint32(se.TicksPerSecond)
	for _, vel := range TestDynamics {
		for _, x := range []uint16{uint16(vel), v.Velocity(note, uint16(vel))} {
			kb.Events = append(kb.Events,
				se.Event{Tick: t, Key: uint8(note), On: true, Vel: x},
				se.Event{Tick: t + hold, Key: uint8(note)})
			t += gap
		}
		t += gap / 2 // a little more between dynamics
	}
	kb.End = t + se.TicksPerSecond
	return &se.Performance{KB: kb, Damper: &se.Pedal{}, Soft: &se.Pedal{}}
}
