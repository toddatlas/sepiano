package deck

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"sepiano/host"
	"sepiano/se"
	"sepiano/sim"
)

// fixture runs a deck against a fast simulated instrument.
type fixture struct {
	t      *testing.T
	inst   *sim.Instrument
	d      *Deck
	dir    string
	mu     sync.Mutex
	saved  []string // pieces imported by OnRecorded
	levels []int
	events []Event
}

func newFixture(t *testing.T, tweak func(*sim.Instrument)) *fixture {
	inst := sim.New()
	inst.Baud = 1_000_000 // ~100 KB/s: uploads take a few ms
	inst.Speed = 10
	if tweak != nil {
		tweak(inst)
	}
	f := &fixture{t: t, inst: inst, dir: t.TempDir()}
	f.d = New(Config{
		Connect: func(ctx context.Context, o host.ConnectOptions) (*host.Conn, error) {
			p, err := inst.Dial()
			if err != nil {
				return nil, err
			}
			return host.ConnectOn(ctx, p, inst.Baud, o)
		},
		Port:       "sim",
		Simulated:  true,
		Player:     func() (*host.Program, error) { return sim.PlayerProgram(), nil },
		Recorder:   func() (*host.Program, error) { return sim.RecorderProgram(), nil },
		StagingDir: t.TempDir(),
		OnRecorded: func(dir, name, title string) (string, error) {
			if _, err := se.Load(filepath.Join(dir, name)); err != nil {
				return "", err
			}
			f.mu.Lock()
			f.saved = append(f.saved, title)
			f.mu.Unlock()
			return "newpiece", nil
		},
		OnLevel: func(l int) {
			f.mu.Lock()
			f.levels = append(f.levels, l)
			f.mu.Unlock()
		},
		ConfirmTimeout: 2 * time.Second,
		RetryMin:       100 * time.Millisecond,
		RetryMax:       300 * time.Millisecond,
		Log:            t.Logf,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	ev, unsub := f.d.Subscribe()
	go func() {
		for e := range ev {
			f.mu.Lock()
			f.events = append(f.events, e)
			f.mu.Unlock()
		}
	}()
	go func() { f.d.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done; unsub() })
	f.wait(Idle, 5*time.Second)
	return f
}

// wait polls until the deck reaches state s.
func (f *fixture) wait(s State, timeout time.Duration) Status {
	f.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		st := f.d.Status()
		if st.State == s {
			return st
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("waited %v for %s; state is %s (%q)", timeout, s, st.State, st.Message)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (f *fixture) ctx() context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	f.t.Cleanup(cancel)
	return ctx
}

// piece writes a performance of the given length (in piano seconds) and
// returns a job for it.
func (f *fixture) piece(seconds int) Job {
	f.t.Helper()
	p := sim.Phrase(uint32(seconds * se.TicksPerSecond))
	base := filepath.Join(f.dir, "TESTPIEC")
	if err := p.Save(base); err != nil {
		f.t.Fatal(err)
	}
	return Job{Piece: PieceRef{ID: "testpiec", Title: "Test"}, Dir: f.dir, Name: "TESTPIEC", Duration: p.Duration()}
}

func (f *fixture) sawNotice(sub string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.events {
		if n, ok := e.Data.(Notice); ok && e.Type == "notice" && strings.Contains(n.Text, sub) {
			return true
		}
	}
	return false
}

func TestPlayPauseResumeLevelStop(t *testing.T) {
	f := newFixture(t, nil)
	if err := f.d.Play(f.ctx(), f.piece(120)); err != nil {
		t.Fatal(err)
	}
	st := f.wait(Playing, 5*time.Second)
	if st.Piece == nil || st.Piece.ID != "testpiec" || st.DurationMs != 120000 || !strings.Contains(st.Program, "PLAYER") {
		t.Fatalf("status %+v", st)
	}
	time.Sleep(500 * time.Millisecond) // 5 s of piano time at Speed 10
	if p := f.d.Status().PositionMs; p < 2000 || p > 9000 {
		t.Fatalf("position %d ms after 5 piano seconds", p)
	}

	if err := f.d.Pause(f.ctx()); err != nil {
		t.Fatal(err)
	}
	if st := f.d.Status(); st.State != Paused {
		t.Fatalf("after pause: %s", st.State)
	}
	time.Sleep(300 * time.Millisecond) // let the position catch up (sampled every 200 ms)
	held := f.d.Status().PositionMs
	time.Sleep(400 * time.Millisecond)
	if p := f.d.Status().PositionMs; p-held > 300 {
		t.Fatalf("position moved while paused: %d -> %d", held, p)
	}
	if err := f.d.Resume(f.ctx()); err != nil {
		t.Fatal(err)
	}
	if st := f.d.Status(); st.State != Playing {
		t.Fatalf("after resume: %s", st.State)
	}

	if err := f.d.ChangeLevel(f.ctx(), -1); err != nil {
		t.Fatal(err)
	}
	if err := f.d.SetLevel(f.ctx(), -3); err != nil {
		t.Fatal(err)
	}
	if st := f.d.Status(); st.Level != -3 || st.TargetLevel != -3 {
		t.Fatalf("level %d target %d", st.Level, st.TargetLevel)
	}
	if err := f.d.ChangeLevel(f.ctx(), +1); err != nil {
		t.Fatal(err)
	}
	if st := f.d.Status(); st.Level != -2 {
		t.Fatalf("level %d after +1", st.Level)
	}

	if err := f.d.Restart(f.ctx()); err != nil {
		t.Fatal(err)
	}
	if err := f.d.Stop(f.ctx()); err != nil {
		t.Fatal(err)
	}
	st = f.wait(Idle, 5*time.Second)
	if st.Piece != nil || st.Level != -2 || st.TargetLevel != -2 {
		t.Fatalf("idle status %+v", st)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.levels) == 0 || f.levels[len(f.levels)-1] != -2 {
		t.Fatalf("persisted levels %v", f.levels)
	}
}

func TestLevelAppliedAtStartAndFloor(t *testing.T) {
	f := newFixture(t, func(i *sim.Instrument) { i.MinLevel = -3 })
	if err := f.d.SetLevel(f.ctx(), -6); err != nil { // idle: remembered
		t.Fatal(err)
	}
	if st := f.d.Status(); st.TargetLevel != -6 || st.Level != -6 {
		t.Fatalf("idle level %+v", st)
	}
	f.d.Play(f.ctx(), f.piece(60))
	f.wait(Playing, 5*time.Second)
	deadline := time.Now().Add(5 * time.Second)
	for {
		st := f.d.Status()
		if st.MinLevel != nil {
			if *st.MinLevel != -3 || st.Level != -3 || st.TargetLevel != -3 {
				t.Fatalf("floor: %+v", st)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("level never reached the floor: %+v", st)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := f.d.ChangeLevel(f.ctx(), -1); err != nil { // below the floor: clamped
		t.Fatal(err)
	}
	if st := f.d.Status(); st.TargetLevel != -3 {
		t.Fatalf("target %d below the floor", st.TargetLevel)
	}
	f.d.Stop(f.ctx())
	f.wait(Idle, 5*time.Second)
}

func TestEndOfPerformance(t *testing.T) {
	f := newFixture(t, nil)
	f.d.Play(f.ctx(), f.piece(3))
	f.wait(Playing, 5*time.Second)
	st := f.wait(Idle, 5*time.Second)
	if st.Piece != nil || st.Message != "" {
		t.Fatalf("after the end: %+v", st)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var sawEnd, sawFull bool
	for _, e := range f.events {
		if e.Type == "console" && e.Data.(map[string]string)["line"] == "END OF PERFORMANCE" {
			sawEnd = true
		}
		if s, ok := e.Data.(Status); ok && s.State == Stopping && s.PositionMs == s.DurationMs {
			sawFull = true
		}
	}
	if !sawEnd || !sawFull {
		t.Fatalf("end seen %v, full progress seen %v", sawEnd, sawFull)
	}
}

func TestPlayWhilePlayingSwitchesPiece(t *testing.T) {
	f := newFixture(t, nil)
	f.d.Play(f.ctx(), f.piece(60))
	f.wait(Playing, 5*time.Second)
	j := f.piece(60)
	j.Piece = PieceRef{ID: "second", Title: "Second"}
	if err := f.d.Play(f.ctx(), j); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		st := f.d.Status()
		if st.State == Playing && st.Piece != nil && st.Piece.ID == "second" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("second piece never played: %+v", st)
		}
		time.Sleep(5 * time.Millisecond)
	}
	f.d.Stop(f.ctx())
	f.wait(Idle, 5*time.Second)
}

func TestRecording(t *testing.T) {
	f := newFixture(t, nil)
	if err := f.d.Arm(f.ctx(), "Sunday practice"); err != nil {
		t.Fatal(err)
	}
	st := f.wait(Armed, 5*time.Second)
	if st.RecordingTitle != "Sunday practice" || !strings.Contains(st.Program, "RECORDER") {
		t.Fatalf("armed: %+v", st)
	}
	if err := f.d.StartRecording(f.ctx()); err != nil {
		t.Fatal(err)
	}
	if st := f.d.Status(); st.State != Recording {
		t.Fatalf("after start: %s", st.State)
	}
	time.Sleep(600 * time.Millisecond) // 6 piano seconds
	if err := f.d.StopRecording(f.ctx()); err != nil {
		t.Fatal(err)
	}
	f.wait(Idle, 5*time.Second)
	f.mu.Lock()
	saved := append([]string(nil), f.saved...)
	f.mu.Unlock()
	if len(saved) != 1 || saved[0] != "Sunday practice" {
		t.Fatalf("saved %v", saved)
	}
	if !f.sawNotice("Saved the recording") {
		t.Fatal("no notice for the saved recording")
	}
}

func TestDiscardRecording(t *testing.T) {
	f := newFixture(t, nil)
	f.d.Arm(f.ctx(), "")
	f.wait(Armed, 5*time.Second)
	f.d.StartRecording(f.ctx())
	if err := f.d.Discard(f.ctx()); err != nil {
		t.Fatal(err)
	}
	f.wait(Idle, 5*time.Second)
	f.mu.Lock()
	n := len(f.saved)
	f.mu.Unlock()
	if n != 0 || !f.sawNotice("discarded") {
		t.Fatalf("saved %d, discard notice %v", n, f.sawNotice("discarded"))
	}
}

func TestInterlockFailure(t *testing.T) {
	f := newFixture(t, func(i *sim.Instrument) { i.InterlockFailure = true })
	f.d.Arm(f.ctx(), "x")
	f.wait(Armed, 5*time.Second)
	f.d.StartRecording(f.ctx())
	f.wait(Idle, 5*time.Second)
	if !f.sawNotice("INTERLOCK") {
		t.Fatal("no interlock notice")
	}
}

func TestPowerCycle(t *testing.T) {
	f := newFixture(t, nil)
	f.inst.SetPower(false)
	st := f.wait(Offline, 5*time.Second)
	if st.Online {
		t.Fatal("online while the piano is off")
	}
	if err := f.d.Arm(f.ctx(), ""); !errors.Is(err, ErrOffline) {
		t.Fatalf("arm while off: %v", err)
	}
	f.inst.SetPower(true)
	if st := f.wait(Idle, 5*time.Second); !st.Online {
		t.Fatal("idle but not online")
	}
}

func TestPowerCutWhilePlaying(t *testing.T) {
	f := newFixture(t, nil)
	f.d.Play(f.ctx(), f.piece(60))
	f.wait(Playing, 5*time.Second)
	f.inst.SetPower(false)
	f.wait(Fault, 5*time.Second)
	f.inst.SetPower(true)
	f.wait(Idle, 5*time.Second)
}

func TestCommandsInWrongState(t *testing.T) {
	f := newFixture(t, nil)
	for name, fn := range map[string]func(context.Context) error{
		"pause": f.d.Pause, "resume": f.d.Resume, "restart": f.d.Restart,
		"recstart": f.d.StartRecording, "recstop": f.d.StopRecording, "discard": f.d.Discard,
	} {
		if err := fn(f.ctx()); !errors.Is(err, ErrState) {
			t.Errorf("%s while idle: %v", name, err)
		}
	}
	if err := f.d.Stop(f.ctx()); err != nil {
		t.Errorf("stop while idle: %v", err)
	}
}

func TestParseLine(t *testing.T) {
	for line, want := range map[string]consoleEvent{
		"START PLAYING":                            evStart,
		"STOP PLAYING AT TIME=1:02.5":              evPause,
		"RESUME PLAYING":                           evResume,
		"QUIT PLAYING":                             evQuit,
		"RESTART PERFORMANCE":                      evRestart,
		"END OF PERFORMANCE":                       evEnd,
		"DEPRESS SPACE BAR TO STOP/RESUME PLAYING": evNone,
		"DEPRESS ENTER TO QUIT":                    evNone,
		"DEPRESS SPACE BAR TO START RECORDING":     evArmed,
		"DEPRESS ENTER TO END RECORDING":           evRecording,
		"KEYBOARD INTERLOCK FAILURE":               evInterlock,
	} {
		if got, _ := parseLine(line); got != want {
			t.Errorf("%q: %v, want %v", line, got, want)
		}
	}
	for line, want := range map[string]int{"LEVEL=0 (NORMAL)": 0, "LEVEL=-3": -3, "LEVEL= -12": -12} {
		if ev, n := parseLine(line); ev != evLevel || n != want {
			t.Errorf("%q: %v %d", line, ev, n)
		}
	}
}

func TestLineWriter(t *testing.T) {
	var got []string
	w := &lineWriter{emit: func(s string) { got = append(got, s) }}
	w.Write([]byte("\r\nSTART PLA"))
	w.Write([]byte("YING\r\n\r\nEND OF PERFORMANCE\r\n"))
	if strings.Join(got, "|") != "START PLAYING|END OF PERFORMANCE" {
		t.Fatalf("%q", got)
	}
}

// A joined performance reports which of its tracks is playing.
func TestTracksFollowThePosition(t *testing.T) {
	f := newFixture(t, nil)
	j := f.piece(40)
	j.Piece = PieceRef{ID: "first", Title: "Aria"}
	j.Collection = &Collection{Kind: "album", ID: "Goldberg", Title: "Goldberg", Start: 0, Count: 2}
	j.Tracks = []TrackRef{
		{Piece: PieceRef{ID: "first", Title: "Aria"}, StartMs: 0, DurationMs: 10000},
		{Piece: PieceRef{ID: "second", Title: "Variation 1"}, StartMs: 10000, DurationMs: 30000},
	}
	if err := f.d.Play(f.ctx(), j); err != nil {
		t.Fatal(err)
	}
	st := f.wait(Playing, 5*time.Second)
	if st.TrackIndex != 0 || st.Piece == nil || st.Piece.ID != "first" || st.Collection == nil || len(st.Tracks) != 2 {
		t.Fatalf("at the start: %+v", st)
	}
	deadline := time.Now().Add(5 * time.Second) // 10 piano seconds take 1 s at Speed 10
	for {
		st = f.d.Status()
		if st.TrackIndex == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("never reached the second track: %+v", st)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st.Piece == nil || st.Piece.ID != "second" || st.PositionMs < 10000 {
		t.Fatalf("second track: %+v", st)
	}
	f.d.Stop(f.ctx())
	if st := f.wait(Idle, 5*time.Second); st.Collection != nil || len(st.Tracks) != 0 {
		t.Fatalf("after stop: %+v", st)
	}
}
