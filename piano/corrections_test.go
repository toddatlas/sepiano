package piano

import (
	"path/filepath"
	"testing"
	"time"

	"sepiano/edit"
)

func TestCorrectionsApply(t *testing.T) {
	c := Corrections{On: true, FillZero: true, Keys: map[int]KeyCorrection{
		55: {Speed: 25, Release: 400}, // G3: 25% louder, lets go 400 ms later
		60: {Release: -2000},          // more than the note: kept 4 ticks long
	}}
	d := edit.Doc{Notes: []edit.Note{
		{Key: 55, On: 800, Off: 1200, Vel: 250},
		{Key: 59, On: 805, Off: 1500, Vel: 200},
		{Key: 62, On: 810, Off: 1500, Vel: 220},
		{Key: 57, On: 1000, Off: 1400, Vel: 0}, // no hammer reading
		{Key: 60, On: 2000, Off: 2400, Vel: 210},
		{Key: 64, On: 9000, Off: 9400, Vel: 0}, // alone: the piece's median
	}}
	if n := c.Apply(&d); n != 4 {
		t.Errorf("%d notes changed, want 4", n)
	}
	want := []edit.Note{
		{Key: 55, On: 800, Off: 1520, Vel: 200}, // 250 / 1.25; 1200 + 320 ticks
		{Key: 59, On: 805, Off: 1500, Vel: 200},
		{Key: 62, On: 810, Off: 1500, Vel: 220},
		{Key: 57, On: 1000, Off: 1400, Vel: 200}, // median of 200, 200, 220 (C4 is over a second away)
		{Key: 60, On: 2000, Off: 2004, Vel: 210},
		{Key: 64, On: 9000, Off: 9400, Vel: 210},
	}
	for i := range want {
		if d.Notes[i] != want[i] {
			t.Errorf("note %d: %+v, want %+v", i, d.Notes[i], want[i])
		}
	}
	if !c.Active() || (Corrections{On: false, FillZero: true}).Active() || (Corrections{On: true}).Active() {
		t.Error("Active")
	}
}

func TestCorrectionsClean(t *testing.T) {
	c, err := Corrections{Keys: map[int]KeyCorrection{55: {Speed: 10}, 56: {}}}.Clean()
	if err != nil || len(c.Keys) != 2 || c.Summary() != "G3 loudness +10%" || (Corrections{Keys: map[int]KeyCorrection{56: {}}}).Empty() != true {
		t.Fatalf("%v %v %q", c.Keys, err, c.Summary())
	}
	for _, k := range []map[int]KeyCorrection{{20: {Speed: 1}}, {60: {Speed: 101}}, {60: {Speed: -51}}, {60: {Release: 1001}}} {
		if _, err := (Corrections{Keys: k}).Clean(); err == nil {
			t.Errorf("%v: no error", k)
		}
	}
}

func TestSuggest(t *testing.T) {
	kc := KeyCheck{Outliers: []KeyStat{{Note: 55, Name: "G3", Count: 2, Deviation: -34.9}, {Note: 43, Name: "G2", Count: 6, Deviation: 46}}}
	rc := ReleaseCheck{Outliers: []ReleaseStat{{Note: 55, Name: "G3", Count: 2, Median: -436}}}
	s := Suggest(kc, rc, ReleaseCheck{Outliers: []ReleaseStat{{Note: 43, Name: "G2", Count: 3, Median: 32}}})
	if len(s) != 2 || s[0].Name != "G2" || s[1].Name != "G3" {
		t.Fatalf("%+v", s)
	}
	if s[0].Speed != -32 || s[0].Release != 0 || s[0].Press != -30 { // 1/1.46 - 1; 32 ms late, to the nearest 5
		t.Errorf("G2 %+v", s[0])
	}
	if s[1].Speed != 54 || s[1].Release != 435 || s[1].Why == "" { // 1/0.651 - 1; 436 to the nearest 5
		t.Errorf("G3 %+v", s[1])
	}
}

func TestCorrectionsStore(t *testing.T) {
	s := &CorrectionsStore{Path: filepath.Join(t.TempDir(), "c", "corrections.json")}
	c, err := s.Load()
	if err != nil || !c.On || !c.Empty() {
		t.Fatalf("empty store: %+v %v", c, err)
	}
	c.Keys[55] = KeyCorrection{Release: 400}
	now := time.Date(2026, 11, 24, 1, 2, 3, 0, time.UTC)
	if _, err := s.Save(c, now); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load()
	if err != nil || got.Keys[55].Release != 400 || !got.Updated.Equal(now) {
		t.Fatalf("%+v %v", got, err)
	}
	if got.Summary() != "G3 release +400 ms" {
		t.Errorf("summary %q", got.Summary())
	}
}

func TestPressCorrection(t *testing.T) {
	c := Corrections{On: true, Keys: map[int]KeyCorrection{60: {Speed: 50, Press: -50}}}
	d := edit.Doc{Notes: []edit.Note{
		{Key: 60, On: 800, Off: 1200, Vel: 300},
		{Key: 60, On: 2000, Off: 2400, Vel: 700}, // a silent press stays silent
		{Key: 60, On: 10, Off: 12, Vel: 300},     // not before zero, and at least 4 ticks long
	}}
	c.Apply(&d)
	if d.Notes[0] != (edit.Note{Key: 60, On: 760, Off: 1200, Vel: 200}) {
		t.Errorf("note %+v", d.Notes[0])
	}
	if d.Notes[1].Vel != 700 || d.Notes[1].On != 1960 {
		t.Errorf("silent press %+v", d.Notes[1])
	}
	if d.Notes[2].On != 0 || d.Notes[2].Off != 12 {
		t.Errorf("early note %+v", d.Notes[2])
	}
	if s := c.Summary(); s != "C4 loudness +50%, press -50 ms" {
		t.Errorf("summary %q", s)
	}
	if _, err := (Corrections{Keys: map[int]KeyCorrection{60: {Press: 301}}}).Clean(); err == nil {
		t.Error("press out of range accepted")
	}
}
