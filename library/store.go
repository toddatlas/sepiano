// Package library stores the piano's performances.
//
// Files are the source of truth. Each piece is a set of files in one folder,
// named by an 8-character id (the longest name the Z80 programs accept, should
// one ask for it):
//
//	<id>.kb  <id>.lp  <id>.sp   the performance, byte for byte as uploaded
//	<id>.json                   metadata (written last)
//	<id>.mid                    the original MIDI file, for converted pieces
//
// The index is kept in memory and rebuilt from the folder at start, so there
// is no database to corrupt. Every write goes to a temporary file that is
// synced and renamed into place, because the Pi's power may be cut at any
// moment.
package library

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"sepiano/se"
)

// Origins of a piece.
const (
	OriginImport   = "import"
	OriginUpload   = "upload"
	OriginMIDI     = "midi"
	OriginRecorded = "recorded"
	OriginEdited   = "edited" // a copy changed in the editor
)

// DefaultInstrument is the rendering this piano is.
const DefaultInstrument = "Steinway_SED133291"

// ErrNotFound means no piece has that id.
var ErrNotFound = errors.New("no such piece")

// Piece is one performance in the library.
type Piece struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	Composer      string    `json:"composer"`
	Performer     string    `json:"performer"`
	RecordedOn    string    `json:"recorded_on"`
	Instrument    string    `json:"instrument"`
	PerfKey       string    `json:"perf_key"`
	DurationMs    int64     `json:"duration_ms"`
	Notes         string    `json:"notes"`
	Origin        string    `json:"origin"`
	SourceName    string    `json:"source_name"`
	HeaderText    string    `json:"header_text"`
	KBSHA256      string    `json:"kb_sha256"`
	HasMIDISource bool      `json:"has_midi_source"`
	AddedAt       time.Time `json:"added_at"`
	Album         string    `json:"album"`
	Track         int       `json:"track"`

	// FromRecording marks an edited copy of a recording made on this
	// piano: like the recording, it holds what the sensors measured.
	FromRecording bool `json:"from_recording,omitempty"`
	// Corrected says the per-key recording corrections are already in the
	// performance, so they are not applied again when it plays.
	Corrected bool `json:"corrected,omitempty"`

	// Locked lists the fields set by hand or from a catalog (by Update);
	// re-deriving metadata from the files never changes them.
	Locked []string `json:"locked,omitempty"`
	// MetaVersion is the metadata parser that described the piece; older
	// pieces are described again when the library opens.
	MetaVersion int `json:"meta_version"`
}

// metaVersion is the current parser: 2 reads the .kb header text, the newer
// Zenph names and full performer names; 3 to 6 spell out known titles; 7 adds
// albums and tracks implied by names (the Goldberg Variations, "NN-" prefixes).
const metaVersion = 7

func (p *Piece) locked(field string) bool {
	for _, f := range p.Locked {
		if f == field {
			return true
		}
	}
	return false
}

// describe re-derives the metadata of p from its files' names and header,
// leaving locked fields alone. It reports whether anything changed.
func (p *Piece) describe() bool {
	m := Describe(p.SourceName, p.HeaderText)
	if p.Origin == OriginEdited {
		// Its own row in the library, not another rendering of the original.
		m.PerfKey = "edited|" + p.ID
	}
	changed := p.MetaVersion != metaVersion
	for _, f := range []struct {
		name string
		dst  *string
		src  string
	}{
		{"title", &p.Title, m.Title}, {"composer", &p.Composer, m.Composer},
		{"performer", &p.Performer, m.Performer}, {"recorded_on", &p.RecordedOn, m.RecordedOn},
		{"instrument", &p.Instrument, m.Instrument}, {"perf_key", &p.PerfKey, m.PerfKey},
		{"notes", &p.Notes, m.Notes},
	} {
		if !p.locked(f.name) && *f.dst != f.src && (f.src != "" || f.name != "title") {
			*f.dst, changed = f.src, true
		}
	}
	// Album and track only when the name implies them; catalogs set the rest.
	if m.Album != "" && p.Album != m.Album && !p.locked("album") {
		p.Album, changed = m.Album, true
	}
	if m.Track != 0 && p.Track != m.Track && !p.locked("track") {
		p.Track, changed = m.Track, true
	}
	if p.Title == "" {
		p.Title = p.ID
	}
	p.MetaVersion = metaVersion
	return changed
}

