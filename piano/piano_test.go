package piano

import (
	"strings"
	"testing"
	"time"

	"sepiano/lut"
)

func TestStore(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	if _, _, err := s.Latest(); err != ErrNotFound {
		t.Fatalf("empty store: %v", err)
	}
	img := lut.Example()
	at := time.Date(2026, 11, 23, 20, 37, 0, 0, time.Local)
	b1, err := s.Save(img, "imported", at)
	if err != nil {
		t.Fatal(err)
	}
	if b1.ID != "20261123-203700-imported" || !b1.Valid || b1.Model != "Simulated SE" || b1.Version != "30.17" {
		t.Errorf("backup %+v", b1)
	}
	b2, err := s.Save(img, "manual", at) // same second, another reason
	if err != nil {
		t.Fatal(err)
	}
	b3, err := s.Save(img, "manual", at) // same second, same reason
	if err != nil {
		t.Fatal(err)
	}
	if b3.ID != "20261123-203700-2-manual" {
		t.Errorf("collision id %q", b3.ID)
	}
	list, err := s.List()
	if err != nil || len(list) != 3 {
		t.Fatalf("list %v %v", list, err)
	}
	got, b, err := s.Get(b2.ID)
	if err != nil || string(got) != string(img) || b.Reason != "manual" || !b.Time.Equal(at) {
		t.Errorf("get %+v %v", b, err)
	}
	if _, _, err := s.Get("../etc/passwd"); err != ErrNotFound {
		t.Errorf("bad id: %v", err)
	}
	if _, err := s.Save(img, "Bad Reason", at); err == nil {
		t.Error("accepted a bad reason")
	}
	bad, err := s.Save(img[:100], "manual", at.Add(time.Second))
	if err != nil || bad.Valid || bad.Problem == "" {
		t.Errorf("short image %+v %v", bad, err)
	}
}

func TestView(t *testing.T) {
	img := lut.Example()
	img[0x624+1] = 90 // A#0 slow to come back up
	img[0x2AC+6*88+7] = 255
	tb, err := lut.Parse(img)
	if err != nil {
		t.Fatal(err)
	}
	v := NewView(tb)
	if v.Name != "Simulated SE" || !v.VersionOK || len(v.Keys) != 88 || v.Keys[0].Name != "A0" || v.HoldLevel != 100 {
		t.Fatalf("view %s %v %d", v.Name, v.VersionOK, len(v.Keys))
	}
	d4 := v.Keys[41]
	if d4.Name != "D4" || d4.Trim != 4 || len(d4.TrimEffect) != 4 || d4.TrimEffect[1].Becomes >= 100 {
		t.Errorf("D4 %+v", d4)
	}
	if len(v.Keys[0].Lead) != 7 || len(v.Keys[0].Curve) != len(v.Velocities) || len(v.Keys[0].Release) != 2 {
		t.Errorf("key shape %+v", v.Keys[0])
	}
	text := ""
	for _, n := range v.Notes {
		text += n.Text + "\n"
	}
	for _, want := range []string{"A#0 90", "E1 reaches the gentlest", "D4 +4", "silent key presses"} {
		if !strings.Contains(text, want) {
			t.Errorf("notes lack %q:\n%s", want, text)
		}
	}
	if len(v.Player) != len(constDefs) || v.Player[0].Value != 800 {
		t.Errorf("player %+v", v.Player)
	}
}

func TestCompare(t *testing.T) {
	a := lut.Example()
	b := lut.Example()
	b[0x67F+41] = 2       // D4 trim +4 → +2
	b[0x2AC+3*88+10] += 5 // a strike level
	b[0x749] = 0xFF       // discard silent notes on
	b[0x6E1+32] = 160     // soft-note hold scaling
	ta, _ := lut.Parse(a)
	tb, _ := lut.Parse(b)
	d := Compare(ta, tb)
	if d.Bytes != 4 || d.Other != 0 {
		t.Errorf("bytes %d other %d", d.Bytes, d.Other)
	}
	got := map[string]Change{}
	for _, c := range d.Changes {
		got[c.Key+"|"+c.What] = c
	}
	for k, want := range map[string][2]string{
		"D4|Trim":                 {"+4", "+2"},
		"|Discard silent notes":   {"off", "on"},
		"|Soft-note hold scaling": {"224", "160"},
	} {
		if c, ok := got[k]; !ok || c.From != want[0] || c.To != want[1] {
			t.Errorf("%s: %+v", k, c)
		}
	}
	if len(d.Changes) != 4 {
		t.Errorf("%d changes: %+v", len(d.Changes), d.Changes)
	}
	if d := Compare(ta, ta); d.Bytes != 0 || len(d.Changes) != 0 {
		t.Errorf("self %+v", d)
	}
}
