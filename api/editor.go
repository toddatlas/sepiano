package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"

	"sepiano/deck"
	"sepiano/edit"
	"sepiano/library"
	"sepiano/se"
)

// The piano-roll editor: a piece as notes and pedal curves, saved as an
// edited copy (the original is never changed), and heard from any point.
// docs/RECORDING-EDITOR.md.

// PreviewLead is the silence before a preview from the middle of a piece,
// while the pedals take their places.
const PreviewLead = se.TicksPerSecond

// PreviewPerformer marks a preview in the deck's status, so the editor can
// follow it with its playhead.
const PreviewPerformer = "Editor preview"

// maxEditBody limits an editor request: a long piece's notes and pedal
// curves as JSON.
const maxEditBody = 32 << 20

type performanceJSON struct {
	Piece        library.Piece `json:"piece"`
	TickHz       int           `json:"tick_hz"`
	End          uint32        `json:"end"`
	Text         string        `json:"text"`
	Notes        [][4]uint32   `json:"notes"` // key, on, off, velocity
	Damper       []int         `json:"damper"`
	DamperPeriod int           `json:"damper_period"`
	Soft         []int         `json:"soft"`
	SoftPeriod   int           `json:"soft_period"`
	Kept         int           `json:"kept"` // key events outside the keyboard, kept as they are
}

type editRequest struct {
	Notes   [][4]int64 `json:"notes"`
	Damper  []int      `json:"damper"`
	Soft    []int      `json:"soft"`
	End     int64      `json:"end"`
	Title   string     `json:"title"`
	Replace bool       `json:"replace"`
	From    int64      `json:"from"`
	To      int64      `json:"to"` // preview: stop here (0: to the end)
	// Corrected says the recording corrections are in these notes.
	Corrected bool `json:"corrected"`
}

func ints(b []byte) []int {
	out := make([]int, len(b))
	for i, v := range b {
		out[i] = int(v)
	}
	return out
}

// loadDoc reads a piece's performance as the editor sees it.
func (s *Server) loadDoc(id string) (library.Piece, edit.Doc, error) {
	p, err := s.Lib.Get(id)
	if err != nil {
		return library.Piece{}, edit.Doc{}, err
	}
	dir, base, err := s.Lib.Files(id)
	if err != nil {
		return library.Piece{}, edit.Doc{}, err
	}
	perf, err := se.Load(filepath.Join(dir, base))
	if err != nil {
		return library.Piece{}, edit.Doc{}, err
	}
	return p, edit.FromPerformance(perf), nil
}

func (s *Server) getPerformance(w http.ResponseWriter, r *http.Request) {
	p, d, err := s.loadDoc(r.PathValue("id"))
	if errors.Is(err, library.ErrNotFound) {
		fail(w, 404, "no such piece")
		return
	}
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	out := performanceJSON{Piece: p, TickHz: se.TicksPerSecond, End: d.End, Text: d.Text,
		Notes: make([][4]uint32, len(d.Notes)), Damper: ints(d.Damper), DamperPeriod: se.DamperPeriod,
		Soft: ints(d.Soft), SoftPeriod: se.SoftPeriod, Kept: len(d.Other)}
	for i, n := range d.Notes {
		out.Notes[i] = [4]uint32{uint32(n.Key), n.On, n.Off, uint32(n.Vel)}
	}
	writeJSON(w, 200, out)
}

