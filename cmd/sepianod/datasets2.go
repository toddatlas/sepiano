package main

// The second batch of collections for `sepianod datasets`
// (docs/LIBRARIES.md):
//   - SUPRA: Stanford's Welte-Mignon reproducing rolls (grade roll);
//   - ASAP: Piano-e-Competition performances not also in MAESTRO;
//   - PiJAMA: jazz piano transcribed from albums' audio (grade transcribed).
// All three come as MIDI and are converted with the Disklavier curve. Their
// on/off pedals are slowed for this piano as they play (piano.SafePedals).

import (
	"encoding/csv"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"sepiano/library"
)

// bom is the byte-order mark some catalogues start with (U+FEFF).
const bom = "\xef\xbb\xbf"

// readCatalogue reads a CSV catalogue: its rows, and each column's index
// by name. It checks the columns named in need are there.
func readCatalogue(file string, need ...string) (rows [][]string, col map[string]int, err error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, nil, err
	}
	r := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(b), bom)))
	r.FieldsPerRecord = -1
	all, err := r.ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", filepath.Base(file), err)
	}
	if len(all) < 2 {
		return nil, nil, fmt.Errorf("%s is empty", filepath.Base(file))
	}
	col = map[string]int{}
	for i, h := range all[0] {
		col[h] = i
	}
	for _, h := range need {
		if _, ok := col[h]; !ok {
			return nil, nil, fmt.Errorf("%s has no %s column", filepath.Base(file), h)
		}
	}
	for _, r := range all[1:] {
		for len(r) < len(all[0]) {
			r = append(r, "")
		}
		rows = append(rows, r)
	}
	return rows, col, nil
}

// ------------------------------------------------------------------- SUPRA

// supraItems: Stanford's Welte-Mignon red rolls (SUPRA), as the
// "expressive" MIDI files its emulator made from the roll scans.
func supraItems(dir string) ([]dsItem, error) {
	src := filepath.Join(dir, "supra", "x", "SUPRA-master", "welte-red", "midi-exp")
	rows, col, err := readCatalogue(filepath.Join(dir, "supra", "catalog.csv"),
		"file", "title", "composer", "performer", "label", "purl")
	if err != nil {
		return nil, err
	}
	// The catalogue has a stray "ÿQ" at the end of some fields.
	field := func(r []string, name string) string {
		return strings.TrimSpace(strings.TrimSuffix(r[col[name]], "\xc3\xbfQ"))
	}
	// A pianist with a few rolls gets an album; the rest share one.
	count := map[string]int{}
	for _, r := range rows {
		count[field(r, "performer")]++
	}
	type roll struct {
		r     []string
		album string
	}
	var rolls []roll
	for _, r := range rows {
		album := "Welte-Mignon rolls: more pianists"
		if count[field(r, "performer")] >= 4 {
			album = "Welte-Mignon rolls: " + personName(field(r, "performer"))
		}
		rolls = append(rolls, roll{r, album})
	}
	sort.SliceStable(rolls, func(i, j int) bool {
		if rolls[i].album != rolls[j].album {
			return rolls[i].album < rolls[j].album
		}
		return supraTitle(field(rolls[i].r, "title")) < supraTitle(field(rolls[j].r, "title"))
	})
	track := map[string]int{}
	var items []dsItem
	for _, rl := range rolls {
		r := rl.r
		name := field(r, "file")
		b, err := os.ReadFile(filepath.Join(src, name))
		if err != nil {
			return nil, err
		}
		track[rl.album]++
		meta := map[string]any{
			"title": supraTitle(field(r, "title")), "performer": personName(field(r, "performer")),
			"instrument": "Welte-Mignon roll", "album": rl.album, "track": track[rl.album],
			"collection": "SUPRA Welte-Mignon rolls", "captured_on": "Welte-Mignon reproducing roll (red)",
			"licence": "CC BY-NC-SA 4.0 (Shi & Sapp, Stanford University Piano Roll Archive)",
			"grade":   library.GradeRoll,
			"grade_why": "a Welte-Mignon reproducing roll, punched as the pianist played (1905–1932) and turned into MIDI " +
				"by an emulation of the Welte player: the pianist's timing, loudness rebuilt from the roll's expression holes, " +
				"the damper pedal only on or off (slowed for this piano as it plays)",
			"notes": strings.TrimSpace(fmt.Sprintf("%s, a red Welte-Mignon roll from the Stanford University Piano Roll Archive (SUPRA). %s",
				field(r, "label"), field(r, "purl"))),
		}
		if c := surname(field(r, "composer")); c != "" {
			meta["composer"] = c
		}
		items = append(items, dsItem{
			label: "supra " + strings.TrimSuffix(name, ".mid"),
			files: []namedFile{{name, b}},
			curve: "disklavier",
			meta:  meta,
		})
	}
	return items, nil
}

