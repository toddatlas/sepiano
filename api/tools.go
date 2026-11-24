package api

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"sepiano/deck"
	"sepiano/host"
	"sepiano/sim"
)

// The suite's other programs, run from the app as tools: the user drives
// them with their own keys and sees their text (docs/SUITE-PROGRAMS.md).
// A program that writes the piano's settings needs a confirmation, and the
// service backs the settings up before starting it.

// toolDef describes one tool.
type toolDef struct {
	Name    string   `json:"name"`
	File    string   `json:"file"`
	Title   string   `json:"title"`
	Does    string   `json:"does"`
	Risk    string   `json:"risk"` // reads (sensors only), moves (the keys or dampers), writes (the piano's settings)
	Keys    []string `json:"keys"` // the keys it understands
	Input   bool     `json:"input,omitempty"`
	Warning string   `json:"warning,omitempty"`
}

var tools = []toolDef{
	{Name: "anykey", File: "anykey.exe", Title: "Key sensor test", Risk: "reads", Keys: []string{"enter"},
		Does: "Tests the key sensors: play keys on the piano and watch what it reports. Enter quits."},
	{Name: "anyham", File: "anyham.exe", Title: "Hammer sensor test", Risk: "reads", Keys: []string{"enter"},
		Does: "Tests the hammer sensors: play keys on the piano and watch what it reports. Enter quits."},
	{Name: "midiin", File: "midiin.exe", Title: "MIDI input", Risk: "moves", Keys: []string{"down", "up", "enter"},
		Does: "Plays whatever arrives at the piano's own MIDI input, live. Down and Up make it softer or restore it; Enter quits."},
	{Name: "sustain", File: "sustain.exe", Title: "Dampers", Risk: "moves", Keys: []string{"space", "enter"},
		Does: "Raises and lowers the dampers with Space. Lower them before quitting with Enter."},
	{Name: "adjust", File: "adjust.exe", Title: "Manual adjustment", Risk: "moves", Keys: []string{"space", "esc", "enter"},
		Does: "For a technician: steps through the keys of the calibration range. Space starts and stops adjusting a key, Esc goes to the next key, Enter quits."},
	{Name: "cal", File: "cal.exe", Title: "Calibrator", Risk: "writes", Keys: []string{"space", "esc", "enter"},
		Does:    "Stahnke's adaptive calibrator: for each key in the calibration range it measures the free release time, finds the release levels, and finds the strike level for each loudness, playing the key many times. Space stops and resumes, Esc restarts, Enter quits.",
		Warning: "This rewrites the calibration of every key in the calibration range (set it with the settings menu). The piano's settings are backed up first, so they can be compared and restored."},
	{Name: "silent", File: "silent.exe", Title: "Silent-press calibrator", Risk: "writes", Keys: []string{"space", "esc", "enter"},
		Does:    "Finds, for each key in the calibration range, the gentle press that moves the key down without sounding it. Space starts and stops, Esc restarts, Enter quits.",
		Warning: "This rewrites the silent-press level of every key in the calibration range. The piano's settings are backed up first."},
	{Name: "setup", File: "setup.exe", Title: "Settings menu", Risk: "writes", Keys: []string{"enter", "esc"}, Input: true,
		Does:    "Stahnke's menu for the serial number, silent notes, German note names, the calibration range and the first-calibration switch. Type the number of an item and press Send, then type its value (T or F for a switch, a MIDI note number for a key: 60 is middle C).",
		Warning: "This changes the piano's settings. They are backed up first."},
}

func init() {
	for _, t := range tools {
		ProgramFiles[t.Name] = t.File
	}
}

func toolByName(name string) (toolDef, bool) {
	for _, t := range tools {
		if t.Name == name {
			return t, true
		}
	}
	return toolDef{}, false
}

func (s *Server) toolRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/tools", s.listTools)
	m.HandleFunc("POST /api/v1/tools/{name}/start", s.startTool)
	m.HandleFunc("POST /api/v1/tools/key", s.toolKey)
	m.HandleFunc("POST /api/v1/tools/stop", s.deckCmd(s.Deck.StopTool))
}

