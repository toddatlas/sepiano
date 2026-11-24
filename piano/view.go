package piano

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strings"

	"sepiano/lut"
)

// View is a table in the terms the Piano screen shows.
type View struct {
	Backup      *Backup    `json:"backup,omitempty"`
	Name        string     `json:"name"`
	Version     string     `json:"version"`
	VersionOK   bool       `json:"version_ok"`
	Length      int        `json:"length"`
	Bottom      int        `json:"bottom"`
	Top         int        `json:"top"`
	Settings    Settings   `json:"settings"`
	Breakpoints []int      `json:"breakpoints"`
	TrimsOn     bool       `json:"trims_on"`
	HoldLevel   int        `json:"hold_level"`
	Velocities  []int      `json:"velocities"` // where each key's curve is sampled
	Keys        []Key      `json:"keys"`
	Player      []Constant `json:"player"`
	Notes       []Note     `json:"notes"`
}

// Settings are setup.exe's six values.
type Settings struct {
	Serial           string `json:"serial"`
	DiscardSilent    bool   `json:"discard_silent"`
	GermanNames      bool   `json:"german_names"`
	FirstNote        int    `json:"first_note"`
	LastNote         int    `json:"last_note"`
	FirstCalibration bool   `json:"first_calibration"`
}

// Key is one key's values.
type Key struct {
	Note        int          `json:"note"`
	Name        string       `json:"name"`
	Trim        int          `json:"trim"`
	Lead        []int        `json:"lead"`  // section 1, per breakpoint (ticks)
	Level       []int        `json:"level"` // section 2, per breakpoint (smaller is harder)
	SilentLevel int          `json:"silent_level"`
	Release     []int        `json:"release"`      // section 6: the 60- and 120-tick release levels
	FreeRelease int          `json:"free_release"` // section 7 (ticks)
	Curve       [][2]int     `json:"curve"`        // [lead, level] at View.Velocities, before the trim
	TrimEffect  []TrimEffect `json:"trim_effect,omitempty"`
	Flags       []string     `json:"flags,omitempty"` // gentlest, slow
}

// TrimEffect is what a key's trim does to one dynamic.
type TrimEffect struct {
	Label    string  `json:"label"`
	Velocity int     `json:"velocity"`
	Becomes  int     `json:"becomes"`
	SpeedPct float64 `json:"speed_pct"`
}

// Constant is one of section 11's player constants.
type Constant struct {
	Offset  int    `json:"offset"`
	Value   int    `json:"value"`
	Factory *int   `json:"factory,omitempty"` // the prototype's value, where known
	Label   string `json:"label"`
	Explain string `json:"explain"`
}

// Note is something worth knowing about the table.
type Note struct {
	Level string   `json:"level"` // info, warn, danger
	Text  string   `json:"text"`
	Keys  []string `json:"keys,omitempty"`
}

// curveVelocities sample each key's lookup, loud to soft.
var curveVelocities = []int{20, 30, 40, 52, 64, 82, 100, 130, 160, 208, 256, 328, 400, 520, 640}

// dynamics name four velocities for the trim effect.
var dynamics = []struct {
	label string
	v     int
}{{"loud", 64}, {"medium", 100}, {"soft", 256}, {"very soft", 400}}

type constDef struct {
	off            int
	label, explain string
}

// The section 11 words play.exe reads (docs/SUITE-PROGRAMS.md).
var constDefs = []constDef{
	{0, "Performance clock", "ticks per second"},
	{6, "Uniform velocity", "used when the player's uniform option is on"},
	{8, "Velocity offset", "added when the player's offset option is on"},
	{10, "Quick re-strike scale", "scales the velocity of a quick re-strike (n/256)"},
	{12, "Check timing", "the check event comes lead × n/256 before the strike"},
	{14, "Quick re-strike time", "re-strikes sooner than this many ticks are handled apart"},
	{16, "Re-strike velocity limit", ""},
	{18, "Let-go time", "ticks between letting the key go and the strike"},
	{20, "Let-go time, marked keys", "the same, for keys marked 2"},
	{22, "Hold delay", "the hold starts lead × n/256 after the strike (0 = no hold)"},
	{24, "Very loud limit", "below this velocity the let-go time grows"},
	{26, "Re-strike timing limit", ""},
	{28, "Silent-note threshold", "this velocity and softer is a silent key press"},
	{30, "Soft-note limit", "above this velocity, the hold after the sensor is scaled"},
	{32, "Soft-note hold scaling", "n/256 of the strike level once the sensor sees the hammer"},
	{34, "Ticks before a release", "time in the release-preparing mode"},
	{36, "Release measure scale", ""},
	{38, "Free release limit", "release measures up to this let the key go freely"},
	{40, "60-tick release limit", "up to this, the 60-tick release; above, the 120-tick one"},
}

