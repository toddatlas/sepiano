package library

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Rank orders the renderings of one performance for this piano: lower is
// better. The owner's instrument (the preferred one) comes first, then its
// variants (e.g. "Steinway_SED133291_2011NovTemp"), then any Steinway, then
// files made for no instrument in particular, then the rest: renderings for
// other pianos were adjusted to play well on those, not on a Steinway.
func Rank(instrument, preferred string) int {
	switch {
	case preferred != "" && strings.EqualFold(instrument, preferred):
		return 0
	case preferred != "" && strings.HasPrefix(strings.ToLower(instrument), strings.ToLower(preferred)):
		return 1
	case strings.HasPrefix(strings.ToLower(instrument), "steinway"):
		return 2
	case instrument == "":
		return 3
	}
	return 4
}

// better reports whether a is the rendering to play rather than b.
func better(a, b *Piece, preferred string) bool {
	if ra, rb := Rank(a.Instrument, preferred), Rank(b.Instrument, preferred); ra != rb {
		return ra < rb
	}
	// Prefer a file that describes itself, then the plain name over marked
	// copies ("_VAR1", "@HUNTING"), then a stable order.
	if ha, hb := ParseHeader(a.HeaderText).Title != "", ParseHeader(b.HeaderText).Title != ""; ha != hb {
		return ha
	}
	marked := func(p *Piece) bool {
		return strings.HasPrefix(p.SourceName, "_") || strings.HasPrefix(p.SourceName, "@")
	}
	if ma, mb := marked(a), marked(b); ma != mb {
		return mb
	}
	return a.ID < b.ID
}

// AlbumTrack is one track of an album, in its best rendering.
type AlbumTrack struct {
	Track      int   `json:"track"`
	Piece      Piece `json:"piece"`
	Renderings int   `json:"renderings"`
}

// Album is the pieces sharing an album name, in track order.
type Album struct {
	Name       string       `json:"name"`
	Performer  string       `json:"performer"`
	Composer   string       `json:"composer"`
	DurationMs int64        `json:"duration_ms"`
	Tracks     []AlbumTrack `json:"tracks"`
}

// AlbumSummary is an album without its tracks.
type AlbumSummary struct {
	Name       string `json:"name"`
	Performer  string `json:"performer"`
	Composer   string `json:"composer"`
	Tracks     int    `json:"tracks"`
	DurationMs int64  `json:"duration_ms"`
}

