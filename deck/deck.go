// Package deck controls the instrument: one goroutine owns the serial link,
// runs Stahnke's player or recorder on it, turns user commands into the
// programs' own keys, and confirms each change by reading their console text.
//
// The deck keeps a link open while idle, so it knows at once whether the
// piano is on. Each piece or recording gets a fresh connection: the idle link
// is closed and a new one opened for the upload, which is the sequence proven
// on the piano. (Whether the firmware accepts a second upload on a link that
// stays up is Phase 0 question 3.)
package deck

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"sepiano/host"
	"sepiano/link"
	"sepiano/se"
)

// State is what the deck is doing.
type State string

// States, as documented in api/openapi.yaml.
const (
	Offline    State = "offline"
	Connecting State = "connecting"
	Idle       State = "idle"
	Loading    State = "loading"
	Playing    State = "playing"
	Paused     State = "paused"
	Stopping   State = "stopping"
	Armed      State = "armed"
	Recording  State = "recording"
	Saving     State = "saving"
	Working    State = "maintenance" // a utility program (getlut) is running
	Fault      State = "fault"
)

// Errors returned by commands. The API maps them to HTTP status codes.
var (
	ErrState     = errors.New("not possible in the current state")
	ErrBusy      = errors.New("the piano is still answering the previous command")
	ErrOffline   = errors.New("the piano is not answering (is it switched on?)")
	ErrNoProgram = errors.New("program not installed")
	ErrNoConfirm = errors.New("the piano did not confirm the command")
)

// PieceRef identifies what is loaded.
type PieceRef struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Composer  string `json:"composer,omitempty"`
	Performer string `json:"performer,omitempty"`
	// SourceName is the file the piece came from, shown to check what plays.
	SourceName string `json:"source_name,omitempty"`
}

// Status is the deck's state as clients see it.
type Status struct {
	State          State       `json:"state"`
	Online         bool        `json:"online"`
	Piece          *PieceRef   `json:"piece"` // the track playing now
	Collection     *Collection `json:"collection"`
	Tracks         []TrackRef  `json:"tracks"`
	TrackIndex     int         `json:"track_index"`
	PositionMs     int64       `json:"position_ms"`
	DurationMs     int64       `json:"duration_ms"`
	Level          int         `json:"level"`
	TargetLevel    int         `json:"target_level"`
	MinLevel       *int        `json:"min_level"`
	Message        string      `json:"message"`
	RecordingTitle string      `json:"recording_title,omitempty"`
	Program        string      `json:"program,omitempty"`
	Voiced         bool        `json:"voiced"`    // playing with software voicing
	Corrected      bool        `json:"corrected"` // a recording playing with the per-key corrections
	Simulated      bool        `json:"simulated"`
	Since          time.Time   `json:"since"`
}

// Job is a performance to play: the files Dir/Name.kb, .lp and .sp.
type Job struct {
	Piece    PieceRef
	Dir      string
	Name     string
	Duration time.Duration

	// Voiced says the files carry software voicing (shown in the status).
	Voiced bool
	// Corrected says the files carry the recording corrections.
	Corrected bool

	// For an album or play list joined into one performance: what it is,
	// and the tracks it is made of (Piece is then the first track).
	Collection *Collection
	Tracks     []TrackRef
}

// Collection is the album or play list a joined performance comes from.
type Collection struct {
	Kind  string `json:"kind"` // album, playlist or list
	ID    string `json:"id"`
	Title string `json:"title"`
	Start int    `json:"start"` // index in the collection of the first track played
	Count int    `json:"count"` // tracks in the whole collection
}

// TrackRef is one track of the performance playing, with its place in it.
type TrackRef struct {
	Piece      PieceRef `json:"piece"`
	StartMs    int64    `json:"start_ms"`
	DurationMs int64    `json:"duration_ms"`
}

// Event is something to tell clients: Type is "status", "console", "library"
// or "notice".
type Event struct {
	Type string
	Data any
}

// Notice is a short message for the user.
type Notice struct {
	Level string `json:"level"` // info, warn, error
	Text  string `json:"text"`
}

// LinkInfo describes the connection, for diagnostics.
type LinkInfo struct {
	Port           string     `json:"port"`
	Baud           int        `json:"baud"`
	Online         bool       `json:"online"`
	Simulated      bool       `json:"simulated"`
	RxBytes        int64      `json:"rx_bytes"`
	TxBytes        int64      `json:"tx_bytes"`
	Retransmits    int        `json:"retransmits"`
	Naks           int        `json:"naks"`
	ConnectedSince *time.Time `json:"connected_since"`
	LastError      string     `json:"last_error"`
	Console        []string   `json:"console"`
}