// steinwayB are the Steinway B-SE prototype's section 11 values.
var steinwayB = map[int]int{0: 800, 6: 80, 8: 16, 10: 208, 12: 16, 14: 176, 16: 256, 18: 30, 20: 64,
	22: 32, 24: 38, 26: 32, 28: 640, 30: 128, 32: 224, 34: 8, 36: 512, 38: 2400, 40: 4800}

// NewView describes table t.
func NewView(t *lut.Table) View {
	s := t.Settings()
	v := View{
		Name:        t.Name,
		Version:     fmt.Sprintf("%02X.%02X", t.Version>>8, t.Version&0xFF),
		VersionOK:   t.Version == lut.Version,
		Length:      t.Length,
		Bottom:      t.Bottom,
		Top:         t.Top,
		Breakpoints: t.Breakpoints(),
		Velocities:  curveVelocities,
		Settings: Settings{Serial: fmt.Sprintf("%02X", s.Serial), DiscardSilent: s.DiscardSilent,
			GermanNames: s.GermanNames, FirstNote: int(s.FirstNote), LastNote: int(s.LastNote),
			FirstCalibration: s.FirstCalibration},
	}
	on, trims := t.Trims()
	v.TrimsOn = on
	if s9 := t.Section(9); len(s9) > 0 {
		v.HoldLevel = int(s9[0])
	}
	g1, g2, g4, g6, g7 := t.Grid(1), t.Grid(2), t.Grid(4), t.Grid(6), t.Grid(7)
	for k := 0; k < t.Keys(); k++ {
		key := Key{Note: t.Bottom + k, Name: lut.NoteName(t.Bottom + k)}
		if k < len(trims) {
			key.Trim = int(trims[k])
		}
		for _, row := range g1 {
			key.Lead = append(key.Lead, int(row[k]))
		}
		for _, row := range g2 {
			key.Level = append(key.Level, int(row[k]))
		}
		if len(g4) > 0 {
			key.SilentLevel = int(g4[0][k])
		}
		for _, row := range g6 {
			key.Release = append(key.Release, int(row[k]))
		}
		if len(g7) > 0 {
			key.FreeRelease = int(g7[0][k])
		}
		for _, vel := range curveVelocities {
			lead, level, err := t.Lookup(key.Note, vel)
			if err != nil {
				lead, level = -1, -1
			}
			key.Curve = append(key.Curve, [2]int{lead, level})
		}
		if key.Trim != 0 {
			for _, d := range dynamics {
				b := lut.TrimVelocity(d.v, key.Trim)
				pct := (float64(d.v)/float64(b) - 1) * 100 // speed is 128000/velocity
				key.TrimEffect = append(key.TrimEffect, TrimEffect{d.label, d.v, b, math.Round(pct*10) / 10})
			}
		}
		v.Keys = append(v.Keys, key)
	}
	v.Player = constants(t)
	v.Notes = notes(t, &v)
	return v
}

func constants(t *lut.Table) []Constant {
	s := t.Section(11)
	if len(s) < 2 {
		return nil
	}
	data := s[1:]
	var out []Constant
	for _, d := range constDefs {
		if d.off+1 >= len(data) {
			continue
		}
		c := Constant{Offset: d.off, Value: int(binary.LittleEndian.Uint16(data[d.off:])), Label: d.label, Explain: d.explain}
		if strings.HasPrefix(t.Name, "Steinway B-SE") {
			if f, ok := steinwayB[d.off]; ok {
				c.Factory = &f
			}
		}
		out = append(out, c)
	}
	return out
}

