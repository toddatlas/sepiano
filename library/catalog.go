package library

import (
	"regexp"
	"strconv"
	"strings"
)

// BatAlbum is a recording catalogued by one of the suite's album batch files
// (e.g. WAYNE\FP1002\_FP1002.BAT): its title and performer, its catalog
// number, and its tracks in order.
type BatAlbum struct {
	Title     string // "Earl Wild: The Art of the Transcription"
	Performer string // "Earl Wild", when the header says
	Catalog   string // "FP-1002"
	Tracks    []Track
}

// Track is one numbered entry of an album.
type Track struct {
	N        int
	File     string // base name, upper case, from the commands that follow
	Composer string
	Title    string
	Roll     string // Ampico roll number, when listed
}

var (
	catalogRE  = regexp.MustCompile(`(?i)catalog no\.?\s*([A-Z]{1,3}-?\d+)`)
	trackRE2   = regexp.MustCompile(`^(\d{1,3})\.\s+(?:(\d{4,6})\s+)?(.+)$`)
	playsRE    = regexp.MustCompile(`^(.+?)\s+(?:plays|in Recital|Live in Concert)\b`)
	albumPerf  = regexp.MustCompile(`^([^:]{3,40}):\s+.+$`)
	fileCmdRE  = regexp.MustCompile(`(?i)^(?:kls2bit|play)\s+(\S+)`)
	inParensRE = regexp.MustCompile(`\(([^(),]+),\s*pianists?\)`)
	hasLetters = regexp.MustCompile(`[A-Za-z]`)
)

// ParseAlbumBat reads an album batch file. ok is false for batch files that
// describe no album (no catalog number or no tracks).
func ParseAlbumBat(text string) (a BatAlbum, ok bool) {
	var heading, headComposer, albumComposer string
	var headerDone bool
	var files []string
	var albumLines []string
	for _, raw := range strings.FieldsFunc(text, func(r rune) bool { return r == '\r' || r == '\n' }) {
		line := strings.TrimSpace(raw)
		low := strings.ToLower(line)
		if !strings.HasPrefix(low, "rem") {
			if m := fileCmdRE.FindStringSubmatch(line); m != nil {
				files = append(files, strings.ToUpper(strings.TrimSuffix(strings.TrimSuffix(m[1], ".*"), ".kb")))
			}
			continue
		}
		line = strings.TrimSpace(line[3:])
		low = strings.ToLower(line)
		if line == "" || !hasLetters.MatchString(line) {
			continue
		}
		if c := catalogRE.FindStringSubmatch(line); c != nil {
			a.Catalog = strings.ToUpper(c[1])
			line = strings.TrimSpace(catalogRE.ReplaceAllString(line, ""))
			low = strings.ToLower(line)
		}
		switch {
		case low == "" || strings.HasPrefix(low, "live performance") && len(low) <= len("live performance"),
			strings.Contains(low, "copyright"), strings.Contains(low, "all rights reserved"),
			strings.HasPrefix(low, "recorded"):
			continue
		}
		if t := trackRE2.FindStringSubmatch(line); t != nil {
			if !headerDone && a.Performer != "" && len(albumLines) > 0 {
				// "Bach: The Goldberg Variations" over "Glenn Gould, pianist":
				// the album names its composer.
				if w := workRE.FindStringSubmatch(albumLines[0]); w != nil && composerLike(w[1]) && w[1] != a.Performer {
					albumComposer = w[1]
				}
			}
			headerDone = true
			n, _ := strconv.Atoi(t[1])
			tr := Track{N: n, Roll: t[2], Composer: headComposer}
			if tr.Composer == "" {
				tr.Composer = albumComposer
			}
			text := strings.TrimSpace(t[3])
			if w := workRE.FindStringSubmatch(text); w != nil && heading == "" && composerLike(w[1]) {
				tr.Composer, text = w[1], w[2]
			}
			if heading != "" {
				text = heading + " — " + text
			}
			tr.Title = text
			a.Tracks = append(a.Tracks, tr)
			continue
		}
		if p := pianistRE.FindStringSubmatch(line); p != nil {
			a.Performer = p[1]
			continue
		}
		// The album title comes before the catalog number (or first, when the
		// number shares the "Live Performance" line); later lines are headings.
		if !headerDone && (len(albumLines) == 0 || a.Catalog == "") {
			albumLines = append(albumLines, line)
			continue
		}
		// A heading over the tracks that follow: "Debussy: Suite bergamasque".
		heading, headComposer = line, ""
		if w := workRE.FindStringSubmatch(line); w != nil && composerLike(w[1]) {
			headComposer, heading = w[1], w[2]
		}
	}
	a.Title = strings.Join(albumLines, ": ")
	if a.Performer == "" && len(albumLines) > 0 {
		first := albumLines[0]
		if m := inParensRE.FindStringSubmatch(a.Title); m != nil { // "Old Fashioned Love (Jim Turner, pianist)"
			a.Performer = m[1]
		} else if m := playsRE.FindStringSubmatch(first); m != nil {
			a.Performer = m[1]
		} else if m := albumPerf.FindStringSubmatch(first); m != nil {
			a.Performer = m[1]
		}
	}
	// Files appear in track order, once per command block.
	seen := map[string]bool{}
	var order []string
	for _, f := range files {
		if !seen[f] {
			seen[f] = true
			order = append(order, f)
		}
	}
	for i := range a.Tracks {
		if i < len(order) {
			a.Tracks[i].File = order[i]
		}
	}
	return a, a.Catalog != "" && len(a.Tracks) > 0
}

var notComposer = regexp.MustCompile(`(?i)\b(movement|variation|part|book|act|scene|theme|finale|canon|aria|no\.)\b`)

// composerLike tells "Chopin-Liszt" in "Chopin-Liszt: The Maiden's Wish"
// from "Second Movement" in "Second Movement: Menuetto".
func composerLike(s string) bool {
	return !strings.ContainsAny(s, "0123456789") && !notComposer.MatchString(s)
}

// TrackFor finds the track for a performance file's base name. Variants
// such as "@HUNTING", "_VAR1" or "XSUITE1" (rescaled or re-made copies)
// belong to the track of the name without the mark.
func (a BatAlbum) TrackFor(base string) (Track, bool) {
	name := strings.ToUpper(base)
	for _, cand := range []string{name, strings.TrimLeft(name, "@_"), strings.TrimPrefix(strings.TrimLeft(name, "@_"), "X")} {
		for _, t := range a.Tracks {
			if t.File != "" && t.File == cand {
				return t, true
			}
		}
	}
	return Track{}, false
}

// Name is the album as shown: title and catalog number.
func (a BatAlbum) Name() string {
	if a.Catalog == "" {
		return a.Title
	}
	return a.Title + " (" + a.Catalog + ")"
}
