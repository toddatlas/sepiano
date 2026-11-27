package api

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"sepiano/backup"
	"sepiano/library"
	"sepiano/lut"
	"sepiano/piano"
)

// Backups (docs/BACKUPS.md): GET /backup downloads everything on the Pi as
// one zip; POST /backup/restore adds back what a backup has and the Pi
// lacks, never changing or removing anything here; GET /backup/status
// says when the last full backup was taken.

// backupState is kept in backup-state.json in the data folder.
type backupState struct {
	Last   time.Time `json:"last"`
	By     string    `json:"by"` // "app" (a browser) or "pc" (the scheduled backup)
	Bytes  int64     `json:"bytes"`
	Pieces int       `json:"pieces"`
}

func (s *Server) backupStatePath() string { return filepath.Join(s.DataDir, "backup-state.json") }

func (s *Server) backupRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/backup", s.backupDownload)
	m.HandleFunc("GET /api/v1/backup/status", s.backupStatus)
	m.HandleFunc("POST /api/v1/backup/restore", s.backupRestore)
}

func (s *Server) backupDownload(w http.ResponseWriter, r *http.Request) {
	if s.DataDir == "" {
		fail(w, 503, "no data folder")
		return
	}
	now := time.Now()
	name := "sepiano-backup-" + now.Format("20060102-1504") + ".zip"
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	m, err := backup.Write(w, s.DataDir, s.Version, now)
	if err != nil {
		s.logf("backup: %v", err) // the download is cut short; the browser shows it failed
		return
	}
	by := "app"
	if r.URL.Query().Get("by") == "pc" {
		by = "pc"
	}
	st := backupState{Last: m.Created, By: by, Bytes: m.Bytes, Pieces: m.Pieces}
	if b, err := json.MarshalIndent(st, "", "  "); err == nil {
		tmp := s.backupStatePath() + ".tmp"
		if os.WriteFile(tmp, b, 0o644) == nil {
			os.Rename(tmp, s.backupStatePath())
		}
	}
	s.logf("backup: %d files, %d pieces, %d bytes, by %s", len(m.Files), m.Pieces, m.Bytes, by)
}

func (s *Server) backupStatus(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"last": nil, "by": "", "pieces": 0, "bytes": 0, "data_bytes": dirBytes(s.DataDir)}
	if b, err := os.ReadFile(s.backupStatePath()); err == nil {
		var st backupState
		if json.Unmarshal(b, &st) == nil && !st.Last.IsZero() {
			out["last"], out["by"], out["pieces"], out["bytes"] = st.Last, st.By, st.Pieces, st.Bytes
			out["age_days"] = int(time.Since(st.Last).Hours() / 24)
		}
	}
	writeJSON(w, 200, out)
}

// dirBytes is the size of what a backup would hold.
func dirBytes(dir string) int64 {
	var n int64
	if dir == "" {
		return 0
	}
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		if rel != "." && backup.Skip(filepath.ToSlash(rel), d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info, err := d.Info(); err == nil && d.Type().IsRegular() {
			n += info.Size()
		}
		return nil
	})
	return n
}

// restoreReport says what a restore did.
type restoreReport struct {
	Created   time.Time `json:"created"` // when the backup was made
	Pieces    int       `json:"pieces"`  // pieces added
	Playlists int       `json:"playlists"`
	Backups   int       `json:"backups"`  // the piano's settings backups added
	Programs  int       `json:"programs"` // programs installed
	Restored  []string  `json:"restored"` // settings files put back (none were here)
	Kept      int       `json:"kept"`     // already here, left as they are
	Skipped   []string  `json:"skipped"`  // not restored, and why
	Errors    []string  `json:"errors"`
}

