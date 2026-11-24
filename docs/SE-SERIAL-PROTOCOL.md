# STAHNKE SE/PC serial protocol

How the PC programs of the *SE Program Suite v0.95* (`play.exe`, `record.exe`,
… © 2005 Wayne Stahnke) talk to the SE instrument. It was reverse engineered
from `play.exe` (V50.45) with Ghidra. All 24 suite programs share the same
host code.

## Architecture: the PC is a terminal and file server

The Windows programs do almost nothing themselves. Each one embeds a **Z80
program** (the "player", "recorder", "calibrator", …). It uploads that
program into the instrument's Z80 controller over the serial link. After
that, the PC only provides:

* a **console**: it shows text the Z80 prints and sends it the keystrokes;
* a **file server**: the Z80 asks for blocks of the `.kb/.lp/.sp` files as it
  needs them, and asks to write them when recording.

All timing, file parsing and piano control happens on the Z80. That program
reaches the hardware (link I/O, actuators, sensors, the EEPROM) only through
the resident firmware's jump table. It first copies a block of 3-byte
entries from the address held at 0x0001 to just past itself (the player's
copy is at 0x2D10–0x2F58, so its calls land in 0x2D10–0x2EB7), and calls
through that copy. The firmware itself is not in any file here.
`monitor.exe` uploads a Z80 monitor at 0xA000 that can read it out. See
`SUITE-PROGRAMS.md` for every program, the firmware entries and the
EEPROM.

```
 PC (play.exe)                         SE instrument (Z80)
 ───────────────                       ─────────────────────────────
 link layer  <── serial 8N1 ──>  link layer (resident firmware)
   ch0  ── program image ──>          boot loader -> runs program
   ch1  <── console text ───          player prints prompts
        ── keystrokes ────>           Space/Enter/Esc/arrows
   ch2  <── file requests ──          "read 1 KB of file 0" ...
        ── file data ──────>
```

## Physical layer

| Setting | Value |
|---------|-------|
| Format | 8 data bits, no parity, 1 stop bit |
| Baud | **57600** or **28800** (non-standard) |
| Flow control | none (no CTS/DSR, no XON/XOFF) |
| Modem lines | DTR and RTS raised on open; RTS then DTR dropped on close |
| Buffers | `SetupComm(1024, 1024)`, overlapped I/O, reads return at once |

Port and baud can be kept in `HKLM\Software\SE Program Suite`, values `Port`
(COM number) and `Baudrate`. If they are missing or don't work, the host tries
COM1…COM8 at 57600, then 28800, on each port. A port and baud are accepted if
the link handshake completes within **250 ms**. The result is written back
only if the key already exists: the programs call `RegOpenKeyEx`, never
`RegCreateKeyEx`. Without the key (as on the XP ThinkPad here), they rescan
on every launch and record nothing. A capture of the line is then the way to
learn the rate: `sesniff` cycles the candidate rates on a tap until frames
with valid CRCs appear. It trusts the instrument's line, because during the
scan the PC sends SYNC2 at 57600 on its own line even when the instrument
uses 28800.

The host runs at `HIGH_PRIORITY_CLASS` and allows one instance at a time (mutex
`SEUtilityMutex`).

## Frames

```
FF FF FF 7F   type  ack  seq  chan  count  data[count]  ts_lo ts_hi  crc0 crc1 crc2 crc3
└─preamble┘   └──────────── covered by CRC-32 ──────────────────┘   └── little-endian ──┘
```

* `count` ≤ 32. Frames with a larger count are discarded.
* CRC is standard CRC-32/IEEE (zlib `crc32`) over `type` … `ts_hi`, sent
  low byte first.
* `ts` is a 16-bit timestamp. The PC always sends 0, and ignores the value it
  receives (it computes a difference and never uses it). The instrument sends
  its **800 Hz performance clock** (1.25 ms ticks, wrapping at 65536). On the
  real piano it advanced 9,464 ticks = 11.83 s between "START PLAYING" and
  "END OF PERFORMANCE". It reads 0 before playback. It counts up from about
  −194 while the key help prints and passes 0 about 0.2 s after "START
  PLAYING". It stops at the end.
* There is no byte stuffing: the receiver finds the preamble, then counts
  bytes. A bad CRC drops the frame and the receiver hunts for the next
  preamble.
* Between frames, a sender with nothing queued sends `00` filler. In practice
  the line is never quiet, because the link layer always has a frame ready
  (idle frames at worst).

### Frame types

