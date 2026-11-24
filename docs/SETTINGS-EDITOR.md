# The piano settings editor (design)

A screen in the app to see, understand and safely change what the piano
stores in its EEPROM: the lookup table described in `SUITE-PROGRAMS.md`. It
is a design for the owner to review. Built so far:
- `lut`: reading, the firmware's lookup and trim arithmetic;
- `selut`;
- `getlut` backups through `sehost`.

Nothing in the app writes to the piano yet.

## Goal

The owner can:
- see every setting in plain terms (what it does, what it is now, what the
  factory prototype had);
- change the ones that are safe to change;
- hear the effect before committing it;
- undo any change, including one made months ago.

## What can be edited

The table, grouped for people rather than by section number:

| Group | What | Section | Tier |
|-------|------|--------:|------|
| Instrument | model, serial number, key range, table version | header, 12 | read only |
| Switches | discard silent notes | 13 | 1 |
| | calibration range (first and last note) | 10 | 1 |
| | first calibration, German note names | 10, 13 | read only |
| Voicing | per-key velocity trim, and trims on/off | 9 | 1 |
| Player | hold after the strike (on/off, delay) | 11 | 2 |
| | silent-note threshold | 11 | 2 |
| | soft-note hold scaling | 11 | 2 |
| | the other player constants | 11 | read only |
| Calibration | strike level and lead time per key and loudness | 1, 2 | 3 (expert) |
| | release levels, free release time, silent level | 6, 7, 4 | 3 (expert) |
| Structure | version, length, directory, name, breakpoints, sections 3, 5, 8, the calibrator's limits | | never |

- **Tier 1** comes first: what an owner plausibly wants to change, with
  little risk.
- **Tier 2** changes how every note is played. It needs a clear warning.
- **Tier 3** values are measured by the calibrators. Changing them by hand
  is an expert's job, behind a switch. The normal way to change them is to
  recalibrate the keys concerned (below).

### What a trim does

A trim is one signed byte per key (−128 to +127). FW#71 applies it before
its lookup, on the hammer's speed (`lut.TrimVelocity`, from the firmware):
1. The speed is 128000 ÷ velocity.
2. Each trim step adds 5 to it.
3. The speed turns back into a velocity.

So **+1 is slightly louder**, a little at loud dynamics and more at soft
ones. This piano's only trim, G3 +7:

| Played as | SE velocity | With +7 | Hammer speed |
|-----------|------------:|--------:|-------------:|
| loud | 64 | 63 | +1.8% |
| medium | 100 | 97 | +2.7% |
| soft | 256 | 239 | +7% |
| very soft | 400 | 361 | +11% |

The editor shows trims this way, as "louder by n% at medium", not as raw
steps.

## Safety rules

1. **Back up before every write.** Each write starts with a fresh `getlut`
   backup. It is refused if the piano's table differs from the one the
   changes were based on: something else changed it meanwhile.
2. **Only known fields, within their ranges.** Edits go through typed
   fields, never raw bytes. Before writing, the new image must:
   - parse with the same layout;
   - keep bytes 0–1 (the version) and the whole header untouched;
   - stay 2048 bytes, with `FF` after the table;
   - differ from the current table only at the fields edited (checked
     byte by byte).
3. **Write with Stahnke's own `putlut.exe`, never our own Z80 code.**
   `putlut` sends the whole 2048-byte image. The firmware's write routine
   (FW#43) skips every byte that is already right and confirms each byte it
   writes. So only the changed bytes are physically written, and the version
   word is never rewritten. A short file would stop `putlut` halfway, so the
   image is always complete.
4. **Verify after the write.** Read the table back with `getlut` and
   compare it byte for byte with the intended image. Any mismatch is shown,
   with Restore offered.
5. **Restore is the same path,** with a backup's image.
6. **Only when the piano is idle.** The deck runs `getlut` and `putlut` as
   an exclusive "maintenance" job; play and record wait, and the app shows
   it.
7. **An interrupted write is recoverable.** Unchanged bytes are skipped and
   the version is never written. So `play.exe`'s version check stays
   satisfied, and repeating the write, or a restore, completes it.
8. **Keep everything.** Every backup (2 KB) is kept, with why it was taken:
   by hand, before a write, after a write. Each write is logged with its
   changes.

## Try before writing: "software voicing"

`sepianod` serves every performance to `play.exe` itself. So it can apply
per-key trims, and a whole-piano louder/softer offset, to the velocities as
it serves them, with the firmware's own formula. The EEPROM is not touched:
- the change is immediate and reversible;
- it can be heard on any piece;
- it can apply to one piece or album.

"Write to piano" then makes a trim permanent in section 9.

- It only affects what `sepianod` plays (not MIDI input or the other
  programs).
- With both on, the two add up (both work on hammer speed).

This is also the "louder than recorded" trim the service plan left for
phase 2: the whole-piano offset, which the player's own level cannot do.

## Screens

A new **Piano** tab:

1. **Overview:**
   - model, serial number, key range, table version, last backup;
   - the switches;
   - notes on the table, by rule. For example: "E1 and C#3 are at the
     gentlest strike for soft notes", "A#0 returns slowly (80 ticks, typical
     33)", "Calibration range is E1 only".
   - Buttons: Back up now, Backups.
