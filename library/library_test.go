package library

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"sepiano/se"
	"sepiano/sim"
)

func TestParseName(t *testing.T) {
	for name, want := range map[string]Meta{
		"02-Rachmaninoff19280404-Rachmaninoff_PreludeOp3_2-Steinway_SED133291": {
			Title: "Prelude Op. 3 No. 2", Composer: "Rachmaninoff", Performer: "Sergei Rachmaninoff",
			RecordedOn: "1928-04-04", Instrument: "Steinway_SED133291",
			PerfKey: "rachmaninoff19280404-rachmaninoff_preludeop3_2", Track: 2},
		"Tatum19490402-Gershwin_STWOM-Boesendorfer_SE42251-590-22(20150112)-SE": {
			Title: "Someone to Watch Over Me", Composer: "Gershwin", Performer: "Art Tatum", RecordedOn: "1949-04-02",
			Instrument: "Boesendorfer_SE42251-590-22(20150112)", PerfKey: "tatum19490402-gershwin_stwom"},
		"03-Rachmaninoff19290416-Rimsky-KorsakovRachmaninoff_TFOTBB-Steinway_SED133291": {
			Title: "The Flight of the Bumble-Bee", Composer: "Rimsky-Korsakov-Rachmaninoff", Performer: "Sergei Rachmaninoff",
			RecordedOn: "1929-04-16", Instrument: "Steinway_SED133291",
			PerfKey: "rachmaninoff19290416-rimsky-korsakovrachmaninoff_tfotbb", Track: 3},
		"Gould19640319-Bach_07-Inv14-BflatMaj-Steinway_SED519960-SE": {
			Title: "Invention 14 in B-flat major", Composer: "Bach", Performer: "Glenn Gould",
			RecordedOn: "1964-03-19", Instrument: "Steinway_SED519960",
			PerfKey: "gould19640319-bach_07-inv14-bflatmaj"},
		"Peterson19853333-HanleyMacDonald_Indiana-Steinway_SED133291": {
			Title: "Indiana", Composer: "Hanley-MacDonald", Performer: "Oscar Peterson", RecordedOn: "1985",
			Instrument: "Steinway_SED133291", PerfKey: "peterson19853333-hanleymacdonald_indiana"},
		"BellHamlisch20100121-RodgersHamlisch_ITM-Boesendorfer_SE42251_590_22(20200831)-SE": {
			Title: "ITM", Composer: "Rodgers-Hamlisch", Performer: "Bell & Hamlisch", RecordedOn: "2010-01-21",
			Instrument: "Boesendorfer_SE42251_590_22(20200831)", PerfKey: "bellhamlisch20100121-rodgershamlisch_itm"},
		"GlennGould19550610-JSBach_GoldbergVariations-Yamaha_CFIIIS": {
			Title: "Goldberg Variations", Composer: "JS Bach", Performer: "Glenn Gould",
			RecordedOn: "1955-06-10", Instrument: "Yamaha_CFIIIS",
			PerfKey: "glenngould19550610-jsbach_goldbergvariations"},
		"10Sec-C9-400":             {Title: "10Sec-C9-400", PerfKey: "10sec-c9-400"},
		"CarnegieShortSnippet":     {Title: "Carnegie Short Snippet", PerfKey: "carnegieshortsnippet"},
		"Waltz-Steinway_SED133291": {Title: "Waltz", Instrument: "Steinway_SED133291", PerfKey: "waltz"},
	} {
		if got := ParseName(name); got != want {
			t.Errorf("%s:\n got %+v\nwant %+v", name, got, want)
		}
	}
}

// set returns the three files of a synthetic performance.
func set(t *testing.T, seconds int) (kb, lp, sp []byte) {
	t.Helper()
	kb, lp, sp, err := sim.Phrase(uint32(seconds * se.TicksPerSecond)).Encode()
	if err != nil {
		t.Fatal(err)
	}
	return
}

