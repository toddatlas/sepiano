package library

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFavorites(t *testing.T) {
	data := t.TempDir()
	l, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	kb, lp, sp := set(t, 5)
	zenph, _, _ := l.AddSet("Gould19550616-Bach_GV07Var6-Steinway_SED519960-SE", kb, lp, sp, OriginImport, "")
	kb2, lp2, sp2 := set(t, 6)
	yamaha, _, _ := l.AddSet("Gould19550616-Bach_GV08Var7-Yamaha_CFIIIS-SE", kb2, lp2, sp2, OriginImport, "")
	kb3, lp3, sp3 := set(t, 7)
	take, _, _ := l.AddSet("take", kb3, lp3, sp3, OriginRecorded, "A take")
	if !zenph.Favorite || yamaha.Favorite || take.Favorite {
		t.Fatalf("new pieces: only the Steinway rendering starts as a favorite: %v %v %v", zenph.Favorite, yamaha.Favorite, take.Favorite)
	}
	// The owner's files came in by upload (sepianod import).
	kb4, lp4, sp4 := set(t, 8)
	if up, _, _ := l.AddSet("Wild19700101-Liszt_Etude-Steinway_SED133291-SE", kb4, lp4, sp4, OriginUpload, ""); !up.Favorite {
		t.Fatal("an uploaded Steinway rendering did not start as a favorite")
	}

	// Pieces: star, un-star; the owner's choice survives reopening.
	if err := l.SetFavorite(FavoritePiece, zenph.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := l.SetFavorite(FavoritePiece, take.ID, true); err != nil {
		t.Fatal(err)
	}
	l2, _ := Open(data)
	if p, _ := l2.Get(zenph.ID); p.Favorite {
		t.Fatal("an un-starred Steinway rendering was starred again on reopening")
	}
	if p, _ := l2.Get(take.ID); !p.Favorite || len(p.Locked) != 0 {
		t.Fatalf("starred recording: %+v", p)
	}

	// Albums and play lists.
	album := "Sunday"
	l2.Update(yamaha.ID, Patch{Album: &album})
	if err := l2.SetFavorite(FavoriteAlbum, album, true); err != nil {
		t.Fatal(err)
	}
	if err := l2.SetFavorite(FavoriteAlbum, "No such album", true); err == nil {
		t.Fatal("starred an album that does not exist")
	}
	pl, err := l2.SavePlaylist(Playlist{Name: "Evening", Pieces: []string{take.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if err := l2.SetFavorite(FavoritePlaylist, pl.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := l2.SetFavorite("tune", "x", true); err == nil {
		t.Fatal("accepted a kind of favorite that does not exist")
	}
	if a, _ := l2.Album(album); !a.Favorite {
		t.Fatal("album not starred")
	}
	for _, a := range l2.Albums() {
		if a.Favorite != (a.Name == album) {
			t.Fatalf("albums: %+v", a)
		}
	}
	if ps, _ := l2.Playlists(); len(ps) != 1 || !ps[0].Favorite {
		t.Fatalf("play lists: %+v", ps)
	}
	l2.SetFavorite(FavoriteAlbum, album, false)
	if f := l2.Favorites(); len(f.Albums) != 0 || len(f.Playlists) != 1 {
		t.Fatalf("favorites: %+v", f)
	}

	// A restore adds favorites, never removes them.
	n, err := l2.MergeFavorites(Favorites{Albums: []string{album}, Playlists: []string{pl.ID}})
	if err != nil || n != 1 {
		t.Fatalf("merge: %d %v", n, err)
	}

	// Pieces from before favorites: the Steinway renderings are starred once.
	path := filepath.Join(l2.Dir(), yamaha.ID+".json")
	raw, _ := os.ReadFile(path)
	var p Piece
	json.Unmarshal(raw, &p)
	p.MetaVersion, p.Instrument = favoritesSince-1, "Steinway_SED519960"
	out, _ := json.Marshal(p)
	os.WriteFile(path, out, 0o644)
	l3, _ := Open(data)
	if q, _ := l3.Get(yamaha.ID); !q.Favorite {
		t.Fatal("a Steinway rendering from before favorites was not starred")
	}
	if q, _ := l3.Get(zenph.ID); q.Favorite {
		t.Fatal("the owner's un-starring was undone")
	}
}

// A starred rendering is the one an album plays.
func TestFavoriteRenderingPlays(t *testing.T) {
	l, _ := Open(t.TempDir())
	kb, lp, sp := set(t, 5)
	a, _, _ := l.AddSet("Gould19550616-Bach_GV07Var6-Steinway_SED519960-SE", kb, lp, sp, OriginImport, "")
	kb2, lp2, sp2 := set(t, 6)
	b, _, _ := l.AddSet("Gould19550616-Bach_GV07Var6-Yamaha_CFIIIS-SE", kb2, lp2, sp2, OriginImport, "")
	album := "Goldberg"
	track := 8
	for _, id := range []string{a.ID, b.ID} {
		l.Update(id, Patch{Album: &album, Track: &track})
	}
	if al, _ := l.Album(album); al.Tracks[0].Piece.ID != a.ID {
		t.Fatal("the Steinway rendering should play")
	}
	l.SetFavorite(FavoritePiece, a.ID, false)
	l.SetFavorite(FavoritePiece, b.ID, true)
	if al, _ := l.Album(album); al.Tracks[0].Piece.ID != b.ID {
		t.Fatal("the starred rendering should play")
	}
}
