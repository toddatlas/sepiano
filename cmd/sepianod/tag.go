package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"sepiano/library"
)

// tagCmd fills in album, track, title, composer and performer for pieces on
// a running sepianod from the album batch files that catalogue them
// (e.g. WAYNE\FP1002\_FP1002.BAT), and expands the abbreviated titles in
// Zenph file names ("STWOM") from titles the library already knows. Pieces
// are matched by content (the .kb file's hash), so any copy of a file finds
// its piece. Fields edited by hand are never changed.
func tagCmd(args []string) int {
	fl := flag.NewFlagSet("sepianod tag", flag.ExitOnError)
	server := fl.String("server", "http://127.0.0.1", "running sepianod to update")
	dry := fl.Bool("n", false, "dry run: print the changes without making them")
	fl.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: sepianod tag [-server URL] [-n] FOLDER...")
		fl.PrintDefaults()
	}
	fl.Parse(args)
	if fl.NArg() == 0 {
		fl.Usage()
		return 2
	}
	base := strings.TrimRight(*server, "/") + "/api/v1"
	pieces, err := fetchPieces(base)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	bySHA := map[string]*library.Piece{}
	for i := range pieces {
		bySHA[pieces[i].KBSHA256] = &pieces[i]
	}

	patches := map[string]*library.Patch{}
	var albums []library.Album
	for _, dir := range fl.Args() {
		a, err := catalogPatches(dir, bySHA, patches)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		albums = append(albums, a...)
	}
	// Apply the catalog's changes locally, so the abbreviation pass sees them.
	for _, p := range pieces {
		if pt := patches[p.ID]; pt != nil {
			apply(bySHA[p.KBSHA256], pt)
		}
	}
	sameName(pieces, patches)
	expandAbbreviations(pieces, albums, patches)

	ids := make([]string, 0, len(patches))
	for id := range patches {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	byID := map[string]*library.Piece{}
	for i := range pieces {
		byID[pieces[i].ID] = &pieces[i]
	}
	failed := 0
	for _, id := range ids {
		pt := patches[id]
		fmt.Printf("%s  %-40.40s  %s\n", id, byID[id].SourceName, describePatch(pt))
		if *dry {
			continue
		}
		if err := sendPatch(base, id, pt); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", id, err)
			failed++
		}
	}
	var abbrevs []string
	for _, p := range pieces {
		if abbrevRE.MatchString(p.Title) {
			abbrevs = append(abbrevs, p.Title+" ("+p.Performer+")")
		}
	}
	sort.Strings(abbrevs)
	fmt.Printf("\n%d albums, %d pieces updated", len(albums), len(ids))
	if *dry {
		fmt.Print(" (dry run: nothing sent)")
	}
	fmt.Printf("\n%d titles still abbreviated: %s\n", len(abbrevs), strings.Join(dedupe(abbrevs), ", "))
	if failed > 0 {
		return 1
	}
	return 0
}

func fetchPieces(base string) ([]library.Piece, error) {
	resp, err := http.Get(base + "/pieces")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, errors.New("listing pieces: " + resp.Status)
	}
	var r struct{ Pieces []library.Piece }
	return r.Pieces, json.NewDecoder(resp.Body).Decode(&r)
}

// catalogPatches walks dir, finds the album batch file of each folder (or of
// the nearest folder above it), and plans the changes for the pieces whose
// files the album lists.
func catalogPatches(dir string, bySHA map[string]*library.Piece, patches map[string]*library.Patch) ([]library.Album, error) {
	albumOf := map[string]*library.Album{}
	var found []library.Album
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		albumOf[path] = albumOf[filepath.Dir(path)] // inherit from the folder above
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if strings.EqualFold(filepath.Ext(e.Name()), ".bat") {
				b, err := os.ReadFile(filepath.Join(path, e.Name()))
				if err != nil {
					return err
				}
				if a, ok := library.ParseAlbumBat(string(b)); ok {
					albumOf[path] = &a
					found = append(found, a)
					break
				}
			}
		}
		a := albumOf[path]
		names := nameList(path, entries)
		if a == nil && len(names) == 0 {
			return nil
		}
		for _, e := range entries {
			name := e.Name()
			if !strings.EqualFold(filepath.Ext(name), ".kb") {
				continue
			}
			base := strings.TrimSuffix(name, filepath.Ext(name))
			var tr library.Track
			ok := false
			if a != nil {
				tr, ok = a.TrackFor(base)
			}
			listed, inList := names[strings.ToUpper(base)]
			albumPerformer := a != nil && a.Performer != ""
			if !ok && !inList && !albumPerformer {
				continue
			}
			kb, err := os.ReadFile(filepath.Join(path, name))
			if err != nil {
				return err
			}
			sum := sha256.Sum256(kb)
			p := bySHA[hex.EncodeToString(sum[:])]
			if p == nil || patches[p.ID] != nil {
				continue // not in the library, or already planned
			}
			var pt *library.Patch
			if ok {
				pt = catalogPatch(p, *a, tr)
			} else if albumPerformer && p.Performer == "" && library.ParseHeader(p.HeaderText).Performer == "" && !locked(p, "performer") {
				// In the album's folder but not on its track list: the same pianist.
				v := a.Performer
				pt = &library.Patch{Performer: &v}
			}
			if inList && library.ParseHeader(p.HeaderText).Title == "" && !locked(p, "title") && p.Title != listed[0] {
				if pt == nil {
					pt = &library.Patch{}
				}
				t := listed[0]
				pt.Title = &t
				if d := library.ParseDate(listed[1]); d != "" && !locked(p, "recorded_on") {
					pt.RecordedOn = &d
				}
			}
			if pt != nil {
				patches[p.ID] = pt
			}
		}
		return nil
	})
	return found, err
}

