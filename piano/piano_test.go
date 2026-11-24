package piano

import (
	"sort"
	"strings"
	"testing"
	"time"

	"sepiano/lut"
	"sepiano/se"
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

func TestVoicing(t *testing.T) {
	v := Voicing{On: true, Offset: 2, Trims: map[int]int{55: 5, 60: 0}}
	v, err := v.Clean()
	if err != nil || len(v.Trims) != 1 || !v.Active() {
		t.Fatalf("clean %+v %v", v, err)
	}
	if v.TrimFor(55) != 7 || v.TrimFor(40) != 2 {
		t.Errorf("trim for G3 %d, E2 %d", v.TrimFor(55), v.TrimFor(40))
	}
	if got := v.Velocity(55, 100); got != 97 { // +7 steps, as the firmware would
		t.Errorf("G3 at 100 → %d, want 97", got)
	}
	if got := v.Velocity(55, 0); got != 0 {
		t.Errorf("velocity 0 changed to %d", got)
	}
	off := v
	off.On = false
	if off.Active() {
		t.Error("an off voicing is active")
	}
	if _, err := (Voicing{Offset: MaxTrim + 1}).Clean(); err == nil {
		t.Error("accepted a huge offset")
	}
	if _, err := (Voicing{Trims: map[int]int{200: 1}}).Clean(); err == nil {
		t.Error("accepted note 200")
	}

	p := TestPhrase(55, 5)
	if n := len(p.KB.Events); n != 16 {
		t.Fatalf("%d events", n)
	}
	if a, b := p.KB.Events[2].Vel, p.KB.Events[0].Vel; a >= b || b != 64 {
		t.Errorf("first pair %d then %d", b, a)
	}
	if _, _, _, err := p.Encode(); err != nil {
		t.Fatal(err)
	}
	n := v.Apply(p) // G3 gets +7 more on every note-on
	if n != 8 {
		t.Errorf("Apply changed %d notes, want 8", n)
	}

	s := &VoicingStore{Path: t.TempDir() + "/voicing.json"}
	if got, err := s.Load(); err != nil || got.Active() {
		t.Fatalf("empty store %+v %v", got, err)
	}
	saved, err := s.Save(Voicing{On: true, Trims: map[int]int{28: -3, 30: 0}})
	if err != nil || len(saved.Trims) != 1 {
		t.Fatalf("save %+v %v", saved, err)
	}
	got, err := s.Load()
	if err != nil || !got.On || got.Trims[28] != -3 {
		t.Errorf("load %+v %v", got, err)
	}
}

func TestCheckKeys(t *testing.T) {
	kb := &se.KB{Format: 0x0F, Model: 0x11}
	tick := uint32(800)
	// Keys 48..60, each four times at velocity 100; key 55 records loud (80)
	// and key 57 soft (125).
	for n := 48; n <= 60; n++ {
		v := uint16(100)
		switch n {
		case 55:
			v = 80
		case 57:
			v = 125
		}
		for i := 0; i < 4; i++ {
			kb.Events = append(kb.Events, se.Event{Tick: tick, Key: uint8(n), On: true, Vel: v}, se.Event{Tick: tick + 100, Key: uint8(n)})
			tick += 200
		}
	}
	kb.End = tick
	c := CheckKeys(&se.Performance{KB: kb})
	if c.Notes != 13*4 || c.Median != 100 || len(c.Keys) != 13 {
		t.Fatalf("check %+v", c)
	}
	if len(c.Outliers) != 2 || c.Outliers[0].Name != "G3" || c.Outliers[0].Deviation <= 0 || c.Outliers[1].Name != "A3" || c.Outliers[1].Deviation >= 0 {
		t.Errorf("outliers %+v", c.Outliers)
	}
	if c.Keys[0].Count != 4 || c.Keys[0].Spread != 0 {
		t.Errorf("first key %+v", c.Keys[0])
	}
	if e := CheckKeys(&se.Performance{KB: &se.KB{}}); e.Notes != 0 || len(e.Keys) != 0 {
		t.Errorf("empty %+v", e)
	}
}

func TestCheckReleases(t *testing.T) {
	// Four chords of G3, B3, D4, G4, all let go together, except G3, which
	// comes up 300 ms early every time.
	var ev []se.Event
	for c := 0; c < 4; c++ {
		at := uint32(800 + c*1600)
		for i, k := range []uint8{55, 59, 62, 67} {
			ev = append(ev, se.Event{Tick: at + uint32(i*8), Key: k, On: true, Vel: 200})
		}
		for _, k := range []uint8{55, 59, 62, 67} {
			off := at + 800
			if k == 55 {
				off -= 240
			}
			ev = append(ev, se.Event{Tick: off, Key: k})
		}
	}
	sort.SliceStable(ev, func(i, j int) bool { return ev[i].Tick < ev[j].Tick })
	p := &se.Performance{KB: &se.KB{Events: ev, End: 8000}}
	c := CheckReleases(p)
	if c.Chords != 4 || len(c.Keys) != 4 {
		t.Fatalf("%d chords, %d keys", c.Chords, len(c.Keys))
	}
	if len(c.Outliers) != 1 || c.Outliers[0].Name != "G3" || c.Outliers[0].Median != -300 || c.Outliers[0].Count != 4 {
		t.Errorf("outliers %+v", c.Outliers)
	}
}
