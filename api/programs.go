package api

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"sepiano/host"
)

// Programs keeps the suite executables (play.exe, record.exe) that hold
// Stahnke's Z80 programs. They are the owner's own copies, installed once
// through the API or by copying them into Dir; sepiano never ships them.
type Programs struct {
	Dir string
	// Fallback, if set, supplies a program when none is installed (the
	// simulator's placeholders).
	Fallback map[string]*host.Program

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	mod  time.Time
	size int64
	prog *host.Program
	err  error
}

// ProgramInfo describes one installed program.
type ProgramInfo struct {
	Present bool   `json:"present"`
	Banner  string `json:"banner,omitempty"`
	Error   string `json:"error,omitempty"`
}

// ProgramFiles maps API names to file names.
var ProgramFiles = map[string]string{"play": "play.exe", "record": "record.exe", "getlut": "getlut.exe"}

// Load returns the named program ("play", "record" or "getlut"), re-reading the file
// when it changes.
func (p *Programs) Load(name string) (*host.Program, error) {
	file, ok := ProgramFiles[name]
	if !ok {
		return nil, fmt.Errorf("unknown program %q", name)
	}
	path := filepath.Join(p.Dir, file)
	fi, err := os.Stat(path)
	if err != nil {
		if fb := p.Fallback[name]; fb != nil {
			return fb, nil
		}
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%s is not installed (add it on the Status page)", file)
		}
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.cache[name]; ok && c.mod.Equal(fi.ModTime()) && c.size == fi.Size() {
		return c.prog, c.err
	}
	prog, err := host.LoadProgram(path)
	if err == nil {
		// A file copied into Dir by hand is checked as an upload is.
		if err = CheckIdentity(name, prog); err != nil {
			prog, err = nil, fmt.Errorf("%s: %w", file, err)
		}
	}
	if p.cache == nil {
		p.cache = map[string]cached{}
	}
	p.cache[name] = cached{fi.ModTime(), fi.Size(), prog, err}
	return prog, err
}

// Info reports whether a program is installed and usable.
func (p *Programs) Info(name string) ProgramInfo {
	if _, err := os.Stat(filepath.Join(p.Dir, ProgramFiles[name])); err != nil && p.Fallback[name] == nil {
		return ProgramInfo{}
	}
	prog, err := p.Load(name)
	if err != nil {
		return ProgramInfo{Present: true, Error: err.Error()}
	}
	return ProgramInfo{Present: true, Banner: prog.Banner}
}

// programBanners is a piece of each program's banner, which tells the
// programs apart. The six table installers share one banner: putlut has
// no table of its own, and each factory one carries its model's name
// (factoryModels).
var programBanners = map[string]string{
	"play": "PERFORMANCE PLAYER", "record": "PERFORMANCE RECORDER", "getlut": "LOOKUP TABLE RETRIEVER",
	"playmidi": "MIDI FILE PLAYER", "playeseq": "PIANOSOFT DISK PLAYER", "midiin": "MIDI INPUT CONTROLLER",
	"edit": "INTERACTIVE PERFORMANCE EDITOR", "dump": "PERFORMANCE DUMPER", "load": "PERFORMANCE LOADER",
	"tape": "MAGNETIC TAPE", "adjust": "MANUAL ADJUSTMENT", "anykey": "ANY KEY TEST", "anyham": "ANY HAMMER TEST",
	"sustain": "SUSTAIN PROGRAM", "monitor": "Z80 MONITOR", "setup": "LOOKUP TABLE SETUP",
	"cal": "ADAPTIVE CALIBRATOR", "silent": "SILENT NOTE CALIBRATOR", "putlut": "LOOKUP TABLE INSTALLER",
	"putb": "LOOKUP TABLE INSTALLER", "put225": "LOOKUP TABLE INSTALLER", "put275": "LOOKUP TABLE INSTALLER",
	"put290": "LOOKUP TABLE INSTALLER", "putc7": "LOOKUP TABLE INSTALLER",
}

// factoryModels is the model name inside each factory-table installer.
var factoryModels = map[string]string{
	"putb": "Steinway B-SE", "put225": "Bosendorfer 225SE", "put275": "Bosendorfer 275SE",
	"put290": "Bosendorfer 290SE", "putc7": "Yamaha C-7SE",
}

// CheckIdentity says whether prog is the program that belongs under name.
// A program in the wrong place could do something its screen does not say
// (a factory table under "Key sensor test", or under getlut, erasing the
// calibration at every backup), so the service refuses one.
func CheckIdentity(name string, prog *host.Program) error {
	want, ok := programBanners[name]
	if !ok {
		return nil
	}
	file := ProgramFiles[name]
	if !strings.Contains(prog.Banner, want) {
		return fmt.Errorf("this is %q, not %s", prog.Banner, file)
	}
	if !strings.Contains(want, "INSTALLER") {
		return nil
	}
	for other, model := range factoryModels {
		has := bytes.Contains(prog.Image, []byte(model))
		switch {
		case other == name && !has:
			return fmt.Errorf("this is not %s: it does not hold the %s table", file, model)
		case other != name && has:
			return fmt.Errorf("this is not %s: it installs the factory table for a %s (%s)", file, model, ProgramFiles[other])
		}
	}
	return nil
}

// Install checks that data holds the named Z80 program and stores it.
func (p *Programs) Install(name string, data []byte) (ProgramInfo, error) {
	file, ok := ProgramFiles[name]
	if !ok {
		return ProgramInfo{}, fmt.Errorf("unknown program %q", name)
	}
	prog, err := host.ParseProgram(data)
	if err != nil {
		if prog, err = host.ExtractProgram(data); err != nil {
			return ProgramInfo{}, errors.New("this file does not contain an SE program")
		}
	}
	if err := CheckIdentity(name, prog); err != nil {
		return ProgramInfo{}, err
	}
	if err := os.MkdirAll(p.Dir, 0o755); err != nil {
		return ProgramInfo{}, err
	}
	tmp := filepath.Join(p.Dir, "."+file+".tmp")
	f, err := os.Create(tmp)
	if err != nil {
		return ProgramInfo{}, err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, filepath.Join(p.Dir, file))
	}
	if err != nil {
		os.Remove(tmp)
		return ProgramInfo{}, err
	}
	return ProgramInfo{Present: true, Banner: prog.Banner}, nil
}
