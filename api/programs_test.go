package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sepiano/host"
)

// loadImage builds a load image with the given banner and extra bytes after it.
func loadImage(t *testing.T, banner, extra string) []byte {
	t.Helper()
	body := []byte("\r\n" + banner + "\r\n\x7f" + extra)
	end := 0x8000 + len(body) - 1
	img := []byte{0, 3, 0x00, 0x80, byte(end), byte(end >> 8), 0x00, 0x80, 0, 0, 0, 0, 0, 0, 0, 0}
	return append(img, body...)
}

func TestCheckIdentity(t *testing.T) {
	const inst = "STAHNKE SE/PC LOOKUP TABLE INSTALLER, V50.44"
	cases := []struct {
		name, banner, extra string
		ok                  bool
	}{
		{"getlut", "STAHNKE SE/PC LOOKUP TABLE RETRIEVER, V50.44", "", true},
		{"getlut", inst, "Steinway B-SE", false},
		{"anykey", inst, "Steinway B-SE", false},
		{"anykey", "ANY KEY TEST, V50.44", "", true},
		{"putlut", inst, "", true},
		{"putlut", inst, "table Steinway B-SE", false},
		{"putb", inst, "table Steinway B-SE", true},
		{"putb", inst, "", false},
		{"putb", inst, "Bosendorfer 225SE", false},
		{"put225", inst, "Bosendorfer 225SE", true},
		{"setup", "LOOKUP TABLE SETUP, V50.44", "", true},
		{"setup", "STAHNKE SE/PC ADAPTIVE CALIBRATOR, V50.44", "", false},
	}
	for _, c := range cases {
		prog, err := host.ParseProgram(loadImage(t, c.banner, c.extra))
		if err != nil {
			t.Fatal(err)
		}
		if err := CheckIdentity(c.name, prog); (err == nil) != c.ok {
			t.Errorf("%s with %q %q: %v", c.name, c.banner, c.extra, err)
		}
	}

	// Installing refuses a program under another program's name.
	p := &Programs{Dir: t.TempDir()}
	if _, err := p.Install("anykey", loadImage(t, inst, "Steinway B-SE")); err == nil || !strings.Contains(err.Error(), "not anykey.exe") {
		t.Fatalf("install under the wrong name: %v", err)
	}
	// So does loading one copied in by hand.
	os.WriteFile(filepath.Join(p.Dir, "getlut.exe"), loadImage(t, inst, "Steinway B-SE"), 0o644)
	if _, err := p.Load("getlut"); err == nil {
		t.Fatal("loaded a factory table as getlut")
	}
	if info := p.Info("getlut"); info.Error == "" {
		t.Fatalf("info: %+v", info)
	}
}

// With the real suite (SE_SUITE), each program is accepted under its own
// name and refused under every name that means something else.
func TestCheckIdentitySuite(t *testing.T) {
	dir := os.Getenv("SE_SUITE")
	if dir == "" {
		t.Skip("set SE_SUITE to a directory with the suite's programs")
	}
	for name, file := range ProgramFiles {
		b, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			t.Logf("%s: %v", file, err)
			continue
		}
		prog, err := host.ExtractProgram(b)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		for other := range ProgramFiles {
			err := CheckIdentity(other, prog)
			if (other == name) != (err == nil) {
				t.Errorf("%s as %s: %v", file, other, err)
			}
		}
	}
}
