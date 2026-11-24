// Package edit turns a performance into what the piano-roll editor works
// on, notes and pedal curves, and back into a performance.
//
// A .kb file holds key presses and releases; the editor pairs them into
// notes. The SE format has no release velocity: how fast a key comes up is
// set by the piano's own table (sections 5 to 7, docs/SUITE-PROGRAMS.md), so
// a note's release can be moved but not made faster or slower.
package edit

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"sepiano/se"
)

// The keyboard the editor shows. Key events outside it (Zenph's marks on
// key 11, the Bösendorfer's extra bass keys 12 to 20) are kept as they are.
const (
	Lowest  = 21
	Highest = 108
)

// Limits for an edited performance.
const (
	MaxNotes = 200000
	MaxTicks = 4 * 3600 * se.TicksPerSecond // four hours
)

// Note is one key press, from On to Off, in ticks (1/800 s). Vel is the SE
// velocity: bigger is softer, and 0 or 640 and more is a silent press (the
// key goes down without sounding; play.exe 1044H). The recorder writes 0
// for a press whose hammer it did not time.
type Note struct {
	Key uint8
	On  uint32
	Off uint32
	Vel uint16
}

// Doc is a performance as the editor sees it.
type Doc struct {
	Notes  []Note     // in order of On
	Other  []se.Event // the sostenuto pedal (key 0) and key events outside the keyboard (cue marks on 126, Zenph's on 11), kept as they are
	Damper []byte     // one sample per se.DamperPeriod ticks, 0 up to 255 down
	Soft   []byte     // one sample per se.SoftPeriod ticks
	End    uint32     // the end of the performance
	Text   string     // the header text the player prints
	Format byte
	Model  byte
}

// FromPerformance pairs each press with the release that follows it on the
// same key. A press on a key that is already down ends the note before it;
// a press with no release lasts to the end; a release with no press is
// dropped. Only the padding fillers (key 127) are dropped; the sostenuto
// pedal's events (key 0) are kept in Other.
func FromPerformance(p *se.Performance) Doc {
	k := p.KB
	d := Doc{End: k.End, Text: k.Text, Format: k.Format, Model: k.Model}
	if p.Damper != nil {
		d.Damper = append([]byte(nil), p.Damper.Samples...)
	}
	if p.Soft != nil {
		d.Soft = append([]byte(nil), p.Soft.Samples...)
	}
	open := map[uint8]int{}
	for _, e := range k.Playable() {
		if e.Tick > d.End {
			d.End = e.Tick
		}
		if e.Key < Lowest || e.Key > Highest {
			d.Other = append(d.Other, e)
			continue
		}
		if i, ok := open[e.Key]; ok {
			d.Notes[i].Off = e.Tick
			delete(open, e.Key)
		}
		if e.On {
			open[e.Key] = len(d.Notes)
			d.Notes = append(d.Notes, Note{Key: e.Key, On: e.Tick, Vel: e.Vel})
		}
	}
	for _, i := range open {
		d.Notes[i].Off = d.End
	}
	for i := range d.Notes {
		if d.Notes[i].Off <= d.Notes[i].On {
			d.Notes[i].Off = d.Notes[i].On + 1
		}
	}
	return d
}

// Check reports what makes d unfit to save.
func (d Doc) Check() error {
	if len(d.Notes) > MaxNotes {
		return fmt.Errorf("too many notes (%d; at most %d)", len(d.Notes), MaxNotes)
	}
	for i, n := range d.Notes {
		switch {
		case n.Key < Lowest || n.Key > Highest:
			return fmt.Errorf("note %d: key %d is not on the keyboard", i+1, n.Key)
		case n.Off <= n.On:
			return fmt.Errorf("note %d: it ends before it starts", i+1)
		case n.Off > MaxTicks:
			return fmt.Errorf("note %d: too late (at most four hours)", i+1)
		case n.Vel > se.MaxVelocity:
			return fmt.Errorf("note %d: velocity %d is out of range (0 to %d)", i+1, n.Vel, se.MaxVelocity)
		}
	}
	if d.End > MaxTicks {
		return errors.New("the performance is too long (at most four hours)")
	}
	if len(d.Damper) > MaxTicks/se.DamperPeriod+se.TicksPerSecond || len(d.Soft) > MaxTicks/se.SoftPeriod+se.TicksPerSecond {
		return errors.New("the pedal curves are too long")
	}
	return nil
}

