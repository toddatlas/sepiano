package se

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"sepiano/smf"
)

// The tempAnatolyBoes folder holds the same performance rendered both as SE
// files and as a Yamaha Disklavier MIDI file: an independent check of the key
// numbering and the 800 Hz time base.
func TestAgainstPairedMIDI(t *testing.T) {
	dir := filepath.Join(corpus(t), "tempAnatolyBoes")
	p, err := Load(filepath.Join(dir, "Larkin20120727-Nowak_BodyBends-Boesendorfer_SE42251-590-22(20150112)-SE"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "Larkin20120727-Nowak_BodyBends-Yamaha_5527(after20120606).mid"))
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := smf.Read(data)
	if err != nil {
		t.Fatal(err)
	}
	var mon []smf.Message
	for _, m := range msgs {
		if m.Kind() == 0x90 && m.Data2 > 0 {
			mon = append(mon, m)
		}
	}
	var kon []Event
	for _, e := range p.KB.Notes() {
		if e.On {
			kon = append(kon, e)
		}
	}
	if len(kon) != len(mon) {
		t.Fatalf("%d SE notes vs %d MIDI notes", len(kon), len(mon))
	}
	off := mon[0].Time - TickDuration(kon[0].Tick)
	var worst time.Duration
	for i := range kon {
		if kon[i].Key != mon[i].Data1 {
			t.Fatalf("note %d: key %d vs MIDI %d", i, kon[i].Key, mon[i].Data1)
		}
		d := mon[i].Time - off - TickDuration(kon[i].Tick)
		worst = max(worst, d, -d)
	}
	if worst > 2*time.Millisecond {
		t.Errorf("worst timing difference %v", worst)
	}
	t.Logf("%d notes match pitch; worst timing difference %v over %v", len(kon), worst, p.Duration())
}

func TestMIDIRoundTrip(t *testing.T) {
	p, err := Load(filepath.Join(corpus(t), "Hamb", "Gould19550616-Bach_GV01Aria-Steinway_SED519960-SE"))
	if err != nil {
		t.Fatal(err)
	}
	mid, err := p.ToMIDI(nil, false)
	if err != nil {
		t.Fatal(err)
	}
	q, err := FromMIDI(mid, nil, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	a, b := p.KB.Notes(), q.KB.Notes()
	if len(a) != len(b) {
		t.Fatalf("%d notes became %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Key != b[i].Key || a[i].On != b[i].On || a[i].Tick != b[i].Tick {
			t.Fatalf("event %d: %+v became %+v", i, a[i], b[i])
		}
		if a[i].On {
			// one MIDI step spans several SE values; the curve must stay close
			if d := int(a[i].Vel) - int(b[i].Vel); d > 30 || d < -30 {
				t.Errorf("event %d: velocity %d became %d", i, a[i].Vel, b[i].Vel)
			}
		}
	}
	for i := 0; i < len(p.Damper.Samples) && uint32(i)*DamperPeriod < p.KB.End; i++ {
		if d := int(p.Damper.Samples[i]) - int(q.Damper.Samples[i]); d > 2 || d < -2 {
			t.Fatalf("damper sample %d: %d became %d", i, p.Damper.Samples[i], q.Damper.Samples[i])
		}
	}
}

func TestCurveMonotonic(t *testing.T) {
	for name, c := range Curves {
		prev := byte(255)
		for v := uint16(4); v <= MaxVelocity; v++ {
			if m := c.ToMIDI(v); m > prev {
				t.Fatalf("%s: not monotonic at %d", name, v)
			} else {
				prev = m
			}
		}
	}
	prev := byte(255)
	for v := uint16(4); v <= MaxVelocity; v++ {
		m := DefaultCurve.ToMIDI(v)
		if m > prev {
			t.Fatalf("curve not monotonic at %d", v)
		}
		prev = m
	}
	for m := byte(1); m <= 127; m++ {
		if got := DefaultCurve.ToMIDI(DefaultCurve.ToSE(m)); got < m-1 || got > m+1 {
			t.Errorf("MIDI %d -> SE %d -> MIDI %d", m, DefaultCurve.ToSE(m), got)
		}
	}
}

// TestMIDISostenuto: the sostenuto pedal (key 0) goes out as CC66 and comes
// back in as key-0 events.
func TestMIDISostenuto(t *testing.T) {
	p := &Performance{
		KB: &KB{Format: 0x0F, Model: 0x11, End: 2400, Events: []Event{
			{Tick: 0, Key: FillerKey},
			{Tick: 800, Key: 60, On: true, Vel: 200},
			{Tick: 880, Key: 0, On: true},
			{Tick: 1200, Key: 60},
			{Tick: 2000, Key: 0},
		}},
		Damper: &Pedal{Samples: make([]byte, 301)},
		Soft:   &Pedal{Samples: make([]byte, 151)},
	}
	data, err := p.ToMIDI(nil, false)
	if err != nil {
		t.Fatal(err)
	}
	q, err := FromMIDI(data, nil, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	var marks []Event
	for _, e := range q.KB.Events {
		if e.Key == 0 {
			marks = append(marks, e)
		}
	}
	if len(marks) != 2 || marks[0] != (Event{Tick: 880, Key: 0, On: true}) || marks[1] != (Event{Tick: 2000, Key: 0}) {
		t.Errorf("sostenuto after the round trip: %+v", marks)
	}
}

// TestSilentPressToMIDI: velocity 0 is a silent press, the softest in MIDI.
func TestSilentPressToMIDI(t *testing.T) {
	if v := DefaultCurve.ToMIDI(0); v != 1 {
		t.Errorf("velocity 0 → MIDI %d, want 1", v)
	}
	if v := DefaultCurve.ToMIDI(12); v != 127 {
		t.Errorf("velocity 12 → MIDI %d, want 127", v)
	}
}
