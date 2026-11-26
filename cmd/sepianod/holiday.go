package main

// sepianod holiday: the holiday collection the owner asked for (2026-11-26):
// Christmas, Nutcracker and winter pieces gathered from the downloaded
// sources by the other session (DATASETS/holiday/holiday_catalog.csv). Each
// distinct piece is added and starred; one already in the library (the
// MAESTRO and PiJAMA rows) keeps its details and is only starred. Then a
// starred play list per category, and one with everything. Running it again
// adds nothing twice and refreshes the play lists.
//
// Organ rolls (176-note) are left out: written for a pipe organ, they hold
// many keys down for a long time, which this vintage action should not be
// asked to do without the owner's say.

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"sepiano/library"
)

type holidayRow struct {
	category, source, grade, title, composer, performer, file, note string
}

func holidayCmd(args []string) int {
	fl := flag.NewFlagSet("sepianod holiday", flag.ExitOnError)
	server := fl.String("server", "http://127.0.0.1", "running sepianod to import into")
	dry := fl.Bool("n", false, "list what would be imported, and import nothing")
	fl.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: sepianod holiday [-server URL] [-n] DATASETS")
		fmt.Fprintln(os.Stderr, "DATASETS holds holiday/holiday_catalog.csv and downloads/ (docs/LIBRARIES.md).")
		fl.PrintDefaults()
	}
	fl.Parse(args)
	if fl.NArg() != 1 {
		fl.Usage()
		return 2
	}
	dir := fl.Arg(0)
	rows, skipped, err := holidayRows(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("holiday: %d pieces (%d organ rolls left out)\n", len(rows), skipped)
	items := make([]dsItem, 0, len(rows))
	for _, r := range rows {
		it, err := holidayItem(dir, r)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", r.title, err)
			return 1
		}
		items = append(items, it)
	}
	if *dry {
		for i, it := range items {
			composer, _ := it.meta["composer"].(string)
			fmt.Printf("  %-10s %-20s %s | %v | %v\n", rows[i].category, rows[i].source, composer, it.meta["title"], it.meta["grade"])
		}
		return 0
	}
	base := strings.TrimRight(*server, "/") + "/api/v1"
	lists := map[string][]string{}
	added, starred, failed := 0, 0, 0
	for i, it := range items {
		id, dup, err := holidayImport(base, it)
		if err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "%s: %v\n", it.label, err)
			continue
		}
		if dup {
			starred++
		} else {
			added++
		}
		cat := holidayList(rows[i].category)
		lists[cat] = append(lists[cat], id)
		lists["Holiday: everything"] = append(lists["Holiday: everything"], id)
	}
	fmt.Printf("%d added, %d already there (starred), %d failed\n", added, starred, failed)
	if err := holidayPlaylists(base, lists); err != nil {
		fmt.Fprintln(os.Stderr, "play lists:", err)
		return 1
	}
	if failed > 0 {
		return 1
	}
	return 0
}

// holidayList: the play list a category goes in.
func holidayList(category string) string {
	if category == "Seasonal favourite" {
		category = "Winter"
	}
	return "Holiday: " + category
}

