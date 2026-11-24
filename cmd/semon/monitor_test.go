package main

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeMonitor behaves like the monitor's console as traced from
// monitor.exe: it echoes what is typed, answers "Dssss eeee" with lines of
// "AAAA=XX … " plus the bytes as text, breaking lines at 16-byte boundaries,
// and prints "/" when ready.
type fakeMonitor struct {
	mem  [65536]byte
	out  *console
	keys chan byte
	seen []string // command lines received
}

func newFake(out *console) *fakeMonitor {
	f := &fakeMonitor{out: out, keys: make(chan byte, 256)}
	rand.New(rand.NewSource(1)).Read(f.mem[:])
	return f
}

func (f *fakeMonitor) say(s string) { f.out.Write([]byte(s)) }

func (f *fakeMonitor) run() {
	f.say("\r\nSTAHNKE Z80 MONITOR, V50.44\r\n/")
	var line []byte
	for k := range f.keys {
		if k != '\r' {
			line = append(line, k)
			f.say(string(k))
			continue
		}
		f.say("\r\n")
		cmd := string(line)
		line = nil
		f.seen = append(f.seen, cmd)
		switch {
		case cmd == "Q":
			return
		case strings.HasPrefix(cmd, "D"):
			var a, b uint64
			parts := strings.Fields(cmd[1:])
			if len(parts) != 2 {
				f.say("SYNTAX ERROR\r\n")
				break
			}
			a, _ = strconv.ParseUint(parts[0], 16, 16)
			b, _ = strconv.ParseUint(parts[1], 16, 16)
			f.dump(uint16(a), int(b)-int(a)+1)
		default:
			f.say("SYNTAX ERROR\r\n")
		}
		f.say("/")
	}
}

func (f *fakeMonitor) dump(hl uint16, bc int) {
	for {
		var s strings.Builder
		fmt.Fprintf(&s, "%04X=", hl)
		h, c := hl, bc
		for {
			fmt.Fprintf(&s, "%02X ", f.mem[h])
			h++
			c--
			if c == 0 || h&0x0F == 0 {
				break
			}
		}
		for {
			ch := f.mem[hl]
			if ch < 0x20 || ch >= 0x7F {
				ch = '.'
			}
			s.WriteByte(ch)
			hl++
			bc--
			if hl == 0 || bc == 0 {
				f.say(s.String() + "\r\n")
				return
			}
			if hl&0x0F == 0 {
				break
			}
		}
		f.say(s.String() + "\r\n")
	}
}

func setup(t *testing.T) (*driver, *fakeMonitor) {
	con := newConsole()
	f := newFake(con)
	go f.run()
	t.Cleanup(func() { close(f.keys) })
	d := &driver{con: con, send: func(k []byte) error {
		for _, b := range k {
			f.keys <- b
		}
		return nil
	}}
	if _, err := con.untilPrompt(context.Background(), time.Second); err != nil {
		t.Fatal(err)
	}
	return d, f
}

func TestDump(t *testing.T) {
	d, f := setup(t)
	for _, r := range [][2]uint16{{0x0000, 0x00FF}, {0x0105, 0x0123}, {0x2D10, 0x2D10}, {0xFFF0, 0xFFFF}, {0x1007, 0x10FF}} {
		got, err := d.dump(context.Background(), r[0], r[1])
		if err != nil {
			t.Fatalf("%04X-%04X: %v", r[0], r[1], err)
		}
		if want := f.mem[r[0] : int(r[1])+1]; !bytes.Equal(got, want) {
			t.Errorf("%04X-%04X: wrong bytes", r[0], r[1])
		}
	}
	if err := d.quit(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	for _, c := range f.seen {
		if c != "Q" && !strings.HasPrefix(c, "D") {
			t.Errorf("typed something other than D or Q: %q", c)
		}
	}
}

func TestParseDumpErrors(t *testing.T) {
	if _, err := parseDump("D0000 0010\r\nSYNTAX ERROR\r\n", 0, 0x10); err == nil {
		t.Error("accepted SYNTAX ERROR")
	}
	if _, err := parseDump("0000=01 02 ..\r\n", 0, 2); err == nil {
		t.Error("accepted a short dump")
	}
	if _, err := parseDump("0000=01 0Z AB\r\n", 0, 1); err == nil {
		t.Error("accepted a malformed line")
	}
	got, err := parseDump("D0000 0001\r\n0000=41 42 AB\r\n", 0, 1)
	if err != nil || !bytes.Equal(got, []byte{0x41, 0x42}) {
		t.Errorf("got % X, %v", got, err)
	}
}
