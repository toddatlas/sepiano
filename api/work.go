package api

import (
	"net/http"
	"path/filepath"

	"sepiano/lut"
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
	// too_loud: notes louder than this piano can strike; they play at the
	// key's limit (piano.StrikeLimits).
	writeJSON(w, 200, struct {
		piano.Work
		TooLoud int `json:"too_loud"`
	}{piano.MeasureWork(perf), s.strikeLimits().TooLoud(perf)})
}

// loadStrikeLimits works out, from the newest backup of the piano's table,
// the loudest velocity each key can strike, and gives them to the deck
// (piano.StrikeLimits). Without a backup there are none, and a note too
// loud for its key would stop the player.
func (s *Server) loadStrikeLimits() {
	if s.Piano == nil {
		return
	}
	img, b, err := s.Piano.Latest()
	if err != nil {
		s.logf("strike limits: no backup of the piano's table yet (%v): notes are not limited", err)
		return
	}
	t, err := lut.Parse(img)
	if err != nil {
		s.logf("strike limits: backup %s: %v", b.ID, err)
		return
	}
	l := piano.NewStrikeLimits(t)
	s.strikeMu.Lock()
	s.strike = &l
	s.strikeMu.Unlock()
	arr := [128]uint16(l)
	s.Deck.SetStrikeLimits(&arr)
	s.logf("strike limits from backup %s", b.ID)
}

func (s *Server) strikeLimits() *piano.StrikeLimits {
	s.strikeMu.Lock()
	defer s.strikeMu.Unlock()
	return s.strike
}
