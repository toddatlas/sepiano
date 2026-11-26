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
  - **Press**, in ms. Plus goes down later. The limit is ±300. (Added
    2026-11-24, with the Key check's press timing.)
  - **Release**, in ms. Plus lets go later. The limit is ±1000.
  - A silent press (velocity 640 or more) keeps its velocity, so it stays
    silent. A softer correction stops at 639, so an audible note never
    becomes a silent press (software voicing does the same).
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
  - a key that goes down or lets go early or late in its chords gets its typical
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

## The sensor test and checking together (2026-11-24)

- **Record → Sensor test** guides two recordings:
  - loudness: each key four times at one loudness;
  - releases: chords pressed and let go together, three times each, up the
    keyboard.

  "Get ready" arms the recorder with a title ("Sensor test: loudness, …").
  When the recording is saved, its Key check opens by itself.
- **Several recordings checked together** (`GET /keycheck?pieces=a,b`;
  `#/keys/a,b`; ticked on the Sensor test or Corrections screen). The
  pieces are laid end to end, so no chord spans two; more takes give
  firmer suggestions.
- The Key check now also has **press timing** (`piano.CheckPresses`): each
  key's press against the middle press of its chord, flagged at 25 ms or
  more in two thirds of its chords.

## More editing tools (2026-11-24)

The editor's operations live in `web/edittools.js`.

- **Time ranges.** Drag along the time ruler; a click there moves the
  cursor. A range can be:
  - played on its own (the preview's `to`);
  - selected (its notes), copied (notes and pedals), deleted (the time
    removed), kept alone (cropped), or stretched (25% to 400%);
  - slowed down or sped up gradually (`tempoRamp`): time passes f0 times
    as long at the start and f1 at the end, easing between them. 100 to
    130 is a ritardando that ends 30% slower; 100 to 80 an accelerando.
    Notes, pedals and marks move together;
  - given a damper (down, half, up) or soft pedal.
- **At the cursor:**
  - insert silence;
  - paste, either mixed in or into a gap opened for it, with its pedals.

  The copy lasts while the app is open, so it can go into another piece.
- **On selected notes:**
  - copy, cut, duplicate;
  - start together and let go together, chord by chord (presses within
    70 ms);
  - a loudness ramp from one percentage to another (a crescendo or
    diminuendo);
  - even out (towards the median, 0 to 100%);
  - all one length; legato.
  - Selecting: all, velocity 0, silent presses, invert.
- **Silent presses stay silent.** The loudness tools (ramp, even out,
  Softer and Louder, dragging several notes in the Loudness lane) leave
  them alone, and keep every other note at 639 or less. The one note you
  drag, or a velocity you type, goes where you put it.
- **Time edits snap to 20 ms.** Inserting, deleting, copying and pasting
  time round to the pedals' common step (16 ticks: the soft pedal's
  period, two of the damper's), so the pedals move exactly as far as the
  notes and never drift from them.
- **Range previews keep the sostenuto paired**: down at the start if it
  was down there, and up at the end if it is still down
  (`edit.Doc.From`, `Until`).
- **Zoom buttons** (2026-11-26): **Time** zooms in and out across, every
  lane together, centred on the cursor when it is in view; **Notes** makes
  the rows taller or shorter. Each lane (loudness, damper, soft) has its
  own Taller and Shorter buttons at its top right (1, 1.5, 2, 3 or 4
  times; remembered per browser). A taller pedal lane gives finer control
  of the pedal's depth. The damper and soft lanes now have separate
  heights (`DAMP_H`, `SOFT_H`).
- **Size** (2026-11-26): the toolbar's menu, Comfortable, Large or Extra
  large, scales the rows, the text, the toolbar and the panel together
  (remembered in each browser). The lanes below keep about their size on
  screen, so the extra room goes to the notes. **Fit keys** (K) gives rows
  just for the keys the piece uses; **Full screen** (F, Esc to leave)
  hides the app's bars.
- **Note names** are drawn in notes when rows are tall enough (Alt+wheel).
- **Shortcuts:** Ctrl+C, X, V (Shift+V inserts), D duplicates, and Esc
  clears. Undo covers ranges too.

## Recording details (2026-11-26)

The owner asked for recordings to carry what the library's catalogue
needs, with the pianist defaulting to the owner. The Record screen asks,
all optional:
- **Title**: what is played. Without one, it is "Recording" and the time.
- **Composer**: suggestions from the library's composers.
- **Pianist**: starts as Status → Settings → "Pianist for recordings" (set
  to the owner's name on the Pi). Another name is allowed for a guest, and
  "Make this the usual pianist" changes the setting.
- **Album or series** (for example "Practice, November 2026"). Takes with
  the same album are numbered in order, and the album is kept for the next
  take on that device.
- **Notes**: occasion, edition, tempo.
- **Add to favorites**.

How it works:
- The details are sent when the recorder is armed (`POST /record/arm`).
  While it waits or records they stay on screen, and changes go to the Pi
  as they are typed (`PUT /record/details`).
- When the recording is saved, `api.SaveRecording` puts them on the new
  piece, along with:
  - collection "Recorded on this piano";
  - captured on "This piano (1909 Steinway D, Stahnke SE)";
  - this piano's instrument (`Steinway_SED133291`, so it sorts with the
    Steinway files);
  - the date.
- The next take starts with no title, composer or notes; the pianist and
  album stay.
- The recorder's own file header (title and time) is unchanged.
- Everything can be changed later on the piece's page (Edit).

## Next steps, for the owner to choose

The editing tools and the decode of `edit.exe` listed here before are
built (above, and docs/STAHNKE-EDITOR.md).

1. **Formal tests at the piano** (velocity, press and release timing,
   playback), with Record → Sensor test, then the Key check and its
   suggested corrections.
2. **More editing**, if wanted: a velocity curve drawn across a
   passage, and markers with names. (Gradual tempo changes are built.)
3. **Writing trims to the piano** (`putlut`), deferred by the owner until
   the recordings show which keys need it.
