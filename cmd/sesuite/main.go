// Command sesuite surveys the programs of the SE Program Suite. For every
// executable in the folders given it extracts the embedded Z80 program,
// traces its code, and reports its banner, addresses, messages, the
// firmware services it calls, the file-service requests it makes, the I/O
// ports it touches and the firmware memory it reads or writes.
//
//	sesuite FOLDER...                 # a report on stdout
//	sesuite -dis NAME FOLDER...       # a listing of one program (NAME: e.g. getlut)
//	sesuite -out DIR FOLDER...        # also write each Z80 image to DIR
//
// Every program starts by copying a block of the resident firmware (its
// address is read from 0001H, the reset vector's operand) to just past
// itself, and calls the firmware through that copy, which looks like a
// table of 3-byte jumps. So firmware services are reported as entry
// numbers in that table ("FW#42"), the same in every program.
//
// It only reads the files. The programs are Wayne Stahnke's copyrighted
// work: keep them, the images and listings out of the repository.
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"sepiano/host"
	"sepiano/z80"
)

// File-service request codes the Z80 sends on channel 2.
var requests = map[byte]string{
	host.ReqSetName: "set-name", host.ReqSetExt: "set-ext", host.ReqGetName: "get-name",
	host.ReqGetExt: "get-ext", host.ReqGetArg: "get-arg", host.ReqOpenPerf: "open-perf",
	host.ReqReadPerf: "read-perf", host.ReqCreatePerf: "create-perf", host.ReqWritePerf: "write-perf",
	host.ReqClosePerf: "close-perf", host.ReqCommitPerf: "commit-perf", host.ReqAbortPerf: "abort-perf",
	host.ReqOpenFile: "open-file", host.ReqReadFile: "read-file", host.ReqCreateFile: "create-file",
	host.ReqWriteFile: "write-file", host.ReqCloseFile: "close-file", host.ReqCommitFile: "commit-file",
	host.ReqAbortFile: "abort-file",
}

// analysis is what the survey finds in one program.
type analysis struct {
	name   string
	prog   *host.Program
	img    []byte
	org    uint16
	fwBase uint16 // where the program puts its copy of the firmware block
	fwSize int
	code   *z80.Program
	fw     map[int]int // firmware entry → calls
	fwOdd  map[uint16]int
	reqs   []string
	ports  map[string]int
	ram    map[uint16]string // firmware memory outside image and block → r/w
	texts  []string
}

func main() {
	out := flag.String("out", "", "also write each Z80 program image to `DIR` as NAME-LOAD.bin (keep it out of the repository)")
	dis := flag.String("dis", "", "print a listing of the program `NAME` (e.g. getlut) instead of the report")
	extra := flag.String("entries", "", "more entry points to trace from, as `HEX,HEX,...` (e.g. a command table's targets)")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: sesuite [-out DIR] [-dis NAME] FOLDER...")
		os.Exit(2)
	}
	var exes []string
	for _, dir := range flag.Args() {
		filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.EqualFold(filepath.Ext(p), ".exe") {
				exes = append(exes, p)
			}
			return nil
		})
	}
	sort.Slice(exes, func(i, j int) bool {
		return strings.ToLower(filepath.Base(exes[i])) < strings.ToLower(filepath.Base(exes[j]))
	})
	fwUse := map[int][]string{}
	for _, p := range exes {
		b, err := os.ReadFile(p)
		if err != nil {
			fmt.Printf("== %s: %v\n\n", p, err)
			continue
		}
		name := strings.TrimSuffix(strings.ToLower(filepath.Base(p)), ".exe")
		prog, err := host.ExtractProgram(b)
		if err != nil {
			if *dis == "" {
				fmt.Printf("== %s.exe (%d bytes): no Z80 program — a PC-only tool\n\n", name, len(b))
			}
			continue
		}
		a := analyze(name, prog, parseEntries(*extra))
		if *out != "" {
			bin := fmt.Sprintf("%s-%04X.bin", name, prog.Start)
			if err := os.WriteFile(filepath.Join(*out, bin), a.img, 0o644); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
		if *dis != "" {
			if strings.EqualFold(*dis, name) {
				fmt.Print(a.code.Listing(a.label))
			}
			continue
		}
		a.report()
		for e := range a.fw {
			fwUse[e] = append(fwUse[e], name)
		}
	}
	if *dis != "" {
		return
	}
	fmt.Println("== firmware entries by program")
	var es []int
	for e := range fwUse {
		es = append(es, e)
	}
	sort.Ints(es)
	for _, e := range es {
		fmt.Printf("   FW#%-3d (+%03XH)  %s\n", e, e*3, strings.Join(fwUse[e], " "))
	}
}

