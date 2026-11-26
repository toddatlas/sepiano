package host

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// File service request codes sent by the Z80 program on channel 2. Every
// request is answered by echoing its code, followed by any result.
const (
	ReqSetName    = 0x0B // name bytes ... 0x7F            -> 0x0B (echo sent before the name is read)
	ReqSetExt     = 0x0C // ext bytes ... 0x7F             -> 0x0C
	ReqGetName    = 0x0D //                                -> 0x0D name 0x7F (8 chars max)
	ReqGetExt     = 0x0E //                                -> 0x0E ext 0x7F (3 chars max)
	ReqGetArg     = 0x0F //                                -> 0x0F second argument ('\' -> CR LF) 0x7F
	ReqOpenPerf   = 0x10 // open name.kb, .lp, .sp as files 0, 1, 2 -> 0x10
	ReqReadPerf   = 0x11 // file len_lo len_hi             -> 0x11 file n_lo n_hi data[n]
	ReqCreatePerf = 0x12 // create name.kb$, .lp$, .sp$    -> 0x12
	ReqWritePerf  = 0x13 // file len_lo len_hi data[len]   -> 0x13 file n_lo n_hi
	ReqClosePerf  = 0x14 // close output files             -> 0x14
	ReqCommitPerf = 0x15 // rename .kb$ -> .kb etc.        -> 0x15
	ReqAbortPerf  = 0x16 // delete .kb$, .lp$, .sp$        -> 0x16
	ReqOpenFile   = 0x38 // open name.ext for reading      -> 0x38
	ReqReadFile   = 0x39 // len_lo len_hi                  -> 0x39 n_lo n_hi data[n]
	ReqCreateFile = 0x3A // create name.$$$                -> 0x3A
	ReqWriteFile  = 0x3B // len_lo len_hi data[len]        -> 0x3B n_lo n_hi
	ReqCloseFile  = 0x3C // close name.$$$                 -> 0x3C
	ReqCommitFile = 0x3D // replace name.ext by name.$$$   -> 0x3D
	ReqAbortFile  = 0x3E // delete name.$$$                -> 0x3E
)

const maxXfer = 0x2000

var perfExt = [3]string{".kb", ".lp", ".sp"}

// FileService answers the Z80 program's file requests, the way the PC host
// does, relative to one "current" file name.
type FileService struct {
	Dir  string // directory of the current file
	Name string // base name without extension
	Ext  string // extension including the dot, or ""
	Arg  string // the optional second command-line argument (ReqGetArg)

	// Log, if set, receives one line per request.
	Log func(format string, args ...any)

	// OpenPerf, if set, opens a performance file for reading in place of
	// os.Open: i is 0, 1 or 2 for .kb, .lp, .sp. The deck uses it to send
	// the key stream with the volume applied as it goes (se.KB.Reader).
	OpenPerf func(i int, path string) (io.ReadCloser, error)

	in  [4]io.ReadCloser // 0-2 performance files, 3 generic file
	out [4]*os.File
}

// NewFileService sets the current file from a path such as "dir/song.kb".
func NewFileService(path string) *FileService {
	ext := filepath.Ext(path)
	return &FileService{Dir: filepath.Dir(path), Name: strings.TrimSuffix(filepath.Base(path), ext), Ext: ext}
}

func (s *FileService) path(ext string) string { return filepath.Join(s.Dir, s.Name+ext) }

func (s *FileService) logf(format string, args ...any) {
	if s.Log != nil {
		s.Log(format, args...)
	}
}

// closeIn closes input file i, if open.
func (s *FileService) closeIn(i int) {
	if s.in[i] != nil {
		s.in[i].Close()
		s.in[i] = nil
	}
}

// Close releases any open files.
func (s *FileService) Close() {
	for i := range s.in {
		if s.in[i] != nil {
			s.in[i].Close()
			s.in[i] = nil
		}
		if s.out[i] != nil {
			s.out[i].Close()
			s.out[i] = nil
		}
	}
}

// Fatal is returned for requests the original host treats as fatal.
type Fatal struct{ Msg string }

func (f Fatal) Error() string { return f.Msg }

func fatalf(format string, args ...any) error { return Fatal{fmt.Sprintf(format, args...)} }

// Serve handles requests from rw until an error occurs.
func (s *FileService) Serve(rw io.ReadWriter) error {
	defer s.Close()
	for {
		if err := s.handle(rw); err != nil {
			return err
		}
	}
}

