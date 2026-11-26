package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"sepiano/deck"
	"sepiano/host"
	"sepiano/library"
	"sepiano/lut"
	"sepiano/piano"
	"sepiano/se"
	"sepiano/sim"
)

// The suite's other programs, run from the app as tools: the user drives
// them with their own keys and sees their text (docs/SUITE-PROGRAMS.md).
// Every program of the suite is here, except play, record and getlut,
// which the service runs itself.
//
// A tool that writes the piano's settings needs a confirmation; one that
// erases its calibration (a factory table) also needs a typed word. The
// service backs the settings up before such a tool starts, and again
// after it ends, so the two can be compared; after a restore it checks
// that the piano holds the table it was given.

// toolDef describes one tool.
type toolDef struct {
	Name  string `json:"name"`
	File  string `json:"file"`
	Title string `json:"title"`
	Group string `json:"group"`
	Does  string `json:"does"`
	// Risk: reads (the sensors only), moves (the keys or dampers), writes
	// (the piano's settings), erases (replaces its calibration).
	Risk    string   `json:"risk"`
	Keys    []string `json:"keys"` // the keys it understands
	Input   bool     `json:"input,omitempty"`
	Warning string   `json:"warning,omitempty"`
	// Needs is what the program reads: a backup of the piano's settings
	// (to restore), a piece from the library, a MIDI file (a piece's
	// original, or an upload), or an uploaded file.
	Needs  string `json:"needs,omitempty"`
	Accept string `json:"accept,omitempty"` // file types for an upload
	// Result is what happens after: "performance" adds the performance
	// the program wrote to the library; "table" checks the piano's table
	// against the one restored.
	Result string `json:"result,omitempty"`
	Phrase string `json:"phrase,omitempty"` // to type to confirm (erases)
	// Help is a command reference shown beside the console: what to type,
	// and what it does.
	Help [][2]string `json:"help,omitempty"`
}

// editHelp is edit.exe's command language, as its code reads
// (docs/STAHNKE-EDITOR.md).
var editHelp = [][2]string{
	{"D", "Status: segment, time, workspace, pedals, settings"},
	{"T", "Where the cursor is"},
	{"T1500 · T+50 · T-50 · T0", "Move the cursor (hundredths of a second) within the loaded part; no spaces"},
	{"B, BB… · F, FF…", "One second back or forward per letter"},
	{"FILL", "Keep the last 4 s and load the next part (about 28 s at a time; there is no way back)"},
	{"W=6000", "Wind forward to a time"},
	{"P · I", "Play from 4 s before the cursor; I pauses at the cursor. Space pauses, Enter stops there, Esc stops"},
	{"S · G (or L)", "Step through the notes, or the chords, striking each. Space next, Enter selects, Esc stops"},
	{"SEARCH", "Next silent or very soft note"},
	{"AI=120", "The selected note's velocity (0 silent, 4–1023, bigger is softer)"},
	{"AN=62", "The selected note becomes key 62 (MIDI number)"},
	{"AT=1520 · AT+5 · AT-5", "Move the selected press or release"},
	{"DE · DN", "Delete the selected event, or the whole note"},
	{"RESTORE", "Put back the last deletion"},
	{"IN=60,80,50", "Insert key 60, velocity 80, half a second long, at the cursor"},
	{"IR=60 · IR=60,20", "Release key 60 at the cursor (or 0.2 s after)"},
	{"SOSTENUTO ACTUATE · SOSTENUTO RELEASE", "Sostenuto pedal down or up at the cursor"},
	{"IC", "A cue mark at the cursor"},
	{"IT", "0.02 s more time at the cursor (repeat for more)"},
	{"SPLICE=1800", "Cut everything from the cursor to 18.00 s"},
	{"DL · DS · AL · AS", "Show or change the damper or soft pedal, sample by sample"},
	{"HEADING=Title\\Performer", "The text shown at the start (first part only; \\ is a new line)"},
	{"LOUD · SOFT · U · M · R=80 · PREFACE=200 · ALERT=400", "How it plays here (pedals, same loudness, a bit softer, tempo %, lead-in, soft-note click); the piece does not change"},
	{"END", "Save the whole piece (it becomes an edited copy in the library) and finish"},
	{"QUIT", "Throw the changes away and finish"},
	{"LOUDER · SOFTER · LEVEL", "⚠ These change the piano's stored per-key trims (its settings), not the piece. LEVEL sets every trim to 0"},
}