// editedDoc reads an editor request and applies it to the piece's own
// performance, which supplies what the editor does not show.
func (s *Server) editedDoc(w http.ResponseWriter, r *http.Request) (library.Piece, edit.Doc, editRequest, bool) {
	var req editRequest
	body := http.MaxBytesReader(w, r.Body, maxEditBody)
	if err := json.NewDecoder(body).Decode(&req); err != nil && err != io.EOF {
		fail(w, 400, "bad request body: "+err.Error())
		return library.Piece{}, edit.Doc{}, req, false
	}
	p, d, err := s.loadDoc(r.PathValue("id"))
	if errors.Is(err, library.ErrNotFound) {
		fail(w, 404, "no such piece")
		return p, d, req, false
	}
	if err != nil {
		fail(w, 500, err.Error())
		return p, d, req, false
	}
	bad := func(msg string) (library.Piece, edit.Doc, editRequest, bool) {
		fail(w, 400, msg)
		return p, d, req, false
	}
	notes, err := notesFrom(req.Notes)
	if err != nil {
		return bad(err.Error())
	}
	d.Notes = notes
	for _, c := range []struct {
		src  []int
		dst  *[]byte
		name string
	}{{req.Damper, &d.Damper, "damper"}, {req.Soft, &d.Soft, "soft pedal"}} {
		if c.src == nil {
			continue // unchanged
		}
		*c.dst = make([]byte, len(c.src))
		for i, v := range c.src {
			if v < 0 || v > 255 {
				return bad(fmt.Sprintf("%s sample %d is out of range (0 to 255)", c.name, i))
			}
			(*c.dst)[i] = byte(v)
		}
	}
	if req.End > 0 && req.End <= edit.MaxTicks {
		d.End = uint32(req.End)
	}
	if err := d.Check(); err != nil {
		return bad(err.Error())
	}
	return p, d, req, true
}

// saveEdit stores an edited performance: as a new edited copy, or over the
// edited copy it came from when replace is set.
func (s *Server) saveEdit(w http.ResponseWriter, r *http.Request) {
	p, d, req, ok := s.editedDoc(w, r)
	if !ok {
		return
	}
	perf, err := d.Performance()
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	if req.Replace {
		saved, err := s.Lib.ReplaceEdited(p.ID, perf, req.Corrected)
		switch {
		case errors.Is(err, library.ErrNotEdited):
			fail(w, 409, err.Error())
		case err != nil:
			fail(w, 409, err.Error())
		default:
			s.logf("edited %s saved over", p.ID)
			s.libraryChanged(p.ID)
			writeJSON(w, 200, map[string]any{"piece": saved, "replaced": true})
		}
		return
	}
	saved, dup, err := s.Lib.AddEdited(p, perf, library.EditedCopy{Title: req.Title, Corrected: req.Corrected})
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	if !dup {
		s.logf("edited copy %s of %s", saved.ID, p.ID)
		s.libraryChanged(saved.ID)
	}
	writeJSON(w, 201, map[string]any{"piece": saved, "duplicate": dup})
}

// preview plays an edited performance from a point, without saving it:
// from tick `from`, after a second for the pedals to take their places.
// Software voicing applies, as it does to every piece.
func (s *Server) preview(w http.ResponseWriter, r *http.Request) {
	p, d, req, ok := s.editedDoc(w, r)
	if !ok {
		return
	}
	if req.From < 0 || req.From > int64(d.End) {
		fail(w, 400, "from is outside the piece")
		return
	}
	// A recording made here plays with the corrections, unless they are
	// already in the notes.
	corrected := false
	if c := s.corrections(); c.Active() && p.NeedsCorrection() && !req.Corrected {
		corrected = c.Apply(&d) > 0
	}
	part := d.From(uint32(req.From), PreviewLead)
	if req.To > req.From && req.To <= int64(d.End) {
		// Only the range from..to, then a second to ring off.
		cut := uint32(req.To)
		if req.From > 0 {
			cut = uint32(req.To-req.From) + PreviewLead
		}
		part = part.Until(cut, se.TicksPerSecond)
	}
	perf, err := part.Performance()
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	dir, err := s.playDir()
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	if err := perf.Save(filepath.Join(dir, "e")); err != nil {
		fail(w, 500, err.Error())
		return
	}
	job := deck.Job{Piece: deck.PieceRef{ID: p.ID, Title: p.Title, Performer: PreviewPerformer},
		Dir: dir, Name: "e", Duration: perf.Duration(), Corrected: corrected}
	if err := s.voice(&job, true); err != nil {
		fail(w, 500, err.Error())
		return
	}
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