func analyze(name string, prog *host.Program, extra []uint16) *analysis {
	a := &analysis{name: name, prog: prog, img: prog.Image[16:], org: prog.Start,
		fw: map[int]int{}, fwOdd: map[uint16]int{}, ports: map[string]int{}, ram: map[uint16]string{}}
	// LD BC,size / LD DE,base / LD HL,(0001H): the firmware block copy
	for i := 0; i+9 <= len(a.img); i++ {
		m := a.img[i:]
		if m[0] == 0x01 && m[3] == 0x11 && m[6] == 0x2A && m[7] == 0x01 && m[8] == 0x00 {
			a.fwSize = int(m[1]) | int(m[2])<<8
			a.fwBase = uint16(m[4]) | uint16(m[5])<<8
			break
		}
	}
	a.code = z80.Trace(a.img, a.org, append([]uint16{prog.Entry}, extra...), inlineText)
	inBlock := func(t uint16) bool { return a.fwSize > 0 && t >= a.fwBase && int(t-a.fwBase) < a.fwSize }
	var addrs []int
	for pc := range a.code.Code {
		addrs = append(addrs, int(pc))
	}
	sort.Ints(addrs)
	for i, pc := range addrs {
		in := a.code.Code[uint16(pc)]
		if in.Flow == z80.Call || in.Flow == z80.Jump || in.Flow == z80.Branch {
			if inBlock(in.Target) {
				off := in.Target - a.fwBase
				if off%3 == 0 {
					a.fw[int(off/3)]++
				} else {
					a.fwOdd[off]++
				}
			}
		}
		if in.Port >= 0 {
			dir := "IN"
			if in.Out {
				dir = "OUT"
			}
			a.ports[fmt.Sprintf("%s %02XH", dir, in.Port)]++
		}
		if in.PortC {
			a.ports["(C)"]++
		}
		if in.Addr >= 0 && !a.code.In(uint16(in.Addr)) {
			t := uint16(in.Addr)
			rw := "r"
			if in.Write {
				rw = "w"
			}
			if inBlock(t) {
				continue
			}
			if !strings.Contains(a.ram[t], rw) {
				a.ram[t] += rw
			}
		}
		// LD A,request / CALL send: a file-service request
		if strings.HasPrefix(in.Text, "LD A,") && in.Len == 2 && i+1 < len(addrs) {
			code := a.img[pc-int(a.org)+1]
			next := a.code.Code[uint16(addrs[i+1])]
			if name, ok := requests[code]; ok && next.Flow == z80.Call && uint16(addrs[i+1]) == uint16(pc)+2 {
				a.reqs = append(a.reqs, name)
			}
		}
	}
	a.reqs = dedupe(a.reqs)
	sort.Strings(a.reqs)
	var datas []int
	for d := range a.code.Data {
		datas = append(datas, int(d))
	}
	sort.Ints(datas)
	for _, d := range datas {
		n := a.code.Data[uint16(d)]
		s := strings.TrimSpace(strings.Map(func(r rune) rune {
			if r < 0x20 || r >= 0x7F {
				return ' '
			}
			return r
		}, string(a.img[d-int(a.org):d-int(a.org)+n])))
		if s != "" {
			a.texts = append(a.texts, s)
		}
	}
	return a
}

// inlineText recognizes the suite's print routine: it starts with
// EX (SP),HL and prints the bytes after the call up to 7FH.
func inlineText(p *z80.Program, target, after uint16) int {
	if !p.In(target) || p.At(target) != 0xE3 {
		return 0
	}
	for a := after; p.In(a) && a-after < 512; a++ {
		if p.At(a) == 0x7F {
			return int(a-after) + 1
		}
	}
	return 0
}

func (a *analysis) label(t uint16) string {
	if a.fwSize > 0 && t >= a.fwBase && int(t-a.fwBase) < a.fwSize {
		off := t - a.fwBase
		if off%3 == 0 {
			return fmt.Sprintf("FW#%d", off/3)
		}
		return fmt.Sprintf("FW+%03XH", off)
	}
	if t == 0x0001 {
		return "firmware block address"
	}
	return ""
}

func (a *analysis) report() {
	cov := a.code.Coverage()
	fmt.Printf("== %s.exe\n", a.name)
	fmt.Printf("   banner %q\n", a.prog.Banner)
	fmt.Printf("   load %04X–%04X, entry %04X, %d bytes; traced %d bytes (%d%%)\n",
		a.prog.Start, a.prog.End, a.prog.Entry, len(a.img), cov, cov*100/max(1, len(a.img)))
	if a.fwSize > 0 {
		fmt.Printf("   firmware block: %d bytes (%d entries) copied to %04X\n", a.fwSize, a.fwSize/3, a.fwBase)
	}
	var fw []int
	for e := range a.fw {
		fw = append(fw, e)
	}
	sort.Ints(fw)
	var parts []string
	for _, e := range fw {
		parts = append(parts, fmt.Sprintf("#%d×%d", e, a.fw[e]))
	}
	for off, n := range a.fwOdd {
		parts = append(parts, fmt.Sprintf("+%03XH×%d", off, n))
	}
	fmt.Printf("   firmware entries: %s\n", strings.Join(parts, " "))
	fmt.Printf("   file requests: %s\n", strings.Join(a.reqs, ", "))
	if len(a.ports) > 0 {
		var ps []string
		for p, n := range a.ports {
			ps = append(ps, fmt.Sprintf("%s×%d", p, n))
		}
		sort.Strings(ps)
		fmt.Printf("   I/O: %s\n", strings.Join(ps, " "))
	}
	if len(a.ram) > 0 {
		var ks []int
		for k := range a.ram {
			ks = append(ks, int(k))
		}
		sort.Ints(ks)
		var rs []string
		for _, k := range ks {
			rs = append(rs, fmt.Sprintf("%04X:%s", k, a.ram[uint16(k)]))
		}
		fmt.Printf("   memory outside the program: %s\n", strings.Join(rs, " "))
	}
	fmt.Println("   messages:")
	for _, t := range a.texts {
		fmt.Printf("     | %s\n", t)
	}
	fmt.Println()
}

func dedupe(s []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// parseEntries reads a comma-separated list of hex addresses.
func parseEntries(s string) []uint16 {
	var out []uint16
	for _, f := range strings.Split(s, ",") {
		f = strings.TrimSuffix(strings.TrimSpace(strings.ToUpper(f)), "H")
		if f == "" {
			continue
		}
		v, err := strconv.ParseUint(f, 16, 16)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bad entry point %q\n", f)
			os.Exit(2)
		}
		out = append(out, uint16(v))
	}
	return out
}