// Config wires the deck to the instrument and the rest of the service.
type Config struct {
	// Connect opens a link to the instrument (host.Connect on a serial port,
	// or a simulated cable). It must honour o.OnFrame, o.RawRx and o.RawTx.
	Connect   func(ctx context.Context, o host.ConnectOptions) (*host.Conn, error)
	Port      string // shown in diagnostics
	Simulated bool

	// Player and Recorder return the Z80 programs, loaded when needed so a
	// newly installed .exe takes effect without a restart.
	Player, Recorder func() (*host.Program, error)

	// StagingDir receives recordings before OnRecorded imports them.
	StagingDir string
	// OnRecorded imports dir/name.{kb,lp,sp} and returns the new piece id.
	OnRecorded func(dir, name, title string) (string, error)

	// Level is the remembered level; OnLevel is told when it changes.
	Level   int
	OnLevel func(level int)

	Log func(format string, args ...any)

	ConfirmTimeout time.Duration // wait for a console confirmation (default 3 s)
	StartTimeout   time.Duration // upload to "START PLAYING" or the recorder's prompt (default 30 s)
	RetryMin       time.Duration // reconnect delay after a failure (default 2 s)
	RetryMax       time.Duration // longest reconnect delay (default 10 s)
}

// Deck is the instrument controller. Create it with New and start it with Run.
type Deck struct {
	cfg   Config
	cmds  chan command
	inbox chan any

	mu      sync.Mutex
	snap    Status
	subs    map[chan Event]struct{}
	console []string
	lastErr string
	since   *time.Time
	cur     atomic.Pointer[host.Conn]
	rx, tx  atomic.Int64
	lastTS  atomic.Uint32 // the piano's latest clock reading; bit 16 marks it valid

	// Owned by the run goroutine.
	ctx        context.Context
	st         Status
	released   chan struct{} // closed once every connection handed to release is closed
	conn       *host.Conn    // the open link (idle or session)
	connecting bool
	sess       *session
	next       *Job
	pend       *pending
	retry      *time.Timer
	retryC     <-chan time.Time
	backoff    time.Duration
	stepping   bool
	stepDown   bool
	stepUntil  time.Time
	havePos    bool
	prevTS     uint16
	pos        int64 // ticks
}

type session struct {
	kind     string // "play", "record" or "utility"
	job      *Job
	title    string
	name     string // staging base name for a recording
	prog     *host.Program
	keys     chan []byte
	ctx      context.Context
	cancel   context.CancelFunc
	conn     *host.Conn
	started  bool // the program reached "START PLAYING" or the record prompt
	deadline time.Time
	aborted  bool
	discard  bool

	// A utility program: its command-line file, its console, when it must
	// have finished, and who waits for it.
	path  string
	lines []string
	limit time.Time
	done  chan UtilityResult
	// interactive: a tool the user drives from the app (keys, stop).
	interactive bool
}

type pending struct {
	what     string
	deadline time.Time
	reply    chan error
}

type command struct {
	op    string
	job   *Job
	util  *session
	data  []byte
	title string
	level *int
	delta int
	reply chan error
}

type connDone struct {
	conn *host.Conn
	err  error
	sess *session // nil for the idle link
}
type linkDown struct{ conn *host.Conn }
type sessDone struct {
	sess *session
	err  error
}
type conLine struct {
	sess *session
	line string
}

// New returns a deck; call Run to start it.
func New(cfg Config) *Deck {
	if cfg.ConfirmTimeout == 0 {
		cfg.ConfirmTimeout = 3 * time.Second
	}
	if cfg.StartTimeout == 0 {
		cfg.StartTimeout = 30 * time.Second
	}
	if cfg.RetryMin == 0 {
		cfg.RetryMin = 2 * time.Second
	}
	if cfg.RetryMax == 0 {
		cfg.RetryMax = 10 * time.Second
	}
	d := &Deck{cfg: cfg, cmds: make(chan command), inbox: make(chan any, 256), subs: map[chan Event]struct{}{},
		released: make(chan struct{})}
	close(d.released)
	d.st = Status{State: Connecting, Level: cfg.Level, TargetLevel: cfg.Level, Simulated: cfg.Simulated, Since: time.Now()}
	d.snap = d.st
	return d
}

func (d *Deck) logf(format string, args ...any) {
	if d.cfg.Log != nil {
		d.cfg.Log(format, args...)
	}
}

// ---- public API (any goroutine) ----

// Status returns the current state.
func (d *Deck) Status() Status {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.snap
}

// Subscribe returns a channel of events, starting with the current status,
// and a function that ends the subscription. Slow subscribers miss events.
func (d *Deck) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 64)
	d.mu.Lock()
	d.subs[ch] = struct{}{}
	ch <- Event{"status", d.snap}
	d.mu.Unlock()
	return ch, func() {
		d.mu.Lock()
		if _, ok := d.subs[ch]; ok {
			delete(d.subs, ch)
			close(ch)
		}
		d.mu.Unlock()
	}
}

// Publish sends an event to every subscriber.
func (d *Deck) Publish(e Event) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for ch := range d.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

// Info describes the link for diagnostics.
func (d *Deck) Info() LinkInfo {
	d.mu.Lock()
	info := LinkInfo{Port: d.cfg.Port, Online: d.snap.Online, Simulated: d.cfg.Simulated,
		RxBytes: d.rx.Load(), TxBytes: d.tx.Load(), LastError: d.lastErr,
		Console: append([]string(nil), d.console...)}
	if d.since != nil {
		t := *d.since
		info.ConnectedSince = &t
	}
	d.mu.Unlock()
	if c := d.cur.Load(); c != nil {
		info.Baud = c.Baud
		info.Retransmits, info.Naks = c.Link.Stats()
	}
	return info
}

