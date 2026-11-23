package library

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Meta is what a performance's file name and .kb header say about it.
type Meta struct {
	Title      string
	Composer   string
	Performer  string
	RecordedOn string // YYYY-MM-DD, YYYY-MM or YYYY
	Instrument string
	PerfKey    string // the same for every rendering of one performance
	Notes      string
}

// Describe derives a piece's metadata from its base file name and the text
// in its .kb header. The header, written for people, wins where both speak;
// the name supplies the rendering instrument and anything the header lacks.
func Describe(name, header string) Meta {
	n, h := ParseName(name), ParseHeader(header)
	m := n
	for _, f := range []struct{ dst, src *string }{
		{&m.Title, &h.Title}, {&m.Composer, &h.Composer}, {&m.Performer, &h.Performer},
		{&m.RecordedOn, &h.RecordedOn}, {&m.Notes, &h.Notes},
	} {
		if *f.src != "" {
			*f.dst = *f.src
		}
	}
	if h.Title != "" && !zenphRE.MatchString(name) {
		// Group the copies of one recording under different file names.
		m.PerfKey = strings.ToLower(strings.Join([]string{h.Performer, h.Composer, h.Title}, "|"))
	}
	return m
}

// Zenph names look like
//
//	02-Rachmaninoff19280404-Rachmaninoff_PreludeOp3_2-Steinway_SED133291
//	Tatum19490402-Gershwin_STWOM-Boesendorfer_SE42251-590-22(20150112)-SE
//
// [track-] performer and recording date - composer_work - instrument [-SE].
var zenphRE = regexp.MustCompile(`^(?:\d+-)?([A-Za-z][A-Za-z.']*?)(\d{8})-(.+?)-(` + instruments + `[A-Za-z]*_.+?|HCXMidi(?:-test)?)(?:-SE)?$`)

const instruments = `(?:Steinway|Yamaha|Bosendorfer|Boesendorfer|Mason|Baldwin|Kawai|Fazioli|Bechstein)`

// A rendering instrument at the end of another kind of name.
var instrumentRE = regexp.MustCompile(`-(` + instruments + `[A-Za-z]*_.+?)(?:-SE)?$`)

// ParseName reads the metadata out of a performance's base file name.
func ParseName(name string) Meta {
	if m := zenphRE.FindStringSubmatch(name); m != nil {
		composer, work, _ := strings.Cut(m[3], "_")
		if work == "" {
			work, composer = composer, ""
		}
		title := prettyWork(work)
		if t, ok := abbreviations[composer+"_"+work]; ok {
			title = t
		}
		return Meta{
			Title:      title,
			Composer:   joinNames(composer, "-", nil),
			Performer:  performerName(m[1]),
			RecordedOn: nameDate(m[2]),
			Instrument: m[4],
			PerfKey:    strings.ToLower(m[1] + m[2] + "-" + m[3]),
		}
	}
	meta := Meta{}
	if m := instrumentRE.FindStringSubmatch(name); m != nil {
		meta.Instrument = m[1]
		name = strings.TrimSuffix(strings.TrimSuffix(name, "-SE"), "-"+m[1])
	}
	meta.Title = pretty(name)
	meta.PerfKey = strings.ToLower(name)
	return meta
}

// abbreviations spells out the titles the Zenph names abbreviate, keyed by
// the name's composer_work part. Only titles that are certain are here;
// `sepianod tag` finds others from catalogs, and the rest can be edited in
// the app.
var abbreviations = map[string]string{
	"Gershwin_TMIL":       "The Man I Love",
	"Gershwin_STWOM":      "Someone to Watch Over Me",
	"Gershwin_LFAB":       "Looking for a Boy",
	"Gershwin_SALD":       "Sweet and Low-Down",
	"Gershwin_TCF":        "That Certain Feeling",
	"Gershwin_WDWD":       "When Do We Dance?",
	"Gershwin_CYH":        "Clap Yo' Hands",
	"Gershwin_DDD":        "Do-Do-Do",
	"Gershwin_MOAO":       "My One and Only",
	"Gershwin_HOTM":       "Hang On to Me",
	"Gershwin_FR":         "Fascinating Rhythm",
	"Gershwin_RiB":        "Rhapsody in Blue",
	"Whiting_TMFW":        "Too Marvelous for Words",
	"Suesse_MSL":          "My Silent Love",
	"Debussy_ClairdeLune": "Clair de lune",
	"Williams_AWLOSGO":    "Whole Lot of Shakin' Going On",
	"Lewis_AWLOSGO":       "Whole Lot of Shakin' Going On",

	"Rimsky-KorsakovRachmaninoff_TFOTBB": "The Flight of the Bumble-Bee",
	"RimskyKorsakovRachmaninoff_TFOTBB":  "The Flight of the Bumble-Bee",
	"MendelssohnRachmaninoff_MSND":       "Scherzo from A Midsummer Night's Dream",
}

