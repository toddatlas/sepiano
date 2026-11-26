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

## Favorites and "Best first" (2026-11-26)

The owner asked for a favorites star, and for search to favor native SE
files, Steinway ones above all.
- **A star** on every piece, album and play list (in lists and on their
  pages). A piece keeps it in its `.json`; albums and play lists keep
  theirs in `favorites.json`. Both are kept in backups.
- **Zenph's Steinway renderings start starred**: SE files whose instrument
  is a Steinway. It happens once, when the library first opens with
  `metaVersion` 9, and again for new ones as they are imported. Un-starring
  one is never undone.
- **Favorites first** (a chip, on by default, remembered on each device)
  lists the starred ones first in Pieces, Albums and Play lists.
- **Best first**, the default order, goes by how good the data is for this
  piano:
  1. SE files for a Steinway;
  2. other SE files;
  3. SE via MIDI;
  4. performances;
  5. rolls;
  6. transcriptions;
  7. sequenced files.

  "A to Z" and "Newest first" remain.
- **Favorites**, a section of the Library, lists the starred pieces,
  albums and play lists, with its own search.
- Among a piece's renderings, a starred one is the one shown and the one
  an album plays.

## Who is playing: the Piano-e-Competition's pianists (2026-11-26)

MAESTRO names no pianists, and ASAP only short codes. The owner asked for
the competitors' names, and for the winners to be marked.
- The competition's own pages (piano-e-competition.com/midi_YYYY.asp,
  archived by the Wayback Machine) list every competitor's repertoire with
  the competition's MIDI file names.
- A helper matched MAESTRO's 1,276 files to them, by year, competitor
  number and title. Four were checked by comparing the MIDI itself.
  - 1,197 are confirmed by the repertoire lists, ASAP or a content check.
  - 79 are very likely (same competitor number; the title is missing from
    the list). Their notes say so.
  - Labels published by another project (PID-CNN) turned out wrong on 137 files,
    mostly 2008, and were not used.
- ASAP's codes are the competition's file names: 465 of its 514 extra
  performances got full names.
- **Prizes** come from the competition's winners history. A piece's
  `award` reads, for example, "1st prize, Piano-e-Competition 2009". It
  shows as a gold label in lists and on the piece's page, and search finds
  it. **Each year's 1st-prize performances are starred** (106 pieces), so
  with "Favorites first" they come right after the Zenph Steinway files.
- The tables are in `DOWNLOADS/ecomp/` with a README (names and prizes are
  public facts, but the folder stays with the downloads).
  `sepianod datasets -details -only maestro,asap DOWNLOADS` puts them on
  pieces already in the library, without uploading anything.

## How hard a piece works the piano (2026-11-26)

Each key the player holds down is a solenoid kept powered, which makes
heat and wear. Every piece's page now has a **Workload** line
(`piano.MeasureWork`, `GET /pieces/{id}/work`), measured from its files:
- keys held at once on average;
- the most held together for a second, and for ten seconds;
- the longest single hold;
- a level.

Silent presses count, since they hold a key too.

The level compares with what this piano usually plays: the owner's 350
Zenph SE files.
- **light**: within their usual range (average under 2.8 keys, at most one
  key held 10 s, no hold past 10 s);
- **moderate**: up to the most they ever do (average 4.1, five keys for
  10 s, a 19 s hold);
- **heavy**: up to about twice that;
- **very heavy**: beyond.

Measured on samples of each source (average keys held at once: middle
and top tenth; longest hold: middle):

| Source | Average held (p50 / p90) | Longest hold (p50) | Levels |
|---|---|---|---|
| The owner's Zenph SE files (350) | 1.9 / 2.7 | 2.6 s | almost all light |
| Vienna SE (69) | 2.2 / 3.4 | 3.5 s | light to moderate |
| Competition (Disklavier, 150) | 2.0 / 2.6 | 5.6 s | mostly light |
| SUPRA Welte rolls (150) | 2.6 / 3.1 | 4.1 s | light and moderate |
| PiJAMA transcriptions (150) | 4.4 / 6.3 | 6.0 s | mostly moderate and heavy |
| Ampico rolls (150) | 4.5 / 7.0 | 5.4 s | moderate to very heavy |
| 88-note rolls (150) | 4.1 / 5.1 | 1.8 s | mostly moderate |
| Organ rolls, 176-note (60) | 4.3 / 7.0 | 12 s | up to very heavy (a 65 s hold) |

