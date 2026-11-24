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
