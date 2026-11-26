package main

// The Piano-e-Competition's original files (Yamaha Disklavier PRO, 2002–
// 2018), for performances MAESTRO does not have (docs/LIBRARIES.md, "The
// competition's original files"). MAESTRO is a stripped subset of these,
// often cut or joined differently, so an original is new only if:
//   - its notes do not open a MAESTRO file (the catalogue's in_maestro);
//   - its competition entry was not matched to a MAESTRO file by title
//     (DOWNLOADS/ecomp/maestro-performers-audit.csv);
//   - none of three 16-note stretches (its start, middle and three
//     quarters in) appears in a MAESTRO file of its year with the same
//     timing, within 15 ms (maestroHas).
// Each new one is named from the competition's own pages
// (DOWNLOADS/ecomp/ecomp-entries.csv): pianist, round, composer, title.
// Their release velocities and key-position aftertouch are left aside: the
// SE format has no place for the first, and the second is not understood.

import (
	"archive/zip"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"sepiano/library"
	"sepiano/smf"
)

type ecompEntry struct {
	performer, nationality, round, composer, title string
}

// ecompOriginalItems: the new originals, in year, pianist and file order.
func ecompOriginalItems(dir string) ([]dsItem, error) {
	base := filepath.Join(dir, "ecompetition")
	rows, col, err := readCatalogue(filepath.Join(base, "catalog.csv"), "year", "file", "in_maestro")
	if err != nil {
		return nil, err
	}
	info, err := loadEcomp(dir)
	if err != nil {
		return nil, fmt.Errorf("ecomp: %w", err)
	}
	if info == nil {
		return nil, fmt.Errorf("%s is needed to name the pianists", filepath.Join(dir, "ecomp"))
	}
	entries := map[string]ecompEntry{} // year|lower-case code
	if er, ec, err := readCatalogue(filepath.Join(dir, "ecomp", "ecomp-entries.csv"), "year", "performer", "nationality", "round", "composer", "title", "code"); err == nil {
		for _, r := range er {
			entries[r[ec["year"]]+"|"+strings.ToLower(r[ec["code"]])] = ecompEntry{ecompSpelling.Replace(oneSpace(r[ec["performer"]])), r[ec["nationality"]],
				oneSpace(r[ec["round"]]), oneSpace(r[ec["composer"]]), oneSpace(r[ec["title"]])}
		}
	} else {
		return nil, err
	}
	titled := map[string]bool{}
	if ar, ac, err := readCatalogue(filepath.Join(dir, "ecomp", "maestro-performers-audit.csv"), "year", "matched_ecomp_file"); err == nil {
		for _, r := range ar {
			if f := r[ac["matched_ecomp_file"]]; f != "" {
				titled[r[ac["year"]]+"|"+strings.ToLower(f)] = true
			}
		}
	}

	type cand struct {
		year, file, code string
		data             []byte
		notes            []timedNote
	}
	var cands []cand
	for _, r := range rows {
		year, file := r[col["year"]], r[col["file"]]
		code := strings.TrimSuffix(path.Base(file), path.Ext(file))
		if r[col["in_maestro"]] != "" || titled[year+"|"+strings.ToLower(code)] {
			continue
		}
		b, err := os.ReadFile(filepath.Join(base, filepath.FromSlash(file)))
		if err != nil {
			return nil, err
		}
		cands = append(cands, cand{year, file, code, b, notesOf(b)})
	}
	has, err := maestroHas(dir)
	if err != nil {
		return nil, err
	}
	var items []dsItem
	for _, c := range cands {
		if has(c.year, c.notes) {
			continue
		}
		e, named := entries[c.year+"|"+strings.ToLower(c.code)]
		if !named {
			e = ecompEntry{performer: asapPlayer(c.code + ".mid"), title: "Piano-e-Competition " + c.year + ", " + c.code}
		}
		album := "Piano-e-Competition " + c.year + " (originals)"
		meta := map[string]any{
			"title": e.title, "performer": e.performer, "recorded_on": c.year,
			"instrument": "Yamaha_Disklavier", "album": album,
			"collection": "Piano-e-Competition originals", "captured_on": "Yamaha Disklavier PRO",
			"licence": "Free downloads from the Piano-e-Competition (from a mirror of its site; no licence stated): personal use",
			"grade":   library.GradePerformance,
			"grade_why": "a competition performance captured on a Yamaha Disklavier PRO (the competition's own file), converted with the Disklavier curve: " +
				"timing as played, loudness within MIDI's 127 steps, pedal depth",
		}
		if comp := surnames(e.composer); comp != "" {
			meta["composer"] = comp
		}
		note := "The Piano-e-Competition's original file " + path.Base(c.file) + " (" + c.year
		if e.round != "" {
			note += ", " + e.round
		}
		note += "), not in MAESTRO."
		if e.nationality != "" {
			note += " " + e.performer + " (" + e.nationality + ")."
		}
		if !named {
			note += " Not in the competition's archived lists: the pianist is guessed from the file name."
		}
		if a, first := info.award(c.year, e.performer); a != "" && named {
			meta["award"] = a
			note += " " + a + "."
			if first {
				meta["favorite"] = true
			}
		}
		meta["notes"] = note
		items = append(items, dsItem{
			label: "ecomp " + c.year + "/" + path.Base(c.file),
			files: []namedFile{{c.year + "-" + path.Base(c.file), c.data}},
			curve: "disklavier",
			meta:  meta,
		})
	}
	// Albums by year, tracks by pianist and file.
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i].meta, items[j].meta
		if a["album"] != b["album"] {
			return fmt.Sprint(a["album"]) < fmt.Sprint(b["album"])
		}
		if a["performer"] != b["performer"] {
			return fmt.Sprint(a["performer"]) < fmt.Sprint(b["performer"])
		}
		return items[i].label < items[j].label
	})
	track := map[string]int{}
	for _, it := range items {
		album := fmt.Sprint(it.meta["album"])
		track[album]++
		it.meta["track"] = track[album]
	}
	return items, nil
}

