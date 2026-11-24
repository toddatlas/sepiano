package z80

import (
	"strings"
	"testing"
)

func TestDecode(t *testing.T) {
	cases := []struct {
		code []byte
		text string
		len  int
		flow Flow
	}{
		{[]byte{0x00}, "NOP", 1, Next},
		{[]byte{0x01, 0x2B, 0x02}, "LD BC,022BH", 3, Next},
		{[]byte{0x2A, 0x01, 0x00}, "LD HL,(0001H)", 3, Next},
		{[]byte{0x32, 0x80, 0xEE}, "LD (EE80H),A", 3, Next},
		{[]byte{0x3E, 0x7F}, "LD A,7FH", 2, Next},
		{[]byte{0x7E}, "LD A,(HL)", 1, Next},
		{[]byte{0x76}, "HALT", 1, Next},
		{[]byte{0xBE}, "CP (HL)", 1, Next},
		{[]byte{0xC3, 0x7C, 0x01}, "JP 017CH", 3, Jump},
		{[]byte{0xC2, 0x83, 0x01}, "JP NZ,0183H", 3, Branch},
		{[]byte{0xCD, 0xF6, 0x0A}, "CALL 0AF6H", 3, Call},
		{[]byte{0xD4, 0x73, 0x01}, "CALL NC,0173H", 3, Call},
		{[]byte{0xC9}, "RET", 1, Return},
		{[]byte{0xD8}, "RET C", 1, CondReturn},
		{[]byte{0xE9}, "JP (HL)", 1, Stop},
		{[]byte{0xFF}, "RST 38H", 1, Call},
		{[]byte{0x18, 0xFE}, "JR 0100H", 2, Jump},
		{[]byte{0x10, 0x00}, "DJNZ 0102H", 2, Branch},
		{[]byte{0xD3, 0x40}, "OUT (40H),A", 2, Next},
		{[]byte{0xDB, 0x41}, "IN A,(41H)", 2, Next},
		{[]byte{0xE3}, "EX (SP),HL", 1, Next},
		{[]byte{0xCB, 0x7E}, "BIT 7,(HL)", 2, Next},
		{[]byte{0xCB, 0x27}, "SLA A", 2, Next},
		{[]byte{0xED, 0xB0}, "LDIR", 2, Next},
		{[]byte{0xED, 0x78}, "IN A,(C)", 2, Next},
		{[]byte{0xED, 0x53, 0x34, 0x12}, "LD (1234H),DE", 4, Next},
		{[]byte{0xED, 0x4D}, "RETI", 2, Return},
		{[]byte{0xDD, 0x21, 0x00, 0x80}, "LD IX,8000H", 4, Next},
		{[]byte{0xDD, 0x7E, 0x05}, "LD A,(IX+05H)", 3, Next},
		{[]byte{0xFD, 0x36, 0xFE, 0x12}, "LD (IY-02H),12H", 4, Next},
		{[]byte{0xDD, 0x66, 0x01}, "LD H,(IX+01H)", 3, Next},
		{[]byte{0xDD, 0x26, 0x01}, "LD IXH,01H", 3, Next},
		{[]byte{0xDD, 0xCB, 0x03, 0x46}, "BIT 0,(IX+03H)", 4, Next},
		{[]byte{0xDD, 0xE9}, "JP (IX)", 2, Stop},
		{[]byte{0xFD, 0xE5}, "PUSH IY", 2, Next},
	}
	for _, c := range cases {
		in := Decode(c.code, 0x0100, 0x0100)
		if in.Text != c.text || in.Len != c.len || in.Flow != c.flow {
			t.Errorf("% X: got %q len %d flow %d, want %q len %d flow %d", c.code, in.Text, in.Len, in.Flow, c.text, c.len, c.flow)
		}
	}
}

func TestDecodeOperands(t *testing.T) {
	in := Decode([]byte{0x3A, 0x80, 0xEE}, 0, 0)
	if in.Addr != 0xEE80 || in.Write {
		t.Errorf("LD A,(nn): addr %X write %v", in.Addr, in.Write)
	}
	in = Decode([]byte{0xED, 0x43, 0x00, 0x90}, 0, 0)
	if in.Addr != 0x9000 || !in.Write {
		t.Errorf("LD (nn),BC: addr %X write %v", in.Addr, in.Write)
	}
	in = Decode([]byte{0xD3, 0x10}, 0, 0)
	if in.Port != 0x10 || !in.Out {
		t.Errorf("OUT: port %X out %v", in.Port, in.Out)
	}
	in = Decode([]byte{0xED, 0xB3}, 0, 0)
	if !in.PortC || !in.Out {
		t.Errorf("OTIR: portC %v out %v", in.PortC, in.Out)
	}
}

// TestTrace follows a call to an inline-text routine and skips the text.
func TestTrace(t *testing.T) {
	code := []byte{
		0xCD, 0x0A, 0x01, // 0100 CALL 010A (print)
		'H', 'I', 0x7F, // 0103 inline text
		0xC3, 0x00, 0x01, // 0106 JP 0100
		0x00,                               // 0109 unreached
		0xE3,                               // 010A EX (SP),HL
		0x23, 0x7E, 0xFE, 0x7F, 0x20, 0xFA, // skip to 7F
		0x23, 0xE3, 0xC9,
	}
	inline := func(p *Program, target, after uint16) int {
		if p.At(target) != 0xE3 {
			return 0
		}
		for a := after; p.In(a); a++ {
			if p.At(a) == 0x7F {
				return int(a-after) + 1
			}
		}
		return 0
	}
	p := Trace(code, 0x0100, []uint16{0x0100}, inline)
	if n := p.Data[0x0103]; n != 3 {
		t.Errorf("inline text length %d, want 3", n)
	}
	if _, ok := p.Code[0x0106]; !ok {
		t.Error("the jump after the text was not traced")
	}
	if _, ok := p.Code[0x0109]; ok {
		t.Error("traced unreachable byte")
	}
	l := p.Listing(func(uint16) string { return "" })
	if !strings.Contains(l, `DB "HI\x7F"`) || !strings.Contains(l, "L010A:") {
		t.Errorf("listing:\n%s", l)
	}
}