// nameDate turns 19280404 into 1928-04-04. Zenph writes 99 for an unknown
// day and 9999 for an unknown day and month.
func nameDate(d string) string {
	y, mo, da := d[0:4], d[4:6], d[6:8]
	m, _ := strconv.Atoi(mo)
	n, _ := strconv.Atoi(da)
	switch {
	case m < 1 || m > 12:
		return y
	case n < 1 || n > 31:
		return y + "-" + mo
	}
	return y + "-" + mo + "-" + da
}

// Names with capitals inside, which the camel-case split would break,
// longest first so that a prefix never hides a longer name.
var oddNames = [][2]string{
	{"Rimsky-Korsakov", "Rimsky-Korsakov"}, {"RimskyKorsakov", "Rimsky-Korsakov"},
	{"deLarrocha", "de Larrocha"}, {"MacDonald", "MacDonald"},
	{"DeFalla", "de Falla"}, {"deFalla", "de Falla"}, {"DePaul", "DePaul"},
	{"McHugh", "McHugh"}, {"O'Pate", "O'Pate"},
}

// nextName returns the first name in a run like "KreislerRachmaninoff".
func nextName(s string) string {
	r := []rune(s)
	i := 1
	switch {
	case len(r) >= 3 && unicode.IsUpper(r[0]) && r[1] == '\'' && unicode.IsUpper(r[2]): // O'Brien
		i = 3
		for i < len(r) && unicode.IsLower(r[i]) {
			i++
		}
	case unicode.IsUpper(r[0]) && len(r) > 1 && unicode.IsUpper(r[1]): // "JS" in "JSBach", "ODJB"
		for i < len(r) && unicode.IsUpper(r[i]) {
			i++
		}
		if i < len(r) && unicode.IsLower(r[i]) {
			i-- // that capital starts the next word
		}
	case unicode.IsUpper(r[0]):
		for i < len(r) && (unicode.IsLower(r[i]) || r[i] == '\'') {
			i++
		}
	default: // "de", "van"
		for i < len(r) && unicode.IsLower(r[i]) {
			i++
		}
	}
	return string(r[:i])
}

// splitNames splits "KreislerRachmaninoff" into Kreisler and Rachmaninoff,
// keeping names such as Rimsky-Korsakov and de Falla whole.
func splitNames(s string) []string {
	var out []string
	particle := false // the last name was "de", "von" or "van"
	for s = strings.Trim(s, "-"); s != ""; s = strings.TrimLeft(s, "-") {
		tok := ""
		for _, o := range oddNames {
			if strings.HasPrefix(s, o[0]) {
				tok, s = o[1], s[len(o[0]):]
				break
			}
		}
		if tok == "" {
			tok = nextName(s)
			s = s[len(tok):]
		}
		if particle {
			out[len(out)-1] += " " + tok
			particle = false
			continue
		}
		out = append(out, tok)
		particle = tok == "de" || tok == "von" || tok == "van"
	}
	return out
}