// The groups, in the order the app shows them.
var toolGroups = []string{"Tests", "Keys and pedals", "Players", "Calibration and settings", "Stahnke's editor", "Tape", "Factory tables", "Expert"}

const factoryWarning = "This replaces the piano's whole table, calibration included, with the factory starting table for one model. Every key would have to be calibrated again (with the Calibrator and the Silent-press calibrator). To put the piano back as it was, restore a backup instead. The settings are backed up first."

var tools = []toolDef{
	{Name: "anykey", File: "anykey.exe", Title: "Key sensor test", Group: "Tests", Risk: "reads", Keys: []string{"enter"},
		Does: "Tests the key sensors: play keys on the piano and watch what it reports. Enter quits."},
	{Name: "anyham", File: "anyham.exe", Title: "Hammer sensor test", Group: "Tests", Risk: "reads", Keys: []string{"enter"},
		Does: "Tests the hammer sensors: play keys on the piano and watch what it reports. Enter quits."},
	{Name: "sustain", File: "sustain.exe", Title: "Dampers", Group: "Keys and pedals", Risk: "moves", Keys: []string{"space", "enter"},
		Does: "Raises and lowers the dampers with Space. Lower them before quitting with Enter."},
	{Name: "adjust", File: "adjust.exe", Title: "Manual adjustment", Group: "Keys and pedals", Risk: "moves", Keys: []string{"space", "esc", "enter"},
		Does: "For a technician: steps through the keys of the calibration range. Space starts and stops adjusting a key, Esc goes to the next key, Enter quits."},
	{Name: "midiin", File: "midiin.exe", Title: "MIDI input", Group: "Players", Risk: "moves", Keys: []string{"down", "up", "enter"},
		Does: "Plays whatever arrives at the piano's own MIDI input, live. Down and Up make it softer or restore it; Enter quits. Pedal moves reach the piano as they arrive, without the app's pedal slowing."},
	{Name: "playmidi", File: "playmidi.exe", Title: "MIDI file player", Group: "Players", Risk: "moves", Keys: []string{"space", "esc", "down", "up", "enter"},
		Needs: "midi", Accept: ".mid,.midi",
		Does: "Stahnke's own player for standard MIDI files, which converts as it plays. Choose a piece converted from MIDI (its original is kept) or upload a MIDI file. Its pedals reach the piano as the file has them, without the app's pedal slowing: for a file with an on/off sustain pedal, play the converted piece from the Library instead. Space stops and resumes, Esc restarts, Down and Up make it softer or restore it, Enter quits."},
	{Name: "playeseq", File: "playeseq.exe", Title: "PianoSoft disk player", Group: "Players", Risk: "moves", Keys: []string{"space", "esc", "down", "up", "enter"},
		Needs: "file", Accept: ".fil,.esq,.mid",
		Does: "Stahnke's player for Yamaha PianoSoft (E-SEQ) files, as on Disklavier floppy disks. Upload the file. Space stops and resumes, Esc restarts, Down and Up make it softer or restore it, Enter quits."},
	{Name: "cal", File: "cal.exe", Title: "Calibrator", Group: "Calibration and settings", Risk: "writes", Keys: []string{"space", "esc", "enter"},
		Does:    "Stahnke's adaptive calibrator: for each key in the calibration range it measures the free release time, finds the release levels, and finds the strike level for each loudness, playing the key many times. Space stops and resumes, Esc restarts, Enter quits.",
		Warning: "This rewrites the calibration of every key in the calibration range (set it with the settings menu). The piano's settings are backed up before and after, so they can be compared and restored."},
	{Name: "silent", File: "silent.exe", Title: "Silent-press calibrator", Group: "Calibration and settings", Risk: "writes", Keys: []string{"space", "esc", "enter"},
		Does:    "Finds, for each key in the calibration range, the gentle press that moves the key down without sounding it. Space starts and stops, Esc restarts, Enter quits.",
		Warning: "This rewrites the silent-press level of every key in the calibration range. The piano's settings are backed up before and after."},
	{Name: "setup", File: "setup.exe", Title: "Settings menu", Group: "Calibration and settings", Risk: "writes", Keys: []string{"enter", "esc"}, Input: true,
		Does:    "Stahnke's menu for the serial number, silent notes, German note names, the calibration range and the first-calibration switch. Type the number of an item and press Send, then type its value (T or F for a switch, a MIDI note number for a key: 60 is middle C).",
		Warning: "This changes the piano's settings. They are backed up before and after."},
	{Name: "putlut", File: "putlut.exe", Title: "Restore a backup", Group: "Calibration and settings", Risk: "writes", Keys: []string{},
		Needs: "backup", Result: "table",
		Does:    "Writes a backup of the piano's settings back into the piano, calibration included: the way back after a calibration that went wrong. It starts at once and needs no keys. Afterwards the service reads the piano's table again and checks that it matches the backup.",
		Warning: "This replaces the piano's whole table (calibration and settings) with the backup chosen. The present settings are backed up first, so this too can be undone."},
	{Name: "edit", File: "edit.exe", Title: "Stahnke's performance editor", Group: "Stahnke's editor", Risk: "writes", Keys: []string{"space", "enter", "esc", "backspace"}, Input: true,
		Needs: "piece", Result: "performance", Help: editHelp,
		Does:    "Wayne Stahnke's own command-line editor, working on a copy of a piece: type its commands (below) and press Send. It plays and steps through notes on the piano. If it saves (END), the result is added to the library as an edited copy; the piece itself never changes. The piano-roll editor does most of the same more easily.",
		Warning: "Its LOUDER, SOFTER and LEVEL commands change the piano's stored per-key trims at once (LEVEL sets them all to zero), so the piano's settings are backed up before and after. Avoid them unless that is what you want."},
	{Name: "dump", File: "dump.exe", Title: "Tape: dump a performance", Group: "Tape", Risk: "moves", Keys: []string{"space", "enter"},
		Needs: "piece",
		Does:  "Sends a performance out through the piano's tape interface to a cassette recorder. Start the recorder, then Space; Enter quits. Needs a cassette recorder on the tape interface."},
	{Name: "load", File: "load.exe", Title: "Tape: load a performance", Group: "Tape", Risk: "moves", Keys: []string{"enter"},
		Result: "performance",
		Does:   "Reads a performance from a cassette through the piano's tape interface; it is added to the library when it ends. Start the tape; Enter quits. Needs a cassette recorder on the tape interface."},
	{Name: "tape", File: "tape.exe", Title: "Tape: record or play", Group: "Tape", Risk: "moves", Keys: []string{"down", "up", "enter"},
		Does: "Records the piano to tape, or plays a tape on the piano, through its tape interface. Operate the recorder; Enter quits. Needs a cassette recorder on the tape interface."},
	{Name: "putb", File: "putb.exe", Title: "Factory table: Steinway B-SE", Group: "Factory tables", Risk: "erases", Keys: []string{}, Phrase: "ERASE",
		Does:    "Writes Stahnke's prototype table for a Steinway B at once: the table this piano's started from before its first calibration (the piano is in fact a 1909 Steinway D).",
		Warning: factoryWarning},
	{Name: "put225", File: "put225.exe", Title: "Factory table: Bösendorfer 225SE", Group: "Factory tables", Risk: "erases", Keys: []string{}, Phrase: "ERASE",
		Does: "Writes Stahnke's prototype table for a Bösendorfer 225 at once. Not this piano's kind.", Warning: factoryWarning},
	{Name: "put275", File: "put275.exe", Title: "Factory table: Bösendorfer 275SE", Group: "Factory tables", Risk: "erases", Keys: []string{}, Phrase: "ERASE",
		Does: "Writes Stahnke's prototype table for a Bösendorfer 275 at once. Not this piano's kind.", Warning: factoryWarning},
	{Name: "put290", File: "put290.exe", Title: "Factory table: Bösendorfer 290SE", Group: "Factory tables", Risk: "erases", Keys: []string{}, Phrase: "ERASE",
		Does: "Writes Stahnke's prototype table for a Bösendorfer 290 (Imperial) at once. Not this piano's kind.", Warning: factoryWarning},
	{Name: "putc7", File: "putc7.exe", Title: "Factory table: Yamaha C-7SE", Group: "Factory tables", Risk: "erases", Keys: []string{}, Phrase: "ERASE",
		Does: "Writes Stahnke's prototype table for a Yamaha C7 at once. Not this piano's kind.", Warning: factoryWarning},
	{Name: "monitor", File: "monitor.exe", Title: "Z80 monitor", Group: "Expert", Risk: "writes", Keys: []string{"esc", "enter"}, Input: true,
		Does:    "Stahnke's machine-code monitor, for an expert: type its commands (D ssss eeee shows memory from ssss to eeee in hex; Q quits). The firmware was read with it (docs/SUITE-PROGRAMS.md).",
		Warning: "The monitor can change memory and the EEPROM as well as show them. Use only the commands you know. The piano's settings are backed up before and after."},
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
	m.HandleFunc("POST /api/v1/tools/files", s.toolUpload)
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
	writeJSON(w, 200, map[string]any{"tools": out, "groups": toolGroups})
}

