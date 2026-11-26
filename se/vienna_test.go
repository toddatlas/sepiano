package se

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestBosendorferCurveOnVienna converts the Vienna 4x22 MIDI files back
// with BosendorferCurve and compares each note with the native SE file of
// the same performance (SE_VIENNA: the corpus's extracted folder, holding
// boesendorfer_se/ and midi/; the data is not in the repository).
func TestBosendorferCurveOnVienna(t *testing.T) {
	dir := os.Getenv("SE_VIENNA")
	if dir == "" {
		t.Skip("set SE_VIENNA to the Vienna 4x22 folder (boesendorfer_se/, midi/)")
	}
	pieces := map[string]string{"bal_bo": "Chopin_op38", "etd_bo": "Chopin_op10_no3", "blk_bo": "Chopin_op38_1st-3rd"}
	var errs []float64
	matched, total, pairs := 0, 0, 0
	for p := 1; p <= 22; p++ {
		for se, mid := range pieces {
			kbData, err1 := os.ReadFile(filepath.Join(dir, "boesendorfer_se", fmt.Sprintf("%02d%s.KB", p, se)))
			midData, err2 := os.ReadFile(filepath.Join(dir, "midi", fmt.Sprintf("%s_p%02d.mid", mid, p)))
			if err1 != nil || err2 != nil {
				continue
			}
			native, err := ParseKB(kbData)
			if err != nil {
				t.Fatal(err)
			}
			conv, err := FromMIDI(midData, BosendorferCurve, 0, "")
			if err != nil {
				t.Fatal(err)
			}
			pairs++
			type note struct {
				key  uint8
				tick uint32
				vel  uint16
			}
			notes := func(k *KB) []note {
				var out []note
				for _, e := range k.Events {
					if e.On && e.Key >= 21 && e.Key <= 108 {
						out = append(out, note{e.Key, e.Tick, e.Vel})
					}
				}
				return out
			}
			a, b := notes(native), notes(conv.KB)
			// The two start at different times: the commonest onset difference
			// between the first notes of each key.
			shift := map[int64]int{}
			for i := 0; i < len(a) && i < 60; i++ {
				for j := 0; j < len(b) && j < 80; j++ {
					if a[i].key == b[j].key {
						shift[int64(b[j].tick)-int64(a[i].tick)]++
					}
				}
			}
			best, bestN := int64(0), 0
			for d, n := range shift {
				if n > bestN {
					best, bestN = d, n
				}
			}
			at := map[[2]int64]uint16{}
			for _, n := range b {
				at[[2]int64{int64(n.key), int64(n.tick) - best}] = n.vel
			}
			for _, n := range a {
				if n.vel < 30 || n.vel > 500 {
					continue // beyond what MIDI holds, or the hardest strikes
				}
				total++
				v, ok := at[[2]int64{int64(n.key), int64(n.tick)}]
				if !ok {
					continue
				}
				matched++
				errs = append(errs, math.Abs(float64(v)-float64(n.vel))/float64(n.vel))
			}
		}
	}
	if pairs == 0 {
		t.Fatal("no Vienna pairs found")
	}
	sort.Float64s(errs)
	median, p95 := errs[len(errs)/2], errs[len(errs)*95/100]
	t.Logf("%d pairs, %d of %d notes matched; velocity error median %.1f%%, 95th percentile %.1f%%",
		pairs, matched, total, median*100, p95*100)
	if float64(matched) < 0.95*float64(total) || median > 0.03 || p95 > 0.08 {
		t.Errorf("BosendorferCurve does not reproduce the native files closely enough")
	}
}
