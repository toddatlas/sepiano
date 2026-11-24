# The SE Program Suite's programs

What each program of *SE Program Suite v0.95* does, how it talks to the
instrument, and what it can change. Found by tracing the Z80 program each
one uploads (`cmd/sesuite`, `z80/`), from the owner's copy of
`se_program_suite_v0.95_setup.exe` (an NSIS 2.25 installer; 7-Zip unpacks
it). Only `play.exe`, `getlut.exe` (one read-only backup) and
`monitor.exe` (dump commands only, through `semon`) have been run on the
piano.

`sesuite FOLDER` prints the survey; `sesuite -dis NAME FOLDER` a listing.
Keep listings, images and the programs out of the repository: they are
Wayne Stahnke's copyrighted work.

## Every program works the same way

- The Windows side is identical in all of them: upload a Z80 program on
  channel 0, then be a console (channel 1) and a file server (channel 2).
  `sehost` already does both for any of them, and `host` implements every
  file request they use (`0B`–`16`, `38`–`3E`).
- **No program touches an I/O port.** There is no `IN` or `OUT` in any
  traced code: the hardware (actuators, sensors, pedals, MIDI, tape, the
  EEPROM) is reached only through the resident firmware.
- **The firmware interface is a jump table.** Each program starts by copying
  a block of the firmware, whose address it reads from `0001H` (the operand
  of the jump at `0000H`), to just past itself, and calls the firmware
  through that copy, as 3-byte entries. The block is 555 bytes (185 entries)
  in most programs and 585 bytes (195 entries) in the performance programs
  (`play`, `playmidi`, `playeseq`, `midiin`, `edit`, `dump`, `load`, `tape`).
  So the "firmware entry points at 2D10H–2EB7H" are `play.exe`'s copy; the
  same entry has a different address in each program. `sesuite` names them
  `FW#n`, the same in every program.

## The programs

Risk: **R** reads only and moves nothing; **M** moves the piano or reads its
sensors but changes nothing stored; **W** writes the instrument's EEPROM
(its lookup table: calibration and settings).

| Program | Banner | Size | What it does | Files | Risk |
|---------|--------|-----:|--------------|-------|------|
| play | PERFORMANCE PLAYER V50.45 | 8115 | Plays `.kb/.lp/.sp`. In use by `sepianod`. **See the version check below.** | reads a performance | M (W) |
| record | PERFORMANCE RECORDER V30.44 | 4314 | Records from the key and pedal sensors | writes a performance (`.kb$`… then commit) | M |
| playmidi | MIDI FILE PLAYER V50.45 | 7988 | Plays a standard MIDI file | reads `name.ext` | M |
| playeseq | PIANOSOFT DISK PLAYER V50.45 | 7503 | Plays Yamaha PianoSoft (E-SEQ) files | reads `name.ext` | M |
| midiin | MIDI INPUT CONTROLLER V50.48 (2007) | 8478 | By its name, plays MIDI arriving at the instrument's MIDI input, live; Down/Up subdue/restore | none | M |
| edit | INTERACTIVE PERFORMANCE EDITOR V50.45 | 27406 | A command-line editor of a performance "workspace": heading, preface, search, splice, louder/softer, mask, mute, uniform, sostenuto, voicing, cue marks. Only 14% of it is traced (its commands dispatch through tables), so whether it plays is not known yet | reads and writes a performance | M? |
| dump | PERFORMANCE DUMPER V50.44 | 3168 | Sends a performance out to a tape recorder | reads a performance | M |
| load | PERFORMANCE LOADER V50.44 | 3503 | Reads a performance from tape into files | writes a performance | M |
| tape | MAGNETIC TAPE RECORD/PLAY V50.45 | 7946 | Records or plays through the tape interface directly | none | M |
| adjust | MANUAL ADJUSTMENT PROCEDURE V50.44 | 1131 | Steps through the notes for a technician (Space start/stop, Esc next note) | none | M |
| anykey | ANY KEY TEST V50.44 | 539 | Key sensor test (by its name; it checks the keyboard connectors) | none | M |
| anyham | ANY HAMMER TEST V50.44 | 542 | Hammer sensor test (by its name) | none | M |
| sustain | SUSTAIN PROGRAM V50.44 | 752 | Raises and lowers the dampers (Space) | none | M |
| getlut | LOOKUP TABLE RETRIEVER V50.44 | 971 | **Copies the whole EEPROM table to a PC file.** The backup tool | writes `name.ext` | **R** |
| monitor | Z80 MONITOR V50.44 | 3901 | A machine-code monitor at A000H (commands below) | none | R / W |
| setup | LOOKUP TABLE SETUP V50.44 | 2374 | Edits six settings in the table (below) | none | W |
| cal | ADAPTIVE CALIBRATOR V50.44 | 2664 | Plays each note, measures it, stores its actuate and release levels | none | W |
| silent | SILENT NOTE CALIBRATOR V50.44 | 1465 | Calibrates each note's "silent actuate" level | none | W |
| putlut | LOOKUP TABLE INSTALLER V50.44 | 1010 | **Writes a whole table from a PC file** (the restore tool) | reads `name.ext` | W |
| put225, put275, put290, putb, putc7 | LOOKUP TABLE INSTALLER V50.44 | 2595 | Each writes a built-in "prototype" table V30.17: Bösendorfer 225SE, 275SE, 290SE, Steinway B-SE, Yamaha C-7SE. **Erases the piano's calibration.** | none | W |

