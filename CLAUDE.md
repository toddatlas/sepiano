# sepiano

Open-source replacement for the Windows "SE Program Suite" that drives a
Stahnke SE reproducing piano. The piano is a Steinway with the SE system
(rendering name `Steinway_SED133291`, "New York").

- `README.md` covers status and the tools.
- `docs/SE-FILE-FORMAT.md` and `docs/SE-SERIAL-PROTOCOL.md` are the
  reverse-engineered specifications. Trust them; every claim is backed by the
  decompiled code, the file collection or captures from the real piano.
- `docs/SUITE-PROGRAMS.md` is the suite's other programs, the firmware, and the
  piano's EEPROM table, decoded; `docs/STAHNKE-EDITOR.md` is Stahnke's own
  performance editor, decoded. `docs/SETTINGS-EDITOR.md` is the design
  for editing that table from the app, with the owner's decisions (steps 1
  and 2 are built). `docs/RECORDING-EDITOR.md` is the piano-roll editor for
  recordings, and what the first recording showed.
- `docs/SERVICE-PLAN.md` is the piano service, `sepianod`: a daemon on the
  Raspberry Pi hidden in the piano, with a web UI and later an iPhone app.
  Phase 1 is built and installed on the Pi. **The piano's owner decides what
  to build next and in what order.** Check its "Open decisions" before
  starting large work.
- `api/openapi.yaml` is the service's API contract; keep it in step with
  `api/server.go`.

## Working with the owner

The piano's owner directs this project and is not a programmer. Explain
in plain words, and say what they should see or hear at the piano.

- **Where the work goes.** Push to the owner's repository,
  `toddatlas/sepiano` (`origin`), on `main`. `brbkr/sepiano` (`upstream`)
  belongs to Jeff Brubaker, who started the project. Pull from it only to
  bring in his changes. brbkr/sepiano#1 offers him the Phase 1 work; it is
  a side channel. Commit as `toddatlas` with
  `121987941+toddatlas@users.noreply.github.com`, so no real email enters
  the public history.
- **Save progress at every step.** The owner asked for this: "Throughout
  the process I need you to be saving your progress". After each tested
  change:
  - commit and push;
  - update this file's status section and the docs in `docs/`.

  The working copy may be in a temporary folder. GitHub and these docs
  are the lasting record.
- **The order of work**, as the owner set it (2026-11-23):
  1. Playback, albums and play lists. Done and confirmed at the piano.
  2. The suite's other programs. Unpacked and decoded. Recording, the
     Key check and Tools are built. One short test recording was made
     (2026-11-23); formal tests at the piano wait until it can make noise.
  3. The piano's own settings. The read-only Piano tab and software
     voicing are built.

     **Deferred by the owner:**
     - writing trims to the piano (`putlut`);
     - setting calibration values by hand (the calibrator sometimes fails
       on a note, and that stops the whole run).

     First the owner wants to learn from recordings which keys record at
     the wrong velocity.
  4. The recording editor (asked for 2026-11-23, while the formal tests
     wait). The piano roll is built (docs/RECORDING-EDITOR.md). The
     detailed work is done on a computer, so every screen also has a
     wide-screen layout. Per-key recording corrections are built too (the
     owner chose them next, 2026-11-23).
  5. Later: an iPhone app.

  Finish and verify one stage before starting the next. Ask before
  starting large work.
- **The piano is a Steinway**: a model B, "Steinway B-SE" in its table.
  Prefer Steinway renderings.
- **At the piano.** Don't play, record or run tools on the real piano
  without asking; the owner tries new features there. Writing the EEPROM
  needs the owner's explicit OK and a verified backup (see Hard rules).

## How it works, in one paragraph

