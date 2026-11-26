package se

import "testing"

func TestPedalLimit(t *testing.T) {
	// An on/off pedal: up, then fully down at sample 10, up again at 30.
	s := make([]byte, 50)
	for i := 10; i < 30; i++ {
		s[i] = 255
	}
	p := &Pedal{Samples: s}
	if !p.OnOff() {
		t.Fatal("not seen as on/off")
	}
	if n := p.Limit(32); n == 0 {
		t.Fatal("nothing changed")
	}
	for i := 1; i < len(s); i++ {
		if d := int(s[i]) - int(s[i-1]); d > 32 || d < -32 {
			t.Fatalf("a step of %d at %d: %v", d, i, s)
		}
	}
	// Centred: about halfway at the recorded press and release.
	if s[10] < 96 || s[10] > 160 || s[30] < 96 || s[30] > 160 {
		t.Errorf("not centred: %d at the press, %d at the release (%v)", s[10], s[30], s)
	}
	if s[20] != 255 || s[0] != 0 || s[49] != 0 {
		t.Errorf("the held and resting parts changed: %v", s)
	}
	if p.OnOff() {
		t.Error("still on/off after smoothing")
	}
	// A pedal that already moves gently is left as it is.
	g := &Pedal{Samples: []byte{0, 20, 60, 110, 170, 220, 255, 255, 200, 140, 80, 20, 0}}
	if n := g.Limit(60); n != 0 {
		t.Errorf("a gentle pedal changed: %v", g.Samples)
	}
}