`uninstall.exe` is the installer's own; it holds no Z80 program. Version
"V50.44" is the programs' release; "V30.17" is the table format.

`dump`, `load` and `tape` need a cassette recorder on the instrument's tape
interface; they are of historical interest only.

## The instrument's EEPROM ("lookup table")

The piano keeps its calibration and settings in a 2048-byte EEPROM. The
firmware reaches it through I/O ports (see "The firmware" below), and every
program goes through these firmware entries. They are confirmed in the
firmware itself:

| Entry | Does |
|-------|------|
| FW#42 | A ← EEPROM[HL]; an address of 0800H or more is a fatal error |
| FW#43 | EEPROM[HL] ← A; carry set when it worked. **It writes nothing when the byte already holds A**; otherwise it writes, then polls until the chip reads back A or a timer runs out |
| FW#44 | HL ← 0800H, the EEPROM's size |
| FW#47, #48, #49, #50 | HL ← 0, 2, 4, 5: where the version, the length, the bottom note and the top note are |
| FW#51 … FW#64 | HL ← the offset of section 0 … 13, read from the directory (FW#51 + n gives section n) |
| FW#65 | HL ← 22H, where the model name is |
| FW#66 | loads and checks the table: notes, key count and section offsets into RAM (D503H–D522H); a bad key range is fatal |
| FW#67, #68, #69 | A ← the bottom note, the top note, the number of keys |
| FW#70 | A ← the key index of note A (fatal outside the keyboard) |
| FW#71 | the velocity lookup: note A, SE velocity HL → L from section 1, A from section 2 (below) |
| FW#72, #73 | section 7's value for a key, and section 8's byte |
| FW#74, #75 | section 6's first or second row for a key, with section 5's first or second word |
| FW#76 | section 9's first byte |
| FW#77 | section 4's value for a key, with section 3's word |
| FW#78 … FW#81 | copy section 10, 11, 12 or 13 (each starts with its own length) into RAM |

The layout (little-endian words), the same in the prototype tables of
`put*.exe` and in the piano:

