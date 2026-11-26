# Content libraries: importing, sorting, and fitting to this piano

ROADMAP idea 4. Free collections of piano performances, imported into the
library with where each piece comes from, converted with the right
curve, and fitted to this piano by ear.

## The collections

The owner's other session downloaded them on 2026-11-26 into
`C:\Users\todd\sepiano\datasets` on the owner's PC. That folder is outside
the repository. **Nothing from it may be committed.** Its `README.md`
lists the files.

| Collection | What | Imported as | Licence |
|------------|------|-------------|---------|
| **Vienna 4x22** | 22 pianists × 5 excerpts (Chopin Op. 38 twice, Op. 10 No. 3, Mozart K. 331, Schubert D. 783), 1999, Bösendorfer SE290 | the **native SE files**, as they are: grade Native SE. Five albums, one per excerpt, with a track per pianist | CC BY 4.0, Werner Goebl, doi:10.21939/4X22 |
| **Batik plays Mozart** | 12 sonatas, 36 movements, Roland Batik, Bösendorfer SE290 | MIDI exported by Bösendorfer's converter, turned back with **Bösendorfer's curve**: grade **SE, via MIDI**. An album per sonata | CC BY-NC-SA 4.0, Hu & Widmer, ISMIR 2023 |
| **MAESTRO v3** | 1,276 International Piano-e-Competition performances, 2004–2018, Yamaha Disklavier | MIDI converted with the **Disklavier curve**: grade Performance (MIDI). An album per year; composer and title from MAESTRO's catalogue | CC BY-NC-SA 4.0, Google LLC |
| **SUPRA Welte-Mignon rolls** (batch 2) | 456 red Welte-Mignon reproducing rolls (1905–1932): Carreño, Paderewski, Hofmann, d'Albert, Busoni, Grieg, Saint-Saëns and many more, from Stanford's archive | the "expressive" MIDI that SUPRA's emulator made from each roll scan, converted with the Disklavier curve: grade **Piano roll (MIDI)**. An album per pianist with 4 or more rolls (41), the rest in "Welte-Mignon rolls: more pianists" | CC BY-NC-SA 4.0, Shi & Sapp, Stanford University Piano Roll Archive |
| **ASAP** (batch 2) | 514 Piano-e-Competition performances that are not also in MAESTRO (the other 553 of ASAP's 1,067 are), Yamaha Disklavier | Disklavier curve: grade Performance (MIDI). An album per composer (14); titles tidied from ASAP's folder names; the pianist named from the file name, which is all ASAP gives | CC BY-NC-SA 4.0, Foscarin et al., ISMIR 2020 |
| **PiJAMA jazz transcriptions** (batch 2) | 2,777 tracks of solo jazz piano from 259 albums (Art Tatum 122, Oscar Peterson 56, Thelonious Monk 31, the Maybeck Recital Series …), transcribed by software from the recordings | the Kong et al. transcriptions (they have the damper pedal), Disklavier curve: grade **Transcribed from audio**. An album per original album; live tracks' venue and date in the notes, with how far two transcription methods agree | CC BY-NC 3.0, Edwards, Dixon & Benetos, TISMIR 2023 |

The Vienna Mozart and Schubert excerpts came without a soft-pedal file
(the pianists did not use it). The importer gives them an empty one.

What the batch 2 sources are like:
- **Rolls** keep the pianist's own timing, as punched while they played.
  Loudness comes from the roll's expression holes, rebuilt by SUPRA's
  emulator: at most 53 levels (MIDI 33–85), so a roll sounds narrower in
  dynamics than a recording until its fit is set. Both pedals are only on
  or off. Many follow a roll's speed changes (up to 156 tempo changes in
  a piece); the conversion follows them.
- **Transcriptions** have the player's timing but loudness guessed from
  the sound, the occasional wrong or missed note, and a damper pedal
  that is only on or off; no soft pedal.
- **ASAP** is like MAESTRO: real pedal depth, but faster moves than SE
  files (100–150 per sample in the pieces tried).

Every on/off pedal is slowed for this piano as it plays ("Pedals",
below).

## What each piece records

So the Library can sort and filter by it, every imported piece carries:
- **collection**: "Vienna 4x22", "Batik plays Mozart", "MAESTRO v3". The
  Library's **All collections** filter uses it;
- **captured on**: the instrument that recorded the playing ("Bösendorfer
  SE290", "Yamaha Disklavier");
- **licence**;
- **curve**: for a converted MIDI file, the velocity curve used;
- **grade**: Native SE, **SE via MIDI**, Performance (MIDI), **Piano roll
  (MIDI)**, **Transcribed from audio** or Sequenced (MIDI). The Library's
  **All sources** filter uses it. "SE via MIDI", "Piano roll" and
  "Transcribed" are set by the importer, because the data alone cannot
  tell them from a Disklavier performance;
- the usual title, composer (surnames, as in the rest of the library),
  performer, year, album and track, and a note naming the source.

A piece's page shows all of these.

## Velocity: two curves

MIDI loudness (1–127) has to become SE velocity (a hammer time, bigger is
softer), and how depends on what made the MIDI file:
- **Bösendorfer's curve** (`se.BosendorferCurve`). It was fitted from 66
  Vienna pairs of the same performance as native SE and as Bösendorfer's
  MIDI: SE ≈ 524.5·e^(−0.02696·MIDI). `TestBosendorferCurveOnVienna`
  checks it against the native files: 99.6% of notes match, and velocity
  is within 1.2% (median; 3.9% for 95% of notes). What is lost is MIDI's
  rounding to 127 steps. Use it for SE recordings exported as MIDI.
- **The Disklavier curve** (`se.DefaultCurve`), fitted from Zenph's paired
  Disklavier and SE renderings. Use it for MAESTRO and other Disklavier or
  general MIDI.

The two agree within about 10%. Uploading a MIDI file in the app uses the
Disklavier curve. `POST /pieces` takes `curve=bosendorfer` for the
other.

## Pedals

MIDI CC 0–127 maps to SE 0–255 in a straight line, for both sources.
Bösendorfer's own converter does exactly this (within 1, over 225,882
damper samples).

