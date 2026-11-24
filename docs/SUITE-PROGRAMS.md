# The SE Program Suite's programs

What each program of *SE Program Suite v0.95* does, how it talks to the
instrument, and what it can change. Found by tracing the Z80 program each
one uploads (`cmd/sesuite`, `z80/`), from the owner's copy of
`se_program_suite_v0.95_setup.exe` (an NSIS 2.25 installer; 7-Zip unpacks
it). **None of these programs has been run on the piano yet**, except
`play.exe`.

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

The piano keeps its calibration and settings in an EEPROM, which the
firmware reads and writes a byte at a time:

| Entry | Does | Evidence |
|-------|------|----------|
| FW#42 | A ← EEPROM[HL] | `getlut` sends what it returns to the file; `put*` verify with it |
| FW#43 | EEPROM[HL] ← A; carry set when it worked | `put*` and `putlut` write with it, "EEPROM ERROR AT ADDRESS" on no carry |
| FW#44 | HL ← the EEPROM's size in bytes | the loop count of every whole-table read and write; the installers hold exactly 2048 bytes of table and padding |
| FW#47 | HL ← offset of the version word (0) | `put*` print "PROTOTYPE LOOKUP TABLE, V" from it |
| FW#65 | HL ← offset of the model name | `put*` print the name from it |
| FW#61, #63, #64 | HL ← offsets of setup's fields | `setup` (below) |
| FW#67, #68 | A ← the instrument's bottom and top note | `setup` prints them next to the calibration range |

The prototype tables in `put*.exe` show the layout (little-endian words):

| Offset | Holds |
|--------|-------|
| 0 | table version, `3017H` (V30.17) |
| 2 | the table's length in bytes: 1873 for the Steinway B-SE, 2048 for the 290SE |
| 4 | a word, not understood yet (`6C15H` for the Steinway B-SE) |
| 6 | 14 offsets of the table's sections, increasing |
| 22H | the model's name, then CR LF 7FH (`Steinway B-SE`) |
| … | the sections: per-note calibration, settings, curves, not decoded yet |

The installers fill the rest of the EEPROM with `FF`.

`setup` edits these, each a byte at a fixed distance from an offset the
firmware supplies:

| Setting | Where |
|---------|-------|
| piano serial number (up to 65,535) | from FW#63 + 2 |
| discard silent notes (true = FFH) | FW#64 + 1 |
| display German note names (true = FFH) | FW#64 + 2 |
| first adjustment/calibration note | FW#61 + 1 |
| last adjustment/calibration note | FW#61 + 2 |
| first calibration (0 = true, 1 = false) | FW#61 + 4 |

### Warning: `play.exe` checks the table version

At start-up `play.exe` (V50.45) reads the word at FW#47. **If it is not
`3017H`, it overwrites EEPROM bytes 0–255 with 0, 1, 2, … 255 and quits.**
Since the piano plays, its table is V30.17. Never install a table with
another version, and never write the first two bytes, or the next play
will damage the table. No other program has this check.

## The monitor

`monitor.exe` loads at A000H, prints `/` as its prompt and takes one-letter
commands with hex arguments:

| Command | Does | Safe? |
|---------|------|-------|
| `D start [end]` | dump memory, hex and text | yes: reads only |
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

The monitor is the way to read the firmware itself: `D` over the console,
at about 3.8 KB/s of text. Nothing in it reads or writes PC files.

## What this means for sepiano

1. **Back up first.** `getlut` copies the EEPROM to a file without writing
   anything to the piano and without moving it (it only reads the EEPROM
   and sends bytes). On the Pi, with the service stopped:

   ```bash
   sudo systemctl stop sepianod
   cd ~/sepiano-data/backups && ~/sepiano-bin/sehost -exe ../suite/getlut.exe -port /dev/sepiano-piano eeprom-YYYYMMDD.lut
   sudo systemctl start sepianod
   ```

   The file comes back as `name.$$$` and is renamed when complete. Keep a
   copy off the Pi too. `putlut` can write it back.
2. **Read the table.** The backup shows the piano's model name, serial
   number, settings and calibration. Comparing it with `putb`'s prototype
   shows what calibration changed.
3. **Run any program from the app.** `sepianod` runs `play` and `record`
   today. A "Tools" screen could run any installed program as a console:
   its text, its keys, and a file folder of its own. R and M programs first;
   W programs only behind a confirmation and only after a verified backup.
4. **Edit the settings.** The first editor is `setup`'s six fields: a few
   bytes at known places, written through `setup.exe` or `putlut`.
   Calibration (`cal`, `silent`) rewrites many values. Run it only when the
   owner wants a recalibration, and with a backup to return to.
5. **Read the firmware** with the monitor's `D`, to name the remaining
   firmware entries and decode the table's sections.
