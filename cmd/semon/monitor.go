package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// console collects what the monitor prints and lets the driver wait for
// its prompt.
type console struct {
	mu   sync.Mutex
	buf  []byte
	more chan struct{} // closed and replaced on every write
	echo func([]byte)  // optional: shows the text as it arrives
}

func newConsole() *console { return &console{more: make(chan struct{})} }

func (c *console) Write(p []byte) (int, error) {
	c.mu.Lock()
	c.buf = append(c.buf, p...)
	close(c.more)
	c.more = make(chan struct{})
	c.mu.Unlock()
	if c.echo != nil {
		c.echo(p)
	}
	return len(p), nil
}

// untilPrompt waits for the monitor's prompt, "/" at the start of a line,
// and returns (and consumes) everything before it.
func (c *console) untilPrompt(ctx context.Context, timeout time.Duration) (string, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		c.mu.Lock()
		if len(c.buf) > 0 && c.buf[0] == '/' { // a prompt with nothing before it
			c.buf = c.buf[1:]
			c.mu.Unlock()
			return "", nil
		}
		if i := bytes.Index(c.buf, []byte("\n/")); i >= 0 {
			out := string(c.buf[:i+1])
			c.buf = c.buf[i+2:]
			c.mu.Unlock()
			return out, nil
		}
		more := c.more
		c.mu.Unlock()
		select {
		case <-more:
		case <-deadline.C:
			return "", errors.New("timed out waiting for the monitor's prompt")
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

// driver types commands into the monitor. It only ever sends D (dump) and
// Q (quit): nothing that changes memory, ports or the EEPROM.
type driver struct {
	con  *console
	send func([]byte) error
	gap  time.Duration // between keystrokes
}

func (d *driver) typeLine(line string) error {
	for _, k := range []byte(line + "\r") {
		if err := d.send([]byte{k}); err != nil {
			return err
		}
		if d.gap > 0 {
			time.Sleep(d.gap)
		}
	}
	return nil
}

// dump reads start..end (inclusive) with the monitor's D command.
func (d *driver) dump(ctx context.Context, start, end uint16) ([]byte, error) {
	if end < start {
		return nil, fmt.Errorf("bad range %04X-%04X", start, end)
	}
	if err := d.typeLine(fmt.Sprintf("D%04X %04X", start, end)); err != nil {
		return nil, err
	}
	n := int(end) - int(start) + 1
	text, err := d.con.untilPrompt(ctx, 15*time.Second+time.Duration(n)*5*time.Millisecond)
	if err != nil {
		return nil, err
	}
	return parseDump(text, start, end)
}

// quit sends Q; the monitor then returns to the firmware and ends the
// session.
func (d *driver) quit() error { return d.typeLine("Q") }

// parseDump reads the D command's output: lines "AAAA=XX XX … " followed by
// the same bytes as text, and checks that it covers start..end exactly.
func parseDump(text string, start, end uint16) ([]byte, error) {
	if strings.Contains(text, "SYNTAX ERROR") {
		return nil, errors.New("the monitor answered SYNTAX ERROR")
	}
	n := int(end) - int(start) + 1
	out := make([]byte, n)
	have := make([]bool, n)
	got := 0
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if len(line) < 5 || line[4] != '=' {
			continue // the echoed command, blank lines
		}
		addr, err := strconv.ParseUint(line[:4], 16, 16)
		if err != nil {
			continue
		}
		rest := line[5:]
		if len(rest)%4 != 0 {
			return nil, fmt.Errorf("malformed line %q", line)
		}
		k := len(rest) / 4
		for i := 0; i < k; i++ {
			tok := rest[3*i : 3*i+3]
			if tok[2] != ' ' {
				return nil, fmt.Errorf("malformed line %q", line)
			}
			v, err := strconv.ParseUint(tok[:2], 16, 8)
			if err != nil {
				return nil, fmt.Errorf("malformed line %q", line)
			}
			off := int(addr) + i - int(start)
			if off < 0 || off >= n {
				return nil, fmt.Errorf("line %q is outside %04X-%04X", line, start, end)
			}
			if have[off] {
				return nil, fmt.Errorf("address %04X dumped twice", int(start)+off)
			}
			out[off], have[off] = byte(v), true
			got++
		}
	}
	if got != n {
		for i, h := range have {
			if !h {
				return nil, fmt.Errorf("got %d of %d bytes; first missing %04X", got, n, int(start)+i)
			}
		}
	}
	return out, nil
}
