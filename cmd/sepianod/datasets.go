package main

// sepianod datasets: imports the free performance collections downloaded
// for the piano (docs/LIBRARIES.md) into a running sepianod, each piece
// with where it comes from, so the library can sort and filter by it:
//   - Vienna 4x22: native Bösendorfer SE290 files, imported as they are;
//   - Batik plays Mozart: SE290 recordings exported as MIDI, converted back
//     with Bösendorfer's own curve (se.BosendorferCurve);
//   - MAESTRO v3: Disklavier performances, converted with the Disklavier
//     curve (se.DefaultCurve), titled from its catalogue;
//   - SUPRA, ASAP and PiJAMA (datasets2.go).
// Running it again adds nothing twice (the library knows each file) and
// sets the details again.

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"sepiano/library"
	"sepiano/se"
)

// dsItem is one piece to import: its files, the curve for a MIDI file, and
// the details to set afterwards.
type dsItem struct {
	label string
	files []namedFile
	curve string
	meta  map[string]any
}

type namedFile struct {
	name string
	data []byte
}

func datasetsCmd(args []string) int {
	fl := flag.NewFlagSet("sepianod datasets", flag.ExitOnError)
	server := fl.String("server", "http://127.0.0.1", "running sepianod to import into")
	only := fl.String("only", "vienna,batik,maestro,supra,asap,pijama", "which collections, comma-separated")
	dry := fl.Bool("n", false, "list what would be imported, and import nothing")
	limit := fl.Int("limit", 0, "import at most this many pieces of each collection (0: all)")
	fl.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: sepianod datasets [-server URL] [-only vienna,batik,maestro,supra,asap,pijama] [-n] [-limit N] DOWNLOADS")
		fmt.Fprintln(os.Stderr, "DOWNLOADS holds vienna4x22/x/, batik/midi/, maestro/maestro-v3.0.0-midi.zip, supra/, asap/ and pijama/ (docs/LIBRARIES.md).")
		fl.PrintDefaults()
	}
	fl.Parse(args)
	if fl.NArg() != 1 {
		fl.Usage()
		return 2
	}
	dir := fl.Arg(0)
	want := map[string]bool{}
	for _, s := range strings.Split(*only, ",") {
		want[strings.TrimSpace(s)] = true
	}
	var items []dsItem
	for _, c := range []struct {
		name string
		load func(string) ([]dsItem, error)
	}{{"vienna", viennaItems}, {"batik", batikItems}, {"maestro", maestroItems}, {"supra", supraItems}, {"asap", asapItems}, {"pijama", pijamaItems}} {
		if !want[c.name] {
			continue
		}
		its, err := c.load(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", c.name, err)
			return 1
		}
		if *limit > 0 && len(its) > *limit {
			its = its[:*limit]
		}
		fmt.Printf("%s: %d pieces\n", c.name, len(its))
		items = append(items, its...)
	}
	if *dry {
		for _, it := range items {
			composer, _ := it.meta["composer"].(string)
			fmt.Printf("  %-34s %s | %v | %v\n", it.label, composer, it.meta["title"], it.meta["album"])
		}
		return 0
	}
	added, dups, failed := 0, 0, 0
	for i, it := range items {
		id, dup, err := importItem(*server, it)
		switch {
		case err != nil:
			failed++
			fmt.Fprintf(os.Stderr, "%s: %v\n", it.label, err)
		case dup:
			dups++
		default:
			added++
		}
		if (i+1)%50 == 0 || i == len(items)-1 {
			fmt.Printf("%d of %d done (%d added, %d already there, %d failed)%s\n", i+1, len(items), added, dups, failed, map[bool]string{true: "", false: " last " + id}[id == ""])
		}
	}
	if failed > 0 {
		return 1
	}
	return 0
}

// importItem uploads one piece and sets its details.
func importItem(server string, it dsItem) (id string, dup bool, err error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, f := range it.files {
		w, err := mw.CreateFormFile("file", f.name)
		if err != nil {
			return "", false, err
		}
		w.Write(f.data)
	}
	if it.curve != "" {
		mw.WriteField("curve", it.curve)
	}
	mw.Close()
	base := strings.TrimRight(server, "/") + "/api/v1/pieces"
	resp, err := http.Post(base, mw.FormDataContentType(), &buf)
	if err != nil {
		return "", false, err
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", false, fmt.Errorf("%s: %s", resp.Status, body)
	}
	var r library.Result
	if err := json.Unmarshal(body, &r); err != nil {
		return "", false, err
	}
	switch {
	case len(r.Errors) > 0:
		return "", false, errors.New(r.Errors[0].Error)
	case len(r.Added) > 0:
		id = r.Added[0].ID
	case len(r.Duplicates) > 0:
		id, dup = r.Duplicates[0].ID, true
	default:
		return "", false, errors.New("nothing was added")
	}
	b, _ := json.Marshal(it.meta)
	req, _ := http.NewRequest("PATCH", base+"/"+id, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		return id, dup, err
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return id, dup, fmt.Errorf("details: %s: %s", resp.Status, body)
	}
	return id, dup, nil
}

