package edit

import (
	"reflect"
	"testing"

	"sepiano/se"
)

func perf(events []se.Event, end uint32, damper, soft []byte) *se.Performance {
	return &se.Performance{
		KB:     &se.KB{Format: 7, Model: 8, Text: "Test\r\n", Events: events, End: end},
		Damper: &se.Pedal{Samples: damper},
		Soft:   &se.Pedal{Samples: soft},
	}
}

func TestRoundTrip(t *testing.T) {
	events := []se.Event{
		{Tick: 0, Key: se.FillerKey},
		{Tick: 100, Key: 11, On: true, Vel: 350}, // a Zenph mark: kept as it is
		{Tick: 104, Key: 11},
		{Tick: 800, Key: 60, On: true, Vel: 200},
		{Tick: 800, Key: 64, On: true, Vel: 0},
		{Tick: 1200, Key: 60},
		{Tick: 1200, Key: 60, On: true, Vel: 300}, // repeated at the release
		{Tick: 1500, Key: 64},
		{Tick: 1600, Key: 60},
	}
	p := perf(events, 2000, make([]byte, 251), make([]byte, 126))
	d := FromPerformance(p)
	want := []Note{
		{Key: 60, On: 800, Off: 1200, Vel: 200},
		{Key: 64, On: 800, Off: 1500, Vel: 0},
		{Key: 60, On: 1200, Off: 1600, Vel: 300},
	}
	if !reflect.DeepEqual(d.Notes, want) {
		t.Fatalf("notes %+v, want %+v", d.Notes, want)
	}
	if len(d.Other) != 2 || d.End != 2000 || d.Format != 7 || d.Model != 8 {
		t.Fatalf("other %d end %d format %d model %d", len(d.Other), d.End, d.Format, d.Model)
	}
	back, err := d.Performance()
	if err != nil {
		t.Fatal(err)
	}
	got := back.KB.Notes()
	if !reflect.DeepEqual(got, p.KB.Notes()) {
		t.Errorf("events after the round trip:\n%+v\nwant\n%+v", got, p.KB.Notes())
	}
	if _, _, _, err := back.Encode(); err != nil {
		t.Fatal(err)
	}
}

func TestOverlapEndsEarlierNote(t *testing.T) {
	d := Doc{End: 1000, Notes: []Note{
		{Key: 60, On: 100, Off: 500, Vel: 200},
		{Key: 60, On: 300, Off: 700, Vel: 200}, // moved onto the first
	}}
	p, err := d.Performance()
	if err != nil {
		t.Fatal(err)
	}
	want := []se.Event{
		{Tick: 100, Key: 60, On: true, Vel: 200},
		{Tick: 300, Key: 60},
		{Tick: 300, Key: 60, On: true, Vel: 200},
		{Tick: 700, Key: 60},
	}
	if !reflect.DeepEqual(p.KB.Events, want) {
		t.Errorf("events %+v, want %+v", p.KB.Events, want)
	}
}

func TestCheck(t *testing.T) {
	for _, n := range []Note{
		{Key: 20, On: 1, Off: 2},
		{Key: 60, On: 5, Off: 5},
		{Key: 60, On: 1, Off: 2, Vel: 1024},
	} {
		if err := (Doc{Notes: []Note{n}}).Check(); err == nil {
			t.Errorf("%+v: no error", n)
		}
	}
}

func TestFrom(t *testing.T) {
	damper := make([]byte, 300)
	for i := 100; i < 200; i++ {
		damper[i] = 255 // down from tick 800 to 1600
	}
	d := Doc{End: 2400, Damper: damper, Soft: make([]byte, 150), Notes: []Note{
		{Key: 60, On: 400, Off: 1000, Vel: 200}, // sounding at 900: left out
		{Key: 62, On: 900, Off: 1100, Vel: 210},
	}}
	part := d.From(900, 800)
	if len(part.Notes) != 1 || part.Notes[0] != (Note{Key: 62, On: 800, Off: 1000, Vel: 210}) {
		t.Fatalf("notes %+v", part.Notes)
	}
	if part.End != 2300 {
		t.Errorf("end %d, want 2300", part.End)
	}
	// The damper is down at 900, so it is down through the lead-in, and
	// comes up where it did (1600 → 1500).
	if part.Damper[0] != 255 || part.Damper[1500/8-1] != 255 || part.Damper[1500/8+1] != 0 {
		t.Errorf("damper %v … %v", part.Damper[:3], part.Damper[1500/8-2:1500/8+2])
	}
	if same := d.From(0, 800); !reflect.DeepEqual(same, d) {
		t.Error("From(0) changed the piece")
	}
}

func TestUntil(t *testing.T) {
	damper := make([]byte, 400)
	for i := range damper {
		damper[i] = 200
	}
	d := Doc{End: 3200, Damper: damper, Notes: []Note{
		{Key: 60, On: 100, Off: 900, Vel: 200},
		{Key: 62, On: 500, Off: 2000, Vel: 200},  // let go at the cut
		{Key: 64, On: 1600, Off: 1800, Vel: 200}, // after it: left out
	}}
	part := d.Until(1600, 800)
	if len(part.Notes) != 2 || part.Notes[1].Off != 1600 || part.End != 2400 {
		t.Fatalf("%+v end %d", part.Notes, part.End)
	}
	if part.Damper[1600/8-1] != 200 || part.Damper[1600/8] != 0 || len(part.Damper) != 2400/8+1 {
		t.Errorf("damper %d samples, %v", len(part.Damper), part.Damper[1600/8-2:1600/8+2])
	}
}
