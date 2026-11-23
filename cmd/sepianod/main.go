// Command sepianod is the piano service: it owns the serial link to the SE
// instrument and serves the API and web UI (see docs/SERVICE-PLAN.md).
//
//	sepianod [-addr :80] [-data /var/lib/sepiano] [-port /dev/sepiano-piano]
//	sepianod -simulate -addr :8080 -data ./devdata     # no piano needed
//	sepianod import [-server http://127.0.0.1] DIR...  # add .kb/.lp/.sp sets
//	sepianod tag [-server http://127.0.0.1] [-n] DIR... # titles and albums from the album .bat files
//	sepianod version
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"sepiano/api"
	"sepiano/deck"
	"sepiano/host"
	"sepiano/library"
	"sepiano/sim"
	"sepiano/web"
)

var version = "dev"

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "import":
			return importCmd(args[1:])
		case "tag":
			return tagCmd(args[1:])
		case "version", "-version", "--version":
			fmt.Println("sepianod", version)
			return 0
		}
	}
	fl := flag.NewFlagSet("sepianod", flag.ExitOnError)
	addr := fl.String("addr", ":80", "HTTP listen address")
	data := fl.String("data", "/var/lib/sepiano", "data folder (library, programs, settings)")
	port := fl.String("port", "/dev/sepiano-piano", "serial port wired to the piano")
	baud := fl.Int("baud", 57600, "baud rate (0 tries 57600 then 28800, like the original)")
	simulate := fl.Bool("simulate", false, "use the built-in simulated piano instead of the serial port")
	simSpeed := fl.Float64("sim-speed", 1, "simulated piano clock rate (1 = real time)")
	demo := fl.Bool("demo", true, "with -simulate: add demo pieces when the library is empty")
	verbose := fl.Bool("v", false, "log simulator details")
	fl.Parse(args)

	flags := log.LstdFlags
	if os.Getenv("JOURNAL_STREAM") != "" { // journald adds its own timestamps
		flags = 0
	}
	logger := log.New(os.Stderr, "", flags)
	logf := logger.Printf

	lib, err := library.Open(*data)
	if err != nil {
		logger.Print(err)
		return 1
	}
	staging := filepath.Join(*data, "staging")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		logger.Print(err)
		return 1
	}
	progs := &api.Programs{Dir: filepath.Join(*data, "programs")}

	var connect func(context.Context, host.ConnectOptions) (*host.Conn, error)
	var inst *sim.Instrument
	portName := *port
	if *simulate {
		inst = sim.New()
		inst.Speed = *simSpeed
		if *verbose {
			inst.Log = logf
		}
		progs.Fallback = map[string]*host.Program{"play": sim.PlayerProgram(), "record": sim.RecorderProgram()}
		connect = func(ctx context.Context, o host.ConnectOptions) (*host.Conn, error) {
			p, err := inst.Dial()
			if err != nil {
				return nil, err
			}
			return host.ConnectOn(ctx, p, inst.Baud, o)
		}
		portName = "simulator"
		if *demo && len(lib.List(library.Filter{})) == 0 {
			for name, perf := range sim.Demo() {
				kb, lp, sp, err := perf.Encode()
				if err == nil {
					_, _, err = lib.AddSet(name, kb, lp, sp, library.OriginImport, "")
				}
				if err != nil {
					logf("demo piece %s: %v", name, err)
				}
			}
		}
	} else {
		connect = func(ctx context.Context, o host.ConnectOptions) (*host.Conn, error) {
			o.Baud = *baud
			return host.Connect(ctx, *port, o)
		}
	}

	d := deck.New(deck.Config{
		Connect:    connect,
		Port:       portName,
		Simulated:  *simulate,
		Player:     func() (*host.Program, error) { return progs.Load("play") },
		Recorder:   func() (*host.Program, error) { return progs.Load("record") },
		StagingDir: staging,
		OnRecorded: func(dir, name, title string) (string, error) {
			p, err := lib.AddRecording(dir, name, title)
			return p.ID, err
		},
		Level: lib.Settings().Level,
		OnLevel: func(level int) {
			s := lib.Settings()
			s.Level = level
			if err := lib.SaveSettings(s); err != nil {
				logf("saving the level: %v", err)
			}
		},
		Log: logf,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	deckDone := make(chan struct{})
	go func() { d.Run(ctx); close(deckDone) }()

	s := &api.Server{Deck: d, Lib: lib, Programs: progs, Sim: inst, UI: web.Files, Version: version, Log: logf,
		PlayDir: filepath.Join(*data, "play")}
	srv := &http.Server{Addr: *addr, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	srvErr := make(chan error, 1)
	go func() { srvErr <- srv.ListenAndServe() }()
	mode := "piano on " + *port
	if *simulate {
		mode = "simulated piano"
	}
	logf("sepianod %s: %s, http %s, data %s, %d pieces", version, mode, *addr, *data, len(lib.List(library.Filter{})))

	select {
	case <-ctx.Done():
	case err := <-srvErr:
		logger.Print(err)
		stop()
		<-deckDone
		return 1
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(shutdown)
	<-deckDone
	logf("stopped")
	return 0
}

// importCmd adds the performance sets found in folders, either through a
// running sepianod (the default, so the service stays the only writer) or
// straight into a data folder while the service is stopped.
func importCmd(args []string) int {
	fl := flag.NewFlagSet("sepianod import", flag.ExitOnError)
	server := fl.String("server", "http://127.0.0.1", "running sepianod to upload to")
	data := fl.String("data", "", "write into this data folder instead (stop the service first)")
	fl.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: sepianod import [-server URL | -data DIR] FOLDER...")
		fl.PrintDefaults()
	}
	fl.Parse(args)
	if fl.NArg() == 0 {
		fl.Usage()
		return 2
	}
	if *data != "" {
		lib, err := library.Open(*data)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		failed := false
		for _, dir := range fl.Args() {
			r, err := lib.ImportDir(dir)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			failed = report(r) || failed
		}
		if failed {
			return 1
		}
		return 0
	}

	sets, err := findSets(fl.Args())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if len(sets) == 0 {
		fmt.Fprintln(os.Stderr, "no .kb/.lp/.sp files found")
		return 1
	}
	failed := false
	for _, files := range sets {
		r, err := uploadSet(*server, files)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", filepath.Base(files[0]), err)
			failed = true
			continue
		}
		failed = report(r) || failed
	}
	if failed {
		return 1
	}
	return 0
}

// findSets groups performance files by folder and base name.
func findSets(dirs []string) ([][]string, error) {
	groups := map[string][]string{}
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			switch strings.ToLower(filepath.Ext(p)) {
			case ".kb", ".lp", ".sp":
				key := strings.TrimSuffix(p, filepath.Ext(p))
				groups[key] = append(groups[key], p)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([][]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, groups[k])
	}
	return out, nil
}

func uploadSet(server string, files []string) (library.Result, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return library.Result{}, err
		}
		w, err := mw.CreateFormFile("file", filepath.Base(f))
		if err != nil {
			return library.Result{}, err
		}
		w.Write(b)
	}
	mw.Close()
	resp, err := http.Post(strings.TrimRight(server, "/")+"/api/v1/pieces", mw.FormDataContentType(), &buf)
	if err != nil {
		return library.Result{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		var e struct{ Error string }
		json.Unmarshal(body, &e)
		return library.Result{}, errors.New(resp.Status + ": " + e.Error)
	}
	var r library.Result
	return r, json.Unmarshal(body, &r)
}

// report prints a result and says whether anything failed.
func report(r library.Result) bool {
	for _, p := range r.Added {
		fmt.Printf("added      %s  %s\n", p.ID, p.SourceName)
	}
	for _, p := range r.Duplicates {
		fmt.Printf("duplicate  %s  %s\n", p.ID, p.SourceName)
	}
	for _, e := range r.Errors {
		fmt.Printf("error      %s: %s\n", e.Name, e.Error)
	}
	return len(r.Errors) > 0
}
