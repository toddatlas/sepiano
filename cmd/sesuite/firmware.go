package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"sepiano/z80"
)

// firmware studies a memory image read from the instrument: it follows
// every entry of the firmware's jump table and reports, per entry, the I/O
// ports and fixed memory its code touches (following calls inside the
// firmware), or prints the whole listing.
func firmware(path, orgHex, tableHex string, extra []uint16, list bool) int {
	img, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	orgs, tabs := parseEntries(orgHex), parseEntries(tableHex)
	if len(orgs) != 1 || len(tabs) != 1 {
		fmt.Fprintln(os.Stderr, "-fw needs -org and -table")
		return 2
	}
	org, tab := orgs[0], tabs[0]
	at := func(a uint16) byte {
		if a < org || int(a-org) >= len(img) {
			return 0
		}
		return img[a-org]
	}
	// The table: 3-byte JP entries for as long as they last.
	var targets []uint16
	entryOf := map[uint16]int{}
	for i := 0; ; i++ {
		a := tab + uint16(3*i)
		if at(a) != 0xC3 {
			break
		}
		t := uint16(at(a+1)) | uint16(at(a+2))<<8
		targets = append(targets, t)
		if _, dup := entryOf[t]; !dup {
			entryOf[t] = i
		}
	}
	p := z80.Trace(img, org, append(append([]uint16{}, targets...), extra...), inlineText)
	label := func(t uint16) string {
		if i, ok := entryOf[t]; ok {
			return fmt.Sprintf("FW#%d", i)
		}
		if t >= tab && t < tab+uint16(3*len(targets)) && (t-tab)%3 == 0 {
			return fmt.Sprintf("FW#%d (via the table)", (t-tab)/3)
		}
		return ""
	}
	if list {
		fmt.Print(p.Listing(label))
		return 0
	}
	fmt.Printf("%s: %d bytes at %04X; %d table entries at %04X; traced %d bytes\n",
		path, len(img), org, len(targets), tab, p.Coverage())
	// Ports used anywhere.
	ports := map[string][]uint16{}
	for pc, in := range p.Code {
		if in.Port >= 0 {
			k := fmt.Sprintf("IN  %02XH", in.Port)
			if in.Out {
				k = fmt.Sprintf("OUT %02XH", in.Port)
			}
			ports[k] = append(ports[k], pc)
		}
		if in.PortC {
			ports["(C)"] = append(ports["(C)"], pc)
		}
	}
	var pk []string
	for k := range ports {
		pk = append(pk, k)
	}
	sort.Strings(pk)
	fmt.Println("I/O instructions:")
	for _, k := range pk {
		fmt.Printf("  %-8s ×%d\n", k, len(ports[k]))
	}
	// Per entry: what its code reaches.
	fmt.Println("entries (target: ports, then fixed memory read/written, through calls):")
	for i, t := range targets {
		ps, mem, n := reach(p, t)
		fmt.Printf("  FW#%-3d %04X  %3d instr  %s  %s\n", i, t, n, strings.Join(ps, " "), strings.Join(mem, " "))
	}
	return 0
}

// reach walks the code from start, following jumps and calls inside the
// image, and lists the ports and absolute memory operands it meets.
func reach(p *z80.Program, start uint16) (ports, mem []string, n int) {
	seen := map[uint16]bool{}
	ps, ms := map[string]bool{}, map[string]bool{}
	work := []uint16{start}
	for len(work) > 0 {
		pc := work[len(work)-1]
		work = work[:len(work)-1]
		for {
			in, ok := p.Code[pc]
			if !ok || seen[pc] {
				break
			}
			seen[pc] = true
			n++
			if in.Port >= 0 {
				d := "in"
				if in.Out {
					d = "out"
				}
				ps[fmt.Sprintf("%s:%02X", d, in.Port)] = true
			}
			if in.PortC {
				ps["(C)"] = true
			}
			if in.Addr >= 0 {
				rw := "r"
				if in.Write {
					rw = "w"
				}
				ms[fmt.Sprintf("%04X%s", in.Addr, rw)] = true
			}
			switch in.Flow {
			case z80.Jump, z80.Branch, z80.Call:
				work = append(work, in.Target)
			}
			if in.Flow == z80.Jump || in.Flow == z80.Return || in.Flow == z80.Stop {
				break
			}
			next := pc + uint16(in.Len)
			if l, ok := p.Data[next]; ok {
				next += uint16(l)
			}
			pc = next
		}
	}
	for k := range ps {
		ports = append(ports, k)
	}
	for k := range ms {
		mem = append(mem, k)
	}
	sort.Strings(ports)
	sort.Strings(mem)
	if len(mem) > 12 {
		mem = append(mem[:12], fmt.Sprintf("…+%d", len(mem)-12))
	}
	return ports, mem, n
}
