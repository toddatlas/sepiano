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
	// WedgeOnDrop makes the instrument behave as the real one did on
	// 2026-11-24: if the host drops the link while a program is being
	// uploaded or is running (rather than letting it end by itself), the
	// controller is left stuck. It still answers on the link, but takes no
	// more programs until it is switched off and on.
	WedgeOnDrop bool
	// Log, if set, receives one line per notable event.
	Log func(format string, args ...any)

	mu     sync.Mutex
	on     bool
	level  int
	wedged bool               // see WedgeOnDrop
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

// Wedged says whether the controller is stuck (see WedgeOnDrop).
func (in *Instrument) Wedged() bool {
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.wedged
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
	in.wedged = false // a power cycle restarts the controller
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
	in.mu.Lock()
	wedged := in.wedged
	in.mu.Unlock()
	if wedged {
		// The link layer still answers, but nothing takes a program.
		in.logf("sim: stuck: ignoring uploads until power is cycled")
		io.Copy(io.Discard, link.Channel{L: c.l, Ch: link.ChanBoot})
		return
	}
	busy := false // a program is arriving or running
	defer func() {
		if busy && in.WedgeOnDrop {
			in.mu.Lock()
			in.wedged = true
			in.mu.Unlock()
			in.logf("sim: the link was dropped while a program was loading or running: stuck")
		}
	}()
	for ctx.Err() == nil {
		img, err := c.readImage(&busy)
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
		} else if strings.Contains(p.Banner, "LOOKUP TABLE INSTALLER") {
			err = c.installer(p.Banner, !strings.Contains(p.Banner, "PROTOTYPE"))
		} else if strings.Contains(p.Banner, "PERFORMANCE LOADER") {
			err = c.loader(p.Banner)
		} else if strings.HasPrefix(p.Banner, "SIMULATED TOOL") {
			err = c.tool(p.Banner)
		} else if strings.Contains(p.Banner, "RECORDER") {
			err = c.recorder(p.Banner)
		} else {
			err = c.player(p.Banner)
		}
		if err != nil {
			in.logf("sim: program ended: %v", err)
			return
		}
		busy = false // it ended by itself: back in the loader
	}
}

