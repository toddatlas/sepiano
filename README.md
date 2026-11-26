# sepiano

Open-source tools for the Stahnke SE reproducing piano system. The goal is to
replace the Windows "SE Program Suite" (`play.exe` and friends) with a Go
program that runs on a Raspberry Pi, and later a network service an iPhone
app can drive.

* `docs/SE-FILE-FORMAT.md` — the `.kb/.lp/.sp` performance files, fully
  decoded
* `docs/SE-SERIAL-PROTOCOL.md` — the PC ⇄ instrument link, program upload,
  console and file service, plus why USB adapters misbehave
* `docs/SERVICE-PLAN.md` — the piano service (library, playback,
  recording, web UI, later an iPhone app), with its open decisions
* `docs/SUITE-PROGRAMS.md` — the suite's other programs, the firmware and
  the piano's EEPROM table, decoded; `docs/STAHNKE-EDITOR.md` — Stahnke's
  own performance editor, decoded
* `docs/SETTINGS-EDITOR.md` — editing the piano's settings from the app
* `docs/RECORDING-EDITOR.md` — the piano-roll editor, the Key check and the
  per-key recording corrections
* `docs/OVERNIGHT-2026-11-24.md` — what the overnight build added, and what
  to try at the piano
* `docs/LIBRARIES.md` — importing free collections (MAESTRO, Vienna 4x22,
  Batik), their sources and curves, and fitting them to this piano
* `docs/BACKUPS.md` — backing up everything on the Pi, every 3 days from the PC
  (for example into Google Drive), and restoring
* `docs/ROADMAP.md` — the next stage: the owner's ideas researched, with
  priorities and a to-do list
* `CLAUDE.md` — working notes for Claude Code sessions: hardware setup, rules,
  and pitfalls

## The big finding

`play.exe` is not really a player. It **uploads a Z80 program into the
piano's controller**, then acts as a terminal and file server for it. The Z80
program parses the song files and drives the piano. A replacement host only
has to speak the link protocol, upload that same Z80 program (taken from your
own copy of `play.exe`), and serve files. That is what `sehost` does.

## Status