// catalogPatch plans the album and track, and the title, composer and
// performer when the file's own header doesn't give them.
func catalogPatch(p *library.Piece, a library.Album, tr library.Track) *library.Patch {
	var pt library.Patch
	changed := false
	set := func(field string, dst **string, cur, val string) {
		if val != "" && val != cur && !locked(p, field) {
			v := val
			*dst = &v
			changed = true
		}
	}
	h := library.ParseHeader(p.HeaderText)
	set("album", &pt.Album, p.Album, a.Name())
	if tr.N > 0 && tr.N != p.Track && !locked(p, "track") {
		n := tr.N
		pt.Track, changed = &n, true
	}
	if h.Title == "" {
		set("title", &pt.Title, p.Title, tr.Title)
	}
	if h.Composer == "" {
		set("composer", &pt.Composer, p.Composer, tr.Composer)
	}
	if h.Performer == "" {
		set("performer", &pt.Performer, p.Performer, a.Performer)
	}
	if !changed {
		return nil
	}
	return &pt
}

var abbrevRE = regexp.MustCompile(`^[A-Z]{2,8}$`)

// expandAbbreviations replaces titles like "STWOM" (from Zenph names) with a
// title by the same performer whose initials match: "Someone to Watch over
// Me". Only unambiguous matches are used.
func expandAbbreviations(pieces []library.Piece, albums []library.Album, patches map[string]*library.Patch) {
	known := map[string]map[string]bool{} // performer → initials → titles
	add := func(performer, title string) {
		if performer == "" || title == "" || abbrevRE.MatchString(title) {
			return
		}
		key := strings.ToLower(performer)
		if known[key] == nil {
			known[key] = map[string]bool{}
		}
		known[key][title] = true
	}
	for _, p := range pieces {
		add(p.Performer, p.Title)
	}
	for _, a := range albums {
		for _, t := range a.Tracks {
			add(a.Performer, t.Title)
		}
	}
	// The same titles by composer, for pieces whose performer has no match:
	// Gershwin's own "TMIL" is the "The Man I Love" Peterson recorded.
	byComposer := map[string][]string{}
	for _, p := range pieces {
		if p.Composer != "" && p.Title != "" && !abbrevRE.MatchString(p.Title) {
			byComposer[p.Title] = append(byComposer[p.Title], p.Composer)
		}
	}
	matching := func(abbr string, titles []string) []string {
		var match []string
		for _, title := range titles {
			last := title
			if i := strings.LastIndex(title, " — "); i >= 0 {
				last = title[i+len(" — "):]
			}
			if library.Initials(title) == abbr || library.Initials(last) == abbr {
				match = append(match, title)
			}
		}
		return dedupeFold(match)
	}
	for i := range pieces {
		p := &pieces[i]
		if !abbrevRE.MatchString(p.Title) || locked(p, "title") {
			continue
		}
		var titles []string
		for t := range known[strings.ToLower(p.Performer)] {
			titles = append(titles, t)
		}
		match := matching(p.Title, titles)
		if len(match) == 0 && p.Composer != "" {
			titles = titles[:0]
			for t, composers := range byComposer {
				for _, c := range composers {
					if sameComposer(p.Composer, c) {
						titles = append(titles, t)
						break
					}
				}
			}
			match = matching(p.Title, titles)
		}
		if len(match) != 1 {
			continue
		}
		t := match[0]
		if patches[p.ID] == nil {
			patches[p.ID] = &library.Patch{}
		}
		patches[p.ID].Title = &t
		p.Title = t
	}
}

// untitled reports whether a piece's title is just its 8.3 file name.
func untitled(p *library.Piece) bool {
	return len(p.SourceName) <= 8 && strings.EqualFold(strings.Trim(p.Title, "_@ "), strings.Trim(p.SourceName, "_@ "))
}

