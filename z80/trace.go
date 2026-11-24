package z80

import (
	"fmt"
	"sort"
	"strings"
)

// Program is code traced from its entry points.
type Program struct {
	Mem  []byte
	Org  uint16
	Code map[uint16]Inst     // instructions found, by address
	Data map[uint16]int      // inline arguments after calls: address → length
	Refs map[uint16][]uint16 // jump and call targets inside the image → where from
}

// Inline reports how many bytes of inline argument follow a call to target
// whose return address is after, or 0. The suite's programs print text this
// way: CALL print / the text / 7FH.
type Inline func(p *Program, target, after uint16) int

// In says whether addr lies in the image.
func (p *Program) In(addr uint16) bool {
	return addr >= p.Org && int(addr-p.Org) < len(p.Mem)
}

// At is the byte at addr, or 0 outside the image.
func (p *Program) At(addr uint16) byte {
	if !p.In(addr) {
		return 0
	}
	return p.Mem[addr-p.Org]
}

// Trace follows jumps and calls from the entry points, decoding every
// instruction it reaches inside the image. Calls and jumps out of the image
// are kept as instructions but not followed. Code reached only through
// JP (HL) or a table is not found.
func Trace(mem []byte, org uint16, entries []uint16, inline Inline) *Program {
	p := &Program{Mem: mem, Org: org, Code: map[uint16]Inst{}, Data: map[uint16]int{}, Refs: map[uint16][]uint16{}}
	work := append([]uint16(nil), entries...)
	for len(work) > 0 {
		pc := work[len(work)-1]
		work = work[:len(work)-1]
	run:
		for p.In(pc) {
			if _, done := p.Code[pc]; done {
				break
			}
			if _, data := p.Data[pc]; data {
				break
			}
			in := Decode(mem, org, pc)
			p.Code[pc] = in
			next := pc + uint16(in.Len)
			switch in.Flow {
			case Jump, Branch, Call:
				if p.In(in.Target) {
					p.Refs[in.Target] = append(p.Refs[in.Target], pc)
					work = append(work, in.Target)
				}
			}
			if in.Flow == Call && inline != nil {
				if n := inline(p, in.Target, next); n > 0 {
					p.Data[next] = n
					next += uint16(n)
				}
			}
			switch in.Flow {
			case Jump, Return, Stop:
				break run
			}
			pc = next
		}
	}
	return p
}

// Coverage is the number of image bytes found to be code or inline data.
func (p *Program) Coverage() int {
	n := 0
	for _, in := range p.Code {
		n += in.Len
	}
	for _, l := range p.Data {
		n += l
	}
	return n
}

// Listing prints the program: traced code with labels, inline text, and
// everything else as data (text where it reads as text).
func (p *Program) Listing(label func(addr uint16) string) string {
	var b strings.Builder
	addrs := make([]int, 0, len(p.Code))
	for a := range p.Code {
		addrs = append(addrs, int(a))
	}
	sort.Ints(addrs)
	end := int(p.Org) + len(p.Mem)
	pc := int(p.Org)
	for pc < end {
		a := uint16(pc)
		if refs := p.Refs[a]; len(refs) > 0 {
			fmt.Fprintf(&b, "L%04X:\n", a)
		}
		if in, ok := p.Code[a]; ok {
			text := in.Text
			if l := label(in.Target); l != "" && (in.Flow == Call || in.Flow == Jump || in.Flow == Branch) {
				text += "  ; " + l
			} else if in.Addr >= 0 {
				if l := label(uint16(in.Addr)); l != "" {
					text += "  ; " + l
				}
			} else if in.Imm >= 0 {
				if l := label(uint16(in.Imm)); l != "" {
					text += "  ; " + l
				}
			}
			fmt.Fprintf(&b, "  %04X  %-12s %s\n", a, hexBytes(p.Mem[pc-int(p.Org):pc-int(p.Org)+in.Len]), text)
			pc += in.Len
			continue
		}
		if n, ok := p.Data[a]; ok {
			fmt.Fprintf(&b, "  %04X  %-12s %s\n", a, "", quote(p.Mem[pc-int(p.Org):pc-int(p.Org)+n]))
			pc += n
			continue
		}
		// data up to the next code, inline text or label
		stop := pc + 1
		for stop < end {
			s := uint16(stop)
			if _, ok := p.Code[s]; ok {
				break
			}
			if _, ok := p.Data[s]; ok {
				break
			}
			if len(p.Refs[s]) > 0 {
				break
			}
			stop++
		}
		for i := pc; i < stop; i += 16 {
			j := min(i+16, stop)
			chunk := p.Mem[i-int(p.Org) : j-int(p.Org)]
			fmt.Fprintf(&b, "  %04X  DB %-48s %s\n", i, hexBytes(chunk), printable(chunk))
		}
		pc = stop
	}
	return b.String()
}

func hexBytes(b []byte) string {
	var s strings.Builder
	for i, c := range b {
		if i > 0 {
			s.WriteByte(' ')
		}
		fmt.Fprintf(&s, "%02X", c)
	}
	return s.String()
}

func printable(b []byte) string {
	out := make([]byte, len(b))
	for i, c := range b {
		if c >= 0x20 && c < 0x7F {
			out[i] = c
		} else {
			out[i] = '.'
		}
	}
	return string(out)
}

// quote shows inline text as a string, with control bytes in hex.
func quote(b []byte) string {
	var s strings.Builder
	s.WriteString("DB \"")
	for _, c := range b {
		switch {
		case c >= 0x20 && c < 0x7F && c != '"':
			s.WriteByte(c)
		default:
			fmt.Fprintf(&s, "\\x%02X", c)
		}
	}
	s.WriteByte('"')
	return s.String()
}
