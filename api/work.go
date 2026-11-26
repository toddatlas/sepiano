package api

import (
	"net/http"
	"path/filepath"

	"sepiano/piano"
	"sepiano/se"
)

// pieceWork measures how hard a piece works the piano's key solenoids
// (piano.MeasureWork), from its files as stored.
func (s *Server) pieceWork(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
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
	writeJSON(w, 200, piano.MeasureWork(perf))
}
