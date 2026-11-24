// Package piano keeps the piano's stored settings: backups of its EEPROM
// lookup table, a plain-language view of a table, and the differences
// between two of them (docs/SETTINGS-EDITOR.md).
package piano

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"sepiano/lut"
)

// Store keeps backups of the table, one file each:
// DIR/YYYYMMDD-HHMMSS-reason.lut, in local time. Nothing is ever deleted.
type Store struct {
	Dir string
	mu  sync.Mutex
}

// Backup describes one stored table.
type Backup struct {
	ID      string    `json:"id"`
	Time    time.Time `json:"time"`
	Reason  string    `json:"reason"` // manual, imported (later: before-write, after-write)
	Size    int       `json:"size"`
	SHA256  string    `json:"sha256"`
	Model   string    `json:"model,omitempty"`
	Version string    `json:"version,omitempty"`
	Valid   bool      `json:"valid"`
	Problem string    `json:"problem,omitempty"`
}

// ErrNotFound means there is no such backup (or none at all).
var ErrNotFound = errors.New("no such backup")

var idRE = regexp.MustCompile(`^(\d{8}-\d{6})(?:-(\d+))?-([a-z]+(?:-[a-z]+)*)$`)

const stamp = "20060102-150405"

// Save stores an image taken at t, and returns its description.
func (s *Store) Save(img []byte, reason string, t time.Time) (Backup, error) {
	if !regexp.MustCompile(`^[a-z]+(-[a-z]+)*$`).MatchString(reason) {
		return Backup{}, fmt.Errorf("bad reason %q", reason)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return Backup{}, err
	}
	base := t.Local().Format(stamp)
	id := base + "-" + reason
	for n := 2; ; n++ {
		if _, err := os.Stat(filepath.Join(s.Dir, id+".lut")); os.IsNotExist(err) {
			break
		}
		id = fmt.Sprintf("%s-%d-%s", base, n, reason)
	}
	path := filepath.Join(s.Dir, id+".lut")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, img, 0o644); err != nil {
		return Backup{}, err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return Backup{}, err
	}
	return describe(id, img), nil
}

// describe builds a backup's description from its id and contents.
func describe(id string, img []byte) Backup {
	m := idRE.FindStringSubmatch(id)
	b := Backup{ID: id, Size: len(img)}
	if m != nil {
		b.Time, _ = time.ParseInLocation(stamp, m[1], time.Local)
		b.Reason = m[3]
	}
	sum := sha256.Sum256(img)
	b.SHA256 = hex.EncodeToString(sum[:])
	t, err := lut.Parse(img)
	switch {
	case err != nil:
		b.Problem = err.Error()
	case len(img) != lut.Size:
		b.Problem = fmt.Sprintf("%d bytes, not %d", len(img), lut.Size)
	default:
		b.Valid = true
	}
	if t != nil {
		b.Model = t.Name
		b.Version = fmt.Sprintf("%02X.%02X", t.Version>>8, t.Version&0xFF)
		if t.Version != lut.Version && b.Problem == "" {
			b.Problem = "table version is not 30.17"
		}
	}
	return b
}

// List returns every backup, newest first.
func (s *Store) List() ([]Backup, error) {
	entries, err := os.ReadDir(s.Dir)
	if os.IsNotExist(err) {
		return []Backup{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Backup{}
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".lut")
		if !ok || e.IsDir() || !idRE.MatchString(id) {
			continue
		}
		img, err := os.ReadFile(filepath.Join(s.Dir, e.Name()))
		if err != nil {
			continue
		}
		out = append(out, describe(id, img))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

// Get returns a backup's image and description.
func (s *Store) Get(id string) ([]byte, Backup, error) {
	if !idRE.MatchString(id) {
		return nil, Backup{}, ErrNotFound
	}
	img, err := os.ReadFile(filepath.Join(s.Dir, id+".lut"))
	if os.IsNotExist(err) {
		return nil, Backup{}, ErrNotFound
	}
	if err != nil {
		return nil, Backup{}, err
	}
	return img, describe(id, img), nil
}

// Latest returns the newest backup.
func (s *Store) Latest() ([]byte, Backup, error) {
	list, err := s.List()
	if err != nil {
		return nil, Backup{}, err
	}
	if len(list) == 0 {
		return nil, Backup{}, ErrNotFound
	}
	return s.Get(list[0].ID)
}

// Path is where a backup's file is.
func (s *Store) Path(id string) string { return filepath.Join(s.Dir, id+".lut") }