// Play starts a performance, stopping whatever plays now. It returns once
// the request is accepted; progress shows in the status.
func (d *Deck) Play(ctx context.Context, j Job) error {
	return d.do(ctx, command{op: "play", job: &j})
}

// Pause, Resume, Stop and Restart press the player's keys and wait for the
// player to confirm.
func (d *Deck) Pause(ctx context.Context) error   { return d.do(ctx, command{op: "pause"}) }
func (d *Deck) Resume(ctx context.Context) error  { return d.do(ctx, command{op: "resume"}) }
func (d *Deck) Stop(ctx context.Context) error    { return d.do(ctx, command{op: "stop"}) }
func (d *Deck) Restart(ctx context.Context) error { return d.do(ctx, command{op: "restart"}) }

// SetLevel sets the level (0 = as recorded, negative = softer).
func (d *Deck) SetLevel(ctx context.Context, level int) error {
	return d.do(ctx, command{op: "level", level: &level})
}

// ChangeLevel moves the level by delta steps.
func (d *Deck) ChangeLevel(ctx context.Context, delta int) error {
	return d.do(ctx, command{op: "level", delta: delta})
}

// Arm loads the recorder. StartRecording, StopRecording and Discard drive it.
func (d *Deck) Arm(ctx context.Context, title string) error {
	return d.do(ctx, command{op: "arm", title: title})
}
func (d *Deck) StartRecording(ctx context.Context) error { return d.do(ctx, command{op: "recstart"}) }
func (d *Deck) StopRecording(ctx context.Context) error  { return d.do(ctx, command{op: "recstop"}) }
func (d *Deck) Discard(ctx context.Context) error        { return d.do(ctx, command{op: "discard"}) }

// UtilityResult is how a utility program ended: its console text, and nil
// when it finished on its own.
type UtilityResult struct {
	Console []string
	Err     error
}

// StartTool runs one of the suite's programs as a tool the user drives:
// it returns once the program is starting; its text arrives as console
// events, ToolKey sends it keys, and StopTool ends it. The piano must be
// idle; play and record are refused until it ends.
func (d *Deck) StartTool(ctx context.Context, prog *host.Program, path string) error {
	s := &session{kind: "utility", interactive: true, prog: prog, path: path,
		limit: time.Now().Add(4 * time.Hour), done: make(chan UtilityResult, 1)}
	return d.do(ctx, command{op: "utility", util: s})
}

// ToolKey sends keystrokes (PC console codes) to the running tool.
func (d *Deck) ToolKey(ctx context.Context, key []byte) error {
	return d.do(ctx, command{op: "toolkey", data: key})
}

// StopTool ends the running tool at once, by dropping the link.
func (d *Deck) StopTool(ctx context.Context) error { return d.do(ctx, command{op: "toolstop"}) }

// RunUtility runs one of the suite's utility programs (getlut, …) to its end
// on a fresh connection, with its file service at path (the file the
// original takes on its command line). The piano must be idle; play and
// record are refused until it ends. It gives up after limit.
func (d *Deck) RunUtility(ctx context.Context, prog *host.Program, path string, limit time.Duration) UtilityResult {
	s := &session{kind: "utility", prog: prog, path: path, limit: time.Now().Add(limit),
		done: make(chan UtilityResult, 1)}
	if err := d.do(ctx, command{op: "utility", util: s}); err != nil {
		return UtilityResult{Err: err}
	}
	select {
	case r := <-s.done:
		return r
	case <-ctx.Done():
		return UtilityResult{Err: ctx.Err()}
	}
}

func (d *Deck) do(ctx context.Context, c command) error {
	c.reply = make(chan error, 1)
	select {
	case d.cmds <- c:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-c.reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// onFrame runs on the link's goroutine for every frame: keep it cheap.
func (d *Deck) onFrame(out bool, f link.Frame) {
	if !out && f.TS != 0 {
		d.lastTS.Store(uint32(f.TS) | 1<<16)
	}
}

type counter struct{ n *atomic.Int64 }

func (c counter) Write(p []byte) (int, error) { c.n.Add(int64(len(p))); return len(p), nil }

func (d *Deck) opts() host.ConnectOptions {
	return host.ConnectOptions{OnFrame: d.onFrame, RawRx: counter{&d.rx}, RawTx: counter{&d.tx}}
}

// send delivers a message from a worker goroutine to the run loop.
func (d *Deck) send(m any) {
	select {
	case d.inbox <- m:
	case <-d.ctx.Done():
	}
}

// ---- the run loop ----

// Run drives the deck until ctx ends.
func (d *Deck) Run(ctx context.Context) {
	d.ctx = ctx
	d.connectIdle()
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			if d.sess != nil {
				d.sess.cancel()
			}
			d.release(d.conn)
			<-d.released // leave the port closed for whoever opens it next
			return
		case c := <-d.cmds:
			d.command(c)
		case m := <-d.inbox:
			d.message(m)
		case <-d.retryC:
			d.retryC = nil
			d.connectIdle()
		case <-tick.C:
			d.tick()
		}
	}
}

