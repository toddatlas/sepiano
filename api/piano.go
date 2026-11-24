package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"sepiano/lut"
	"sepiano/piano"
)

// The piano's stored settings, read only for now (docs/SETTINGS-EDITOR.md):
// backups of its EEPROM table taken with getlut.exe, a view of any backup,
// and the differences between two.

// backupLimit is how long a backup may take: getlut reads 2 KB in a few
// seconds; the upload and connection add a few more.
const backupLimit = 90 * time.Second

func (s *Server) pianoRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/piano/table", s.pianoTable)
	m.HandleFunc("GET /api/v1/piano/backups", s.pianoBackups)
	m.HandleFunc("POST /api/v1/piano/backups", s.pianoBackup)
	m.HandleFunc("GET /api/v1/piano/backups/{id}", s.pianoDownload)
	m.HandleFunc("GET /api/v1/piano/compare", s.pianoCompare)
}

// pianoTable returns the view of a backup: ?backup=ID, or the latest.
func (s *Server) pianoTable(w http.ResponseWriter, r *http.Request) {
	if s.Piano == nil {
		fail(w, 503, "no settings store configured")
		return
	}
	var img []byte
	var b piano.Backup
	var err error
	if id := r.URL.Query().Get("backup"); id != "" {
		img, b, err = s.Piano.Get(id)
	} else {
		img, b, err = s.Piano.Latest()
	}
	if errors.Is(err, piano.ErrNotFound) {
		fail(w, 404, "no backup of the piano's settings yet")
		return
	}
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	t, err := lut.Parse(img)
	if err != nil {
		fail(w, 422, "this backup is not a valid table: "+err.Error())
		return
	}
	v := piano.NewView(t)
	v.Backup = &b
	writeJSON(w, 200, v)
}

func (s *Server) pianoBackups(w http.ResponseWriter, r *http.Request) {
	if s.Piano == nil {
		fail(w, 503, "no settings store configured")
		return
	}
	list, err := s.Piano.List()
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"backups": list, "retriever": s.Programs.Info("getlut")})
}

// pianoBackup reads the table from the piano now, with getlut.exe, and
// keeps it. It answers with the backup and how it differs from the one
// before.
func (s *Server) pianoBackup(w http.ResponseWriter, r *http.Request) {
	if s.Piano == nil {
		fail(w, 503, "no settings store configured")
		return
	}
	if !s.backupMu.TryLock() {
		fail(w, 409, "a backup is already being taken")
		return
	}
	defer s.backupMu.Unlock()
	prog, err := s.Programs.Load("getlut")
	if err != nil {
		fail(w, 503, err.Error())
		return
	}
	work := filepath.Join(s.Piano.Dir, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		fail(w, 500, err.Error())
		return
	}
	path := filepath.Join(work, "table.lut")
	os.Remove(path)
	ctx, cancel := context.WithTimeout(r.Context(), backupLimit+10*time.Second)
	defer cancel()
	res := s.Deck.RunUtility(ctx, prog, path, backupLimit)
	if res.Err != nil {
		deckError(w, fmt.Errorf("could not read the piano's settings: %w", res.Err))
		return
	}
	img, err := os.ReadFile(path)
	if err != nil {
		fail(w, 502, "the piano sent no table: "+err.Error())
		return
	}
	prevImg, prev, prevErr := s.Piano.Latest()
	b, err := s.Piano.Save(img, "manual", time.Now())
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	os.Remove(path)
	s.logf("piano settings backed up: %s (%s, %d bytes)", b.ID, b.Model, b.Size)
	reply := map[string]any{"backup": b, "console": res.Console}
	if prevErr == nil {
		reply["previous"] = prev
		reply["same_as_previous"] = string(prevImg) == string(img)
		if ta, err1 := lut.Parse(prevImg); err1 == nil {
			if tb, err2 := lut.Parse(img); err2 == nil {
				reply["changes"] = piano.Compare(ta, tb)
			}
		}
	}
	code := 201
	if !b.Valid {
		reply["problem"] = b.Problem
	}
	writeJSON(w, code, reply)
}

// pianoDownload sends a backup's .lut file.
func (s *Server) pianoDownload(w http.ResponseWriter, r *http.Request) {
	if s.Piano == nil {
		fail(w, 503, "no settings store configured")
		return
	}
	img, b, err := s.Piano.Get(r.PathValue("id"))
	if errors.Is(err, piano.ErrNotFound) {
		fail(w, 404, "no such backup")
		return
	}
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="piano-%s.lut"`, b.ID))
	w.Write(img)
}

// pianoCompare describes how backup ?to differs from backup ?from.
func (s *Server) pianoCompare(w http.ResponseWriter, r *http.Request) {
	if s.Piano == nil {
		fail(w, 503, "no settings store configured")
		return
	}
	q := r.URL.Query()
	load := func(id string) (*lut.Table, piano.Backup, bool) {
		img, b, err := s.Piano.Get(id)
		if err != nil {
			fail(w, 404, "no such backup: "+id)
			return nil, b, false
		}
		t, err := lut.Parse(img)
		if err != nil {
			fail(w, 422, id+" is not a valid table")
			return nil, b, false
		}
		return t, b, true
	}
	ta, ba, ok := load(q.Get("from"))
	if !ok {
		return
	}
	tb, bb, ok := load(q.Get("to"))
	if !ok {
		return
	}
	writeJSON(w, 200, map[string]any{"from": ba, "to": bb, "diff": piano.Compare(ta, tb)})
}
