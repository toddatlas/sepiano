// Package sim is a stand-in for the SE instrument, so the piano service can
// be developed and tested without the piano.
//
// It speaks the real link protocol over a simulated cable (link.NewPipe),
// accepts a program upload on channel 0 and then behaves like Stahnke's
// player or recorder, depending on the uploaded program's banner: it prints
// the same console text, reads and writes performances through the host's
// file service, reacts to the player's keys and sends an 800 Hz performance
// clock in the ts field of its frames, as the real piano does.
//
// It does not run Z80 code. PlayerProgram and RecorderProgram return
// placeholder images, so no copyrighted program is needed; a real program's
// image works too and is recognised by its banner.
//
// Behaviour the real piano has not confirmed yet (see Phase 0 in
// docs/SERVICE-PLAN.md) is marked as an assumption below.
package sim

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"time"

	"sepiano/host"
	"sepiano/link"
	"sepiano/lut"
	"sepiano/se"
)

// Instrument is a simulated SE instrument. The zero value is not usable; use New.
type Instrument struct {
	// Speed scales the performance clock: 1 is real time. Tests run faster.
	Speed float64
	// Baud paces the simulated cable, as the real serial line would.
	Baud int
	// MinLevel is the lowest level the player goes down to: -16 on the real
	// piano, which prints nothing when Down is pressed there.
	MinLevel int
	// KeepLevel makes the level survive from one program to the next, as it
	// might if the player keeps it in firmware RAM. The real behaviour is not
	// known yet; by default each upload starts at 0.
	KeepLevel bool
	// InterlockFailure makes the recorder fail as the real one does when the
	// keyboard interlock is not ready.
	InterlockFailure bool
	// EEPROM is the instrument's lookup table, which a retriever program
	// (getlut) reads. New fills it with lut.Example().
	EEPROM []byte
	// Log, if set, receives one line per notable event.
	Log func(format string, args ...any)

	mu     sync.Mutex
	on     bool
	level  int
	cancel context.CancelFunc // the connection currently plugged in
	open   *hostPort          // the host's end of the latest cable
	gen    int
}

// New returns a powered-on instrument running in real time at 57600 baud.
func New() *Instrument {
	return &Instrument{Speed: 1, Baud: 57600, MinLevel: -16, on: true, EEPROM: lut.Example()}
}

func (in *Instrument) logf(format string, args ...any) {
	if in.Log != nil {
		in.Log(format, args...)
	}
}

// Power reports whether the instrument is switched on.
func (in *Instrument) Power() bool {
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.on
}

// SetPower switches the instrument on or off. Switching off silences the
// cable at once, as cutting the piano's power would.
func (in *Instrument) SetPower(on bool) {
	in.mu.Lock()
	defer in.mu.Unlock()
	if in.on == on {
		return
	}
	in.on = on
	in.logf("sim: power %v", on)
	if !on && in.cancel != nil {
		in.cancel()
		in.cancel = nil
	}
	if !on {
		in.level = 0
	}
}

// ErrBusy is returned by Dial while the previous cable is still open, as a
// real serial port refuses a second open ("Serial port busy" on Linux).
var ErrBusy = errors.New("sim: serial port busy")

// hostPort is the host's end of a cable; it remembers being closed.
type hostPort struct {
	host.Port
	once   sync.Once
	closed chan struct{}
}

func (p *hostPort) Close() error {
	p.once.Do(func() { close(p.closed) })
	return p.Port.Close()
}

// Dial plugs a new cable into the instrument and returns the host's end.
// Like a serial port, it can be open only once: Dial fails with ErrBusy until
// the previous end is closed. While the power is off nothing answers, so the
// host's handshake times out.
func (in *Instrument) Dial() (host.Port, error) {
	in.mu.Lock()
	defer in.mu.Unlock()
	if in.open != nil {
		select {
		case <-in.open.closed:
		default:
			return nil, ErrBusy
		}
	}
	pc, piano := link.NewPipe(0, 0)
	if in.cancel != nil {
		in.cancel()
		in.cancel = nil
	}
	hostEnd := &hostPort{Port: pc.(host.Port), closed: make(chan struct{})}
	in.open = hostEnd
	if !in.on {
		return hostEnd, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	in.cancel = cancel
	in.gen++
	go in.serve(ctx, piano, in.gen)
	return hostEnd, nil
}

// clock is the instrument's 800 Hz performance clock, in ticks.
type clock struct {
	mu      sync.Mutex
	speed   float64
	base    float64
	running bool
	since   time.Time
}

func (c *clock) value() int32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return int32(math.Floor(c.valueLocked()))
}