func (d *Deck) publish() {
	d.mu.Lock()
	d.snap = d.st
	if d.st.Piece != nil {
		p := *d.st.Piece
		d.snap.Piece = &p
	}
	if d.st.MinLevel != nil {
		m := *d.st.MinLevel
		d.snap.MinLevel = &m
	}
	if d.st.Collection != nil {
		c := *d.st.Collection
		d.snap.Collection = &c
	}
	d.snap.Tracks = append([]TrackRef(nil), d.st.Tracks...)
	if d.snap.Tracks == nil {
		d.snap.Tracks = []TrackRef{}
	}
	snap := d.snap
	d.mu.Unlock()
	d.Publish(Event{"status", snap})
}

func (d *Deck) setState(s State) {
	if d.st.State != s {
		d.st.State = s
		d.st.Since = time.Now()
		d.logf("deck: %s", s)
	}
	d.publish()
}

func (d *Deck) notice(level, format string, args ...any) {
	text := fmt.Sprintf(format, args...)
	d.logf("deck: %s: %s", level, text)
	d.Publish(Event{"notice", Notice{level, text}})
}

func (d *Deck) setErr(err error) {
	if err == nil {
		return
	}
	d.mu.Lock()
	d.lastErr = err.Error()
	d.mu.Unlock()
}

func (d *Deck) setConn(c *host.Conn) {
	d.conn = c
	d.cur.Store(c)
	d.mu.Lock()
	if c != nil {
		t := time.Now()
		d.since = &t
	} else {
		d.since = nil
	}
	d.mu.Unlock()
}

// release closes a connection in the background. A serial port can only be
// open once (Linux refuses a second open with "Serial port busy"), so every
// new connection first waits for the closes before it (see connect).
func (d *Deck) release(c *host.Conn) {
	if c == nil {
		return
	}
	prev, done := d.released, make(chan struct{})
	d.released = done
	go func() {
		<-prev
		c.Close()
		close(done)
	}()
}

// connect opens a connection in the background, once earlier connections
// are closed, and reports it to the run loop.
func (d *Deck) connect(ctx context.Context, sess *session) {
	wait := d.released
	go func() {
		<-wait
		c, err := d.cfg.Connect(ctx, d.opts())
		d.send(connDone{conn: c, err: err, sess: sess})
	}()
}

// connectIdle opens the idle link, unless one is open or opening.
func (d *Deck) connectIdle() {
	if d.conn != nil || d.connecting || d.sess != nil {
		return
	}
	d.connecting = true
	if d.st.State != Offline {
		d.setState(Connecting)
	}
	d.connect(d.ctx, nil)
}

func (d *Deck) scheduleRetry() {
	if d.backoff == 0 {
		d.backoff = d.cfg.RetryMin
	} else {
		d.backoff = min(2*d.backoff, d.cfg.RetryMax)
	}
	if d.retry != nil {
		d.retry.Stop()
	}
	d.retry = time.NewTimer(d.backoff)
	d.retryC = d.retry.C
}

func (d *Deck) offline(err error) {
	d.setErr(err)
	d.st.Online = false
	d.st.Message = ErrOffline.Error()
	d.setState(Offline)
	if d.next != nil {
		d.notice("error", "Could not start “%s”: %v", d.next.Piece.Title, ErrOffline)
		d.next = nil
	}
	d.scheduleRetry()
}

func (d *Deck) message(m any) {
	switch m := m.(type) {
	case connDone:
		d.connDone(m)
	case linkDown:
		if m.conn == d.conn && d.sess == nil {
			d.release(m.conn) // the link stopped, but the port is still open
			d.setConn(nil)
			d.setErr(m.conn.Err())
			d.notice("warn", "Lost the connection to the piano")
			d.offline(m.conn.Err())
		}
	case sessDone:
		d.sessDone(m)
	case conLine:
		if m.sess == d.sess {
			d.consoleLine(m.line)
		}
	}
}

