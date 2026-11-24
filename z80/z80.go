// Package z80 decodes Z80 machine code: instruction lengths, mnemonics,
// control flow, I/O ports and absolute memory operands. It exists to study
// the programs the SE Program Suite uploads into the instrument, and the
// instrument's firmware once it can be read out.
//
// Decoding follows the standard x/y/z/p/q split of the opcode byte, with
// the CB, ED, DD and FD prefixes.
package z80

import "fmt"

// Flow says how an instruction passes control on.
type Flow int

const (
	Next       Flow = iota // falls through to the next instruction
	Jump                   // goes to Target
	Branch                 // goes to Target or falls through
	Call                   // calls Target, then falls through (also RST)
	Return                 // returns
	CondReturn             // returns or falls through
	Stop                   // no static successor: JP (HL), JP (IX), JP (IY)
)

// Inst is one decoded instruction.
type Inst struct {
	PC     uint16
	Len    int
	Text   string
	Flow   Flow
	Cond   bool   // a conditional Call
	Target uint16 // for Jump, Branch and Call

	Port  int  // the port of IN A,(n) / OUT (n),A, else -1
	Out   bool // Port is written
	PortC bool // uses the port in register C (IN r,(C), OUT (C),r, INI, OTIR, ...)

	Addr  int  // an absolute memory operand (nn), else -1
	Write bool // Addr is written
	Imm   int  // a 16-bit immediate (LD rr,nn), else -1
}

var (
	r8   = [8]string{"B", "C", "D", "E", "H", "L", "(HL)", "A"}
	rp   = [4]string{"BC", "DE", "HL", "SP"}
	rp2  = [4]string{"BC", "DE", "HL", "AF"}
	cc   = [8]string{"NZ", "Z", "NC", "C", "PO", "PE", "P", "M"}
	alu  = [8]string{"ADD A,", "ADC A,", "SUB ", "SBC A,", "AND ", "XOR ", "OR ", "CP "}
	rot  = [8]string{"RLC", "RRC", "RL", "RR", "SLA", "SRA", "SLL", "SRL"}
	acc  = [8]string{"RLCA", "RRCA", "RLA", "RRA", "DAA", "CPL", "SCF", "CCF"}
	misc = [8]string{"LD I,A", "LD R,A", "LD A,I", "LD A,R", "RRD", "RLD", "NOP*", "NOP*"}
	ims  = [8]string{"0", "0/1", "1", "2", "0", "0/1", "1", "2"}
	bli  = [4][4]string{
		{"LDI", "CPI", "INI", "OUTI"},
		{"LDD", "CPD", "IND", "OUTD"},
		{"LDIR", "CPIR", "INIR", "OTIR"},
		{"LDDR", "CPDR", "INDR", "OTDR"},
	}
)

type decoder struct {
	code    []byte
	org, pc uint16
	n       int    // bytes consumed
	idx     string // "", "IX" or "IY"
	in      *Inst
}

func (d *decoder) byte() byte {
	i := int(d.pc+uint16(d.n)) - int(d.org)
	d.n++
	if i < 0 || i >= len(d.code) {
		return 0
	}
	return d.code[i]
}

func (d *decoder) word() uint16 {
	lo := d.byte()
	return uint16(lo) | uint16(d.byte())<<8
}

func (d *decoder) rel() uint16 {
	e := int8(d.byte())
	return d.pc + uint16(d.n) + uint16(int16(e))
}

func (d *decoder) hl() string {
	if d.idx != "" {
		return d.idx
	}
	return "HL"
}

// mem is the (HL) operand, (IX+d) under a prefix (reading d).
func (d *decoder) mem() string {
	if d.idx == "" {
		return "(HL)"
	}
	e := int8(d.byte())
	if e < 0 {
		return fmt.Sprintf("(%s-%02XH)", d.idx, -int(e))
	}
	return fmt.Sprintf("(%s+%02XH)", d.idx, e)
}

// r is register i; under a prefix H and L become IXH and IXL, unless
// plain is set (the instruction also has an (IX+d) operand).
func (d *decoder) r(i int, plain bool) string {
	switch {
	case i == 6:
		return d.mem()
	case d.idx != "" && !plain && (i == 4 || i == 5):
		return d.idx + r8[i][:1]
	}
	return r8[i]
}

func (d *decoder) rp(p int) string {
	if p == 2 {
		return d.hl()
	}
	return rp[p]
}

func (d *decoder) rp2(p int) string {
	if p == 2 {
		return d.hl()
	}
	return rp2[p]
}

func hex16(v uint16) string { return fmt.Sprintf("%04XH", v) }
func hex8(v byte) string    { return fmt.Sprintf("%02XH", v) }

// Decode decodes the instruction at pc in mem, which is loaded at org.
// Bytes outside mem read as zero.
func Decode(mem []byte, org, pc uint16) Inst {
	in := Inst{PC: pc, Port: -1, Addr: -1, Imm: -1}
	d := &decoder{code: mem, org: org, pc: pc, in: &in}
	op := d.byte()
	for op == 0xDD || op == 0xFD {
		d.idx = map[byte]string{0xDD: "IX", 0xFD: "IY"}[op]
		op = d.byte()
	}
	switch op {
	case 0xCB:
		in.Text = d.cb()
	case 0xED:
		d.idx = ""
		in.Text = d.ed()
	default:
		in.Text = d.main(op)
	}
	in.Len = d.n
	return in
}