func (c *clock) valueLocked() float64 {
	if !c.running {
		return c.base
	}
	return c.base + time.Since(c.since).Seconds()*se.TicksPerSecond*c.speed
}

func (c *clock) set(t int32, run bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.base, c.running, c.since = float64(t), run, time.Now()
}

func (c *clock) run(on bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if on == c.running {
		return
	}
	c.base, c.running, c.since = c.valueLocked(), on, time.Now()
}

// conn is one plugged-in cable.
type conn struct {
	in    *Instrument
	l     *link.Link
	clk   *clock
	con   link.Channel
	file  link.Channel
	keys  chan string
	ctx   context.Context
	err   error
	errMu sync.Mutex
}

func (in *Instrument) serve(ctx context.Context, port link.Port, gen int) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	c := &conn{
		in:   in,
		l:    link.New(),
		clk:  &clock{speed: in.Speed},
		keys: make(chan string, 16),
		ctx:  ctx,
	}
	c.con = link.Channel{L: c.l, Ch: link.ChanConsole}
	c.file = link.Channel{L: c.l, Ch: link.ChanFile}
	pump := &link.Pump{L: c.l, Port: port, Baud: in.Baud, Silence: 2 * time.Second,
		Stamp: func() uint16 { return uint16(c.clk.value()) }}
	go func() {
		err := pump.Run(ctx)
		in.logf("sim: cable %d: %v", gen, err)
		cancel()
	}()
	go c.readKeys()
	for ctx.Err() == nil {
		img, err := c.readImage()
		if err != nil {
			return
		}
		p, err := host.ParseProgram(img)
		if err != nil {
			in.logf("sim: bad upload: %v", err)
			return
		}
		in.logf("sim: running %q", p.Banner)
		c.clk.set(0, false)
		if strings.Contains(p.Banner, "LOOKUP TABLE RETRIEVER") {
			err = c.retriever(p.Banner)
		} else if strings.Contains(p.Banner, "RECORDER") {
			err = c.recorder(p.Banner)
		} else {
			err = c.player(p.Banner)
		}
		if err != nil {
			in.logf("sim: program ended: %v", err)
			return
		}
	}
}

// readImage reads one program upload from channel 0.
func (c *conn) readImage() ([]byte, error) {
	boot := link.Channel{L: c.l, Ch: link.ChanBoot}
	hdr := make([]byte, 16)
	if _, err := io.ReadFull(boot, hdr); err != nil {
		return nil, err
	}
	start := int(hdr[2]) | int(hdr[3])<<8
	end := int(hdr[4]) | int(hdr[5])<<8
	if hdr[0] != 0 || hdr[1] != 3 || end < start {
		return nil, errors.New("sim: not a load image")
	}
	body := make([]byte, end-start+1)
	if _, err := io.ReadFull(boot, body); err != nil {
		return nil, err
	}
	return append(hdr, body...), nil
}

// readKeys decodes keystrokes from the console channel into key names.
func (c *conn) readKeys() {
	for {
		b, err := c.con.ReadByte()
		if err != nil {
			close(c.keys)
			return
		}
		var k string
		switch b {
		case 0x00:
			sc, err := c.con.ReadByte()
			if err != nil {
				close(c.keys)
				return
			}
			k = map[byte]string{0x48: "up", 0x50: "down", 0x4B: "left", 0x4D: "right"}[sc]
			if k == "" {
				k = fmt.Sprintf("scan%02x", sc)
			}
		case 0x0D:
			k = "enter"
		case 0x1B:
			k = "esc"
		case ' ':
			k = "space"
		default:
			k = string(rune(b))
		}
		select {
		case c.keys <- k:
		default: // a real console would beep; drop it
		}
	}
}