// Patch changes metadata; nil fields are left alone. Every field it sets is
// locked against automatic re-description.
type Patch struct {
	Title      *string `json:"title"`
	Composer   *string `json:"composer"`
	Performer  *string `json:"performer"`
	RecordedOn *string `json:"recorded_on"`
	Instrument *string `json:"instrument"`
	Notes      *string `json:"notes"`
	Album      *string `json:"album"`
	Track      *int    `json:"track"`
}

// Settings are the service's preferences.
type Settings struct {
	PreferredInstrument string `json:"preferred_instrument"`
	Level               int    `json:"level"`
}

// Filter selects pieces. Empty fields match everything.
type Filter struct {
	Q, Composer, Performer, Instrument string
}

// Library is the store. It is safe for concurrent use.
type Library struct {
	dir      string // the pieces
	settings string // settings file

	mu     sync.RWMutex
	pieces map[string]*Piece
	bySHA  map[string]string
	set    Settings

	plMu sync.Mutex // play lists file
}

// Open opens (creating if needed) a library under dataDir: pieces go in
// dataDir/library, settings in dataDir/settings.json.
func Open(dataDir string) (*Library, error) {
	l := &Library{dir: filepath.Join(dataDir, "library"), settings: filepath.Join(dataDir, "settings.json")}
	if err := os.MkdirAll(l.dir, 0o755); err != nil {
		return nil, err
	}
	l.set = Settings{PreferredInstrument: DefaultInstrument}
	if b, err := os.ReadFile(l.settings); err == nil {
		if err := json.Unmarshal(b, &l.set); err != nil {
			return nil, fmt.Errorf("%s: %w", l.settings, err)
		}
	}
	if _, err := l.Rescan(); err != nil {
		return nil, err
	}
	return l, nil
}

// Dir is the folder holding the pieces' files.
func (l *Library) Dir() string { return l.dir }

// Rescan rebuilds the index from the folder. Performance files without
// metadata (copied in by hand, or left by a power cut before the metadata was
// written) get metadata derived from their contents.
func (l *Library) Rescan() (int, error) {
	pieces := map[string]*Piece{}
	bySHA := map[string]string{}
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return 0, err
	}
	have := map[string]bool{}
	for _, e := range entries {
		have[e.Name()] = true
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		id, ext := strings.TrimSuffix(name, filepath.Ext(name)), strings.ToLower(filepath.Ext(name))
		switch {
		case ext == ".json":
			if !have[id+".kb"] {
				continue // metadata left over from a delete cut short
			}
			b, err := os.ReadFile(filepath.Join(l.dir, name))
			if err != nil {
				return 0, err
			}
			var p Piece
			if err := json.Unmarshal(b, &p); err != nil || p.ID != id {
				p = Piece{} // damaged: rebuilt below
				if built, err := l.describe(id, id, OriginImport); err == nil {
					p = *built
					l.writeMeta(&p)
				} else {
					continue
				}
			}
			pieces[id] = &p
		case ext == ".kb" && !have[id+".json"]:
			if !have[id+".lp"] || !have[id+".sp"] {
				continue
			}
			source := id
			if !isID(id) { // copied in under its own name: give it an id
				nid := newID(func(s string) bool { return pieces[s] != nil || have[s+".kb"] })
				for _, x := range []string{".kb", ".lp", ".sp", ".mid"} {
					if have[id+x] {
						if err := os.Rename(filepath.Join(l.dir, id+x), filepath.Join(l.dir, nid+x)); err != nil {
							return 0, err
						}
					}
				}
				have[nid+".kb"] = true
				id = nid
			}
			p, err := l.describe(id, source, OriginImport)
			if err != nil {
				continue
			}
			if err := l.writeMeta(p); err != nil {
				return 0, err
			}
			pieces[id] = p
		}
	}
	// Describe again the pieces an older parser described, then share the
	// full performer names around.
	dirty := map[*Piece]bool{}
	for _, p := range pieces {
		if p.MetaVersion < metaVersion && p.describe() {
			dirty[p] = true
		}
	}
	for _, p := range fullNames(pieces) {
		dirty[p] = true
	}
	for p := range dirty {
		if err := l.writeMeta(p); err != nil {
			return 0, err
		}
	}
	for id, p := range pieces {
		bySHA[p.KBSHA256] = id
	}
	l.mu.Lock()
	l.pieces, l.bySHA = pieces, bySHA
	l.mu.Unlock()
	return len(pieces), nil
}