// Performance builds the performance files' contents from d. Notes on one
// key may not overlap: a note that starts while the one before it on the
// same key is still down ends that one. At the same tick, releases come
// before presses, so a repeated note is struck again.
func (d Doc) Performance() (*se.Performance, error) {
	if err := d.Check(); err != nil {
		return nil, err
	}
	notes := append([]Note(nil), d.Notes...)
	sort.SliceStable(notes, func(i, j int) bool {
		if notes[i].Key != notes[j].Key {
			return notes[i].Key < notes[j].Key
		}
		return notes[i].On < notes[j].On
	})
	for i := 1; i < len(notes); i++ {
		if notes[i].Key == notes[i-1].Key && notes[i].On < notes[i-1].Off {
			notes[i-1].Off = notes[i].On
		}
	}
	evs := make([]se.Event, 0, 2*len(notes)+len(d.Other))
	for _, n := range notes {
		if n.Off <= n.On {
			continue // two presses at the same tick: the later one stays
		}
		evs = append(evs, se.Event{Tick: n.On, Key: n.Key, On: true, Vel: n.Vel}, se.Event{Tick: n.Off, Key: n.Key})
	}
	evs = append(evs, d.Other...)
	sort.SliceStable(evs, func(i, j int) bool {
		if evs[i].Tick != evs[j].Tick {
			return evs[i].Tick < evs[j].Tick
		}
		return !evs[i].On && evs[j].On
	})
	end := d.End
	if n := len(evs); n > 0 && evs[n-1].Tick > end {
		end = evs[n-1].Tick
	}
	kb := &se.KB{Format: d.Format, Model: d.Model, Text: strings.ReplaceAll(d.Text, "\x7f", ""), Events: evs, End: end}
	return &se.Performance{
		KB:     kb,
		Damper: &se.Pedal{Samples: append([]byte(nil), d.Damper...)},
		Soft:   &se.Pedal{Samples: append([]byte(nil), d.Soft...)},
	}, nil
}

// From returns the part of d that starts at tick from, to be heard from
// there: lead ticks of silence, in which the pedals take the positions they
// have at from, then the notes that start at or after from. Notes already
// sounding at from are left out; the sostenuto pedal's events from then on
// are kept (the marks outside the keyboard are not: they never sound).
func (d Doc) From(from, lead uint32) Doc {
	if from == 0 {
		return d
	}
	shift := func(t uint32) uint32 { return t - from + lead }
	out := Doc{Text: d.Text, Format: d.Format, Model: d.Model, End: shift(max(d.End, from))}
	for _, n := range d.Notes {
		if n.On >= from {
			out.Notes = append(out.Notes, Note{Key: n.Key, On: shift(n.On), Off: shift(n.Off), Vel: n.Vel})
		}
	}
	// A sostenuto already down goes down at the start too, so its release
	// later has a press to end (unless it comes up right at from).
	if d.sostenutoDown(from) && !d.sostenutoAt(from) {
		out.Other = append(out.Other, se.Event{Tick: lead, Key: 0, On: true})
	}
	for _, e := range d.Other {
		if e.Key == 0 && e.Tick >= from {
			e.Tick = shift(e.Tick)
			out.Other = append(out.Other, e)
		}
	}
	out.Damper = resample(d.Damper, se.DamperPeriod, from, lead, out.End)
	out.Soft = resample(d.Soft, se.SoftPeriod, from, lead, out.End)
	return out
}

func resample(s []byte, period, from, lead, end uint32) []byte {
	at := func(t uint32) byte {
		if len(s) == 0 {
			return 0
		}
		return s[min(int(t/period), len(s)-1)]
	}
	out := make([]byte, int(end/period)+1)
	for i := range out {
		t := uint32(i) * period
		if t < lead {
			out[i] = at(from)
		} else {
			out[i] = at(t - lead + from)
		}
	}
	return out
}

// Until cuts d at tick t, then lets it ring for tail ticks: the notes that
// start before t, each let go by t at the latest; the pedals as they are up
// to t, then up.
func (d Doc) Until(t, tail uint32) Doc {
	out := Doc{Text: d.Text, Format: d.Format, Model: d.Model, End: t + tail}
	for _, n := range d.Notes {
		if n.On >= t {
			continue
		}
		n.Off = min(n.Off, t)
		if n.Off <= n.On {
			n.Off = n.On + 1
		}
		out.Notes = append(out.Notes, n)
	}
	for _, e := range d.Other {
		if e.Tick < t {
			out.Other = append(out.Other, e)
		}
	}
	// A sostenuto still down comes up at the cut, as the notes and the
	// other pedals do, so nothing it holds rings on.
	if d.sostenutoDown(t) {
		out.Other = append(out.Other, se.Event{Tick: t, Key: 0})
	}
	cut := func(s []byte, period uint32) []byte {
		n := int(out.End/period) + 1
		o := make([]byte, n)
		k := min(len(s), int(t/period))
		copy(o, s[:k])
		return o
	}
	out.Damper = cut(d.Damper, se.DamperPeriod)
	out.Soft = cut(d.Soft, se.SoftPeriod)
	return out
}

// sostenutoDown says whether the sostenuto pedal (key 0) is down just
// before tick t.
func (d Doc) sostenutoDown(t uint32) bool {
	down, last := false, uint32(0)
	for _, e := range d.Other {
		if e.Key == 0 && e.Tick < t && e.Tick >= last {
			down, last = e.On, e.Tick
		}
	}
	return down
}

// sostenutoAt says whether the sostenuto pedal moves at tick t.
func (d Doc) sostenutoAt(t uint32) bool {
	for _, e := range d.Other {
		if e.Key == 0 && e.Tick == t {
			return true
		}
	}
	return false
}