| Piece | State |
|-------|-------|
| `.kb/.lp/.sp` decoding and encoding (`se`) | **Verified**: all 349 + 698 files re-encode byte-identically. Notes match independent MIDI renderings exactly. |
| SE ⇄ MIDI conversion (`se2mid`, `mid2se`) | Works. The velocity curve is empirical (fitted to Zenph's paired renderings). |
| Link protocol (`link`) | **Works with the real instrument** (57600 baud). Byte-identical to the original host's frames. Regression tests replay real captures from both `play.exe` and the piano. |
| Host (`sehost`) | **Plays on the real piano** from a Raspberry Pi 4 through a PL2303 USB adapter. Rachmaninoff's Prelude in C♯ minor (3:49) ran with the piano's clock at 799.99 of 800 ticks/s, file replies within 4 ms and no link errors, and sounded right. The player's keys (stop, quit, level) were confirmed later through the service. |
| Sniffer (`sesniff`) | Decodes captures and live taps, and finds the baud rate. Used on real `play.exe` traffic. |
| Piano service (`sepianod`) | **Installed on the Pi and playing on the real piano**: the library, albums and play lists played continuously, play/pause/stop/next, level, recording (as `record.exe` does it), upload/download. It runs as a web app on a phone or, with a side bar, on a computer (`http://raspberrypi.local/`). See `docs/SERVICE-PLAN.md`. |
| The piano's settings | A read-only Piano tab with backups (`getlut`), comparing and **restoring** them; software voicing tried by ear. `docs/SETTINGS-EDITOR.md`. |
| Every suite program | All 24 usable from the app: the service runs play, record and getlut, and the Tools run the other 21 as consoles. Anything that writes the piano's settings is confirmed and backed up before and after. |
| Recording tools | A piano-roll editor (notes, timing, loudness, damper, soft and sostenuto pedals, time ranges, copy and paste, chords, compare with the original), the Key check (velocity, press and release timing by key), a guided sensor test, and per-key corrections applied when recordings play. `docs/RECORDING-EDITOR.md`. |

## Tools

```bash
cd sepiano
go build -o bin/ ./cmd/...        # or: GOOS=linux GOARCH=arm64 go build -o bin-pi/ ./cmd/...
```

| Command | Purpose |
|---------|---------|
| `sedump [-pedals] song` | list notes and pedal moves as text |
| `se2mid song ...` | SE → Standard MIDI File (800 ticks/s, CC64 damper, CC67 soft) |
| `mid2se -o SONG in.mid` | MIDI → SE files the player can play |
| `sehost -exe play.exe -port /dev/ttyUSB0 song` | play on the instrument (replaces `play.exe`) |
| `sehost -list` | list serial ports |
| `sesniff -pc a.bin -piano b.bin` / `-tap-pc P1 -tap-piano P2` | decode traffic |
| `sepianod` | the piano service: API and web UI on port 80 (`-simulate` runs it without a piano) |
| `sepianod import DIR` | add the `.kb/.lp/.sp` sets in DIR to a running service |
| `sepianod tag DIR` | fill in albums, tracks and titles from the album `.bat` catalogs and name lists in DIR (`-n` for a dry run) |

`sehost` needs a suite executable to take the Z80 program from. It is
installed on the XP machine. You can also unpack
`se_program_suite_v0.95_setup.exe` with 7-Zip (`7z x …`). Other suite programs
run the same way: `-exe record.exe` records through the same file service.
Add `-trace` to log frames, and `-capture run1` to save raw traffic for
`sesniff`.

While playing: **Space** stop/resume, **Enter** quit, **Esc** restart,
**Up/Down** restore/subdue level, **Ctrl-C** abort.

## On the Raspberry Pi

```bash
sehost -probe -port /dev/ttyUSB1          # handshake only, nothing uploaded
sehost -lines -port /dev/ttyUSB1          # modem status lines, for cabling checks
sehost -exe play.exe -port /dev/ttyUSB1 -trace -capture run1 song   # play
```

Use the stable `/dev/serial/by-id/...` names when several adapters are
plugged in: the `ttyUSB` numbers depend on plug-in order. The adapter goes
straight to the piano's cable; a null-modem adapter is only needed between
two computers.

## Next steps on real hardware

1. **Baud rate**: done. The instrument uses 57600. (The XP machine has no
   `HKLM\Software\SE Program Suite` key: the programs only update that key and
   never create it, so they auto-scan on each launch and keep no record.)
2. **Optional: capture a ThinkPad session.** Tap the cable between the ThinkPad and
   the piano: two USB-serial adapters, receive pins on each TXD line, grounds
   joined. Start `sesniff -tap-pc … -tap-piano … -dump cap/` *before*
   launching `play` on the ThinkPad. It cycles 57600/28800 on the
   instrument's line until frames with valid CRCs appear, then prints the
   rate. This checks the handshake, the CRC and the timing against the real
   firmware, and `cap/pc-ch0.bin` is the exact program image. If the
   instrument is silent until the PC connects, the first moments may be
   missed; rerun with `-baud` set to the printed rate.
3. **Keys and recording**: try the keys (Space, Enter, Esc, arrows) in an
   interactive session (`ssh -t` or tmux), and try `record.exe`. A PL2303 USB adapter
   works. The Pi's built-in UART (`/dev/serial0`) through a **MAX3232 RS-232
   level shifter** would remove USB entirely. Never wire the Pi's 3.3 V pins
   straight to the RS-232 cable.
4. The piano service in `docs/SERVICE-PLAN.md` is built: a daemon on the Pi
   with a song library, playback, volume and recording controls, and a web UI
   that a phone can use (`http://raspberrypi.local/`), ahead of a native
   iPhone app. Install or update it with `deploy/deploy.sh`. Try it without
   the piano with `go run ./cmd/sepianod -simulate -addr 127.0.0.1:8080 -data ./devdata`.

## Layout

```
se/      file formats, MIDI conversion       link/    frames, ARQ link, pacing pump
smf/     tiny MIDI file reader/writer        host/    Z80 upload, file service, console
sim/     simulated instrument                deck/    instrument controller (sepianod)
library/ performance store (sepianod)        api/     REST + SSE, openapi.yaml
web/     phone web app (no build step)       deploy/  systemd, udev, avahi, install scripts
cmd/     sedump se2mid mid2se sehost sesniff sepianod             docs/  specifications
```

`go test ./...` runs the unit tests. The corpus tests read the recordings in
the parent directory (or `$SE_CORPUS`). The `play.exe` extraction test needs
`SE_SUITE=<dir with play.exe>`.

## Copyright

The Z80 programs and the Windows suite are © Wayne Stahnke. The recordings
are © Zenph and others. This repository contains neither: `sehost` reads the
Z80 program from your copy at run time. Keep the recordings and the suite out
of any public repository.