func (s *Server) listTools(w http.ResponseWriter, r *http.Request) {
	type entry struct {
		toolDef
		ProgramInfo
	}
	out := []entry{}
	for _, t := range tools {
		out = append(out, entry{t, s.Programs.Info(t.Name)})
	}
	writeJSON(w, 200, map[string]any{"tools": out})
}

func (s *Server) startTool(w http.ResponseWriter, r *http.Request) {
	t, ok := toolByName(r.PathValue("name"))
	if !ok {
		fail(w, 404, "no such tool")
		return
	}
	var req struct {
		Confirm bool `json:"confirm"`
	}
	if r.ContentLength != 0 {
		if err := readJSON(r, &req); err != nil {
			fail(w, 400, err.Error())
			return
		}
	}
	prog, err := s.Programs.Load(t.Name)
	if err != nil {
		fail(w, 503, err.Error())
		return
	}
	if st := s.Deck.Status(); st.State != deck.Idle {
		fail(w, 409, "the piano is busy: "+string(st.State))
		return
	}
	var backup any
	if t.Risk == "writes" {
		if !req.Confirm {
			fail(w, 409, t.Warning+" Confirm to go ahead.")
			return
		}
		if s.Piano == nil {
			fail(w, 503, "no settings store, so no backup can be taken first")
			return
		}
		reply, err := s.backup(r.Context(), "before-tool")
		if err != nil {
			playError(w, errors.Join(errors.New("could not back up the piano's settings first, so the tool was not started"), err))
			return
		}
		backup = reply["backup"]
		if err := s.waitIdle(r.Context(), 20*time.Second); err != nil {
			fail(w, 503, "the piano did not come back after the backup: "+err.Error())
			return
		}
	}
	if err := s.runTool(r.Context(), prog, t.Name); err != nil {
		deckError(w, err)
		return
	}
	s.logf("tool started: %s", prog.Banner)
	writeJSON(w, 202, map[string]any{"status": s.Deck.Status(), "backup": backup})
}

func (s *Server) runTool(ctx context.Context, prog *host.Program, name string) error {
	dir := filepath.Join(os.TempDir(), "sepiano-tools")
	if s.Piano != nil {
		dir = filepath.Join(s.Piano.Dir, "work")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return s.Deck.StartTool(ctx, prog, filepath.Join(dir, name+".dat"))
}

// waitIdle waits for the deck to be idle (after a utility, it reconnects).
func (s *Server) waitIdle(ctx context.Context, limit time.Duration) error {
	deadline := time.Now().Add(limit)
	for {
		st := s.Deck.Status()
		if st.State == deck.Idle {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New(string(st.State))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// toolKeys are the keys a tool can be sent, as the PC console codes the
// suite's host passes on.
var toolKeys = map[string][]byte{
	"space": {' '}, "enter": {0x0D}, "esc": {0x1B},
	"up": {0x00, 0x48}, "down": {0x00, 0x50}, "left": {0x00, 0x4B}, "right": {0x00, 0x4D},
}

// toolKey sends a key, or typed text (then Enter), to the running tool.
func (s *Server) toolKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key  string `json:"key"`
		Text string `json:"text"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, err.Error())
		return
	}
	var keys [][]byte
	switch {
	case req.Key != "":
		k, ok := toolKeys[req.Key]
		if !ok {
			fail(w, 400, "no such key: "+req.Key)
			return
		}
		keys = append(keys, k)
	case req.Text != "":
		for _, c := range []byte(req.Text) {
			if c < 0x20 || c >= 0x7F {
				fail(w, 400, "type plain letters and digits only")
				return
			}
			keys = append(keys, []byte{c})
		}
		keys = append(keys, []byte{0x0D})
	default:
		fail(w, 400, "give a key or some text")
		return
	}
	ctx, cancel := cmdContext(r)
	defer cancel()
	for _, k := range keys {
		if err := s.Deck.ToolKey(ctx, k); err != nil {
			deckError(w, err)
			return
		}
		time.Sleep(20 * time.Millisecond) // one key at a time, as typed
	}
	w.WriteHeader(204)
}

// SimulatedTools are stand-ins for the tools' programs, for the simulator.
func SimulatedTools() map[string]*host.Program {
	m := map[string]*host.Program{}
	for _, t := range tools {
		m[t.Name] = sim.ToolProgram(t.Title)
	}
	return m
}