type timedNote struct {
	ms  int64
	key byte
}

// notesOf lists a MIDI file's note-ons in time order, chords low to high
// (in 20 ms steps, so the same performance orders the same way twice).
func notesOf(data []byte) []timedNote {
	msgs, err := smf.Read(data)
	if err != nil {
		return nil
	}
	var ns []timedNote
	for _, m := range msgs {
		if m.Kind() == 0x90 && m.Data2 > 0 {
			ns = append(ns, timedNote{int64(m.Time / time.Millisecond), m.Data1})
		}
	}
	sort.SliceStable(ns, func(i, j int) bool {
		if bi, bj := ns[i].ms/20, ns[j].ms/20; bi != bj {
			return bi < bj
		}
		return ns[i].key < ns[j].key
	})
	return ns
}

const stretch = 16 // notes compared

func keysHash(ns []timedNote) uint64 {
	h := fnv.New64a()
	for _, n := range ns {
		h.Write([]byte{n.key})
	}
	return h.Sum64()
}

// maestroHas reports whether MAESTRO holds a performance (some of it): any
// of three stretches of its notes, with the same timing within 15 ms, in a
// MAESTRO file of the same year. The same piece by another pianist has the
// same notes but not the same timing.
func maestroHas(dir string) (func(year string, notes []timedNote) bool, error) {
	zr, err := zip.OpenReader(filepath.Join(dir, "maestro", "maestro-v3.0.0-midi.zip"))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	type place struct {
		file int
		at   int
	}
	byYear := map[string][][]timedNote{}
	index := map[string]map[uint64][]place{}
	for _, f := range zr.File {
		parts := strings.Split(f.Name, "/")
		if len(parts) < 3 || !strings.HasSuffix(f.Name, ".midi") {
			continue
		}
		year := parts[len(parts)-2]
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		ns := notesOf(b)
		if index[year] == nil {
			index[year] = map[uint64][]place{}
		}
		fi := len(byYear[year])
		byYear[year] = append(byYear[year], ns)
		for i := 0; i+stretch <= len(ns); i++ {
			h := keysHash(ns[i : i+stretch])
			index[year][h] = append(index[year][h], place{fi, i})
		}
	}
	same := func(a, b []timedNote) bool {
		for i := range a {
			if d := (a[i].ms - a[0].ms) - (b[i].ms - b[0].ms); d < -15 || d > 15 {
				return false
			}
		}
		return true
	}
	return func(year string, ns []timedNote) bool {
		for _, at := range []int{0, len(ns) / 2, len(ns) * 3 / 4} {
			if at+stretch > len(ns) {
				continue
			}
			w := ns[at : at+stretch]
			for _, p := range index[year][keysHash(w)] {
				if same(w, byYear[year][p.file][p.at:p.at+stretch]) {
					return true
				}
			}
		}
		return false
	}, nil
}
