# The recording editor (piano roll)

The owner asked for it on 2026-11-23: "the recording editor (moving notes,
timing (on/off), release speed, pedals, etc) - essentially anything that
would be associated with the recording/playback", in piano-roll style. The
detailed work is done on a computer, in a browser, so the whole app also has
a layout for wide screens.

## What the first recording showed (2026-11-23)

The owner made one short test recording ("First Test", 64 notes, 34 s). It
seemed reliable, but releases seemed to come up out of sync where the owner
let go together. The owner thinks some key sensors are set too high (late)
or too low (early) against the point where the damper quiets the string.

From that recording (`GET /pieces/{id}/keys`, the Key check screen):

- **G3 let go 305 ms and 568 ms before the rest of its chords**, both times
  it was in one. It also recorded 35% softer (in hammer speed) than its
  neighbours. That fits a G3 sensor problem, but two chords are not proof.
- **Two notes have velocity 0** (A3 at 14.05 s, A4 at 14.43 s): the key
  went down, but the recorder measured no hammer speed. In `record.exe`, a
  press is written with velocity 0 on the path at 0405H–0421H (FW#12, then
  FW#13, then `B=FF, HL=0`), where no hammer timing is used. **On
  playback, velocity 0 is a silent press** (`play.exe` 1044H): the key goes
  down without sounding. (Corrected 2026-11-24: this doc first said the
  hardest strike.) If those two notes sounded when they were played, their
  hammer sensors missed them and they go missing on playback; "fill in"
  (below) gives them the loudness around them.
- The recorder has no per-key correction (docs/SUITE-PROGRAMS.md, "The
  recorder").

The formal tests at the piano, when it can make noise, will show whether
these repeat:

- each key a few times at the same loudness, for velocity;
- chords pressed and let go together, several times each, for releases.

## What can be edited

| | In the SE file | In the editor |
|---|---|---|
| When a note starts and ends | yes, to 1.25 ms | drag; ←/→; type a time; "Move" and "Release" by any ms |
| Which key | yes | drag up or down; ↑/↓ |
| Loudness (velocity) | 10 bits; bigger is softer | drag its stalk; Louder/Softer (5% hammer speed); type a value |
| Damper pedal | 0–255 every 10 ms | draw it |
| Soft pedal | 0–255 every 20 ms | draw it |
| Adding and deleting notes | yes | double-click; Delete |
| **Release speed** | **not in the file** | not possible per note |

**Release speed is not in the file.** A `.kb` release is only a time. How
fast a key comes up is set by the piano's own table, for each key and not
for each note:

- section 5 holds the release target times;
- section 6 holds the release levels;
- section 7 holds the free release time.

Changing those is a settings edit (docs/SETTINGS-EDITOR.md), not a recording
edit. What the editor can do is move a release earlier or later.

Not decoded yet: Stahnke's own `edit.exe` ("INTERACTIVE PERFORMANCE EDITOR",
docs/SUITE-PROGRAMS.md) has commands for sostenuto, cue marks, splice,
mask, mute and "uniform". Its file events for sostenuto are not known.

## How it works

- **Reading** (`GET /pieces/{id}/performance`, `edit.FromPerformance`).
  Each press is paired with the release that follows it on the same key, to
  make a note.
  - A press on a key that is already down ends the note before it.
  - A press with no release lasts to the end.
  - Events outside the keyboard are kept as they are. These are Zenph's
    marks on key 11 and the Bösendorfer's extra bass keys 12–20.

  All 350 performances in the owner's collection come back with the same
  notes and pedals after an unchanged save (`edit/corpus_test.go`).
- **Saving** (`POST /pieces/{id}/edits`, `edit.Doc.Performance`).
  - An original is never changed. Saving it makes an **edited copy** (origin
    `edited`, "Title (edited)", "Edited from …").
  - The copy has its own row in the library. Its metadata is locked from the
    original.
  - An edited copy is saved over (`replace`).
  - Notes on one key may not overlap: a note that starts while the one before
    it is still down ends that one. At the same tick, releases come first, so
    a repeated note is struck again.
- **Hearing it** (`POST /pieces/{id}/preview`). The edited version plays
  from the cursor, without saving.
  - It starts with one second for the pedals to take their places.
  - Notes already sounding at the cursor are left out.
  - Software voicing applies, as it does to every piece.
- **In the browser** (`web/editor.js`). One canvas holds the roll (keys up
  the side, time across), the Loudness, Damper and Soft lanes, and a strip of
  the whole piece.
  - Up to 100 steps of undo.
  - Unsaved work is kept while the app stays open, so leaving the screen
    loses nothing. Closing the page asks first.

## Using it

- Open a piece (Library, or a recording's "Edit" after it is saved), then
  **Piano roll → Open in the editor**.
- **Select**:
  - click a note;
  - drag a box;
  - Shift adds to the selection;
  - **click a key at the left for all its notes.**
- **Change**:
  - drag a note to move it, or its right end to move its release;
  - drag a stalk in Loudness;
  - draw in Damper or Soft;
  - the selection panel has buttons and number fields for exact changes.
- **Correct one key through a whole recording**: click the key at the left
  (for example G3), then "Release by 400 ms", or Louder/Softer.
- **Keys**:

  | Key | Does |
  |-----|------|
  | Space | play from the cursor, or stop |
  | Delete | remove |
  | ←/→ | move 10 ms (Shift: 100 ms) |
  | Alt+←/→ | move the releases |
  | ↑/↓ | change the key |
  | Ctrl+Z, Ctrl+Y | undo, redo |
  | Ctrl+S | save |
  | +, −, 0 | zoom in, zoom out, whole piece |
- **View**:
  - the wheel scrolls; Shift+wheel goes along the piece;
  - Ctrl+wheel (or a pinch) zooms;
  - Alt+wheel makes rows taller;
  - right-drag pans;
  - click the strip at the bottom to jump.

## On a computer

Any computer on the home network opens `http://raspberrypi.local/` in a
browser.

- **1000 pixels or wider**: the tab bar becomes a side bar and pages are
  wider.
- **The editor** uses the whole width.
- **1280 pixels or wider**: the selection panel sits beside the roll.

On a phone the editor works for looking, small changes and playing from a
point.

## Per-key corrections (built 2026-11-23, the owner's choice)

Corrections put right what this piano's sensors get wrong when it records.
They are kept in one list, `data/corrections.json` (`piano.Corrections`).

- **For each key**:
  - **Loudness**, in % hammer speed. +20 makes the key's notes 20% faster
    (velocity ÷ 1.2), so louder. The limits are −50 to +100.
  - **Release**, in ms. Plus lets go later. The limit is ±1000.
- **Fill in notes with no hammer reading.** The player plays a velocity-0
  press as a silent press. Where the key really sounded and its hammer
  sensor missed it, the note would be lost. With "fill in", a velocity-0
  press takes the median velocity of the notes within one second of it
  (silent presses left out), so it sounds. This is worked out after the
  keys are corrected, and from measured notes only. Leave it off for
  recordings with deliberate silent presses.
- **Where they apply.** When "on", corrections apply as a recording made
  on this piano plays (`Piece.NeedsCorrection`):
  - an origin `recorded`;
  - or an edited copy of one (`from_recording`) that is not `corrected`.

  They apply alone, in albums and play lists (`library.Join` takes a
  `Fix`), and in the editor's "Play from cursor". Now Playing shows
  "Recording corrections on".
- **The files never change.** The Key check therefore always reads what
  the sensors measured, and its suggestions stay true.
- **Putting them into a copy**:
  - "Save a corrected copy" on a recording's page (`POST
    /pieces/{id}/corrected`) makes "Title (corrected)", marked `corrected`;
  - in the editor, "Apply corrections" (`POST /corrections/apply`, the same
    code as playback) puts them into the notes as one undoable step, and a
    save marks the copy `corrected`.

  A `corrected` piece is never corrected again.
- **Suggestions**. A recording's Key check suggests corrections (`piano.Suggest`):
  - a key d% off its neighbours gets the speed that brings it back
    (1 / (1 + d) − 1);
  - a key that lets go early or late in its chords gets its typical
    difference the other way, rounded to 5 ms;
  - "fill in" is suggested when there are velocity-0 presses.

  Each suggestion has a checkbox. "Use the checked ones" replaces those
  keys' corrections. In an ordinary piece, many keys stand out only because
  of how they were played. The suggestions are only as good as the test
  recording behind them: each key several times at one loudness, and
  chords let go together.
- **Screens**:
  - Record → Corrections (`#/record/corrections`): on or off, fill-in,
    the keys, and the recordings made here;
  - the Key check;
  - a recording's page;
  - the editor.

From the first test recording, for example, "G3 loudness +54%, release
+435 ms" is suggested. That rests on only two notes and two chords.

## Next steps, for the owner to choose

1. **Formal tests at the piano** (velocity, releases, playback), then the
   Key check on each recording and its suggested corrections.
2. **More editing**:
   - insert or remove time at the cursor;
   - stretch the tempo of a passage;
   - copy and paste;
   - set a pedal up or down over a selection.
3. **Decode `edit.exe`**: sostenuto and cue-mark events, and how Stahnke's
   own editor works.
