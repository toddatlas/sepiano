package main

// The Piano-e-Competition's pianists and prizes, for the MAESTRO and ASAP
// pieces (docs/LIBRARIES.md, "Who is playing"). MAESTRO names no pianists
// and ASAP only codes from the competition's file names. DOWNLOADS/ecomp/
// holds tables built from the competition's own pages, as archived by the
// Wayback Machine (piano-e-competition.com/midi_YYYY.asp and its winners'
// history):
//   - maestro-performers.csv: midi_filename, performer, source (how sure);
//   - ecomp-entries.csv: year, performer, nationality, round, composer,
//     title, midi_href, code (the competition's MIDI file name);
//   - ecomp-winners.csv: year, prize, name, country, source_url.
// Without the folder, pieces keep "Piano-e-Competition YEAR".

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

type ecompPerf struct {
	name, year, sure string
}

type ecompInfo struct {
	byMaestro map[string]ecompPerf   // MAESTRO midi_filename
	byCode    map[string][]ecompPerf // lower-case competition file code
	codeWork  map[string][]string    // the same, composer of each entry
	awards    map[string][]string    // year|name key: prizes
	first     map[string]bool        // year|name key: a 1st prize
}

// loadEcomp reads DOWNLOADS/ecomp/, or returns nil if it is not there.
func loadEcomp(dir string) (*ecompInfo, error) {
	base := filepath.Join(dir, "ecomp")
	if _, err := os.Stat(filepath.Join(base, "maestro-performers.csv")); err != nil {
		return nil, nil
	}
	e := &ecompInfo{byMaestro: map[string]ecompPerf{}, byCode: map[string][]ecompPerf{}, codeWork: map[string][]string{},
		awards: map[string][]string{}, first: map[string]bool{}}
	rows, col, err := readCatalogue(filepath.Join(base, "maestro-performers.csv"), "midi_filename", "performer", "source")
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		f := r[col["midi_filename"]]
		year, _, _ := strings.Cut(f, "/")
		e.byMaestro[f] = ecompPerf{name: oneSpace(r[col["performer"]]), year: year, sure: r[col["source"]]}
	}
	if rows, col, err = readCatalogue(filepath.Join(base, "ecomp-entries.csv"), "year", "performer", "composer", "code"); err == nil {
		for _, r := range rows {
			code := strings.ToLower(r[col["code"]])
			e.byCode[code] = append(e.byCode[code], ecompPerf{name: ecompSpelling.Replace(oneSpace(r[col["performer"]])), year: r[col["year"]], sure: "the competition's file " + r[col["code"]]})
			e.codeWork[code] = append(e.codeWork[code], r[col["composer"]])
		}
	}
	rows, col, err = readCatalogue(filepath.Join(base, "ecomp-winners.csv"), "year", "prize", "name")
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		year, prize, name := r[col["year"]], r[col["prize"]], r[col["name"]]
		if strings.HasPrefix(name, "(") { // "(not awarded)"
			continue
		}
		text := prizeText(prize) + ", Piano-e-Competition " + year
		for _, n := range nameVariants(name) {
			k := year + "|" + nameKey(n)
			if !slices.Contains(e.awards[k], text) {
				e.awards[k] = append(e.awards[k], text)
			}
			if strings.HasPrefix(prize, "1st") {
				e.first[k] = true
			}
		}
	}
	return e, nil
}

// award returns a pianist's prizes in a year, and whether one is a 1st.
func (e *ecompInfo) award(year, name string) (string, bool) {
	k := year + "|" + nameKey(name)
	return strings.Join(e.awards[k], "; "), e.first[k]
}

// asap names the pianist of an ASAP file from its code ("Rizikov01M.mid":
// the competition's Rizikov01, "M" marking MAESTRO's copy). A code used in
// several years is settled by the composer.
func (e *ecompInfo) asap(file, composer string) (ecompPerf, bool) {
	code := strings.ToLower(strings.TrimSuffix(file, filepath.Ext(file)))
	if c, ok := strings.CutSuffix(code, "m"); ok && c != "" && c[len(c)-1] >= '0' && c[len(c)-1] <= '9' {
		code = c
	}
	ps := e.byCode[code]
	if len(ps) == 1 {
		return ps[0], true
	}
	var hit []ecompPerf
	for i, p := range ps {
		if strings.Contains(strings.ToLower(e.codeWork[code][i]), strings.ToLower(composer)) {
			hit = append(hit, p)
		}
	}
	if len(hit) >= 1 && allSame(hit) {
		return hit[0], true
	}
	return ecompPerf{}, false
}

