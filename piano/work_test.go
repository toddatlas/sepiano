package piano

import (
	"math"
	"testing"

	"sepiano/se"
)

func TestMeasureWork(t *testing.T) {
	s := func(sec float64) uint32 { return uint32(sec * se.TicksPerSecond) }
	kb := &se.KB{End: s(14), Events: []se.Event{
		{Tick: s(0), Key: 60, On: true, Vel: 300},
		{Tick: s(0), Key: 64, On: true, Vel: 300},
		{Tick: s(1), Key: 67, On: true, Vel: 700}, // a silent press holds a key too
		{Tick: s(2), Key: 60},
		{Tick: s(3), Key: 67},
		{Tick: s(12), Key: 64},
		{Tick: s(13), Key: 0, On: true}, // the sostenuto is not a key
	}}
	damper := make([]byte, 14*se.TicksPerSecond/se.DamperPeriod)
	for i := range damper {
		if i >= 100 && i < 1300 { // fully down from 1 s to 13 s
			damper[i] = 255
		}
	}
	w := MeasureWork(&se.Performance{KB: kb, Damper: &se.Pedal{Samples: damper}})
	near := func(got, want float64) bool { return math.Abs(got-want) < 0.02 }
	if w.Notes != 3 || !near(w.HoldS, 16) || w.PeakHeld != 3 || w.Held1s != 3 || w.Held10s != 1 ||
		!near(w.LongestHoldS, 12) || w.LongestKey != 64 || !near(w.LongHoldS, 8) || !near(w.DurationS, 14) {
		t.Fatalf("work: %+v", w)
	}
	// Under the full pedal: key 64 from 1 s (11 s), key 60 from 1 s (1 s),
	// key 67 from its strike, 1.15 s (1.85 s).
	if !near(w.PedalFreeS, 13.85) {
		t.Fatalf("pedal-free holding %.2f s, want 13.85", w.PedalFreeS)
	}
	if w.Level != "moderate" { // a 12 s hold
		t.Fatalf("level %q", w.Level)
	}
	if lv := WorkLevel(Work{AvgHeld: 2, LongestHoldS: 5}); lv != "light" {
		t.Fatalf("light: %q", lv)
	}
	if lv := WorkLevel(Work{AvgHeld: 3, LongestHoldS: 65, Held10s: 4}); lv != "very heavy" {
		t.Fatalf("a 65 s hold: %q", lv)
	}
	if w := MeasureWork(&se.Performance{KB: &se.KB{}}); w.Level != "light" || w.HoldS != 0 {
		t.Fatalf("empty: %+v", w)
	}
}
