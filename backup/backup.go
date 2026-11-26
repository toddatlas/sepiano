// Package backup writes everything sepianod keeps into one zip file, and
// reads such a file back (docs/BACKUPS.md).
//
// The zip holds the data folder as it is, under "sepiano/", with a
// manifest ("sepiano/manifest.json") listing every file with its size and
// SHA-256, so a backup can be checked before anything is restored from
// it. Temporary files are left out (Skip).
package backup

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Format is the backup layout's version.
const Format = 1

// Root is the folder every entry sits in.
const Root = "sepiano/"

// ManifestName is the manifest's entry name.
const ManifestName = Root + "manifest.json"

// Manifest describes a backup.
type Manifest struct {
	Format  int       `json:"format"`
	Created time.Time `json:"created"`
	Version string    `json:"version"` // sepianod's
	Pieces  int       `json:"pieces"`  // performances (.kb files) in library/
	Bytes   int64     `json:"bytes"`   // all files, before compression
	Files   []File    `json:"files"`
}

// File is one backed-up file, by its path in the data folder.
type File struct {
	Path   string `json:"path"` // slash-separated, relative to the data folder
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Skip says whether a path in the data folder (slash-separated, relative)
// is left out: joined albums (play/, made again when played), scratch
// folders (work/), and temporary or hidden files.
func Skip(rel string, dir bool) bool {
	base := path.Base(rel)
	switch {
	case strings.HasPrefix(base, "."), strings.HasSuffix(base, ".tmp"), strings.HasSuffix(base, ".part"):
		return true
	case strings.SplitN(rel, "/", 2)[0] == "play":
		return true
	case dir && base == "work":
		return true
	}
	return false
}

// Write zips everything under dataDir to w and returns the manifest it
// wrote last.
func Write(w io.Writer, dataDir, version string, now time.Time) (Manifest, error) {
	zw := zip.NewWriter(w)
	m := Manifest{Format: Format, Created: now.UTC().Truncate(time.Second), Version: version, Files: []File{}}
	err := filepath.WalkDir(dataDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dataDir, p)
		if err != nil || rel == "." {
			return err
		}
		rel = filepath.ToSlash(rel)
		if Skip(rel, d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		hw, err := zw.CreateHeader(&zip.FileHeader{Name: Root + rel, Method: zip.Deflate, Modified: info.ModTime()})
		if err != nil {
			return err
		}
		h := sha256.New()
		n, err := io.Copy(io.MultiWriter(hw, h), f)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		m.Files = append(m.Files, File{Path: rel, Size: n, SHA256: hex.EncodeToString(h.Sum(nil))})
		m.Bytes += n
		if strings.HasPrefix(rel, "library/") && strings.HasSuffix(rel, ".kb") {
			m.Pieces++
		}
		return nil
	})
	if err != nil {
		return m, err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return m, err
	}
	mw, err := zw.CreateHeader(&zip.FileHeader{Name: ManifestName, Method: zip.Deflate, Modified: now})
	if err != nil {
		return m, err
	}
	if _, err := mw.Write(b); err != nil {
		return m, err
	}
	return m, zw.Close()
}

// Archive is a backup opened for reading.
type Archive struct {
	Manifest Manifest
	files    map[string]*zip.File // by data-folder path
	listed   map[string]File
}

// ErrNotBackup means the file is not a sepiano backup.
var ErrNotBackup = errors.New("this is not a sepiano backup (it has no manifest)")

// Open reads a backup's manifest and checks that every file it lists is
// there. Read checks each file's contents.
func Open(r io.ReaderAt, size int64) (*Archive, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("not a zip file: %w", err)
	}
	a := &Archive{files: map[string]*zip.File{}, listed: map[string]File{}}
	var man *zip.File
	for _, f := range zr.File {
		if f.Name == ManifestName {
			man = f
			continue
		}
		if rel, ok := strings.CutPrefix(f.Name, Root); ok && rel != "" && !strings.HasSuffix(rel, "/") {
			a.files[rel] = f
		}
	}
	if man == nil {
		return nil, ErrNotBackup
	}
	rc, err := man.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	if err := json.NewDecoder(io.LimitReader(rc, 64<<20)).Decode(&a.Manifest); err != nil {
		return nil, fmt.Errorf("the manifest cannot be read: %w", err)
	}
	if a.Manifest.Format < 1 || a.Manifest.Format > Format {
		return nil, fmt.Errorf("backup format %d is not one this version reads (%d)", a.Manifest.Format, Format)
	}
	for _, f := range a.Manifest.Files {
		if _, ok := a.files[f.Path]; !ok {
			return nil, fmt.Errorf("the backup is incomplete: %s is missing", f.Path)
		}
		a.listed[f.Path] = f
	}
	return a, nil
}

// Paths lists the backed-up files, sorted.
func (a *Archive) Paths() []string {
	out := make([]string, 0, len(a.listed))
	for p := range a.listed {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// Read returns a backed-up file's contents, checked against the manifest.
func (a *Archive) Read(rel string) ([]byte, error) {
	want, ok := a.listed[rel]
	f := a.files[rel]
	if !ok || f == nil {
		return nil, fmt.Errorf("%s is not in the backup", rel)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, want.Size+1))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rel, err)
	}
	sum := sha256.Sum256(b)
	if int64(len(b)) != want.Size || hex.EncodeToString(sum[:]) != want.SHA256 {
		return nil, fmt.Errorf("%s is damaged in the backup", rel)
	}
	return b, nil
}
