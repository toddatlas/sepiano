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

The Vienna Mozart and Schubert excerpts came without a soft-pedal file
(the pianists did not use it). The importer gives them an empty one.

## What each piece records

So the Library can sort and filter by it, every imported piece carries:
- **collection**: "Vienna 4x22", "Batik plays Mozart", "MAESTRO v3". The
  Library's **All collections** filter uses it;
- **captured on**: the instrument that recorded the playing ("Bösendorfer
  SE290", "Yamaha Disklavier");
- **licence**;
- **curve**: for a converted MIDI file, the velocity curve used;
- **grade**: Native SE, **SE via MIDI**, Performance (MIDI) or Sequenced
  (MIDI). The Library's **All sources** filter uses it. "SE via MIDI" is
  set by the importer, because the data alone cannot tell it from a
  Disklavier performance;
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

- `-only vienna,batik,maestro` chooses collections, `-n` lists without
  importing, and `-limit N` imports at most N of each.
- Running it again adds nothing twice (the library knows each file) and
  sets the details again.

Code: `cmd/sepianod/datasets.go` (the importer), `se.BosendorferCurve` and
`se.Curves`, `library.AddMIDIWith` and `AddFilesWith` (the curve), the
`Piece` provenance fields, `piano/fit.go` and `api/fits.go` (fits).

## Still to do

- A listening test at the piano: a Vienna piece played native, then
  converted from its own MIDI with Bösendorfer's curve.
- Set each collection's fit by ear (after the technician's work).
- More sources, if wanted (Yamaha's free e-Competition files, Saarland
  Music Data), with their own curve if their MIDI differs.