| Offset | Holds |
|--------|-------|
| 0 | table version, `3017H` (V30.17) |
| 2 | the table's length in bytes: 1873 for the Steinway B-SE, 2048 for the 290SE |
| 4, 5 | the bottom and top note (MIDI numbers): 21 and 108 (A0–C8, 88 keys) on the Steinway B and Yamaha C-7, 12–108 (97 keys) on the Bösendorfer 290, 17–108 (92) on the 225 and 275 |
| 6 | 14 offsets of the table's sections, increasing |
| 22H | the model's name, then CR LF 7FH (`Steinway B-SE`) |
| after the name | a word: where section 9's velocity trims start (`067EH`) |
| sections | below |

The installers fill the rest of the EEPROM with `FF`.

The Steinway B-SE's sections. "Per key" means one value per key, bottom
note first; a grid of n × 88 is stored row by row. "Ticks" are the
calibrator's and player's timer ticks. **Wherever the calibrators tune an
actuator value (the strike, the release, the silent press), a bigger value
means less force; FF means none.**

| # | Offset | Size | Shape | Read by | What it is | Written by |
|--:|-------:|-----:|-------|---------|------------|------------|
| 0 | 34H | 16 | 8 words | FW#71 | **Breakpoints**: the SE velocities the calibration targets, ending in 0. 25 40 64 100 160 256 400 in the prototype, 30 40 64 … in the piano | — |
| 1 | 44H | 616 | 7 × 88 | FW#71 | **Lead time** per key at each breakpoint: the ticks from the strike command to the hammer reaching the sensor, as measured. Prototype rows 28 36 48 64 80 108 136 | `cal`, "ACTUATE LEVEL" |
| 2 | 2ACH | 616 | 7 × 88 | FW#71 | **Strike level** per key at each breakpoint: the value that makes the hammer arrive at the breakpoint's velocity, within 2% (smaller = harder). Prototype rows 18 30 48 68 100 128 160 | `cal`, "ACTUATE LEVEL" |
| 3 | 514H | 2 | word | FW#77 | 288: the silent press's target time | — |
| 4 | 516H | 88 | per key | FW#77 | **Silent level**: the mode-00 value that presses the key down without sounding, in 192–384 ticks. 29 in the prototype, 28–29 in the piano | `silent` |
| 5 | 56EH | 6 | words | FW#74, #75 | The two **release target times**, 60 and 120 ticks (then 0) | — |
| 6 | 574H | 176 | 2 × 88 | FW#74, #75 | **Release levels**: the mode-00 value that lets the key back up in 60 (row 0) or 120 (row 1) ticks, within 25%. Prototype rows 46 and 38 | `cal`, "RELEASE LEVEL" |
| 7 | 624H | 88 | per key | FW#73 | **Free release time**: the ticks a key takes to come back up when let go (value FF), as measured. Prototype 44 | `cal`, "UNCONTROLLED RELEASE" |
| 8 | 67CH | 1 | | FW#73 | FF: the value for letting a key go | — |
| 9 | 67DH | 90 | | FW#76, FW#71 | A byte (100: **the hold level**, used once the hammer has struck); then the trims' on/off flag; then **a signed velocity trim per key**, which FW#71 applies before the lookup when the flag isn't 0 | — |
| 10 | 6D7H | 9 | length + 8 | FW#78; setup | +1 first and +2 last adjustment/calibration note, +3 the calibrator's tries per value (24), +4 first calibration, +5 and +7 the silent press window (192 and 384 ticks, words) | `setup`, `cal` (+4) |
| 11 | 6E0H | 91 | length + 90 | FW#79 | **The player's constants** (below) | — |
| 12 | 73BH | 13 | length + 12 | FW#80; setup | +2 piano serial number | `setup` |
| 13 | 748H | 9 | length + 8 | FW#81; setup | +1 discard silent notes, +2 German note names | `setup` |

**How a note's loudness becomes an actuation (FW#71).** Given a key and an
SE velocity (bigger is softer), the firmware:
1. Applies the key's trim from section 9, if trims are on.
2. Finds the two breakpoints around the velocity (past either end, it
   extends the first or last interval).
