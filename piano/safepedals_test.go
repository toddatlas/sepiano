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
// The only change allowed is the gentle ending of a pedal left down when
// the file ends (5 of the owner's 350 files; the piano would otherwise let
// it go all at once).
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
		leftDown := false
		for _, ped := range []*se.Pedal{perf.Damper, perf.Soft} {
			if ped != nil && len(ped.Samples) > 0 && ped.Samples[len(ped.Samples)-1] > pedalUp {
				leftDown = true
			}
		}
		if n := SafePedals(perf); n > 0 && !leftDown {
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
