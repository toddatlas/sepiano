# Piano service

**Status: Phase 1 built.** `sepianod` (playback, level, recording, library,
API, web UI, deployment) is written, tested against the simulated piano and
installed on the Pi. It has played full pieces on the real piano from a
phone, with stop and the level confirmed; pause, restart, recording and power
cycles are still to check at the piano. See "What is built" at the end. The
decisions below stay open to the piano's owner.

## Goal

`sehost` on a Raspberry Pi 4 already plays full performances on the SE piano
(see the README). The next step is a headless service, since the Pi will be
hidden inside the piano. It should provide:

- a library of performances that can be played;
- play / pause / stop;
- recording (start and stop);
- volume up and down;
- uploading new files and downloading existing ones.

It needs a web UI first, and an iPhone app later.

## Decided so far (revisit freely)

| Decision | Choice | Why |
|----------|--------|-----|
| Where it lives | One daemon, `sepianod`, in this repo | It owns the serial port and serves the API and web UI from one static binary, so the API and UI change together. |
| Web UI | First pass, as a mobile-first PWA | It's the only way to use the piano without SSH. "Add to Home Screen" gives an iPhone app right away, and it proves the API. |
| Web UI toolchain | Plain HTML, CSS and JavaScript modules, no build step, embedded in the binary | *Changed from Vite + Preact + TypeScript.* Seven screens don't need a framework, and the project stays Go-only: no Node on the development machine, nothing to rebuild before `go build`. |
| Library index | In memory, rebuilt from the per-piece `.json` files at start | *Changed from SQLite.* About 350 pieces fit in memory; the files were already the source of truth, so there is no database to corrupt on a power cut and no extra dependency. Revisit if search or history outgrow it. |
| Link per piece | A fresh connection for each piece or recording; a link held open while idle | *Settled ahead of Phase 0 question 3.* Closing and reopening is the sequence already proven on the piano, and it avoids a real problem with reusing a link: the previous session's channel readers stay blocked and would steal the next session's data. |
| iPhone app | Later, in a separate repo (`sepiano-ios`, SwiftUI) | Different toolchain and release cycle. It talks to the daemon through a checked-in OpenAPI spec. Build it once the PWA shows which native features matter (lock screen, Siri/Shortcuts, widgets). |
| How playback is controlled | Drive Stahnke's own Z80 player and recorder | Every control maps onto a key those programs already understand, and their console text confirms each state change. Nothing is reimplemented on the Z80. |
| Access | Home LAN only, no login | Simplest. Destructive actions get confirmations in the UI. |
| Default renderings | Prefer `Steinway_SED133291` (New York) | This piano is that instrument; other renderings stay available. |
| Power | Assume it can be cut without warning | Undecided, so the SD card must survive sudden power loss. |

## Open decisions for the owner

- How the Pi is powered inside the piano: switched with the piano, or always on.
- Whether to use Wi-Fi or Ethernet inside the closed piano.
- Which features come first, and whether recording belongs in the first pass.
- Whether the web UI needs anything beyond the screens listed below.
- Whether remote access (outside the home network) is ever wanted.

## Architecture

```
 phone / browser ──HTTP+SSE──▶ sepianod (Pi, systemd, http://piano.local)
                                ├─ api/      REST /api/v1 + SSE /api/v1/events, serves web/dist
                                ├─ deck/     one actor goroutine: serial port, sessions, state machine
                                │             └─ host.Connect / host.Session / host.FileService (existing)
                                ├─ library/  file store + in-memory index (+ se package for parse/convert)
                                └─ sim/      fake instrument for development without the piano
```

### Deck (the instrument controller)

- **One goroutine owns `host.Conn`.** Commands arrive on a channel, so many
  clients can't race each other. State is broadcast over SSE.
- **States:** Offline → Connecting → Idle → Loading (upload, about 2.1 s) →
  Playing ⇄ Paused → Idle. Recording runs Armed → Recording → Saving → Idle.
  Fault carries a message and retries.
- **Controls map to player keys,** sent through the existing
  `host.Session.Keys` channel (codes in `host/keys.go`). Each state change is
  confirmed by parsing the console, not assumed. The parser is a small writer
  wrapping `Session.Console`.

  | Control | Key | Confirmed by |
  |---------|-----|--------------|
  | pause / resume | Space | "STOP PLAYING AT TIME=…" / "RESUME PLAYING" |
  | stop | Enter | "QUIT PLAYING", then ESC |
  | restart | Esc | "RESTART PERFORMANCE" |
  | volume | Down / Up | "LEVEL=0 (NORMAL)" / "LEVEL=-n" |

