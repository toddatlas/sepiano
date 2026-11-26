package api

import (
	"net/http"
	"sort"

	"sepiano/deck"
	"sepiano/library"
	"sepiano/piano"
	"sepiano/se"
)

// Each collection's fit to this piano (docs/LIBRARIES.md): loudness and
// pedal depth, set by ear, applied to its pieces as they play.

func (s *Server) fitRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/fits", s.getFits)
	m.HandleFunc("PUT /api/v1/fits", s.putFits)
}

func (s *Server) fits() piano.Fits {
	if s.Fits == nil {
		return piano.Fits{}
	}
	f, err := s.Fits.Load()
	if err != nil {
		s.logf("fits: %v", err)
	}
	return f
}

// collectionInfo is a collection in the library, for the fits screen.
type collectionInfo struct {
	Name       string    `json:"name"`
	Pieces     int       `json:"pieces"`
	CapturedOn string    `json:"captured_on,omitempty"`
	Licence    string    `json:"licence,omitempty"`
	Sample     string    `json:"sample,omitempty"` // a piece to try the fit on
	Fit        piano.Fit `json:"fit"`
}

func (s *Server) getFits(w http.ResponseWriter, r *http.Request) {
	fs := s.fits()
	byName := map[string]*collectionInfo{}
	for _, p := range s.Lib.List(library.Filter{Hidden: "all"}) {
		if p.Collection == "" {
			continue
		}
		c := byName[p.Collection]
		if c == nil {
			c = &collectionInfo{Name: p.Collection, CapturedOn: p.CapturedOn, Licence: p.Licence, Sample: p.ID}
			byName[p.Collection] = c
		}
		c.Pieces++
	}
	out := []collectionInfo{}
	for _, c := range byName {
		c.Fit = fs.Collections[c.Name]
		if c.Fit.Damper == 0 {
			c.Fit.Damper = 100
		}
		if c.Fit.Soft == 0 {
			c.Fit.Soft = 100
		}
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, 200, map[string]any{"on": fs.On, "collections": out,
		"limits": map[string]int{"min_speed": piano.MinFitSpeed, "max_speed": piano.MaxFitSpeed, "min_pedal": piano.MinFitPedal, "max_pedal": piano.MaxFitPedal}})
}

func (s *Server) putFits(w http.ResponseWriter, r *http.Request) {
	if s.Fits == nil {
		fail(w, 503, "no fits store")
		return
	}
	var req piano.Fits
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, err.Error())
		return
	}
	saved, err := s.Fits.Save(req)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, saved)
}

// playFix is what playback does to each piece before the piano gets it:
// the recording corrections for a recording made here, then its
// collection's fit. Nil when nothing applies.
func (s *Server) playFix() library.Fix {
	corr := s.correctFix()
	fs := s.fits()
	if corr == nil && !fs.Active() {
		return nil
	}
	return func(p library.Piece, perf *se.Performance) (*se.Performance, bool, error) {
		changed := false
		if corr != nil {
			out, ch, err := corr(p, perf)
			if err != nil {
				return perf, false, err
			}
			perf, changed = out, ch
		}
		if f, ok := fs.For(p.Collection); ok && f.Apply(perf) > 0 {
			changed = true
		}
		return perf, changed, nil
	}
}

// markFixes sets a job's Corrected and Fitted from the pieces it plays.
func (s *Server) markFixes(job *deck.Job, pieces []library.Piece) {
	c, fs := s.corrections(), s.fits()
	for _, p := range pieces {
		if c.Active() && p.NeedsCorrection() {
			job.Corrected = true
		}
		if _, ok := fs.For(p.Collection); ok {
			job.Fitted = true
		}
	}
}