3. Reads the key's values at those two breakpoints from section 1, and
   interpolates linearly. It rounds to the nearest whole number. If the
   next row is lower, it treats the interval as flat. The result is clamped
   to 0–255, and an interpolation step over 255 is a fatal error.
4. Does the same with section 2.

`play.exe`, `midiin` and `cal` call it. `lut.Table.Lookup` does steps 2–4
the same way.

**How the player drives a key.** Each key's actuator has two parts:
- a 2-bit **mode**: 01 (strike), 11 (hold), 10, or 00 (gentle force: release
  or silent press);
- an 8-bit **value**, written through ports 33H (register address) and 34H
  (data). In the strike and in mode 00 a bigger value is less force (that is
  how the calibrators tune them); FF is none.

FW#17, #19, #20 and #21 set a key's mode to 01, 11, 10 and 00 with a
value; #19 and #20 act only on a key that is already active. `play.exe`
keeps a time-ordered list of events (type 1, 2, 4 or 8 becomes mode 01,
11, 10 or 00) and runs them from its timer interrupt. For each note at
strike time T it:
1. Prepares the velocity:
   - it scales it by a factor for the subdue level (section 11);
   - it can replace it with a uniform value or add an offset to it.
2. Takes a velocity of 640 or more (section 11's word 28) as a **silent
   note**. Unless "discard silent notes" is on, it is played as a silent
   press:
   - mode 00 with the key's section 4 level;
   - started about twice section 3's 288 ticks before T, kept within
     section 10's 192–384.
3. Otherwise calls FW#71, which returns the lead time (section 1) and the
   strike level (section 2), and schedules:

| When | Event | Value |
|------|-------|-------|
| T − lead − 30 ticks (64 for some keys; longer for very loud notes) | mode 00 | FF (section 8): let the key go, in case it is down |
| T − lead | **strike**: mode 01 | the strike level (section 2) |
| T − lead × 16/256 | a check event | |
| T + lead × 32/256, when section 11 allows it | **hold**: mode 11 | the hold level (section 9's first byte, 100) |

When a key's sensor reports during the strike, the player switches that key
to mode 11, with the strike level as its value. For a soft note (velocity
over 128) it first scales that value by section 11's word 32 / 256.

**Releasing a note.** Before the release, the key spends 8 ticks in mode 10
(section 11's word 34). Then `play.exe` lets it up in one of three ways, by
a measure of the note's timing:
- free, with FF, taking the key's section 7 time;
- the 60-tick release (section 6, row 0);
- the 120-tick release (section 6, row 1).

The measure is computed through FW#190, a table-based function, and
section 11's word 36, then compared with words 38 and 40. The player starts
the release that much early, so the key is up at the recorded time.

The ticks are the 800 Hz performance clock (section 11's word 0). The
player runs its timer at 6,400 Hz (FW#32 divides a 2.048 MHz clock).

**How the calibrators fill the table.** `cal` works through the keys of the
calibration range (section 10) in three phases. Each test press is a
strike, a hold once the sensor fires, 8 ticks in mode 10, then a release:
1. **"UNCONTROLLED RELEASE":** a press at velocity 80, then a free release
   (FF). It times the key's return and stores it in section 7.
2. **"RELEASE LEVEL":** for each of section 5's times (60, then 120 ticks),
   it releases with a trial level and times the return. It nudges the level
   by one (up if too slow) until the time is within 25%, at most 24 tries
   (section 10). The result goes to section 6.
3. **"ACTUATE LEVEL":** for each breakpoint (from the first, or from the
   second after the first calibration), it strikes with a trial level and
   reads the hammer's velocity from the sensor. It moves the level by one
   until the velocity is within 2% of the breakpoint; too soft means a
   smaller level, never below 8. Section 2 gets the level, and section 1
   the ticks from strike to sensor.
4. Then it marks the first calibration as done (section 10 + 4).

`silent` raises or lowers each key's mode-00 level until the key goes down
within section 10's 192–384 ticks, and stores it in section 4.

Section 11, as `play.exe` reads it (its data, after the length byte;
little-endian words):

| Offset | Steinway B-SE | Used for |
|-------:|--------------:|----------|
| 0 | 800 | the performance clock, ticks per second |
| 6 | 80 | the uniform velocity, when that option is on |
| 8 | 16 | the velocity offset, when that option is on |
| 10 | 208 | scales the velocity of a quick re-strike |
| 12 | 16 | when the check event comes: lead × n/256 before the strike |
| 14 | 176 | re-strikes sooner than this many ticks are handled apart |
| 16 | 256 | a velocity limit for re-strikes |
| 18, 20 | 30, 64 | ticks between letting the key go and the strike (64 for keys marked 2) |
| 22 | 32 | when the hold starts: lead × n/256 after the strike (0 = no hold) |
| 24 | 38 | below this velocity (very loud), the let-go time grows (× 38 / velocity) |
| 26 | 32 | a re-strike timing limit |
| 28 | 640 | **silent-note threshold**: this velocity or more (softer) is a silent press |
| 30 | 128 | above this velocity (softer), the value after the sensor is scaled… |
| 32 | 224 (**160 in this piano**) | …by n/256 |
| 34 | 8 | ticks in mode 10 before a release (also in `cal` and `silent`) |
| 36 | 512 | scales the release measure |
| 38, 40 | 2400, 4800 | the release measure's limits: free release, 60-tick, 120-tick |
| 58–65 | bytes 40 11 10 07 05 02 01 00 | level thresholds |
| 74–89 | 240 256 256 256 256 256 288 480 | the velocity factor (/256) for each threshold |

Words 2 (22) and 4 (120) are not read by `play`, `cal` or `silent`.

`setup` edits these six settings, each one byte:

| Setting | Where | Prototype |
|---------|-------|-----------|
| piano serial number (shown as two hex digits) | section 12 + 2 | 00 |
| discard silent notes (true = FFH) | section 13 + 1 | true |
| display German note names (true = FFH) | section 13 + 2 | false |
| first adjustment/calibration note (MIDI number) | section 10 + 1 | 21 (A0) |
| last adjustment/calibration note | section 10 + 2 | 108 (C8) |
| first calibration (0 = true, 1 = false) | section 10 + 4 | true |

`selut TABLE [OTHER]` prints a table key by key and lists every difference
from another table. `selut -key E1 TABLE` shows what FW#71 gives one key
across velocities.

### This piano's table (backed up 2026-11-23)

Retrieved with `getlut` through `sehost`. It took a few seconds, nothing
moved, and the service went back online. The 2048-byte image is on the Pi
in `~/sepiano-data/backups/eeprom-20261123.lut` and in the owner's
`Desktop\SE\EEPROM backups\` (SHA-256 `e40de190…c53ce303b`). It is not in
the repository.

- **"Steinway B-SE", table V30.17**, 1873 bytes: the `putb` prototype's
  layout, so a Steinway model B.
- Settings:
  - serial number **08**;
  - discard silent notes **false** (the prototype says true), so notes
    softer than velocity 640 are played as silent key presses;
  - German names false;
  - calibration range **E1 to E1**: running `cal` or `silent` now would
    recalibrate E1 alone;
  - first calibration **false** (it has been calibrated).
- 1,537 of the 2,048 bytes differ from the prototype. Nearly all are the
  per-key values of sections 1, 2, 4, 6 and 7: the piano's calibration.
  The others:
  - the settings;
  - the first breakpoint (25 → 30);
  - section 11's word 32, 224 → 160: once the sensor sees the hammer, soft
    notes switch to a smaller hold value, probably a firmer hold;
  - **a velocity trim of +7 on G3** in section 9. Trims are switched on;
    it is the only one.
- Keys that stand out, read with the sections' meanings:
  - **E1** and **C#3** reach 255 in the soft rows of section 2: the gentlest
    strike the table can hold. For E1 that is from velocity 400 on, so its
    softest notes may come out a little louder than recorded.
  - **A#0** takes 80 ticks to come back up when let go (section 7), against
    about 33 for a typical key, and its release levels (section 6) are
    correspondingly gentle. It is a slow key, perhaps worth a technician's
    look.

  E1 is also the only key in the calibration range, so the last
  calibration or adjustment was probably of that key alone.

### Warning: `play.exe` checks the table version

At start-up `play.exe` (V50.45) reads the word at FW#47. **If it is not
`3017H`, it overwrites EEPROM bytes 0–255 with 0, 1, 2, … 255 and quits.**
Since the piano plays, its table is V30.17. Never install a table with
another version, and never write the first two bytes, or the next play
will damage the table. No other program has this check.

## The monitor

`monitor.exe` loads at A000H, prints `/` as its prompt and takes one-letter
commands. The hex arguments follow the letter **with no space**, separated
from each other by a space or a comma (`D0000 00FF`). `D` prints lines of
`AAAA=XX XX … ` and then the same bytes as text.

| Command | Does | Safe? |
|---------|------|-------|
| `Dstart [end]` | dump memory, hex and text (128 bytes without an end) | yes: reads only |
| `H a b` | print a+b and a−b | yes |
| `T addr` | copy the whole EEPROM table into RAM at addr | yes for the EEPROM (writes RAM) |
| `V start end dest` | compare two memory blocks | yes |
| `S addr` | show and change memory, byte by byte | changes RAM |
| `F start end byte` | fill memory | changes RAM |
| `M start end dest` | move memory | changes RAM |
| `X [reg]` | show or change the registers | — |
| `I port` | read an I/O port | can have side effects |
| `O port byte` | write an I/O port | **no** |
| `G [addr] [break]` | run code | **no** |
| `P addr` | **write the whole EEPROM from RAM at addr**, then verify | **no** |
| `R`, `W start end [entry]` | read and write memory as a recording ("BEGIN READING", "BEGIN WRITING"), probably through the tape interface: they share firmware entries with `load` | needs a tape deck |
| `Q` | quit | |

The monitor is the way to read the firmware itself: `D` over the console
gives about 850 bytes of memory a second. Nothing in it reads or writes PC
files. `semon` drives it and types nothing but `D` and `Q`.

## The firmware (read 2026-11-23)

Read with `semon`, with the owner's OK, through the monitor's `D`: page
zero and A000H–FFFFH. It took 29 seconds for 24 KB, and nothing moved.
The dump's first 3,901 bytes are the monitor itself and match the uploaded
image byte for byte, so the read path is exact. The images are on the Pi in
`~/sepiano-data/firmware/` and in the owner's `Desktop\SE\Firmware dumps\`,
not in the repository (they are Stahnke's code).

- It is the **"STAHNKE BOOTSTRAP LOADER, V43.47"**, "(FOR USE WITH
  BOSENDORFER SE/PC AND MULTIBUS PROCESSOR)", © 2009.
- `0000H` holds `JP B82AH`, and **B82AH** is the jump table: 195 entries,
  all `JP`. Its text and code follow at BA73H, up to about E172H. Its
  variables are around D4C3H–D540H, E273H–E672H and EE80H (the player's
  level). Apart from the jump, page zero holds leftover bytes: it is RAM.
- **The EEPROM sits behind I/O ports:** the address goes to ports 35H (low
  byte) and 36H (high byte), and the data byte is read or written at 37H.
  A write is checked against a timer (ports 39H and 3BH, an 8254-style
  counter).
- The rest of the hardware, by the entries that use it. "Probably" marks
  inferences from which programs call what:

| Ports | Entries | What |
|-------|---------|------|
| 28H–2FH (in) | FW#3–#15 | key and hammer sensors: `record`, `cal`, `anykey`, `anyham`, and the player's sensor loop (FW#9–#11) |
| 30H–34H | FW#16–#22 | the actuators. FW#16 sets them up for the key count, 4 keys per group and 5 registers per group, all off. 33H is the register address and 34H the data. Per key: a 2-bit mode and an 8-bit value |
| 30H, 31H (in), 33H/34H | FW#23–#30 | probably the pedals (two of them, state in D4FDH and D4FEH; `sustain` raises the dampers through FW#24) |
| 38H–3BH | FW#31, #32, #34, #43 | an 8254-style timer on a 2.048 MHz clock (FW#32 takes a rate in Hz); also the EEPROM write timeout |
| 3DH | FW#35–#39, #45, #46 | an interrupt-enable register: FW#37 and #38 switch the player's timer interrupt off and on (with FW#143 and #144), FW#35 and #36 another source; FW#39 reads a status bit |
| 3CH | FW#40, #41 | a raw register, read and written; not identified |
| 35H–37H | FW#42, #43 | the EEPROM |
| BAH, BBH | FW#107–#113, #147–#156, #193 | a serial channel: the PC link (the exit and error paths use it) |
| B8H, B9H | FW#114–#120, #157–#167, #194 | a second serial channel, probably MIDI (`midiin` uses it) |
| D8H, D9H | FW#141–#145 | the interrupt controller, 8259-style: FW#141 sets it up, #143 masks and #144 unmasks a source, #145 ends an interrupt |
| CAH, CBH | FW#129–#132 | a second timer, for delays: FW#130 waits 1 ms, FW#129 waits HL ms (the pedals and `sustain` use them); FW#131 and #132 give clock rates |
| E0H, F0H | FW#133, #134 | not identified |

  Other entries without ports:
  - FW#87–#106 are the link's buffers. FW#97 tests for a key, FW#99 reads
    one and FW#101 prints a character on the console; FW#104 and FW#106
    receive and send on the file channel.
  - FW#121–#128 are arithmetic.
  - FW#137 and FW#138 set and read one of 8 interrupt vectors; FW#139 and
    FW#140 set and read the NMI handler.

`sesuite -fw IMAGE -org A000 -table B82A` reports, entry by entry, the
ports and fixed memory each one reaches; `-list` prints the listing.

## What this means for sepiano

1. **Back up first** (done 2026-11-23). `getlut` copies the EEPROM to a
   file without writing anything to the piano and without moving it (it only
   reads the EEPROM and sends bytes). Stop a paused or playing piece first
   (`POST /api/v1/deck/stop`) so the player quits cleanly. Then, on the Pi:

   ```bash
   sudo systemctl stop sepianod
   cd ~/sepiano-data/backups && ~/sepiano-bin/sehost -exe ../suite/getlut.exe -port /dev/sepiano-piano eeprom-YYYYMMDD.lut
   sudo systemctl start sepianod
   ```

   The file comes back as `name.$$$` and is renamed when complete; a file
   of the same name is replaced, so use a new name. Keep a copy off the Pi
   too. `putlut` can write it back.
2. **Read the table** (done: `selut`, and the section map above, from the
   firmware). Still open: sections 2, 4, 6, 7 and 11 in physical terms, and
   how the actuation event uses section 2's value.
3. **Run any program from the app.** `sepianod` runs `play` and `record`
   today. A "Tools" screen could run any installed program as a console:
   its text, its keys, and a file folder of its own. R and M programs first;
   W programs only behind a confirmation and only after a verified backup.
4. **Edit the settings.** The first editor is `setup`'s six fields: a few
   bytes at known places, written through `setup.exe` or `putlut`.
   Calibration (`cal`, `silent`) rewrites many values. Run it only when the
   owner wants a recalibration, and with a backup to return to.
5. **Read the firmware** (done 2026-11-23, `semon`; decoded: the hardware
   map, the actuator modes, how `play.exe` schedules a note, section 11).
   Still open:
   - what modes 10 and 00 do physically;
   - sections 4, 6 and 7 in physical terms;
   - the untraced words of section 11;
   - the ports marked "not identified".