func (d *Deck) connDone(m connDone) {
	if m.sess == nil { // the idle link
		d.connecting = false
		if m.err != nil {
			d.offline(m.err)
			return
		}
		if d.sess != nil { // a session started meanwhile
			d.release(m.conn)
			return
		}
		d.backoff = 0
		d.setConn(m.conn)
		d.st.Online = true
		d.st.Message = ""
		d.setState(Idle)
		go func(c *host.Conn) {
			<-c.Exited()
			d.send(linkDown{c})
		}(m.conn)
		if j := d.next; j != nil {
			d.next = nil
			d.startSession("play", j, "")
		}
		return
	}
	s := m.sess
	if s != d.sess {
		d.release(m.conn)
		return
	}
	if m.err != nil {
		d.sess = nil
		d.finishUtility(s, m.err)
		if s.aborted {
			d.afterSession(false)
			return
		}
		d.st.Piece, d.st.PositionMs, d.st.DurationMs, d.st.Program, d.st.RecordingTitle = nil, 0, 0, "", ""
		d.st.Collection, d.st.Tracks, d.st.TrackIndex = nil, nil, 0
		d.notice("error", "Could not connect to the piano: %v", m.err)
		d.offline(m.err)
		return
	}
	s.conn = m.conn
	d.setConn(m.conn)
	d.st.Online = true
	d.backoff = 0
	path := s.path // a utility program's file
	if s.kind != "utility" {
		path = filepath.Join(s.dir(d), s.base()+".kb")
	}
	fs := host.NewFileService(path)
	if s.kind == "record" {
		fs.Arg = recordingHeader(s.title, time.Now())
	}
	hs := &host.Session{Program: s.prog, Files: fs, Keys: s.keys,
		Console: &lineWriter{emit: func(line string) { d.send(conLine{s, line}) }}}
	go func() {
		err := hs.Run(s.ctx, m.conn.Link, m.conn.Exited())
		d.send(sessDone{s, err})
	}()
}

func (s *session) dir(d *Deck) string {
	if s.kind == "record" {
		return d.cfg.StagingDir
	}
	return s.job.Dir
}

func (s *session) base() string {
	if s.kind == "record" {
		return s.name
	}
	return s.job.Name
}

// startSession closes the idle link and opens a fresh one for a program.
func (d *Deck) startSession(kind string, j *Job, title string) error {
	load := d.cfg.Player
	if kind == "record" {
		load = d.cfg.Recorder
	}
	if load == nil {
		return fmt.Errorf("%w: no %s program configured", ErrNoProgram, kind)
	}
	prog, err := load()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNoProgram, err)
	}
	ctx, cancel := context.WithCancel(d.ctx)
	s := &session{kind: kind, job: j, title: title, prog: prog, keys: make(chan []byte, 16),
		ctx: ctx, cancel: cancel, deadline: time.Now().Add(d.cfg.StartTimeout)}
	d.st.Voiced, d.st.Corrected = false, false
	if kind == "record" {
		s.name = "r" + strconv.FormatInt(time.Now().Unix()%78364164096, 36) // 7 base-36 digits at most
		d.st.Piece = nil
		d.st.DurationMs = 0
		d.st.RecordingTitle = title
		d.st.Collection, d.st.Tracks, d.st.TrackIndex = nil, nil, 0
	} else {
		d.st.DurationMs = j.Duration.Milliseconds()
		d.st.Voiced, d.st.Corrected = j.Voiced, j.Corrected
		d.st.RecordingTitle = ""
		d.st.Collection = j.Collection
		d.st.Tracks = j.Tracks
		if len(d.st.Tracks) == 0 {
			d.st.Tracks = []TrackRef{{Piece: j.Piece, DurationMs: d.st.DurationMs}}
		}
		d.st.TrackIndex = 0
		p := d.st.Tracks[0].Piece
		d.st.Piece = &p
	}
	d.sess = s
	d.st.Program = prog.Banner
	d.st.PositionMs = 0
	d.st.Message = ""
	d.resetClock()
	d.release(d.conn) // the idle link: the new connection waits for it to close
	d.setConn(nil)
	d.setState(Loading)
	d.connect(ctx, s)
	return nil
}

// startUtility closes the idle link and opens a fresh one for a utility program.
func (d *Deck) startUtility(s *session) {
	ctx, cancel := context.WithCancel(d.ctx)
	s.ctx, s.cancel = ctx, cancel
	s.keys = make(chan []byte, 16)
	s.deadline = time.Now().Add(d.cfg.StartTimeout)
	d.sess = s
	d.st.Piece, d.st.Collection, d.st.Tracks, d.st.TrackIndex = nil, nil, nil, 0
	d.st.PositionMs, d.st.DurationMs, d.st.RecordingTitle = 0, 0, ""
	d.st.Program = s.prog.Banner
	d.st.Message = ""
	d.release(d.conn)
	d.setConn(nil)
	d.setState(Working)
	d.connect(ctx, s)
}

// finishUtility tells whoever waits for a utility program how it ended.
func (d *Deck) finishUtility(s *session, err error) {
	if s.kind != "utility" || s.done == nil {
		return
	}
	s.done <- UtilityResult{Console: s.lines, Err: err}
	s.done = nil
}

// abort ends the session at once by dropping the link.
func (d *Deck) abort() {
	if s := d.sess; s != nil {
		s.aborted = true
		s.cancel()
	}
}

func (d *Deck) sessDone(m sessDone) {
	s := m.sess
	if s != d.sess {
		return
	}
	d.sess = nil
	d.release(s.conn)
	d.setConn(nil)
	ok := errors.Is(m.err, host.ErrQuit)
	failed := !ok && !s.aborted
	if s.kind == "utility" {
		var err error
		switch {
		case s.aborted:
			err = errors.New("the program did not finish")
		case !ok:
			err = m.err
		}
		if s.interactive && err == nil {
			d.notice("info", "%s finished", s.prog.Banner)
		}
		d.finishUtility(s, err)
	}
	if s.kind == "record" {
		d.finishRecording(s, ok)
	}
	if d.pend != nil {
		if d.pend.what == "stop" || ok {
			d.pend.reply <- nil
		} else {
			d.pend.reply <- fmt.Errorf("%w: the program ended", ErrState)
		}
		d.pend = nil
	}
	d.stepping = false
	if failed {
		d.setErr(m.err)
		d.notice("error", "The piano stopped: %v", m.err)
		d.st.Message = m.err.Error()
	}
	d.afterSession(failed)
}