// ------------------------------------------------------------- Vienna 4x22

// viennaPieces names the corpus's excerpts by their file-name codes.
var viennaPieces = map[string]struct{ composer, title, album string }{
	"bal_bo": {"Chopin", "Ballade No. 2 in F major, Op. 38 (opening)", "Vienna 4x22: Chopin, Ballade Op. 38"},
	"blk_bo": {"Chopin", "Ballade No. 2, Op. 38 (opening, 1st and 3rd voices brought out)", "Vienna 4x22: Chopin, Ballade Op. 38 (voices)"},
	"etd_bo": {"Chopin", "Étude in E major, Op. 10 No. 3 (opening)", "Vienna 4x22: Chopin, Étude Op. 10 No. 3"},
	"-moz":   {"Mozart", "Piano Sonata in A major, K. 331: I. Andante grazioso (theme)", "Vienna 4x22: Mozart, Sonata K. 331"},
	"-schub": {"Schubert", "German Dance, D. 783 No. 15", "Vienna 4x22: Schubert, D. 783 No. 15"},
}

var viennaName = regexp.MustCompile(`^(\d\d)(bal_bo|blk_bo|etd_bo|-moz|-schub)$`)

func viennaItems(dir string) ([]dsItem, error) {
	src := filepath.Join(dir, "vienna4x22", "x", "boesendorfer_se")
	entries, err := os.ReadDir(src)
	if err != nil {
		return nil, err
	}
	var items []dsItem
	for _, e := range entries {
		base, ok := strings.CutSuffix(e.Name(), ".KB")
		if !ok {
			continue
		}
		m := viennaName.FindStringSubmatch(base)
		if m == nil {
			continue // the averaged performance and other extras
		}
		read := func(ext string) ([]byte, error) { return os.ReadFile(filepath.Join(src, base+ext)) }
		kb, err := read(".KB")
		if err != nil {
			return nil, err
		}
		lp, err := read(".LP")
		if err != nil {
			return nil, err
		}
		sp, err := read(".SP")
		if errors.Is(err, os.ErrNotExist) {
			// Mozart and Schubert came without a soft-pedal file: the
			// pianists did not use it. An empty one, as long as the piece.
			k, err := se.ParseKB(kb)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", base, err)
			}
			sp, err = (&se.Pedal{Samples: make([]byte, k.End/se.SoftPeriod+2)}).Encode()
			if err != nil {
				return nil, err
			}
		} else if err != nil {
			return nil, err
		}
		p := viennaPieces[m[2]]
		n, _ := strconv.Atoi(m[1])
		items = append(items, dsItem{
			label: "vienna " + base,
			files: []namedFile{{base + ".kb", kb}, {base + ".lp", lp}, {base + ".sp", sp}},
			meta: map[string]any{
				"title": p.title, "composer": p.composer, "performer": fmt.Sprintf("Pianist %d (Vienna 4x22)", n),
				"recorded_on": "1999", "instrument": "Bosendorfer_SE290", "album": p.album, "track": n,
				"collection": "Vienna 4x22", "captured_on": "Bösendorfer SE290",
				"licence": "CC BY 4.0 (Werner Goebl, doi:10.21939/4X22)",
				"notes":   "Vienna 4x22 Piano Corpus: 22 pianists, recorded in 1999 on a Bösendorfer SE290. The native SE files.",
			},
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].label < items[j].label })
	return items, nil
}

// -------------------------------------------------------------------- Batik

var batikName = regexp.MustCompile(`^kv(\d+[a-z]?)_(\d)$`)

