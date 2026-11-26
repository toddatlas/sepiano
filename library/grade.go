package library

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"sepiano/se"
)

// Grades say where a piece's performance data comes from, best first, so the
// library can offer the most accurate files (docs/ROADMAP.md, idea 4).
const (
	// GradeNative: SE files from an SE piano, at the format's full
	// resolution: loudness in 1,024 steps, timing to 1.25 ms, pedal depth.
	GradeNative = "native"
	// GradePerformance: converted from a MIDI file of a person playing
	// (a Disklavier or SE capture, or a careful transcription): timing and
	// loudness vary as a player's do, at MIDI's 127 loudness steps.
	GradePerformance = "performance"
	// GradeSequenced: converted from a MIDI file made on a computer, typed in
	// from a score or generated: chord notes start together exactly, few
	// loudness levels, a strict grid. (Four "ISI … BMP100" files in the
	// owner's collection grade so; 33 Disklavier performances do not.)
	GradeSequenced = "sequenced"
)

// gradeOf grades a piece from its origin and its data. Only pieces
// converted from MIDI need the data: everything else came as SE files.
func gradeOf(origin string, kb *se.KB, damper *se.Pedal) (grade, why string) {
	if origin != OriginMIDI {
		return GradeNative, "SE files: loudness in 1,024 steps, timing to 1.25 ms, pedal depth"
	}
	s := measure(kb, damper)
	if s.notes < 30 {
		return GradePerformance, "converted from MIDI; too short to tell how it was made"
	}
	var signs []string
	points := 0.0
	if s.together > 0.35 {
		points++
		signs = append(signs, fmt.Sprintf("%.0f%% of notes start exactly with another", s.together*100))
	}
	if s.levels <= 16 {
		points++
		signs = append(signs, fmt.Sprintf("only %d loudness levels", s.levels))
	}
	if s.grid > 0.6 {
		points++
		signs = append(signs, fmt.Sprintf("%.0f%% of the gaps between notes fall on a few lengths", s.grid*100))
	}
	if s.pedalLevels <= 2 {
		points += 0.5
		if s.pedalLevels == 2 {
			signs = append(signs, "the pedal only fully up or down")
		} else {
			signs = append(signs, "no pedal")
		}
	}
	if points >= 2 {
		return GradeSequenced, "converted from MIDI made on a computer (typed in or generated): " + strings.Join(signs, "; ")
	}
	return GradePerformance, fmt.Sprintf("converted from MIDI of a person playing: %d loudness levels, timing that varies, %s",
		s.levels, map[bool]string{true: "pedal depth recorded", false: "pedal on and off"}[s.pedalLevels > 2])
}

type gradeStats struct {
	notes       int
	together    float64 // the share of notes starting on the same tick as another
	levels      int     // distinct velocities
	grid        float64 // the share of gaps between onsets near one of the five commonest
	pedalLevels int     // distinct damper positions
}

func measure(kb *se.KB, damper *se.Pedal) gradeStats {
	var s gradeStats
	onsets := map[uint32]int{}
	vels := map[uint16]bool{}
	for _, e := range kb.Events {
		if !e.On || e.Key < 21 || e.Key > 108 {
			continue
		}
		s.notes++
		onsets[e.Tick]++
		vels[e.Vel] = true
	}
	s.levels = len(vels)
	if s.notes == 0 {
		return s
	}
	shared := 0
	ticks := make([]uint32, 0, len(onsets))
	for t, n := range onsets {
		if n > 1 {
			shared += n
		}
		ticks = append(ticks, t)
	}
	s.together = float64(shared) / float64(s.notes)
	sort.Slice(ticks, func(i, j int) bool { return ticks[i] < ticks[j] })
	gaps := map[uint32]int{}
	total := 0
	for i := 1; i < len(ticks); i++ {
		gaps[ticks[i]-ticks[i-1]]++
		total++
	}
	if total > 0 {
		// The five commonest gaps, each taking its neighbours within a tick
		// (a MIDI grid rounds to ticks unevenly).
		taken := 0
		for range 5 {
			best, bestN := uint32(0), 0
			for g := range gaps {
				if n := gaps[g] + gaps[g-1] + gaps[g+1]; n > bestN {
					best, bestN = g, n
				}
			}
			if bestN == 0 {
				break
			}
			taken += bestN
			delete(gaps, best-1)
			delete(gaps, best)
			delete(gaps, best+1)
		}
		s.grid = float64(taken) / float64(total)
	}
	if damper != nil {
		levels := map[byte]bool{}
		for _, v := range damper.Samples {
			levels[v] = true
		}
		s.pedalLevels = len(levels)
		if s.pedalLevels == 1 && levels[0] {
			s.pedalLevels = 0 // never pressed
		}
	}
	return s
}

// gradeFromFiles grades a piece already in the library from its files.
func (l *Library) gradeFromFiles(p *Piece) {
	if p.Origin != OriginMIDI {
		p.Grade, p.GradeWhy = gradeOf(p.Origin, nil, nil)
		return
	}
	kbData, err := os.ReadFile(filepath.Join(l.dir, p.ID+".kb"))
	if err != nil {
		return
	}
	kb, err := se.ParseKB(kbData)
	if err != nil {
		return
	}
	var damper *se.Pedal
	if lp, err := os.ReadFile(filepath.Join(l.dir, p.ID+".lp")); err == nil {
		damper, _ = se.ParsePedal(lp)
	}
	p.Grade, p.GradeWhy = gradeOf(p.Origin, kb, damper)
}

// gradeAll grades the pieces that have no grade yet (made before grades
// existed): originals from their files, then edited copies from the piece
// each was made from. It returns the pieces it changed.
func (l *Library) gradeAll(pieces map[string]*Piece) []*Piece {
	var changed []*Piece
	for _, p := range pieces {
		if p.Grade == "" && p.Origin != OriginEdited {
			l.gradeFromFiles(p)
			if p.Grade != "" {
				changed = append(changed, p)
			}
		}
	}
	var copyOf func(p *Piece, depth int)
	copyOf = func(p *Piece, depth int) { // a copy of a copy: its original first
		if p.Grade != "" || p.Origin != OriginEdited {
			return
		}
		from := pieces[p.EditedFrom]
		if from != nil && depth < 50 {
			copyOf(from, depth+1)
		}
		p.Grade, p.GradeWhy = editedGrade(from)
		changed = append(changed, p)
	}
	for _, p := range pieces {
		copyOf(p, 0)
	}
	return changed
}

// editedGrade is an edited copy's grade: its original's, since editing
// does not change where the data came from.
func editedGrade(from *Piece) (string, string) {
	if from == nil || from.Grade == "" {
		return GradeNative, "an edited copy of SE files"
	}
	return from.Grade, "an edited copy: " + from.GradeWhy
}