func (c *conn) print(s string) error {
	_, err := c.con.Write([]byte(s))
	return err
}

// end prints the last text and the ESC that tells the host the program is done.
func (c *conn) end(s string) error { return c.print(s + "\x1b") }

// call sends a file-service request and reads a reply of n bytes.
func (c *conn) call(req []byte, n int) ([]byte, error) {
	if _, err := c.file.Write(req); err != nil {
		return nil, err
	}
	reply := make([]byte, n)
	if _, err := io.ReadFull(c.file, reply); err != nil {
		return nil, err
	}
	if reply[0] != req[0] {
		return nil, fmt.Errorf("sim: file service answered %#02x to %#02x", reply[0], req[0])
	}
	return reply, nil
}

// readAll reads file fn to the end in blocks of size, as the player does.
func (c *conn) readAll(fn byte, size int) ([]byte, error) {
	var all []byte
	for {
		hdr, err := c.call([]byte{host.ReqReadPerf, fn, byte(size), byte(size >> 8)}, 4)
		if err != nil {
			return nil, err
		}
		k := int(hdr[2]) | int(hdr[3])<<8
		buf := make([]byte, k)
		if _, err := io.ReadFull(c.file, buf); err != nil {
			return nil, err
		}
		all = append(all, buf...)
		if k == 0 {
			return all, nil
		}
	}
}

func (c *conn) levelLine() string {
	c.in.mu.Lock()
	level := c.in.level
	c.in.mu.Unlock()
	if level == 0 {
		return "LEVEL=0 (NORMAL)\r\n"
	}
	return fmt.Sprintf("LEVEL=%d\r\n", level)
}

// changeLevel moves the level and reports whether it changed.
func (c *conn) changeLevel(d int) bool {
	c.in.mu.Lock()
	defer c.in.mu.Unlock()
	old := c.in.level
	c.in.level = min(0, max(c.in.MinLevel, c.in.level+d))
	return c.in.level != old
}

// fmtTime formats clock ticks as the real player does in "STOP PLAYING AT
// TIME=3912": hundredths of a second.
func fmtTime(t int32) string {
	return fmt.Sprint(max(t, 0) * 100 / se.TicksPerSecond)
}

// player imitates the PERFORMANCE PLAYER program.
func (c *conn) player(banner string) error {
	if err := c.print("\r\n" + banner + "\r\n"); err != nil {
		return err
	}
	if _, err := c.call([]byte{host.ReqOpenPerf}, 1); err != nil {
		return err
	}
	kbRaw, err := c.readAll(0, 0x400)
	if err != nil {
		return err
	}
	if _, err := c.readAll(1, 0x200); err != nil {
		return err
	}
	if _, err := c.readAll(2, 0x100); err != nil {
		return err
	}
	kb, err := se.ParseKB(kbRaw)
	if err != nil {
		return c.end("\r\nINVALID PERFORMANCE FILE\r\n")
	}
	c.in.mu.Lock()
	if !c.in.KeepLevel {
		c.in.level = 0
	}
	c.in.mu.Unlock()
	text := strings.TrimRight(kb.Text, "\r\n")
	if err := c.print("\r\n" + text + "\r\n\r\n\r\n" + c.levelLine() +
		"DEPRESS DOWN/UP ARROW TO SUBDUE/RESTORE\r\n" +
		"DEPRESS SPACE BAR TO STOP/RESUME PLAYING\r\n" +
		"DEPRESS ENTER TO QUIT\r\n" +
		"DEPRESS ESCAPE TO RESTART\r\n\r\n"); err != nil {
		return err
	}
	// The real clock counts up from about -194 while the key help prints and
	// passes 0 about 0.2 s after "START PLAYING".
	c.clk.set(-194, true)
	if !c.sleep(-30) {
		return c.ctx.Err()
	}
	if err := c.print("START PLAYING\r\n"); err != nil {
		return err
	}
	end := int32(kb.End)
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return c.ctx.Err()
		case <-tick.C:
			if c.clk.value() >= end {
				c.clk.set(end, false)
				return c.end("END OF PERFORMANCE\r\n")
			}
		case k, ok := <-c.keys:
			if !ok {
				return errors.New("sim: console closed")
			}
			switch k {
			case "space":
				c.clk.mu.Lock()
				running := c.clk.running
				c.clk.mu.Unlock()
				if running {
					c.clk.run(false)
					err = c.print("STOP PLAYING AT TIME=" + fmtTime(c.clk.value()) + "\r\n")
				} else {
					c.clk.run(true)
					err = c.print("RESUME PLAYING\r\n")
				}
			case "enter":
				c.clk.run(false)
				return c.end("QUIT PLAYING\r\n")
			case "esc":
				// An assumption: the player starts over with a fresh clock.
				c.clk.set(-194, true)
				err = c.print("RESTART PERFORMANCE\r\n")
				if err == nil && c.sleep(-30) {
					err = c.print("START PLAYING\r\n")
				}
			case "down", "up":
				// At a limit the real player prints nothing.
				if c.changeLevel(map[string]int{"down": -1, "up": 1}[k]) {
					err = c.print(c.levelLine())
				}
			}
			if err != nil {
				return err
			}
		}
	}
}

