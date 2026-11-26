package api

import (
	"errors"
	"net/http"

	"sepiano/library"
)

// favoriteRequest stars or un-stars a piece (id), an album (its name) or a
// play list (id).
type favoriteRequest struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Favorite bool   `json:"favorite"`
}

// getFavorites returns the starred albums and play lists (starred pieces
// carry `favorite` in the piece list).
func (s *Server) getFavorites(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.Lib.Favorites())
}

func (s *Server) setFavorite(w http.ResponseWriter, r *http.Request) {
	var req favoriteRequest
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if req.ID == "" {
		fail(w, 400, "say which piece, album or play list (id)")
		return
	}
	if err := s.Lib.SetFavorite(req.Kind, req.ID, req.Favorite); err != nil {
		code := 400
		if errors.Is(err, library.ErrNotFound) {
			code = 404
		}
		fail(w, code, err.Error())
		return
	}
	writeJSON(w, 200, req)
}