func constant(v *View, off int) (int, bool) {
	for _, c := range v.Player {
		if c.Offset == off {
			return c.Value, true
		}
	}
	return 0, false
}

func notes(t *lut.Table, v *View) []Note {
	var out []Note
	if !v.VersionOK {
		out = append(out, Note{"danger", fmt.Sprintf("The table's version is %s, not 30.17: play.exe would overwrite its first 256 bytes.", v.Version), nil})
	}
	if v.Settings.FirstCalibration {
		out = append(out, Note{"warn", "The table says it has never been calibrated.", nil})
	}
	// Keys at the gentlest strike (255) somewhere in section 2.
	var gentle []string
	for i := range v.Keys {
		k := &v.Keys[i]
		for _, x := range k.Level {
			if x == 255 {
				gentle = append(gentle, k.Name)
				k.Flags = append(k.Flags, "gentlest")
				break
			}
		}
	}
	if len(gentle) > 0 {
		out = append(out, Note{"warn", fmt.Sprintf("%s reach%s the gentlest strike the table can hold (255) for soft notes: the softest notes may sound louder than recorded.",
			list(gentle), plural(len(gentle) == 1, "es", "")), gentle})
	}
	// Keys that return slowly when let go.
	var frees []int
	for _, k := range v.Keys {
		frees = append(frees, k.FreeRelease)
	}
	sort.Ints(frees)
	if len(frees) > 0 {
		median := frees[len(frees)/2]
		var slow []string
		var parts []string
		for i := range v.Keys {
			k := &v.Keys[i]
			if median > 0 && k.FreeRelease*2 >= median*3 {
				slow = append(slow, k.Name)
				parts = append(parts, fmt.Sprintf("%s %d", k.Name, k.FreeRelease))
				k.Flags = append(k.Flags, "slow")
			}
		}
		if len(slow) > 0 {
			out = append(out, Note{"warn", fmt.Sprintf("Slow to come back up when let go: %s ticks (typical %d). Worth a technician's look.",
				strings.Join(parts, ", "), median), slow})
		}
	}
	if s := v.Settings; s.FirstNote != v.Bottom || s.LastNote != v.Top {
		r := lut.NoteName(s.FirstNote) + " to " + lut.NoteName(s.LastNote)
		if s.FirstNote == s.LastNote {
			r = lut.NoteName(s.FirstNote) + " only"
		}
		out = append(out, Note{"info", "The calibration range is " + r + ": a calibration run now would only cover those keys.", nil})
	}
	var trimmed []string
	for _, k := range v.Keys {
		if k.Trim != 0 {
			trimmed = append(trimmed, fmt.Sprintf("%s %+d", k.Name, k.Trim))
		}
	}
	switch {
	case len(trimmed) > 0 && v.TrimsOn:
		out = append(out, Note{"info", "Velocity trims: " + strings.Join(trimmed, ", ") + ".", nil})
	case len(trimmed) > 0:
		out = append(out, Note{"info", "Velocity trims are set (" + strings.Join(trimmed, ", ") + ") but switched off.", nil})
	}
	if th, ok := constant(v, 28); ok {
		if v.Settings.DiscardSilent {
			out = append(out, Note{"info", fmt.Sprintf("Notes softer than velocity %d are left out.", th), nil})
		} else {
			out = append(out, Note{"info", fmt.Sprintf("Notes softer than velocity %d are played as silent key presses.", th), nil})
		}
	}
	for _, c := range v.Player {
		if c.Factory != nil && *c.Factory != c.Value {
			out = append(out, Note{"info", fmt.Sprintf("%s is %d (the factory table has %d).", c.Label, c.Value, *c.Factory), nil})
		}
	}
	return out
}

func list(s []string) string {
	switch len(s) {
	case 1:
		return s[0]
	case 2:
		return s[0] + " and " + s[1]
	}
	return strings.Join(s[:len(s)-1], ", ") + " and " + s[len(s)-1]
}

func plural(one bool, a, b string) string {
	if one {
		return a
	}
	return b
}