func TestAddGetDownloadRoundTrip(t *testing.T) {
	l, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	kb, lp, sp := set(t, 20)
	name := "02-Rachmaninoff19280404-Rachmaninoff_PreludeOp3_2-Steinway_SED133291"
	p, dup, err := l.AddSet(name, kb, lp, sp, OriginUpload, "")
	if err != nil || dup {
		t.Fatal(err, dup)
	}
	if len(p.ID) != 8 || !isID(p.ID) || p.Title != "Prelude Op. 3 No. 2" || p.DurationMs != 20000 || p.SourceName != name {
		t.Fatalf("%+v", p)
	}
	if _, dup, _ := l.AddSet("other", kb, lp, sp, OriginUpload, ""); !dup {
		t.Fatal("same .kb not detected as a duplicate")
	}

	var buf bytes.Buffer
	if err := l.WriteSet(&buf, p.ID); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]byte{name + ".kb": kb, name + ".lp": lp, name + ".sp": sp}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		if !bytes.Equal(b, want[f.Name]) {
			t.Errorf("%s differs after the round trip", f.Name)
		}
		delete(want, f.Name)
	}
	if len(want) != 0 {
		t.Errorf("missing from the zip: %v", want)
	}

	mid, err := l.MIDI(p.ID)
	if err != nil || !bytes.HasPrefix(mid, []byte("MThd")) {
		t.Fatalf("midi: %v", err)
	}
	// MIDI back in: converted, the original kept, and not a duplicate of the
	// source (the conversion is not byte-identical).
	q, dup, err := l.AddMIDI("prelude.mid", mid, "Prelude from MIDI")
	if err != nil || dup || q.Origin != OriginMIDI || !q.HasMIDISource || q.Title != "Prelude from MIDI" {
		t.Fatalf("add midi: %+v %v %v", q, dup, err)
	}
	if got, _ := os.ReadFile(filepath.Join(l.Dir(), q.ID+".mid")); !bytes.Equal(got, mid) {
		t.Fatal("original MIDI not kept")
	}
	if d := q.DurationMs - p.DurationMs; d < 500 || d > 2500 { // MIDI import adds a 1 s lead-in
		t.Fatalf("durations %d and %d", p.DurationMs, q.DurationMs)
	}
}

func TestAddFilesZipAndErrors(t *testing.T) {
	l, _ := Open(t.TempDir())
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	for i, n := range []string{"A-Steinway_SED133291", "A-Yamaha_CFIIIS"} {
		kb, lp, sp := set(t, 10+i)
		for ext, b := range map[string][]byte{".kb": kb, ".lp": lp, ".sp": sp} {
			w, _ := zw.Create("folder/" + n + ext)
			w.Write(b)
		}
	}
	w, _ := zw.Create("__MACOSX/._junk")
	w.Write([]byte("x"))
	zw.Close()
	kb, _, _ := set(t, 12)
	r := l.AddFiles([]File{
		{Name: "set.zip", Data: zbuf.Bytes()},
		{Name: "Lonely.kb", Data: kb},
		{Name: "notes.pdf", Data: []byte("%PDF")},
	}, OriginUpload, "ignored for many")
	if len(r.Added) != 2 || len(r.Errors) != 2 {
		t.Fatalf("%+v", r)
	}
	var errs []string
	for _, e := range r.Errors {
		errs = append(errs, e.Name+": "+e.Error)
	}
	sort.Strings(errs)
	if !strings.Contains(errs[0], "Lonely: missing .lp, .sp") || !strings.HasPrefix(errs[1], "notes.pdf") {
		t.Fatalf("errors %q", errs)
	}
	ps := l.List(Filter{})
	if len(ps) != 2 || ps[0].PerfKey != ps[1].PerfKey || ps[0].Instrument == ps[1].Instrument {
		t.Fatalf("renderings not grouped: %+v", ps)
	}
	if got := l.List(Filter{Instrument: "yamaha_cfiiis"}); len(got) != 1 {
		t.Fatalf("instrument filter: %d", len(got))
	}
	if got := l.List(Filter{Q: "steinway a"}); len(got) != 1 {
		t.Fatalf("search: %d", len(got))
	}
}