var datesRe = regexp.MustCompile(`\s*\([^)]*\d[^)]*\)\s*$`)

// personName turns a library catalogue's "Carreño, Teresa (1853-1917)"
// into "Teresa Carreño" ("Albert, Eugen d'" into "Eugen d'Albert"), and
// several people joined by "; " into "A & B".
func personName(s string) string {
	var out []string
	for _, p := range strings.Split(s, ";") {
		p = strings.TrimSpace(datesRe.ReplaceAllString(strings.TrimSpace(p), ""))
		last, first, ok := strings.Cut(p, ", ")
		switch {
		case p == "":
			continue
		case !ok:
			out = append(out, p)
		case strings.HasSuffix(first, "'"):
			out = append(out, first+last)
		default:
			out = append(out, first+" "+last)
		}
	}
	return strings.Join(out, " & ")
}

// surname takes the composer's surname from "Chopin, Frédéric
// (1810-1849)", as the rest of the library names composers.
func surname(s string) string {
	s = strings.TrimSpace(datesRe.ReplaceAllString(s, ""))
	last, _, _ := strings.Cut(s, ",")
	if last == "Mendelssohn-Bartholdy" {
		return "Mendelssohn"
	}
	return strings.TrimSpace(last)
}

// supraTitle tidies a library catalogue title: "Polonaise : (polonaise
// militaire) : op. 40, no. 1" → "Polonaise (polonaise militaire), op. 40,
// no. 1".
func supraTitle(s string) string {
	s = strings.ReplaceAll(s, " : (", " (")
	s = strings.ReplaceAll(s, " : ", ", ")
	return strings.TrimSpace(s)
}

// -------------------------------------------------------------------- ASAP

