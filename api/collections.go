package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"time"

	"sepiano/deck"
	"sepiano/library"
)

// queue remembers the album or play list being played, for next/previous.
type queue struct {
	ids  []string
	coll *deck.Collection // nil for a single piece
}

type playRequest struct {
	Piece    string   `json:"piece"`
	Album    string   `json:"album"`
	Playlist string   `json:"playlist"`
	Pieces   []string `json:"pieces"`
	Title    string   `json:"title"`
	Start    int      `json:"start"`
}

type httpError struct {
	code int
	msg  string
}

func (e httpError) Error() string { return e.msg }

// resolve turns a play request into piece ids and what they belong to.
func (s *Server) resolve(req playRequest) ([]string, *deck.Collection, error) {
	given := 0
	for _, v := range []bool{req.Piece != "", req.Album != "", req.Playlist != "", len(req.Pieces) > 0} {
		if v {
			given++
		}
	}
	if given != 1 {
		return nil, nil, httpError{400, "give exactly one of piece, album, playlist or pieces"}
	}
	switch {
	case req.Piece != "":
		if _, err := s.Lib.Get(req.Piece); err != nil {
			return nil, nil, httpError{404, "no such piece"}
		}
		return []string{req.Piece}, nil, nil
	case req.Album != "":
		a, err := s.Lib.Album(req.Album)
		if err != nil {
			return nil, nil, httpError{404, "no such album"}
		}
		ids := make([]string, len(a.Tracks))
		for i, t := range a.Tracks {
			ids[i] = t.Piece.ID
		}
		return ids, &deck.Collection{Kind: "album", ID: a.Name, Title: a.Name}, nil
	case req.Playlist != "":
		pl, pieces, err := s.Lib.GetPlaylist(req.Playlist)
		if err != nil {
			return nil, nil, httpError{404, "no such play list"}
		}
		if len(pieces) == 0 {
			return nil, nil, httpError{409, "this play list has no pieces in the library"}
		}
		ids := make([]string, len(pieces))
		for i, p := range pieces {
			ids[i] = p.ID
		}
		return ids, &deck.Collection{Kind: "playlist", ID: pl.ID, Title: pl.Name}, nil
	}
	for _, id := range req.Pieces {
		if _, err := s.Lib.Get(id); err != nil {
			return nil, nil, httpError{404, "no such piece: " + id}
		}
	}
	title := req.Title
	if title == "" {
		p, _ := s.Lib.Get(req.Pieces[0])
		title = p.Title
	}
	return req.Pieces, &deck.Collection{Kind: "list", Title: title}, nil
}

func ref(p library.Piece) deck.PieceRef {
	return deck.PieceRef{ID: p.ID, Title: p.Title, Composer: p.Composer, Performer: p.Performer, SourceName: p.SourceName}
}

// startQueue plays ids[start:] as one continuous performance.
func (s *Server) startQueue(ctx context.Context, ids []string, coll *deck.Collection, start int) error {
	if start < 0 || start >= len(ids) {
		return httpError{400, fmt.Sprintf("start must be between 0 and %d", len(ids)-1)}
	}
	sel := ids[start:]
	var job deck.Job
	if len(sel) == 1 {
		p, err := s.Lib.Get(sel[0])
		if err != nil {
			return httpError{404, "no such piece"}
		}
		dir, base, _ := s.Lib.Files(p.ID)
		job = deck.Job{Piece: ref(p), Dir: dir, Name: base, Duration: time.Duration(p.DurationMs) * time.Millisecond}
		if err := s.correct(&job, p); err != nil {
			return fmt.Errorf("corrections: %w", err)
		}
	} else {
		dir, err := s.playDir()
		if err != nil {
			return err
		}
		parts, _, err := s.Lib.Join(sel, dir, "q", coll.Title, s.playFix())
		if err != nil {
			return err
		}
		var total int64
		var played []library.Piece
		for _, part := range parts {
			p, _ := s.Lib.Get(part.ID)
			played = append(played, p)
			job.Tracks = append(job.Tracks, deck.TrackRef{Piece: ref(p), StartMs: part.StartMs, DurationMs: part.DurationMs})
			total = part.StartMs + part.DurationMs
		}
		job.Piece, job.Dir, job.Name = job.Tracks[0].Piece, dir, "q"
		s.markFixes(&job, played)
		job.Duration = time.Duration(total) * time.Millisecond
	}
	if err := s.voice(&job, len(sel) > 1 || job.Corrected || job.Fitted); err != nil {
		return fmt.Errorf("voicing: %w", err)
	}
	if coll != nil {
		c := *coll
		c.Start, c.Count = start, len(ids)
		job.Collection = &c
		if len(job.Tracks) == 0 {
			job.Tracks = []deck.TrackRef{{Piece: job.Piece, DurationMs: job.Duration.Milliseconds()}}
		}
	}
	if err := s.Deck.Play(ctx, job); err != nil {
		return err
	}
	s.qmu.Lock()
	s.queue = queue{ids: ids, coll: coll}
	s.qmu.Unlock()
	return nil
}