// sleep waits until the clock reaches t; false if the connection ended.
func (c *conn) sleep(t int32) bool {
	for c.clk.value() < t {
		select {
		case <-c.ctx.Done():
			return false
		case <-time.After(2 * time.Millisecond):
		}
	}
	return true
}

// waitKey waits for one of the given keys and returns it.
func (c *conn) waitKey(want ...string) (string, error) {
	for {
		select {
		case <-c.ctx.Done():
			return "", c.ctx.Err()
		case k, ok := <-c.keys:
			if !ok {
				return "", errors.New("sim: console closed")
			}
			for _, w := range want {
				if k == w {
					return k, nil
				}
			}
		}
	}
}

// recorder imitates the PERFORMANCE RECORDER program. Its prompts are the
// documented ones; how the real recorder names and writes its files has not
// been checked yet (Phase 0), so this follows the file service as specified:
// create name.kb$/.lp$/.sp$, write, close, commit.
func (c *conn) recorder(banner string) error {
	if err := c.print("\r\n" + banner + "\r\n\r\nDEPRESS SPACE BAR TO START RECORDING\r\n"); err != nil {
		return err
	}
	if k, err := c.waitKey("space", "esc"); err != nil {
		return err
	} else if k == "esc" {
		return c.end("QUIT\r\n")
	}
	if c.in.InterlockFailure {
		return c.end("KEYBOARD INTERLOCK FAILURE\r\n")
	}
	c.clk.set(0, true)
	if err := c.print("RECORDING\r\nDEPRESS ENTER TO END RECORDING\r\n"); err != nil {
		return err
	}
	if _, err := c.waitKey("enter"); err != nil {
		return err
	}
	c.clk.run(false)
	length := max(c.clk.value(), se.TicksPerSecond)
	kb, lp, sp, err := Phrase(uint32(length)).Encode()
	if err != nil {
		return err
	}
	if _, err := c.call([]byte{host.ReqCreatePerf}, 1); err != nil {
		return err
	}
	for fn, data := range [][]byte{kb, lp, sp} {
		for off := 0; off < len(data); off += 0x400 {
			chunk := data[off:min(off+0x400, len(data))]
			req := append([]byte{host.ReqWritePerf, byte(fn), byte(len(chunk)), byte(len(chunk) >> 8)}, chunk...)
			if _, err := c.call(req, 4); err != nil {
				return err
			}
		}
	}
	if _, err := c.call([]byte{host.ReqClosePerf}, 1); err != nil {
		return err
	}
	if _, err := c.call([]byte{host.ReqCommitPerf}, 1); err != nil {
		return err
	}
	return c.end("END OF RECORDING\r\n")
}

