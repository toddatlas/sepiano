package library

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"sepiano/se"
)

// File is one uploaded or imported file.
type File struct {
	Name string // file name; only the base name and extension matter
	Data []byte
}

// Result reports what happened to a batch of files.
type Result struct {
	Added      []Piece     `json:"added"`
	Duplicates []Piece     `json:"duplicates"`
	Errors     []ItemError `json:"errors"`
}

// ItemError is a file or set that could not be added.
type ItemError struct {
	Name  string `json:"name"`
	Error string `json:"error"`
}

func (r *Result) fail(name string, err error) {
	r.Errors = append(r.Errors, ItemError{name, err.Error()})
}

// Limits on what one upload may contain, so a bad zip can't fill the card.
const (
	maxFile  = 64 << 20
	maxTotal = 512 << 20
)

// AddSet adds a performance from the contents of its three files. If the
// library already holds the same .kb, that piece is returned with dup set.
func (l *Library) AddSet(sourceName string, kb, lp, sp []byte, origin, title string) (p Piece, dup bool, err error) {
	k, err := se.ParseKB(kb)
	if err != nil {
		return Piece{}, false, fmt.Errorf(".kb: %w", err)
	}
	damper, err := se.ParsePedal(lp)
	if err != nil {
		return Piece{}, false, fmt.Errorf(".lp: %w", err)
	}
	if _, err := se.ParsePedal(sp); err != nil {
		return Piece{}, false, fmt.Errorf(".sp: %w", err)
	}
	sum := sha256.Sum256(kb)
	l.mu.Lock()
	defer l.mu.Unlock()
	if id, ok := l.bySHA[hex.EncodeToString(sum[:])]; ok {
		return *l.pieces[id], true, nil
	}
	id := newID(func(s string) bool {
		_, err := os.Stat(filepath.Join(l.dir, s+".kb"))
		return l.pieces[s] != nil || err == nil
	})
	piece := newPiece(id, sourceName, origin, kb, k)
	piece.Grade, piece.GradeWhy = gradeOf(origin, k, damper)
	if title = strings.TrimSpace(title); title != "" {
		piece.Title = title
	}
	for _, f := range []struct {
		ext string
		b   []byte
	}{{".kb", kb}, {".lp", lp}, {".sp", sp}} {
		if err := writeAtomic(filepath.Join(l.dir, id+f.ext), f.b); err != nil {
			return Piece{}, false, err
		}
	}
	if err := l.writeMeta(piece); err != nil {
		return Piece{}, false, err
	}
	l.pieces[id] = piece
	l.bySHA[piece.KBSHA256] = id
	return *piece, false, nil
}

// AddMIDI converts a Standard MIDI File and adds it, keeping the original.
func (l *Library) AddMIDI(fileName string, data []byte, title string) (Piece, bool, error) {
	base := strings.TrimSuffix(path.Base(filepath.ToSlash(fileName)), path.Ext(fileName))
	perf, err := se.FromMIDI(data, nil, time.Second, "Converted from "+path.Base(filepath.ToSlash(fileName))+"\r\n")
	if err != nil {
		return Piece{}, false, err
	}
	kb, lp, sp, err := perf.Encode()
	if err != nil {
		return Piece{}, false, err
	}
	p, dup, err := l.AddSet(base, kb, lp, sp, OriginMIDI, title)
	if err != nil || dup {
		return p, dup, err
	}
	if err := writeAtomic(filepath.Join(l.dir, p.ID+".mid"), data); err != nil {
		return p, false, err
	}
	l.mu.Lock()
	l.pieces[p.ID].HasMIDISource = true
	q := *l.pieces[p.ID]
	err = l.writeMeta(&q)
	l.mu.Unlock()
	return q, false, err
}