// readImage reads one program upload from channel 0; busy is set once its
// first byte arrives.
func (c *conn) readImage(busy *bool) ([]byte, error) {
	boot := link.Channel{L: c.l, Ch: link.ChanBoot}
	hdr := make([]byte, 16)
	if _, err := io.ReadFull(boot, hdr[:1]); err != nil {
		return nil, err
	}
	*busy = true
	if _, err := io.ReadFull(boot, hdr[1:]); err != nil {
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

// recorder imitates the PERFORMANCE RECORDER (record.exe V30.44) as its Z80
// code reads:
//   - it checks the keyboard interlock;
//   - it takes the recording type and the piano's serial number from the
//     table's section 12, and refuses with "ILLEGAL RECORDING TYPE OR MODE"
//     when either is 0;
//   - it creates name.kb$/.lp$/.sp$ and asks for the command-line argument,
//     which becomes the header text;
//   - it prints three prompts. Space starts ("START RECORDING"); Enter
//     before that quits ("QUIT RECORDING"); Esc at any time discards and
//     starts over ("DISCARD RECORDING");
//   - Enter while recording ends it ("END OF RECORDING"): the files are
//     closed, committed, and the program exits.
//
// The real recorder writes as it goes; this one writes a phrase as long as
// the recording at the end, with the real header bytes.
func (c *conn) recorder(banner string) error {
	if err := c.print("\r\n" + banner + "\r\n"); err != nil {
		return err
	}
	if c.in.InterlockFailure {
		return c.end("\r\nKEYBOARD INTERLOCK FAILURE\r\nVERIFY THAT KEYBOARD CONNECTORS ARE PROPERLY MATED\r\n")
	}
	kind, serial := c.recordingType()
	if kind == 0 || serial == 0 {
		return c.end("ILLEGAL RECORDING TYPE OR MODE\r\n")
	}
	abort := func() error {
		if _, err := c.call([]byte{host.ReqClosePerf}, 1); err != nil {
			return err
		}
		_, err := c.call([]byte{host.ReqAbortPerf}, 1)
		return err
	}
	for {
		if _, err := c.call([]byte{host.ReqCreatePerf}, 1); err != nil {
			return err
		}
		text, err := c.argument()
		if err != nil {
			return err
		}
		if err := c.print("\r\nDEPRESS SPACE BAR TO START RECORDING\r\nDEPRESS ENTER TO END RECORDING\r\nDEPRESS ESCAPE TO RESTART\r\n"); err != nil {
			return err
		}
		k, err := c.waitKey("space", "enter", "esc")
		if err != nil {
			return err
		}
		switch k {
		case "enter":
			if err := c.print("QUIT RECORDING\r\n"); err != nil {
				return err
			}
			if err := abort(); err != nil {
				return err
			}
			return c.end("")
		case "esc":
			if err := c.print("DISCARD RECORDING\r\n"); err != nil {
				return err
			}
			if err := abort(); err != nil {
				return err
			}
			continue
		}
		c.clk.set(0, true)
		if err := c.print("\r\nSTART RECORDING\r\n"); err != nil {
			return err
		}
		k, err = c.waitKey("enter", "esc")
		if err != nil {
			return err
		}
		c.clk.run(false)
		if k == "esc" {
			if err := c.print("DISCARD RECORDING\r\n"); err != nil {
				return err
			}
			if err := abort(); err != nil {
				return err
			}
			continue
		}
		if err := c.print("END OF RECORDING\r\n"); err != nil {
			return err
		}
		p := Phrase(uint32(max(c.clk.value(), se.TicksPerSecond)))
		p.KB.Format, p.KB.Model, p.KB.Text = kind, serial, text
		kb, lp, sp, err := p.Encode()
		if err != nil {
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
		return c.end("")
	}
}

// recordingType reads the recording type and serial number from the
// table's section 12, as the recorder does (FW#80).
func (c *conn) recordingType() (kind, serial byte) {
	c.in.mu.Lock()
	img := append([]byte(nil), c.in.EEPROM...)
	c.in.mu.Unlock()
	t, err := lut.Parse(img)
	if err != nil {
		return 0, 0
	}
	s := t.Section(12)
	if len(s) < 3 {
		return 0, 0
	}
	return s[1], s[2]
}

// argument asks the host for the command-line argument (request 0FH), as
// the recorder does for its header text.
func (c *conn) argument() (string, error) {
	if _, err := c.file.Write([]byte{host.ReqGetArg}); err != nil {
		return "", err
	}
	var text []byte
	first := true
	for {
		b, err := c.file.ReadByte()
		if err != nil {
			return "", err
		}
		if first {
			if b != host.ReqGetArg {
				return "", fmt.Errorf("sim: file service answered %#02x to get-arg", b)
			}
			first = false
			continue
		}
		if b == 0x7F {
			return string(text), nil
		}
		text = append(text, b)
	}
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

// tool stands in for the suite's other programs (cal, sustain, setup, …):
// it prints its banner, reports each key it receives, and quits on Enter.
func (c *conn) tool(banner string) error {
	if err := c.print("\r\n" + banner + "\r\n\r\nDEPRESS ENTER TO QUIT\r\n"); err != nil {
		return err
	}
	for {
		select {
		case k, ok := <-c.keys:
			if !ok {
				return io.EOF
			}
			if k == "enter" {
				return c.end("QUIT\r\n")
			}
			if err := c.print("KEY " + strings.ToUpper(k) + "\r\n"); err != nil {
				return err
			}
		case <-c.ctx.Done():
			return c.ctx.Err()
		}
	}
}

// ToolProgram is a placeholder for one of the suite's other programs.
func ToolProgram(title string) *host.Program {
	return placeholder("SIMULATED TOOL: "+strings.ToUpper(title), 600)
}

// installer imitates the LOOKUP TABLE INSTALLER. putlut.exe reads a table
// from the host's file (name.ext) and writes it into the EEPROM; the
// prototype installers (put225, put275, put290, putb, putc7) write the
// table they carry, for which the simulator writes lut.Example().
func (c *conn) installer(banner string, fromFile bool) error {
	if err := c.print("\r\n" + banner + "\r\n"); err != nil {
		return err
	}
	var img []byte
	if fromFile {
		if _, err := c.call([]byte{host.ReqOpenFile}, 1); err != nil {
			return err
		}
		for {
			hdr, err := c.call([]byte{host.ReqReadFile, 128, 0}, 3)
			if err != nil {
				return err
			}
			n := int(hdr[1]) | int(hdr[2])<<8
			buf := make([]byte, n)
			if _, err := io.ReadFull(c.file, buf); err != nil {
				return err
			}
			img = append(img, buf...)
			if n == 0 {
				break
			}
		}
	} else {
		img = lut.Example()
		if err := c.print("\r\nPROTOTYPE LOOKUP TABLE, V30.17\r\n"); err != nil {
			return err
		}
	}
	if err := c.print("\r\nBEGIN INSTALLATION\r\n"); err != nil {
		return err
	}
	c.in.mu.Lock()
	copy(c.in.EEPROM, img)
	c.in.mu.Unlock()
	if !c.pause(300 * time.Millisecond) { // writing 2 KB a byte at a time takes a moment
		return c.ctx.Err()
	}
	return c.end("INSTALLATION COMPLETE\r\n")
}

// InstallerProgram is a placeholder for putlut.exe (fromFile) or one of the
// prototype installers.
func InstallerProgram(fromFile bool) *host.Program {
	if fromFile {
		return placeholder("SIMULATED SE/PC LOOKUP TABLE INSTALLER, V50.44", 1010)
	}
	return placeholder("SIMULATED SE/PC LOOKUP TABLE INSTALLER (PROTOTYPE), V50.44", 2595)
}

// loader imitates the PERFORMANCE LOADER (load.exe), which reads a
// performance from tape into files: the simulator "loads" a short phrase.
func (c *conn) loader(banner string) error {
	if err := c.print("\r\n" + banner + "\r\n\r\nSTART TAPE RECORDER TO LOAD\r\nDEPRESS ENTER TO QUIT\r\n"); err != nil {
		return err
	}
	if !c.pause(300 * time.Millisecond) {
		return c.ctx.Err()
	}
	kb, lp, sp, err := Phrase(6 * se.TicksPerSecond).Encode()
	if err != nil {
		return err
	}
	if _, err := c.call([]byte{host.ReqCreatePerf}, 1); err != nil {
		return err
	}
	if err := c.print("START LOADING\r\n"); err != nil {
		return err
	}
	for fn, data := range [][]byte{kb, lp, sp} {
		for i := 0; i < len(data); i += 256 {
			n := min(256, len(data)-i)
			req := append([]byte{host.ReqWritePerf, byte(fn), byte(n), byte(n >> 8)}, data[i:i+n]...)
			if _, err := c.call(req, 4); err != nil {
				return err
			}
		}
	}
	if err := c.print("END OF PERFORMANCE\r\n"); err != nil {
		return err
	}
	if _, err := c.call([]byte{host.ReqClosePerf}, 1); err != nil {
		return err
	}
	if _, err := c.call([]byte{host.ReqCommitPerf}, 1); err != nil {
		return err
	}
	return c.end("")
}

// LoaderProgram is a placeholder for load.exe.
func LoaderProgram() *host.Program {
	return placeholder("SIMULATED SE/PC PERFORMANCE LOADER, V50.44", 3503)
}

// pause waits d of real time (the clock does not run for these programs);
// false if the connection ended.
func (c *conn) pause(d time.Duration) bool {
	select {
	case <-c.ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
