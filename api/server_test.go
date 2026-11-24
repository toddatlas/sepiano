package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"sepiano/deck"
	"sepiano/host"
	"sepiano/library"
	"sepiano/piano"
	"sepiano/se"
	"sepiano/sim"
)

type env struct {
	t   *testing.T
	srv *httptest.Server
	lib *library.Library
	in  *sim.Instrument
}

func newEnv(t *testing.T) *env {
	data := t.TempDir()
	lib, err := library.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	in := sim.New()
	in.Baud, in.Speed = 1_000_000, 10
	progs := &Programs{Dir: t.TempDir(), Fallback: map[string]*host.Program{
		"play": sim.PlayerProgram(), "record": sim.RecorderProgram(), "getlut": sim.RetrieverProgram()}}
	for name, prog := range SimulatedTools() {
		progs.Fallback[name] = prog
	}
	d := deck.New(deck.Config{
		Connect: func(ctx context.Context, o host.ConnectOptions) (*host.Conn, error) {
			p, err := in.Dial()
			if err != nil {
				return nil, err
			}
			return host.ConnectOn(ctx, p, in.Baud, o)
		},
		Simulated:  true,
		Player:     func() (*host.Program, error) { return progs.Load("play") },
		Recorder:   func() (*host.Program, error) { return progs.Load("record") },
		StagingDir: t.TempDir(),
		OnRecorded: func(dir, name, title string) (string, error) {
			p, err := lib.AddRecording(dir, name, title)
			return p.ID, err
		},
		RetryMin: 100 * time.Millisecond, RetryMax: 200 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { d.Run(ctx); close(done) }()
	s := &Server{Deck: d, Lib: lib, Programs: progs, Sim: in, Version: "test", PlayDir: t.TempDir(), Piano: &piano.Store{Dir: t.TempDir()}, Voicing: &piano.VoicingStore{Path: t.TempDir() + "/voicing.json"},
		UI: fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>Piano</title>")}}}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(func() { srv.Close(); cancel(); <-done })
	e := &env{t: t, srv: srv, lib: lib, in: in}
	e.waitState("idle")
	return e
}

func (e *env) do(method, path string, body any) (int, []byte) {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rd)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func (e *env) status() deck.Status {
	_, b := e.do("GET", "/api/v1/status", nil)
	var st deck.Status
	json.Unmarshal(b, &st)
	return st
}

func (e *env) waitState(s string) deck.Status {
	e.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		st := e.status()
		if string(st.State) == s {
			return st
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("state %s, want %s", st.State, s)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (e *env) upload(files map[string][]byte) library.Result {
	e.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for name, data := range files {
		w, _ := mw.CreateFormFile("file", name)
		w.Write(data)
	}
	mw.Close()
	resp, err := http.Post(e.srv.URL+"/api/v1/pieces", mw.FormDataContentType(), &buf)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var r library.Result
	json.NewDecoder(resp.Body).Decode(&r)
	if resp.StatusCode != 200 {
		e.t.Fatalf("upload: %d", resp.StatusCode)
	}
	return r
}

func TestEndToEnd(t *testing.T) {
	e := newEnv(t)
	kb, lp, sp, _ := sim.Phrase(60 * se.TicksPerSecond).Encode()
	name := "03-Simulator20261122-Sepiano_Test-Steinway_SED133291"
	r := e.upload(map[string][]byte{name + ".kb": kb, name + ".lp": lp, name + ".sp": sp})
	if len(r.Added) != 1 {
		t.Fatalf("upload: %+v", r)
	}
	id := r.Added[0].ID

	code, b := e.do("GET", "/api/v1/pieces?q=sepiano", nil)
	if code != 200 || !strings.Contains(string(b), id) {
		t.Fatalf("list: %d %s", code, b)
	}

	// The event stream starts with a status.
	resp, err := http.Get(e.srv.URL + "/api/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Scan()
	if sc.Text() != "event: status" || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("first SSE line %q", sc.Text())
	}
	resp.Body.Close()

	if code, b := e.do("POST", "/api/v1/deck/play", map[string]string{"piece": id}); code != 202 {
		t.Fatalf("play: %d %s", code, b)
	}
	e.waitState("playing")
	if code, _ := e.do("DELETE", "/api/v1/pieces/"+id, nil); code != 409 {
		t.Fatalf("delete while playing: %d", code)
	}
	if code, b := e.do("POST", "/api/v1/deck/level", map[string]int{"delta": -1}); code != 200 || !strings.Contains(string(b), `"level":-1`) {
		t.Fatalf("level: %d %s", code, b)
	}
	if code, _ := e.do("POST", "/api/v1/deck/pause", nil); code != 200 {
		t.Fatalf("pause: %d", code)
	}
	if code, _ := e.do("POST", "/api/v1/deck/resume", nil); code != 200 {
		t.Fatalf("resume: %d", code)
	}
	if code, _ := e.do("POST", "/api/v1/deck/stop", nil); code != 200 {
		t.Fatalf("stop: %d", code)
	}
	e.waitState("idle")
	if code, _ := e.do("POST", "/api/v1/deck/pause", nil); code != 409 {
		t.Fatalf("pause while idle: %d", code)
	}
	if code, _ := e.do("POST", "/api/v1/deck/play", map[string]string{"piece": "nope"}); code != 404 {
		t.Fatalf("play unknown: %d", code)
	}

	code, b = e.do("GET", "/api/v1/pieces/"+id+"/download?format=se", nil)
	if code != 200 || !bytes.HasPrefix(b, []byte("PK")) {
		t.Fatalf("download se: %d", code)
	}
	code, b = e.do("GET", "/api/v1/pieces/"+id+"/download?format=midi", nil)
	if code != 200 || !bytes.HasPrefix(b, []byte("MThd")) {
		t.Fatalf("download midi: %d", code)
	}
	code, b = e.do("PATCH", "/api/v1/pieces/"+id, map[string]string{"title": "Renamed", "notes": "n"})
	if code != 200 || !strings.Contains(string(b), `"title":"Renamed"`) {
		t.Fatalf("patch: %d %s", code, b)
	}
	if code, b := e.do("GET", "/api/v1/export", nil); code != 200 || !bytes.HasPrefix(b, []byte("PK")) {
		t.Fatalf("export: %d", code)
	}
	if code, _ := e.do("DELETE", "/api/v1/pieces/"+id, nil); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	if code, _ := e.do("GET", "/api/v1/pieces/"+id, nil); code != 404 {
		t.Fatalf("get deleted: %d", code)
	}
}

func TestRecordingThroughAPI(t *testing.T) {
	e := newEnv(t)
	if code, b := e.do("POST", "/api/v1/record/arm", map[string]string{"title": "Take one"}); code != 202 {
		t.Fatalf("arm: %d %s", code, b)
	}
	e.waitState("armed")
	if code, _ := e.do("POST", "/api/v1/record/start", nil); code != 200 {
		t.Fatalf("start: %d", code)
	}
	time.Sleep(300 * time.Millisecond)
	if code, _ := e.do("POST", "/api/v1/record/stop", nil); code != 200 {
		t.Fatalf("stop: %d", code)
	}
	e.waitState("idle")
	ps := e.lib.List(library.Filter{})
	if len(ps) != 1 || ps[0].Title != "Take one" || ps[0].Origin != library.OriginRecorded || ps[0].RecordedOn == "" {
		t.Fatalf("library after recording: %+v", ps)
	}
}

func TestInstrumentSettingsProgramsSim(t *testing.T) {
	e := newEnv(t)
	code, b := e.do("GET", "/api/v1/instrument", nil)
	if code != 200 || !strings.Contains(string(b), `"simulated":true`) || !strings.Contains(string(b), "SIMULATED SE/PC PERFORMANCE PLAYER") {
		t.Fatalf("instrument: %d %s", code, b)
	}
	if code, b := e.do("PUT", "/api/v1/settings", map[string]any{"level": -2}); code != 200 || !strings.Contains(string(b), `"level":-2`) {
		t.Fatalf("settings: %d %s", code, b)
	}
	if st := e.status(); st.TargetLevel != -2 {
		t.Fatalf("target level %d after settings", st.TargetLevel)
	}
	req, _ := http.NewRequest("PUT", e.srv.URL+"/api/v1/programs/play", strings.NewReader("not an exe"))
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("bad program accepted: %d", resp.StatusCode)
	}
	img := sim.PlayerProgram().Image
	req, _ = http.NewRequest("PUT", e.srv.URL+"/api/v1/programs/play", bytes.NewReader(append([]byte("MZ junk"), img...)))
	resp, _ = http.DefaultClient.Do(req)
	b, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(b), "PERFORMANCE PLAYER") {
		t.Fatalf("install: %d %s", resp.StatusCode, b)
	}
	if code, _ := e.do("POST", "/api/v1/sim/power", map[string]bool{"on": false}); code != 200 {
		t.Fatalf("sim power: %d", code)
	}
	e.waitState("offline")
	e.do("POST", "/api/v1/sim/power", map[string]bool{"on": true})
	e.waitState("idle")
	if code, b := e.do("GET", "/", nil); code != 200 || !strings.Contains(string(b), "Piano") {
		t.Fatalf("ui: %d", code)
	}
	if code, b := e.do("GET", "/apple-touch-icon.png", nil); code != 200 || !bytes.HasPrefix(b, []byte("\x89PNG")) {
		t.Fatalf("icon: %d", code)
	}
	if code, _ := e.do("GET", "/api/v1/nothing", nil); code != 404 {
		t.Fatalf("unknown api: %d", code)
	}
}

func TestAlbumAndPlaylistPlayback(t *testing.T) {
	e := newEnv(t)
	var ids []string
	for i, name := range []string{"Gould19550616-Bach_GV01Aria-Steinway_SED519960-SE",
		"Gould19550616-Bach_GV02Var1-Steinway_SED519960-SE", "Gould19550616-Bach_GV03Var2-Steinway_SED519960-SE",
		"Gould19550616-Bach_GV02Var1-Boesendorfer_SE42251-590-22(20150112)-SE"} {
		kb, lp, sp, _ := sim.Phrase(uint32((20 + i) * se.TicksPerSecond)).Encode()
		r := e.upload(map[string][]byte{name + ".kb": kb, name + ".lp": lp, name + ".sp": sp})
		if len(r.Added) != 1 {
			t.Fatalf("upload %s: %+v", name, r)
		}
		ids = append(ids, r.Added[0].ID)
	}
	code, b := e.do("GET", "/api/v1/albums", nil)
	if code != 200 || !strings.Contains(string(b), `"tracks":3`) {
		t.Fatalf("albums: %d %s", code, b)
	}
	album := "Bach: The Goldberg Variations (Glenn Gould, 1955)"
	code, b = e.do("GET", "/api/v1/albums/"+url.PathEscape(album), nil)
	if code != 200 || strings.Contains(string(b), "Boesendorfer") || !strings.Contains(string(b), `"renderings":2`) {
		t.Fatalf("album: %d %s", code, b)
	}

	if code, b := e.do("POST", "/api/v1/deck/play", map[string]any{"album": album}); code != 202 {
		t.Fatalf("play album: %d %s", code, b)
	}
	st := e.waitState("playing")
	if st.Collection == nil || st.Collection.Kind != "album" || st.Collection.Count != 3 || len(st.Tracks) != 3 ||
		st.Tracks[1].StartMs != 20000 || st.DurationMs != 20000+21000+22000 || st.Piece.Title != "Goldberg Variations: Aria" {
		t.Fatalf("album status: %+v", st)
	}
	if code, b := e.do("POST", "/api/v1/deck/next", nil); code != 202 {
		t.Fatalf("next: %d %s", code, b)
	}
	waitFor(t, e, func(s deck.Status) bool {
		return s.State == "playing" && s.Collection != nil && s.Collection.Start == 1
	})
	if code, b := e.do("POST", "/api/v1/deck/previous", nil); code != 202 { // early in the track: the one before
		t.Fatalf("previous: %d %s", code, b)
	}
	waitFor(t, e, func(s deck.Status) bool {
		return s.State == "playing" && s.Collection != nil && s.Collection.Start == 0
	})
	if code, _ := e.do("POST", "/api/v1/deck/play", map[string]any{"album": album, "start": 2}); code != 202 {
		t.Fatalf("play from track 3: %d", code)
	}
	waitFor(t, e, func(s deck.Status) bool {
		return s.State == "playing" && s.Collection != nil && s.Collection.Start == 2 && len(s.Tracks) == 1
	})
	if code, _ := e.do("POST", "/api/v1/deck/next", nil); code != 409 {
		t.Fatalf("next on the last track: %d", code)
	}

	code, b = e.do("POST", "/api/v1/playlists", map[string]any{"name": "Two", "pieces": []string{ids[2], ids[0]}, "source": "x/play.bat"})
	if code != 201 {
		t.Fatalf("create play list: %d %s", code, b)
	}
	var pl struct{ ID string }
	json.Unmarshal(b, &pl)
	if code, b := e.do("POST", "/api/v1/deck/play", map[string]any{"playlist": pl.ID}); code != 202 {
		t.Fatalf("play play list: %d %s", code, b)
	}
	waitFor(t, e, func(s deck.Status) bool {
		return s.State == "playing" && s.Collection != nil && s.Collection.Kind == "playlist" && len(s.Tracks) == 2 &&
			s.Piece != nil && s.Piece.ID == ids[2]
	})
	e.do("POST", "/api/v1/deck/stop", nil)
	e.waitState("idle")
	if code, _ := e.do("DELETE", "/api/v1/playlists/"+pl.ID, nil); code != 204 {
		t.Fatalf("delete play list: %d", code)
	}
	if code, _ := e.do("POST", "/api/v1/deck/play", map[string]any{"piece": ids[0], "album": album}); code != 400 {
		t.Fatalf("two things at once: %d", code)
	}
}

func waitFor(t *testing.T, e *env, ok func(deck.Status) bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for {
		st := e.status()
		if ok(st) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("waited for a state; have %+v", st)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestPianoSettings takes a backup of the simulated piano's table, reads its
// view, takes another after a change, and compares the two.
func TestPianoSettings(t *testing.T) {
	e := newEnv(t)
	if code, _ := e.do("GET", "/api/v1/piano/table", nil); code != 404 {
		t.Fatalf("table before any backup: %d", code)
	}
	code, body := e.do("POST", "/api/v1/piano/backups", nil)
	if code != 201 {
		t.Fatalf("backup: %d %s", code, body)
	}
	var first struct {
		Backup piano.Backup `json:"backup"`
	}
	json.Unmarshal(body, &first)
	if !first.Backup.Valid || first.Backup.Model != "Simulated SE" {
		t.Fatalf("backup %+v", first.Backup)
	}
	e.waitState("idle")
	code, body = e.do("GET", "/api/v1/piano/table", nil)
	var v piano.View
	if code != 200 || json.Unmarshal(body, &v) != nil || len(v.Keys) != 88 || v.Backup == nil || v.Backup.ID != first.Backup.ID {
		t.Fatalf("table: %d %.300s", code, body)
	}

	e.in.EEPROM[0x67F+41] = 2           // the simulated piano's D4 trim, +4 → +2
	time.Sleep(1100 * time.Millisecond) // a new second, so a new id
	code, body = e.do("POST", "/api/v1/piano/backups", nil)
	if code != 201 {
		t.Fatalf("second backup: %d %s", code, body)
	}
	var second struct {
		Backup  piano.Backup `json:"backup"`
		Same    bool         `json:"same_as_previous"`
		Changes piano.Diff   `json:"changes"`
	}
	json.Unmarshal(body, &second)
	if second.Same || second.Changes.Bytes != 1 || len(second.Changes.Changes) != 1 || second.Changes.Changes[0].Key != "D4" {
		t.Errorf("second backup reply %.400s", body)
	}
	code, body = e.do("GET", "/api/v1/piano/backups", nil)
	if code != 200 || !strings.Contains(string(body), second.Backup.ID) || !strings.Contains(string(body), `"retriever"`) {
		t.Errorf("list: %d %.300s", code, body)
	}
	code, body = e.do("GET", "/api/v1/piano/compare?from="+first.Backup.ID+"&to="+second.Backup.ID, nil)
	if code != 200 || !strings.Contains(string(body), `"what":"Trim"`) {
		t.Errorf("compare: %d %.300s", code, body)
	}
	code, body = e.do("GET", "/api/v1/piano/backups/"+first.Backup.ID, nil)
	if code != 200 || len(body) != 2048 {
		t.Errorf("download: %d, %d bytes", code, len(body))
	}
	if code, _ := e.do("GET", "/api/v1/piano/backups/nope", nil); code != 404 {
		t.Errorf("missing backup: %d", code)
	}
}

// TestVoicing sets software voicing, plays a piece with a trimmed key,
// replays it, and plays a key's test notes.
func TestVoicing(t *testing.T) {
	e := newEnv(t)
	kb, lp, sp, _ := sim.Phrase(30 * se.TicksPerSecond).Encode() // plays C4 (60) among others
	name := "Voicing_Test"
	r := e.upload(map[string][]byte{name + ".kb": kb, name + ".lp": lp, name + ".sp": sp})
	if len(r.Added) != 1 {
		t.Fatalf("upload: %+v", r)
	}
	id := r.Added[0].ID

	if code, b := e.do("PUT", "/api/v1/piano/voicing", map[string]any{"on": true, "offset": 0, "trims": map[string]int{"60": 10, "61": 0}}); code != 200 ||
		!strings.Contains(string(b), `"name":"C4"`) || !strings.Contains(string(b), `"active":true`) {
		t.Fatalf("put voicing: %d %s", code, b)
	}
	if code, _ := e.do("PUT", "/api/v1/piano/voicing", map[string]any{"on": true, "offset": 500}); code != 400 {
		t.Errorf("huge offset: %d", code)
	}
	if code, b := e.do("GET", "/api/v1/piano/voicing", nil); code != 200 || !strings.Contains(string(b), `"60":10`) {
		t.Errorf("get voicing: %d %s", code, b)
	}

	if code, b := e.do("POST", "/api/v1/deck/play", map[string]string{"piece": id}); code != 202 {
		t.Fatalf("play: %d %s", code, b)
	}
	if st := e.waitState("playing"); !st.Voiced {
		t.Errorf("playing without voicing: %+v", st)
	}
	if code, b := e.do("POST", "/api/v1/deck/replay", nil); code != 202 {
		t.Errorf("replay: %d %s", code, b)
	}
	if st := e.waitState("playing"); !st.Voiced || st.Piece == nil || st.Piece.ID != id {
		t.Errorf("after replay: %+v", st)
	}

	// Switched off, the same piece plays as recorded.
	e.do("PUT", "/api/v1/piano/voicing", map[string]any{"on": false, "trims": map[string]int{"60": 10}})
	if code, _ := e.do("POST", "/api/v1/deck/play", map[string]string{"piece": id}); code != 202 {
		t.Fatal("play again")
	}
	time.Sleep(300 * time.Millisecond)
	if st := e.waitState("playing"); st.Voiced {
		t.Errorf("voiced while off: %+v", st)
	}

	if code, b := e.do("POST", "/api/v1/piano/audition", map[string]int{"note": 60}); code != 202 {
		t.Fatalf("audition: %d %s", code, b)
	}
	time.Sleep(300 * time.Millisecond)
	st := e.waitState("playing")
	if st.Piece == nil || !strings.Contains(st.Piece.Title, "C4") || !strings.Contains(st.Piece.Title, "+10") {
		t.Errorf("audition status %+v", st.Piece)
	}
	if code, _ := e.do("POST", "/api/v1/deck/replay", nil); code != 409 {
		t.Errorf("replay of test notes: %d", code)
	}
	if code, _ := e.do("POST", "/api/v1/piano/audition", map[string]int{"note": 5}); code != 400 {
		t.Errorf("audition of note 5: %d", code)
	}
	e.do("POST", "/api/v1/deck/stop", nil)
	e.waitState("idle")
}

// TestTools runs simulated tools: one that moves the piano, driven by keys,
// and one that writes its settings, which needs a confirmation and is
// preceded by a backup.
func TestTools(t *testing.T) {
	e := newEnv(t)
	code, b := e.do("GET", "/api/v1/tools", nil)
	if code != 200 || !strings.Contains(string(b), `"name":"cal"`) || !strings.Contains(string(b), `"present":true`) {
		t.Fatalf("tools: %d %.300s", code, b)
	}
	if code, b := e.do("POST", "/api/v1/tools/sustain/start", nil); code != 202 {
		t.Fatalf("start sustain: %d %s", code, b)
	}
	e.waitState("maintenance")
	time.Sleep(300 * time.Millisecond)
	if code, b := e.do("POST", "/api/v1/tools/key", map[string]string{"key": "space"}); code != 204 {
		t.Fatalf("key: %d %s", code, b)
	}
	if code, _ := e.do("POST", "/api/v1/deck/play", map[string]string{"piece": "nope"}); code != 404 && code != 409 {
		t.Errorf("play during a tool: %d", code)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		_, b := e.do("GET", "/api/v1/instrument", nil)
		if strings.Contains(string(b), "KEY SPACE") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the tool never saw the key: %.400s", b)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if code, _ := e.do("POST", "/api/v1/tools/key", map[string]string{"key": "enter"}); code != 204 {
		t.Fatal("enter")
	}
	e.waitState("idle")
	if code, _ := e.do("POST", "/api/v1/tools/key", map[string]string{"key": "space"}); code != 409 {
		t.Errorf("key with no tool: %d", code)
	}

	if code, b := e.do("POST", "/api/v1/tools/cal/start", map[string]bool{"confirm": false}); code != 409 || !strings.Contains(string(b), "backed up first") {
		t.Errorf("cal without confirm: %d %s", code, b)
	}
	code, b = e.do("POST", "/api/v1/tools/cal/start", map[string]bool{"confirm": true})
	if code != 202 || !strings.Contains(string(b), "before-tool") {
		t.Fatalf("cal with confirm: %d %s", code, b)
	}
	e.waitState("maintenance")
	if code, _ := e.do("POST", "/api/v1/tools/key", map[string]string{"text": "12"}); code != 204 {
		t.Error("typed text")
	}
	if code, _ := e.do("POST", "/api/v1/tools/stop", nil); code != 200 {
		t.Error("stop")
	}
	e.waitState("idle")
}

// TestPieceKeys reports each key's velocities in a piece.
func TestPieceKeys(t *testing.T) {
	e := newEnv(t)
	kb, lp, sp, _ := sim.Phrase(30 * se.TicksPerSecond).Encode()
	r := e.upload(map[string][]byte{"Keys_Test.kb": kb, "Keys_Test.lp": lp, "Keys_Test.sp": sp})
	code, b := e.do("GET", "/api/v1/pieces/"+r.Added[0].ID+"/keys", nil)
	if code != 200 || !strings.Contains(string(b), `"name":"C4"`) || !strings.Contains(string(b), `"outliers"`) {
		t.Fatalf("keys: %d %.300s", code, b)
	}
	if code, _ := e.do("GET", "/api/v1/pieces/nope/keys", nil); code != 404 {
		t.Errorf("missing piece: %d", code)
	}
}

func TestEditor(t *testing.T) {
	e := newEnv(t)
	kb, lp, sp, _ := sim.Phrase(20 * se.TicksPerSecond).Encode()
	r := e.upload(map[string][]byte{"Edit_Me.kb": kb, "Edit_Me.lp": lp, "Edit_Me.sp": sp})
	id := r.Added[0].ID

	code, b := e.do("GET", "/api/v1/pieces/"+id+"/performance", nil)
	var perf struct {
		Piece  library.Piece `json:"piece"`
		End    int64         `json:"end"`
		Notes  [][4]int64    `json:"notes"`
		Damper []int         `json:"damper"`
		Soft   []int         `json:"soft"`
	}
	if code != 200 || json.Unmarshal(b, &perf) != nil || len(perf.Notes) == 0 || len(perf.Damper) == 0 {
		t.Fatalf("performance: %d %.300s", code, b)
	}

	// Louder first note, damper down for a second: saved as a copy.
	perf.Notes[0][3] = 100
	for i := 0; i < 100 && i < len(perf.Damper); i++ {
		perf.Damper[i] = 255
	}
	body := map[string]any{"notes": perf.Notes, "damper": perf.Damper, "soft": perf.Soft, "end": perf.End}
	code, b = e.do("POST", "/api/v1/pieces/"+id+"/edits", body)
	var saved struct {
		Piece library.Piece `json:"piece"`
	}
	if code != 201 || json.Unmarshal(b, &saved) != nil {
		t.Fatalf("save: %d %s", code, b)
	}
	cp := saved.Piece
	if cp.ID == id || cp.Origin != library.OriginEdited || cp.Title != perf.Piece.Title+" (edited)" || !strings.Contains(cp.Notes, "Edited from") {
		t.Fatalf("copy %+v", cp)
	}
	if cp.PerfKey == perf.Piece.PerfKey {
		t.Error("the copy shares the original's library row")
	}
	code, b = e.do("GET", "/api/v1/pieces/"+cp.ID+"/performance", nil)
	var again struct {
		Notes  [][4]int64 `json:"notes"`
		Damper []int      `json:"damper"`
	}
	if code != 200 || json.Unmarshal(b, &again) != nil || again.Notes[0][3] != 100 || again.Damper[50] != 255 {
		t.Fatalf("copy's performance: %d %.200s", code, b)
	}

	// The original is never saved over; the copy is.
	body["replace"] = true
	if code, b := e.do("POST", "/api/v1/pieces/"+id+"/edits", body); code != 409 {
		t.Errorf("replace the original: %d %s", code, b)
	}
	perf.Notes = perf.Notes[1:]
	body["notes"] = perf.Notes
	if code, b := e.do("POST", "/api/v1/pieces/"+cp.ID+"/edits", body); code != 200 {
		t.Fatalf("replace the copy: %d %s", code, b)
	}
	code, b = e.do("GET", "/api/v1/pieces/"+cp.ID+"/performance", nil)
	if code != 200 || json.Unmarshal(b, &again) != nil || len(again.Notes) != len(perf.Notes) {
		t.Fatalf("after replace: %d notes, want %d", len(again.Notes), len(perf.Notes))
	}

	// Bad notes are refused.
	bad := map[string]any{"notes": [][4]int64{{60, 100, 50, 200}}}
	if code, _ := e.do("POST", "/api/v1/pieces/"+id+"/edits", bad); code != 400 {
		t.Errorf("note ending before it starts: %d", code)
	}

	// Heard from the middle, without saving.
	body["from"] = 4 * se.TicksPerSecond
	if code, b := e.do("POST", "/api/v1/pieces/"+id+"/preview", body); code != 202 {
		t.Fatalf("preview: %d %s", code, b)
	}
	st := e.waitState("playing")
	if st.Piece == nil || st.Piece.Performer != PreviewPerformer {
		t.Errorf("playing %+v", st.Piece)
	}
}
