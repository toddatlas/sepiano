package library

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"sepiano/se"
)

// ErrNotEdited means a piece is an original: the editor never saves over
// one, only over an edited copy.
var ErrNotEdited = errors.New("only an edited copy can be saved over; the original stays as it is")

// EditedCopy describes a copy to add with AddEdited.
type EditedCopy struct {
	Title     string // default: the original's title with " (edited)"
	Note      string // default: "Edited from “…”."
	Corrected bool   // the recording corrections are in it
}

// AddEdited adds an edited copy of the piece from. The copy keeps the
// original's composer, performer, date and instrument, locked so that
// re-reading names never changes them, and says what it was made from. A
// copy of a recording made on this piano is marked as one. If the library
// already holds the same performance, that piece is returned with dup set.
func (l *Library) AddEdited(from Piece, perf *se.Performance, c EditedCopy) (p Piece, dup bool, err error) {
	kb, lp, sp, err := perf.Encode()
	if err != nil {
		return Piece{}, false, err
	}
	title := strings.TrimSpace(c.Title)
	if title == "" {
		title = from.Title + " (edited)"
	}
	note := c.Note
	if note == "" {
		note = fmt.Sprintf("Edited from “%s”.", from.Title)
	}
	p, dup, err = l.AddSet(from.SourceName, kb, lp, sp, OriginEdited, title)
	if err != nil || dup {
		return p, dup, err
	}
	none, zero := "", 0
	if p, err = l.Update(p.ID, Patch{
		Title: &title, Composer: &from.Composer, Performer: &from.Performer,
		RecordedOn: &from.RecordedOn, Instrument: &from.Instrument, Notes: &note,
		Album: &none, Track: &zero,
	}); err != nil {
		return p, false, err
	}
	p, err = l.setFlags(p.ID, from.RecordedHere(), c.Corrected)
	return p, false, err
}

// setFlags sets the recording flags of a piece.
func (l *Library) setFlags(id string, fromRecording, corrected bool) (Piece, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	p, ok := l.pieces[id]
	if !ok {
		return Piece{}, ErrNotFound
	}
	q := *p
	q.FromRecording, q.Corrected = fromRecording, corrected
	if err := l.writeMeta(&q); err != nil {
		return Piece{}, err
	}
	*p = q
	return q, nil
}

// ReplaceEdited saves perf over the edited copy id; corrected says whether
// the recording corrections are in it.
func (l *Library) ReplaceEdited(id string, perf *se.Performance, corrected bool) (Piece, error) {
	kb, lp, sp, err := perf.Encode()
	if err != nil {
		return Piece{}, err
	}
	k, err := se.ParseKB(kb)
	if err != nil {
		return Piece{}, err
	}
	sum := sha256.Sum256(kb)
	hash := hex.EncodeToString(sum[:])
	l.mu.Lock()
	defer l.mu.Unlock()
	p, ok := l.pieces[id]
	if !ok {
		return Piece{}, ErrNotFound
	}
	if p.Origin != OriginEdited {
		return Piece{}, ErrNotEdited
	}
	if other, ok := l.bySHA[hash]; ok && other != id {
		return Piece{}, fmt.Errorf("the library already holds this performance: “%s”", l.pieces[other].Title)
	}
	// The pedals first, the .kb last: the .kb is what makes the piece.
	for _, f := range []struct {
		ext string
		b   []byte
	}{{".lp", lp}, {".sp", sp}, {".kb", kb}} {
		if err := writeAtomic(filepath.Join(l.dir, id+f.ext), f.b); err != nil {
			return Piece{}, err
		}
	}
	q := *p
	q.KBSHA256 = hash
	q.DurationMs = se.TickDuration(k.End).Milliseconds()
	q.HeaderText = strings.TrimSpace(k.Text)
	q.Corrected = corrected
	if err := l.writeMeta(&q); err != nil {
		return Piece{}, err
	}
	delete(l.bySHA, p.KBSHA256)
	l.bySHA[hash] = id
	*p = q
	return q, nil
}
