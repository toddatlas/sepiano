package main

import (
	"os"
	"path/filepath"
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
	albums := []library.Album{{Performer: "Art Tatum", Tracks: []library.Track{
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