// toolWork is the folder the tools' files are put in.
func (s *Server) toolWork(sub string) (string, error) {
	dir := filepath.Join(os.TempDir(), "sepiano-tools")
	if s.Piano != nil {
		dir = filepath.Join(s.Piano.Dir, "work")
	}
	dir = filepath.Join(dir, sub)
	return dir, os.MkdirAll(dir, 0o755)
}

var notNameChar = regexp.MustCompile(`[^a-z0-9]`)

// shortName fits a file name to the instrument's 8.3 names.
func shortName(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	base := notNameChar.ReplaceAllString(strings.ToLower(strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))), "")
	ext = "." + notNameChar.ReplaceAllString(strings.TrimPrefix(ext, "."), "")
	if base == "" {
		base = "file"
	}
	if len(base) > 8 {
		base = base[:8]
	}
	if len(ext) > 4 {
		ext = ext[:4]
	}
	if ext == "." {
		ext = ""
	}
	return base + ext
}

// toolUpload stores a file for a tool to read (a MIDI or PianoSoft file):
// POST /tools/files?name=song.mid with the file as the body.
func (s *Server) toolUpload(w http.ResponseWriter, r *http.Request) {
	name := shortName(r.URL.Query().Get("name"))
	dir, err := s.toolWork("upload")
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
	if err != nil {
		fail(w, 400, "the file is too big (at most 4 MB)")
		return
	}
	if len(data) == 0 {
		fail(w, 400, "the file is empty")
		return
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		fail(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"file": name, "size": len(data)})
}

