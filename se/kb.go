package se

import (
	"errors"
	"fmt"
	"sort"
)

// TicksPerSecond is the .kb time base: one tick is 1.25 ms.
const TicksPerSecond = 800

// Pedal sample periods, in ticks.
const (
	DamperPeriod = 8  // .lp: one sample every 10 ms
	SoftPeriod   = 16 // .sp: one sample every 20 ms
)

// Key numbers 0 and 127 never sound; the player treats their events as
// pure time advances. Encoders use FillerKey to pad gaps longer than 255 ticks.
const FillerKey = 0x7F

// MaxVelocity is the largest velocity a .kb note-on can carry (10 bits).
// SE velocity is an inverse scale: larger values are softer notes.
const MaxVelocity = 0x3FF

// Event is one key transition.
type Event struct {
	Tick uint32 // absolute time in ticks
	Key  uint8  // MIDI note number (0-127)
	On   bool
	Vel  uint16 // note-on velocity, 0 or 4..1023; zero for note-off
}

// Filler reports whether the event is a silent time-advance marker.
func (e Event) Filler() bool { return e.Key == 0 || e.Key == FillerKey }

// KB is a decoded .kb file.
type KB struct {
	Seed   [4]byte
	Format byte   // header byte 0 (0x0F in every known file)
	Model  byte   // header byte 1 (0x11 in Zenph files)
	Extra  []byte // length-prefixed opaque header bytes (normally empty)
	Text   string // header text shown by the player, usually a copyright line
	Events []Event
	End    uint32 // tick at which the end-of-performance marker occurs
}

var errShort = errors.New("se: truncated file")

// ParseKB decodes a .kb file.
func ParseKB(raw []byte) (*KB, error) {
	if len(raw) < 8 {
		return nil, errShort
	}
	d := append([]byte(nil), raw...)
	Crypt(d)
	k := &KB{Seed: [4]byte{d[0], d[1], d[2], d[3]}, Format: d[4], Model: d[5]}
	n := int(d[6])<<8 | int(d[7])
	p := 8
	if p+n > len(d) {
		return nil, errShort
	}
	k.Extra = append([]byte(nil), d[p:p+n]...)
	p += n
	t0 := p
	for p < len(d) && d[p] != FillerKey {
		p++
	}
	if p >= len(d) {
		return nil, errors.New("se: header text not terminated by 0x7F")
	}
	k.Text = string(d[t0:p])

	var t uint32
	next := func() (byte, error) {
		if p >= len(d) {
			return 0, errShort
		}
		b := d[p]
		p++
		return b, nil
	}
	for {
		a, err := next()
		if err != nil {
			return nil, fmt.Errorf("se: missing end marker: %w", err)
		}
		if a == 0xFF {
			k.End = t
			return k, nil
		}
		ev := Event{Tick: t, Key: a & 0x7F, On: a >= 0x80}
		if a > 0x80 {
			b, err := next()
			if err != nil {
				return nil, err
			}
			if b >= 1 && b <= 3 {
				c, err := next()
				if err != nil {
					return nil, err
				}
				ev.Vel = uint16(b)<<8 | uint16(c)
			} else {
				ev.Vel = uint16(b)
			}
		}
		dt, err := next()
		if err != nil {
			return nil, err
		}
		k.Events = append(k.Events, ev)
		t += uint32(dt)
	}
}

// Playable returns the events without the padding fillers (key 127): the
// notes and the sostenuto pedal, which is key 0 (0x80 puts it down, 0x00
// lets it up; play.exe 1604H, docs/SE-FILE-FORMAT.md). Use it, not Notes,
// to rebuild a performance.
func (k *KB) Playable() []Event {
	var out []Event
	for _, e := range k.Events {
		if e.Key != FillerKey {
			out = append(out, e)
		}
	}
	return out
}

// Notes returns the events with filler markers removed.
func (k *KB) Notes() []Event {
	var out []Event
	for _, e := range k.Events {
		if !e.Filler() {
			out = append(out, e)
		}
	}
	return out
}

// Encode serialises the key stream. Events are stably sorted by tick; filler
// events are inserted for gaps longer than 255 ticks, the same way the
// original encoder does it, so re-encoding a parsed file reproduces it exactly
// (including the 0xFF padding when pad is true).
func (k *KB) Encode(pad bool) ([]byte, error) {
	ev := append([]Event(nil), k.Events...)
	sort.SliceStable(ev, func(i, j int) bool { return ev[i].Tick < ev[j].Tick })
	if len(ev) == 0 || ev[0].Tick != 0 || ev[0].Key != FillerKey || ev[0].On {
		// The header text is terminated by the first event byte, which must be 0x7F.
		ev = append([]Event{{Tick: 0, Key: FillerKey}}, ev...)
	}
	end := k.End
	if last := ev[len(ev)-1].Tick; end < last {
		end = last
	}
	out := append([]byte(nil), k.Seed[:]...)
	out = append(out, k.Format, k.Model, byte(len(k.Extra)>>8), byte(len(k.Extra)))
	out = append(out, k.Extra...)
	for i := 0; i < len(k.Text); i++ {
		if k.Text[i] == FillerKey {
			return nil, errors.New("se: header text may not contain 0x7F")
		}
	}
	out = append(out, k.Text...)
	for i, e := range ev {
		if e.Key > 0x7F {
			return nil, fmt.Errorf("se: key %d out of range", e.Key)
		}
		next := end
		if i+1 < len(ev) {
			next = ev[i+1].Tick
		}
		if e.On && e.Key == 0 {
			out = append(out, 0x80) // marker byte with no velocity, like 0x00
		} else if e.On {
			v := e.Vel
			if v > MaxVelocity {
				return nil, fmt.Errorf("se: velocity %d out of range", v)
			}
			if v >= 1 && v <= 3 {
				v = 4 // not representable; nearest encodable value
			}
			out = append(out, 0x80|e.Key)
			if v > 0xFF {
				out = append(out, byte(v>>8), byte(v))
			} else {
				out = append(out, byte(v))
			}
		} else {
			out = append(out, e.Key)
		}
		gap := next - e.Tick
		step := min(gap, 255)
		out = append(out, byte(step))
		for gap -= step; gap > 0; gap -= step {
			step = min(gap, 255)
			out = append(out, FillerKey, byte(step))
		}
	}
	out = append(out, 0xFF)
	if pad {
		// Zenph files end with 20 0xFF bytes in total; the player stops at the first.
		for range 19 {
			out = append(out, 0xFF)
		}
	}
	Crypt(out)
	return out, nil
}