- **Volume = the player's level,** from 0 (as recorded) down to −N. The player
  can only subdue, not go louder. The level lives in firmware RAM at Z80
  `0xEE80`, so it may persist between songs. If it doesn't, the deck re-applies
  the chosen level after "START PLAYING". A "louder than recorded" velocity
  trim, applied while serving the file, is phase 2.
- **Live position comes from the piano's own 800 Hz clock.** Its frames carry
  the clock in `ts`, idle frames included: 87,220 of 88,080 idle frames had it
  during a 3:49 piece. Capture it through the existing `link.Pump.OnFrame`
  hook, unwrap 16 bits, divide by 800, and push it over SSE at about 5 Hz.
  Duration comes from `se.KB.End`.
- **Recording uses `record.exe`'s Z80 program**, whose behaviour is now read
  from its code (docs/SUITE-PROGRAMS.md, "The recorder"):
  1. arm: upload the recorder. The file service points at a staging name,
     and the recording's title and date go in as its argument (the header
     text). It prints all three prompts; "DEPRESS SPACE BAR TO START
     RECORDING" means armed.
  2. start: Space, confirmed by "START RECORDING".
  3. stop: Enter. It prints "END OF RECORDING", then commits the files
     (`0x12`/`0x13`/`0x14`/`0x15` in `host/files.go`) and exits. The deck
     imports the result as a new piece.

  "KEYBOARD INTERLOCK FAILURE" comes before arming, and "ILLEGAL RECORDING
  TYPE OR MODE" (a table without a recording type or serial number) is
  shown as the reason. Velocity by key checks a recording's sensors.
- **Link lifetime.** Keep the link up while idle, so the UI shows
  Online/Offline at once and a piano power cycle is noticed (`link.ErrSilent`
  starts a reconnect loop). Each piece closes the idle link and opens a fresh
  one for the upload (already proven, about 2.5 s to the first note). Running
  several sessions on one open link would first need `host.Session` to stop
  its channel readers when it ends: today they stay blocked in `Link.Read`
  until the link fails, and would take the next session's bytes.

### Library

- **Files are the source of truth; the index lives in memory.**
  - Each piece is stored as `/var/lib/sepiano/library/<id>.{kb,lp,sp}` plus
    `<id>.json` metadata (and `<id>.mid` for converted MIDI files). `<id>` is
    8 characters, because the Z80 rejects longer names if it asks for one.
  - The index is rebuilt from the folder at start and on `POST
    /library/rescan`. Sets copied in by hand get an id and metadata; a
    missing `.json` is rebuilt from the `.kb`.
  - Writes go to a temp file, then fsync and rename (and fsync the folder).
    The `.json` is written last.
  - Settings (preferred instrument, level) are in
    `/var/lib/sepiano/settings.json`.
- **Piece fields:** id, title, composer, performer, recorded_on, instrument,
  perf_key, duration_ms, notes, origin (import | upload | midi | recorded),
  source_name, header_text, kb_sha256, has_midi_source, added_at.
- **Hidden pieces** (`hidden`, set with PATCH; 2026-11-24, the owner's
  wish: "we never delete anything"). A hidden piece keeps its files but is
  left out of `GET /pieces` and search unless `hidden=only` or `all`.
  `updated_at` is when an edited copy was last saved over; the Library
  can sort by it ("Newest first").
- **Metadata** comes, best first, from: edits in the app; the album `.bat`
  catalogs and name lists, applied by `sepianod tag` (album, track, titles for
  files without a header); the `.kb` header text ("Mozart: Sonata No. 12 …
  / Philippe Entremont, pianist / Recorded February 12, 1990"; Ampico roll
  headers); and the file name (Zenph style, e.g.
  `Tatum19490402-Gershwin_STWOM-Boesendorfer_SE42251-590-22(20150112)-SE`, with
  full performer names and known abbreviations spelled out). Fields set by
  hand or by `tag` are locked; the rest are derived again whenever the parser
  improves (`metaVersion`).
- **Renderings.** `perf_key` (performer + date + work) groups the renderings
  of one performance. The default rendering comes from the
  `settings.preferred_instrument` setting.
