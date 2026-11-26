package piano

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sepiano/se"
)

// TestSafePedalsLeaveNativeAlone: native SE recordings (the owner's
// collection, SE_CORPUS) already move their pedals no faster than
// PedalStep, so the playback limit leaves every one exactly as recorded.
func TestSafePedalsLeaveNativeAlone(t *testing.T) {
	dir := os.Getenv("SE_CORPUS")
	if dir == "" {
		t.Skip("set SE_CORPUS to the folder of .kb/.lp/.sp recordings")
	}
	files, changed := 0, 0
	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.EqualFold(filepath.Ext(p), ".lp") || strings.Contains(p, "sepiano") {
			return nil
		}
		base := strings.TrimSuffix(p, filepath.Ext(p))
		perf, err := se.Load(base)
		if err != nil {
			return nil
		}
		files++
		if n := SafePedals(perf); n > 0 {
			changed++
			t.Logf("%s: %d samples changed", filepath.Base(base), n)
		}
		return nil
	})
	if files == 0 {
		t.Fatal("no recordings found")
	}
	if changed > 0 {
		t.Errorf("%d of %d native recordings would not play exactly as recorded", changed, files)
	}
	t.Logf("%d native recordings, none changed", files)
}