// Phrase returns a short synthetic performance lasting about length ticks:
// a rising and falling arpeggio with the damper pedal changing every bar.
func Phrase(length uint32) *se.Performance {
	kb := &se.KB{Format: 0x0F, Model: 0x11, Text: "Recorded with the sepiano simulator\r\n"}
	notes := []byte{48, 52, 55, 60, 64, 67, 72, 67, 64, 60, 55, 52}
	const step = 200 // 0.25 s
	var i int
	for t := uint32(se.TicksPerSecond); t+step < length; t += step {
		k := notes[i%len(notes)]
		kb.Events = append(kb.Events,
			se.Event{Tick: t, Key: k, On: true, Vel: uint16(300 + 20*(i%12))},
			se.Event{Tick: t + step*3/4, Key: k})
		i++
	}
	kb.End = max(length, se.TicksPerSecond*2)
	damper := &se.Pedal{}
	for t := uint32(0); t < kb.End; t += se.DamperPeriod {
		var v byte
		if (t/(se.TicksPerSecond*2))%2 == 1 {
			v = 0x7F
		}
		damper.Samples = append(damper.Samples, v)
	}
	return &se.Performance{KB: kb, Damper: damper, Soft: &se.Pedal{}}
}

// Demo returns synthetic performances for a simulated library, named in the
// Zenph style so the metadata parser has something to work with.
func Demo() map[string]*se.Performance {
	return map[string]*se.Performance{
		"01-Simulator20261122-Sepiano_ArpeggioInC-Steinway_SED133291": Phrase(30 * se.TicksPerSecond),
		// A second rendering must differ, or the library treats it as a duplicate.
		"01-Simulator20261122-Sepiano_ArpeggioInC-Yamaha_CFIIIS":    Phrase(31 * se.TicksPerSecond),
		"02-Simulator20261122-Sepiano_LongStudy-Steinway_SED133291": Phrase(150 * se.TicksPerSecond),
	}
}

// placeholder builds a load image of n bytes in total with the given banner,
// laid out like the suite's images.
func placeholder(banner string, n int) *host.Program {
	body := []byte("\r\n" + banner + "\r\n\x7f")
	body = append(body, bytes.Repeat([]byte{0xC9}, n-16-len(body))...) // Z80 RET
	start, end := uint16(0x100), uint16(0x100+len(body)-1)
	img := []byte{0, 3, byte(start), byte(start >> 8), byte(end), byte(end >> 8), 0x53, 0x01, 0, 0, 0, 0, 0, 0, 0, 0}
	p, err := host.ParseProgram(append(img, body...))
	if err != nil {
		panic(err)
	}
	return p
}

// PlayerProgram is a placeholder for play.exe's program, the same size as
// the real one so uploads take as long.
func PlayerProgram() *host.Program {
	return placeholder("SIMULATED SE/PC PERFORMANCE PLAYER, V50.45", 8131)
}

// RecorderProgram is a placeholder for record.exe's program.
func RecorderProgram() *host.Program {
	return placeholder("SIMULATED SE/PC PERFORMANCE RECORDER, V30.44", 4330)
}

// retriever imitates the LOOKUP TABLE RETRIEVER (getlut.exe): it sends the
// whole EEPROM to the host's file service as name.$$$ in 128-byte blocks,
// then has it renamed to name.ext, with the original's console text.
func (c *conn) retriever(banner string) error {
	c.in.mu.Lock()
	img := append([]byte(nil), c.in.EEPROM...)
	c.in.mu.Unlock()
	if err := c.print("\r\n" + banner + "\r\n"); err != nil {
		return err
	}
	if _, err := c.call([]byte{host.ReqCreateFile}, 1); err != nil {
		return err
	}
	if err := c.print("\r\nBEGIN RETRIEVAL\r\n"); err != nil {
		return err
	}
	for i := 0; i < len(img); i += 128 {
		n := min(128, len(img)-i)
		req := append([]byte{host.ReqWriteFile, byte(n), byte(n >> 8)}, img[i:i+n]...)
		if _, err := c.call(req, 3); err != nil {
			return err
		}
	}
	if err := c.print("RETRIEVAL COMPLETE\r\n"); err != nil {
		return err
	}
	if _, err := c.call([]byte{host.ReqCloseFile}, 1); err != nil {
		return err
	}
	if _, err := c.call([]byte{host.ReqCommitFile}, 1); err != nil {
		return err
	}
	return c.end("")
}

// RetrieverProgram is a placeholder for getlut.exe's program.
func RetrieverProgram() *host.Program {
	return placeholder("SIMULATED SE/PC LOOKUP TABLE RETRIEVER, V50.44", 987)
}
