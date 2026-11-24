// Package api is the HTTP interface of sepianod: the REST API and event
// stream described in openapi.yaml, and the web UI.
package api

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"sepiano/deck"
	"sepiano/library"
	"sepiano/piano"
	"sepiano/sim"
)

// OpenAPI is the API description, served at /api/v1/openapi.yaml.
//
//go:embed openapi.yaml
var OpenAPI []byte

func init() {
	// Not in Go's built-in table, and the Pi's /etc/mime.types may lack it.
	mime.AddExtensionType(".webmanifest", "application/manifest+json")
	mime.AddExtensionType(".js", "text/javascript; charset=utf-8")
}

// Server serves the API and the UI.
type Server struct {
	Deck     *deck.Deck
	Lib      *library.Library
	Programs *Programs
	Sim      *sim.Instrument // set only when simulating
	UI       fs.FS
	Version  string
	Log      func(format string, args ...any)

	// PlayDir holds the joined performances of albums and play lists.
	PlayDir string

	// Piano keeps the backups of the piano's stored settings.
	Piano   *piano.Store
	Voicing *piano.VoicingStore
	// Corrections keeps the per-key corrections for recordings.
	Corrections *piano.CorrectionsStore
	backupMu    sync.Mutex

	qmu   sync.Mutex
	queue queue
}

func (s *Server) logf(format string, args ...any) {
	if s.Log != nil {
		s.Log(format, args...)
	}
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /api/v1/status", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, s.Deck.Status()) })
	m.HandleFunc("GET /api/v1/events", s.events)
	m.HandleFunc("POST /api/v1/deck/play", s.play)
	m.HandleFunc("POST /api/v1/deck/next", s.skip(true))
	m.HandleFunc("POST /api/v1/deck/previous", s.skip(false))
	m.HandleFunc("GET /api/v1/albums", s.albums)
	m.HandleFunc("GET /api/v1/albums/{name}", s.album)
	m.HandleFunc("GET /api/v1/playlists", s.playlists)
	m.HandleFunc("POST /api/v1/playlists", s.createPlaylist)
	m.HandleFunc("GET /api/v1/playlists/{id}", s.getPlaylist)
	m.HandleFunc("PUT /api/v1/playlists/{id}", s.updatePlaylist)
	m.HandleFunc("DELETE /api/v1/playlists/{id}", s.deletePlaylist)
	m.HandleFunc("POST /api/v1/deck/pause", s.deckCmd(s.Deck.Pause))
	m.HandleFunc("POST /api/v1/deck/resume", s.deckCmd(s.Deck.Resume))
	m.HandleFunc("POST /api/v1/deck/stop", s.deckCmd(s.Deck.Stop))
	m.HandleFunc("POST /api/v1/deck/restart", s.deckCmd(s.Deck.Restart))
	m.HandleFunc("POST /api/v1/deck/level", s.level)
	m.HandleFunc("POST /api/v1/record/arm", s.arm)
	m.HandleFunc("POST /api/v1/record/start", s.deckCmd(s.Deck.StartRecording))
	m.HandleFunc("POST /api/v1/record/stop", s.deckCmd(s.Deck.StopRecording))
	m.HandleFunc("POST /api/v1/record/discard", s.deckCmd(s.Deck.Discard))
	m.HandleFunc("GET /api/v1/pieces", s.listPieces)
	m.HandleFunc("POST /api/v1/pieces", s.upload)
	m.HandleFunc("GET /api/v1/pieces/{id}", s.getPiece)
	m.HandleFunc("PATCH /api/v1/pieces/{id}", s.patchPiece)
	m.HandleFunc("DELETE /api/v1/pieces/{id}", s.deletePiece)
	m.HandleFunc("GET /api/v1/pieces/{id}/download", s.download)
	m.HandleFunc("GET /api/v1/pieces/{id}/keys", s.pieceKeys)
	m.HandleFunc("GET /api/v1/pieces/{id}/performance", s.getPerformance)
	m.HandleFunc("POST /api/v1/pieces/{id}/edits", s.saveEdit)
	m.HandleFunc("POST /api/v1/pieces/{id}/preview", s.preview)
	m.HandleFunc("GET /api/v1/export", s.export)
	m.HandleFunc("POST /api/v1/library/rescan", s.rescan)
	m.HandleFunc("GET /api/v1/instrument", s.instrument)
	m.HandleFunc("PUT /api/v1/programs/{name}", s.installProgram)
	m.HandleFunc("GET /api/v1/settings", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, s.Lib.Settings()) })
	m.HandleFunc("PUT /api/v1/settings", s.putSettings)
	m.HandleFunc("POST /api/v1/sim/power", s.simPower)
	m.HandleFunc("GET /api/v1/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.Write(OpenAPI)
	})
	s.pianoRoutes(m)
	s.voicingRoutes(m)
	m.HandleFunc("GET /api/v1/corrections", s.getCorrections)
	m.HandleFunc("PUT /api/v1/corrections", s.putCorrections)
	m.HandleFunc("POST /api/v1/corrections/apply", s.applyCorrections)
	m.HandleFunc("POST /api/v1/pieces/{id}/corrected", s.correctedCopy)
	s.toolRoutes(m)
	m.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { fail(w, 404, "no such API endpoint") })
	m.HandleFunc("GET /apple-touch-icon.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "max-age=86400")
		w.Write(touchIcon())
	})
	if s.UI != nil {
		files := http.FileServer(http.FS(s.UI))
		m.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.Header().Set("Allow", "GET, HEAD")
				http.Error(w, "method not allowed", 405)
				return
			}
			// Always revalidate: the service worker provides offline start-up,
			// and a new deploy must show up at once.
			w.Header().Set("Cache-Control", "no-cache")
			files.ServeHTTP(w, r)
		}))
	}
	return s.logged(m)
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(c int)           { w.code = c; w.ResponseWriter.WriteHeader(c) }
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (s *Server) logged(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w, code: 200}
		t := time.Now()
		h.ServeHTTP(sw, r)
		if r.Method != http.MethodGet || sw.code >= 500 {
			s.logf("http: %s %s %d %v", r.Method, r.URL.Path, sw.code, time.Since(t).Round(time.Millisecond))
		}
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func readJSON(r *http.Request, v any) error {
	body := http.MaxBytesReader(nil, r.Body, 1<<20)
	if err := json.NewDecoder(body).Decode(v); err != nil && err != io.EOF {
		return fmt.Errorf("bad request body: %v", err)
	}
	return nil
}

