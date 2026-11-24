package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"time"

	"sepiano/deck"
	"sepiano/edit"
	"sepiano/library"
	"sepiano/lut"
	"sepiano/piano"
	"sepiano/se"
)

// Per-key corrections for recordings made on this piano
// (piano.Corrections, docs/RECORDING-EDITOR.md): applied when such a
// recording plays, or saved into a corrected copy. Recordings themselves
// are never changed.

type correctionJSON struct {
	Note    int    `json:"note"`
	Name    string `json:"name"`
	Speed   int    `json:"speed"`
	Press   int    `json:"press_ms"`
	Release int    `json:"release_ms"`
}

type correctionsJSON struct {
	On       bool             `json:"on"`
	FillZero bool             `json:"fill_zero"`
	Keys     []correctionJSON `json:"keys"`
	Active   bool             `json:"active"`
	Summary  string           `json:"summary"`
	Updated  *time.Time       `json:"updated,omitempty"`
}

func toCorrectionsJSON(c piano.Corrections) correctionsJSON {
	out := correctionsJSON{On: c.On, FillZero: c.FillZero, Keys: []correctionJSON{}, Active: c.Active(), Summary: c.Summary()}
	for _, n := range c.Notes() {
		k := c.Keys[n]
		out.Keys = append(out.Keys, correctionJSON{Note: n, Name: lut.NoteName(n), Speed: k.Speed, Press: k.Press, Release: k.Release})
	}
	if !c.Updated.IsZero() {
		t := c.Updated
		out.Updated = &t
	}
	return out
}

// corrections returns the stored corrections; none if they cannot be read.
func (s *Server) corrections() piano.Corrections {
	if s.Corrections == nil {
		return piano.Corrections{Keys: map[int]piano.KeyCorrection{}}
	}
	c, err := s.Corrections.Load()
	if err != nil {
		s.logf("corrections: %v", err)
	}
	return c
}

func (s *Server) getCorrections(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, toCorrectionsJSON(s.corrections()))
}

func (s *Server) putCorrections(w http.ResponseWriter, r *http.Request) {
	if s.Corrections == nil {
		fail(w, 503, "corrections are not available")
		return
	}
	var req correctionsJSON
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, err.Error())
		return
	}
	c := piano.Corrections{On: req.On, FillZero: req.FillZero, Keys: map[int]piano.KeyCorrection{}}
	for _, k := range req.Keys {
		if _, dup := c.Keys[k.Note]; dup {
			fail(w, 400, fmt.Sprintf("%s is listed twice", lut.NoteName(k.Note)))
			return
		}
		c.Keys[k.Note] = piano.KeyCorrection{Speed: k.Speed, Press: k.Press, Release: k.Release}
	}
	saved, err := s.Corrections.Save(c, time.Now())
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	s.logf("corrections: %s", saved.Summary())
	writeJSON(w, 200, toCorrectionsJSON(saved))
}

// applyCorrections corrects notes sent by the editor and sends them back,
// so the editor and playback correct in the same way.
func (s *Server) applyCorrections(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Notes [][4]int64 `json:"notes"`
	}
	body := http.MaxBytesReader(w, r.Body, maxEditBody)
	if err := json.NewDecoder(body).Decode(&req); err != nil && err != io.EOF {
		fail(w, 400, "bad request body: "+err.Error())
		return
	}
	c := s.corrections()
	if c.Empty() {
		fail(w, 409, "there are no corrections yet")
		return
	}
	notes, err := notesFrom(req.Notes)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	d := edit.Doc{Notes: notes}
	changed := c.Apply(&d)
	out := make([][4]uint32, len(d.Notes))
	for i, n := range d.Notes {
		out[i] = [4]uint32{uint32(n.Key), n.On, n.Off, uint32(n.Vel)}
	}
	writeJSON(w, 200, map[string]any{"notes": out, "changed": changed, "summary": c.Summary()})
}

// notesFrom checks notes in the editor's form: [key, on, off, velocity].
func notesFrom(in [][4]int64) ([]edit.Note, error) {
	if len(in) > edit.MaxNotes {
		return nil, fmt.Errorf("too many notes (at most %d)", edit.MaxNotes)
	}
	out := make([]edit.Note, len(in))
	for i, n := range in {
		key, on, off, vel := n[0], n[1], n[2], n[3]
		if key < 0 || key > 127 || on < 0 || off < 0 || on > edit.MaxTicks || off > edit.MaxTicks || vel < 0 || vel > se.MaxVelocity {
			return nil, fmt.Errorf("note %d is out of range", i+1)
		}
		out[i] = edit.Note{Key: uint8(key), On: uint32(on), Off: uint32(off), Vel: uint16(vel)}
	}
	return out, nil
}

// correctFix is the Join fix that corrects the recordings among a
// collection's pieces, or nil when there is nothing to correct.
func (s *Server) correctFix() library.Fix {
	c := s.corrections()
	if !c.Active() {
		return nil
	}
	return func(p library.Piece, perf *se.Performance) (*se.Performance, bool, error) {
		if !p.NeedsCorrection() {
			return perf, false, nil
		}
		out, n, err := c.ApplyTo(perf)
		return out, n > 0, err
	}
}

// correct gives a single-piece job the recording corrections when they
// apply: it then plays a corrected copy from a fresh play folder.
func (s *Server) correct(job *deck.Job, p library.Piece) error {
	fix := s.correctFix()
	if fix == nil || !p.NeedsCorrection() {
		return nil
	}
	perf, err := se.Load(filepath.Join(job.Dir, job.Name))
	if err != nil {
		return err
	}
	out, changed, err := fix(p, perf)
	if err != nil || !changed {
		return err
	}
	dir, err := s.playDir()
	if err != nil {
		return err
	}
	if err := out.Save(filepath.Join(dir, "c")); err != nil {
		return err
	}
	job.Dir, job.Name, job.Corrected = dir, "c", true
	return nil
}

// correctedCopy saves a recording, corrected, as a new edited copy.
func (s *Server) correctedCopy(w http.ResponseWriter, r *http.Request) {
	p, d, err := s.loadDoc(r.PathValue("id"))
	if errors.Is(err, library.ErrNotFound) {
		fail(w, 404, "no such piece")
		return
	}
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	if !p.NeedsCorrection() {
		fail(w, 409, "only a recording made on this piano, not corrected yet, can be corrected")
		return
	}
	c := s.corrections()
	if c.Empty() {
		fail(w, 409, "there are no corrections yet")
		return
	}
	changed := c.Apply(&d)
	perf, err := d.Performance()
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	note := fmt.Sprintf("Corrected from “%s”: %s.", p.Title, c.Summary())
	saved, dup, err := s.Lib.AddEdited(p, perf, library.EditedCopy{Title: p.Title + " (corrected)", Note: note, Corrected: true})
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	if !dup {
		s.logf("corrected copy %s of %s (%d notes changed)", saved.ID, p.ID, changed)
		s.libraryChanged(saved.ID)
	}
	writeJSON(w, 201, map[string]any{"piece": saved, "changed": changed, "duplicate": dup})
}