// holidayRows reads the catalogue: the distinct pieces (duplicate_of
// empty), organ rolls left out, best sources first within each category.
func holidayRows(dir string) ([]holidayRow, int, error) {
	rows, col, err := readCatalogue(filepath.Join(dir, "holiday", "holiday_catalog.csv"),
		"category", "source", "grade", "title", "composer", "performer", "file", "duplicate_of", "note")
	if err != nil {
		return nil, 0, err
	}
	var out []holidayRow
	skipped := 0
	for _, r := range rows {
		if r[col["duplicate_of"]] != "" {
			continue
		}
		h := holidayRow{category: r[col["category"]], source: r[col["source"]], grade: r[col["grade"]], title: oneSpace(r[col["title"]]),
			composer: r[col["composer"]], performer: oneSpace(r[col["performer"]]), file: r[col["file"]], note: r[col["note"]]}
		if strings.Contains(h.grade, "organ") {
			skipped++
			continue
		}
		out = append(out, h)
	}
	order := []string{"Christmas", "Nutcracker", "Winter", "Seasonal favourite"}
	rank := func(h holidayRow) (int, int) {
		c := len(order)
		for i, o := range order {
			if h.category == o {
				c = i
			}
		}
		g := 5
		switch {
		case strings.HasPrefix(h.grade, "performance"):
			g = 0
		case strings.HasPrefix(h.grade, "reproducing roll"):
			g = 1
		case h.grade == "transcribed" || h.grade == "transcribed (jazz)":
			g = 2
		case strings.HasPrefix(h.grade, "transcribed"):
			g = 3
		case strings.HasPrefix(h.grade, "88-note"):
			g = 4
		}
		return c, g
	}
	sort.SliceStable(out, func(i, j int) bool {
		ci, gi := rank(out[i])
		cj, gj := rank(out[j])
		if ci != cj {
			return ci < cj
		}
		if gi != gj {
			return gi < gj
		}
		return out[i].title < out[j].title
	})
	return out, skipped, nil
}

// holidayItem reads a holiday piece's file and describes it.
func holidayItem(dir string, r holidayRow) (dsItem, error) {
	var data []byte
	name := path.Base(r.file)
	if zipPath, entry, ok := strings.Cut(r.file, "::"); ok {
		zr, err := zip.OpenReader(filepath.Join(dir, filepath.FromSlash(zipPath)))
		if err != nil {
			return dsItem{}, err
		}
		defer zr.Close()
		f, err := zr.Open(entry)
		if err != nil {
			return dsItem{}, err
		}
		data, err = io.ReadAll(f)
		f.Close()
		if err != nil {
			return dsItem{}, err
		}
		name = path.Base(entry)
	} else {
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(r.file)))
		if err != nil {
			return dsItem{}, err
		}
		data = b
	}
	composer := surnames(r.composer)
	if composer != "" {
		composer = strings.ToUpper(composer[:1]) + composer[1:]
	}
	meta := map[string]any{"title": r.title, "favorite": true}
	if composer != "" {
		meta["composer"] = composer
	}
	if r.performer != "" {
		meta["performer"] = r.performer
	}
	note := "Holiday collection (" + r.category + ")."
	if r.note != "" {
		note += " " + r.note + "."
	}
	switch r.source {
	case "ATEPP":
		meta["collection"], meta["licence"] = "ATEPP", "CC BY 4.0 (ATEPP, Zhang et al., ISMIR 2022); transcribed from a commercial recording: personal use"
		meta["instrument"], meta["captured_on"] = "Audio transcription", "Transcribed from audio"
	case "Aria-MIDI":
		meta["collection"], meta["licence"] = "Aria-MIDI", "CC BY-NC-SA 4.0 (Aria-MIDI, Bradshaw & Colton, 2025); may contain third-party material: personal use"
		meta["instrument"], meta["captured_on"] = "Audio transcription", "Transcribed from audio"
	case "GiantMIDI":
		meta["collection"], meta["licence"] = "GiantMIDI-Piano", "CC BY 4.0 (GiantMIDI-Piano, Kong et al., 2022); transcribed from YouTube recordings: personal use"
		meta["instrument"], meta["captured_on"] = "Audio transcription", "Transcribed from audio"
	case "Terry Smythe rolls":
		title, roll := smytheTitle(r.title)
		meta["title"] = title
		if roll != "" {
			note += " Roll " + roll + "."
		}
		meta["collection"], meta["licence"] = "Terry Smythe rolls", "Terry Smythe's roll scans (Internet Archive): for personal enjoyment, not commercial use"
		meta["captured_on"] = strings.TrimPrefix(strings.TrimSuffix(r.grade, " (expression in file)"), "reproducing roll: ") + " roll"
		meta["instrument"] = meta["captured_on"]
	}
	switch {
	case strings.HasPrefix(r.grade, "reproducing roll"):
		meta["grade"] = library.GradeRoll
		meta["grade_why"] = "a reproducing piano roll (" + strings.TrimPrefix(strings.TrimSuffix(r.grade, " (expression in file)"), "reproducing roll: ") +
			"), scanned with its expression worked out: the pianist's timing as punched, loudness from the roll's expression code; pedals slowed for this piano as it plays"
	case strings.HasPrefix(r.grade, "88-note"):
		meta["grade"] = library.GradeRoll
		meta["grade_why"] = "an 88-note player-piano roll: notes and timing as punched, at one loudness throughout (such rolls had none of their own)"
		meta["captured_on"], meta["instrument"] = "88-note player-piano roll", "88-note roll"
	case strings.HasPrefix(r.grade, "transcribed"):
		meta["grade"] = library.GradeTranscribed
		meta["grade_why"] = "notes found by software in a recording's audio: the pianist's timing, loudness estimated from the sound, the damper pedal only on or off (slowed for this piano as it plays)"
		if strings.Contains(r.grade, "coarse") {
			meta["grade_why"] = fmt.Sprint(meta["grade_why"]) + "; few loudness levels"
		}
	}
	meta["notes"] = note
	return dsItem{label: r.source + " " + name, files: []namedFile{{name, data}}, curve: "disklavier", meta: meta}, nil
}

