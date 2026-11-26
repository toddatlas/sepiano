package api

import (
	"errors"
	"net/http"

	"sepiano/deck"
	"sepiano/edit"
	"sepiano/se"
)

// seekRequest: play the track playing now from position_ms into its piece
// (a point chosen on the progress bar). piece, if given, must be the piece
// playing, so a late tap never moves the next track.
type seekRequest struct {
	Piece      string `json:"piece"`
	PositionMs int64  `json:"position_ms"`
}

// seek plays the current track again from a point, and the rest of its
// album or play list after it. The piano takes the player again (a few
// seconds), as for Next.
func (s *Server) seek(w http.ResponseWriter, r *http.Request) {
	var req seekRequest
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, err.Error())
		return
	}
	st := s.Deck.Status()
	if st.State != deck.Playing && st.State != deck.Paused || st.Piece == nil {
		fail(w, 409, "nothing is playing")
		return
	}
	if req.Piece != "" && req.Piece != st.Piece.ID {
		fail(w, 409, "another piece is playing now")
		return
	}
	p, err := s.Lib.Get(st.Piece.ID)
	if err != nil {
		fail(w, 409, "nothing from the library is playing")
		return
	}
	if req.PositionMs < 0 || req.PositionMs >= p.DurationMs {
		fail(w, 400, "the position is outside the piece")
		return
	}
	s.qmu.Lock()
	q := s.queue
	s.qmu.Unlock()
	ids, coll, start := []string{p.ID}, (*deck.Collection)(nil), 0
	if st.Collection != nil && q.coll != nil && q.coll.Kind == st.Collection.Kind && q.coll.ID == st.Collection.ID {
		ids, coll, start = q.ids, q.coll, st.Collection.Start+st.TrackIndex
		if start < 0 || start >= len(ids) || ids[start] != p.ID {
			fail(w, 409, "the album or play list has changed")
			return
		}
	}
	// Less than a second in: from the beginning, with no lead-in.
	from := req.PositionMs
	if from < int64(PreviewLead)*1000/se.TicksPerSecond {
		from = 0
	}
	ctx, cancel := cmdContext(r)
	defer cancel()
	if err := s.startQueue(ctx, ids, coll, start, from); err != nil {
		playError(w, err)
		return
	}
	writeJSON(w, 202, s.Deck.Status())
}

// playFrom cuts a performance to start at fromMs, after PreviewLead of
// silence in which the pedals take their places.
func playFrom(perf *se.Performance, fromMs int64) (*se.Performance, error) {
	d := edit.FromPerformance(perf)
	tick := uint32(fromMs * se.TicksPerSecond / 1000)
	if tick >= d.End {
		return nil, errors.New("the position is past the end of the piece")
	}
	return d.From(tick, PreviewLead).Performance()
}
