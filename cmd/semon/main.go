// Command semon reads the SE instrument's memory through Stahnke's Z80
// monitor (monitor.exe): it uploads the monitor, types D (dump) commands,
// parses what comes back into a binary file, and quits with Q. It never
// types anything else, so it cannot change memory, ports or the EEPROM.
//
//	semon -port /dev/sepiano-piano -exe monitor.exe -from 0000 -to 00FF -out page0.bin
//
// The service owns the serial port: stop sepianod first. The output is the
// instrument's firmware, Stahnke's copyrighted work: keep it out of the
// repository.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"sepiano/host"
)

func main() { os.Exit(run()) }

func hexArg(s string) (uint16, error) {
	v, err := strconv.ParseUint(s, 16, 16)
	return uint16(v), err
}

func run() int {
	port := flag.String("port", "", "serial port of the instrument")
	exe := flag.String("exe", "monitor.exe", "suite executable holding the Z80 monitor")
	from := flag.String("from", "0000", "first address, hex")
	to := flag.String("to", "00FF", "last address, hex")
	chunk := flag.Int("chunk", 0x400, "bytes per D command")
	out := flag.String("out", "", "binary file to write (required)")
	verbose := flag.Bool("v", false, "show the monitor's console on stderr")
	flag.Parse()
	start, err1 := hexArg(*from)
	end, err2 := hexArg(*to)
	if err := errors.Join(err1, err2); err != nil || *port == "" || *out == "" || end < start || *chunk < 16 {
		fmt.Fprintln(os.Stderr, "usage: semon -port DEV [-exe monitor.exe] -from HEX -to HEX -out FILE")
		flag.PrintDefaults()
		return 2
	}
	prog, err := host.LoadProgram(*exe)
	if err != nil {
		return fail(err)
	}
	if prog.Start != 0xA000 {
		return fail(fmt.Errorf("%s does not look like the monitor (loads at %04X)", *exe, prog.Start))
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	conn, err := host.Connect(ctx, *port, host.ConnectOptions{})
	if err != nil {
		return fail(err)
	}
	defer conn.Close()
	fmt.Fprintf(os.Stderr, "connected on %s at %d baud\n", *port, conn.Baud)

	con := newConsole()
	if *verbose {
		con.echo = func(p []byte) { os.Stderr.Write(p) }
	}
	keys := make(chan []byte, 64)
	tmp, err := os.MkdirTemp("", "semon")
	if err != nil {
		return fail(err)
	}
	defer os.RemoveAll(tmp)
	sess := &host.Session{Program: prog, Files: host.NewFileService(filepath.Join(tmp, "none.txt")), Console: con, Keys: keys}
	done := make(chan error, 1)
	go func() { done <- sess.Run(ctx, conn.Link, conn.Exited()) }()

	d := &driver{con: con, gap: 5 * time.Millisecond, send: func(k []byte) error {
		select {
		case keys <- k:
			return nil
		case err := <-done:
			done <- err
			return fmt.Errorf("session ended: %v", err)
		}
	}}
	if _, err := con.untilPrompt(ctx, 30*time.Second); err != nil {
		return fail(fmt.Errorf("monitor did not start: %w", err))
	}
	fmt.Fprintf(os.Stderr, "%s running\n", prog.Banner)

	image := make([]byte, 0, int(end)-int(start)+1)
	t0 := time.Now()
	var dumpErr error
	for a := int(start); a <= int(end); a += *chunk {
		b := min(a+*chunk-1, int(end))
		var data []byte
		for try := 0; try < 2; try++ {
			if data, dumpErr = d.dump(ctx, uint16(a), uint16(b)); dumpErr == nil {
				break
			}
			fmt.Fprintf(os.Stderr, "\n%04X-%04X: %v; retrying\n", a, b, dumpErr)
		}
		if dumpErr != nil {
			break
		}
		image = append(image, data...)
		fmt.Fprintf(os.Stderr, "\r%04X-%04X  %d bytes  %.0f s ", start, b, len(image), time.Since(t0).Seconds())
	}
	fmt.Fprintln(os.Stderr)
	path := *out
	if dumpErr != nil {
		path += ".partial"
	}
	if err := os.WriteFile(path, image, 0o644); err != nil {
		return fail(err)
	}
	fmt.Fprintf(os.Stderr, "wrote %d bytes (%04X-%04X) to %s\n", len(image), start, int(start)+len(image)-1, path)

	if err := d.quit(); err == nil {
		select {
		case err := <-done:
			if !errors.Is(err, host.ErrQuit) {
				fmt.Fprintf(os.Stderr, "session ended: %v\n", err)
			} else {
				fmt.Fprintln(os.Stderr, "monitor quit")
			}
		case <-time.After(5 * time.Second):
			fmt.Fprintln(os.Stderr, "the monitor did not quit within 5 s")
		}
	}
	if dumpErr != nil {
		return fail(dumpErr)
	}
	return 0
}

func fail(err error) int {
	fmt.Fprintln(os.Stderr, err)
	return 1
}