// describe builds metadata for the files dir/id.* named after sourceName.
func (l *Library) describe(id, sourceName, origin string) (*Piece, error) {
	kb, err := os.ReadFile(filepath.Join(l.dir, id+".kb"))
	if err != nil {
		return nil, err
	}
	k, err := se.ParseKB(kb)
	if err != nil {
		return nil, err
	}
	p := newPiece(id, sourceName, origin, kb, k)
	if fi, err := os.Stat(filepath.Join(l.dir, id+".kb")); err == nil {
		p.AddedAt = fi.ModTime()
	}
	_, err = os.Stat(filepath.Join(l.dir, id+".mid"))
	p.HasMIDISource = err == nil
	return p, nil
}

func newPiece(id, sourceName, origin string, kb []byte, k *se.KB) *Piece {
	sum := sha256.Sum256(kb)
	p := &Piece{
		ID: id, DurationMs: se.TickDuration(k.End).Milliseconds(), Origin: origin,
		SourceName: sourceName, HeaderText: strings.TrimSpace(k.Text),
		KBSHA256: hex.EncodeToString(sum[:]), AddedAt: time.Now().UTC().Truncate(time.Second),
	}
	p.describe()
	return p
}

// fullNames learns full performer names from the pieces whose headers spell
// them out ("Earl Wild, pianist") and gives them to pieces that only have
// the surname, as the Zenph names do. Ambiguous surnames are left alone. It
// returns the pieces it changed.
func fullNames(pieces map[string]*Piece) []*Piece {
	full := map[string]string{}
	for _, p := range pieces {
		f := strings.Fields(p.Performer)
		if len(f) < 2 || strings.ContainsAny(p.Performer, "&,") || strings.Contains(p.Performer, " and ") {
			continue
		}
		last := f[len(f)-1]
		if prev, ok := full[last]; ok && prev != p.Performer {
			full[last] = "" // two people share the surname
			continue
		}
		full[last] = p.Performer
	}
	var changed []*Piece
	for _, p := range pieces {
		if f := full[p.Performer]; f != "" && !p.locked("performer") {
			p.Performer = f
			changed = append(changed, p)
		}
	}
	return changed
}

func (l *Library) writeMeta(p *Piece) error {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(l.dir, p.ID+".json"), append(b, '\n'))
}

// writeAtomic replaces path with data so that a power cut leaves either the
// old file or the new one.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if d, err := os.Open(dir); err == nil { // make the rename itself durable
		d.Sync()
		d.Close()
	}
	return nil
}

const idAlphabet = "abcdefghijkmnpqrstuvwxyz23456789" // no l, o, 0, 1

func isID(s string) bool {
	if len(s) != 8 {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune(idAlphabet, c) {
			return false
		}
	}
	return true
}

func newID(taken func(string) bool) string {
	const alphabet = idAlphabet
	for {
		b := make([]byte, 8)
		rand.Read(b)
		for i := range b {
			b[i] = alphabet[int(b[i])%len(alphabet)]
		}
		if id := string(b); !taken(id) {
			return id
		}
	}
}

