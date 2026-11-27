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
- `docs/ROADMAP.md` is the next stage (2026-11-26): the owner's six ideas,
  researched, with a suggested order and a to-do list. Tick items off
  there as they are done.

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
- **The piano is a 1909 Steinway D** (a 9-foot concert grand), retrofitted
  with a Stahnke SE system. Its table calls it "Steinway B-SE": that is
  the label of Stahnke's prototype table for Steinways (`putb`), which
  this piano's table started from, not its model (owner, 2026-11-26). A D
  behaves quite differently from a 7-foot B, so:
  - the calibration values are this piano's own;
  - Stahnke's factory "Steinway B" table is only a starting point, never
    a reset;
  - Steinway renderings are still the best match. Prefer them.
- **It is a vintage acoustic piano, not a MIDI instrument.** Never send
  sudden full pedal moves: sources with an on/off sustain pedal are
  smoothed at playback (docs/LIBRARIES.md, "Pedals").

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
- **A note louder than its key can strike stops the player** ("PROGRAM
  ERROR AT ADDRESS BEDA": the firmware's FW#17 refuses a strike level of 0).
  MIDI sources have such notes; every note sent is kept within the table's
  limits (`piano.StrikeLimits`, docs/SUITE-PROGRAMS.md).
- **Never drop the link while the piano is taking a program.** Cut off
  mid-upload, the real piano took no program at all until it was switched
  off and on (2026-11-24; docs/SE-SERIAL-PROTOCOL.md, the last section).
  Let a program start, then end it with its own key (Enter for the player).
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
  - **"Steinway B-SE"**, table V30.17, serial 08. That is the label of
    Stahnke's Steinway prototype table; the piano is a 1909 Steinway D;
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
- **Editing tools** (`web/edittools.js`, commit 6c88a01): time ranges
  (play, select, copy, delete, crop, stretch, pedals over them), insert
  silence, paste (mixed in or into a gap; across pieces), chords started
  and let go together, loudness ramps and evening out, lengths, legato,
  note names.
- **Stahnke's editor decoded** (docs/STAHNKE-EDITOR.md, by a helper; the
  key facts were checked by hand):
  - its LOUDER, SOFTER and LEVEL write the trims, so the tool is now gated
    like setup;
  - **key 0 is the sostenuto pedal** (0x80 down, 0x00 up). Joining, the
    editor, corrections and previews had dropped it; `KB.Playable` keeps
    it. MIDI carries it as CC66;
  - **velocity 0 is a silent press**, not the hardest strike: corrected in
    the docs and the app;
  - header byte 1 is read (a velocity factor).
- **Sensor test** (Record → Sensor test): two guided recordings, each
  opening its Key check. Several recordings can be checked together
  (`/keycheck?pieces=`). The Key check has **press timing** too, and
  corrections a per-key **press** shift.
- Later the same night:
  - **sostenuto lane and cue marks** in the editor. The editor now moves
    every event that is not a note (`others`) with its time edits; before,
    the server put them back at their old times;
  - **"Show the original"** on an edited copy;
  - **select by** keys and velocity;
  - **Versions** on a piece's page (`edited_from`), and the Library's
    "All kinds" filter;
  - the Status page installs any of the 24 programs;
  - silent presses (0, or 640 and more) stay silent in software voicing
    and in corrections, and export to MIDI as velocity 1;
  - the morning report: docs/OVERNIGHT-2026-11-24.md.
- An 18-minute piece (8,298 notes) loads in about 20 ms and draws in
  4–8 ms a frame. Performance numbers from a hidden browser pane are
  throttled to about 1 frame per second; measure the draw itself instead.
- **A code review of the night's work** found ten defects; all are
  fixed, with tests:
  - `api.CheckIdentity` (install and every load) refuses a program in
    the wrong slot. The factory installers are told apart by the model
    name in their image; `TestCheckIdentitySuite` checks all 24 with
    `SE_SUITE`.
  - Restore checks the model against every backup read from the piano
    (`checkModel`), not only the latest.
  - A writing tool needs a whole 2048-byte backup before it starts.
  - Corrections and voicing cap audible notes at 639. A copy of a
    corrected piece stays `corrected`.
  - In the editor:
    - the loudness tools leave silent presses alone;
    - a save in flight keeps later edits unsaved;
    - time edits snap to the pedals' 16-tick grid, so the pedals can't
      drift;
    - previews pair the sostenuto.