// sameName gives a piece still titled by its 8.3 file name (OLDFOLKS) the
// title, composer, performer and date found for another rendering with the
// same file name.
func sameName(pieces []library.Piece, patches map[string]*library.Patch) {
	known := map[string]library.Piece{}
	for _, p := range pieces {
		if len(p.SourceName) <= 8 && !untitled(&p) && p.Title != "" {
			known[strings.ToUpper(p.SourceName)] = p
		}
	}
	for i := range pieces {
		p := &pieces[i]
		src, ok := known[strings.ToUpper(p.SourceName)]
		if !ok || !untitled(p) || locked(p, "title") {
			continue
		}
		pt := patches[p.ID]
		if pt == nil {
			pt = &library.Patch{}
			patches[p.ID] = pt
		}
		for _, f := range []struct {
			name     string
			dst      **string
			cur, val string
		}{{"title", &pt.Title, p.Title, src.Title}, {"composer", &pt.Composer, p.Composer, src.Composer},
			{"performer", &pt.Performer, p.Performer, src.Performer}, {"recorded_on", &pt.RecordedOn, p.RecordedOn, src.RecordedOn}} {
			if f.val != "" && (f.cur == "" || f.name == "title") && !locked(p, f.name) {
				v := f.val
				*f.dst = &v
			}
		}
		apply(p, pt)
	}
}

// sameComposer reports whether every name in a ("Gershwin") appears in b
// ("George Gershwin-Ira Gershwin").
func sameComposer(a, b string) bool {
	words := func(s string) []string {
		return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) })
	}
	in := map[string]bool{}
	for _, w := range words(b) {
		in[w] = true
	}
	wa := words(a)
	for _, w := range wa {
		if !in[w] {
			return false
		}
	}
	return len(wa) > 0
}

// nameListRE matches the lines of a simple list such as NAMES.TXT:
// "Old Folks – February 19, 1988".
var nameListRE = regexp.MustCompile(`^\s*(.+?)\s+[-–—\x{FFFD}]\s+(.+?)\s*$`)

// nameList reads the .txt files in a folder that list titles (and dates),
// keyed by the 8.3 file name they would have: "Old Folks" → OLDFOLKS.
func nameList(dir string, entries []fs.DirEntry) map[string][2]string {
	out := map[string][2]string{}
	for _, e := range entries {
		if !strings.EqualFold(filepath.Ext(e.Name()), ".txt") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil || len(b) > 64<<10 || bytes.Contains(b, []byte("Catalog No")) {
			continue // unreadable, or a full catalog (the .bat files say the same)
		}
		b = bytes.ReplaceAll(b, []byte{0x96}, []byte("-")) // Windows-1252 en dash
		for _, line := range strings.Split(string(b), "\n") {
			m := nameListRE.FindStringSubmatch(strings.TrimSpace(line))
			if m == nil {
				continue
			}
			key := strings.Map(func(r rune) rune {
				if unicode.IsLetter(r) || unicode.IsDigit(r) {
					return unicode.ToUpper(r)
				}
				return -1
			}, m[1])
			if len(key) > 8 {
				key = key[:8]
			}
			out[key] = [2]string{m[1], strings.TrimSuffix(m[2], "?")}
		}
	}
	return out
}

func apply(p *library.Piece, pt *library.Patch) {
	for _, f := range []struct {
		src *string
		dst *string
	}{{pt.Title, &p.Title}, {pt.Composer, &p.Composer}, {pt.Performer, &p.Performer}, {pt.Album, &p.Album}, {pt.RecordedOn, &p.RecordedOn}} {
		if f.src != nil {
			*f.dst = *f.src
		}
	}
	if pt.Track != nil {
		p.Track = *pt.Track
	}
}

func locked(p *library.Piece, field string) bool {
	for _, f := range p.Locked {
		if f == field {
			return true
		}
	}
	return false
}

func describePatch(pt *library.Patch) string {
	var parts []string
	for _, f := range []struct {
		name string
		v    *string
	}{{"title", pt.Title}, {"composer", pt.Composer}, {"performer", pt.Performer}, {"album", pt.Album}, {"recorded_on", pt.RecordedOn}} {
		if f.v != nil {
			parts = append(parts, fmt.Sprintf("%s=%q", f.name, *f.v))
		}
	}
	if pt.Track != nil {
		parts = append(parts, fmt.Sprintf("track=%d", *pt.Track))
	}
	return strings.Join(parts, " ")
}

func sendPatch(base, id string, pt *library.Patch) error {
	b, _ := json.Marshal(pt)
	req, _ := http.NewRequest(http.MethodPatch, base+"/pieces/"+id, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func dedupe(s []string) []string {
	var out []string
	for i, v := range s {
		if i == 0 || v != s[i-1] {
			out = append(out, v)
		}
	}
	return out
}

func dedupeFold(s []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range s {
		if k := strings.ToLower(v); !seen[k] {
			seen[k] = true
			out = append(out, v)
		}
	}
	return out
}