func (d *decoder) main(op byte) string {
	in := d.in
	x, y, z := int(op>>6), int(op>>3&7), int(op&7)
	p, q := y>>1, y&1
	switch x {
	case 0:
		switch z {
		case 0:
			switch y {
			case 0:
				return "NOP"
			case 1:
				return "EX AF,AF'"
			case 2:
				in.Flow, in.Target = Branch, d.rel()
				return "DJNZ " + hex16(in.Target)
			case 3:
				in.Flow, in.Target = Jump, d.rel()
				return "JR " + hex16(in.Target)
			}
			in.Flow, in.Target = Branch, d.rel()
			return "JR " + cc[y-4] + "," + hex16(in.Target)
		case 1:
			if q == 0 {
				nn := d.word()
				in.Imm = int(nn)
				return "LD " + d.rp(p) + "," + hex16(nn)
			}
			return "ADD " + d.hl() + "," + d.rp(p)
		case 2:
			switch p {
			case 0, 1:
				if q == 0 {
					return "LD (" + rp[p] + "),A"
				}
				return "LD A,(" + rp[p] + ")"
			}
			nn := d.word()
			in.Addr, in.Write = int(nn), q == 0
			reg := d.hl()
			if p == 3 {
				reg = "A"
			}
			if q == 0 {
				return "LD (" + hex16(nn) + ")," + reg
			}
			return "LD " + reg + ",(" + hex16(nn) + ")"
		case 3:
			return [2]string{"INC ", "DEC "}[q] + d.rp(p)
		case 4:
			return "INC " + d.r(y, false)
		case 5:
			return "DEC " + d.r(y, false)
		case 6:
			reg := d.r(y, false)
			return "LD " + reg + "," + hex8(d.byte())
		}
		return acc[y]
	case 1:
		if y == 6 && z == 6 {
			return "HALT"
		}
		plain := y == 6 || z == 6
		dst := d.r(y, plain)
		return "LD " + dst + "," + d.r(z, plain)
	case 2:
		return alu[y] + d.r(z, false)
	}
	switch z {
	case 0:
		in.Flow = CondReturn
		return "RET " + cc[y]
	case 1:
		if q == 0 {
			return "POP " + d.rp2(p)
		}
		switch p {
		case 0:
			in.Flow = Return
			return "RET"
		case 1:
			return "EXX"
		case 2:
			in.Flow = Stop
			return "JP (" + d.hl() + ")"
		}
		return "LD SP," + d.hl()
	case 2:
		in.Flow, in.Target = Branch, d.word()
		return "JP " + cc[y] + "," + hex16(in.Target)
	case 3:
		switch y {
		case 0:
			in.Flow, in.Target = Jump, d.word()
			return "JP " + hex16(in.Target)
		case 2:
			n := d.byte()
			in.Port, in.Out = int(n), true
			return "OUT (" + hex8(n) + "),A"
		case 3:
			n := d.byte()
			in.Port = int(n)
			return "IN A,(" + hex8(n) + ")"
		case 4:
			return "EX (SP)," + d.hl()
		case 5:
			return "EX DE,HL"
		case 6:
			return "DI"
		case 7:
			return "EI"
		}
	case 4:
		in.Flow, in.Cond, in.Target = Call, true, d.word()
		return "CALL " + cc[y] + "," + hex16(in.Target)
	case 5:
		if q == 0 {
			return "PUSH " + d.rp2(p)
		}
		if p == 0 {
			in.Flow, in.Target = Call, d.word()
			return "CALL " + hex16(in.Target)
		}
	case 6:
		return alu[y] + hex8(d.byte())
	case 7:
		in.Flow, in.Target = Call, uint16(y*8)
		return "RST " + hex8(byte(y*8))
	}
	return "DB " + hex8(op) // not reached: prefixes are handled by Decode
}

func (d *decoder) cb() string {
	var operand string
	if d.idx != "" {
		operand = d.mem() // DD CB d op: the displacement comes first
	}
	op := d.byte()
	x, y, z := int(op>>6), int(op>>3&7), int(op&7)
	if d.idx == "" {
		operand = r8[z]
	}
	switch x {
	case 0:
		return rot[y] + " " + operand
	case 1:
		return fmt.Sprintf("BIT %d,%s", y, operand)
	case 2:
		return fmt.Sprintf("RES %d,%s", y, operand)
	}
	return fmt.Sprintf("SET %d,%s", y, operand)
}

func (d *decoder) ed() string {
	in := d.in
	op := d.byte()
	x, y, z := int(op>>6), int(op>>3&7), int(op&7)
	p, q := y>>1, y&1
	if x == 1 {
		switch z {
		case 0:
			in.PortC = true
			if y == 6 {
				return "IN (C)"
			}
			return "IN " + r8[y] + ",(C)"
		case 1:
			in.PortC, in.Out = true, true
			if y == 6 {
				return "OUT (C),0"
			}
			return "OUT (C)," + r8[y]
		case 2:
			return [2]string{"SBC HL,", "ADC HL,"}[q] + rp[p]
		case 3:
			nn := d.word()
			in.Addr, in.Write = int(nn), q == 0
			if q == 0 {
				return "LD (" + hex16(nn) + ")," + rp[p]
			}
			return "LD " + rp[p] + ",(" + hex16(nn) + ")"
		case 4:
			return "NEG"
		case 5:
			in.Flow = Return
			if y == 1 {
				return "RETI"
			}
			return "RETN"
		case 6:
			return "IM " + ims[y]
		}
		return misc[y]
	}
	if x == 2 && z <= 3 && y >= 4 {
		if z >= 2 {
			in.PortC, in.Out = true, z == 3
		}
		return bli[y-4][z]
	}
	return "NOP* ED " + hex8(op)
}