func allSame(ps []ecompPerf) bool {
	for _, p := range ps[1:] {
		if p.name != ps[0].name || p.year != ps[0].year {
			return false
		}
	}
	return true
}

// prizeText: "1st (shared; junior competition)" → "1st prize (shared;
// junior competition)"; "Schubert Prize" stays.
func prizeText(p string) string {
	p = strings.TrimSpace(strings.ReplaceAll(p, " (named in winner bio)", ""))
	if m := ordinalRe.FindStringSubmatch(p); m != nil {
		return strings.TrimSpace(m[1] + " prize " + m[2])
	}
	return p
}

var ordinalRe = regexp.MustCompile(`^(\d+(?:st|nd|rd|th))\b\s*(.*)$`)

// nameVariants: "Frank Dupree (Düpree)" → Frank Dupree, Frank Düpree;
// "Chen Guang (Cheng Guang on MIDI page)" → Chen Guang, Cheng Guang.
func nameVariants(s string) []string {
	main, alt, _ := strings.Cut(s, "(")
	main = strings.TrimSpace(main)
	out := []string{main}
	alt = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(alt), ")"))
	if a, _, ok := strings.Cut(alt, " on "); ok {
		alt = a
	}
	f := strings.Fields(main)
	switch words := strings.Fields(alt); {
	case len(words) == 0 || strings.Contains(alt, "says") || strings.Contains(alt, "named"):
	case len(words) >= 2:
		out = append(out, alt)
	case len(f) >= 2:
		out = append(out, strings.Join(append(append([]string{}, f[:len(f)-1]...), alt), " "), alt+" "+strings.Join(f[1:], " "))
	}
	return out
}

// nameKey compares names without accents, case, hyphens or spaces.
func nameKey(s string) string {
	var b strings.Builder
	for _, r := range accentFold.Replace(strings.ToLower(s)) {
		if r >= 'a' && r <= 'z' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ecompNote says how sure a name is, for the piece's notes.
func ecompNote(p ecompPerf) string {
	switch {
	case strings.HasPrefix(p.sure, "[C-group]"):
		return fmt.Sprintf("Pianist: %s (very likely: the same competitor number as the rest of the group; this title is not in their %s repertoire list).", p.name, p.year)
	default:
		return fmt.Sprintf("Pianist: %s, named from the competition's own %s pages.", p.name, p.year)
	}
}

// accentFold takes the accents off the letters the pianists' names use.
var accentFold = strings.NewReplacer(
	"à", "a", "á", "a", "â", "a", "ã", "a", "ä", "a", "å", "a", "ā", "a", "ă", "a", "ą", "a",
	"ç", "c", "ć", "c", "č", "c", "ď", "d", "đ", "d",
	"è", "e", "é", "e", "ê", "e", "ë", "e", "ē", "e", "ę", "e", "ě", "e", "ğ", "g",
	"ì", "i", "í", "i", "î", "i", "ï", "i", "ı", "i", "ł", "l", "ľ", "l",
	"ñ", "n", "ń", "n", "ň", "n",
	"ò", "o", "ó", "o", "ô", "o", "õ", "o", "ö", "o", "ø", "o", "ő", "o",
	"ř", "r", "ś", "s", "š", "s", "ş", "s", "ß", "ss", "ť", "t", "ţ", "t",
	"ù", "u", "ú", "u", "û", "u", "ü", "u", "ū", "u", "ů", "u", "ű", "u",
	"ý", "y", "ÿ", "y", "ź", "z", "ż", "z", "ž", "z",
)

// ecompSpelling corrects the competition pages' spellings that differ from
// its winners list (as the matching did): Duepree, Johannson, Cheng Guang.
var ecompSpelling = strings.NewReplacer("Duepree", "Dupree", "Johannson", "Johansson", "Cheng Guang", "Chen Guang")
