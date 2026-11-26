package main

// sepianod work: how hard pieces work the piano's key solenoids
// (piano.MeasureWork), for SE files or MIDI files not in the library, such
// as rolls being considered for import.

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sepiano/piano"
	"sepiano/se"
)

func workCmd(args []string) int {
	fl := flag.NewFlagSet("sepianod work", flag.ExitOnError)
	curve := fl.String("curve", "disklavier", "velocity curve for MIDI files (disklavier, bosendorfer)")
	fl.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: sepianod work [-curve NAME] FILE.kb|FILE.mid ...")
		fl.PrintDefaults()
	}
	fl.Parse(args)
	if fl.NArg() == 0 {
		fl.Usage()
		return 2
	}
	vc, ok := se.Curves[*curve]
	if !ok {
		fmt.Fprintf(os.Stderr, "no such curve %q\n", *curve)
		return 2
	}
	fmt.Println("level       avg held  peak  held 1s  held 10s  longest      long holds  file")
	failed := 0
	for _, f := range fl.Args() {
		var perf *se.Performance
		var err error
		switch strings.ToLower(filepath.Ext(f)) {
		case ".mid", ".midi":
			var b []byte
			if b, err = os.ReadFile(f); err == nil {
				perf, err = se.FromMIDI(b, vc, time.Second, "")
			}
		default:
			perf, err = se.Load(f)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", f, err)
			failed++
			continue
		}
		w := piano.MeasureWork(perf)
		fmt.Printf("%-11s %8.1f  %4d  %7d  %8d  %5.0f s %-4s  %6.0f s    %s\n", w.Level, w.AvgHeld, w.PeakHeld, w.Held1s, w.Held10s,
			w.LongestHoldS, noteName(w.LongestKey), w.LongHoldS, filepath.Base(f))
	}
	if failed > 0 {
		return 1
	}
	return 0
}

// noteName: 60 → "C4".
func noteName(k int) string {
	if k <= 0 {
		return ""
	}
	return []string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}[k%12] + fmt.Sprint(k/12-1)
}