// afterSession reopens the idle link, or starts the next piece.
func (d *Deck) afterSession(failed bool) {
	d.st.Piece = nil
	d.st.Voiced, d.st.Corrected = false, false
	d.st.Collection, d.st.Tracks, d.st.TrackIndex = nil, nil, 0
	d.st.PositionMs = 0
	d.st.DurationMs = 0
	d.st.Program = ""
	d.st.RecordingTitle = ""
	d.st.Level = d.st.TargetLevel
	if failed {
		d.next = nil
		d.st.Online = false
		d.setState(Fault)
		d.scheduleRetry()
		return
	}
	if j := d.next; j != nil {
		d.next = nil
		if err := d.startSession("play", j, ""); err == nil {
			return
		}
	}
	d.connectIdle()
}

func (d *Deck) finishRecording(s *session, ok bool) {
	base := filepath.Join(d.cfg.StagingDir, s.name)
	clean := func() {
		for _, ext := range []string{".kb", ".lp", ".sp", ".kb$", ".lp$", ".sp$"} {
			os.Remove(base + ext)
		}
	}
	if s.discard {
		clean()
		d.notice("info", "Recording discarded")
		return
	}
	if _, err := os.Stat(base + ".kb"); err != nil {
		clean()
		if ok {
			d.notice("warn", "The recorder finished without saving anything")
		}
		return
	}
	if d.cfg.OnRecorded == nil {
		return
	}
	id, err := d.cfg.OnRecorded(d.cfg.StagingDir, s.name, s.title)
	if err != nil {
		d.notice("error", "Could not save the recording (it is kept as %s): %v", base, err)
		return
	}
	clean()
	d.Publish(Event{"library", map[string]string{"id": id}})
	d.notice("info", "Saved the recording “%s”", s.title)
}

func (d *Deck) addConsole(line string) {
	d.mu.Lock()
	d.console = append(d.console, line)
	if n := len(d.console); n > 200 {
		d.console = append([]string(nil), d.console[n-200:]...)
	}
	d.mu.Unlock()
	d.Publish(Event{"console", map[string]string{"line": line}})
}

func (d *Deck) consoleLine(line string) {
	if s := d.sess; s != nil && s.kind == "utility" {
		s.started = true
		s.lines = append(s.lines, line)
		d.addConsole(line)
		return
	}
	d.addConsole(line)
	s := d.sess
	ev, n := parseLine(line)
	switch ev {
	case evStart:
		s.started = true
		d.resetClock()
		d.setState(Playing)
		d.applyLevel()
	case evPause:
		d.updatePosition()
		if d.st.State == Playing {
			d.setState(Paused)
		}
		d.resolve("pause", nil)
	case evResume:
		d.setState(Playing)
		d.resolve("resume", nil)
	case evQuit:
		d.setState(Stopping)
		d.resolve("stop", nil)
	case evRestart:
		d.resetClock()
		d.st.PositionMs = 0
		d.followTrack()
		d.setState(Playing)
		d.resolve("restart", nil)
	case evEnd:
		d.st.PositionMs = d.st.DurationMs
		d.followTrack()
		d.setState(Stopping)
	case evLevel:
		d.levelReport(n)
	case evArmed:
		s.started = true
		d.setState(Armed)
	case evRecording:
		d.resetClock()
		d.st.PositionMs = 0
		d.setState(Recording)
		d.resolve("recstart", nil)
	case evInterlock:
		d.st.Message = line
		d.notice("error", "%s", line)
		d.publish()
	case evRecEnd:
		if d.st.State == Recording {
			d.setState(Saving)
		}
	case evDiscarded:
		// The recorder starts over and prints its prompts again (evArmed).
		d.notice("info", "The recorder discarded the recording")
	case evRecRefused:
		d.st.Message = "The piano's settings have no recording type or serial number, so it will not record"
		d.notice("error", "%s", d.st.Message)
		d.publish()
	}
}

func (d *Deck) resolve(what string, err error) {
	if d.pend != nil && d.pend.what == what {
		d.pend.reply <- err
		d.pend = nil
		if what != "level" {
			d.applyLevel() // a level change may have waited for this command
		}
	}
}

func (d *Deck) await(what string, reply chan error) {
	d.pend = &pending{what: what, deadline: time.Now().Add(d.cfg.ConfirmTimeout), reply: reply}
}

func (d *Deck) key(k []byte) {
	if d.sess == nil {
		return
	}
	select {
	case d.sess.keys <- k:
	default:
	}
}

func (d *Deck) resetClock() {
	d.havePos = false
	d.pos = 0
	d.lastTS.Store(0)
}

