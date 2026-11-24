package edit

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"sepiano/se"
)

// TestCorpus loads every performance in the collection ($SE_CORPUS) as the
// editor does and builds it again unchanged: the notes, the events outside
// the keyboard and the pedals must come back the same.
func TestCorpus(t *testing.T) {
	dir := os.Getenv("SE_CORPUS")
	if dir == "" {
		dir = "../.."
	}
	if _, err := os.Stat(filepath.Join(dir, "Boes", "10Sec-C9-400.kb")); err != nil {
		t.Skip("recording corpus not found; set SE_CORPUS")
	}
	files, exact := 0, 0
	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".kb") || strings.Contains(p, "sepiano") {
			return nil
		}
		perf, err := se.Load(p)
		if err != nil {
			return nil // incomplete sets are the se tests' business
		}
		files++
		d := FromPerformance(perf)
		back, err := d.Performance()
		if err != nil {
			t.Errorf("%s: %v", p, err)
			return nil
		}
		again := FromPerformance(back)
		sortNotes(again.Notes)
		sortNotes(d.Notes)
		if !reflect.DeepEqual(again.Notes, d.Notes) || !reflect.DeepEqual(again.Other, d.Other) {
			t.Errorf("%s: notes changed", p)
		}
		if !reflect.DeepEqual(back.Damper.Samples, perf.Damper.Samples) || !reflect.DeepEqual(back.Soft.Samples, perf.Soft.Samples) {
			t.Errorf("%s: pedals changed", p)
		}
		if count0(back) != count0(perf) {
			t.Errorf("%s: sostenuto events %d, want %d", p, count0(back), count0(perf))
		}
		if reflect.DeepEqual(back.KB.Notes(), perf.KB.Notes()) {
			exact++
		}
		return nil
	})
	if files == 0 {
		t.Fatal("no performances")
	}
	t.Logf("%d performances; %d with identical key events", files, exact)
}

// sortNotes orders notes by start, then key: notes that start together come
// back from a .kb in key order.
func sortNotes(n []Note) {
	sort.Slice(n, func(i, j int) bool {
		if n[i].On != n[j].On {
			return n[i].On < n[j].On
		}
		return n[i].Key < n[j].Key
	})
}

// count0 counts the sostenuto pedal's events (key 0).
func count0(p *se.Performance) int {
	n := 0
	for _, e := range p.KB.Events {
		if e.Key == 0 && e.Tick > 0 {
			n++
		}
	}
	return n
}