func batikItems(dir string) ([]dsItem, error) {
	src := filepath.Join(dir, "batik", "midi")
	entries, err := os.ReadDir(src)
	if err != nil {
		return nil, err
	}
	roman := []string{"", "I", "II", "III", "IV"}
	var items []dsItem
	for _, e := range entries {
		base, ok := strings.CutSuffix(e.Name(), ".mid")
		if !ok {
			continue
		}
		m := batikName.FindStringSubmatch(base)
		if m == nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			return nil, err
		}
		mv, _ := strconv.Atoi(m[2])
		mov := m[2]
		if mv < len(roman) {
			mov = roman[mv]
		}
		items = append(items, dsItem{
			label: "batik " + base,
			files: []namedFile{{e.Name(), b}},
			curve: "bosendorfer",
			meta: map[string]any{
				"title": fmt.Sprintf("Piano Sonata K. %s: %s", m[1], mov), "composer": "Mozart", "performer": "Roland Batik",
				"instrument": "Bosendorfer_SE290", "album": fmt.Sprintf("Mozart: Piano Sonata K. %s (Roland Batik)", m[1]), "track": mv,
				"collection": "Batik plays Mozart", "captured_on": "Bösendorfer SE290",
				"licence": "CC BY-NC-SA 4.0 (Hu & Widmer, ISMIR 2023)",
				"grade":   library.GradeSEMIDI,
				"grade_why": "an SE290 recording exported as MIDI by Bösendorfer's converter, turned back with its own curve: " +
					"timing as recorded, loudness within MIDI's 127 steps",
				"notes": "Batik plays Mozart: Roland Batik on a Bösendorfer SE290, from the SE recording via MIDI.",
			},
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].label < items[j].label })
	return items, nil
}

// ------------------------------------------------------------------ MAESTRO

func maestroItems(dir string) ([]dsItem, error) {
	zr, err := zip.OpenReader(filepath.Join(dir, "maestro", "maestro-v3.0.0-midi.zip"))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	files := map[string]*zip.File{}
	var catalogue *zip.File
	for _, f := range zr.File {
		rel := f.Name
		if i := strings.Index(rel, "/"); i >= 0 {
			rel = rel[i+1:] // under maestro-v3.0.0/
		}
		files[rel] = f
		if path.Base(f.Name) == "maestro-v3.0.0.csv" {
			catalogue = f
		}
	}
	if catalogue == nil {
		return nil, errors.New("maestro-v3.0.0.csv is not in the zip")
	}
	rc, err := catalogue.Open()
	if err != nil {
		return nil, err
	}
	rows, err := csv.NewReader(rc).ReadAll()
	rc.Close()
	if err != nil || len(rows) < 2 {
		return nil, fmt.Errorf("the catalogue: %v", err)
	}
	col := map[string]int{}
	for i, h := range rows[0] {
		col[h] = i
	}
	for _, h := range []string{"canonical_composer", "canonical_title", "year", "midi_filename"} {
		if _, ok := col[h]; !ok {
			return nil, fmt.Errorf("the catalogue has no %s column", h)
		}
	}
	track := map[string]int{}
	var items []dsItem
	for _, r := range rows[1:] {
		name := r[col["midi_filename"]]
		f := files[name]
		if f == nil {
			return nil, fmt.Errorf("%s is in the catalogue but not in the zip", name)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		year := r[col["year"]]
		album := "Piano-e-Competition " + year + " (MAESTRO)"
		track[album]++
		items = append(items, dsItem{
			label: "maestro " + path.Base(name),
			files: []namedFile{{path.Base(name), b}},
			curve: "disklavier",
			meta: map[string]any{
				"title": r[col["canonical_title"]], "composer": surnames(r[col["canonical_composer"]]),
				"performer": "Piano-e-Competition " + year, "recorded_on": year,
				"instrument": "Yamaha_Disklavier", "album": album, "track": track[album],
				"collection": "MAESTRO v3", "captured_on": "Yamaha Disklavier",
				"licence": "CC BY-NC-SA 4.0 (Google LLC)",
				"grade":   library.GradePerformance,
				"grade_why": "a competition performance captured on a Yamaha Disklavier, converted with the Disklavier curve: " +
					"timing as played, loudness within MIDI's 127 steps, pedal depth",
				"notes": "MAESTRO v3 (Hawthorne et al., ICLR 2019): the International Piano-e-Competition, " + year + ".",
			},
		})
	}
	return items, nil
}

// surnames turns MAESTRO's full composer names into the surnames the rest
// of the library uses ("Frédéric Chopin" → "Chopin"; "Franz Schubert /
// Franz Liszt" → "Schubert / Liszt"), so the composer filter groups them.
func surnames(s string) string {
	parts := strings.Split(s, "/")
	for i, p := range parts {
		f := strings.Fields(p)
		if len(f) > 0 {
			parts[i] = f[len(f)-1]
		}
	}
	return strings.Join(parts, " / ")
}
