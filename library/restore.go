package library

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"sepiano/se"
)

// RestorePiece puts back a piece from a backup: its files by extension
// (".kb", ".lp" and ".sp", and ".json" and ".mid" if it had them), with
// its metadata as it was. A piece already here (the same .kb) is left as
// it is, and added is false. The piece keeps its id unless another piece
// has taken it.
func (l *Library) RestorePiece(id string, files map[string][]byte) (p Piece, added bool, err error) {
	kb, lp, sp := files[".kb"], files[".lp"], files[".sp"]
	if kb == nil || lp == nil || sp == nil {
		return Piece{}, false, errors.New("its .kb, .lp or .sp file is missing")
	}
	k, err := se.ParseKB(kb)
	if err != nil {
		return Piece{}, false, fmt.Errorf(".kb: %w", err)
	}
	for _, b := range [][]byte{lp, sp} {
		if _, err := se.ParsePedal(b); err != nil {
			return Piece{}, false, fmt.Errorf("pedal file: %w", err)
		}
	}
	sum := sha256.Sum256(kb)
	hash := hex.EncodeToString(sum[:])
	l.mu.Lock()
	defer l.mu.Unlock()
	if have, ok := l.bySHA[hash]; ok {
		return *l.pieces[have], false, nil
	}
	taken := func(s string) bool {
		_, err := os.Stat(filepath.Join(l.dir, s+".kb"))
		return l.pieces[s] != nil || err == nil
	}
	if !isID(id) || taken(id) {
		id = newID(taken)
	}
	piece := newPiece(id, id, OriginImport, kb, k)
	if meta := files[".json"]; meta != nil {
		var saved Piece
		if err := json.Unmarshal(meta, &saved); err == nil && saved.Title != "" {
			saved.ID, saved.KBSHA256, saved.DurationMs = id, hash, piece.DurationMs
			piece = &saved
		}
	}
	if piece.Grade == "" {
		l.gradeFromData(piece, k, lp)
	}
	for ext, b := range map[string][]byte{".lp": lp, ".sp": sp, ".mid": files[".mid"], ".kb": kb} {
		if b == nil {
			continue
		}
		if err := writeAtomic(filepath.Join(l.dir, id+ext), b); err != nil {
			return Piece{}, false, err
		}
	}
	piece.HasMIDISource = files[".mid"] != nil
	if err := l.writeMeta(piece); err != nil {
		return Piece{}, false, err
	}
	l.pieces[id] = piece
	l.bySHA[hash] = id
	return *piece, true, nil
}

// gradeFromData grades a restored piece that had no grade.
func (l *Library) gradeFromData(p *Piece, k *se.KB, lp []byte) {
	if p.Origin == OriginEdited {
		p.Grade, p.GradeWhy = editedGrade(l.pieces[p.EditedFrom])
		return
	}
	damper, _ := se.ParsePedal(lp)
	p.Grade, p.GradeWhy = gradeOf(p.Origin, k, damper)
}

// MergePlaylists adds the play lists whose id is not here yet, as they
// are, and returns how many it added. Play lists already here are kept as
// they are.
func (l *Library) MergePlaylists(ps []Playlist) (int, error) {
	l.plMu.Lock()
	defer l.plMu.Unlock()
	have, err := l.loadPlaylists()
	if err != nil {
		return 0, err
	}
	ids := map[string]bool{}
	for _, p := range have {
		ids[p.ID] = true
	}
	added := 0
	for _, p := range ps {
		if p.ID == "" || p.Name == "" || ids[p.ID] {
			continue
		}
		if p.Pieces == nil {
			p.Pieces = []string{}
		}
		have = append(have, p)
		ids[p.ID] = true
		added++
	}
	if added == 0 {
		return 0, nil
	}
	return added, l.savePlaylists(have)
}

// SettingsSaved says whether settings.json exists: a restore puts back the
// backup's settings only on a Pi that has none of its own yet.
func (l *Library) SettingsSaved() bool {
	_, err := os.Stat(l.settings)
	return err == nil
}