### Pedal speed: protecting the mechanism

This is a 1909 acoustic piano with a retrofitted pedal actuator, not a
MIDI instrument. A source whose sustain pedal is only ever "on" or "off"
asks the pedal to go from fully up to fully down in one 10 ms step, and
to drop back just as suddenly. That is a hard knock for the mechanism
(the owner, 2026-11-26). So **every piece is slowed to safe pedal speeds
as it plays** (`piano.SafePedals`):

- **No pedal moves more than 60 (of 255) per sample**: 10 ms for the
  damper, 20 ms for the soft pedal. That is the fastest move in native SE
  recordings: none of the owner's 350 Zenph files goes faster, so they
  play exactly as recorded (`TestSafePedalsLeaveNativeAlone`).
- **An on/off pedal moves more gently still: 32 per sample**, so a full
  stroke takes about 80 ms (damper) or 160 ms (soft pedal), about a
  pianist's usual pace.
- The move is spread evenly around the moment it was recorded (half
  before, half after), so the pedal is half down when the file says
  "down", and the dampers' timing against the notes stays as recorded
  (`se.Pedal.Limit`).
- It applies everywhere the app plays: single pieces, albums, play
  lists and the editor's previews. The files never change.

What the sources hold (largest move in one sample):

| Source | Pedal | Largest step | Slowed? |
|---|---|---|---|
| The owner's Zenph SE files (350) | depth | 60 | never |
| Vienna 4x22 (SE, from a Bösendorfer) | depth | 127 | a few moves |
| MAESTRO (Disklavier) | depth, often on/off | 255 (about 4,200 moves of 200 or more) | yes |
| SUPRA Welte-Mignon rolls | on/off only | 255, every change | yes, the gentle rate |
| PiJAMA jazz transcriptions | on/off only | 255 | yes, the gentle rate |

Stahnke's own MIDI players (Tools → "MIDI file player", "MIDI input")
send pedals as the file or the input has them; the slowing does not
apply there. For a file with an on/off pedal, play the converted piece
from the Library instead.

## Fitting to this piano

Files from other pianos come out a little "all over the place" here:
- SE velocity is a hammer speed, but a Bösendorfer's action and hammers
  are not this Steinway's. That is why Zenph made a separate rendering for
  each instrument;
- a Disklavier's MIDI loudness and pedal depth are its own.

So each collection has a **fit** (Piano → **Fit by collection**). It is
set by ear and applied as the collection's pieces play, albums included.
The files never change:
- **Loudness**, in % hammer speed: +10 is a little louder. Silent presses
  stay silent, and audible notes stay audible;
- **Damper pedal depth** and **soft pedal depth**, in % of the recorded
  depth: 80 lifts the dampers less, 120 more, up to fully.

"Play a piece from it" tries the fit. Now Playing says **"Fitted to this
piano"** while a fit applies. The editor's previews use it too. The fits
are saved in `fits.json` and kept in backups.

Later, if needed: a pedal *threshold* (the depth at which this piano's
dampers start to lift), and fits per performer or per piece.

## Importing

On the PC, with the service running:

```
sepianod datasets -server http://raspberrypi.local "C:\Users\todd\sepiano\datasets\downloads"
```

- `-only vienna,batik,maestro,supra,asap,pijama` chooses collections
  (all six by default), `-n` lists without importing, and `-limit N`
  imports at most N of each.
- Running it again adds nothing twice (the library knows each file) and
  sets the details again.
- Batch 2 expects `supra/catalog.csv` and `supra/x/SUPRA-master/`,
  `asap/new_performances.csv` and `asap/x/asap-dataset-master/`, and
  `pijama/pijama.csv`, `pijama/shortened-names.txt` and
  `pijama/x_kong/midi_kong/`, as the other session unpacked them.

Code: `cmd/sepianod/datasets.go` and `datasets2.go` (the importer), `se.BosendorferCurve` and
`se.Curves`, `library.AddMIDIWith` and `AddFilesWith` (the curve), the
`Piece` provenance fields, `piano/fit.go` and `api/fits.go` (fits).

## Still to do

- A listening test at the piano: a Vienna piece played native, then
  converted from its own MIDI with Bösendorfer's curve.
- Set each collection's fit by ear (after the technician's work).
- More sources, if wanted (Yamaha's free e-Competition files, Saarland
  Music Data), with their own curve if their MIDI differs.