func TestRescanRebuildsIndex(t *testing.T) {
	data := t.TempDir()
	l, _ := Open(data)
	kb, lp, sp := set(t, 15)
	p, _, _ := l.AddSet("Nocturne", kb, lp, sp, OriginUpload, "")
	notes := "my favourite"
	l.Update(p.ID, Patch{Notes: &notes})

	// A set copied in by hand under its own name, and a lost .json.
	kb2, lp2, sp2 := set(t, 16)
	name := "01-Horowitz19650509-Chopin_Ballade1-Steinway_SED133291"
	for ext, b := range map[string][]byte{".kb": kb2, ".lp": lp2, ".sp": sp2} {
		os.WriteFile(filepath.Join(l.Dir(), name+ext), b, 0o644)
	}
	kb3, lp3, sp3 := set(t, 17)
	q, _, _ := l.AddSet("Etude", kb3, lp3, sp3, OriginUpload, "")
	os.Remove(filepath.Join(l.Dir(), q.ID+".json"))

	l2, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	ps := l2.List(Filter{})
	if len(ps) != 3 {
		t.Fatalf("%d pieces after rescan", len(ps))
	}
	if got, _ := l2.Get(p.ID); got.Notes != notes {
		t.Fatalf("metadata lost: %+v", got)
	}
	// The name went with the .json, so the rebuilt piece is titled by its id.
	if got, err := l2.Get(q.ID); err != nil || got.Title != q.ID || got.DurationMs != 17000 || got.KBSHA256 != q.KBSHA256 {
		t.Fatalf("lost .json not rebuilt: %+v %v", got, err)
	}
	var horowitz *Piece
	for i := range ps {
		if ps[i].SourceName == name {
			horowitz = &ps[i]
		}
	}
	if horowitz == nil || !isID(horowitz.ID) || horowitz.Composer != "Chopin" || horowitz.Title != "Ballade 1" ||
		horowitz.Performer != "Vladimir Horowitz" {
		t.Fatalf("hand-copied set: %+v", horowitz)
	}
	if _, err := os.Stat(filepath.Join(l.Dir(), name+".kb")); !os.IsNotExist(err) {
		t.Fatal("hand-copied files not renamed to the id")
	}
	if err := l2.Delete(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := l2.Get(p.ID); err != ErrNotFound {
		t.Fatal("deleted piece still there")
	}
	if n, _ := l2.Rescan(); n != 2 {
		t.Fatalf("%d pieces after delete and rescan", n)
	}
}

func TestSettingsAndExport(t *testing.T) {
	data := t.TempDir()
	l, _ := Open(data)
	if s := l.Settings(); s.PreferredInstrument != DefaultInstrument || s.Level != 0 {
		t.Fatalf("defaults %+v", s)
	}
	if err := l.SaveSettings(Settings{PreferredInstrument: "Yamaha_CFIIIS", Level: -2}); err != nil {
		t.Fatal(err)
	}
	if err := l.SaveSettings(Settings{Level: 1}); err == nil {
		t.Fatal("level above 0 accepted")
	}
	l2, _ := Open(data)
	if s := l2.Settings(); s.PreferredInstrument != "Yamaha_CFIIIS" || s.Level != -2 {
		t.Fatalf("reloaded %+v", s)
	}
	kb, lp, sp := set(t, 5)
	p, _, _ := l2.AddSet("x", kb, lp, sp, OriginUpload, "")
	var buf bytes.Buffer
	if err := l2.Export(&buf); err != nil {
		t.Fatal(err)
	}
	zr, _ := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	for _, want := range []string{"sepiano-library/settings.json", "sepiano-library/library/" + p.ID + ".kb", "sepiano-library/library/" + p.ID + ".json"} {
		if !names[want] {
			t.Errorf("export lacks %s (has %v)", want, names)
		}
	}
}

func TestUpdateRejectsEmptyTitle(t *testing.T) {
	l, _ := Open(t.TempDir())
	kb, lp, sp := set(t, 5)
	p, _, _ := l.AddSet("x", kb, lp, sp, OriginUpload, "")
	empty := " "
	if _, err := l.Update(p.ID, Patch{Title: &empty}); err == nil {
		t.Fatal("empty title accepted")
	}
}

func TestParseHeader(t *testing.T) {
	for text, want := range map[string]Meta{
		"Mozart: Sonata No. 12 in F Major, K.332\r\nFirst Movement (Allegro)\r\nPhilippe Entremont, pianist\r\nRecorded February 12, 1990 in Vienna": {
			Title: "Sonata No. 12 in F Major, K.332 — First Movement (Allegro)", Composer: "Mozart",
			Performer: "Philippe Entremont", RecordedOn: "1990-02-12", Notes: "Recorded February 12, 1990 in Vienna"},
		"Bach: The Goldberg Variations\r\nGlenn Gould, pianist\r\nRecorded June, 1955 in New York\r\nVariation 30: Quodlibet\r\nCopyright (c) Zenph(R) Studios, Inc. 2002-2008\r\nAll rights reserved": {
			Title: "The Goldberg Variations — Variation 30: Quodlibet", Composer: "Bach", Performer: "Glenn Gould",
			RecordedOn: "1955-06", Notes: "Recorded June, 1955 in New York"},
		"Toot, Toot, Tootsie! Goo'Bye\r\nPlayed by The Original Piano Trio\r\nAmpico 203091 (issued December, 1922)": {
			Title: "Toot, Toot, Tootsie! Goo'Bye", Performer: "The Original Piano Trio", RecordedOn: "1922-12",
			Notes: "Ampico 203091 (issued December, 1922)"},
		"Ampico Roll Number: 206421\r\nAmpico Roll Title: Manhattan (from \"Garrick Gaieties\")\r\nComposer: Rodgers\r\nPianist: Zez Confrey\r\nDate: 9-25\r\nCopyright (c) 1993 by Wayne Stahnke": {
			Title: "Manhattan (from \"Garrick Gaieties\")", Composer: "Rodgers", Performer: "Zez Confrey",
			RecordedOn: "1925-09", Notes: "Ampico roll 206421. Roll date 9-25"},
		"Copyright (c) Zenph Sound Innovations, Inc. 2002-2011.  All Rights Reserved.  File created Mon Jul 11 14:41:33 2011": {},
	} {
		if got := ParseHeader(text); got != want {
			t.Errorf("%q:\n got %+v\nwant %+v", text, got, want)
		}
	}
	if s := Initials("Someone to Watch over Me"); s != "STWOM" {
		t.Errorf("initials %q", s)
	}
}

func TestDescribePrefersHeader(t *testing.T) {
	m := Describe("K332_1-Steinway_SED133291", "Mozart: Sonata No. 12 in F Major, K.332\r\nPhilippe Entremont, pianist")
	if m.Title != "Sonata No. 12 in F Major, K.332" || m.Composer != "Mozart" || m.Instrument != "Steinway_SED133291" ||
		m.PerfKey != "philippe entremont|mozart|sonata no. 12 in f major, k.332" {
		t.Fatalf("%+v", m)
	}
}

// Pieces described by an older parser are described again when the library
// opens; fields set by hand stay as they were.
func TestMetadataMigrationKeepsLockedFields(t *testing.T) {
	data := t.TempDir()
	l, _ := Open(data)
	kb, lp, sp := set(t, 5)
	a, _, _ := l.AddSet("Gould19550616-Bach_GV07Var6-Steinway_SED519960-SE", kb, lp, sp, OriginImport, "")
	kb2, lp2, sp2 := set(t, 6)
	b, _, _ := l.AddSet("Gould19550616-Bach_GV08Var7-Steinway_SED519960-SE", kb2, lp2, sp2, OriginImport, "")
	mine := "My favourite variation"
	album := "Sunday favourites"
	l.Update(b.ID, Patch{Title: &mine, Album: &album})

	// Make both look as an old parser left them.
	for _, id := range []string{a.ID, b.ID} {
		path := filepath.Join(l.Dir(), id+".json")
		raw, _ := os.ReadFile(path)
		var p Piece
		json.Unmarshal(raw, &p)
		p.MetaVersion, p.Performer, p.Composer = 0, "Gould", ""
		out, _ := json.Marshal(p)
		os.WriteFile(path, out, 0o644)
	}
	l2, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	ga, _ := l2.Get(a.ID)
	gb, _ := l2.Get(b.ID)
	if ga.Title != "Goldberg Variations: Variation 6" || ga.Performer != "Glenn Gould" || ga.Composer != "Bach" || ga.MetaVersion != metaVersion {
		t.Fatalf("not described again: %+v", ga)
	}
	if gb.Title != mine || gb.Album != album || gb.Performer != "Glenn Gould" {
		t.Fatalf("locked fields lost: %+v", gb)
	}
	if got := l2.List(Filter{Q: "sunday"}); len(got) != 1 || got[0].ID != b.ID {
		t.Fatalf("search by album: %+v", got)
	}
}

func TestFullNamesFromHeaders(t *testing.T) {
	pieces := map[string]*Piece{
		"a": {Performer: "Earl Wild"},
		"b": {Performer: "Wild"},
		"c": {Performer: "Larkin"},
		"d": {Performer: "Abe Lincoln"}, "e": {Performer: "Mary Lincoln"}, "f": {Performer: "Lincoln"},
		"g": {Performer: "Wild", Locked: []string{"performer"}},
	}
	fullNames(pieces)
	if pieces["b"].Performer != "Earl Wild" || pieces["c"].Performer != "Larkin" ||
		pieces["f"].Performer != "Lincoln" || pieces["g"].Performer != "Wild" {
		t.Fatalf("b %q c %q f %q g %q", pieces["b"].Performer, pieces["c"].Performer, pieces["f"].Performer, pieces["g"].Performer)
	}
}