// holidayImport uploads a piece. A new one gets its details; one already in
// the library is only starred, keeping what it has.
func holidayImport(base string, it dsItem) (string, bool, error) {
	only := it
	only.meta = map[string]any{"favorite": true}
	id, dup, err := importItem(strings.TrimSuffix(base, "/api/v1"), only)
	if err != nil || dup {
		return id, dup, err
	}
	return id, false, patchPiece(base, id, it.meta)
}

func patchPiece(base, id string, meta map[string]any) error {
	b, _ := json.Marshal(meta)
	req, _ := http.NewRequest("PATCH", base+"/pieces/"+id, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("details: %s: %s", resp.Status, body)
	}
	return nil
}

// holidayPlaylists makes (or refreshes) the play lists and stars them.
func holidayPlaylists(base string, lists map[string][]string) error {
	resp, err := http.Get(base + "/playlists")
	if err != nil {
		return err
	}
	var have struct {
		Playlists []library.PlaylistSummary `json:"playlists"`
	}
	err = json.NewDecoder(resp.Body).Decode(&have)
	resp.Body.Close()
	if err != nil {
		return err
	}
	names := make([]string, 0, len(lists))
	for n := range lists {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		id := ""
		for _, p := range have.Playlists {
			if p.Name == name && p.Source == "" {
				id = p.ID
			}
		}
		method, url := "POST", base+"/playlists"
		if id != "" {
			method, url = "PUT", base+"/playlists/"+id
		}
		b, _ := json.Marshal(map[string]any{"name": name, "pieces": lists[name]})
		req, _ := http.NewRequest(method, url, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 && resp.StatusCode != 201 {
			return fmt.Errorf("%s: %s: %s", name, resp.Status, body)
		}
		var pl struct{ ID string }
		json.Unmarshal(body, &pl)
		if pl.ID == "" {
			return errors.New(name + ": no id in the reply")
		}
		fb, _ := json.Marshal(map[string]any{"kind": "playlist", "id": pl.ID, "favorite": true})
		fr, err := http.Post(base+"/favorites", "application/json", bytes.NewReader(fb))
		if err != nil {
			return err
		}
		fr.Body.Close()
		fmt.Printf("play list %q: %d pieces, starred\n", name, len(lists[name]))
	}
	return nil
}

var rollNumberRe = regexp.MustCompile(`^(.*\S)[ _-]+(\d{3,6} ?[A-Za-z]{0,2})$`)

// smytheTitle tidies a roll's file-name title and splits off its roll
// number: "A_Christmas_Eve_Fantasy-6800A" → "A Christmas Eve Fantasy",
// "6800A"; "Noel - 87 D" → "Noel", "87 D".
func smytheTitle(s string) (title, roll string) {
	s = oneSpace(strings.ReplaceAll(s, "_", " "))
	if i := strings.LastIndex(s, " - "); i > 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+3:])
	}
	if m := rollNumberRe.FindStringSubmatch(s); m != nil {
		return m[1], m[2]
	}
	return s, ""
}