// ---- level ----

func (d *Deck) playerRunning() bool {
	return d.sess != nil && d.sess.kind == "play" && d.sess.started &&
		(d.st.State == Playing || d.st.State == Paused)
}

// applyLevel steps the player toward the target level, one key at a time.
func (d *Deck) applyLevel() {
	if !d.playerRunning() || d.stepping || d.st.Level == d.st.TargetLevel {
		if !d.stepping {
			d.resolve("level", nil)
		}
		return
	}
	d.stepping = true
	d.stepDown = d.st.TargetLevel < d.st.Level
	d.stepUntil = time.Now().Add(d.cfg.ConfirmTimeout)
	if d.stepDown {
		d.key(host.KeyDown)
	} else {
		d.key(host.KeyUp)
	}
}

func (d *Deck) levelReport(n int) {
	prev := d.st.Level
	d.st.Level = n
	if d.stepping {
		d.stepping = false
		if n == prev && d.stepDown { // no change: the lowest level
			d.floor(n)
		}
	}
	d.publish()
	d.applyLevel()
}

// floor records that the player goes no lower than n.
func (d *Deck) floor(n int) {
	m := n
	d.st.MinLevel = &m
	if d.st.TargetLevel < n {
		d.st.TargetLevel = n
		if d.cfg.OnLevel != nil {
			d.cfg.OnLevel(n)
		}
	}
}

func (d *Deck) setTarget(t int) {
	t = min(t, 0)
	if d.st.MinLevel != nil {
		t = max(t, *d.st.MinLevel)
	}
	if t != d.st.TargetLevel {
		d.st.TargetLevel = t
		if d.cfg.OnLevel != nil {
			d.cfg.OnLevel(t)
		}
	}
	if !d.playerRunning() {
		d.st.Level = t
	}
}

// ---- commands ----

func (d *Deck) command(c command) {
	reply := func(err error) { c.reply <- err }
	st := d.st.State
	busy := d.pend != nil
	switch c.op {
	case "play":
		switch {
		case d.sess != nil && d.sess.kind == "utility":
			reply(fmt.Errorf("%w: the piano is busy with %s", ErrState, d.sess.prog.Banner))
		case d.sess != nil && d.sess.kind == "record":
			reply(fmt.Errorf("%w: a recording is in progress", ErrState))
		case d.sess != nil:
			if d.cfg.Player == nil {
				reply(ErrNoProgram)
				return
			}
			if _, err := d.cfg.Player(); err != nil {
				reply(fmt.Errorf("%w: %v", ErrNoProgram, err))
				return
			}
			d.next = c.job
			d.stop(nil)
			reply(nil)
		case st == Idle:
			reply(d.startSession("play", c.job, ""))
		default: // offline, connecting, fault: start once the link is up
			if d.cfg.Player == nil {
				reply(ErrNoProgram)
				return
			}
			if _, err := d.cfg.Player(); err != nil {
				reply(fmt.Errorf("%w: %v", ErrNoProgram, err))
				return
			}
			d.next = c.job
			if d.retryC != nil {
				d.retry.Stop()
				d.retryC = nil
			}
			d.connectIdle()
			reply(nil)
		}
	case "pause", "resume", "restart":
		want := map[string]State{"pause": Playing, "resume": Paused, "restart": Playing}[c.op]
		if c.op == "pause" && st == Paused || c.op == "resume" && st == Playing {
			reply(nil)
			return
		}
		if st != want && !(c.op == "restart" && st == Paused) || d.sess == nil {
			reply(fmt.Errorf("%w: %s", ErrState, st))
			return
		}
		if busy {
			reply(ErrBusy)
			return
		}
		d.await(c.op, c.reply)
		if c.op == "restart" {
			d.key([]byte{0x1B})
		} else {
			d.key([]byte{' '})
		}
	case "stop":
		d.next = nil
		if d.sess == nil || d.sess.kind != "play" {
			reply(nil)
			return
		}
		d.stop(c.reply)
	case "level":
		t := d.st.TargetLevel + c.delta
		if c.level != nil {
			t = *c.level
		}
		if t > 0 && c.level != nil {
			reply(errors.New("the level cannot be above 0 (as recorded)"))
			return
		}
		d.setTarget(t)
		if d.playerRunning() && d.st.Level != d.st.TargetLevel {
			if busy && d.pend.what != "level" {
				d.publish()
				reply(nil) // applied once the current command is answered
				return
			}
			if d.pend != nil {
				d.pend.reply <- nil
			}
			d.pend = &pending{what: "level", deadline: time.Now().Add(time.Hour), reply: c.reply}
			d.publish()
			d.applyLevel()
			return
		}
		d.publish()
		reply(nil)
	case "arm":
		if d.sess != nil {
			reply(fmt.Errorf("%w: %s", ErrState, st))
			return
		}
		if st != Idle {
			reply(ErrOffline)
			return
		}
		title := c.title
		if title == "" {
			title = "Recording " + time.Now().Format("2006-01-02 15:04")
		}
		reply(d.startSession("record", nil, title))
	case "recstart":
		if st != Armed {
			reply(fmt.Errorf("%w: %s", ErrState, st))
			return
		}
		if busy {
			reply(ErrBusy)
			return
		}
		d.await("recstart", c.reply)
		d.key([]byte{' '})
	case "recstop":
		if st != Recording {
			reply(fmt.Errorf("%w: %s", ErrState, st))
			return
		}
		d.key([]byte{0x0D})
		d.setState(Saving)
		reply(nil)
	case "toolkey":
		if d.sess == nil || d.sess.kind != "utility" || !d.sess.interactive {
			reply(fmt.Errorf("%w: no tool is running", ErrState))
			return
		}
		d.key(c.data)
		reply(nil)
	case "toolstop":
		if d.sess != nil && d.sess.kind == "utility" && d.sess.interactive {
			d.abort()
		}
		reply(nil)
	case "utility":
		switch {
		case d.sess != nil:
			reply(fmt.Errorf("%w: the piano is busy (%s)", ErrState, st))
		case st != Idle:
			reply(ErrOffline)
		default:
			d.startUtility(c.util)
			reply(nil)
		}
	case "discard":
		if d.sess == nil || d.sess.kind != "record" || st == Saving {
			reply(fmt.Errorf("%w: %s", ErrState, st))
			return
		}
		d.sess.discard = true
		d.abort()
		reply(nil)
	default:
		reply(fmt.Errorf("unknown command %q", c.op))
	}
}

