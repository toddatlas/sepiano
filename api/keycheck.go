package api

import (
	"net/http"
	"path/filepath"

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
	writeJSON(w, 200, map[string]any{"piece": ref(p), "origin": p.Origin, "check": c})
}