- **Import:** `sepianod import <dir>` scans for `.kb/.lp/.sp` sets and uploads
  them to the running service, which validates them and dedupes by the `.kb`
  hash (`-data DIR` writes straight into a stopped service's folder). Turning
  the `.bat` files into playlists comes with playlists, in phase 2.
- **Upload** accepts a `.kb/.lp/.sp` set, a `.zip` of sets, or a `.mid`
  (converted with `se.FromMIDI`, original kept).
- **Download** offers:
  - the original set as a `.zip`;
  - MIDI (`Performance.ToMIDI`);
  - a whole-library backup `.zip`, which matters with an SD card.

### API (`/api/v1`, OpenAPI in `api/openapi.yaml`)

| Area | Endpoints |
|------|-----------|
| Live state | `GET /status`; `GET /events` (SSE: state, position, level, console, library-changed) |
| Playback | `POST /deck/play {piece}`, `/deck/pause`, `/deck/resume`, `/deck/stop`, `/deck/restart`, `/deck/level {delta \| level}` |
| Recording | `POST /record/arm {title}`, `/record/start`, `/record/stop`, `/record/discard` |
| Library | `GET /pieces?q=&composer=&performer=&instrument=`, `GET/PATCH/DELETE /pieces/{id}`, `POST /pieces` (multipart), `GET /pieces/{id}/download?format=se\|midi`, `GET /export`, `POST /library/rescan` |
| Albums and play lists | `GET /albums`, `GET /albums/{name}`; `/playlists` CRUD; `POST /deck/play {album | playlist | pieces, start}`; `POST /deck/next`, `/deck/previous` |
| Instrument | `GET /instrument` (port, baud, link stats, program banners, recent console lines), `PUT /programs/{play\|record}` (upload the .exe) |
| Settings | `GET/PUT /settings` (preferred instrument, level) |
| Simulator | `POST /sim/power {on}`, only with `-simulate` |

The API uses the stdlib `net/http` with Go 1.22 method routes. It sends SSE
rather than WebSockets: it's stdlib-only, `EventSource` reconnects by itself,
and iOS can read it.

### Web UI (`web/`, plain HTML/CSS/JavaScript, embedded as is)

The screens:
- **Now Playing:** title and performer, a progress bar from the piano clock,
  pause/stop/restart, and level −/+ with a readout.
- **Library:** search, filters, renderings grouped together, A to Z or
  newest first, and a "Hidden" filter.
- **Recordings** (a Library section): the recordings made here and their
  copies, newest first; tick several and hide them, or show hidden ones
  again.
- **Piece:** play, download as SE or MIDI, edit, hide (or show again),
  delete for good (only after typing DELETE).
- **Albums and play lists:** play a whole album or play list, or from any track; previous and next on Now Playing.
- **Record:** arm → start → stop → name it. The timer counts the Pi's
  time from "START RECORDING"; the piano's clock gave no usable position
  while recording.
- **Upload.**
- **Status & Settings:** link health, programs, preferred instrument, backup.

There is no build step and no Node: edit the files and rebuild `sepianod`. The
UI loads nothing from the internet, so it works on a home network without
one. The Pi gets one binary.

### Running hidden in the piano

- **systemd unit** `sepianod.service`:
  - runs as a dedicated `sepiano` user in group `dialout`, with
    `Restart=always`;
  - gets `CAP_NET_BIND_SERVICE` so the UI lives on port 80, at
    `http://<hostname>.local/` through mDNS/avahi, which needs no DNS changes.
    The Pi is still called `raspberrypi`; renaming it `piano`
    (`sudo hostnamectl set-hostname piano`, then reboot) would give
    `http://piano.local/`.
- **Discovery:** an avahi service file advertises `_http._tcp` and
  `_sepiano._tcp`, for the iOS app's Bonjour discovery later.
- **Stable port:** a udev rule creates `/dev/sepiano-piano`, matching the
  PL2303 by its USB ids (067b:2303), because `ttyUSB` numbers change with plug
  order. It is the only PL2303 on the Pi; the rule notes how to match the USB
  port instead if that changes.
- **Programs:** `play.exe` and `record.exe` live in
  `/var/lib/sepiano/programs` and are never in git. They're uploaded once via
  the Status page (`PUT /programs/…`) or copied there.
- **The service owns the serial port.** Stop it (`sudo systemctl stop
  sepianod`) before using `sehost` by hand, and start it again afterwards.
- **Sudden power loss:** besides the atomic writes above, phase 2 can switch
  on Raspberry Pi OS's overlay (read-only) root, with data on a separate
  writable partition.
- **Deploy:** `deploy/deploy.sh` (or `make deploy`) cross-compiles
  (`GOOS=linux GOARCH=arm64`), copies over SSH, and runs `deploy/install.sh`
  with sudo: user, binary, unit, udev rule, avahi file, restart.

## Phases

**Phase 0 — hardware spikes at the piano.** Short, using `sehost` in an
interactive session (tmux or `ssh -t`). Someone has to be at the piano to watch
and listen. Each answer changes deck details:
1. Do Space, Enter, Esc and Up/Down work, and what does each print?
   *(Enter and Down: yes, see "What is built".)*
2. How low does the level go, and does it persist across songs?
   *(−16; each upload starts at 0.)*
3. Does a second upload on a link that is still up start a new program?
4. Run `record.exe` end to end: what files does it write, and does it ask for
   the name?
5. What happens on the link when the piano is power-cycled?

**Phase 1 — a usable piano service** (built; milestones in this order):
1. `sim/`: a fake instrument with an echoing handshake, the real prompt
   strings, an 800 Hz clock, key reactions, and file reads through the file
   service. Grown from `fakeZ80` in `host/host_test.go`.
   `sepianod -simulate` then runs everything without the piano.
2. `deck/` with play, pause, resume, stop, restart and level, tested against
   `sim/`.
3. `library/` with import, metadata, upload, download and export.
4. `api/` with SSE, plus `api/openapi.yaml`.
5. The `web/` PWA: Now Playing, Library, Piece, Upload, Status.
6. Recording, end to end.
7. `deploy/` (unit, avahi, udev) and a Makefile.

**Phase 2:**
- editing play lists in the app (they can be imported and played already);
- play history;
- "start at" seeking, by serving a performance cut with `se`;
- the velocity trim for louder-than-recorded;
- the overlay root.

**The suite's other programs** (the owner's order: after playback, before
the editor). `docs/SUITE-PROGRAMS.md` has the survey and the plan: back up
the EEPROM with `getlut`, decode the table, a "Tools" console in the app for
any suite program, then an editor for the instrument's settings. The
editor's design is in `docs/SETTINGS-EDITOR.md`.