| type | name | fields used | meaning |
|------|------|-------------|---------|
| `00` | IDLE | — | keep-alive |
| `01` | SYNC1 | — | handshake: receiver goes to state 0 |
| `02` | SYNC2 | — | handshake: receiver goes to state 1 |
| `03` | SYNC3 | — | handshake: receiver goes to state 2 (up) |
| `04` | PURGE | chan | channel purge acknowledgement |
| `08` | NAK | ack | "frame `ack` is missing: resend from it" |
| `09` | ACK | ack | "received everything before `ack`" |
| `0A` | DATA+ACK | ack, seq, chan, data | data frame with a piggy-backed ACK |
| `0B` | DATA | seq, chan, data | data frame |
| other | — | — | protocol error: the host stops ("Communicator Error") |

### Handshake

Each side's state is −1 (unsynchronised), 1, 0 or 2 (up). While not up it
keeps sending the frame for its state:

| own state | sends | on receiving SYNC1 / SYNC2 / SYNC3 go to |
|-----------|-------|------------------------------------------|
| −1 | SYNC2 | 0 / 1 / 2 |
| 1  | SYNC1 | 0 / 1 / 2 |
| 0  | SYNC3 | 0 / 1 / 2 |

When both sides start at −1, both go −1 → 1 → 0 → 2. The host's sequence
numbers start at 0 when the program starts and are never reset. Since
re-running `play.exe` works, the instrument presumably resets its own on
handshake (inferred, not seen in code).

**The instrument does not follow that table** (observed on the real piano,
`link/testdata/piano-handshake-57600.bin`). It **echoes** each handshake
frame it receives: SYNC2 → SYNC2, SYNC1 → SYNC1, SYNC3 → SYNC3. It alternates
every handshake frame with an IDLE frame, and counts itself up once it has
received SYNC3. Against the host's table this still works: the host sends
SYNC2 and gets SYNC2 back (→ 1), sends SYNC1 and gets SYNC1 (→ 0), sends
SYNC3 and gets SYNC3 (→ up). If the instrument is already up from an
earlier session it sends IDLE frames, and an incoming SYNC2 restarts its
handshake. Unlike the PC, it transmits frames back to back with no `00`
filler.

### Reliable delivery

* Data frames carry an 8-bit `seq`, incremented per new frame. Comparisons are
  signed 8-bit differences.
* The receiver delivers a frame only if `seq` = next expected. If `seq` is
  ahead (a frame was lost) it sends a **NAK** carrying the expected number.
  Duplicates are dropped, but ACKed again.
* ACKs ride on the next outgoing data frame (type `0A`) or go out alone as
  type `09`.
* **Window**: at most 8 frames in flight. With 8 unacknowledged frames the
  sender goes back to the oldest and resends them all in order (go-back-N).
* On a NAK the sender drops frames before `ack` (now implicitly
  acknowledged) and resends from `ack`.
* With nothing new to send but frames unacknowledged, the sender keeps
  re-offering the **oldest unacknowledged frame**. A capture therefore shows
  many repeats; this is normal.

### Channels and flow control

Three channels share the link. A data frame carries bytes of one channel; the
sender fills frames from channel 0 first, then 1, then 2.

| ch | PC → Z80 | Z80 → PC |
|----|----------|----------|
| 0 | the program image | ESC (`0x1B`, bit 7 ignored) ends the session |
| 1 | keystrokes | console text; ESC ends the session |
| 2 | file-service replies | file-service requests |

Each side buffers 4096 received bytes per channel. When a channel holds more
than 2729 (`0xAA9`) unread bytes, the receiver sends a **flow frame**: a data
frame (reliable, sequenced) with `chan = FF`, `count = 3`, one byte per
channel, `FF` = stop and `00` = go. When the backlog falls below 2047
(`0x7FF`) it sends another one. Outgoing data waits in a 256-byte buffer per
channel.

A PURGE (type 4, `chan` = n) empties both sides' buffers for channel n. The
host empties outstanding frames for n and sends PURGE until the peer
answers with PURGE. `play.exe` never uses it.

### Timers in the PC host

* Every time Windows signals `EV_TXEMPTY` (the UART transmitter emptied), the
  host generates **16 bytes** and writes them. This is its only pacing.
* If no bytes arrive for **750 ms**, or a write is not finished after
  **1 s**, the host gives up with "Communicator Error".

### Confirmed on the real host (2026-11-22)

A Raspberry Pi with a Keyspan USA-19HS, null-modem-cabled to the XP ThinkPad,
recorded `play.exe` scanning with no instrument attached
(`link/testdata/playexe-scan-read-at-57600.bin`):

* 91 SYNC2 frames at 57600 (≈ 250 ms), then 45 at 28800 (≈ 250 ms). The
  28800 part reads as a repeating 19-byte pattern at 57600. A final frame
  went out at 57600, probably the UART FIFO draining after the port's
  previous settings were restored on close.
