package api

import (
	"net/http"
	"path/filepath"
	"strings"

	"sepiano/deck"
	"sepiano/library"
	"sepiano/piano"
	"sepiano/se"
)

// pieceKeys reports the velocities each key has in a piece: for a
// recording, what each key's hammer sensor measured, with the keys that
// stand apart from their neighbours.
func (s *Server) pieceKeys(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := s.Lib.Get(id)
	if err != nil {
		fail(w, 404, "no such piece")
		return
	}
	dir, base, err := s.Lib.Files(id)
	if err != nil {
		fail(w, 404, "no such piece")
		return
	}
	perf, err := se.Load(filepath.Join(dir, base))
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	c := piano.CheckKeys(perf)
	rc := piano.CheckReleases(perf)
	pc := piano.CheckPresses(perf)
	writeJSON(w, 200, map[string]any{
		"piece": ref(p), "origin": p.Origin, "check": c, "releases": rc,
		// Corrections the stand-out keys suggest, for a recording made here.
		"recorded_here": p.RecordedHere(), "corrected": p.Corrected,
		"presses": pc, "suggestions": piano.Suggest(c, rc, pc),
	})
}

// keyCheckMany checks several recordings together (GET
// /keycheck?pieces=a,b,c): more notes and chords on each key give firmer
// suggestions. The performances are laid end to end, so no chord spans two.
func (s *Server) keyCheckMany(w http.ResponseWriter, r *http.Request) {
	ids := strings.Split(r.URL.Query().Get("pieces"), ",")
	var perfs []*se.Performance
	var refs []deck.PieceRef
	recordedHere, corrected := true, false
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		p, err := s.Lib.Get(id)
		if err != nil {
			fail(w, 404, "no such piece: "+id)
			return
		}
		dir, base, _ := s.Lib.Files(id)
		perf, err := se.Load(filepath.Join(dir, base))
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		perfs = append(perfs, perf)
		refs = append(refs, ref(p))
		recordedHere = recordedHere && p.RecordedHere()
		corrected = corrected || p.Corrected
	}
	if len(perfs) == 0 {
		fail(w, 400, "give the pieces to check: ?pieces=a,b")
		return
	}
	if len(perfs) > 50 {
		fail(w, 400, "at most 50 pieces at once")
		return
	}
	all, _, err := library.JoinPerformances(perfs, "")
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	c := piano.CheckKeys(all)
	rc := piano.CheckReleases(all)
	pc := piano.CheckPresses(all)
	writeJSON(w, 200, map[string]any{
		"pieces": refs, "check": c, "releases": rc,
		"recorded_here": recordedHere, "corrected": corrected,
		"presses": pc, "suggestions": piano.Suggest(c, rc, pc),
	})
}
