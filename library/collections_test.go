package library

import (
	"path/filepath"
	"testing"

	"sepiano/se"
	"sepiano/sim"
)

func TestJoinPerformances(t *testing.T) {
	a := sim.Phrase(5 * se.TicksPerSecond)   // 4000 ticks
	b := sim.Phrase(3*se.TicksPerSecond + 7) // not a multiple of 16
	j, starts, err := JoinPerformances([]*se.Performance{a, b}, "Two\r\n")
	if err != nil {
		t.Fatal(err)
	}
	bEnd := (b.KB.End + 15) / 16 * 16
	if starts[0] != 0 || starts[1] != 4000 || j.KB.End != 4000+bEnd {
		t.Fatalf("starts %v end %d", starts, j.KB.End)
	}
	if na, nb, nj := len(a.KB.Notes()), len(b.KB.Notes()), len(j.KB.Notes()); nj != na+nb {
		t.Fatalf("notes %d + %d != %d", na, nb, nj)
	}
	first := b.KB.Notes()[0]
	found := false
	for _, e := range j.KB.Notes() {
		if e.Tick == first.Tick+4000 && e.Key == first.Key && e.On == first.On && e.Vel == first.Vel {
			found = true
		}
	}
	if !found {
		t.Fatal("second part's first note not shifted by the first part's length")
	}
	if len(j.Damper.Samples) != int(j.KB.End/se.DamperPeriod) || len(j.Soft.Samples) != int(j.KB.End/se.SoftPeriod) {
		t.Fatalf("pedal samples %d/%d for end %d", len(j.Damper.Samples), len(j.Soft.Samples), j.KB.End)
	}
	// The pedal of the second part starts where its keys start.
	if j.Damper.Samples[4000/se.DamperPeriod] != b.Damper.Samples[0] {
		t.Fatal("damper stream misaligned")
	}
	// It must survive an encode and a parse, as the player will read it.
	dir := t.TempDir()
	if err := j.Save(filepath.Join(dir, "J")); err != nil {
		t.Fatal(err)
	}
	back, err := se.Load(filepath.Join(dir, "J"))
	if err != nil || back.KB.End != j.KB.End || len(back.KB.Notes()) != len(j.KB.Notes()) {
		t.Fatalf("round trip: %v", err)
	}
}

func TestAlbumsPreferSteinwayAndJoin(t *testing.T) {
	l, _ := Open(t.TempDir())
	add := func(name string, seconds int) Piece {
		kb, lp, sp, _ := sim.Phrase(uint32(seconds * se.TicksPerSecond)).Encode()
		p, _, err := l.AddSet(name, kb, lp, sp, OriginImport, "")
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	add("Gould19550616-Bach_GV01Aria-Boesendorfer_SE42251-590-22(20150112)-SE", 10)
	aria := add("Gould19550616-Bach_GV01Aria-Steinway_SED519960-SE", 11)
	add("Gould19550616-Bach_GV02Var1-Boesendorfer_SE42251-590-22(20150112)-SE", 12)
	add("Gould19550616-Bach_GV02Var1-Yamaha_CFIIIS", 13)
	v2 := add("Gould19550616-Bach_GV03Var2-Steinway_SED519960-SE", 14)
	add("Gould19550616-Bach_GV03Var2-Steinway_SED133291", 15)

	sums := l.Albums()
	if len(sums) != 1 || sums[0].Name != "Bach: The Goldberg Variations (Glenn Gould, 1955)" || sums[0].Tracks != 3 {
		t.Fatalf("albums %+v", sums)
	}
	a, err := l.Album(sums[0].Name)
	if err != nil {
		t.Fatal(err)
	}
	// Steinway wins where there is one; Bösendorfer and Yamaha rank alike.
	if a.Tracks[0].Piece.Instrument != "Steinway_SED519960" || a.Tracks[2].Piece.Instrument != "Steinway_SED133291" ||
		Rank(a.Tracks[1].Piece.Instrument, DefaultInstrument) != 4 {
		t.Fatalf("renderings chosen: %q, %q, %q", a.Tracks[0].Piece.Instrument, a.Tracks[1].Piece.Instrument, a.Tracks[2].Piece.Instrument)
	}
	if a.Tracks[0].Piece.ID != aria.ID || a.Tracks[0].Renderings != 2 || a.Tracks[2].Track != 3 || a.Performer != "Glenn Gould" {
		t.Fatalf("%+v", a)
	}
	_ = v2

	parts, fixed, err := l.Join([]string{a.Tracks[0].Piece.ID, a.Tracks[2].Piece.ID}, filepath.Join(t.TempDir(), "q"), "Q", "Goldberg", nil)
	if err != nil || fixed {
		t.Fatal(err, fixed)
	}
	if len(parts) != 2 || parts[0].StartMs != 0 || parts[1].StartMs != 11000 || parts[1].DurationMs != 15000 {
		t.Fatalf("parts %+v", parts)
	}
}

func TestPlaylists(t *testing.T) {
	l, _ := Open(t.TempDir())
	kb, lp, sp, _ := sim.Phrase(5 * se.TicksPerSecond).Encode()
	p, _, _ := l.AddSet("one", kb, lp, sp, OriginImport, "")
	pl, err := l.SavePlaylist(Playlist{Name: "Evening", Source: "SE/Hamb/playGould55.bat", Pieces: []string{p.ID, "gone1234"}})
	if err != nil || pl.ID == "" {
		t.Fatal(err)
	}
	got, pieces, err := l.GetPlaylist(pl.ID)
	if err != nil || got.Name != "Evening" || len(pieces) != 1 || pieces[0].ID != p.ID {
		t.Fatalf("%+v %+v %v", got, pieces, err)
	}
	sums, _ := l.Playlists()
	if len(sums) != 1 || sums[0].Tracks != 1 || sums[0].DurationMs != 5000 {
		t.Fatalf("%+v", sums)
	}
	pl.Name = "Late evening"
	if _, err := l.SavePlaylist(pl); err != nil {
		t.Fatal(err)
	}
	if err := l.DeletePlaylist(pl.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := l.GetPlaylist(pl.ID); err != ErrNotFound {
		t.Fatal("deleted play list still there")
	}
	if _, err := l.SavePlaylist(Playlist{Name: " "}); err == nil {
		t.Fatal("a play list without a name was saved")
	}
}

func TestRank(t *testing.T) {
	const pref = "Steinway_SED133291"
	for inst, want := range map[string]int{
		"Steinway_SED133291": 0, "Steinway_SED133291_2011NovTemp": 1, "Steinway_SED519960": 2,
		"": 3, "Boesendorfer_SE42251-590-22(20150112)": 4, "Yamaha_CFIIIS": 4,
	} {
		if got := Rank(inst, pref); got != want {
			t.Errorf("%q: %d, want %d", inst, got, want)
		}
	}
}
