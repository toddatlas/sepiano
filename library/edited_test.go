package library

import (
	"testing"

	"sepiano/se"
)

// TestJoinKeepsSostenuto: joining pieces for an album or play list keeps
// the sostenuto pedal's events (key 0), and drops only the fillers.
func TestJoinKeepsSostenuto(t *testing.T) {
	mk := func() *se.Performance {
		return &se.Performance{
			KB: &se.KB{End: 1600, Events: []se.Event{
				{Tick: 0, Key: se.FillerKey},
				{Tick: 100, Key: 60, On: true, Vel: 200},
				{Tick: 200, Key: 0, On: true},
				{Tick: 300, Key: 60},
				{Tick: 1000, Key: 0},
			}},
			Damper: &se.Pedal{Samples: make([]byte, 201)},
			Soft:   &se.Pedal{Samples: make([]byte, 101)},
		}
	}
	joined, starts, err := JoinPerformances([]*se.Performance{mk(), mk()}, "Album")
	if err != nil {
		t.Fatal(err)
	}
	var marks []uint32
	for _, e := range joined.KB.Events {
		if e.Key == 0 {
			marks = append(marks, e.Tick)
		}
		if e.Key == se.FillerKey {
			t.Errorf("a filler was kept at %d", e.Tick)
		}
	}
	if len(marks) != 4 || marks[2] != starts[1]+200 || marks[3] != starts[1]+1000 {
		t.Errorf("sostenuto events at %v (second part starts at %d)", marks, starts[1])
	}
}

// A copy of a corrected copy (from the editor or Stahnke's edit.exe, which
// does not say) is corrected too, so playback never corrects it twice.
func TestCopyOfCorrectedStaysCorrected(t *testing.T) {
	l, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	perf := func(vel uint16) *se.Performance {
		return &se.Performance{
			KB:     &se.KB{Format: 0x0F, Model: 0x11, End: 1600, Events: []se.Event{{Tick: 100, Key: 60, On: true, Vel: vel}, {Tick: 300, Key: 60}}},
			Damper: &se.Pedal{Samples: make([]byte, 201)},
			Soft:   &se.Pedal{Samples: make([]byte, 101)},
		}
	}
	kb, lp, sp, _ := perf(200).Encode()
	rec, _, err := l.AddSet("rec", kb, lp, sp, OriginRecorded, "Take 1")
	if err != nil {
		t.Fatal(err)
	}
	if !rec.NeedsCorrection() {
		t.Fatal("a recording needs correcting")
	}
	corr, _, err := l.AddEdited(rec, perf(180), EditedCopy{Corrected: true})
	if err != nil || !corr.Corrected || corr.NeedsCorrection() {
		t.Fatalf("corrected copy: %+v %v", corr, err)
	}
	again, _, err := l.AddEdited(corr, perf(170), EditedCopy{})
	if err != nil || !again.Corrected || again.NeedsCorrection() || !again.FromRecording {
		t.Fatalf("copy of the corrected copy: %+v %v", again, err)
	}
	if p, err := l.ReplaceEdited(again.ID, perf(160), false); err != nil || !p.Corrected {
		t.Fatalf("replaced: %+v %v", p, err)
	}
	plain, _, err := l.AddEdited(rec, perf(190), EditedCopy{})
	if err != nil || plain.Corrected || !plain.NeedsCorrection() {
		t.Fatalf("plain copy: %+v %v", plain, err)
	}
}
