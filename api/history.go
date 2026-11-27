package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"sepiano/deck"
)

// Play history: every piece the piano starts playing, newest first, so the
// owner can find something heard and liked again (asked 2026-11-27). Kept
// in history.json in the data folder (and so in backups). Entries can be
// deleted one by one, or all at once.

// MaxHistory is how many entries are kept; the oldest go first.
const MaxHistory = 2000

// historyRepeat: the same piece starting again within this time (a seek, a
// restart) is not a new entry.
const historyRepeat = 10 * time.Minute

// historyGroupGap: tracks of the same album or play list played within
// this time of each other, with nothing else between, are one group.
const historyGroupGap = 3 * time.Hour

type historyEntry struct {
	ID        string    `json:"id"`
	At        time.Time `json:"at"`
	Piece     string    `json:"piece"`
	Title     string    `json:"title"`
	Composer  string    `json:"composer,omitempty"`
	Performer string    `json:"performer,omitempty"`
	// From: the album or play list it was played from, if any.
	FromKind  string `json:"from_kind,omitempty"` // album or playlist
	FromID    string `json:"from_id,omitempty"`
	FromTitle string `json:"from_title,omitempty"`
	// Group: plays of one album or play list in a row share it (the id of
	// the first), so the history can show them as one row.
	Group string `json:"group,omitempty"`
}

type history struct {
	path    string // "": in memory only
	mu      sync.Mutex
	entries []historyEntry // oldest first
	loaded  bool
}

func (h *history) load() {
	if h.loaded {
		return
	}
	h.loaded = true
	if h.path == "" {
		return
	}
	b, err := os.ReadFile(h.path)
	if err != nil {
		return
	}
	var es []historyEntry
	if json.Unmarshal(b, &es) == nil {
		h.entries = es
	}
}

func (h *history) save() error {
	if h.path == "" {
		return nil
	}
	b, err := json.MarshalIndent(h.entries, "", " ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(h.path), 0o755); err != nil {
		return err
	}
	tmp := h.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, h.path)
}

// add records a piece starting, unless it is the same piece as the last
// entry, started again within historyRepeat. It reports whether it added.
func (h *history) add(e historyEntry) (bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.load()
	if n := len(h.entries); n > 0 {
		last := h.entries[n-1]
		if last.Piece == e.Piece && e.At.Sub(last.At) < historyRepeat {
			return false, nil
		}
	}
	b := make([]byte, 6)
	rand.Read(b)
	e.ID = "h" + hex.EncodeToString(b)
	if e.FromKind != "" {
		e.Group = e.ID
		if n := len(h.entries); n > 0 {
			if last := h.entries[n-1]; last.FromKind == e.FromKind && last.FromID == e.FromID && e.At.Sub(last.At) < historyGroupGap {
				e.Group = last.Group
				if e.Group == "" {
					e.Group = last.ID
				}
			}
		}
	}
	h.entries = append(h.entries, e)
	if len(h.entries) > MaxHistory {
		h.entries = append([]historyEntry(nil), h.entries[len(h.entries)-MaxHistory:]...)
	}
	return true, h.save()
}

// list returns the entries, newest first.
func (h *history) list() []historyEntry {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.load()
	out := make([]historyEntry, len(h.entries))
	for i, e := range h.entries {
		out[len(out)-1-i] = e
	}
	return out
}

var errNoEntry = errors.New("no such entry in the history")

func (h *history) remove(id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.load()
	for i, e := range h.entries {
		if e.ID == id {
			h.entries = append(h.entries[:i:i], h.entries[i+1:]...)
			return h.save()
		}
	}
	return errNoEntry
}

// removeGroup takes out every entry of a group (an album or play list
// played through).
func (h *history) removeGroup(group string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.load()
	kept := h.entries[:0:0]
	for _, e := range h.entries {
		if e.Group != group && e.ID != group {
			kept = append(kept, e)
		}
	}
	if len(kept) == len(h.entries) {
		return errNoEntry
	}
	h.entries = kept
	return h.save()
}

func (h *history) clear() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.load()
	h.entries = nil
	return h.save()
}

// restore takes a backup's history when this Pi has none.
func (h *history) restore(b []byte) (bool, error) {
	var es []historyEntry
	if err := json.Unmarshal(b, &es); err != nil {
		return false, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.load()
	if len(h.entries) > 0 {
		return false, nil
	}
	h.entries = es
	return true, h.save()
}

// hist returns the play history (in history.json beside the library).
func (s *Server) hist() *history {
	s.histOnce.Do(func() {
		s.histStore = &history{}
		if s.DataDir != "" {
			s.histStore.path = filepath.Join(s.DataDir, "history.json")
		}
	})
	return s.histStore
}

// watchHistory adds an entry each time a piece starts playing (status
// events: playing, with a piece; an album's tracks one by one).
func (s *Server) watchHistory() {
	ch, _ := s.Deck.Subscribe()
	go func() {
		last := ""
		for ev := range ch {
			st, ok := ev.Data.(deck.Status)
			if ev.Type != "status" || !ok {
				continue
			}
			if st.State != deck.Playing || st.Piece == nil {
				if st.State == deck.Idle {
					last = ""
				}
				continue
			}
			if st.Piece.ID == last {
				continue
			}
			last = st.Piece.ID
			e := historyEntry{At: time.Now().UTC().Truncate(time.Second), Piece: st.Piece.ID, Title: st.Piece.Title,
				Composer: st.Piece.Composer, Performer: st.Piece.Performer}
			if c := st.Collection; c != nil && (c.Kind == "album" || c.Kind == "playlist") {
				e.FromKind, e.FromID, e.FromTitle = c.Kind, c.ID, c.Title
			}
			added, err := s.hist().add(e)
			if err != nil {
				s.logf("history: %v", err)
			}
			if added {
				s.Deck.Publish(deck.Event{Type: "history", Data: map[string]string{"id": e.Piece}})
			}
		}
	}()
}

func (s *Server) getHistory(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"entries": s.hist().list()})
}

// deleteHistoryEntry takes out one entry, or with ?group=1 its whole group
// (an album or play list played through).
func (s *Server) deleteHistoryEntry(w http.ResponseWriter, r *http.Request) {
	remove := s.hist().remove
	if r.URL.Query().Get("group") == "1" {
		remove = s.hist().removeGroup
	}
	if err := remove(r.PathValue("id")); err != nil {
		code := 500
		if errors.Is(err, errNoEntry) {
			code = 404
		}
		fail(w, code, err.Error())
		return
	}
	s.Deck.Publish(deck.Event{Type: "history", Data: map[string]string{}})
	writeJSON(w, 200, map[string]any{"entries": s.hist().list()})
}

func (s *Server) clearHistory(w http.ResponseWriter, r *http.Request) {
	if err := s.hist().clear(); err != nil {
		fail(w, 500, err.Error())
		return
	}
	s.Deck.Publish(deck.Event{Type: "history", Data: map[string]string{}})
	writeJSON(w, 200, map[string]any{"entries": []historyEntry{}})
}
