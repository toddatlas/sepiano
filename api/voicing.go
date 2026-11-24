package api

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"

	"sepiano/deck"
	"sepiano/lut"
	"sepiano/piano"
	"sepiano/se"
)

// Software voicing (docs/SETTINGS-EDITOR.md, step 2): per-key trims and a
// whole-piano offset applied to the velocities of every performance as it
// is served to the player. Nothing is written to the piano.

func (s *Server) voicingRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/piano/voicing", s.getVoicing)
	m.HandleFunc("PUT /api/v1/piano/voicing", s.putVoicing)
	m.HandleFunc("POST /api/v1/piano/audition", s.audition)
	m.HandleFunc("POST /api/v1/deck/replay", s.replay)
}

// voicingJSON is a voicing as the API shows it: trims keyed by note number
// (as JSON object keys) and listed with their names.
type voicingJSON struct {
	On      bool           `json:"on"`
	Offset  int            `json:"offset"`
	Trims   map[string]int `json:"trims"`
	Active  bool           `json:"active"`
	MaxTrim int            `json:"max_trim"`
	Keys    []voicedKey    `json:"keys"`
}

type voicedKey struct {
	Note int    `json:"note"`
	Name string `json:"name"`
	Trim int    `json:"trim"`
}

func toVoicingJSON(v piano.Voicing) voicingJSON {
	out := voicingJSON{On: v.On, Offset: v.Offset, Trims: map[string]int{}, Active: v.Active(), MaxTrim: piano.MaxTrim, Keys: []voicedKey{}}
	for _, n := range v.TrimmedNotes() {
		out.Trims[strconv.Itoa(n)] = v.Trims[n]
		out.Keys = append(out.Keys, voicedKey{n, lut.NoteName(n), v.Trims[n]})
	}
	return out
}

func (s *Server) voicing() piano.Voicing {
	if s.Voicing == nil {
		return piano.Voicing{}
	}
	v, err := s.Voicing.Load()
	if err != nil {
		s.logf("voicing: %v", err)
	}
	return v
}

func (s *Server) getVoicing(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, toVoicingJSON(s.voicing()))
}

func (s *Server) putVoicing(w http.ResponseWriter, r *http.Request) {
	if s.Voicing == nil {
		fail(w, 503, "no voicing store configured")
		return
	}
	var req struct {
		On     bool           `json:"on"`
		Offset int            `json:"offset"`
		Trims  map[string]int `json:"trims"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, err.Error())
		return
	}
	v := piano.Voicing{On: req.On, Offset: req.Offset, Trims: map[int]int{}}
	for k, t := range req.Trims {
		n, err := strconv.Atoi(k)
		if err != nil {
			fail(w, 400, fmt.Sprintf("trims are keyed by note number, not %q", k))
			return
		}
		v.Trims[n] = t
	}
	saved, err := s.Voicing.Save(v)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, toVoicingJSON(saved))
}

// voice gives a job the software voicing, if any: it plays a voiced copy
// from a fresh play folder (or voices a joined performance in place). The
// library's own files are never changed.
func (s *Server) voice(job *deck.Job, inPlayDir bool) error {
	v := s.voicing()
	if !v.Active() {
		return nil
	}
	p, err := se.Load(filepath.Join(job.Dir, job.Name))
	if err != nil {
		return err
	}
	if v.Apply(p) == 0 {
		return nil // no trimmed key plays in this piece
	}
	dir := job.Dir
	if !inPlayDir {
		if dir, err = s.playDir(); err != nil {
			return err
		}
	}
	if err := p.Save(filepath.Join(dir, "v")); err != nil {
		return err
	}
	job.Dir, job.Name, job.Voiced = dir, "v", true
	return nil
}

// audition plays test notes for one key: at four dynamics, the note as
// recorded and then with a trim (the key's software trim, or the one given).
func (s *Server) audition(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Note int  `json:"note"`
		Trim *int `json:"trim"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if req.Note < 21 || req.Note > 108 {
		fail(w, 400, "the note must be a piano key (21 to 108)")
		return
	}
	v := s.voicing()
	trim := v.TrimFor(req.Note)
	if req.Trim != nil {
		trim = *req.Trim
	}
	if trim < -piano.MaxTrim*2 || trim > piano.MaxTrim*2 {
		fail(w, 400, "trim out of range")
		return
	}
	dir, err := s.playDir()
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	p := piano.TestPhrase(req.Note, trim)
	if err := p.Save(filepath.Join(dir, "t")); err != nil {
		fail(w, 500, err.Error())
		return
	}
	title := fmt.Sprintf("Test notes: %s, as recorded then %+d", lut.NoteName(req.Note), trim)
	job := deck.Job{Piece: deck.PieceRef{Title: title, Performer: "Voicing"}, Dir: dir, Name: "t", Duration: p.Duration()}
	ctx, cancel := cmdContext(r)
	defer cancel()
	if err := s.Deck.Play(ctx, job); err != nil {
		deckError(w, err)
		return
	}
	s.qmu.Lock()
	s.queue = queue{}
	s.qmu.Unlock()
	writeJSON(w, 202, s.Deck.Status())
}

// replay plays the current track again from its start, so a voicing change
// can be heard at once.
func (s *Server) replay(w http.ResponseWriter, r *http.Request) {
	st := s.Deck.Status()
	s.qmu.Lock()
	q := s.queue
	s.qmu.Unlock()
	if st.Piece == nil || len(q.ids) == 0 {
		fail(w, 409, "nothing from the library is playing")
		return
	}
	target := 0
	if q.coll == nil && st.Piece.ID != q.ids[0] {
		fail(w, 409, "nothing from the library is playing")
		return
	}
	if q.coll != nil {
		if st.Collection == nil || q.coll.Kind != st.Collection.Kind || q.coll.ID != st.Collection.ID {
			fail(w, 409, "nothing from the library is playing")
			return
		}
		target = st.Collection.Start + st.TrackIndex
	}
	ctx, cancel := cmdContext(r)
	defer cancel()
	if err := s.startQueue(ctx, q.ids, q.coll, target); err != nil {
		playError(w, err)
		return
	}
	writeJSON(w, 202, s.Deck.Status())
}
