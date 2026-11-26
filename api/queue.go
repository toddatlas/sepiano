package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"sepiano/deck"
)

// Up next: pieces, albums and play lists queued while the piano plays. When
// a performance finishes by itself, the first one starts (a few seconds, as
// the piano takes the player again). Stop, or choosing something else to
// play, leaves the queue as it is. It lives in memory: a restart of the
// service empties it.

// upNextItem is one thing queued: a piece, an album or a play list.
type upNextItem struct {
	Kind   string `json:"kind"` // piece, album or playlist
	ID     string `json:"id"`   // piece or play list id, or album name
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"` // composer · performer, or "12 tracks"
}

func (it upNextItem) request() playRequest {
	switch it.Kind {
	case "album":
		return playRequest{Album: it.ID}
	case "playlist":
		return playRequest{Playlist: it.ID}
	}
	return playRequest{Piece: it.ID}
}

// MaxUpNext is how many things can wait in the queue.
const MaxUpNext = 200

func (s *Server) upNextList() []upNextItem {
	s.unMu.Lock()
	defer s.unMu.Unlock()
	return append([]upNextItem{}, s.upNext...)
}

// upNextChanged tells the apps (event "queue").
func (s *Server) upNextChanged() {
	s.Deck.Publish(deck.Event{Type: "queue", Data: map[string]any{"items": s.upNextList()}})
}

func (s *Server) getUpNext(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"items": s.upNextList()})
}

// addUpNext queues a piece, album or play list (the body is as for
// /deck/play, without start): at the end, or first with ?next=1. When the
// piano is idle it plays at once.
func (s *Server) addUpNext(w http.ResponseWriter, r *http.Request) {
	var req playRequest
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if len(req.Pieces) > 0 {
		fail(w, 400, "queue one piece, album or play list at a time")
		return
	}
	ids, _, err := s.resolve(req)
	if err != nil {
		playError(w, err)
		return
	}
	it := upNextItem{}
	switch {
	case req.Piece != "":
		p, _ := s.Lib.Get(req.Piece)
		it = upNextItem{Kind: "piece", ID: p.ID, Title: p.Title, Detail: strings.Join(nonEmpty(p.Composer, p.Performer), " · ")}
	case req.Album != "":
		it = upNextItem{Kind: "album", ID: req.Album, Title: req.Album, Detail: tracksText(len(ids))}
	default:
		pl, _, _ := s.Lib.GetPlaylist(req.Playlist)
		it = upNextItem{Kind: "playlist", ID: pl.ID, Title: pl.Name, Detail: tracksText(len(ids))}
	}
	if st := s.Deck.Status().State; st == deck.Idle {
		// Nothing plays: play it now.
		ctx, cancel := cmdContext(r)
		defer cancel()
		ids, coll, _ := s.resolve(req)
		if err := s.startQueue(ctx, ids, coll, 0, 0); err != nil {
			playError(w, err)
			return
		}
		writeJSON(w, 202, map[string]any{"items": s.upNextList(), "started": true})
		return
	}
	s.unMu.Lock()
	if len(s.upNext) >= MaxUpNext {
		s.unMu.Unlock()
		fail(w, 409, "up next is full")
		return
	}
	if r.URL.Query().Get("next") == "1" {
		s.upNext = append([]upNextItem{it}, s.upNext...) // play next, before the rest
	} else {
		s.upNext = append(s.upNext, it)
	}
	s.unMu.Unlock()
	s.upNextChanged()
	writeJSON(w, 200, map[string]any{"items": s.upNextList(), "started": false})
}

// removeUpNext takes one thing out of the queue, by its place (0 first).
// kind and id must match what is there, so a stale tap removes nothing else.
func (s *Server) removeUpNext(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.PathValue("n"))
	q := r.URL.Query()
	s.unMu.Lock()
	if err != nil || n < 0 || n >= len(s.upNext) ||
		q.Get("id") != "" && (s.upNext[n].ID != q.Get("id") || s.upNext[n].Kind != q.Get("kind")) {
		s.unMu.Unlock()
		fail(w, 404, "no such item in up next")
		return
	}
	s.upNext = append(s.upNext[:n:n], s.upNext[n+1:]...)
	s.unMu.Unlock()
	s.upNextChanged()
	writeJSON(w, 200, map[string]any{"items": s.upNextList()})
}

// startUpNext plays the first thing waiting, when the piano is idle (after
// Stop, say).
func (s *Server) startUpNext(w http.ResponseWriter, r *http.Request) {
	if s.Deck.Status().State != deck.Idle {
		fail(w, 409, "the piano is busy; up next starts when the piece playing ends")
		return
	}
	if len(s.upNextList()) == 0 {
		fail(w, 409, "nothing is waiting in up next")
		return
	}
	s.playUpNext()
	writeJSON(w, 202, s.Deck.Status())
}

func (s *Server) clearUpNext(w http.ResponseWriter, r *http.Request) {
	s.unMu.Lock()
	s.upNext = nil
	s.unMu.Unlock()
	s.upNextChanged()
	writeJSON(w, 200, map[string]any{"items": []upNextItem{}})
}

// playUpNext starts the first thing queued; the deck calls it when a
// performance finishes by itself. Something gone from the library is
// skipped.
func (s *Server) playUpNext() {
	for {
		s.unMu.Lock()
		if len(s.upNext) == 0 {
			s.unMu.Unlock()
			return
		}
		it := s.upNext[0]
		s.upNext = s.upNext[1:]
		s.unMu.Unlock()
		s.upNextChanged()
		ids, coll, err := s.resolve(it.request())
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			err = s.startQueue(ctx, ids, coll, 0, 0)
			cancel()
		}
		if err == nil {
			return
		}
		s.logf("up next: %s: %v", it.Title, err)
		s.Deck.Publish(deck.Event{Type: "notice", Data: deck.Notice{Level: "warn", Text: "Up next: could not play " + it.Title + ": " + err.Error()}})
	}
}

func nonEmpty(ss ...string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func tracksText(n int) string {
	if n == 1 {
		return "1 track"
	}
	return strconv.Itoa(n) + " tracks"
}