// List returns the pieces matching f, sorted by composer, title and instrument.
func (l *Library) List(f Filter) []Piece {
	words := strings.Fields(strings.ToLower(f.Q))
	l.mu.RLock()
	out := make([]Piece, 0, len(l.pieces))
	for _, p := range l.pieces {
		if f.Composer != "" && !strings.EqualFold(p.Composer, f.Composer) ||
			f.Performer != "" && !strings.EqualFold(p.Performer, f.Performer) ||
			f.Instrument != "" && !strings.EqualFold(p.Instrument, f.Instrument) {
			continue
		}
		text := strings.ToLower(strings.Join([]string{p.Title, p.Composer, p.Performer, p.Album, p.Notes,
			p.SourceName, p.Instrument, p.RecordedOn, p.HeaderText}, " "))
		match := true
		for _, w := range words {
			if !strings.Contains(text, w) {
				match = false
				break
			}
		}
		if match {
			out = append(out, *p)
		}
	}
	l.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if c := strings.Compare(strings.ToLower(a.Composer), strings.ToLower(b.Composer)); c != 0 {
			return c < 0
		}
		if c := strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title)); c != 0 {
			return c < 0
		}
		if a.Instrument != b.Instrument {
			return a.Instrument < b.Instrument
		}
		return a.ID < b.ID
	})
	return out
}

// Get returns one piece.
func (l *Library) Get(id string) (Piece, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	p, ok := l.pieces[id]
	if !ok {
		return Piece{}, ErrNotFound
	}
	return *p, nil
}

// Files returns the folder and base name of a piece's performance files.
func (l *Library) Files(id string) (dir, base string, err error) {
	if _, err := l.Get(id); err != nil {
		return "", "", err
	}
	return l.dir, id, nil
}

// Update changes a piece's metadata.
func (l *Library) Update(id string, patch Patch) (Piece, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	p, ok := l.pieces[id]
	if !ok {
		return Piece{}, ErrNotFound
	}
	q := *p
	q.Locked = append([]string(nil), p.Locked...)
	lock := func(name string) {
		if !q.locked(name) {
			q.Locked = append(q.Locked, name)
		}
	}
	for _, f := range []struct {
		name string
		src  *string
		dst  *string
	}{{"title", patch.Title, &q.Title}, {"composer", patch.Composer, &q.Composer},
		{"performer", patch.Performer, &q.Performer}, {"recorded_on", patch.RecordedOn, &q.RecordedOn},
		{"instrument", patch.Instrument, &q.Instrument}, {"notes", patch.Notes, &q.Notes},
		{"album", patch.Album, &q.Album}} {
		if f.src != nil {
			*f.dst = strings.TrimSpace(*f.src)
			lock(f.name)
		}
	}
	if patch.Track != nil {
		if *patch.Track < 0 {
			return Piece{}, errors.New("the track number cannot be negative")
		}
		q.Track = *patch.Track
		lock("track")
	}
	if q.Title == "" {
		return Piece{}, errors.New("the title cannot be empty")
	}
	if err := l.writeMeta(&q); err != nil {
		return Piece{}, err
	}
	*p = q
	return q, nil
}

// Delete removes a piece and its files.
func (l *Library) Delete(id string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	p, ok := l.pieces[id]
	if !ok {
		return ErrNotFound
	}
	// The performance first: without its .kb a piece no longer exists, even
	// if the power fails half way.
	for _, ext := range []string{".kb", ".lp", ".sp", ".mid", ".json"} {
		if err := os.Remove(filepath.Join(l.dir, id+ext)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	delete(l.bySHA, p.KBSHA256)
	delete(l.pieces, id)
	return nil
}

// Settings returns the preferences.
func (l *Library) Settings() Settings {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.set
}

// SaveSettings stores the preferences.
func (l *Library) SaveSettings(s Settings) error {
	if s.Level > 0 {
		return errors.New("the level cannot be above 0")
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := writeAtomic(l.settings, append(b, '\n')); err != nil {
		return err
	}
	l.set = s
	return nil
}

// RecordedHere reports whether the piece holds what this piano's sensors
// measured: a recording made on it, or an edited copy of one.
func (p Piece) RecordedHere() bool { return p.Origin == OriginRecorded || p.FromRecording }

// NeedsCorrection reports whether the per-key recording corrections apply
// to the piece: recorded here, and not corrected yet.
func (p Piece) NeedsCorrection() bool { return p.RecordedHere() && !p.Corrected }