// deckError maps deck errors to HTTP replies.
func deckError(w http.ResponseWriter, err error) {
	code := 500
	switch {
	case errors.Is(err, deck.ErrState), errors.Is(err, deck.ErrBusy):
		code = 409
	case errors.Is(err, deck.ErrOffline), errors.Is(err, deck.ErrNoProgram):
		code = 503
	case errors.Is(err, deck.ErrNoConfirm), errors.Is(err, context.DeadlineExceeded):
		code = 504
	}
	fail(w, code, err.Error())
}

func cmdContext(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), 15*time.Second)
}

func (s *Server) deckCmd(fn func(context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := cmdContext(r)
		defer cancel()
		if err := fn(ctx); err != nil {
			deckError(w, err)
			return
		}
		writeJSON(w, 200, s.Deck.Status())
	}
}

func (s *Server) level(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Delta *int `json:"delta"`
		Level *int `json:"level"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, err.Error())
		return
	}
	ctx, cancel := cmdContext(r)
	defer cancel()
	var err error
	switch {
	case req.Level != nil:
		if *req.Level > 0 {
			fail(w, 400, "the level cannot be above 0 (as recorded)")
			return
		}
		err = s.Deck.SetLevel(ctx, *req.Level)
	case req.Delta != nil:
		err = s.Deck.ChangeLevel(ctx, *req.Delta)
	default:
		fail(w, 400, "give delta or level")
		return
	}
	if err != nil {
		deckError(w, err)
		return
	}
	writeJSON(w, 200, s.Deck.Status())
}

func (s *Server) arm(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, err.Error())
		return
	}
	ctx, cancel := cmdContext(r)
	defer cancel()
	if err := s.Deck.Arm(ctx, strings.TrimSpace(req.Title)); err != nil {
		deckError(w, err)
		return
	}
	writeJSON(w, 202, s.Deck.Status())
}

func (s *Server) libraryChanged(id string) {
	s.Deck.Publish(deck.Event{Type: "library", Data: map[string]string{"id": id}})
}

func (s *Server) listPieces(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ps := s.Lib.List(library.Filter{Q: q.Get("q"), Composer: q.Get("composer"), Performer: q.Get("performer"), Instrument: q.Get("instrument")})
	writeJSON(w, 200, map[string]any{"pieces": ps})
}

const maxUpload = 512 << 20

func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	mr, err := r.MultipartReader()
	if err != nil {
		fail(w, 400, "expected multipart/form-data with file parts")
		return
	}
	var files []library.File
	var title string
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			fail(w, 400, "upload interrupted: "+err.Error())
			return
		}
		switch part.FormName() {
		case "file", "files", "file[]":
			b, err := io.ReadAll(part)
			if err != nil {
				fail(w, 400, "upload interrupted: "+err.Error())
				return
			}
			files = append(files, library.File{Name: part.FileName(), Data: b})
		case "title":
			b, _ := io.ReadAll(io.LimitReader(part, 1000))
			title = string(b)
		}
		part.Close()
	}
	if len(files) == 0 {
		fail(w, 400, "no files in the upload")
		return
	}
	res := s.Lib.AddFiles(files, library.OriginUpload, title)
	if res.Added == nil {
		res.Added = []library.Piece{}
	}
	if res.Duplicates == nil {
		res.Duplicates = []library.Piece{}
	}
	if res.Errors == nil {
		res.Errors = []library.ItemError{}
	}
	if len(res.Added) > 0 {
		id := ""
		if len(res.Added) == 1 {
			id = res.Added[0].ID
		}
		s.libraryChanged(id)
	}
	writeJSON(w, 200, res)
}

func (s *Server) getPiece(w http.ResponseWriter, r *http.Request) {
	p, err := s.Lib.Get(r.PathValue("id"))
	if err != nil {
		fail(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, p)
}

func (s *Server) patchPiece(w http.ResponseWriter, r *http.Request) {
	var patch library.Patch
	if err := readJSON(r, &patch); err != nil {
		fail(w, 400, err.Error())
		return
	}
	p, err := s.Lib.Update(r.PathValue("id"), patch)
	if errors.Is(err, library.ErrNotFound) {
		fail(w, 404, err.Error())
		return
	} else if err != nil {
		fail(w, 400, err.Error())
		return
	}
	s.libraryChanged(p.ID)
	writeJSON(w, 200, p)
}

func (s *Server) deletePiece(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if st := s.Deck.Status(); st.Piece != nil && st.Piece.ID == id {
		fail(w, 409, "this piece is playing; stop it first")
		return
	}
	if err := s.Lib.Delete(id); errors.Is(err, library.ErrNotFound) {
		fail(w, 404, err.Error())
		return
	} else if err != nil {
		fail(w, 500, err.Error())
		return
	}
	s.libraryChanged(id)
	w.WriteHeader(204)
}

func attachment(w http.ResponseWriter, contentType, name string) {
	w.Header().Set("Content-Type", contentType)
	ascii := strings.Map(func(r rune) rune {
		if r < 32 || r > 126 || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, name)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": ascii})+
		"; filename*=UTF-8''"+url.PathEscape(name))
}

func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.Lib.Get(id); err != nil {
		fail(w, 404, err.Error())
		return
	}
	name := s.Lib.DownloadName(id)
	switch f := r.URL.Query().Get("format"); f {
	case "", "se":
		attachment(w, "application/zip", name+".zip")
		if err := s.Lib.WriteSet(w, id); err != nil {
			s.logf("download %s: %v", id, err)
		}
	case "midi", "mid":
		b, err := s.Lib.MIDI(id)
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		attachment(w, "audio/midi", name+".mid")
		w.Write(b)
	default:
		fail(w, 400, "format must be se or midi")
	}
}

func (s *Server) export(w http.ResponseWriter, r *http.Request) {
	attachment(w, "application/zip", "sepiano-library-"+time.Now().Format("2006-01-02")+".zip")
	if err := s.Lib.Export(w); err != nil {
		s.logf("export: %v", err)
	}
}

func (s *Server) rescan(w http.ResponseWriter, r *http.Request) {
	n, err := s.Lib.Rescan()
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	s.libraryChanged("")
	writeJSON(w, 200, map[string]int{"pieces": n})
}

func (s *Server) instrument(w http.ResponseWriter, r *http.Request) {
	info := s.Deck.Info()
	type linkStats struct {
		RxBytes        int64      `json:"rx_bytes"`
		TxBytes        int64      `json:"tx_bytes"`
		Retransmits    int        `json:"retransmits"`
		Naks           int        `json:"naks"`
		ConnectedSince *time.Time `json:"connected_since"`
		LastError      string     `json:"last_error"`
	}
	console := info.Console
	if console == nil {
		console = []string{}
	}
	writeJSON(w, 200, map[string]any{
		"port":      info.Port,
		"baud":      info.Baud,
		"online":    info.Online,
		"simulated": info.Simulated,
		"link": linkStats{info.RxBytes, info.TxBytes, info.Retransmits, info.Naks,
			info.ConnectedSince, info.LastError},
		"programs": map[string]ProgramInfo{"play": s.Programs.Info("play"), "record": s.Programs.Info("record"), "getlut": s.Programs.Info("getlut")},
		"console":  console,
		"version":  s.Version,
	})
}

func (s *Server) installProgram(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if _, ok := ProgramFiles[name]; !ok {
		fail(w, 404, "no such program: "+name)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<20)
	var data []byte
	var err error
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt == "multipart/form-data" {
		f, _, ferr := r.FormFile("file")
		if ferr != nil {
			fail(w, 400, "expected a file part")
			return
		}
		data, err = io.ReadAll(f)
		f.Close()
	} else {
		data, err = io.ReadAll(r.Body)
	}
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	info, err := s.Programs.Install(name, data)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	s.logf("installed %s: %s", ProgramFiles[name], info.Banner)
	writeJSON(w, 200, info)
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PreferredInstrument *string `json:"preferred_instrument"`
		Level               *int    `json:"level"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, err.Error())
		return
	}
	set := s.Lib.Settings()
	if req.PreferredInstrument != nil {
		set.PreferredInstrument = strings.TrimSpace(*req.PreferredInstrument)
	}
	if req.Level != nil {
		set.Level = *req.Level
	}
	if err := s.Lib.SaveSettings(set); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if req.Level != nil {
		ctx, cancel := cmdContext(r)
		defer cancel()
		if err := s.Deck.SetLevel(ctx, *req.Level); err != nil {
			deckError(w, err)
			return
		}
	}
	writeJSON(w, 200, s.Lib.Settings())
}

func (s *Server) simPower(w http.ResponseWriter, r *http.Request) {
	if s.Sim == nil {
		fail(w, 404, "not running the simulator")
		return
	}
	var req struct {
		On *bool `json:"on"`
	}
	if err := readJSON(r, &req); err != nil || req.On == nil {
		fail(w, 400, `give {"on": true|false}`)
		return
	}
	s.Sim.SetPower(*req.On)
	writeJSON(w, 200, map[string]bool{"on": *req.On})
}

// events streams deck events as Server-Sent Events.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	rc.Flush()
	ch, unsubscribe := s.Deck.Subscribe()
	defer unsubscribe()
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
		case e, ok := <-ch:
			if !ok {
				return
			}
			b, err := json.Marshal(e.Data)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, b); err != nil {
				return
			}
		}
		if err := rc.Flush(); err != nil {
			return
		}
	}
}
