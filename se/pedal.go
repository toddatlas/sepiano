package se

import (
	"errors"
	"fmt"
)

// Pedal is a decoded .lp (damper) or .sp (soft) file: one position sample
// (0 = released, 255 = fully depressed) per DamperPeriod or SoftPeriod ticks.
type Pedal struct {
	Seed    [4]byte
	Samples []byte
}

// ParsePedal decodes a pedal file. The body is PackBits-style run-length
// coded: a control byte c < 0x80 is followed by c literal samples; c > 0x80
// is followed by one sample repeated c&0x7F times. Counts of zero are errors.
func ParsePedal(raw []byte) (*Pedal, error) {
	if len(raw) < 4 {
		return nil, errShort
	}
	d := append([]byte(nil), raw...)
	Crypt(d)
	p := &Pedal{Seed: [4]byte{d[0], d[1], d[2], d[3]}}
	for i := 4; i < len(d); {
		c := d[i]
		i++
		n := int(c & 0x7F)
		if n == 0 {
			return nil, fmt.Errorf("se: zero pedal count at offset %d", i-1)
		}
		if c < 0x80 {
			if i+n > len(d) {
				return nil, errShort
			}
			p.Samples = append(p.Samples, d[i:i+n]...)
			i += n
		} else {
			if i >= len(d) {
				return nil, errShort
			}
			for range n {
				p.Samples = append(p.Samples, d[i])
			}
			i++
		}
	}
	return p, nil
}

// PedalMinRun is the shortest repeat the encoder stores as a run. Matching
// the original encoder lets parsed files re-encode byte for byte.
const PedalMinRun = 2

// Encode run-length codes the samples.
func (p *Pedal) Encode() ([]byte, error) {
	if len(p.Samples) == 0 {
		return nil, errors.New("se: empty pedal stream")
	}
	out := append([]byte(nil), p.Seed[:]...)
	s := p.Samples
	var lit []byte
	flush := func() {
		for len(lit) > 0 {
			n := min(len(lit), 127)
			out = append(out, byte(n))
			out = append(out, lit[:n]...)
			lit = lit[n:]
		}
	}
	for i := 0; i < len(s); {
		r := 1
		for i+r < len(s) && s[i+r] == s[i] {
			r++
		}
		if r >= PedalMinRun {
			// Whole run first, split into 127-sample chunks; even a
			// one-sample remainder stays a run, as in the original encoder.
			flush()
			for left := r; left > 0; left -= min(left, 127) {
				out = append(out, 0x80|byte(min(left, 127)), s[i])
			}
			i += r
			continue
		}
		lit = append(lit, s[i])
		i++
		if len(lit) == 127 {
			flush()
		}
	}
	flush()
	Crypt(out)
	return out, nil
}

// At returns the pedal position in effect at tick t for a stream sampled every
// period ticks. Past the end of the stream the last sample holds.
func (p *Pedal) At(t uint32, period uint32) byte {
	if len(p.Samples) == 0 {
		return 0
	}
	i := int(t / period)
	if i >= len(p.Samples) {
		i = len(p.Samples) - 1
	}
	return p.Samples[i]
}

// Limit slows the pedal so it never moves more than step per sample, and
// returns how many samples it changed. A change is spread evenly around
// the moment it was recorded (half before, half after), so a press still
// reaches halfway when it did. It is for a piano's sake: a sudden full
// press or release (an on/off MIDI pedal) is a hard knock for a vintage
// mechanism (docs/LIBRARIES.md, "Pedals").
func (p *Pedal) Limit(step int) int {
	s := p.Samples
	if step <= 0 || len(s) < 2 {
		return 0
	}
	n := len(s)
	clamp := func(v, lo, hi int) int { return max(lo, min(hi, v)) }
	// A forward pass lags a change and a backward pass leads it; at twice
	// the rate, their average is a ramp centred on the change.
	fwd := make([]int, n)
	bwd := make([]int, n)
	fwd[0] = int(s[0])
	for i := 1; i < n; i++ {
		fwd[i] = clamp(int(s[i]), fwd[i-1]-2*step, fwd[i-1]+2*step)
	}
	bwd[n-1] = int(s[n-1])
	for i := n - 2; i >= 0; i-- {
		bwd[i] = clamp(int(s[i]), bwd[i+1]-2*step, bwd[i+1]+2*step)
	}
	changed := 0
	prev := (fwd[0] + bwd[0] + 1) / 2
	for i := 0; i < n; i++ {
		v := (fwd[i] + bwd[i] + 1) / 2
		if i > 0 {
			v = clamp(v, prev-step, prev+step) // and never faster than step
		}
		prev = v
		if byte(v) != s[i] {
			s[i] = byte(v)
			changed++
		}
	}
	return changed
}

// OnOff says whether the pedal only ever sits fully up or fully down: an
// on/off MIDI pedal, with no depth.
func (p *Pedal) OnOff() bool {
	pressed := false
	for _, v := range p.Samples {
		switch v {
		case 0:
		case 255:
			pressed = true
		default:
			return false
		}
	}
	return pressed
}
