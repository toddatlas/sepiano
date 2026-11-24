package api

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

// Install checks that data holds a Z80 program and stores it.
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
