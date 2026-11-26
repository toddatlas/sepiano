package library

import (
	"math/rand"
	"path/filepath"
	"testing"
	"time"

	"sepiano/se"
	"sepiano/smf"
)

// midiFile writes a format-0 file from messages.
func midiFile(t *testing.T, msgs []smf.Message) []byte {
	t.Helper()
	b, err := smf.Write(msgs, 800, "")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestGrades: SE files are native; MIDI typed in from a score (chords
// exactly together, one loudness, a strict grid, no pedal) and MIDI of a
// person playing (spread chords, many loudnesses, varying timing, pedal
// depth) are told apart; an edited copy keeps its original's grade.
func TestGrades(t *testing.T) {
	data := t.TempDir()
	l, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	kb, lp, sp := set(t, 20)
	native, _, err := l.AddSet("native", kb, lp, sp, OriginImport, "")
	if err != nil || native.Grade != GradeNative {
		t.Fatalf("native: %q %v", native.Grade, err)
	}

	var score []smf.Message
	at := func(ms int) time.Duration { return time.Duration(ms) * time.Millisecond }
	for i := range 80 {
		t0 := i * 250 // eighth notes at 120 bpm
		for _, k := range []byte{60, 64, 67} {
			score = append(score, smf.Message{Time: at(t0), Status: 0x90, Data1: k + byte(i%5), Data2: 80},
				smf.Message{Time: at(t0 + 200), Status: 0x80, Data1: k + byte(i%5)})
		}
	}
	s, _, err := l.AddMIDI("typed.mid", midiFile(t, score), "")
	if err != nil || s.Grade != GradeSequenced {
		t.Fatalf("score: %q (%s) %v", s.Grade, s.GradeWhy, err)
	}

	rng := rand.New(rand.NewSource(1))
	var played []smf.Message
	ms := 0
	for i := range 160 {
		ms += 180 + rng.Intn(140)
		for j, k := range []byte{55, 62} {
			on := ms + j*(5+rng.Intn(25)) // chord notes spread a little
			played = append(played, smf.Message{Time: at(on), Status: 0x90, Data1: k + byte(i%7), Data2: byte(30 + rng.Intn(80))},
				smf.Message{Time: at(on + 150 + rng.Intn(200)), Status: 0x80, Data1: k + byte(i%7)})
		}
		played = append(played, smf.Message{Time: at(ms), Status: 0xB0, Data1: 64, Data2: byte(rng.Intn(128))})
	}
	p, _, err := l.AddMIDI("played.mid", midiFile(t, played), "")
	if err != nil || p.Grade != GradePerformance {
		t.Fatalf("performance: %q (%s) %v", p.Grade, p.GradeWhy, err)
	}

	dir, base, err := l.Files(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	perf, err := se.Load(filepath.Join(dir, base))
	if err != nil {
		t.Fatal(err)
	}
	for i := range perf.KB.Events { // a change, so it is a new copy
		if e := &perf.KB.Events[i]; e.On && e.Vel > 0 {
			e.Vel++
			break
		}
	}
	copyOfScore, _, err := l.AddEdited(s, perf, EditedCopy{Title: "Typed, edited"})
	if err != nil || copyOfScore.Grade != GradeSequenced {
		t.Fatalf("edited copy: %q %v", copyOfScore.Grade, err)
	}

	// Pieces from before grades existed are graded when the library opens.
	for _, id := range []string{p.ID, copyOfScore.ID, native.ID} {
		q := l.pieces[id]
		q.Grade, q.GradeWhy = "", ""
		if err := l.writeMeta(q); err != nil {
			t.Fatal(err)
		}
	}
	l2, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{p.ID: GradePerformance, copyOfScore.ID: GradeSequenced, native.ID: GradeNative} {
		if got, _ := l2.Get(id); got.Grade != want || got.GradeWhy == "" {
			t.Errorf("%s after reopening: %q (%s), want %q", got.Title, got.Grade, got.GradeWhy, want)
		}
	}
}