- Also: the Restore screen lists what a chosen backup would change from
  the newest backup, and the editor has a gradual tempo change over a
  range (`tempoRamp`: ritardando, accelerando).

## Status (2026-11-24, morning: the piano stuck after quick previews)

- While using the editor, the owner started previews in quick succession.
  One arrived about a second into the previous player's upload, and the
  deck dropped the link mid-upload. After that the piano took no program
  (each load stayed at "loading" with no text) until it was switched off
  and on.
- Fixed in `deck`: the link is never dropped before the program has
  printed (`abortWhenLoaded`), and a player stopped while it loads is quit
  with Enter at "START PLAYING" (`quitOnStart`). `sim.WedgeOnDrop` imitates
  the stuck piano, and `TestNoDropWhileLoading` covers it.
- Not known yet: whether dropping a *running* program (the recorder's
  discard, a tool's stop, the deck's fallback when a stop is not confirmed)
  also leaves the piano stuck. Those still drop the link, but now only
  after the program has started.

## Status (2026-11-24, later: tidying the library, the recording timer)

- The owner asked for a way to clear away test recordings, preferring
  that nothing is ever deleted. Pieces can now be **hidden**:
  - `Piece.Hidden` is set with PATCH, and hidden pieces are left out of
    `GET /pieces` unless `?hidden=only|all`;
  - Library → **Recordings** lists the recordings made here and their
    copies, newest first, with tick boxes to hide or show several;
  - the Library's Pieces list has "Newest first" (`updated_at`, else
    `added_at`) and a "Hidden (N)" filter;
  - a piece's page has Hide / Show it again. Delete for good is still
    there but needs DELETE typed (`confirmDialog`'s `phrase`).
- **Recording timer:** it stopped at 0:03 on the real piano. The app
  moves its clock at most 3 s past the last position, and the deck took
  the position from the piano's clock, which gave nothing usable while
  recording. The deck now times a recording by the Pi's clock from
  "START RECORDING" (`recordingStarted`, `recordingPosition`).

## Status (2026-11-26: planning the next stage)

- The owner set out six ideas (key sensors, volume, a bigger editor,
  content libraries, audio to SE, a schematic for the designer). Fine-
  tuning accuracy waits for a piano technician. Researched and written up
  in `docs/ROADMAP.md`, with a suggested order.
- Found from the firmware: the key sensors' thresholds are set in the
  sensor-scanner hardware. Nothing in software or the EEPROM can change
  them (docs/SUITE-PROGRAMS.md, "The sensor scanner").
- The app's level presses the player's Down/Up keys; the player scales
  each note's velocity by section 11's factors. Moving the volume into the
  app is planned (ROADMAP, idea 2).

## Status (2026-11-26, later: volume on the Pi, a bigger editor, grades)

The owner approved building three roadmap items in one go:
- **Volume on the Pi** (ROADMAP idea 2). The app's level (0 to −16)
  never presses the player's Down/Up keys now. `deck.openPerf` sends the
  `.kb` through `se.KB.Reader`, a lazy encoder (byte-identical to
  `Encode` on all 350 corpus files), which asks `deck.Subdue` for each
  note's velocity as the piano reads it:
  - 6% slower hammers per step;
  - no softer than 400 (this piano's softest calibrated breakpoint)
    unless recorded softer;
  - silent presses untouched.

  A change reaches every note not yet sent: a few seconds, as the player
  reads ahead. `Config.PlayerLevel` keeps the old way;
  `Status.LevelOnPi` tells the app. Not yet heard at the piano.
- **Editor size** (idea 3): Comfortable / Large / Extra large scales the
  canvas, with pointer positions scaled back. The toolbar and panel
  follow by CSS zoom. The lanes keep their on-screen size. Fit keys (K)
  and Full screen (F).
- **Grades** (idea 4): `library.Grade` is native, performance or
  sequenced, found from the data (`grade.go`: notes starting together,
  loudness levels, a grid of gaps, pedal depth). On the owner's 37
  Disklavier MIDI files: 33 performances, 4 "ISI … BMP100" files
  sequenced, which is right. A Library "Sources" filter, and a label on
  converted pieces. `metaVersion` 8: converted MIDI files are titled from
  their names, not "Converted from …".
- Flaky under load: `TestTracksFollowThePosition` and `TestNoisyTransfer`
  fail when every package runs at once alongside the simulator. They
  pass alone.
- **Both volume methods** (the owner wants to compare them before
  dropping the piano's own). Settings.LevelMethod is "pi" (default) or
  "piano". `deck.SetLevelMethod` switches at once when idle, or from the
  next piece (`applyMethod` runs at the start and end of each piece;
  `Status.LevelMethodPending`). The toggle "Volume set by: On the Pi |
  Piano's own" is on the volume card and in Settings.
- **Editor zoom buttons**: Time (in/out) and Notes (taller/shorter rows)
  in the toolbar. Each lane has Taller/Shorter at its top right
  (`laneZoom`, 1 to 4 times).
- Added to the roadmap: play lists you build from search (idea 7), and
  full backups to the desktop or Google Drive (idea 8).

## Status (2026-11-26, later: backups)

- **Backups** (ROADMAP idea 8; docs/BACKUPS.md):
  - `backup.Write` zips the whole data folder with a manifest of
    SHA-256s. It leaves out `play/`, `work/` and temporary files.
  - `GET /api/v1/backup` downloads it; `/backup/status` says when the
    last one was taken and by what (`backup-state.json`).
  - `POST /backup/restore` adds back only what is missing. Pieces go
    through `library.RestorePiece`, keeping their `.json` details, and
    play lists through `MergePlaylists`. Settings backups and programs
    are added only when absent. `settings.json`, `voicing.json` and
    `corrections.json` are restored only if absent.
  - Status → Backups has the buttons and a warning after 7 days.
- `deploy/windows/piano-backup.ps1` (tried against the simulator) and
  `Install-PianoBackupTask.ps1` (a nightly "Piano backup" task).
  Installed on the owner's PC on 2026-11-26: 3 a.m., into
  `G:\My Drive\Piano backups` (Google Drive), and it runs on battery.
  Its script lives in `%USERPROFILE%\sepiano`. Files this app writes
  under AppData land in a packaged-app private copy that Task Scheduler
  cannot see; the first try failed that way.
- The API tests now lay out their folders as sepianod does (one data
  folder), so a backup in a test holds everything.

## Status (2026-11-26, later: play lists you build)

- **Play lists you build** (ROADMAP idea 7):
  - Library → Play lists has "New play list" (`newPlaylist`);
  - Pieces has a **Select** chip, which ticks rows (search results
    included) and shows a bar with "Add to play list…";
  - a piece's page has "Add to play list". `addToPlaylist` opens a chooser
    (`pickDialog`): your lists first, then "New play list…";
  - `POST /playlists/{id}/pieces` (`library.AddToPlaylist`) appends,
    each piece once;
  - a list's page has Rename and Edit order (up, down, remove; saved with
    PUT), and "Your play list" or "Imported from …";
  - search already finds play lists, and now labels yours.
- `confirmDialog` takes `input` (asks for a line of text, resolving to it
  or null).
- The backup button is now "Back up now".

## Status (2026-11-26, later: content libraries)

- The owner's other session downloaded MAESTRO v3, Vienna 4x22 and Batik
  to `C:\Users\todd\sepiano\datasets` (never commit any of it) and handed
  the work here. docs/LIBRARIES.md covers it:
  - `sepianod datasets DOWNLOADS` imports all three: Vienna native,
    Batik with `se.BosendorferCurve` (grade `se-midi`), MAESTRO with the
    Disklavier curve. Each piece records collection, captured_on,
    licence and curve. The Library has an "All collections" filter;
  - `TestBosendorferCurveOnVienna` (`SE_VIENNA`): 99.6% of notes, median
    velocity error 1.2%;
  - per-collection **fits** (`piano.Fit`, `/api/v1/fits`, Piano → Fit by
    collection): loudness % and pedal depth %, applied at playback with
    the corrections (`playFix`, `markFixes`; `Status.Fitted`).
- The owner confirmed (2026-11-26) that the piano is a **1909 Steinway D**.
  "Steinway B-SE" is the prototype table's label.
- **Imported into the Pi (2026-11-26):** all 1,422 pieces (Vienna 110,
  Batik 36, MAESTRO 1,276), none failed, as 27 new albums. The library is
  now 3,170 pieces: 1,858 native, 1,276 performance, 36 se-midi. The data
  folder is 176 MB, up from 29 MB, so nightly backups grow to match.
- Batch 2 (SUPRA rolls, ASAP, PiJAMA) is downloaded and waits for the
  owner's choice (docs/ROADMAP.md, idea 4).

## Status (2026-11-26, later: the 1909 D, safe pedals, backups every 3 days)

- The owner corrected the model: a **1909 Steinway D**. The docs and the
  Tools text now say so; "Steinway B-SE" is the label of Stahnke's
  prototype table (`putb`), which this piano's table started from.
- **Pedal speed limits** (docs/LIBRARIES.md, "Pedals"): the owner asked
  that on/off sustain pedals (MIDI sources) never slam the vintage
  mechanism. `piano.SafePedals` runs on every piece as it plays (in
  `playFix`, so albums and play lists too, and in editor previews): at
  most 60 per sample, 32 for an on/off pedal, centred on the recorded
  moment (`se.Pedal.Limit`). The owner's 350 native files never exceed
  60 and play unchanged. Stahnke's MIDI players (Tools) are not covered;
  their descriptions say so.
- **Backups every 3 days, keeping 3** (the owner's choice): the "Piano
  backup" task was reinstalled that way on the owner's PC.
- The PC backup now downloads to the PC's own temporary folder, then
  moves the file into Google Drive, with Windows PowerShell's slow
  progress display off. The 130 MB backup had taken 11 minutes straight
  into Drive, close to the old 15-minute limit (now an hour; the task's
  limit is 2 hours).

## Status (2026-11-26, later: content libraries, batch 2)

- `sepianod datasets` (`cmd/sepianod/datasets2.go`) imports SUPRA (456
  Welte-Mignon rolls, grade `roll`), ASAP (514 Piano-e-Competition
  performances not in MAESTRO) and PiJAMA (2,777 jazz transcriptions,
  grade `transcribed`), all with the Disklavier curve; docs/LIBRARIES.md
  has the table, albums and quirks. New grades are in `library/grade.go`,
  the app's `GRADES` and `openapi.yaml`.
- Tried in the simulator (6 of each): grades, titles and lengths right
  (the rolls' tempo maps are followed); on/off pedals (PiJAMA's damper,
  both SUPRA pedals) come out at 32 per step, ASAP's at 60.
- Batch 3 (ATEPP, Aria-MIDI, about 44,000 transcriptions) is downloaded
  by the other session and waits for the owner (docs/ROADMAP.md).
- Batch 2 imported into the Pi (2026-11-26): 3,746 pieces added (ASAP
  lists one Beethoven recording twice), none failed. The library is 6,916
  pieces.

## Status (2026-11-26, evening: favorites, seeking, Up next, names)

The owner asked for several improvements at once and said to build what
needs no further decision:
- **Favorites** (`library/favorites.go`, `api/favorites.go`): stars on
  pieces, albums and play lists; "Favorites first" and "Best first"
  (`qualityTier`) in the app; a Favorites section. Zenph's Steinway
  files start starred (`zenphSteinway`, `metaVersion` 10). The owner's
  files came in by upload, which version 9 missed.
- **Seeking** (`api/seek.go`): the Now Playing bar is a slider;
  `startQueue` takes a start point and trims the first piece with
  `edit.Doc.From` (as "Play from cursor"); tracks carry `from_ms`.
- **Up next** (`api/queue.go`): `deck.OnPerformanceEnd` fires only when
  the player reports "END OF PERFORMANCE" and nothing else was chosen;
  the queue lives in memory.
- **Editor**: on a computer the side bar shrinks to icons. Full screen
  (`editor-focus`) puts the title and a one-row toolbar together.
- **Competitors' names and prizes**: `cmd/sepianod/ecomp.go` and
  `-details`; tables in `datasets/downloads/ecomp/`. Applied on the Pi:
  every MAESTRO piece named, 426 awards, 88 1st-prize pieces starred.
- **Holiday collection**: `sepianod holiday`; tried in the simulator
  (141 pieces, 4 play lists; a second run adds nothing).
- Lesson: in perl `s|…|…|` an escaped `\|` in the pattern becomes
  "or". It matched the empty string and wrote text at the top of files
  three times this session. Use the Edit tool for code edits.
- Installed on the Pi (290bbed): 393 pieces starred (305 Zenph Steinway
  files, 88 1st-prize performances). The holiday collection was imported
  (115 new, 26 already there and starred; 4 starred play lists): 7,031
  pieces. A backup is now 251.5 MB and took 47 s into Google Drive.

## Status (2026-11-26, night: the competition's original files)

- The working copy moved to `C:\Users\todd\sepiano\progress\sepiano` (the
  owner's choice). The app's move copied no `.git`, so git was restored
  in place: `git init`, the two remotes, `git reset origin/main`.
- The owner chose the Piano-e-Competition originals next. `sepianod
  datasets -only ecomp` (`cmd/sepianod/ecomporig.go`) imports the 1,279
  MAESTRO lacks: 170 h, named from the competition's pages, 136 with
  prizes, 40 starred. `maestroHas` finds performances MAESTRO holds cut
  or joined differently: 16-note stretches with the same timing within
  15 ms. Tried in the simulator (all convert).
- Imported into the Pi the same night: all 1,279 added, none failed. The
  library is 8,310 pieces (795 hours); 581 starred, 569 with a prize.
  ASAP's names were refreshed with the spelling fixes (`-details`).

## Status (2026-11-26, night: recording details)

- The owner asked for recordings to be catalogued, the pianist defaulting
  to the owner. The Record screen asks title, composer, pianist, album or
  series, notes and a star (docs/RECORDING-EDITOR.md, "Recording
  details"); `api/recording.go` keeps them from arm (and `PUT
  /record/details` while recording) to the save. `SaveRecording` (now the
  deck's `OnRecorded`) adds collection, captured on, this piano's
  instrument and the next album track. The default pianist is the setting
  `recording_performer` (Status → Settings). It lives on the Pi, not in the
  code: the repository is public.
- Tried in the simulator: a take armed with details, its title changed
  while waiting, saved with all of them; the form kept pianist and album.
- Flaky under load too: `link.TestFlowControl` (passed 2 of 3 alone).
- Installed on the Pi (6c25b28); "Pianist for recordings" set there to the
  owner's name. The 10 earlier recordings (test takes, the two sensor-test
  takes and one edited copy) were given the owner as pianist and "Recorded
  on this piano" too.

## Status (2026-11-26, night: how hard a piece works the piano)

- `piano.MeasureWork` (`piano/work.go`): keys held at once (average, peak,
  held together 1 s and 10 s), longest hold, sustained holding, and the
  holding a full pedal makes unnecessary. `WorkLevel` rates it against the
  owner's 350 Zenph files (light, moderate, heavy, very heavy). Shown as
  "Workload" on each piece's page (`GET /pieces/{id}/work`); `sepianod work
  FILES` measures SE or MIDI files outside the library.
- Measured across sources (docs/LIBRARIES.md, "How hard a piece works the
  piano"). Rolls and transcriptions hold about twice as much as the Zenph
  files. Of the 11 holiday organ rolls, 3 are very heavy (O Holy Night, O
  Little Town of Bethlehem, The First Noel).
- The owner decided to leave all 11 holiday organ rolls out (2026-11-27);
  `sepianod holiday` already skips them.

## Status (2026-11-27: the player stopped; Up next stalled; history)

- **The piano stopped mid-piece** with "PROGRAM ERROR AT ADDRESS BEDA". A
  MAESTRO note louder than its key can strike (E1 at velocity 21; E1's
  limit is 25) made FW#71 ask FW#17 for strike level 0, which the firmware
  refuses (docs/SUITE-PROGRAMS.md). About 400 MAESTRO pieces and many other
  MIDI pieces have such notes; the Zenph files never do.
- **Fixed:**
  - `piano.StrikeLimits` (from the newest table backup, with a step of
    margin) is applied to every note in `deck.openPerf`, which now serves
    every piece whatever the volume method;
  - the deck recognises the message (`evProgramError`), shows it, and
    Up next goes on;
  - `sim.Instrument.StrikeLimit` imitates the crash;
  - `TestStrikeLimitsAndProgramError` covers both.
- **Up next:** Next skips to its first item when there is no next track,
  and each item has a play button (`POST /queue/{n}/play`).
- **Play history** (`api/history.go`, `history.json`, in backups): an
  entry each time a piece starts (a seek adds none). The History page
  (button at the top of Now Playing) has play, remove and Clear.
- Tried in the simulator: Next into Up next, an item played now, History
  listing, removing.

## Status (2026-11-27, later: gentle endings; history by album)

- The owner heard the sustain pedal come up hard at the end of a MAESTRO
  piece. Many MIDI recordings drop a pedal in one step as the recording
  stops, or end with a pedal still down, which the piano lets go at once.
  `piano.GentleEnding` (in `SafePedals`) limits the release from just
  before the last note to a full stroke in 1.5 s and never leaves a pedal
  down, making the piece longer as needed (docs/LIBRARIES.md, "A gentle
  ending"). It changes only endings with a jump no foot makes (more than
  60 per sample) or a pedal left down; the Zenph endings stay as made.
  `api.correct` now takes the length from the adjusted piece.
- **History by album:** consecutive plays of one album or play list share
  a group (`historyEntry.Group`, within 3 h); the History page shows one
  row with "N tracks played" and a + that lists every track, ticked if
  played, each with a play-from-here button; `DELETE
  /history/{id}?group=1` removes a group.
- **Editor, easier to grab** (docs/RECORDING-EDITOR.md, "Easier to grab"):
  notes catch the pointer in a band at least 14 px tall (`HIT_ROW`, the
  nearest row wins) and 6 px past their ends; length handles up to 10 px;
  grips drawn on the note under the pointer (`hoverHit`); stalks caught
  within 10 px; taller lanes (88/60/60/24); a wider panel with larger text.
  Tried in the simulator: a 15 px band on 10 px rows, and an end drag.
- Installed on the Pi (667155b), 2026-11-27 12:35.

## Status (2026-11-27, later: the key-sensor rail investigation)

- The owner gave an engineering brief (key-sensor rail: document,
  reconstruct, plan resistor work; no software or hardware changes). The
  work lives **outside this repository** in
  `C:\Users\todd\sepiano\hardware\se-key-sensors\` (private photos and
  correspondence; README there). Summary in docs/ROADMAP.md, idea 6.
- HEIC photos and HEVC video: Windows' decoders refuse desktop programs on the
  owner's PC. FFmpeg 9.1 was installed with the owner's approval (winget
  `Gyan.FFmpeg`); `ffmpeg -i X.HEIC -frames:v 1 -update 1 -q:v 2 X.jpg`.
- Second pass done: every chip on the rail identified (none programmable),
  Wayne's predictions confirmed, the feedback layout seen in the copper. Two
  first-pass readings were corrected (10-contact connectors, not 5; part
  number SSB-, not SSD-).
