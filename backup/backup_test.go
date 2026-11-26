package backup

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteAndOpen(t *testing.T) {
	data := t.TempDir()
	put := func(rel, s string) {
		p := filepath.Join(data, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put("library/abcd1234.kb", "kb data")
	put("library/abcd1234.json", `{"id":"abcd1234"}`)
	put("library/.x.tmp", "temporary")
	put("playlists.json", "[]")
	put("eeprom/20261123-214616-manual.lut", "table")
	put("eeprom/work/table.lut", "scratch")
	put("play/dn3o7/e.kb", "joined album")
	put("programs/play.exe", "program")

	var buf bytes.Buffer
	m, err := Write(&buf, data, "test", time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if m.Pieces != 1 || len(m.Files) != 5 {
		t.Fatalf("manifest: %d pieces, files %+v", m.Pieces, m.Files)
	}
	a, err := Open(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(a.Paths(), " ")
	want := "eeprom/20261123-214616-manual.lut library/abcd1234.json library/abcd1234.kb playlists.json programs/play.exe"
	if got != want {
		t.Fatalf("paths %q", got)
	}
	if b, err := a.Read("library/abcd1234.kb"); err != nil || string(b) != "kb data" {
		t.Fatalf("read: %q %v", b, err)
	}
	if _, err := a.Read("play/dn3o7/e.kb"); err == nil {
		t.Fatal("a joined album was backed up")
	}

	// A file that does not match its checksum is refused.
	f := a.listed["programs/play.exe"]
	f.SHA256 = strings.Repeat("0", 64)
	a.listed["programs/play.exe"] = f
	if _, err := a.Read("programs/play.exe"); err == nil || !strings.Contains(err.Error(), "damaged") {
		t.Errorf("a damaged file: %v", err)
	}
	// A zip without a manifest is not a backup.
	if _, err := Open(bytes.NewReader([]byte("PK\x05\x06"+strings.Repeat("\x00", 18))), 22); err != ErrNotBackup {
		t.Errorf("empty zip: %v", err)
	}
}
