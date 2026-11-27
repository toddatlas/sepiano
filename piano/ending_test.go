package piano

import (
	"testing"

	"sepiano/se"
)

func TestGentleEnding(t *testing.T) {
	s := func(sec float64) uint32 { return uint32(sec * se.TicksPerSecond) }
	perf := func(damper, soft func(i int) byte, seconds float64) *se.Performance {
		kb := &se.KB{End: s(seconds), Events: []se.Event{
			{Tick: s(1), Key: 60, On: true, Vel: 300}, {Tick: s(seconds - 0.5), Key: 60},
		}}
		d := make([]byte, int(s(seconds)/se.DamperPeriod)+1)
		for i := range d {
			d[i] = damper(i)
		}
		sp := make([]byte, int(s(seconds)/se.SoftPeriod)+1)
		for i := range sp {
			sp[i] = soft(i)
		}
		return &se.Performance{KB: kb, Damper: &se.Pedal{Samples: d}, Soft: &se.Pedal{Samples: sp}}
	}
	maxFall := func(b []byte) int {
		m := 0
		for i := 1; i < len(b); i++ {
			m = max(m, int(b[i-1])-int(b[i]))
		}
		return m
	}
	// MAESTRO's way: held at 194, dropped in one step with the last notes;
	// the soft pedal still down at the end.
	p := perf(func(i int) byte {
		if i < 950 { // 9.5 s, the last release
			return 194
		}
		return 0
	}, func(int) byte { return 255 }, 10)
	end := p.KB.End
	if n := GentleEnding(p); n == 0 {
		t.Fatal("nothing changed")
	}
	rateD := 255/(EndingRelease*se.TicksPerSecond/se.DamperPeriod) + 1
	if f := maxFall(p.Damper.Samples); float64(f) > rateD {
		t.Fatalf("damper falls %d in a sample (at most %.1f)", f, rateD)
	}
	if last := p.Soft.Samples[len(p.Soft.Samples)-1]; last != 0 {
		t.Fatalf("soft pedal left at %d", last)
	}
	upAt := func(ped *se.Pedal, period uint32) uint32 { // the tick of the last sample still down
		for i := len(ped.Samples) - 1; i >= 0; i-- {
			if ped.Samples[i] > 0 {
				return uint32(i) * period
			}
		}
		return 0
	}
	if p.KB.End <= end || upAt(p.Soft, se.SoftPeriod) >= p.KB.End || upAt(p.Damper, se.DamperPeriod) >= p.KB.End {
		t.Fatalf("end %d (was %d) comes before the pedals are up", p.KB.End, end)
	}
	if extra := float64(p.KB.End-end) / se.TicksPerSecond; extra > EndingRelease+0.2 {
		t.Fatalf("made %.2f s longer", extra)
	}

	// A pianist's ending: the pedal lifted in steps a foot makes, before
	// the end. Left exactly as it is.
	p = perf(func(i int) byte {
		switch {
		case i < 940:
			return 200
		case i < 945:
			return byte(200 - (i-939)*40)
		}
		return 0
	}, func(int) byte { return 0 }, 10)
	before := append([]byte(nil), p.Damper.Samples...)
	end = p.KB.End
	if n := GentleEnding(p); n != 0 || string(before) != string(p.Damper.Samples) || p.KB.End != end {
		t.Fatalf("a foot's ending was changed (%d samples)", n)
	}
}
