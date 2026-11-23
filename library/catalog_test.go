package library

import "testing"

func TestParseAlbumBat(t *testing.T) {
	a, ok := ParseAlbumBat("\r\nrem Live Performance\r\nrem Philippe Entremont plays Mozart Piano Sonatas, Volume 1\r\n" +
		"rem Catalog No. FP-1003\r\nrem Copyright (c) 1992 Wayne Stahnke Associates\r\n\r\n" +
		"rem Sonata No. 4 in E Flat Major, K.282\r\nrem     1. First Movement (Adagio)\r\nrem     2. Second Movement: Menuetto I and II\r\n" +
		"rem Sonata No. 5 in G Major, K.283\r\nrem     3. First Movement (Allegro)\r\n\r\n" +
		"kls2bit k282_1\r\nkls2bit k282_2\r\nkls2bit k283_1\r\n\r\nbit2wav k282_1\r\nbit2wav k282_2\r\nbit2wav k283_1\r\n")
	if !ok || a.Catalog != "FP-1003" || a.Performer != "Philippe Entremont" || len(a.Tracks) != 3 ||
		a.Title != "Philippe Entremont plays Mozart Piano Sonatas, Volume 1" {
		t.Fatalf("%+v", a)
	}
	want := []Track{
		{N: 1, File: "K282_1", Title: "Sonata No. 4 in E Flat Major, K.282 — First Movement (Adagio)"},
		{N: 2, File: "K282_2", Title: "Sonata No. 4 in E Flat Major, K.282 — Second Movement: Menuetto I and II"},
		{N: 3, File: "K283_1", Title: "Sonata No. 5 in G Major, K.283 — First Movement (Allegro)"},
	}
	for i, w := range want {
		if a.Tracks[i] != w {
			t.Errorf("track %d: %+v, want %+v", i+1, a.Tracks[i], w)
		}
	}

	// Composer in each track, a later album line, Ampico roll numbers.
	b, _ := ParseAlbumBat("rem Live Performance Catalog No. JZ-2006\nrem Oscar Peterson: Unmistakable\n" +
		"rem Copyright (c) Zenph\nrem 1. Body and Soul\nrem 2. 7249 Palmgren: Bird Song\nkls2bit bodysoul\nkls2bit 7249_2\n")
	if b.Title != "Oscar Peterson: Unmistakable" || b.Performer != "Oscar Peterson" || b.Catalog != "JZ-2006" ||
		b.Tracks[1] != (Track{N: 2, File: "7249_2", Composer: "Palmgren", Title: "Bird Song", Roll: "7249"}) {
		t.Fatalf("%+v", b)
	}

	// A heading with a composer; a pianist line; a two-line album title.
	c, _ := ParseAlbumBat("rem Live Performance\nrem Bach: The Goldberg Variations\nrem Glenn Gould, pianist\n" +
		"rem Catalog No. IP-5005\nrem  1. Aria\nrem  2. Variation 3: Canon on the unison\nkls2bit aria\nkls2bit var3\n")
	if c.Performer != "Glenn Gould" || c.Tracks[1].Title != "Variation 3: Canon on the unison" || c.Tracks[1].Composer != "Bach" {
		t.Fatalf("%+v", c)
	}
	d, _ := ParseAlbumBat("rem Live Performance\nrem Frederick Moyer in Recital\nrem Catalog No. FP-1011\n" +
		"rem Mussorgsky: Pictures at an Exhibition\nrem  8. First Promenade\nkls2bit prom1\n")
	if d.Performer != "Frederick Moyer" || d.Tracks[0] != (Track{N: 8, File: "PROM1", Composer: "Mussorgsky", Title: "Pictures at an Exhibition — First Promenade"}) {
		t.Fatalf("%+v", d)
	}
	e, _ := ParseAlbumBat("rem Live Performance\nrem The Original Piano Trio\nrem The Complete Recordings, Vol. 1\nrem Catalog No. MH-4007\nrem 1. 60943  Tambourin Chinois\nkls2bit 60943\n")
	if e.Title != "The Original Piano Trio: The Complete Recordings, Vol. 1" || e.Tracks[0].Roll != "60943" || e.Name() != e.Title+" (MH-4007)" {
		t.Fatalf("%+v", e)
	}
	if tr, ok := a.TrackFor("@k282_2"); !ok || tr.N != 2 {
		t.Fatalf("variant lookup: %+v %v", tr, ok)
	}
	if _, ok := ParseAlbumBat("rem Scale Hunting Song to avoid harsh quality\ncopy @hunting.kb huntings.kb\n"); ok {
		t.Fatal("a helper batch file taken for an album")
	}
}