func joinNames(s, sep string, full map[string]string) string {
	var parts []string
	for _, p := range splitNames(s) {
		if f, ok := full[p]; ok {
			p = f
		}
		// Initials belong to the next name: "JSBach" → "JS Bach".
		if n := len(parts); n > 0 && isInitials(parts[n-1]) {
			parts[n-1] += " " + p
			continue
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, sep)
}

func isInitials(s string) bool {
	if len(s) == 0 || len(s) > 3 {
		return false
	}
	for _, r := range s {
		if !unicode.IsUpper(r) {
			return false
		}
	}
	return true
}

// Pianists and groups in the Zenph names, by the token the names use.
var performers = map[string]string{
	"Gould": "Glenn Gould", "Rubinstein": "Arthur Rubinstein", "Gershwin": "George Gershwin",
	"Lipatti": "Dinu Lipatti", "Rachmaninoff": "Sergei Rachmaninoff", "Tatum": "Art Tatum",
	"Peterson": "Oscar Peterson", "Granados": "Enrique Granados", "Evans": "Bill Evans",
	"Morton": "Jelly Roll Morton", "Albeniz": "Isaac Albéniz", "Monk": "Thelonious Monk",
	"Horowitz": "Vladimir Horowitz", "Ellington": "Duke Ellington", "Cliburn": "Van Cliburn",
	"Cortot": "Alfred Cortot", "Garner": "Erroll Garner", "Schnabel": "Artur Schnabel",
	"Hofmann": "Josef Hofmann", "Gilels": "Emil Gilels", "Richter": "Sviatoslav Richter",
	"Slenczynska": "Ruth Slenczynska", "Ogdon": "John Ogdon", "Kapell": "William Kapell",
	"Michelangeli": "Arturo Benedetti Michelangeli", "Hess": "Myra Hess", "Curzon": "Clifford Curzon",
	"Bernstein": "Leonard Bernstein", "Arrau": "Claudio Arrau", "Waller": "Fats Waller",
	"Jamal": "Ahmad Jamal", "Cziffra": "György Cziffra", "Cutner": "Solomon",
	"de Larrocha": "Alicia de Larrocha", "Wild": "Earl Wild", "Shearing": "George Shearing",

	// Whole tokens naming more than one musician.
	"BrownPeterson":            "Ray Brown & Oscar Peterson",
	"SerkinSerkin":             "Rudolf Serkin & Peter Serkin",
	"ShearingQuintet":          "George Shearing Quintet",
	"BudPowellTrio":            "Bud Powell Trio",
	"OPAndFriends":             "Oscar Peterson & Friends",
	"AstairesGershwin":         "Fred & Adele Astaire with George Gershwin",
	"EvansGomezMorell":         "Bill Evans, Eddie Gómez & Marty Morell",
	"LewisJerryLee":            "Jerry Lee Lewis",
	"BarrientosDeFalla":        "Maria Barrientos & Manuel de Falla",
	"BallardEllingtonMarshall": "Ballard, Ellington & Marshall",
}

func performerName(tok string) string {
	if f, ok := performers[tok]; ok {
		return f
	}
	parts := splitNames(tok)
	if n := len(parts); n >= 2 {
		// "GlennGould" is one person: first name, then a known surname.
		if f, ok := performers[parts[n-1]]; ok && strings.HasPrefix(f, strings.Join(parts[:n-1], " ")+" ") {
			return f
		}
	}
	return joinNames(tok, " & ", performers)
}

var (
	opusNoRE  = regexp.MustCompile(`Op(\d+)_(\d+)`)
	opusRE    = regexp.MustCompile(`Op(\d+)`)
	catNoRE   = regexp.MustCompile(`\b(BWV|K|D|Hob|S|L)(\d+)_(\d+)\b`)
	catRE     = regexp.MustCompile(`\b(BWV|K|D)(\d+)\b`)
	varRE     = regexp.MustCompile(`\bVar\s*0*(\d+)`)
	mvtRE     = regexp.MustCompile(`(\d)\s*mvt\s*(\d+)`)
	goldRE    = regexp.MustCompile(`^GV\d+(.*)$`)
	trackRE   = regexp.MustCompile(`^\d{1,3}[-_]`)
	keyRE     = regexp.MustCompile(`[-_ ]([A-G])(flat|sharp|Sh|Fl|b)?(Maj|Min|maj|min)$`)
	wordRE    = regexp.MustCompile(`\b(Inv|Sin|ET)\b`)
	letDigRE  = regexp.MustCompile(`([a-z])(\d)`)
	workWords = map[string]string{"Inv": "Invention", "Sin": "Sinfonia", "ET": "Étude-tableau"}
)

// prettyWork turns the work part of a Zenph name into a title:
// "PreludeOp3_2" → "Prelude Op. 3 No. 2", "GV26Var25" → "Goldberg
// Variations: Variation 25", "07-Inv14-BflatMaj" → "Invention 14 in B-flat
// major". All-capital abbreviations such as "STWOM" stay as they are;
// `sepianod tag` expands them from catalogs.
func prettyWork(s string) string {
	if m := goldRE.FindStringSubmatch(s); m != nil {
		rest := prettyWork(strings.TrimLeft(m[1], "_-"))
		if rest == "" {
			return "Goldberg Variations"
		}
		return "Goldberg Variations: " + rest
	}
	s = trackRE.ReplaceAllString(s, "") // a running number in front
	key := ""
	if k := keyRE.FindStringSubmatch(s); k != nil {
		acc := map[string]string{"flat": "-flat", "Fl": "-flat", "b": "-flat", "sharp": "-sharp", "Sh": "-sharp"}[k[2]]
		key = " in " + k[1] + acc + " " + strings.ToLower(k[3]) + "or"
		s = strings.TrimSuffix(s, k[0])
	}
	s = opusNoRE.ReplaceAllString(s, " Op. $1 No. $2 ")
	s = opusRE.ReplaceAllString(s, " Op. $1 ")
	s = catNoRE.ReplaceAllString(s, " $1. $2/$3 ")
	s = catRE.ReplaceAllString(s, " $1. $2 ")
	s = strings.ReplaceAll(s, "BWV.", "BWV")
	s = strings.NewReplacer("_", " ", "-", " ").Replace(s)
	s = mvtRE.ReplaceAllString(s, "$1, Movement $2")
	s = varRE.ReplaceAllString(s, " Variation $1 ")
	s = letDigRE.ReplaceAllString(s, "$1 $2")
	s = pretty(s)
	s = wordRE.ReplaceAllStringFunc(s, func(w string) string { return workWords[w] })
	return s + key
}

// pretty turns "CarnegieShortSnippet" into "Carnegie Short Snippet".
func pretty(s string) string {
	s = opusNoRE.ReplaceAllString(s, "Op. $1 No. $2")
	s = strings.ReplaceAll(s, "_", " ")
	var b strings.Builder
	r := []rune(s)
	for i, c := range r {
		if i > 0 {
			p := r[i-1]
			next := rune(0)
			if i+1 < len(r) {
				next = r[i+1]
			}
			switch {
			case unicode.IsUpper(c) && unicode.IsLower(p):
				b.WriteRune(' ') // camelCase boundary
			case unicode.IsUpper(c) && unicode.IsUpper(p) && unicode.IsLower(next):
				b.WriteRune(' ') // "JSBach" -> "JS Bach"
			}
		}
		b.WriteRune(c)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

var (
	pianistRE  = regexp.MustCompile(`(?i)^(.+?),\s*(?:pianists?|piano)$`)
	playedByRE = regexp.MustCompile(`(?i)^played by (.+)$`)
	recordedRE = regexp.MustCompile(`(?i)^recorded\s+(.+?)(?:\s+(?:in|at)\s+(.+))?$`)
	issuedRE   = regexp.MustCompile(`(?i)^ampico\s+\S+\s+\(issued\s+(.+)\)$`)
	keyValRE   = regexp.MustCompile(`^(Ampico Roll Number|Ampico Roll Title|Roll Number|Roll Title|Title|Composer|Pianist|Performer|Date):\s*(.*)$`)
	workRE     = regexp.MustCompile(`^([^:]{2,60}?):\s+(.+)$`)
	rollDateRE = regexp.MustCompile(`^(\d{1,2})-(\d{2})$`) // month-year on Ampico rolls
)

// ParseHeader reads the text a .kb file carries, in the styles found in the
// collection:
//
//	Mozart: Sonata No. 12 in F Major, K.332 / First Movement (Allegro) /
//	Philippe Entremont, pianist / Recorded February 12, 1990 in Vienna
//
//	Toot, Toot, Tootsie! Goo'Bye / Played by The Original Piano Trio /
//	Ampico 203091 (issued December, 1922)
//
//	Ampico Roll Number: 206421 / Ampico Roll Title: Manhattan / Composer:
//	Rodgers / Pianist: Zez Confrey / Date: 9-25
//
// Copyright lines are skipped; a header holding nothing else (the newer
// Zenph files) gives an empty Meta.
func ParseHeader(text string) Meta {
	var m Meta
	var content, notes []string
	for _, line := range strings.FieldsFunc(text, func(r rune) bool { return r == '\r' || r == '\n' }) {
		line = strings.TrimSpace(line)
		low := strings.ToLower(line)
		switch {
		case line == "", strings.Contains(low, "copyright"), strings.Contains(low, "all rights reserved"):
			continue
		}
		if kv := keyValRE.FindStringSubmatch(line); kv != nil {
			v := strings.TrimSpace(kv[2])
			switch kv[1] {
			case "Ampico Roll Title", "Roll Title", "Title":
				m.Title = v
			case "Composer":
				m.Composer = v
			case "Pianist", "Performer":
				m.Performer = v
			case "Ampico Roll Number", "Roll Number":
				notes = append(notes, "Ampico roll "+v)
			case "Date":
				if d := rollDateRE.FindStringSubmatch(v); d != nil {
					mo, _ := strconv.Atoi(d[1])
					if mo >= 1 && mo <= 12 {
						m.RecordedOn = fmt.Sprintf("19%s-%02d", d[2], mo)
					}
				}
				notes = append(notes, "Roll date "+v)
			}
			continue
		}
		if p := pianistRE.FindStringSubmatch(line); p != nil {
			m.Performer = p[1]
			continue
		}
		if p := playedByRE.FindStringSubmatch(line); p != nil {
			m.Performer = p[1]
			continue
		}
		if r := recordedRE.FindStringSubmatch(line); r != nil {
			if d := ParseDate(r[1]); d != "" {
				m.RecordedOn = d
			}
			notes = append(notes, line)
			continue
		}
		if r := issuedRE.FindStringSubmatch(line); r != nil {
			if d := ParseDate(r[1]); d != "" && m.RecordedOn == "" {
				m.RecordedOn = d
			}
			notes = append(notes, line)
			continue
		}
		content = append(content, line)
	}
	if len(content) > 0 && m.Title == "" {
		first := content[0]
		if w := workRE.FindStringSubmatch(first); w != nil && m.Composer == "" && !strings.ContainsAny(w[1], "0123456789") {
			m.Composer, first = w[1], w[2]
		}
		m.Title = strings.Join(append([]string{first}, content[1:]...), " — ")
	}
	m.Notes = strings.Join(notes, ". ")
	return m
}

// ParseDate reads "February 12, 1990", "June, 1955", "December 1922" or
// "1955" into YYYY-MM-DD, YYYY-MM or YYYY.
func ParseDate(s string) string {
	s = strings.TrimSpace(strings.TrimSuffix(s, "."))
	for _, layout := range []string{"January 2, 2006", "January 2 2006", "Jan. 2, 2006", "Jan 2, 2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02")
		}
	}
	for _, layout := range []string{"January, 2006", "January 2006", "Jan. 2006", "Jan 2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01")
		}
	}
	if t, err := time.Parse("2006", s); err == nil {
		return t.Format("2006")
	}
	return ""
}

// Initials returns the first letters of a title's words, for matching the
// abbreviations in Zenph names: "Someone to Watch over Me" → "STWOM".
func Initials(title string) string {
	var b strings.Builder
	for _, w := range strings.FieldsFunc(title, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '\''
	}) {
		r := []rune(w)
		b.WriteRune(unicode.ToUpper(r[0]))
	}
	return b.String()
}
