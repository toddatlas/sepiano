package piano

import (
	"testing"

	"sepiano/lut"
	"sepiano/se"
)

func TestStrikeLimits(t *testing.T) {
	tb, err := lut.Parse(lut.Example())
	if err != nil {
		t.Fatal(err)
	}
	l := NewStrikeLimits(tb)
	on, trims := tb.Trims()
	level := func(note, v int) int {
		if on {
			v = lut.TrimVelocity(v, int(trims[note-tb.Bottom]))
		}
		_, lv, err := tb.Lookup(note, v)
		if err != nil {
			return -1
		}
		return lv
	}
	for note := tb.Bottom; note <= tb.Top; note++ {
		m := int(l[note])
		if m == 0 {
			t.Fatalf("%s: no limit", lut.NoteName(note))
		}
		if level(note, m) < MinStrikeLevel || m > 2 && level(note, m-1) >= MinStrikeLevel {
			t.Fatalf("%s: limit %d (level %d; one louder: %d)", lut.NoteName(note), m, level(note, m), level(note, m-1))
		}
	}
	key := uint8(tb.Bottom)
	m := l[key]
	for vel, want := range map[uint16]uint16{m - 1: m, m: m, m + 50: m + 50, 0: 0, 700: 700} {
		if got := l.Velocity(key, vel); got != want {
			t.Errorf("Velocity(%d) = %d, want %d", vel, got, want)
		}
	}
	p := &se.Performance{KB: &se.KB{Events: []se.Event{
		{Tick: 0, Key: key, On: true, Vel: m - 1}, {Tick: 10, Key: key},
		{Tick: 20, Key: key, On: true, Vel: m + 1}, {Tick: 30, Key: key},
	}}}
	if n := l.TooLoud(p); n != 1 {
		t.Fatalf("too loud: %d", n)
	}
	var none *StrikeLimits
	if none.Velocity(key, 3) != 3 || none.TooLoud(p) != 0 {
		t.Fatal("no limits should change nothing")
	}
}