**The 11 holiday organ rolls** (`sepianod work FILES` measures files
outside the library):
- light: Nutcracker "Dance of the Automatons";
- moderate: Noel and Variations, Waltz of the Flowers, Christmas Bells;
- heavy: For Unto Us a Child Is Born, Hallelujah Chorus, Silent Night, He
  Shall Feed His Flock;
- very heavy: O Holy Night, O Little Town of Bethlehem, The First Noel.
  These hold about 9–10 keys down on average, and 10–11 together for over
  10 s.

The figure also measures how much holding a full damper pedal makes
unnecessary (`pedal_free_s`). Under a pedal at 90% or more, a held key
changes nothing in the sound. That is the room the "cool-running
playback" idea could use (docs/ROADMAP.md, idea 2): about 5% of holding
in the Zenph files, but 25–40% in rolls and transcriptions. It needs a
listening test before it is used.

## The competition's original files (2026-11-26)

The owner chose these next. They are the Piano-e-Competition's own
Disklavier PRO recordings, 2002–2018, which the other session found on a
mirror of the competition's site (`DOWNLOADS/ecompetition/`, 2,730
files). MAESTRO is a stripped-down subset of them.
- **Only what MAESTRO lacks is imported**: 1,279 performances, 170 hours,
  including all of 2002. MAESTRO often cuts or joins the competition's
  files differently, so an original counts as new only if:
  - its opening doesn't match a MAESTRO file (the other session's
    catalogue);
  - its entry wasn't matched to a MAESTRO piece by title;
  - none of three 16-note stretches (start, middle, three quarters in) is
    in a MAESTRO file of its year with the same timing, within 15 ms. The
    same piece by another pianist has the same notes but not the same
    timing. This last test removed 344 more.
- **Named from the competition's pages**: pianist (with nationality),
  round, composer and title, for all but 7. Those 7 are missing from the
  archived lists, so their pianist is guessed from the file name and
  their notes say so.
- **Prizes as for MAESTRO**: 136 carry an award, and the 40 by first-prize
  winners are starred (Mei-Ting Sun 2002, Peter Friis Johansson 2014 and
  others).
- Albums "Piano-e-Competition YEAR (originals)", one per year, with
  tracks by pianist. Collection "Piano-e-Competition originals", grade
  Performance (MIDI), the Disklavier curve.
- **What the originals add over MAESTRO is set aside for now**:
  - note-on loudness is the same 7-bit MIDI;
  - the real release speeds have no place in the SE format;
  - the key-position data (polyphonic aftertouch on keys that aren't
    sounding) is not understood well enough to use.

  So there is no gain in re-sourcing the MAESTRO pieces from these.
- `sepianod datasets -only ecomp DOWNLOADS` (`cmd/sepianod/ecomporig.go`);
  it needs `DOWNLOADS/ecomp/` for the names.

## The holiday collection (2026-11-26)

The owner asked (through the other session) for holiday music first, in a
collection or play list, starred. `sepianod holiday -server
http://raspberrypi.local "C:\Users\todd\sepiano\datasets"` imports the
other session's catalogue (`holiday/holiday_catalog.csv`):
- 141 distinct pieces, about 12 hours:
  - Christmas 102, Nutcracker 25, Winter 14;
  - sources: Terry Smythe's Ampico, Duo-Art and Welte rolls and 88-note
    rolls; ATEPP, Aria-MIDI and GiantMIDI transcriptions; PiJAMA jazz;
    MAESTRO's Pletnev Nutcracker suites.
- Pieces already in the library keep their details and are only starred.
- Every piece is starred, and the starred play lists "Holiday: Christmas",
  "Holiday: Nutcracker", "Holiday: Winter" and "Holiday: everything" hold
  them, best sources first.
- **The 11 organ rolls (176-note) are left out** until the owner decides.
  Written for a pipe organ, they hold many keys down for a long time.
- Running it again adds nothing twice and refreshes the play lists.

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
