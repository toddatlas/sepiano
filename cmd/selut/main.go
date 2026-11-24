// Command selut prints an SE lookup table (the EEPROM image getlut.exe
// retrieves) and compares two of them.
//
//	selut TABLE              # header, settings, sections, per-key values
//	selut TABLE OTHER        # also every difference, by section and key
//
// It only reads. Keep tables out of the repository: the prototype parts are
// Stahnke's, and the rest is the owner's piano.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"sepiano/lut"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: selut TABLE [OTHER]")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() < 1 || flag.NArg() > 2 {
		flag.Usage()
		os.Exit(2)
	}
	t := load(flag.Arg(0))
	report(flag.Arg(0), t)
	if flag.NArg() == 2 {
		compare(t, load(flag.Arg(1)), flag.Arg(1))
	}
}

func load(path string) *lut.Table {
	b, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	t, err := lut.Parse(b)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
		os.Exit(1)
	}
	return t
}

func yesNo(b bool) string {
	if b {
		return "TRUE"
	}
	return "FALSE"
}

func report(path string, t *lut.Table) {
	fmt.Printf("%s: %q, table V%02X.%02X, %d of %d bytes\n", path, t.Name, t.Version>>8, t.Version&0xFF, t.Length, len(t.Raw))
	if t.Version != lut.Version {
		fmt.Printf("  WARNING: play.exe needs version %04XH and would overwrite this table's first 256 bytes\n", lut.Version)
	}
	fmt.Printf("  word at 4: %04XH\n", t.Word2)
	s := t.Settings()
	fmt.Println("  settings (as setup.exe shows them):")
	fmt.Printf("    piano serial number:              %02X\n", s.Serial)
	fmt.Printf("    discard silent notes:             %s\n", yesNo(s.DiscardSilent))
	fmt.Printf("    display German note designations: %s\n", yesNo(s.GermanNames))
	fmt.Printf("    first adjustment/calibration note: %d (%s)\n", s.FirstNote, lut.NoteName(int(s.FirstNote)))
	fmt.Printf("    last adjustment/calibration note:  %d (%s)\n", s.LastNote, lut.NoteName(int(s.LastNote)))
	fmt.Printf("    first calibration:                %s\n", yesNo(s.FirstCalibration))
	fmt.Printf("  breakpoints (section 0): %v\n", t.Breakpoints())
	fmt.Println("  sections:")
	for i, off := range t.Sections {
		sec := t.Section(i)
		shape := fmt.Sprintf("%d bytes", len(sec))
		if g := t.Grid(i); g != nil {
			shape += fmt.Sprintf(" = %d × %d keys", len(g), lut.Keys)
		}
		fmt.Printf("    %2d  @%04XH  %-24s", i, off, shape)
		if len(sec) <= 16 {
			fmt.Printf(" % X", sec)
		}
		fmt.Println()
	}
	for i := range t.Sections {
		g := t.Grid(i)
		if g == nil {
			continue
		}
		fmt.Printf("  section %d by key (one column per row):\n", i)
		for k := 0; k < lut.Keys; k++ {
			var cols []string
			for _, row := range g {
				cols = append(cols, fmt.Sprintf("%3d", row[k]))
			}
			fmt.Printf("    %-4s %s\n", lut.NoteName(lut.BottomNote+k), strings.Join(cols, " "))
		}
	}
}

// compare lists every byte that differs, named by section, row and key.
func compare(a, b *lut.Table, path string) {
	fmt.Printf("differences from %s (%q):\n", path, b.Name)
	n := 0
	for off := 0; off < len(a.Raw) && off < len(b.Raw); off++ {
		if a.Raw[off] == b.Raw[off] {
			continue
		}
		n++
		sec := a.SectionOf(off)
		where := "header"
		switch {
		case sec >= len(a.Sections):
			where = "after the table"
		case sec >= 0:
			rel := off - a.Sections[sec]
			where = fmt.Sprintf("section %d +%d", sec, rel)
			if a.Grid(sec) != nil {
				where = fmt.Sprintf("section %d row %d %s", sec, rel/lut.Keys, lut.NoteName(lut.BottomNote+rel%lut.Keys))
			}
		}
		fmt.Printf("  %04XH  %-28s %3d → %3d\n", off, where, b.Raw[off], a.Raw[off])
	}
	fmt.Printf("  %d bytes differ\n", n)
}
