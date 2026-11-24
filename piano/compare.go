package piano

import (
	"fmt"
	"strings"

	"sepiano/lut"
)

// Change is one difference between two tables, in words.
type Change struct {
	Key  string `json:"key,omitempty"` // the note, for per-key values
	What string `json:"what"`
	From string `json:"from"`
	To   string `json:"to"`
}

// Diff is every difference from one table to another.
type Diff struct {
	Bytes   int      `json:"bytes"` // bytes that differ in the whole image
	Changes []Change `json:"changes"`
	Other   int      `json:"other"` // differing bytes not described by Changes
}

// Compare describes how table b differs from table a. Tables with another
// layout are compared byte by byte only.
func Compare(a, b *lut.Table) Diff {
	d := Diff{Changes: []Change{}}
	n := min(len(a.Raw), len(b.Raw))
	for i := 0; i < n; i++ {
		if a.Raw[i] != b.Raw[i] {
			d.Bytes++
		}
	}
	d.Bytes += max(len(a.Raw), len(b.Raw)) - n
	if a.Sections != b.Sections || a.Bottom != b.Bottom || a.Top != b.Top || a.Length != b.Length {
		d.Other = d.Bytes
		return d
	}
	va, vb := NewView(a), NewView(b)
	add := func(key, what string, x, y any) {
		fx, fy := fmt.Sprint(x), fmt.Sprint(y)
		if fx != fy {
			d.Changes = append(d.Changes, Change{key, what, fx, fy})
		}
	}
	yes := func(b bool) string {
		if b {
			return "on"
		}
		return "off"
	}
	if va.Name != vb.Name {
		add("", "Model name", va.Name, vb.Name)
	}
	add("", "Table version", va.Version, vb.Version)
	sa, sb := va.Settings, vb.Settings
	add("", "Serial number", sa.Serial, sb.Serial)
	add("", "Discard silent notes", yes(sa.DiscardSilent), yes(sb.DiscardSilent))
	add("", "German note names", yes(sa.GermanNames), yes(sb.GermanNames))
	add("", "Calibration range", lut.NoteName(sa.FirstNote)+"–"+lut.NoteName(sa.LastNote), lut.NoteName(sb.FirstNote)+"–"+lut.NoteName(sb.LastNote))
	add("", "First calibration", yes(sa.FirstCalibration), yes(sb.FirstCalibration))
	add("", "Velocity trims", yes(va.TrimsOn), yes(vb.TrimsOn))
	add("", "Hold level", va.HoldLevel, vb.HoldLevel)
	add("", "Breakpoints", ints(va.Breakpoints), ints(vb.Breakpoints))
	for i := range va.Keys {
		ka, kb := va.Keys[i], vb.Keys[i]
		add(ka.Name, "Trim", signed(ka.Trim), signed(kb.Trim))
		rows(add, ka.Name, "Lead time", va.Breakpoints, ka.Lead, kb.Lead)
		rows(add, ka.Name, "Strike level", va.Breakpoints, ka.Level, kb.Level)
		add(ka.Name, "Silent level", ka.SilentLevel, kb.SilentLevel)
		if len(ka.Release) == 2 && len(kb.Release) == 2 {
			add(ka.Name, "60-tick release level", ka.Release[0], kb.Release[0])
			add(ka.Name, "120-tick release level", ka.Release[1], kb.Release[1])
		}
		add(ka.Name, "Free release time", ka.FreeRelease, kb.FreeRelease)
	}
	for i := range va.Player {
		if i < len(vb.Player) {
			add("", va.Player[i].Label, va.Player[i].Value, vb.Player[i].Value)
		}
	}
	d.Other = d.Bytes - describedBytes(a, b)
	return d
}

func rows(add func(key, what string, x, y any), key, what string, bp, a, b []int) {
	for r := 0; r < len(a) && r < len(b); r++ {
		label := what
		if r < len(bp) {
			label = fmt.Sprintf("%s at velocity %d", what, bp[r])
		}
		add(key, label, a[r], b[r])
	}
}

func ints(v []int) string {
	s := make([]string, len(v))
	for i, x := range v {
		s[i] = fmt.Sprint(x)
	}
	return strings.Join(s, " ")
}

func signed(n int) string { return fmt.Sprintf("%+d", n) }

// describedBytes counts the differing bytes that Compare puts into words:
// everything from section 0 to the end of section 13 except the unnamed
// parts of sections 3, 5, 8, 10, 11, 12 and 13, plus the header fields.
func describedBytes(a, b *lut.Table) int {
	described := map[int]bool{}
	mark := func(from, n int) {
		for i := from; i < from+n; i++ {
			described[i] = true
		}
	}
	mark(0, 6) // version, length, key range
	for _, s := range []int{0, 1, 2, 4, 6, 7, 9} {
		mark(a.Sections[s], len(a.Section(s)))
	}
	mark(a.Sections[10]+1, 2) // calibration range
	mark(a.Sections[10]+4, 1) // first calibration
	mark(a.Sections[12]+2, 1) // serial
	mark(a.Sections[13]+1, 2) // switches
	for _, def := range constDefs {
		mark(a.Sections[11]+1+def.off, 2)
	}
	mark(0x22, a.Sections[0]-0x22) // the name and the trim pointer
	n := 0
	for i := range described {
		if i < len(a.Raw) && i < len(b.Raw) && a.Raw[i] != b.Raw[i] {
			n++
		}
	}
	return n
}
