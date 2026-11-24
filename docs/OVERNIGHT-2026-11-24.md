# The overnight build, 2026-11-24

The owner asked for an unattended overnight build: every suite program
usable from the app, then more editing tools, and anything else that moves
the project forward. This page says what was built, what changed in what
you were told before, and what to try at the piano.

Everything below is installed on the Pi and works in the app (on a phone,
or in a browser on a computer: `http://raspberrypi.local/`). **Nothing was
played, recorded or run on the real piano.** It was all tested in the
simulator, with a copy of your "First Test" recording.

## Corrections to what you were told

1. **Velocity 0 is a silent press, not the loudest note.** Your First Test
   has two notes with velocity 0 (A3 and A4 at about 14 s). Before, I said
   the player would strike them as hard as it can. That was wrong. The
   player treats velocity 0 like any silent press: the key goes down without
   sounding (`play.exe` code at 1044H).
   - If you heard those two notes when you played them, their hammer
     sensors missed them, and they are missing on playback.
   - "Fill in notes with no hammer reading" (Corrections) gives such notes
     the loudness of the notes around them, so they sound again.
   - It is off unless you turn it on.
2. **Stahnke's editor (`edit.exe`) can change the piano's settings.** Its
   LOUDER, SOFTER and LEVEL commands change the stored per-key trims
   (LEVEL sets them all to zero). In the app it now asks you to confirm,
   and backs up the settings before and after, like the calibrators.
3. **Sostenuto was being lost.** In the files, the sostenuto (middle) pedal
   is stored as events on "key 0". 16 of your 350 pieces use it. Albums and
   play lists played as one (and the editor) dropped those events. That is
   fixed: albums and play lists keep the sostenuto, and so does a piece
   edited or corrected in the app.

## Every program usable

**Piano → Tools** now lists all of Stahnke's programs that the service does
not run itself (play, record and getlut it runs itself).

| Group | Programs |
|-------|----------|
| Tests | key sensor test, hammer sensor test |
| Keys and pedals | dampers, manual adjustment |
| Players | MIDI input, MIDI file player (a piece's original MIDI, or a file you upload), PianoSoft disk player (an uploaded file) |
| Calibration and settings | calibrator, silent-press calibrator, settings menu, **Restore a backup** |
| Stahnke's editor | his command-line editor, on a copy of a piece, with his commands listed beside it |
| Tape | dump, load, record/play (they need a cassette recorder on the piano's tape interface) |
| Factory tables | Steinway B, Bösendorfer 225/275/290, Yamaha C7 |
| Expert | the Z80 monitor |

Safety:

- **Changing settings.** A program that changes the piano's settings asks
  you to confirm. The settings are backed up just before it starts and
  again when it ends, so you can compare the two on the Backups screen.
- **Factory tables** replace the whole calibration. They also need the
  word ERASE typed. You should not need them; restoring a backup is the
  way back.
- **Restore a backup.** It is also on each backup in Piano → Backups.
  - It writes only a whole, valid table for this piano's model.
  - It backs up first.
  - Afterwards it reads the piano back and tells you whether it now
    matches the backup exactly.

  This is the way back after a calibration that goes wrong.

A piece's page also has "Stahnke's programs": his editor, his MIDI player
(for pieces converted from MIDI), and dump to tape.

## The editor (a piece's page → Piano roll → Open in the editor)

New since yesterday:

- **Time ranges.** Drag along the time ruler at the top to mark a stretch
  of time; a click there moves the cursor. Then you can:
  - **Play it** (just that stretch);
  - select its notes;
  - copy it (notes and pedals);
  - delete it (the time is removed and what follows moves up);
  - **keep only it** (make an excerpt);
  - **stretch it** (for example 110% to slow that passage by 10%);
  - set the damper (down, half, up), the soft pedal or the sostenuto over
    it.
- **Copy and paste.** Ctrl+C copies the selected notes (or the range with
  its pedals). Ctrl+V adds them at the cursor; Ctrl+Shift+V opens a gap at
  the cursor and puts them in it. The copy stays while the app is open, so
  a passage can be moved from one take into another.
- **Chords** (select some notes):
  - "Start together" makes each chord start together;
  - **"Let go together"** makes each chord's keys let go together (the
    release problem you noticed).
- **Loudness:**
  - "louder from … % to … %" makes a gradual crescendo or diminuendo
    across the selection;
  - "even out" brings the notes closer to one loudness.
- **Insert silence** at the cursor; **all one length**; **legato** (hold
  each note to the next).
- **Sostenuto pedal** in its own lane, and **cue marks** (Stahnke's) as
  flags on the ruler.
- **Show the original.** On an edited or corrected copy, the original's
  notes are drawn as dashed outlines, so you can see every change.
- Note names appear inside the notes when the rows are tall enough
  (Alt+wheel makes them taller).
- The side panel lists every shortcut when nothing is selected.

## Checking the sensors

- **Record → Sensor test** guides two short recordings:
  - each key four times at the same loudness;
  - chords pressed and let go together, three times each, up the keyboard.

  When each is saved, its Key check opens by itself.
- **Several recordings checked together**: tick them on the Sensor test or
  Corrections screen. More takes give firmer suggestions.
- The Key check now has **press timing** as well as release timing: a key
  whose sensor fires early or late on the way down.
- **Corrections** now include a per-key **press** shift (Record →
  Corrections, and the Key check's suggestions). Silent presses stay
  silent.

## The library

- A piece's page shows its **Versions**: the copies made from it, or what a
  copy was made from.
- The Library has an **"All kinds"** filter: recorded on this piano, edited
  copies, uploaded, and so on.

## What to try at the piano, in this order

1. **Record → Sensor test → Loudness by key.** Play as it says, then read
   the Key check.
2. **Sensor test → Release timing** (the chords). Read the Key check's
   press and release timing.
3. **Check them together** (the Sensor test screen, at the bottom). Look
   at the suggestions. Use only the ones that look like sensors: the same
   key, the same way, in most chords.
4. **Play a recording** and hear it with the corrections. Now Playing says
   "Recording corrections on". Turn them off in Record → Corrections to
   compare.
5. **The editor:** open a recording. Drag a range on the ruler and "Play
   it"; select a chord and "Let go together"; save a copy.
6. **Tools → Key sensor test** and **Hammer sensor test** only read the
   sensors. They are safe, and show what each key reports.

Leave the calibrators, the settings menu, factory tables, the monitor and
restoring a backup until you mean to use them.

## For the record

- Every step was committed and pushed to toddatlas/sepiano. The commits of
  the night run from 7d140fd to the last one with this page.
- The tests cover the new parts:
  - all 350 pieces keep their notes, pedals and sostenuto through the
    editor;
  - restores are checked against the simulated piano;
  - the editing operations were checked in the browser.
- The details are in docs/RECORDING-EDITOR.md, docs/STAHNKE-EDITOR.md,
  docs/SUITE-PROGRAMS.md ("Run any program from the app") and
  docs/SE-FILE-FORMAT.md (sostenuto, cue marks, header byte 1, velocity 0).
