package se

import (
	"fmt"
	"sort"
	"time"

	"sepiano/smf"
)

// VelocityCurve maps SE velocity (an inverse scale: larger is softer) to MIDI
// velocity. The default is fitted to Zenph's paired renderings of the same
// performances for a Bösendorfer SE and a Yamaha Disklavier (11,261 notes;
// median MIDI velocity per SE bucket). Points must be sorted by SE value with
// MIDI values decreasing.
type VelocityCurve [][2]float64

var DefaultCurve = VelocityCurve{
	{12, 127}, {20, 110}, {30, 99}, {40, 90}, {56, 85}, {72, 76}, {88, 68},
	{104, 63}, {120, 59}, {136, 54.5}, {152, 50}, {168, 46}, {184, 42},
	{200, 39}, {216, 35.5}, {232, 33}, {248, 31}, {264, 29}, {280, 26},
	{296, 25}, {312, 22}, {400, 12}, {600, 4}, {1023, 1},
}

// BosendorferCurve is the curve Bösendorfer's own converter used for SE
// files exported as MIDI (the Vienna 4x22 and Batik collections). It was
// fitted from 66 Vienna pairs (32,074 notes matched by key and onset): SE
// ≈ 524.5·e^(−0.02696·MIDI), within 1.6 SE units over MIDI 15–96; above 96
// it is extrapolated (TestBosendorferCurveOnVienna checks it). Use it to
// turn such MIDI back into SE files; DefaultCurve suits Disklavier MIDI.
var BosendorferCurve = VelocityCurve{
	{17.1, 127}, {20.6, 120}, {27, 110}, {35.4, 100}, {39, 96}, {46, 90}, {53, 85},
	{60, 80}, {69, 75}, {79.5, 70}, {91, 65}, {104, 60}, {120, 55}, {137, 50},
	{156, 45}, {179, 40}, {204, 35}, {233, 30}, {267, 25}, {305, 20}, {351, 15},
	{407, 10}, {466, 5}, {521, 1},
}

// Curves names the curves a MIDI file can be converted with: "disklavier"
// (DefaultCurve, the default) and "bosendorfer" (BosendorferCurve).
var Curves = map[string]VelocityCurve{"disklavier": DefaultCurve, "bosendorfer": BosendorferCurve}

// ToMIDI returns the MIDI velocity (1-127) for an SE velocity. SE velocity
// 0 is a silent press (play.exe 1044H), not the loudest note: MIDI has no
// silent press, so it becomes the softest, 1.
func (c VelocityCurve) ToMIDI(v uint16) byte {
	if v == 0 {
		return 1
	}
	x := float64(v)
	return byte(clamp(interp(c, x, 0, 1), 1, 127) + 0.5)
}

// ToSE returns the SE velocity for a MIDI velocity (1-127).
func (c VelocityCurve) ToSE(m byte) uint16 {
	y := float64(m)
	// interpolate on the reversed curve (MIDI ascending)
	r := make(VelocityCurve, len(c))
	for i, p := range c {
		r[len(c)-1-i] = [2]float64{p[1], p[0]}
	}
	v := clamp(interp(r, y, 0, 1), 4, MaxVelocity)
	return uint16(v + 0.5)
}

func interp(c VelocityCurve, x float64, xi, yi int) float64 {
	if x <= c[0][xi] {
		return c[0][yi]
	}
	for i := 1; i < len(c); i++ {
		if x <= c[i][xi] {
			a, b := c[i-1], c[i]
			return a[yi] + (x-a[xi])*(b[yi]-a[yi])/(b[xi]-a[xi])
		}
	}
	return c[len(c)-1][yi]
}

func clamp(v, lo, hi float64) float64 { return max(lo, min(hi, v)) }

// MIDI controller numbers used for the pedals.
const (
	CCDamper    = 64
	CCSostenuto = 66 // the .kb file's key-0 events: 0x80 down, 0x00 up
	CCSoft      = 67
)