// asapItems: the ASAP performances that are not also in MAESTRO
// (new_performances.csv), from the Piano-e-Competition's Disklavier.
func asapItems(dir string) ([]dsItem, error) {
	info, err := loadEcomp(dir)
	if err != nil {
		return nil, fmt.Errorf("ecomp: %w", err)
	}
	src := filepath.Join(dir, "asap", "x", "asap-dataset-master")
	rows, col, err := readCatalogue(filepath.Join(dir, "asap", "new_performances.csv"), "composer", "title", "midi_performance")
	if err != nil {
		return nil, err
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if a, b := rows[i][col["composer"]], rows[j][col["composer"]]; a != b {
			return a < b
		}
		return rows[i][col["midi_performance"]] < rows[j][col["midi_performance"]]
	})
	track := map[string]int{}
	var items []dsItem
	for _, r := range rows {
		rel := r[col["midi_performance"]]
		b, err := os.ReadFile(filepath.Join(src, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		composer := r[col["composer"]]
		album := "Piano-e-Competition: " + composer + " (ASAP)"
		track[album]++
		items = append(items, dsItem{
			label: "asap " + rel,
			files: []namedFile{{path.Base(rel), b}},
			curve: "disklavier",
			meta: map[string]any{
				"title": asapTitle(composer, r[col["title"]]), "composer": composer, "performer": asapPlayer(path.Base(rel)),
				"instrument": "Yamaha_Disklavier", "album": album, "track": track[album],
				"collection": "ASAP", "captured_on": "Yamaha Disklavier",
				"licence": "CC BY-NC-SA 4.0 (Foscarin et al., ISMIR 2020)",
				"grade":   library.GradePerformance,
				"grade_why": "a competition performance captured on a Yamaha Disklavier, converted with the Disklavier curve: " +
					"timing as played, loudness within MIDI's 127 steps, pedal depth",
				"notes": "ASAP (Aligned Scores and Performances): a Piano-e-Competition performance not in MAESTRO. File " + rel + ".",
			},
		})
		if info != nil {
			if p, ok := info.asap(path.Base(rel), composer); ok {
				nameEcomp(items[len(items)-1].meta, info, p)
			}
		}
	}
	return items, nil
}

// asapPlayer names the pianist from an ASAP file name, which is all it
// gives: "Rizikov01M.mid" → "Rizikov", "ZhangE01M.mid" → "Zhang E.",
// "CHEN04.mid" → "Chen", "KIM_J01.mid" → "Kim J.", "Feiner_2006_01.mid" →
// "Feiner".
func asapPlayer(name string) string {
	n := strings.TrimSuffix(name, path.Ext(name))
	if i := strings.IndexAny(n, "0123456789"); i >= 0 {
		n = n[:i]
	}
	var words []string
	for _, w := range strings.Split(strings.Trim(n, "_"), "_") {
		if w == "" {
			continue
		}
		if strings.ToUpper(w) == w && len(w) > 1 {
			w = w[:1] + strings.ToLower(w[1:])
		}
		if len(w) == 1 {
			w += "."
		} else if l := len(w); l > 2 && w[l-1] >= 'A' && w[l-1] <= 'Z' && w[l-2] >= 'a' && w[l-2] <= 'z' {
			w = w[:l-1] + " " + w[l-1:] + "."
		}
		words = append(words, strings.ToUpper(w[:1])+w[1:])
	}
	if len(words) == 0 {
		return "Piano-e-Competition"
	}
	return strings.Join(words, " ")
}

// asapTitles are the titles the rules below don't cover, by
// "composer|ASAP title".
var asapTitles = map[string]string{
	"Bach|Italian_concerto":                                "Italian Concerto, BWV 971",
	"Chopin|Barcarolle":                                    "Barcarolle, Op. 60",
	"Chopin|Scherzos_20":                                   "Scherzo No. 1, Op. 20",
	"Chopin|Scherzos_31":                                   "Scherzo No. 2, Op. 31",
	"Liszt|Sonata":                                         "Piano Sonata in B minor, S. 178",
	"Liszt|Mephisto_Waltz":                                 "Mephisto Waltz No. 1",
	"Liszt|Gran_Etudes_de_Paganini_2_La_campanella":        "Grandes études de Paganini: La campanella",
	"Liszt|Gran_Etudes_de_Paganini_6_Theme_and_Variations": "Grandes études de Paganini: Theme and Variations",
	"Prokofiev|Toccata":                                    "Toccata, Op. 11",
	"Schumann|Toccata":                                     "Toccata, Op. 7",
	"Ravel|Pavane":                                         "Pavane pour une infante défunte",
	"Ravel|Gaspard_de_la_Nuit_1_Ondine":                    "Gaspard de la nuit: I. Ondine",
	"Ravel|Miroirs_3_Une_Barque":                           "Miroirs: III. Une barque sur l'océan",
	"Ravel|Miroirs_4_Alborada_del_gracioso":                "Miroirs: IV. Alborada del gracioso",
	"Schubert|Wanderer_fantasie":                           "Wanderer Fantasy, D. 760",
	"Scriabin|Sonatas_5":                                   "Piano Sonata No. 5, Op. 53",
}

var asapRules = []struct {
	re  *regexp.Regexp
	out func(m []string, composer string) string
}{
	{regexp.MustCompile(`^(Fugue|Prelude)_bwv_(\d+)$`), func(m []string, _ string) string { return m[1] + ", BWV " + m[2] }},
	{regexp.MustCompile(`^(Piano|Keyboard)_Sonatas_(\d+)-(\d)(?:_(\d))?$`), func(m []string, c string) string {
		mv := roman(m[3])
		if m[4] != "" {
			mv += "–" + roman(m[4])
		}
		if c == "Schubert" {
			return m[1] + " Sonata, D. " + m[2] + ": " + mv
		}
		return m[1] + " Sonata No. " + m[2] + ": " + mv
	}},
	{regexp.MustCompile(`^Sonata_(\d)_(\d)(?:st|nd|rd|th)$`), func(m []string, _ string) string { return "Piano Sonata No. " + m[1] + ": " + roman(m[2]) }},
	{regexp.MustCompile(`^Ballades?_(\d)$`), func(m []string, _ string) string { return "Ballade No. " + m[1] }},
	{regexp.MustCompile(`^Etudes_op_(\d+)_(\d+)$`), func(m []string, _ string) string { return "Étude, Op. " + m[1] + " No. " + m[2] }},
	{regexp.MustCompile(`^Preludes_op_(\d+)_(\d+)$`), func(m []string, _ string) string { return "Prelude, Op. " + m[1] + " No. " + m[2] }},
	{regexp.MustCompile(`^Six_Pieces_op_118_(\d)$`), func(m []string, _ string) string { return "Six Pieces, Op. 118: No. " + m[1] }},
	{regexp.MustCompile(`^Kreisleriana_(\d)$`), func(m []string, _ string) string { return "Kreisleriana, Op. 16: No. " + m[1] }},
	{regexp.MustCompile(`^Transcendental_Etudes_(\d+)$`), func(m []string, _ string) string { return "Transcendental Étude No. " + m[1] }},
	{regexp.MustCompile(`^Concert_Etude_S145_(\d)$`), func(m []string, _ string) string { return "Concert Étude, S. 145 No. " + m[1] }},
	{regexp.MustCompile(`^Hungarian_Rhapsodies_(\d+)$`), func(m []string, _ string) string { return "Hungarian Rhapsody No. " + m[1] }},
	{regexp.MustCompile(`^Impromptu_op\.?90_D\.899_(\d)$`), func(m []string, _ string) string { return "Impromptu, Op. 90 No. " + m[1] + " (D. 899)" }},
	{regexp.MustCompile(`^Impromptu_op142_(\d)$`), func(m []string, _ string) string { return "Impromptu, Op. 142 No. " + m[1] + " (D. 935)" }},
}

// asapTitle turns ASAP's folder-style titles ("Piano_Sonatas_11-1") into
// the library's ("Piano Sonata No. 11: I").
func asapTitle(composer, t string) string {
	if s, ok := asapTitles[composer+"|"+t]; ok {
		return s
	}
	for _, r := range asapRules {
		if m := r.re.FindStringSubmatch(t); m != nil {
			return r.out(m, composer)
		}
	}
	return strings.ReplaceAll(t, "_", " ")
}

func roman(d string) string {
	n, _ := strconv.Atoi(d)
	if r := []string{"", "I", "II", "III", "IV", "V", "VI"}; n > 0 && n < len(r) {
		return r[n]
	}
	return d
}

// ------------------------------------------------------------------ PiJAMA

// pijamaItems: PiJAMA's jazz piano, transcribed from the albums' audio
// (the Kong et al. transcriptions, which have the damper pedal).
func pijamaItems(dir string) ([]dsItem, error) {
	base := filepath.Join(dir, "pijama")
	src := filepath.Join(base, "x_kong", "midi_kong")
	rows, col, err := readCatalogue(filepath.Join(base, "pijama.csv"),
		"id", "artist", "album", "title", "recording_condition", "midi_filepath", "agreement")
	if err != nil {
		return nil, err
	}
	// Three names were too long for the zip; they are stored shortened.
	short := map[string]string{}
	if b, err := os.ReadFile(filepath.Join(base, "shortened-names.txt")); err == nil {
		for _, line := range strings.Split(strings.TrimPrefix(string(b), bom), "\n") {
			if long, s, ok := strings.Cut(strings.TrimRight(line, "\r"), "\t->\t"); ok {
				short[long] = s
			}
		}
	}
	track := map[string]int{}
	var items []dsItem
	for _, r := range rows {
		rel, ok := strings.CutPrefix(r[col["midi_filepath"]], "data/midi/")
		if !ok {
			return nil, fmt.Errorf("row %s: unexpected path %q", r[col["id"]], r[col["midi_filepath"]])
		}
		file := filepath.Join(src, filepath.FromSlash(rel))
		if s, ok := short["midi_kong/"+rel]; ok {
			file = filepath.Join(filepath.Dir(file), s)
		}
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		artist, albumName := oneSpace(r[col["artist"]]), oneSpace(r[col["album"]])
		album := artist + ": " + albumName + " (PiJAMA)"
		track[album]++
		title, venue := pijamaTitle(oneSpace(r[col["title"]]))
		notes := "PiJAMA (Piano Jazz with Automatic MIDI Annotations): transcribed by software from the album \"" + albumName + "\""
		if venue != "" {
			notes += ", " + venue
		} else if r[col["recording_condition"]] == "live" {
			notes += ", a live recording"
		}
		if a, err := strconv.ParseFloat(r[col["agreement"]], 64); err == nil {
			notes += fmt.Sprintf(". Two transcription methods agree on %.0f%% of its notes", a*100)
		}
		notes += "."
		items = append(items, dsItem{
			label: "pijama " + rel,
			files: []namedFile{{path.Base(rel), b}},
			curve: "disklavier",
			meta: map[string]any{
				"title": title, "performer": artist,
				"instrument": "Audio transcription", "album": album, "track": track[album],
				"collection": "PiJAMA jazz transcriptions", "captured_on": "Transcribed from audio",
				"licence": "CC BY-NC 3.0 (Edwards, Dixon & Benetos, TISMIR 2023)",
				"grade":   library.GradeTranscribed,
				"grade_why": "notes found by software in the album's audio: the pianist's timing, loudness estimated from the sound, " +
					"the damper pedal only on or off (slowed for this piano as it plays); now and then a wrong or missed note",
				"notes": notes,
			},
		})
	}
	return items, nil
}

// pijamaTitle splits "Begin The Beguine - Live At Maybeck Recital Hall,
// Berkeley, CA  July 19, 1992" into the title and where it was played
// ("live at Maybeck Recital Hall, Berkeley, CA July 19, 1992").
func pijamaTitle(s string) (title, venue string) {
	i := strings.Index(s, " - Live")
	if i <= 0 {
		return strings.TrimSpace(s), ""
	}
	rest := strings.Join(strings.Fields(s[i+len(" - Live"):]), " ")
	venue = "live"
	if rest != "" {
		venue += " " + strings.ToLower(rest[:1]) + rest[1:]
	}
	return strings.TrimSpace(s[:i]), venue
}

// oneSpace trims s and collapses its runs of spaces.
func oneSpace(s string) string { return strings.Join(strings.Fields(s), " ") }