// Albums lists every album.
func (l *Library) Albums() []AlbumSummary {
	pref := l.Settings().PreferredInstrument
	l.mu.RLock()
	names := map[string]bool{}
	for _, p := range l.pieces {
		if p.Album != "" {
			names[p.Album] = true
		}
	}
	l.mu.RUnlock()
	out := make([]AlbumSummary, 0, len(names))
	for n := range names {
		a := l.album(n, pref)
		out = append(out, AlbumSummary{Name: a.Name, Performer: a.Performer, Composer: a.Composer,
			Tracks: len(a.Tracks), DurationMs: a.DurationMs})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// Album returns one album with its tracks.
func (l *Library) Album(name string) (Album, error) {
	a := l.album(name, l.Settings().PreferredInstrument)
	if len(a.Tracks) == 0 {
		return Album{}, ErrNotFound
	}
	return a, nil
}

func (l *Library) album(name, pref string) Album {
	l.mu.RLock()
	defer l.mu.RUnlock()
	// One entry per track number, or per performance for unnumbered pieces.
	type slot struct {
		best  *Piece
		count int
	}
	slots := map[string]*slot{}
	for _, p := range l.pieces {
		if p.Album != name {
			continue
		}
		key := fmt.Sprintf("%04d", p.Track)
		if p.Track == 0 {
			key = "9999 " + strings.ToLower(p.Title) + " " + p.PerfKey
		}
		s := slots[key]
		if s == nil {
			s = &slot{}
			slots[key] = s
		}
		s.count++
		if s.best == nil || better(p, s.best, pref) {
			s.best = p
		}
	}
	keys := make([]string, 0, len(slots))
	for k := range slots {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	a := Album{Name: name}
	performers, composers := map[string]int{}, map[string]int{}
	for _, k := range keys {
		s := slots[k]
		a.Tracks = append(a.Tracks, AlbumTrack{Track: s.best.Track, Piece: *s.best, Renderings: s.count})
		a.DurationMs += s.best.DurationMs
		performers[s.best.Performer]++
		composers[s.best.Composer]++
	}
	a.Performer, a.Composer = mostCommon(performers), mostCommon(composers)
	return a
}

func mostCommon(m map[string]int) string {
	best, n := "", 0
	for k, c := range m {
		if k != "" && (c > n || c == n && k < best) {
			best, n = k, c
		}
	}
	return best
}

// Playlist is an ordered list of pieces, made in the app or imported from
// a play list batch file ("play NAME" lines).
type Playlist struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Source string   `json:"source,omitempty"`
	Pieces []string `json:"pieces"`
}

// PlaylistSummary is a play list without its pieces.
type PlaylistSummary struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Source     string `json:"source,omitempty"`
	Tracks     int    `json:"tracks"`
	DurationMs int64  `json:"duration_ms"`
}

func (l *Library) playlistFile() string {
	return strings.TrimSuffix(l.settings, "settings.json") + "playlists.json"
}

func (l *Library) loadPlaylists() ([]Playlist, error) {
	b, err := os.ReadFile(l.playlistFile())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var out []Playlist
	return out, json.Unmarshal(b, &out)
}

func (l *Library) savePlaylists(ps []Playlist) error {
	b, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(l.playlistFile(), append(b, '\n'))
}

// Playlists lists the play lists, counting only pieces still in the library.
func (l *Library) Playlists() ([]PlaylistSummary, error) {
	l.plMu.Lock()
	ps, err := l.loadPlaylists()
	l.plMu.Unlock()
	if err != nil {
		return nil, err
	}
	out := make([]PlaylistSummary, 0, len(ps))
	for _, p := range ps {
		pieces := l.present(p.Pieces)
		s := PlaylistSummary{ID: p.ID, Name: p.Name, Source: p.Source, Tracks: len(pieces)}
		for _, x := range pieces {
			s.DurationMs += x.DurationMs
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

// present returns the pieces of ids that are still in the library.
func (l *Library) present(ids []string) []Piece {
	l.mu.RLock()
	defer l.mu.RUnlock()
	var out []Piece
	for _, id := range ids {
		if p, ok := l.pieces[id]; ok {
			out = append(out, *p)
		}
	}
	return out
}

// GetPlaylist returns a play list and its pieces in order.
func (l *Library) GetPlaylist(id string) (Playlist, []Piece, error) {
	l.plMu.Lock()
	ps, err := l.loadPlaylists()
	l.plMu.Unlock()
	if err != nil {
		return Playlist{}, nil, err
	}
	for _, p := range ps {
		if p.ID == id {
			return p, l.present(p.Pieces), nil
		}
	}
	return Playlist{}, nil, ErrNotFound
}

// SavePlaylist creates (empty ID) or replaces a play list.
func (l *Library) SavePlaylist(p Playlist) (Playlist, error) {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return Playlist{}, errors.New("a play list needs a name")
	}
	if p.Pieces == nil {
		p.Pieces = []string{}
	}
	l.plMu.Lock()
	defer l.plMu.Unlock()
	ps, err := l.loadPlaylists()
	if err != nil {
		return Playlist{}, err
	}
	if p.ID == "" {
		b := make([]byte, 6)
		rand.Read(b)
		p.ID = "pl" + fmt.Sprintf("%x", b)
		ps = append(ps, p)
	} else {
		found := false
		for i := range ps {
			if ps[i].ID == p.ID {
				ps[i], found = p, true
			}
		}
		if !found {
			return Playlist{}, ErrNotFound
		}
	}
	return p, l.savePlaylists(ps)
}

// DeletePlaylist removes a play list; its pieces stay.
func (l *Library) DeletePlaylist(id string) error {
	l.plMu.Lock()
	defer l.plMu.Unlock()
	ps, err := l.loadPlaylists()
	if err != nil {
		return err
	}
	for i := range ps {
		if ps[i].ID == id {
			return l.savePlaylists(append(ps[:i], ps[i+1:]...))
		}
	}
	return ErrNotFound
}