func (s *Server) backupRestore(w http.ResponseWriter, r *http.Request) {
	if s.DataDir == "" {
		fail(w, 503, "no data folder")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	var src io.Reader = r.Body
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt == "multipart/form-data" {
		f, _, err := r.FormFile("file")
		if err != nil {
			fail(w, 400, "expected a file part")
			return
		}
		defer f.Close()
		src = f
	}
	tmp, err := os.CreateTemp(s.DataDir, ".restore-*.zip") // hidden: never itself backed up
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	size, err := io.Copy(tmp, src)
	if err != nil {
		fail(w, 400, "upload interrupted: "+err.Error())
		return
	}
	a, err := backup.Open(tmp, size)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	rep := s.restore(a)
	s.logf("restore from %s: %d pieces, %d play lists, %d settings backups, %d programs, %d kept, %d errors",
		rep.Created.Format(time.RFC3339), rep.Pieces, rep.Playlists, rep.Backups, rep.Programs, rep.Kept, len(rep.Errors))
	if rep.Pieces > 0 || rep.Playlists > 0 {
		s.libraryChanged("")
	}
	writeJSON(w, 200, rep)
}

// restore adds back from a what is missing here. Nothing here is changed
// or removed: a piece already here (the same performance), a settings
// backup or program already here, and settings files that exist are kept.
func (s *Server) restore(a *backup.Archive) restoreReport {
	rep := restoreReport{Created: a.Manifest.Created, Restored: []string{}, Skipped: []string{}, Errors: []string{}}
	errf := func(format string, args ...any) { rep.Errors = append(rep.Errors, fmt.Sprintf(format, args...)) }
	pieces := map[string]map[string]string{} // id -> extension -> path in the backup
	var ids []string
	for _, p := range a.Paths() {
		dir, base := path.Split(p)
		switch dir {
		case "library/":
			ext := path.Ext(base)
			id := strings.TrimSuffix(base, ext)
			if pieces[id] == nil {
				pieces[id] = map[string]string{}
				ids = append(ids, id)
			}
			pieces[id][strings.ToLower(ext)] = p
		case "eeprom/":
			s.restoreSettingsBackup(a, p, base, &rep)
		case "programs/":
			s.restoreProgram(a, p, base, &rep)
		case "":
			s.restoreFile(a, p, &rep)
		default:
			rep.Skipped = append(rep.Skipped, p+" (not restored: "+strings.TrimSuffix(dir, "/")+")")
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		files := map[string][]byte{}
		for ext, p := range pieces[id] {
			b, err := a.Read(p)
			if err != nil {
				errf("%v", err)
				continue
			}
			files[ext] = b
		}
		if files[".kb"] == nil {
			continue // a stray file without its performance
		}
		_, added, err := s.Lib.RestorePiece(id, files)
		switch {
		case err != nil:
			errf("piece %s: %v", id, err)
		case added:
			rep.Pieces++
		default:
			rep.Kept++
		}
	}
	return rep
}

func (s *Server) restoreSettingsBackup(a *backup.Archive, p, base string, rep *restoreReport) {
	id, ok := strings.CutSuffix(base, ".lut")
	if !ok || s.Piano == nil {
		rep.Skipped = append(rep.Skipped, p)
		return
	}
	if _, err := os.Stat(s.Piano.Path(id)); err == nil {
		rep.Kept++
		return
	}
	b, err := a.Read(p)
	if err != nil {
		rep.Errors = append(rep.Errors, err.Error())
		return
	}
	if len(b) != lut.Size {
		rep.Skipped = append(rep.Skipped, p+" (not a whole table)")
		return
	}
	if err := os.MkdirAll(s.Piano.Dir, 0o755); err == nil {
		err = os.WriteFile(s.Piano.Path(id), b, 0o644)
		if err == nil {
			rep.Backups++
			return
		}
		rep.Errors = append(rep.Errors, p+": "+err.Error())
	}
}

func (s *Server) restoreProgram(a *backup.Archive, p, base string, rep *restoreReport) {
	name := strings.TrimSuffix(strings.ToLower(base), ".exe")
	if _, known := ProgramFiles[name]; !known {
		rep.Skipped = append(rep.Skipped, p+" (not a suite program)")
		return
	}
	if info := s.Programs.Info(name); info.Present && info.Error == "" {
		rep.Kept++
		return
	}
	b, err := a.Read(p)
	if err != nil {
		rep.Errors = append(rep.Errors, err.Error())
		return
	}
	if _, err := s.Programs.Install(name, b); err != nil {
		rep.Errors = append(rep.Errors, p+": "+err.Error())
		return
	}
	rep.Programs++
}

// restoreFile puts back a settings file at the top of the data folder, only
// if this Pi has none: a restore never replaces settings already here.
func (s *Server) restoreFile(a *backup.Archive, p string, rep *restoreReport) {
	put := func(exists bool, apply func([]byte) error) {
		if exists {
			rep.Kept++
			return
		}
		b, err := a.Read(p)
		if err == nil {
			err = apply(b)
		}
		if err != nil {
			rep.Errors = append(rep.Errors, p+": "+err.Error())
			return
		}
		rep.Restored = append(rep.Restored, p)
	}
	exists := func(f string) bool { _, err := os.Stat(f); return err == nil }
	switch p {
	case "playlists.json":
		b, err := a.Read(p)
		var ps []library.Playlist
		if err == nil {
			err = json.Unmarshal(b, &ps)
		}
		if err != nil {
			rep.Errors = append(rep.Errors, p+": "+err.Error())
			return
		}
		n, err := s.Lib.MergePlaylists(ps)
		if err != nil {
			rep.Errors = append(rep.Errors, p+": "+err.Error())
		}
		rep.Playlists += n
		rep.Kept += len(ps) - n
	case "settings.json":
		put(s.Lib.SettingsSaved(), func(b []byte) error {
			var set library.Settings
			if err := json.Unmarshal(b, &set); err != nil {
				return err
			}
			return s.Lib.SaveSettings(set)
		})
	case "voicing.json":
		if s.Voicing == nil {
			return
		}
		put(exists(s.Voicing.Path), func(b []byte) error {
			var v piano.Voicing
			if err := json.Unmarshal(b, &v); err != nil {
				return err
			}
			_, err := s.Voicing.Save(v)
			return err
		})
	case "corrections.json":
		if s.Corrections == nil {
			return
		}
		put(exists(s.Corrections.Path), func(b []byte) error {
			var c piano.Corrections
			if err := json.Unmarshal(b, &c); err != nil {
				return err
			}
			_, err := s.Corrections.Save(c, c.Updated)
			return err
		})
	case "fits.json":
		if s.Fits == nil {
			return
		}
		put(exists(s.Fits.Path), func(b []byte) error {
			var fs piano.Fits
			if err := json.Unmarshal(b, &fs); err != nil {
				return err
			}
			_, err := s.Fits.Save(fs)
			return err
		})
	case "favorites.json":
		// Starred albums and play lists: added to any starred here.
		b, err := a.Read(p)
		var f library.Favorites
		if err == nil {
			err = json.Unmarshal(b, &f)
		}
		var n int
		if err == nil {
			n, err = s.Lib.MergeFavorites(f)
		}
		if err != nil {
			rep.Errors = append(rep.Errors, p+": "+err.Error())
			return
		}
		if n > 0 {
			rep.Restored = append(rep.Restored, p)
		} else {
			rep.Kept++
		}
	case "history.json":
		// The play history: taken only when this Pi has none.
		b, err := a.Read(p)
		var took bool
		if err == nil {
			took, err = s.hist().restore(b)
		}
		switch {
		case err != nil:
			rep.Errors = append(rep.Errors, p+": "+err.Error())
		case took:
			rep.Restored = append(rep.Restored, p)
		default:
			rep.Kept++
		}
	case "backup-state.json":
		// The backup's own record of backups: this Pi keeps its own.
	default:
		rep.Skipped = append(rep.Skipped, p+" (not a file sepianod restores)")
	}
}