// ToMIDI renders the performance as a format-0 Standard MIDI File at the
// native 800 ticks per second. Filler markers and keys outside 21..108 that
// Zenph uses as sync marks (key 11 and the like) are kept unless skipMarks.
func (p *Performance) ToMIDI(curve VelocityCurve, skipMarks bool) ([]byte, error) {
	if curve == nil {
		curve = DefaultCurve
	}
	var msgs []smf.Message
	at := func(t uint32) time.Duration { return TickDuration(t) }
	for _, e := range p.KB.Events {
		if e.Key == 0 && e.Tick > 0 { // the sostenuto pedal
			v := byte(0)
			if e.On {
				v = 127
			}
			msgs = append(msgs, smf.Message{Time: at(e.Tick), Status: 0xB0, Data1: CCSostenuto, Data2: v})
			continue
		}
		if e.Filler() || (skipMarks && (e.Key < 12 || e.Key > 108)) {
			continue
		}
		if e.On {
			msgs = append(msgs, smf.Message{Time: at(e.Tick), Status: 0x90, Data1: e.Key, Data2: curve.ToMIDI(e.Vel)})
		} else {
			msgs = append(msgs, smf.Message{Time: at(e.Tick), Status: 0x80, Data1: e.Key, Data2: 64})
		}
	}
	for _, ped := range []struct {
		p      *Pedal
		period uint32
		cc     byte
	}{{p.Damper, DamperPeriod, CCDamper}, {p.Soft, SoftPeriod, CCSoft}} {
		if ped.p == nil {
			continue
		}
		last := -1
		for i, s := range ped.p.Samples {
			t := uint32(i) * ped.period
			if t > p.KB.End {
				break
			}
			v := int(s) * 127 / 255
			if v != last {
				msgs = append(msgs, smf.Message{Time: at(t), Status: 0xB0, Data1: ped.cc, Data2: byte(v)})
				last = v
			}
		}
	}
	// Controllers before notes at equal times, so pedal state is set first.
	sort.SliceStable(msgs, func(i, j int) bool {
		if msgs[i].Time != msgs[j].Time {
			return msgs[i].Time < msgs[j].Time
		}
		return msgs[i].Kind() == 0xB0 && msgs[j].Kind() != 0xB0
	})
	end := smf.Message{Time: at(p.KB.End), Status: 0xB0, Data1: 123} // all notes off at the end
	msgs = append(msgs, end)
	return smf.Write(msgs, TicksPerSecond, "")
}

// FromMIDI builds a performance from a Standard MIDI File. All channels are
// merged. Note-on velocities are mapped through curve; CC64 and CC67 become
// the damper and soft pedal streams. A one-second lead-in is added, matching
// the Zenph files, unless leadIn is zero.
func FromMIDI(data []byte, curve VelocityCurve, leadIn time.Duration, text string) (*Performance, error) {
	if curve == nil {
		curve = DefaultCurve
	}
	msgs, err := smf.Read(data)
	if err != nil {
		return nil, err
	}
	tick := func(d time.Duration) uint32 {
		return uint32(((d+leadIn)*TicksPerSecond + time.Second/2) / time.Second)
	}
	kb := &KB{Format: 0x0F, Model: 0x11, Text: text}
	type ccv struct {
		t uint32
		v byte
	}
	var damper, soft []ccv
	down := map[byte]int{} // key -> number of sounding note-ons
	sostenuto := false
	var end uint32
	for _, m := range msgs {
		t := tick(m.Time)
		end = max(end, t)
		switch k := m.Kind(); {
		case k == 0x90 && m.Data2 > 0:
			if m.Data1 == 0 || m.Data1 == FillerKey {
				continue
			}
			if down[m.Data1] > 0 {
				// Re-strike without release: release first so the key can repeat.
				kb.Events = append(kb.Events, Event{Tick: t, Key: m.Data1})
			}
			down[m.Data1]++
			kb.Events = append(kb.Events, Event{Tick: t, Key: m.Data1, On: true, Vel: curve.ToSE(m.Data2)})
		case k == 0x80 || k == 0x90:
			if down[m.Data1] > 0 {
				down[m.Data1]--
				if down[m.Data1] == 0 {
					kb.Events = append(kb.Events, Event{Tick: t, Key: m.Data1})
				}
			}
		case k == 0xB0 && m.Data1 == CCDamper:
			damper = append(damper, ccv{t, m.Data2})
		case k == 0xB0 && m.Data1 == CCSoft:
			soft = append(soft, ccv{t, m.Data2})
		case k == 0xB0 && m.Data1 == CCSostenuto:
			// Key 0 events: 0x80 puts the sostenuto down, 0x00 lets it up.
			if on := m.Data2 >= 64; on != sostenuto && t > 0 {
				kb.Events = append(kb.Events, Event{Tick: t, Key: 0, On: on})
				sostenuto = on
			}
		}
	}
	for key, n := range down {
		if n > 0 {
			kb.Events = append(kb.Events, Event{Tick: end, Key: key})
		}
	}
	if len(kb.Notes()) == 0 {
		return nil, fmt.Errorf("se: MIDI file has no notes")
	}
	sort.SliceStable(kb.Events, func(i, j int) bool { return kb.Events[i].Tick < kb.Events[j].Tick })
	kb.End = end + TicksPerSecond/2
	sample := func(cc []ccv, period uint32) *Pedal {
		n := int((kb.End+TicksPerSecond)/period) + 1
		s := make([]byte, n)
		var v byte
		j := 0
		for i := range s {
			t := uint32(i) * period
			for j < len(cc) && cc[j].t <= t {
				v = cc[j].v
				j++
			}
			s[i] = byte(int(v) * 255 / 127)
		}
		return &Pedal{Samples: s}
	}
	return &Performance{KB: kb, Damper: sample(damper, DamperPeriod), Soft: sample(soft, SoftPeriod)}, nil
}