// playDir makes a fresh folder for a joined performance, removing all but
// the newest older one: the piano may still be reading the one being stopped.
func (s *Server) playDir() (string, error) {
	if s.PlayDir == "" {
		return "", errors.New("no folder configured for joined performances")
	}
	if err := os.MkdirAll(s.PlayDir, 0o755); err != nil {
		return "", err
	}
	entries, _ := os.ReadDir(s.PlayDir)
	var old []string
	for _, e := range entries {
		if e.IsDir() {
			old = append(old, e.Name())
		}
	}
	sort.Strings(old)
	for i := 0; i+1 < len(old); i++ {
		os.RemoveAll(filepath.Join(s.PlayDir, old[i]))
	}
	return os.MkdirTemp(s.PlayDir, strconv.FormatInt(time.Now().UnixNano(), 36)+"-")
}

func (s *Server) play(w http.ResponseWriter, r *http.Request) {
	var req playRequest
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, err.Error())
		return
	}
	ids, coll, err := s.resolve(req)
	if err == nil {
		ctx, cancel := cmdContext(r)
		defer cancel()
		err = s.startQueue(ctx, ids, coll, req.Start)
	}
	if err != nil {
		playError(w, err)
		return
	}
	writeJSON(w, 202, s.Deck.Status())
}

func playError(w http.ResponseWriter, err error) {
	var he httpError
	if errors.As(err, &he) {
		fail(w, he.code, he.msg)
		return
	}
	deckError(w, err)
}

// skip plays the collection again from another track.
func (s *Server) skip(next bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st := s.Deck.Status()
		s.qmu.Lock()
		q := s.queue
		s.qmu.Unlock()
		if st.Collection == nil || q.coll == nil || q.coll.Kind != st.Collection.Kind || q.coll.ID != st.Collection.ID ||
			len(st.Tracks) == 0 {
			fail(w, 409, "not playing an album or play list")
			return
		}
		abs := st.Collection.Start + st.TrackIndex
		target := abs
		if next {
			if abs+1 >= len(q.ids) {
				fail(w, 409, "this is the last track")
				return
			}
			target = abs + 1
		} else if st.PositionMs-st.Tracks[st.TrackIndex].StartMs < 5000 && abs > 0 {
			target = abs - 1 // early in a track: the one before it
		}
		ctx, cancel := cmdContext(r)
		defer cancel()
		if err := s.startQueue(ctx, q.ids, q.coll, target); err != nil {
			playError(w, err)
			return
		}
		writeJSON(w, 202, s.Deck.Status())
	}
}

func (s *Server) albums(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"albums": s.Lib.Albums()})
}

func (s *Server) album(w http.ResponseWriter, r *http.Request) {
	a, err := s.Lib.Album(r.PathValue("name"))
	if err != nil {
		fail(w, 404, "no such album")
		return
	}
	writeJSON(w, 200, a)
}

func (s *Server) playlists(w http.ResponseWriter, r *http.Request) {
	ps, err := s.Lib.Playlists()
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"playlists": ps})
}

func (s *Server) playlistReply(w http.ResponseWriter, code int, id string) {
	pl, pieces, err := s.Lib.GetPlaylist(id)
	if err != nil {
		fail(w, 404, "no such play list")
		return
	}
	var total int64
	for _, p := range pieces {
		total += p.DurationMs
	}
	if pieces == nil {
		pieces = []library.Piece{}
	}
	writeJSON(w, code, map[string]any{"id": pl.ID, "name": pl.Name, "source": pl.Source, "duration_ms": total, "pieces": pieces,
		"favorite": slices.Contains(s.Lib.Favorites().Playlists, pl.ID)})
}

func (s *Server) getPlaylist(w http.ResponseWriter, r *http.Request) {
	s.playlistReply(w, 200, r.PathValue("id"))
}

func (s *Server) createPlaylist(w http.ResponseWriter, r *http.Request) {
	var req library.Playlist
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, err.Error())
		return
	}
	req.ID = ""
	pl, err := s.Lib.SavePlaylist(req)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	s.libraryChanged("")
	s.playlistReply(w, 201, pl.ID)
}

func (s *Server) updatePlaylist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   *string  `json:"name"`
		Pieces []string `json:"pieces"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, err.Error())
		return
	}
	pl, _, err := s.Lib.GetPlaylist(r.PathValue("id"))
	if err != nil {
		fail(w, 404, "no such play list")
		return
	}
	if req.Name != nil {
		pl.Name = *req.Name
	}
	if req.Pieces != nil {
		pl.Pieces = req.Pieces
	}
	if _, err := s.Lib.SavePlaylist(pl); err != nil {
		fail(w, 400, err.Error())
		return
	}
	s.libraryChanged("")
	s.playlistReply(w, 200, pl.ID)
}

func (s *Server) deletePlaylist(w http.ResponseWriter, r *http.Request) {
	if err := s.Lib.DeletePlaylist(r.PathValue("id")); errors.Is(err, library.ErrNotFound) {
		fail(w, 404, "no such play list")
		return
	} else if err != nil {
		fail(w, 500, err.Error())
		return
	}
	s.libraryChanged("")
	w.WriteHeader(204)
}

// addToPlaylist appends pieces to a play list, skipping those already in it.
func (s *Server) addToPlaylist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Pieces []string `json:"pieces"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, err.Error())
		return
	}
	pl, added, err := s.Lib.AddToPlaylist(r.PathValue("id"), req.Pieces)
	if errors.Is(err, library.ErrNotFound) {
		fail(w, 404, "no such play list")
		return
	} else if err != nil {
		fail(w, 400, err.Error())
		return
	}
	if added > 0 {
		s.libraryChanged("")
	}
	w.Header().Set("X-Added", strconv.Itoa(added))
	s.playlistReply(w, 200, pl.ID)
}