* Every frame is byte-identical to `sepiano`'s encoding
  (`ff ff ff 7f 02 00 00 00 00 00 00 57 ce f3 0a`): preamble, field order,
  zero timestamp, CRC-32/IEEE little-endian.
* Each refill is one frame plus one `00` filler byte: the 16-byte
  `EV_TXEMPTY` chunks.

### Confirmed with the instrument (2026-11-22)

`sehost` on a Raspberry Pi 4, with a Prolific PL2303 USB adapter wired
directly to the piano, played `10Sec-C9-400` from start to finish using the
Z80 program from `play.exe` V50.45:

* The instrument runs at **57600 baud**.
* Handshake as described above; the upload of 255 frames (8,131 bytes) took
  2.15 s. The instrument ACKed every frame and sent **no NAKs**. No frame was
  corrupted, apart from the first one, cut off by opening the port
  mid-frame.
* File service exactly as specified. The player reads `.kb` in 1 KB
  requests, `.lp` in 512 B and `.sp` in **256 B** requests. It opens and
  reads all three once for the header, then reopens to play.
* The console showed the banner, the copyright line, "LEVEL=0 (NORMAL)", the
  key help, "START PLAYING", and 11.8 s later "END OF PERFORMANCE", followed
  by ESC on channel 1.
* The instrument re-offers its oldest unacknowledged frame, just like the
  PC, so duplicates show up in traces; the link drops them.
* A full performance, Rachmaninoff's Prelude in C♯ minor (3:49), played
  through. The piano's clock, fitted against the Pi's wall clock, ran at
  799.99 ticks/s and never paused. The 26 file reads were answered in 1 ms
  (median), 4 ms at worst. There were no NAKs, and no corrupted frames
  beyond the one cut off at port open. Listening at the piano, the pacing
  sounded right.

## Program upload (channel 0)

Right after the handshake the host writes the whole image to channel 0. The
image begins with a 16-byte load header for the boot loader:

```
00 03 | start (LE16) | end (LE16) | entry (LE16) | 00 00 00 00 00 00 00 00 | bytes for start..end
```

The header itself lands at start−16. The program text usually begins with
`CR LF <banner> CR LF 7F`. Upload images in the suite:

| program | banner | load | entry | bytes |
|---------|--------|------|-------|-------|
| play.exe | PERFORMANCE PLAYER V50.45 | 0100–20B2 | 0153 | 8131 |
| record.exe | PERFORMANCE RECORDER V30.44 | 0100–11D9 | 0155 | 4330 |
| playmidi.exe | MIDI FILE PLAYER V50.45 | 0100–2033 | 0151 | 8004 |
| midiin.exe | MIDI INPUT CONTROLLER V50.48 | 0100–221D | 0156 | 8494 |
| edit.exe | INTERACTIVE PERFORMANCE EDITOR V50.45 | 0100–6C0D | 015F | 27422 |
| monitor.exe | Z80 MONITOR V50.44 | A000–AF3C | A046 | 3917 |
| cal.exe, getlut.exe, setup.exe, put*.exe, … | calibration and lookup-table tools: see `SUITE-PROGRAMS.md` | 0100–… | | |

At 57600 baud the 8 KB player takes about 2.1 s to upload. The link carries
about 3.8 KB/s of payload: 32 data bytes per 47-byte frame.

## Console (channel 1)

The Z80 sends plain text with CR LF line endings. Keystrokes go the other way
as the Windows console delivers them: printable keys, Enter (`0D`), Esc (`1B`)
and Space as ASCII; special keys as `00` + PC scan code:

| key | code | key | code |
|-----|------|-----|------|
| Up | `00 48` | Home | `00 47` |
| Down | `00 50` | End | `00 4F` |
| Left | `00 4B` | PgUp | `00 49` |
| Right | `00 4D` | PgDn | `00 51` |
| Ins | `00 52` | Del | `00 53` |
| F1–F10 | `00 3B` … `00 44` | | |