`play.exe` is not really a player. It uploads a Z80 program (Stahnke's) into
the piano's controller, then acts as a terminal (channel 1: text out, keys in)
and a file server (channel 2: the Z80 asks for blocks of `.kb/.lp/.sp`).
`sehost` does the same. It reads that Z80 program from the user's own copy of
`play.exe` (`host.LoadProgram`) and never ships it.

## Layout

| Package | Role |
|---------|------|
| `se/` | `.kb/.lp/.sp` codec (byte-exact on the whole corpus), scrambler, SE ⇄ MIDI |
| `smf/` | minimal MIDI file reader/writer |
| `link/` | frames, CRC, ARQ link, clock-paced pump, simulated cable (`NewPipe`) |
| `host/` | Z80 program extraction, session, console keys, file service |
| `sim/` | simulated instrument: real link protocol, player/recorder behaviour, 800 Hz clock |
| `deck/` | the instrument controller: one goroutine owns the link, keys ⇄ console confirmations |
| `library/` | performance store: files + `.json` metadata, in-memory index, upload/zip/MIDI/export |
| `api/` | REST + SSE (`openapi.yaml`), program installs, touch icon |
| `web/` | the PWA: plain HTML/CSS/JS, no build step, embedded in `sepianod` |
| `z80/` | Z80 decoder and tracer, for studying the suite's programs (and later the firmware) |
| `lut/` | the instrument's EEPROM lookup table: header, settings, sections (`selut` prints and compares) |
| `edit/` | a performance as notes and pedal curves for the piano-roll editor, and back; "play from here" |
| `cmd/` | `sedump`, `se2mid`, `mid2se`, `sehost`, `sesniff`, `sepianod`, `sesuite` (survey of the suite's programs and the firmware), `selut` (lookup tables), `semon` (memory dumps through the monitor) |
| `deploy/` | systemd unit, udev rule, avahi file, `install.sh` (on the Pi), `deploy.sh` (from here) |

## Commands

```bash
go vet ./... && go test ./...      # all tests; link tests take about 25 s, deck about 13 s
gofmt -l .                         # must print nothing
GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o dist/ ./cmd/...   # for the Pi
go run ./cmd/sepianod -simulate -addr 127.0.0.1:8080 -data ./devdata             # service + UI, no piano
deploy/deploy.sh                   # build, copy and install sepianod on the Pi (ssh alias "pi")
```

- On Windows, Git Bash's `ssh` can't use keys held by the Windows ssh-agent.
  Use Windows' OpenSSH: `SSH=/c/Windows/System32/OpenSSH/ssh.exe
  SCP=/c/Windows/System32/OpenSSH/scp.exe deploy/deploy.sh`.
- On the owner's Windows PC, Go is in `C:\Program Files\Go\bin`. A shell
  started before Go was installed may need it added to `PATH`.
- Git for Windows defaults to `core.autocrlf=true`; `.gitattributes` keeps
  checkouts LF so `gofmt -l .` stays clean.

- Corpus tests read the recordings from the parent folder or `$SE_CORPUS`, and
  skip when they aren't there.
- The `play.exe` extraction test needs `SE_SUITE=<folder with play.exe>`.
- `go build` needs `-buildvcs=false` only outside a git checkout.

## The hardware

- **Raspberry Pi 4**, Raspberry Pi OS Bookworm, arm64. Go is **not** installed
  on it: cross-compile, then `scp` the binaries over. By convention:
  - binaries in `~/sepiano-bin/`;
  - `play.exe`, `record.exe` and songs in `~/sepiano-data/`.
- **SSH:** each developer adds their own key and a host alias (e.g. `pi`) in
  `~/.ssh/config`. The home network's DNS can't be changed; use the IP in the
  alias, or mDNS (`<hostname>.local`).
- **Two USB serial adapters** are on the Pi. Check
  `ls -l /dev/serial/by-id/`, because the `ttyUSB` numbers depend on plug-in
  order:
  - **Prolific PL2303 → the piano**, wired directly. It has been `/dev/ttyUSB1`.
  - **Keyspan USA-19HS → the old XP ThinkPad**, through a null-modem adapter,
    used to capture the original `play.exe`. It has been `/dev/ttyUSB0`.
- **The piano talks at 57600 baud.** It is silent until a host sends a
  handshake.

### The service on the Pi

- `sepianod` runs as the `sepiano` user (systemd unit `sepianod`), serves
  `http://raspberrypi.local/` on port 80, and keeps an idle link to the piano
  on `/dev/sepiano-piano` (a udev symlink to the PL2303).
- Data: `/var/lib/sepiano/{library,programs,staging,settings.json}`.
  `play.exe`, `record.exe`, `getlut.exe` and the eight tools (anykey,
  anyham, midiin, sustain, adjust, cal, silent, setup; installed
  2026-11-23) are in `programs/`. All 24 suite
  programs are in `~/sepiano-data/suite/` for `sehost`.
- **It owns the serial port.** Before using `sehost` by hand:
  `sudo systemctl stop sepianod`; afterwards `sudo systemctl start sepianod`.
- Logs: `journalctl -u sepianod -f`. Status: `curl -s localhost/api/v1/status`.
- Add songs: `sepianod import DIR` (uploads to the running service; `-server
  http://raspberrypi.local` from another machine), then `sepianod tag DIR` to
  apply the album `.bat` catalogs (`-n` first for a dry run). Library metadata
  is re-derived from file names and `.kb` headers when `library.metaVersion`
  goes up; fields set by hand or by `tag` are `locked` and kept. Backup of the
  pre-v2 metadata: `/var/lib/sepiano-meta-backup-20261123.tar.gz`.
- Single pieces can also go through the web UI's Upload screen.
- `sepianod tag -write-bats DIR` also checks that every folder has a batch
  file playing all of it, and writes `play_folder.bat` (first line
  `rem … Written by sepianod tag.`) where none does; it never touches other
  files, and rewrites only its own. The suite's album batch files
  (`_FP1002.BAT` …) convert (`kls2bit`, `bit2wav`); they never played.
- Albums and play lists play as one joined performance (`library.Join`, files
  in `/var/lib/sepiano/play/`); renderings are chosen Steinway first
  (`library.Rank`). Play lists live in `/var/lib/sepiano/playlists.json`.

### Working with the piano

1. **Check the link first** with `sehost -probe -port <dev>`. It does a
   handshake only and uploads nothing. If no bytes come back, it's a cabling
   or power problem, not a protocol one: try `sehost -lines`, then check the
   adapter.
2. **Play:** `sehost -exe play.exe -port <dev> -trace -capture runN SONG`, run
   from `~/sepiano-data`. Use tmux or `ssh -t` when someone needs the player's
   keys: Space stop/resume, Enter quit, Esc restart, Up/Down level.
3. **You cannot see or hear the piano.** Ask the person at the piano what
   happened, and say what they should expect before playing.
4. `10Sec-C9-400` is Zenph's near-silent lead-in file. Use a real piece (e.g.
   a Rachmaninoff file) to check that keys move.

## Hard rules

- **Never commit copyrighted material:**
  - Stahnke's programs (`*.exe`, extracted Z80 images);
  - the recordings (`.kb/.lp/.sp`, `.mid`);
  - EEPROM tables (`.lut`, from `getlut`);
  - memory and firmware dumps (`semon`);
  - **full session captures** (`sehost -capture` `.tx` files contain the
    uploaded Z80 program).

  `.gitignore` blocks most of these. The repository is public. `link/testdata/`
  holds only small link-frame captures; check new test data with `strings`
  before adding it.
- Keep `se` byte-exact: `go test ./se` re-encodes every corpus file and must
  stay identical.
- Protocol changes must keep passing the real-capture tests in
  `link/capture_test.go`, which replay real `play.exe` and piano traffic.
- **The piano's EEPROM holds its calibration and settings** (docs/SUITE-PROGRAMS.md).
  Never run a program that writes it (`setup`, `cal`, `silent`, `putlut`,
  `put*`, the monitor's `P`) without the owner's explicit OK and a verified
  `getlut` backup. Never write a table whose first word isn't `3017H`:
  `play.exe` would then overwrite the table's first 256 bytes.

## Facts that are easy to get wrong

- SE velocity is an **inverse** scale: bigger means softer. It's 10 bits.
- `.lp` is the damper ("loud") pedal, sampled every 10 ms. `.sp` is the soft
  pedal, sampled every 20 ms. A `.kb` tick is 1.25 ms.
- **The piano's firmware does not use the PC's handshake logic.** It echoes
  each handshake frame and interleaves IDLE frames. `link` only treats 16
  non-handshake frames in a row as "peer is up" (`quietUp`).
- The piano's frames, idle ones included, carry its 800 Hz playback clock in
  `ts`, which gives a live position.
- The player's level only goes from 0 (as recorded) down. It can't go louder.
- The suite never creates its `HKLM\Software\SE Program Suite` registry key,
  so the original programs rescan the ports every launch.

## Status (end of the first working session)

- Verified on the real piano from the Pi: the handshake, the upload, the file
  service, and a full 3:49 piece. The piano's clock ran at 799.99 of 800
  ticks/s, file replies took ≤ 4 ms, there were no link errors, and the pacing
  sounded right by ear.
- Not yet tried: the player keys over the link, `record.exe`, re-uploading on
  a link that's still up, and piano power cycles. That's Phase 0 in
  `docs/SERVICE-PLAN.md`, which needs someone at the piano.

## Status (end of the second session: the service)

- `sepianod` Phase 1 is built: deck, simulator, library, API, PWA, recording,
  deploy. All of it is tested against `sim/` (`go test ./...`).
- Installed on the Pi. The idle link to the real piano comes up and stays
  clean (no retransmits or NAKs); `play.exe` is installed. The library holds
  the songs from `~/sepiano-data` and the owner's collection from their
  Desktop (uploaded with `sepianod import`).
- **Confirmed on the real piano through the service:** full pieces play from
  the phone UI; Enter → "QUIT PLAYING"; Space → "STOP PLAYING AT TIME=3912"
  (hundredths of a second); Down → "LEVEL=-1" … "LEVEL=-16", and −16 is the
  floor (Down there prints nothing).
- **Not yet checked at the piano:** resume and restart text, recording
  (`record.exe` is not installed; it's in the suite installer on the owner's
  Desktop), piano power cycles.
- Lesson: a serial port opens only once. Close a connection fully before
  opening the next (`deck.release`), and close the port when a link dies.
  `sim.Instrument.Dial` enforces this so tests catch it.

## Status (third session, 2026-11-23)

- Library metadata from `.kb` headers, album `.bat` catalogs, `NAMES.TXT`
  and better Zenph-name parsing; 1,738 pieces from the owner's Desktop
  (`C:\Users\todd\OneDrive\Desktop`, which is in OneDrive).
- Albums, play lists, continuous play of a whole album or play list,
  previous/next, Steinway renderings first. On the Pi: 36 albums, including
  "Bach: The Goldberg Variations (Glenn Gould, 1955)" (32 tracks, all
  `Steinway_SED519960`), and 33 play lists (the owner's `play…bat` files
  and one whole-folder list per folder without an album).
- 56 `play_folder.bat` files written to the Desktop folders; 57 folders now
  play whole from a batch file. Broken references reported, not changed:
  `SE\Hamb\playTatum.bat` (1 missing file), `20200831\playall.bat` (2).
- **Confirmed on the real piano (2026-11-23):** the joined Goldberg album
  played continuously from 17:16:27 to 17:19:16, through the Aria into
  Variations 1 and 2 with no stop or reload; **Next** works on the hardware
  (tap to playing in about 3.5 s: stop, reconnect, upload). The owner
  listened and approved ("Looking good").
- The app shows each piece's file name (search results, tracks, Now
  Playing) so the owner can check what plays; search also lists matching
  albums and folders (play lists), whose pages offer "Play all".
- Next, as the owner asked: unpack the suite's other programs
  (`se_program_suite_v0.95_setup.exe` on the Desktop is an NSIS installer:
  needs 7-Zip or an extractor), then find what can be read from the
  instrument itself (its stored playback settings and calibration tables),
  aiming to edit them.

## Status (fourth part, 2026-11-23: the suite's other programs)

- The suite installer is unpacked (7-Zip). `sesuite` and `z80/` trace each
  program's Z80 code. Findings are in `docs/SUITE-PROGRAMS.md`. In short:
  - every program is the same Windows carrier;
  - all hardware access goes through a firmware jump table, which each
    program copies from the address at `0001H`;
  - calibration and settings live in the piano's EEPROM, read and written
    a byte at a time through `FW#42`/`FW#43`.
- `record.exe` is installed in the service (V30.44); recording is still
  untested at the piano. All 24 programs are on the Pi in
  `~/sepiano-data/suite/`.
- **EEPROM backed up (2026-11-23, with the owner's OK):** `getlut`
  through `sehost`. The service was stopped for a few seconds and nothing
  moved. The 2048-byte image is in `~/sepiano-data/backups/eeprom-20261123.lut`
  on the Pi and in the owner's `Desktop\SE\EEPROM backups\`.
- What it holds:
  - **"Steinway B-SE"** (a model B), table V30.17, serial 08;
  - calibrated throughout, with the calibration range set to E1 only;
  - E1 and C#3 at the maximum (255) in section 2.

  `selut` decodes it; the sections are mapped in `docs/SUITE-PROGRAMS.md`.
- **Firmware read (2026-11-23, with the owner's OK):** `semon` typed only
  `D` and `Q` into the monitor; page zero and A000H–FFFFH were dumped in
  29 s, and the monitor area matched its image exactly. The firmware is
  "STAHNKE BOOTSTRAP LOADER, V43.47" (Multibus, 2009), with its jump table
  at B82AH. Dumps: `~/sepiano-data/firmware/` on the Pi and the owner's
  `Desktop\SE\Firmware dumps\`.
- From the firmware:
  - the EEPROM is behind ports 35H, 36H and 37H;
  - FW#43 skips bytes that are already right;
  - the bytes at offsets 4 and 5 are the key range;
  - FW#71 interpolates sections 1 and 2 between section 0's velocity
    breakpoints (section 1 is the lead time);
  - section 9 holds per-key velocity trims (G3 +7 on this piano).

  `lut.Table.Lookup` and `selut -key` reproduce the lookup.
- Decoded further (same day):
  - the hardware map (sensors, actuators, pedals, timer, two serial
    channels, EEPROM);
  - each key's actuator has a 2-bit mode (strike 01, hold 11) and an
    8-bit value;
  - `play.exe` schedules each note as events from FW#71: section 2 is the
    strike drive;
  - section 11 holds the player's constants (800 = the clock). This
    piano's one change there is word 32, 224 → 160.
- Decoded from `cal`, `silent` and `play.exe` (same day). Every section
  now has a meaning:
  - section 1 is the measured strike-to-sensor time;
  - section 2 is the strike level (smaller is harder);
  - section 4 is the silent-press level;
  - sections 5 and 6 are the release target times and levels;
  - section 7 is the free release time;
  - section 8 (FF) lets a key go;
  - section 10 also holds the calibrator's limits;
  - most of section 11 is decoded; velocity 640 or more is a silent note.

  Worth telling the owner: E1 and C#3 are at the gentlest strike for soft
  notes, and A#0 is a slow-returning key.
- **The Piano tab (read only) is live** (commit b30d8d7): backups through the
  deck (`RunUtility` runs `getlut`; state `maintenance`), stored in
  `/var/lib/sepiano/eeprom/` (the 20:37 backup imported). It shows notes, an
  88-key strip, a key sheet and the player's constants, and compares any two
  backups. `getlut.exe` is installed in the service. A backup takes about
  1.5 s.
- The owner's decisions for the editor are in `docs/SETTINGS-EDITOR.md`:
  software voicing first, per-key loudness, no PIN, calibration overrides
  wanted.
- **Software voicing (step 2) is built:** per-key trims and a whole-piano
  setting, tried by ear. They are applied to each piece as it plays
  (`api/voicing.go`, `piano/voicing.go`), with test notes per key and
  Replay; nothing is written to the piano. Next: step 3, writing trims to
  the piano (the owner deferred it, to learn from recordings first).
- **Recording now follows `record.exe`'s code** (docs/SUITE-PROGRAMS.md,
  "The recorder"). The old parser took "DEPRESS ENTER TO END RECORDING"
  for the start, but the recorder prints all three prompts at arm time;
  "START RECORDING" is the start. Esc discards and re-arms; "ILLEGAL
  RECORDING TYPE OR MODE" (no type or serial in section 12) is shown as
  the reason. The header text is the title and date (`deck.recordingHeader`).
  `sim` records exactly as the real one does. Still untried at the piano.
- **Velocity by key** (`piano.CheckKeys`, `GET /pieces/{id}/keys`, linked
  from the Record and Piece screens) shows keys that record louder or
  softer than their neighbours. The recorder has no per-key correction.
- **Tools** (`api/tools.go`, Piano tab → Tools): anykey, anyham, midiin,
  sustain, adjust, cal, silent and setup run as interactive programs
  (`deck.StartTool`, keys through `POST /tools/key`). Writing tools (cal,
  silent, setup) need a confirmation and back up the EEPROM first. None
  has been run on the real piano.

## Status (fifth part, 2026-11-23: the recording editor)

- The owner made one short test recording. It seemed reliable, but
  releases seemed out of sync. Formal tests wait until the piano can make
  noise. What the Key check found in it:
  - G3 let go 305 and 568 ms before the rest of its chords;
  - two presses have velocity 0 (no hammer reading), which the player
    plays as silent presses (`play.exe` 1044H; corrected 2026-11-24, it
    was first described here as the hardest strike).
- **Key check** (was "Velocity by key"): velocity against the neighbours,
  presses with no hammer reading, and release timing in chords
  (`piano.CheckReleases`).
- **Piano-roll editor** (`edit/`, `api/editor.go`, `web/editor.js`):
  - it edits notes (time, release, key, loudness, add, delete), the
    damper and the soft pedal, with undo;
  - "Play from cursor" plays the edited version without saving;
  - saving makes an edited copy, and an original is never changed.

  The SE format has no release speed; the piano's table sets it.
- **Wide screens**: at 1000 px or more the tab bar becomes a side bar. The
  editor uses the whole width, with its selection panel beside the roll
  from 1280 px. From a laptop: `http://raspberrypi.local/` in a browser.
- Checked in the simulator with the owner's test recording: select, drag,
  release shift by key, pedal drawing, undo and redo, save as copy, play
  from cursor, and phone and laptop layouts. Not yet tried at the piano.
- **Per-key recording corrections** (`piano/corrections.go`,
  `api/corrections.go`, Record → Corrections; docs/RECORDING-EDITOR.md):
  - each key gets a loudness (% hammer speed) and a release (ms) correction;
  - "fill in" gives a velocity-0 press the loudness of the notes around it;
  - they are applied as recordings made here play (alone, in collections,
    and in the editor's preview); the files never change;
  - "Save a corrected copy" and the editor's "Apply corrections" put them
    into a copy marked `corrected`, which is never corrected twice;
  - a recording's Key check suggests them, with checkboxes.

  Checked in the simulator with the test recording: G3 velocity 361 → 234,
  its release 400 ms later, and the two velocity-0 notes → 273. Nothing
  is set on the Pi yet: with an empty list nothing changes.

## Status (overnight build, 2026-11-24: every program usable)

The owner asked for an unattended overnight build: every suite program
usable from the app, then more editing tools.

- **All 24 programs are usable.** The service runs play, record and
  getlut; Piano → Tools runs the other 21, grouped (docs/SUITE-PROGRAMS.md,
  "Run any program from the app"). New:
  - Restore a backup (putlut). It checks the table first, backs up before
    and after, then reads the piano back and compares (`checkRestore`,
    `afterTool`). The Backups screen has a Restore button on each backup.
  - The five factory tables, which need "ERASE" typed.
  - playmidi, with a piece's original MIDI or an upload; playeseq, with an
    uploaded file.
  - Stahnke's editor (edit), on a copy of a piece; a changed result
    becomes an edited copy.
  - dump, load and tape; a tape load joins the library.
  - The monitor.
- Every tool that changes settings now also takes an "after-tool" backup,
  so before and after can be compared.
- `deck.StartTool` returns a channel that reports when the tool ends.
  `sim` imitates the installers and the tape loader.
