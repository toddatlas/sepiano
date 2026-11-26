package se

import (
	"bytes"
	"io"
	"testing"
)

// TestReaderChangesOnlyWhatIsToCome: the lazy encoder applies vel as it
// reaches each event, so a change partway through (the volume on the Pi)
// leaves everything already read as it was, and the result is still a
// valid file.
func TestReaderChangesOnlyWhatIsToCome(t *testing.T) {
	kb := &KB{Format: 0x0F, Model: 0x11, Seed: [4]byte{1, 2, 3, 4}, Text: "Test\r\n"}
	for i := range 400 {
		kb.Events = append(kb.Events, Event{Tick: uint32(100 + i*40), Key: uint8(40 + i%40), On: true, Vel: 200},
			Event{Tick: uint32(120 + i*40), Key: uint8(40 + i%40)})
	}
	kb.End = 100 + 400*40 + 800
	want, err := kb.Encode(true)
	if err != nil {
		t.Fatal(err)
	}
	same, _ := kb.Reader(true, func(e Event) uint16 { return e.Vel })
	got, _ := io.ReadAll(same)
	if !bytes.Equal(got, want) {
		t.Fatal("the reader with no change differs from Encode")
	}

	softer := false
	r, err := kb.Reader(true, func(e Event) uint16 {
		if softer {
			return e.Vel * 2
		}
		return e.Vel
	})
	if err != nil {
		t.Fatal(err)
	}
	first := make([]byte, 1024)
	if _, err := io.ReadFull(r, first); err != nil {
		t.Fatal(err)
	}
	softer = true
	rest, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	all := append(first, rest...)
	if !bytes.Equal(all[:1024], want[:1024]) {
		t.Fatal("bytes already read changed")
	}
	back, err := ParseKB(all)
	if err != nil {
		t.Fatal(err)
	}
	var before, after int
	for _, e := range back.Events {
		switch {
		case !e.On || e.Filler():
		case e.Vel == 200:
			before++
		case e.Vel == 400:
			after++
		default:
			t.Fatalf("velocity %d", e.Vel)
		}
	}
	if before == 0 || after == 0 || before+after != 400 {
		t.Fatalf("%d notes as they were, %d softer", before, after)
	}
}