func (s *FileService) handle(rw io.ReadWriter) error {
	var one [1]byte
	get := func() (byte, error) {
		_, err := io.ReadFull(rw, one[:])
		return one[0], err
	}
	get16 := func() (int, error) {
		lo, err := get()
		if err != nil {
			return 0, err
		}
		hi, err := get()
		return int(lo) | int(hi)<<8, err
	}
	put := func(b ...byte) error { _, err := rw.Write(b); return err }
	str := func() (string, error) {
		var b []byte
		for {
			c, err := get()
			if err != nil || c == 0x7F {
				return string(b), err
			}
			if len(b) >= 255 {
				return "", fatalf("Excessive name length")
			}
			b = append(b, c)
		}
	}

	req, err := get()
	if err != nil {
		return err
	}
	switch req {
	case ReqSetName:
		if err := put(req); err != nil {
			return err
		}
		n, err := str()
		if err != nil {
			return err
		}
		s.Name = n
		s.logf("set name %q", n)
		return nil
	case ReqSetExt:
		if err := put(req); err != nil {
			return err
		}
		e, err := str()
		if err != nil {
			return err
		}
		s.Ext = "." + e
		s.logf("set extension %q", s.Ext)
		return nil
	case ReqGetName:
		if len(s.Name) > 8 {
			return fatalf("Filename '%s' is too long for the SE instrument. The maximum supported length is 8 characters.", s.Name)
		}
		return put(append(append([]byte{req}, s.Name...), 0x7F)...)
	case ReqGetExt:
		e := strings.TrimPrefix(s.Ext, ".")
		if len(e) > 3 {
			return fatalf("File extension '%s' is too long for the SE instrument. The maximum supported length is 3 characters.", s.Ext)
		}
		return put(append(append([]byte{req}, e...), 0x7F)...)
	case ReqGetArg:
		b := []byte{req}
		b = append(b, strings.ReplaceAll(s.Arg, `\`, "\r\n")...)
		return put(append(b, 0x7F)...)
	case ReqOpenPerf:
		for i := range s.in {
			if s.in[i] != nil {
				s.in[i].Close()
				s.in[i] = nil
			}
		}
		for i, ext := range perfExt {
			open := func(i int, path string) (io.ReadCloser, error) { return os.Open(path) }
			if s.OpenPerf != nil {
				open = s.OpenPerf
			}
			f, err := open(i, s.path(ext))
			if err != nil {
				return fatalf("Missing file: %s", s.path(ext))
			}
			s.in[i] = f
		}
		s.logf("open %s.{kb,lp,sp}", s.Name)
		return put(req)
	case ReqReadPerf, ReqReadFile:
		fn := 3
		if req == ReqReadPerf {
			b, err := get()
			if err != nil {
				return err
			}
			fn = int(b)
		}
		n, err := get16()
		if err != nil {
			return err
		}
		if fn > 3 || s.in[fn] == nil {
			return fatalf("Illegal file number: %d", fn)
		}
		if n > maxXfer {
			return fatalf("Excessive byte count: %d", n)
		}
		buf := make([]byte, n)
		k, err := io.ReadFull(s.in[fn], buf)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return fatalf("Read error")
		}
		s.logf("read file %d: %d of %d bytes", fn, k, n)
		hdr := []byte{req}
		if req == ReqReadPerf {
			hdr = append(hdr, byte(fn))
		}
		return put(append(append(hdr, byte(k), byte(k>>8)), buf[:k]...)...)
	case ReqCreatePerf:
		for i, ext := range perfExt {
			f, err := os.Create(s.path(ext + "$"))
			if err != nil {
				return fatalf("File cannot be created: %s", s.path(ext+"$"))
			}
			s.out[i] = f
		}
		s.logf("create %s.{kb,lp,sp}$", s.Name)
		return put(req)
	case ReqWritePerf, ReqWriteFile:
		fn := 3
		if req == ReqWritePerf {
			b, err := get()
			if err != nil {
				return err
			}
			fn = int(b)
		}
		n, err := get16()
		if err != nil {
			return err
		}
		if n > maxXfer {
			return fatalf("Excessive byte count: %d", n)
		}
		buf := make([]byte, n)
		if _, err := io.ReadFull(rw, buf); err != nil {
			return err
		}
		if fn > 3 || s.out[fn] == nil {
			return fatalf("Illegal file number: %d", fn)
		}
		if _, err := s.out[fn].Write(buf); err != nil {
			return fatalf("Write error")
		}
		hdr := []byte{req}
		if req == ReqWritePerf {
			hdr = append(hdr, byte(fn))
		}
		return put(append(hdr, byte(n), byte(n>>8))...)
	case ReqClosePerf:
		for i := range s.out {
			if s.out[i] != nil {
				s.out[i].Close()
				s.out[i] = nil
			}
		}
		return put(req)
	case ReqCommitPerf:
		for i, ext := range perfExt {
			s.closeIn(i)
			if s.out[i] != nil {
				s.out[i].Close()
				s.out[i] = nil
			}
			if err := os.Rename(s.path(ext+"$"), s.path(ext)); err != nil {
				return fatalf("File cannot be renamed: %s", s.path(ext+"$"))
			}
		}
		s.logf("saved %s.{kb,lp,sp}", s.Name)
		return put(req)
	case ReqAbortPerf:
		for _, ext := range perfExt {
			os.Remove(s.path(ext + "$"))
		}
		return put(req)
	case ReqOpenFile:
		if s.in[3] != nil {
			s.in[3].Close()
			s.in[3] = nil
		}
		f, err := os.Open(s.path(s.Ext))
		if err != nil {
			return fatalf("Missing file: %s", s.path(s.Ext))
		}
		s.in[3] = f
		s.logf("open %s%s", s.Name, s.Ext)
		return put(req)
	case ReqCreateFile:
		f, err := os.Create(s.path(".$$$"))
		if err != nil {
			return fatalf("File cannot be created: %s", s.path(".$$$"))
		}
		s.out[3] = f
		return put(req)
	case ReqCloseFile:
		if s.out[3] != nil {
			s.out[3].Close()
			s.out[3] = nil
		}
		return put(req)
	case ReqCommitFile:
		s.closeIn(3)
		if s.out[3] != nil {
			s.out[3].Close()
			s.out[3] = nil
		}
		os.Remove(s.path(s.Ext))
		if err := os.Rename(s.path(".$$$"), s.path(s.Ext)); err != nil {
			return fatalf("File cannot be renamed: %s", s.path(".$$$"))
		}
		return put(req)
	case ReqAbortFile:
		os.Remove(s.path(".$$$"))
		return put(req)
	}
	return fatalf("Illegal file request: 0x%02X", req)
}