The player understands: **Space** stop/resume (it shows "STOP PLAYING AT
TIME=…"), **Enter** quit, **Esc** restart, **Down/Up** subdue/restore the level
("LEVEL=-n").

## File service (channel 2)

The Z80 sends a request code and arguments. The PC answers with the same code,
then any result. Files are named relative to the path given on the command
line (directory, base name, extension). All counts are little-endian and at
most 0x2000.

| code | request (Z80 → PC) | reply (PC → Z80) |
|------|--------------------|------------------|
| `0B` | name… `7F` | `0B` (sent *before* the name is read) — sets the base name |
| `0C` | ext… `7F` | `0C` — sets the extension |
| `0D` | — | `0D` name `7F` (name must be ≤ 8 chars, else fatal) |
| `0E` | — | `0E` ext `7F` (≤ 3 chars) |
| `0F` | — | `0F` second command-line argument (`\` → CR LF) `7F` |
| `10` | — | `10` — opens *name*.kb, .lp, .sp as files 0, 1, 2 (missing file = fatal) |
| `11` | file, n | `11` file, k, data[k] — reads up to n bytes (k = 0 at end of file) |
| `12` | — | `12` — creates *name*.kb$, .lp$, .sp$ |
| `13` | file, n, data[n] | `13` file, n — writes |
| `14` | — | `14` — closes the output files |
| `15` | — | `15` — closes all, renames .kb$→.kb, .lp$→.lp, .sp$→.sp |
| `16` | — | `16` — deletes the .$ files |
| `38` | — | `38` — opens *name.ext* for reading (file 3) |
| `39` | n | `39` k data[k] |
| `3A` | — | `3A` — creates *name*.$$$ |
| `3B` | n data[n] | `3B` n |
| `3C` | — | `3C` — closes *name*.$$$ |
| `3D` | — | `3D` — replaces *name.ext* with *name*.$$$ |
| `3E` | — | `3E` — deletes *name*.$$$ |
| other | | fatal ("Illegal file request") |

Fatal errors make the host print the message and exit.

## A `play` session

1. Open the port, raise DTR/RTS, handshake (≤ 250 ms).
2. Upload the 8131-byte player on channel 0 (≈ 2 s).
3. The player prints its banner (ch 1). It checks that the instrument's
   lookup table (in EEPROM) is version 30.17; if it isn't, it overwrites the
   table's first 256 bytes and quits (`SUITE-PROGRAMS.md`).
4. `10` open, then `11 00 00 04` (1 KB of `.kb`). It prints the header text
   (the copyright line), then reopens with `10` to start over.
5. It prints the key help and "START PLAYING", then plays. As its buffers
   drain it sends `11 00 00 04` (key stream, 1 KB), `11 01 00 02` (damper,
   512 B) and `11 02 00 01` (soft pedal, 256 B). Look-ahead is large: 2000
   bytes of key events,
   800 damper samples (8 s) and 400 soft samples (8 s).
6. Keys arrive on channel 1.
7. "END OF PERFORMANCE", then the Z80 sends ESC on channel 1. The host exits
   and the Z80 returns to its resident firmware.

## Why a USB adapter could start playback and then stop

This is an analysis, not confirmed on the failing laptop. A USB adapter
itself is not the problem: a PL2303 on Linux with `sepiano`'s clock pacing
ran a session without a single error. The host's design assumes a real
16550 UART:

1. **Pacing through `EV_TXEMPTY`.** On a UART this event fires when the
   16-byte FIFO drains, so the host is never more than about 3 ms ahead of
   the wire. USB serial drivers report it in their own ways. Some report it
   as soon as data enters the driver's buffer, so the host stuffs kilobytes
   into the adapter, and every ACK and file reply queues behind up to a
   second of filler. Others report it late or not at all, and the host stops
   transmitting. The upload survives either way: window stalls only slow it,
   so playback starts. The steady two-way traffic during play is what breaks.
2. **Strict watchdogs.** 750 ms without received bytes, or one write not
   completing within 1 s, ends the session. USB latency spikes and deep
   adapter buffers can trip either one.
3. **28800 baud** is non-standard. Some adapter and driver combinations
   approximate it badly.

What the laptop showed at the moment it stopped would narrow this down: a
"Communicator Error", the "COMM ERROR: … TX QUEUE: n RX QUEUE: n" line, or a
silent hang.

## Differences in `sepiano`

`sepiano/link` and `sepiano/host` mirror all of the above, with three
deliberate changes:

* **Pacing by the clock.** Writes are paced at 97% of the line rate with at
  most 32 bytes queued, whatever the adapter reports. Better still, on a
  Raspberry Pi the native UART (`/dev/ttyAMA0`/`serial0`) with an RS-232
  level shifter avoids USB altogether.
* **Handshake hardening.** The handshake only advances when a side receives
  the frame for its *current* state. A side that handles two transitions
  before transmitting can skip a state, and its peer then waits for ever. The
  original mostly avoids this by handling one frame per loop. `sepiano`
  always emits a frame for each state it passes through. If it waits in
  state 0 through 16 consecutive frames with no handshake frame among them,
  it treats the peer as already up. (An earlier version needed just one IDLE
  frame. The real instrument interleaves IDLE with its handshake frames, so
  that came up too early; tests now replay the instrument's own capture.)
  Tests also run it against a deliberately adversarial copy of the original
  logic (`Link.Strict`) and an echoing model of the firmware.
* A SYNC frame arriving after the link is up (the instrument restarted) ends
  the session with an error instead of stalling.