// AddFiles adds a batch: .kb/.lp/.sp sets (grouped by base name), .zip files
// of sets and MIDI files. title, if set, names a batch holding one piece.
func (l *Library) AddFiles(files []File, origin, title string) Result {
	var r Result
	sets := map[string]map[string][]byte{} // base name -> ext -> data
	var mids []File
	total := 0
	var add func(f File, depth int)
	add = func(f File, depth int) {
		full := filepath.ToSlash(f.Name)
		name := path.Base(full)
		ext := strings.ToLower(path.Ext(name))
		base := strings.TrimSuffix(full, path.Ext(full)) // sets in different folders stay apart
		total += len(f.Data)
		switch {
		case total > maxTotal:
			r.fail(name, errors.New("upload too large"))
		case ext == ".kb" || ext == ".lp" || ext == ".sp":
			if sets[base] == nil {
				sets[base] = map[string][]byte{}
			}
			sets[base][ext] = f.Data
		case ext == ".mid" || ext == ".midi":
			mids = append(mids, f)
		case ext == ".zip" && depth == 0:
			zr, err := zip.NewReader(bytes.NewReader(f.Data), int64(len(f.Data)))
			if err != nil {
				r.fail(name, err)
				return
			}
			for _, zf := range zr.File {
				if zf.FileInfo().IsDir() || strings.HasPrefix(path.Base(zf.Name), ".") || strings.Contains(zf.Name, "__MACOSX") {
					continue
				}
				if zf.UncompressedSize64 > maxFile {
					r.fail(zf.Name, errors.New("file too large"))
					continue
				}
				rc, err := zf.Open()
				if err != nil {
					r.fail(zf.Name, err)
					continue
				}
				b, err := io.ReadAll(io.LimitReader(rc, maxFile+1))
				rc.Close()
				if err != nil {
					r.fail(zf.Name, err)
					continue
				}
				add(File{Name: zf.Name, Data: b}, depth+1)
			}
		case strings.HasPrefix(name, "."), ext == ".bat", ext == ".txt", ext == ".json":
			// ignored companions
		default:
			r.fail(name, errors.New("not a performance (.kb/.lp/.sp), .zip or MIDI file"))
		}
	}
	for _, f := range files {
		add(f, 0)
	}
	one := len(sets)+len(mids) == 1
	if !one {
		title = ""
	}
	names := make([]string, 0, len(sets))
	for n := range sets {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		s := sets[n]
		var missing []string
		for _, ext := range []string{".kb", ".lp", ".sp"} {
			if s[ext] == nil {
				missing = append(missing, ext)
			}
		}
		if len(missing) > 0 {
			r.fail(path.Base(n), fmt.Errorf("missing %s (a performance needs .kb, .lp and .sp)", strings.Join(missing, ", ")))
			continue
		}
		p, dup, err := l.AddSet(path.Base(n), s[".kb"], s[".lp"], s[".sp"], origin, title)
		r.record(path.Base(n), p, dup, err)
	}
	for _, m := range mids {
		p, dup, err := l.AddMIDI(m.Name, m.Data, title)
		r.record(path.Base(m.Name), p, dup, err)
	}
	return r
}

func (r *Result) record(name string, p Piece, dup bool, err error) {
	switch {
	case err != nil:
		r.fail(name, err)
	case dup:
		r.Duplicates = append(r.Duplicates, p)
	default:
		r.Added = append(r.Added, p)
	}
}

// ImportDir adds every performance set found under dir.
func (l *Library) ImportDir(dir string) (Result, error) {
	var files []File
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".kb", ".lp", ".sp":
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			// Keep sets from different folders apart.
			rel, _ := filepath.Rel(dir, p)
			files = append(files, File{Name: rel, Data: b})
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	return l.AddFiles(files, OriginImport, ""), nil
}

// AddRecording imports a recording made on the piano.
func (l *Library) AddRecording(dir, name, title string) (Piece, error) {
	read := func(ext string) ([]byte, error) { return os.ReadFile(filepath.Join(dir, name+ext)) }
	kb, err := read(".kb")
	if err != nil {
		return Piece{}, err
	}
	lp, err := read(".lp")
	if err != nil {
		return Piece{}, err
	}
	sp, err := read(".sp")
	if err != nil {
		return Piece{}, err
	}
	p, _, err := l.AddSet(name, kb, lp, sp, OriginRecorded, title)
	if err != nil {
		return Piece{}, err
	}
	today := time.Now().Format("2006-01-02")
	return l.Update(p.ID, Patch{RecordedOn: &today})
}

// WriteSet writes a piece's performance files as a zip, under its original name.
func (l *Library) WriteSet(w io.Writer, id string) error {
	p, err := l.Get(id)
	if err != nil {
		return err
	}
	name := safeName(p.SourceName, p.ID)
	zw := zip.NewWriter(w)
	for _, ext := range []string{".kb", ".lp", ".sp"} {
		if err := addZip(zw, name+ext, filepath.Join(l.dir, id+ext)); err != nil {
			return err
		}
	}
	return zw.Close()
}

// MIDI renders a piece as a Standard MIDI File.
func (l *Library) MIDI(id string) ([]byte, error) {
	if _, err := l.Get(id); err != nil {
		return nil, err
	}
	perf, err := se.Load(filepath.Join(l.dir, id))
	if err != nil {
		return nil, err
	}
	return perf.ToMIDI(nil, false)
}

// DownloadName is a file name for a piece's downloads.
func (l *Library) DownloadName(id string) string {
	p, err := l.Get(id)
	if err != nil {
		return id
	}
	return safeName(p.SourceName, p.ID)
}

// Export writes the whole library (performances, metadata, settings) as a zip.
func (l *Library) Export(w io.Writer) error {
	zw := zip.NewWriter(w)
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if err := addZip(zw, "sepiano-library/library/"+e.Name(), filepath.Join(l.dir, e.Name())); err != nil {
			return err
		}
	}
	if _, err := os.Stat(l.settings); err == nil {
		if err := addZip(zw, "sepiano-library/settings.json", l.settings); err != nil {
			return err
		}
	}
	return zw.Close()
}

func addZip(zw *zip.Writer, name, file string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	h, err := zip.FileInfoHeader(fi)
	if err != nil {
		return err
	}
	h.Name, h.Method = name, zip.Deflate
	zf, err := zw.CreateHeader(h)
	if err != nil {
		return err
	}
	_, err = io.Copy(zf, f)
	return err
}

// safeName keeps a download name to characters every filesystem accepts.
func safeName(s, fallback string) string {
	s = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, strings.TrimSpace(s))
	if s == "" || s == "." || s == ".." {
		return fallback
	}
	return s
}