// stop ends the playing session: Enter when the player is running, which it
// confirms with "QUIT PLAYING"; otherwise, or if it does not confirm, the
// link is dropped.
func (d *Deck) stop(reply chan error) {
	if d.pend != nil && d.pend.what != "stop" {
		d.pend.reply <- fmt.Errorf("%w: stopped", ErrState)
		d.pend = nil
	}
	if d.pend != nil { // a stop is already under way
		if reply != nil {
			old := d.pend.reply
			d.pend.reply = reply
			old <- nil
		}
		return
	}
	if d.playerRunning() {
		if reply == nil {
			reply = make(chan error, 1)
		}
		d.await("stop", reply)
		d.key([]byte{0x0D})
		d.setState(Stopping)
		return
	}
	d.abort()
	d.setState(Stopping)
	if reply != nil {
		reply <- nil
	}
}

// ---- timers ----

func (d *Deck) tick() {
	now := time.Now()
	if p := d.pend; p != nil && now.After(p.deadline) {
		switch p.what {
		case "stop":
			d.notice("warn", "The player did not confirm the stop; dropping the link")
			d.abort()
			p.reply <- nil
		case "recstart":
			// The recorder's confirmation text is not verified on the piano yet.
			d.resetClock()
			d.st.Message = "Recording (the piano did not confirm)"
			d.setState(Recording)
			p.reply <- nil
		default:
			p.reply <- fmt.Errorf("%w (%s)", ErrNoConfirm, p.what)
		}
		d.pend = nil
	}
	if d.stepping && now.After(d.stepUntil) {
		d.stepping = false
		if d.stepDown { // pressing Down changed nothing: the lowest level
			d.floor(d.st.Level)
			d.publish()
			d.resolve("level", nil)
		} else {
			d.resolve("level", fmt.Errorf("%w (level)", ErrNoConfirm))
		}
	}
	if s := d.sess; s != nil && s.kind == "utility" && !s.aborted && now.After(s.limit) {
		d.notice("error", "The piano did not finish %s", s.prog.Banner)
		d.abort()
	}
	if s := d.sess; s != nil && !s.started && now.After(s.deadline) {
		s.deadline = now.Add(time.Hour)
		d.notice("error", "The piano did not start the program")
		d.st.Message = "The piano did not start the program"
		d.abort()
	}
	switch d.st.State {
	case Playing, Paused, Recording, Stopping:
		d.updatePosition()
	}
}

// updatePosition follows the piano's clock: a 16-bit count of 1.25 ms ticks
// that starts slightly below 0 just before "START PLAYING".
func (d *Deck) updatePosition() {
	v := d.lastTS.Load()
	if v&(1<<16) == 0 {
		return
	}
	ts := uint16(v)
	if !d.havePos {
		d.pos, d.havePos = int64(int16(ts)), true
	} else {
		d.pos += int64(int16(ts - d.prevTS))
	}
	d.prevTS = ts
	ms := max(d.pos, 0) * 1000 / se.TicksPerSecond
	if d.st.DurationMs > 0 {
		ms = min(ms, d.st.DurationMs)
	}
	if ms != d.st.PositionMs {
		d.st.PositionMs = ms
		d.followTrack()
		d.publish()
	}
}

// followTrack makes the status name the track the position is in.
func (d *Deck) followTrack() {
	i := 0
	for j, t := range d.st.Tracks {
		if t.StartMs <= d.st.PositionMs {
			i = j
		}
	}
	if i < len(d.st.Tracks) && (i != d.st.TrackIndex || d.st.Piece == nil) {
		d.st.TrackIndex = i
		p := d.st.Tracks[i].Piece
		d.st.Piece = &p
	}
}
