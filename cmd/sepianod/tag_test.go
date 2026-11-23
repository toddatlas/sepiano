package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sepiano/library"
)

func TestExpandAbbreviations(t *testing.T) {
	pieces := []library.Piece{
		{ID: "a", Title: "TFT", Performer: "Art Tatum"},
		{ID: "b", Title: "TMIL", Performer: "George Gershwin", Composer: "Gershwin"},
		{ID: "c", Title: "The Man I Love", Performer: "Oscar Peterson", Composer: "George Gershwin-Ira Gershwin"},
		{ID: "d", Title: "XYZ", Performer: "Art Tatum"},
		{ID: "e", Title: "TP", Performer: "Nobody"}, // two candidates below: left alone
		{ID: "f", Title: "Tango Pequeño", Performer: "Nobody"},
		{ID: "g", Title: "Tiny Polka", Performer: "Nobody"},
		{ID: "h", Title: "SL", Performer: "Art Tatum", Locked: []string{"title"}},
	}
	albums := []library.BatAlbum{{Performer: "Art Tatum", Tracks: []library.Track{
		{Title: "Tea for Two"}, {Title: "Sophisticated Lady"}}}}
	patches := map[string]*library.Patch{}
	expandAbbreviations(pieces, albums, patches)
	want := map[string]string{"a": "Tea for Two", "b": "The Man I Love"}
	if len(patches) != len(want) {
		t.Fatalf("patched %d pieces, want %d", len(patches), len(want))
	}
	for id, title := range want {
		if p := patches[id]; p == nil || p.Title == nil || *p.Title != title {
			t.Errorf("%s: %+v, want %q", id, p, title)
		}
	}
}

func TestNameList(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "NAMES.TXT"), []byte("When I Fall In Love - 1989\r\n\r\nOld Folks \x96 February 19, 1988\r\nWho Can I Turn To \x96 1985?\r\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "TITLES.TXT"), []byte("Catalog No. FP-1001\r\n1. Chopin - Ballade\r\n"), 0o644)
	entries, _ := os.ReadDir(dir)
	got := nameList(dir, entries)
	if got["OLDFOLKS"] != [2]string{"Old Folks", "February 19, 1988"} || got["WHOCANIT"][1] != "1985" ||
		got["WHENIFAL"][0] != "When I Fall In Love" || len(got) != 3 {
		t.Fatalf("%q", got)
	}
	if d := library.ParseDate(got["OLDFOLKS"][1]); d != "1988-02-19" {
		t.Fatalf("date %q", d)
	}
}

func TestEnsureFolderBat(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Hamb")
	os.MkdirAll(dir, 0o755)
	for _, n := range []string{"Tatum19330321-Handy_StLouisBlues", "10Sec-C9-400", "Abramski20080629-Chopin_Sonata3mvt1", "55min_spacer", "Copy of Zed"} {
		for _, ext := range []string{".kb", ".lp", ".sp"} {
			os.WriteFile(filepath.Join(dir, n+ext), []byte("x"), 0o644)
		}
	}
	os.WriteFile(filepath.Join(dir, "playTatum.bat"), []byte("play Tatum19330321-Handy_StLouisBlues\r\nplay Gone-Missing\r\n"), 0o644)
	var report []string
	check := func(write bool) bool {
		entries, _ := os.ReadDir(dir)
		wrote, err := ensureFolderBat(root, dir, entries, setsIn(entries), nil, false, false, folderOpts{writeBats: write, report: &report})
		if err != nil {
			t.Fatal(err)
		}
		return wrote
	}
	if check(false) {
		t.Fatal("wrote without -write-bats")
	}
	if !check(true) {
		t.Fatalf("did not write: %q", report)
	}
	b, _ := os.ReadFile(filepath.Join(dir, folderBat))
	want := folderBatMark + "\r\nplay 10Sec-C9-400\r\nplay Abramski20080629-Chopin_Sonata3mvt1\r\nplay \"Copy of Zed\"\r\nplay Tatum19330321-Handy_StLouisBlues\r\n"
	if string(b) != want {
		t.Fatalf("play_folder.bat:\n%q\nwant\n%q", b, want)
	}
	if check(true) { // now covered by our own file
		t.Fatalf("wrote twice: %q", report)
	}
	joined := strings.Join(report, "\n")
	for _, s := range []string{"playTatum.bat refers to 1 missing file(s), e.g. GONE-MISSING", "wrote play_folder.bat (4 pieces)", "plays whole with play_folder.bat"} {
		if !strings.Contains(joined, s) {
			t.Errorf("report lacks %q:\n%s", s, joined)
		}
	}
	// A play_folder.bat the owner wrote is never replaced.
	os.WriteFile(filepath.Join(dir, folderBat), []byte("play Only-This\r\n"), 0o644)
	if check(true) {
		t.Fatal("replaced the owner's play_folder.bat")
	}
}
