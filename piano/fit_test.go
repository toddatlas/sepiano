package piano

import (
	"path/filepath"
	"testing"

	"sepiano/se"
)

func TestFit(t *testing.T) {
	p := &se.Performance{
		KB: &se.KB{Events: []se.Event{
			{Tick: 10, Key: 60, On: true, Vel: 200},
			{Tick: 20, Key: 62, On: true, Vel: 700}, // a silent press
			{Tick: 30, Key: 0, On: true},            // the sostenuto
			{Tick: 40, Key: 64, On: true, Vel: 600}, // 750 would be silent: kept at 639
		}},
		Damper: &se.Pedal{Samples: []byte{0, 100, 200, 255}},
		Soft:   &se.Pedal{Samples: []byte{0, 255}},
	}
	f := Fit{Speed: -20, Damper: 120} // softer, dampers higher
	if !f.Active() || f.Check() != nil {
		t.Fatal("fit not active or refused")
	}
	f.Apply(p)
	ev := p.KB.Events
	if ev[0].Vel != 250 || ev[1].Vel != 700 || ev[3].Vel != SilentVelocity-1 {
		t.Errorf("velocities %d %d %d", ev[0].Vel, ev[1].Vel, ev[3].Vel)
	}
	if got := p.Damper.Samples; got[1] != 120 || got[2] != 240 || got[3] != 255 {
		t.Errorf("damper %v", got)
	}
	if got := p.Soft.Samples; got[1] != 255 {
		t.Errorf("soft changed: %v", got)
	}
	if (Fit{Speed: 200}).Check() == nil || (Fit{Damper: 10}).Check() == nil {
		t.Error("out-of-range fits accepted")
	}

	s := &FitStore{Path: filepath.Join(t.TempDir(), "fits.json")}
	saved, err := s.Save(Fits{On: true, Collections: map[string]Fit{"MAESTRO v3": {Speed: 5}, "Batik plays Mozart": {}}})
	if err != nil || len(saved.Collections) != 1 {
		t.Fatalf("save: %+v %v", saved, err)
	}
	back, _ := s.Load()
	if f, ok := back.For("MAESTRO v3"); !ok || f.Speed != 5 || f.Damper != 100 {
		t.Errorf("loaded %+v %v", f, ok)
	}
	if _, ok := back.For("Batik plays Mozart"); ok {
		t.Error("a fit that changes nothing applies")
	}
	if _, ok := back.For(""); ok {
		t.Error("a piece with no collection is fitted")
	}
}