type toolRequest struct {
	Confirm bool   `json:"confirm"`
	Phrase  string `json:"phrase"`
	Backup  string `json:"backup"` // for needs "backup": the backup's id
	Piece   string `json:"piece"`  // for needs "piece" or "midi"
	File    string `json:"file"`   // for needs "file" or "midi": an uploaded file's name
}

// toolRun is a started tool and what to do when it ends.
type toolRun struct {
	def      toolDef
	path     string        // the file service's current file
	piece    library.Piece // the piece it works on
	original []byte        // for "edit": the .kb it was given
	restore  []byte        // for "putlut": the table it writes
}

func newSuffix() string {
	b := make([]byte, 3)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// toolFiles prepares the files a tool reads, and its file service path.
func (s *Server) toolFiles(t toolDef, req toolRequest) (*toolRun, error) {
	run := &toolRun{def: t}
	dir, err := s.toolWork(t.Name)
	if err != nil {
		return nil, err
	}
	copyPiece := func(id string) error {
		p, err := s.Lib.Get(id)
		if err != nil {
			return httpError{404, "no such piece"}
		}
		src, base, _ := s.Lib.Files(id)
		name := "p" + newSuffix() // 7 characters: an 8.3 name
		for _, ext := range []string{".kb", ".lp", ".sp"} {
			b, err := os.ReadFile(filepath.Join(src, base+ext))
			if err != nil {
				return err
			}
			if ext == ".kb" {
				run.original = b
			}
			if err := os.WriteFile(filepath.Join(dir, name+ext), b, 0o644); err != nil {
				return err
			}
		}
		run.piece, run.path = p, filepath.Join(dir, name+".kb")
		return nil
	}
	uploaded := func(name string) (string, error) {
		up, err := s.toolWork("upload")
		if err != nil {
			return "", err
		}
		path := filepath.Join(up, shortName(name))
		if _, err := os.Stat(path); err != nil {
			return "", httpError{404, "no such uploaded file: upload it again"}
		}
		return path, nil
	}
	switch t.Needs {
	case "":
		run.path = filepath.Join(dir, t.Name+".dat")
		if t.Result == "performance" {
			run.path = filepath.Join(dir, "t"+newSuffix()+".kb") // a fresh name to write to
		}
	case "piece":
		if req.Piece == "" {
			return nil, httpError{400, "choose a piece"}
		}
		if err := copyPiece(req.Piece); err != nil {
			return nil, err
		}
	case "midi":
		switch {
		case req.File != "":
			if run.path, err = uploaded(req.File); err != nil {
				return nil, err
			}
		case req.Piece != "":
			p, err := s.Lib.Get(req.Piece)
			if err != nil {
				return nil, httpError{404, "no such piece"}
			}
			src, base, _ := s.Lib.Files(p.ID)
			b, err := os.ReadFile(filepath.Join(src, base+".mid"))
			if err != nil || !p.HasMIDISource {
				return nil, httpError{409, "this piece has no original MIDI file"}
			}
			run.piece, run.path = p, filepath.Join(dir, "m"+newSuffix()+".mid")
			if err := os.WriteFile(run.path, b, 0o644); err != nil {
				return nil, err
			}
		default:
			return nil, httpError{400, "choose a piece converted from MIDI, or upload a MIDI file"}
		}
	case "file":
		if req.File == "" {
			return nil, httpError{400, "upload a file first"}
		}
		if run.path, err = uploaded(req.File); err != nil {
			return nil, err
		}
	case "backup":
		if s.Piano == nil {
			return nil, httpError{503, "no settings store"}
		}
		if req.Backup == "" {
			return nil, httpError{400, "choose a backup"}
		}
		img, b, err := s.Piano.Get(req.Backup)
		if errors.Is(err, piano.ErrNotFound) {
			return nil, httpError{404, "no such backup"}
		}
		if err != nil {
			return nil, err
		}
		if err := checkRestore(img); err != nil {
			return nil, httpError{409, fmt.Sprintf("backup %s cannot be restored: %v", b.ID, err)}
		}
		if err := s.checkModel(img); err != nil {
			return nil, err
		}
		run.restore = img
		run.path = filepath.Join(dir, "restore.lut")
		if err := os.WriteFile(run.path, img, 0o644); err != nil {
			return nil, err
		}
	}
	return run, nil
}

// checkModel refuses to restore a table for a model this piano has never
// had: one from another piano. It compares with every backup read from
// the piano (all but imported ones), not only the latest, so that after a
// factory table for another model the piano's own backup can still go
// back.
func (s *Server) checkModel(img []byte) error {
	bt, err := lut.Parse(img)
	if err != nil {
		return nil // checkRestore has refused it already
	}
	list, err := s.Piano.List()
	if err != nil {
		return err
	}
	var models []string
	for _, b := range list {
		if b.Reason == "imported" || b.Model == "" {
			continue
		}
		if b.Model == bt.Name {
			return nil
		}
		if !slices.Contains(models, b.Model) {
			models = append(models, b.Model)
		}
	}
	if len(models) == 0 {
		return nil // nothing read from the piano yet to compare with
	}
	return httpError{409, fmt.Sprintf("this backup is a table for “%s”, but the piano's own backups are for “%s”", bt.Name, strings.Join(models, "”, “"))}
}

// checkRestore refuses a table that must never be written: one whose
// first word is not 3017H (play.exe would then wipe its first 256 bytes),
// or that is not a whole, readable table.
func checkRestore(img []byte) error {
	if len(img) != lut.Size {
		return fmt.Errorf("it is %d bytes, not %d", len(img), lut.Size)
	}
	if len(img) < 2 || img[0] != 0x17 || img[1] != 0x30 {
		return errors.New("its first word is not 3017H")
	}
	if _, err := lut.Parse(img); err != nil {
		return err
	}
	return nil
}

func (s *Server) startTool(w http.ResponseWriter, r *http.Request) {
	t, ok := toolByName(r.PathValue("name"))
	if !ok {
		fail(w, 404, "no such tool")
		return
	}
	var req toolRequest
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
	writes := t.Risk == "writes" || t.Risk == "erases"
	if writes && !req.Confirm {
		fail(w, 409, t.Warning+" Confirm to go ahead.")
		return
	}
	if t.Phrase != "" && !strings.EqualFold(strings.TrimSpace(req.Phrase), t.Phrase) {
		fail(w, 409, fmt.Sprintf("Type %s to confirm.", t.Phrase))
		return
	}
	run, err := s.toolFiles(t, req)
	if err != nil {
		playError(w, err)
		return
	}
	var backup any
	if writes {
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
		// A short or empty read is no backup to go back to. (A table the
		// service cannot read is still kept and allowed: putlut may be
		// what repairs it.)
		if b, ok := backup.(piano.Backup); !ok || b.Size != lut.Size {
			size := 0
			if ok {
				size = b.Size
			}
			fail(w, 502, fmt.Sprintf("the backup taken first is %d bytes, not a whole table (%d), so the tool was not started. Try again.", size, lut.Size))
			return
		}
		if err := s.waitIdle(r.Context(), 20*time.Second); err != nil {
			fail(w, 503, "the piano did not come back after the backup: "+err.Error())
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	done, err := s.Deck.StartTool(ctx, prog, run.path)
	if err != nil {
		deckError(w, err)
		return
	}
	s.logf("tool started: %s (%s)", prog.Banner, run.path)
	go s.afterTool(run, done)
	writeJSON(w, 202, map[string]any{"status": s.Deck.Status(), "backup": backup})
}

// afterTool waits for a tool to end, then does what its result asks: a
// performance it wrote joins the library; a writing tool's settings are
// backed up again; a restore is checked against the piano.
func (s *Server) afterTool(run *toolRun, done <-chan deck.UtilityResult) {
	res := <-done
	t := run.def
	if t.Result == "performance" {
		s.importToolPerformance(run)
	}
	if t.Risk != "writes" && t.Risk != "erases" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := s.waitIdle(ctx, 60*time.Second); err != nil {
		s.Deck.Notice("warn", fmt.Sprintf("After %s the piano did not come back, so its settings were not backed up again: %v", t.Title, err))
		return
	}
	reply, err := s.backup(ctx, "after-tool")
	if err != nil {
		s.Deck.Notice("warn", fmt.Sprintf("After %s the piano's settings could not be backed up again: %v", t.Title, err))
		return
	}
	b, _ := reply["backup"].(piano.Backup)
	if t.Result == "table" && run.restore != nil {
		img, _, err := s.Piano.Get(b.ID)
		switch {
		case err != nil:
			s.Deck.Notice("warn", "The restore could not be checked: "+err.Error())
		case bytes.Equal(img, run.restore):
			s.Deck.Notice("info", "Restore checked: the piano's table now matches the backup exactly.")
		default:
			n := 0
			for i := range img {
				if i < len(run.restore) && img[i] != run.restore[i] {
					n++
				}
			}
			s.Deck.Notice("error", fmt.Sprintf("Restore NOT confirmed: the piano's table differs from the backup in %d bytes. Compare the backups on the Piano tab before playing.", n))
		}
		return
	}
	if res.Err == nil {
		what := "unchanged"
		if same, _ := reply["same_as_previous"].(bool); !same {
			what = "changed: compare the last two backups on the Piano tab"
		}
		s.Deck.Notice("info", fmt.Sprintf("After %s the piano's settings were backed up again (%s).", t.Title, what))
	}
}

// importToolPerformance adds what a tool wrote (name.kb, .lp, .sp in its
// folder) to the library: an edited copy of the piece edit.exe worked on,
// or a performance loaded from tape.
func (s *Server) importToolPerformance(run *toolRun) {
	base := strings.TrimSuffix(run.path, ".kb")
	kb, err := os.ReadFile(base + ".kb")
	if err != nil {
		return // nothing was saved
	}
	if run.original != nil && bytes.Equal(kb, run.original) {
		return // saved unchanged
	}
	lp, err1 := os.ReadFile(base + ".lp")
	sp, err2 := os.ReadFile(base + ".sp")
	if err1 != nil || err2 != nil {
		s.Deck.Notice("warn", fmt.Sprintf("%s wrote an incomplete performance; it was not added to the library.", run.def.Title))
		return
	}
	var p library.Piece
	if run.piece.ID != "" {
		perf, err := parseSet(kb, lp, sp)
		if err == nil {
			p, _, err = s.Lib.AddEdited(run.piece, perf, library.EditedCopy{
				Title: run.piece.Title + " (edit.exe)",
				Note:  fmt.Sprintf("Edited with Stahnke's editor from “%s”.", run.piece.Title),
			})
		}
		if err != nil {
			s.Deck.Notice("warn", "The edited performance could not be added to the library: "+err.Error())
			return
		}
	} else {
		title := "Loaded from tape, " + time.Now().Format("2006-01-02 15:04")
		p, _, err = s.Lib.AddSet(filepath.Base(base), kb, lp, sp, library.OriginUpload, title)
		if err != nil {
			s.Deck.Notice("warn", "The loaded performance could not be added to the library: "+err.Error())
			return
		}
	}
	s.libraryChanged(p.ID)
	s.Deck.Notice("info", fmt.Sprintf("“%s” was added to the library.", p.Title))
}

func parseSet(kb, lp, sp []byte) (*se.Performance, error) {
	k, err := se.ParseKB(kb)
	if err != nil {
		return nil, err
	}
	d, err := se.ParsePedal(lp)
	if err != nil {
		return nil, err
	}
	f, err := se.ParsePedal(sp)
	if err != nil {
		return nil, err
	}
	return &se.Performance{KB: k, Damper: d, Soft: f}, nil
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
	"space": {' '}, "enter": {0x0D}, "esc": {0x1B}, "backspace": {0x08},
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
		switch {
		case t.Name == "putlut":
			m[t.Name] = sim.InstallerProgram(true)
		case t.Risk == "erases":
			m[t.Name] = sim.InstallerProgram(false)
		case t.Name == "load":
			m[t.Name] = sim.LoaderProgram()
		default:
			m[t.Name] = sim.ToolProgram(t.Title)
		}
	}
	return m
}
