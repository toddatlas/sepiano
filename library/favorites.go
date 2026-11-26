package library

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
)

// Favorites are the starred albums (by name) and play lists (by id), kept in
// favorites.json beside the play lists. A starred piece carries its own
// Piece.Favorite. The app lists favorites first and has a Favorites section.
type Favorites struct {
	Albums    []string `json:"albums"`
	Playlists []string `json:"playlists"`
}

// Kinds of favorite for SetFavorite.
const (
	FavoritePiece    = "piece"
	FavoriteAlbum    = "album"
	FavoritePlaylist = "playlist"
)

func (l *Library) favoritesFile() string {
	return strings.TrimSuffix(l.settings, "settings.json") + "favorites.json"
}

func (l *Library) loadFavorites() (Favorites, error) {
	f := Favorites{Albums: []string{}, Playlists: []string{}}
	b, err := os.ReadFile(l.favoritesFile())
	if errors.Is(err, os.ErrNotExist) {
		return f, nil
	} else if err != nil {
		return f, err
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return Favorites{Albums: []string{}, Playlists: []string{}}, fmt.Errorf("favorites.json: %w", err)
	}
	if f.Albums == nil {
		f.Albums = []string{}
	}
	if f.Playlists == nil {
		f.Playlists = []string{}
	}
	return f, nil
}

func (l *Library) saveFavorites(f Favorites) error {
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(l.favoritesFile(), append(b, '\n'))
}

// Favorites returns the starred albums and play lists. A damaged file reads
// as none, so the library still lists everything.
func (l *Library) Favorites() Favorites {
	l.favMu.Lock()
	defer l.favMu.Unlock()
	f, _ := l.loadFavorites()
	return f
}

// SetFavorite stars (on) or un-stars a piece (by id), an album (by name) or
// a play list (by id).
func (l *Library) SetFavorite(kind, key string, on bool) error {
	switch kind {
	case FavoritePiece:
		_, err := l.Update(key, Patch{Favorite: &on})
		return err
	case FavoriteAlbum:
		if _, err := l.Album(key); err != nil && on {
			return err
		}
	case FavoritePlaylist:
		if _, _, err := l.GetPlaylist(key); err != nil && on {
			return err
		}
	default:
		return fmt.Errorf("no such kind of favorite %q (piece, album or playlist)", kind)
	}
	l.favMu.Lock()
	defer l.favMu.Unlock()
	f, err := l.loadFavorites()
	if err != nil {
		return err
	}
	list := &f.Albums
	if kind == FavoritePlaylist {
		list = &f.Playlists
	}
	has := slices.Contains(*list, key)
	switch {
	case on && !has:
		*list = append(*list, key)
	case !on && has:
		*list = slices.DeleteFunc(*list, func(s string) bool { return s == key })
	default:
		return nil
	}
	return l.saveFavorites(f)
}

// MergeFavorites adds the favorites in f that are not starred here yet (a
// restore never un-stars anything) and returns how many it added.
func (l *Library) MergeFavorites(f Favorites) (int, error) {
	l.favMu.Lock()
	defer l.favMu.Unlock()
	cur, err := l.loadFavorites()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, a := range f.Albums {
		if !slices.Contains(cur.Albums, a) {
			cur.Albums = append(cur.Albums, a)
			n++
		}
	}
	for _, p := range f.Playlists {
		if !slices.Contains(cur.Playlists, p) {
			cur.Playlists = append(cur.Playlists, p)
			n++
		}
	}
	if n == 0 {
		return 0, nil
	}
	return n, l.saveFavorites(cur)
}

// zenphSteinway says whether p is SE files rendered for a Steinway (the
// owner's Zenph collection): these start as favorites (the owner, 2026-11-26).
func zenphSteinway(p *Piece) bool {
	return p.Origin == OriginImport && (p.Grade == "" || p.Grade == GradeNative) &&
		strings.HasPrefix(strings.ToLower(p.Instrument), "steinway")
}