2. **Keyboard:** the 88 keys as a strip, coloured by a chosen measure:
   - trim;
   - strike level or lead time at medium loudness;
   - free release time;
   - silent level.

   Tapping a key opens its sheet.
3. **Key sheet:**
   - the note;
   - a trim control with its effect at four dynamics;
   - curves of strike level and lead time against loudness (`Lookup`);
   - release levels and times, silent level;
   - flags such as "at the limit".

   Calibration values are read only unless Expert is on.
4. **Player:** tier 2 options with plain names, allowed ranges and the
   prototype's values.
5. **Changes** (a drawer):
   - the pending edits, each with its before and after;
   - Try (software voicing), Write to piano, Discard.

   Writing shows each stage: backing up, checking, writing, verifying.
6. **Backups:**
   - when each was taken, and why;
   - any two compared in words ("G3 trim +7 → +4");
   - download the `.lut`;
   - Restore.

## API

| Method and path | Does |
|-----------------|------|
| `GET /api/v1/piano/table` | the current table (latest backup) as fields: value, prototype value, range, tier, label |
| `POST /api/v1/piano/backups` | read the table now (`getlut`); 409 if the piano is busy |
| `GET /api/v1/piano/backups`, `GET …/backups/{id}` | list; one as JSON or as `.lut` |
| `GET /api/v1/piano/backups/{a}/diff/{b}` | the differences, as fields |
| `POST /api/v1/piano/draft` | edits → the new image's summary, its differences and warnings; writes nothing |
| `POST /api/v1/piano/write` | a draft, its base backup and `confirm: true` → back up, check, write, verify; progress on SSE |
| `POST /api/v1/piano/restore/{id}` | the same, with a backup |
| `GET`/`PUT /api/v1/piano/voicing` | software voicing: trims and offset, in `settings.json` |

## Where it goes in the code

- **`lut`:**
  - a field model (id, section, offset, kind, range, tier, label);
  - `Apply(edits)` → a new image;
  - `Diff(a, b)` → field changes;
  - `Validate(base, new, allowed)` with the invariants above.
- **`deck`:** `RunUtility(ctx, program, dir, name)`. It runs a program to
  its end as a maintenance job, using `host.Session` and a file service in
  `/var/lib/sepiano/eeprom/work/`.
- **`api/piano.go`:** the endpoints. Backups go in `/var/lib/sepiano/eeprom/`
  as `YYYYMMDD-HHMMSS-reason.lut` with a `.json` note.
- **`library` (serving):** the software voicing hook.
- **`sim`:** an emulated EEPROM. The simulator cannot run Z80 code, so it
  recognises `getlut` and `putlut` by their banners and makes their file
  traffic. It can also fail a write at a given address, or stop halfway.
- **`web`:** the Piano tab.

## Recalibration

Recalibration is not the editor's job, but it works with it:
1. Back up.
2. Set the calibration range to the keys concerned (an editor write).
3. Run `cal` or `silent` from the Tools console.
4. Back up again.
5. Compare the two backups.

The range is E1 to E1 today, so a calibration started now would touch E1
only.

## Testing

- **Unit:** edits change only their own bytes; the header is never
  touched; the length and the `FF` tail are kept; diffs read back as the
  edits.
- **Simulator, end to end:** backup, draft, write, verify, restore; a write
  failing at an address; an interrupted write, then completed by repeating
  it.
- **At the piano, with the owner,** in this order:
  1. a write of the identical image: `putlut` should change nothing, and
     the read-back should match;
  2. one harmless change, German note names on, then off again (only
     Stahnke's own programs display it);
  3. a real edit.

## Built in this order

1. **Read only** (built 2026-11-23, commit b30d8d7, on the Pi):
   - backups from the app (`getlut` through the deck), the Overview,
     Keyboard and Key sheet views, the backup list and comparison;
   - the simulator's EEPROM.
2. **Software voicing:** trims and the offset applied while serving. No
   EEPROM writes.
3. **Writes:** the `putlut` path with backup, check and verify, for the
   tier 1 fields; then Restore.
4. **Tier 2 player options,** then the tier 3 expert edits.

## The owner's decisions (2026-11-23)

1. **Software voicing first.** Trims are tried by ear while `sepianod`
   serves the piece, before anything is written to the piano.
2. **Per-key loudness** is the tier 1 edit that matters most.
3. **No PIN.** Anyone on the home network with the app may write.
4. **Calibration values can be overridden by hand.** The calibrator
   sometimes can't calibrate a note ("CALIBRATION IMPOSSIBLE ON NOTE"), and
   that can stop the whole run. So an expert edit of a key's values, written
   to the piano, is wanted. It is still tier 3, with its own warnings.

Step 1, the read-only Piano screen, is built and on the Pi. The first
backup through it took 1.4 s, and matched the one taken earlier with
`sehost` byte for byte. Next: step 2, software voicing (trims by ear).