**Phase 3:** `sepiano-ios` (SwiftUI, Bonjour, an OpenAPI-generated client), and
remote access if it's ever wanted.

## Files

New:
- `cmd/sepianod/main.go` (the daemon, `-simulate`, `import`, `version`)
- `deck/deck.go`, `deck/console.go` (console text → events), `deck/deck_test.go`
- `sim/sim.go`
- `library/store.go`, `library/files.go`, `library/names.go`, `library/library_test.go`
- `api/server.go` (REST and SSE), `api/programs.go`, `api/icon.go`, `api/openapi.yaml`, `api/server_test.go`
- `web/index.html`, `web/app.js`, `web/style.css`, `web/sw.js`, `web/manifest.webmanifest`, `web/icon.svg`, `web/embed.go`
- `deploy/sepianod.service`, `deploy/sepiano.avahi.service`, `deploy/99-sepiano.rules`, `deploy/install.sh`, `deploy/deploy.sh`
- `Makefile`

Reused unchanged: `host.Session`, `host.FileService`, `host.LoadProgram`, the
key codes, `link.Pump.OnFrame`, `link.ErrSilent`, `se.ParseKB`,
`Performance.ToMIDI` and `se.FromMIDI`.

Small additions to existing packages:
- `host.ConnectOn` runs the link over any open port (the simulator's cable);
  `host.Connect` uses it and behaves as before.
- `link.Pump.Stamp` lets the simulator send its clock in `ts`;
  `link.Link.Stats` reads the counters safely; the simulated cable can be
  closed.
- `se.Performance.Encode` returns the bytes `Save` writes (`Save` uses it).

## Verification

- **Unit tests:** `go test ./...` with deck scenarios against `sim/`:
  - play → pause → resume → level → stop;
  - end of performance;
  - recording produces files that `se.Load` accepts;
  - a link drop moves the deck to Offline, then recovery.
  Library tests: the Zenph name parser, the upload and download round trip
  (bytes identical), MIDI conversion, zips, and rebuilding the index from
  files.
- **Without the piano:** `sepianod -simulate` plus the web UI in a browser;
  check every control and live progress.
- **On the piano:** deploy to the Pi, open `http://raspberrypi.local` on a
  phone, then:
  1. play a known piece and pause, resume, change the level, stop;
  2. upload a MIDI file and play it;
  3. download a piece as MIDI;
  4. record a short phrase and play it back;
  5. power-cycle the piano while idle: the UI should show Offline, then recover.

## What is built

Phase 1, as of the second working session:

- **Simulated piano** (`sim/`): speaks the real link protocol over a
  simulated cable, recognises the uploaded program by its banner, plays the
  part of the player (console text, file reads, keys, level, 800 Hz clock in
  `ts`) or the recorder (prompts, writes through the file service), and can be
  switched off and on. `sepianod -simulate` uses placeholder programs, so no
  copyrighted file is needed to develop.
- **Deck** (`deck/`): one goroutine owns the link. States, confirmations from
  the console, the level stepped one key at a time (the lowest level is found
  when Down stops changing it, then remembered), position from the piano's
  clock, a fresh connection per piece, an idle link for Online/Offline,
  reconnects with back-off, recordings imported when the recorder finishes.
- **Library** (`library/`), **API** (`api/`, with `openapi.yaml`) and **web
  UI** (`web/`): as described above.
- **Deployment** (`deploy/`): installed on the Pi with `deploy/deploy.sh`.
  The idle link to the piano comes up at once and runs clean.

Tests: `go test ./...` covers the deck against the simulator (play, pause,
resume, level, restart, stop, end of performance, switching pieces, the
level floor, recording, discard, interlock failure, power cuts idle and while
playing), the library (names, round trips, zips, MIDI, rebuild, settings,
export) and the API end to end.

**On the real piano** (through the service, driven from a phone):

- Rachmaninoff's Prelude and a second piece played; progress followed the
  piano's clock.
- **Enter** stops: the player prints "QUIT PLAYING", then ends the session.
- **Space** pauses: "STOP PLAYING AT TIME=3912", the position in hundredths
  of a second (39.12 s; the deck's clock said 39.126 s).
- **Down** subdues: "LEVEL=-1" … "LEVEL=-16". **The lowest level is −16**;
  Down at −16 prints nothing, and the deck takes the silence as the floor.
- Each upload starts at "LEVEL=0 (NORMAL)" (seen at every program start).
- Two bugs the simulator could not show, both fixed: a new connection could
  open the serial port before the previous one had closed ("Serial port
  busy"), and a link that went silent was dropped without closing the port,
  which would have kept the service offline after a piano power cycle. The
  simulated cable is now exclusive like a real port, so the tests cover both.

**Assumptions still to check at the piano** (Phase 0), each a small change in
`deck/console.go` or `sim/sim.go` if the piano differs:

- the text after Space when resuming ("RESUME PLAYING") and after Esc
  ("RESTART PERFORMANCE");
- whether a level set during one piece carries into the next upload (the
  deck re-applies the remembered level either way);
- the recorder: that it uses the command-line name and the `.kb$` →
  `.kb` commit, and prints "DEPRESS ENTER TO END RECORDING" once recording
  (if not, the deck still moves to Recording after 3 s and says so);
- `record.exe` is installed (2026-11-23, from the unpacked suite installer)
  but has not recorded at the piano yet;
- a piano power cycle while idle and while playing.

**Not built yet:** editing play lists in the app, play history,
seeking, the louder-than-recorded trim, the overlay root, the iOS app.

### Albums, play lists and continuous play

- **Albums** are the pieces sharing an `album` name, in `track` order: the
  catalog albums (`sepianod tag` from the album `.bat` files), folders of
  numbered Zenph files (named after the folder), and the Zenph Goldberg
  Variations (`GV01`…`GV32`, from the file names).
- **Play lists** are stored in `/var/lib/sepiano/playlists.json`. `sepianod
  tag` imports the play list batch files (`play NAME` lines), matching each
  line to the exact file in the batch file's folder; spacers such as
  `10Sec-C9-400` are kept as the batch file has them.
- **Continuous play.** An album or play list plays as **one joined
  performance** (`library.Join`): key events shifted in time, pedal samples
  laid end to end, each part keeping its own lead-in and ending. Playing
  track by track would leave 2–3 s between tracks while the player program
  uploads again. The status lists the tracks with their start times, and the
  deck reports the one playing from the piano's clock. Next and previous
  play the collection again from another track (a short pause).
- **Which rendering.** Where a track exists in several renderings, the
  preferred one plays (`library.Rank`): this piano's `Steinway_SED133291`,
  then its variants (`_2011NovTemp`, `_2012FebTemp`), then any Steinway, then
  files made for no instrument in particular, then the rest. Renderings for
  other pianos were adjusted to play well on those, not on a Steinway.
