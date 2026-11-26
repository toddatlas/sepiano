# Roadmap: the next stage (2026-11-26)

The owner's six ideas, researched, with what is already known, what each
needs, and a suggested order. Tick items off as they are done. The owner
decides the order; "Suggested priority" is advice.

**Where things stand.** Playback, recording, the Key check, corrections,
every suite program and the piano-roll editor are built. Fine-tuning
playback and recording accuracy now waits for a piano technician: the
action needs work first. So this stage is about the app, the content, and
understanding the hardware.

## Suggested priority

| # | Work | Why now | Needs the owner? |
|--:|------|---------|------------------|
| 1 | **Volume in the app** (idea 2), and backups of the library | Protects the piano and the recordings; small | One listening test |
| 2 | **Hardware picture for the designer** (ideas 6 and 1) | He is engaged now; the sensor answer lies in the hardware | Board photos; send the questions |
| 3 | **Bigger, easier editor** (idea 3) | Usable now; no piano needed | Feedback afterwards |
| 4 | **Libraries of good content** (idea 4) | Biggest gain in what the piano can play | Choose sources; download |
| 5 | **Audio to the piano** (idea 5) | Builds on 4's converter | A PC with Python; rights to the audio |
| 6 | **After the technician:** recalibrate, repair notes that fail, re-measure the sensors, then corrections | Only meaningful once the action is regulated | At the piano |

Items 1, 3 and most of 4 can be built in one go without stopping to ask.

---

## Idea 1: the key sensors (release timing that varies by key)

**Question:** do the key sensors have a calibration stored inside the
instrument, which software could adjust?

**Answer so far (2026-11-26, from the firmware already read):** no setting
for the sensors exists anywhere software can reach.

- The 2 KB EEPROM is fully mapped (14 sections, docs/SUITE-PROGRAMS.md).
  None of it is about the key sensors: it holds the strike, lead-time,
  release and silent-press calibration of the *actuators*, the trims and
  the player's constants.
- The firmware does not measure the sensors itself. A separate
  sensor-scanning circuit hands the computer finished events through ports
  28H–2FH (FW#3–#15):
  - a key number (7 bits, port 2BH), added to the lowest key;
  - the kind of event, as status bits in port 2AH (bits 4–7). Which bit
    is key down, key up or hammer is not fully traced yet;
  - for hammer events, a **10-bit timing count** (port 29H plus two bits
    of 2AH). This is very likely the SE velocity itself: a time, which is
    why bigger is softer;
  - port 28H holds status bits: the keyboard interlock (bit 3), events
    waiting (bit 0), an error (bit 2).
- **Nothing ever writes to the sensor circuit.** No firmware routine and
  none of the 24 suite programs sends it anything (every program reaches
  the hardware only through the firmware's routines). So the height at
  which a key counts as "up" is set in the sensor hardware. It may be fixed
  by components, or adjustable with trim pots on the sensor boards.

**What this means:**
- [ ] **Look at the sensor boards** in the photos for trim pots, DIP
      switches, or a chip with its own program (an EPROM or
      microcontroller). Trim pots would mean a technician can set each
      sensor's sensitivity, with no new parts.
- [ ] **Ask the designer** (questions below). He will know how the scanner
      decides "up" and "down", and whether it can be adjusted.
- [ ] **Software can still compensate** after recording. The Key check
      measures each key's release against its chord neighbours, and the
      per-key release corrections (built) shift them. A sensor set too
      high or too low gives a time error that depends on how fast the key
      rises. Keys mostly rise freely at their own speed (section 7 of the
      table measures it), so a fixed per-key offset should correct most of
      it. To do after the technician's work: re-run the Sensor test, then
      apply the suggested release corrections.
- [ ] Optional: trace FW#11–#15's event bits fully, by recording while
      watching which bits arrive (with `record.exe`'s own code as the
      guide). Needs no new hardware.

**Other storage that shapes playback and recording** (the owner's
question):

| Where | What | Can we read or change it? |
|-------|------|---------------------------|
| EEPROM, 2 KB | the actuator calibration, trims, player constants, settings | Read (backups) and write (restore), done |
| Firmware ROM, CPU board | boot loader, monitor, hardware routines (V43.47, 2009) | Read 2026-11-23; not changeable from software |
| **The programs uploaded each time** (`play.exe`, `record.exe`) | All the logic of playing and recording: scheduling, holds, releases, the level | They come from the PC (now the Pi) every session. A modified copy *could* change behaviour, used privately from the owner's own copy. Not planned; noted as a possibility |
| Sensor scanner boards | where "key down/up" and hammer timing are decided | Hardware: photos and the designer |
| Actuator driver boards | per-key mode and value registers, set at every note | Volatile, set by the player; maybe current settings on the boards |

**Questions for the designer** (draft to send):
1. How does the key-sensor scanner decide a key is up or down: a fixed
   threshold, one per board, or one per key? Is it adjustable (trim pots,
   jumpers, a programmable part)?
2. Is the hammer value a time between two positions? Which positions, and
   at what clock?
3. The reflective sensors vary key to key, and LEDs age. Is there a
   recommended way to restore them evenly: re-aiming, cleaning,
   component changes?
4. Owners report overheating when the player's level is lowered. Is that
   known, and what causes it?
5. Is there a newer firmware, player or recorder than V43.47 / V50.45 /
   V30.44?
6. What would he most like in a schematic: the sensor boards first?

## Idea 2: volume (does the app use the piano's level command?)

**Answer: yes, today it does.** The app's − and + press the player's own
Down and Up keys, exactly as the old suite did ("LEVEL=-1" … "-16").

What the decoded player does with it: before each note, `play.exe` scales
the note's velocity by a factor from section 11 of the table, then looks up
the strike as usual (docs/SUITE-PROGRAMS.md, "How the player drives a
key"). It changes nothing else that the code shows: holds and releases are
the same. So nothing in the decoded code explains the overheating owners
report. It may lie in the drive hardware, or in a different version.

Either way, **doing the volume in the app is easy, and removes the
question**: the piano's level stays at 0 and never changes.

- [x] **Both methods, side by side** (built 2026-11-26, the owner's
      wish): "Volume set by: On the Pi | Piano's own" on the volume card
      and in Settings, remembered on the Pi. A switch while a piece plays
      applies from the next piece. Compare them; refine the Pi's curve
      before dropping the piano's own.
- [x] **App volume** (built 2026-11-26): scale each note's velocity on the Pi as the piece is
      sent, using the same machinery as software voicing (already built,
      already used for every piece). The player's level stays at 0.
  - The piano reads a few seconds ahead, so a change takes a few seconds
    to be heard (as now, roughly).
  - Silent presses stay silent, and audible notes stay audible (capped at
    639, as voicing does).
  - The curve can be gentler than the player's: for example, quiet
    passages quieter without the loud ones losing their shape.
- [ ] **A "solenoid work" figure per piece:** how long keys are held down
      in total. It is a stand-in for heat, to compare pieces and settings.
- [ ] **Idea: cool-running playback.** While the damper pedal is fully
      down, a key held down does nothing for the sound: the pedal is
      already holding its damper up. If a note ends before the pedal next
      comes up, the key can be let go as soon as it has struck. The sound is
      the same, and the solenoids work far less, which means less heat and
      less wear. It needs a listening test, and only fully pressed pedal
      counts.

## Idea 3: a bigger, easier editor

Everything is drawn small, even on a laptop. To do (no piano needed):
- [x] A **size setting** (Comfortable / Large / Extra large; built 2026-11-26) that scales
      the rows, the lanes, the text and the buttons; remembered.
- [x] **Fit to the keys used** (K): rows only for the range the piece uses, so
      they fill the height.
- [x] **Full screen** (F; hides the side bar), and keyboard zoom (+ and −).
      Pinch zoom on a trackpad (Ctrl + wheel) was already there.
- [x] **Zoom buttons** (2026-11-26): Time (in and out, every lane
      together) and Notes (taller and shorter rows) in the toolbar; each
      lane (loudness, damper, soft) has its own Taller and Shorter buttons
      at its top right, up to 4 times, for fine pedal and loudness work.
- [ ] Bigger note handles and lane drag areas; larger fonts in the panel.
- [ ] Then the owner tries it and says what is still awkward.

## Idea 4: broad libraries of good content

**The best free sources, best first.** These are personal, non-commercial
licences, and nothing from them goes into the public repository.

| Grade | Source | What | Notes |
|-------|--------|------|-------|
| **Native SE** | [Vienna 4x22 Piano Corpus](https://datasets.mdw.ac.at/datasets/dataset/98ea25fa-2468-43ff-929b-3c926e163583) (mdw, DOI 10.21939/4X22) | 22 pianists × 4 excerpts, recorded 1999 on a **Bösendorfer SE290** | Comes with the SE files themselves (.KB/.LP/.SP): plays as is. CC BY 4.0. Also MIDI made with Bösendorfer's own software |
| SE, as MIDI | [Batik plays Mozart](https://github.com/huispaty/batik_plays_mozart) (ISMIR 2023) | 12 Mozart sonatas complete, about 4 h, Roland Batik, on a Bösendorfer SE290 | Converted from SE at 1.25 ms. The Vienna pairs show how Bösendorfer's software converts, so this can be turned back very closely |
| Performance capture | [MAESTRO v3](https://magenta.tensorflow.org/datasets/maestro) (Google Magenta) | About 200 h of the International Piano-e-Competition, on Yamaha Disklaviers | Velocities and all three pedals. MIDI-only download is about 56 MB. CC BY-NC-SA 4.0. Composer and title metadata included |
| Performance capture | Yamaha's free "Signature MIDI Collection" (e-Competition 2002–2009); [Saarland Music Data](https://resources.mpi-inf.mpg.de/SMD/SMD_MIDI-Audio-Piano-Music.html) | Disklavier performances | Check that the links still work |
| Transcribed from audio | GiantMIDI-Piano (ByteDance), ATEPP | Thousands of pieces transcribed from recordings | Lower accuracy; good for breadth |
| From the score | piano-midi.de, Kunst der Fuge, Mutopia | Typed in from scores | Mechanical; little expression |

**Downloaded (2026-11-26, by the owner's other session, handed over to
this project):** in `C:\Users\todd\sepiano\datasets` on the owner's PC.
That folder is outside the repository, and nothing in it may be
committed. Its `README.md` has the details:
- MAESTRO v3 MIDI: 1,276 performances, with the composer/title/year
  catalogue;
- Vienna 4x22: 113 native SE290 `.KB/.LP/.SP` sets and their MIDI
  versions;
- Batik: 36 movements as MIDI.

Findings there, to verify in the Go code:
- **Bösendorfer's SE→MIDI velocity curve**, fitted from 66 Vienna pairs:
  SE ≈ 524.5·e^(−0.02696·MIDI), within 1.6 SE units over MIDI 15–96;
- its pedals map 0–255 ↔ CC 0–127 linearly;
- converting Vienna's MIDI back with that curve: onsets exact, releases
  95% exact, velocity within 1.4% on average, and 1.9% of the native
  notes missing from the MIDI;
- MAESTRO: 384 or 480 ppq, one tempo each, all three pedals, and two
  files with repeats so fast that releases must be clamped to the next
  strike.

To do:
- [x] **Import Vienna 4x22's SE files** (native, so a straight import;
      built 2026-11-26: `sepianod datasets`, docs/LIBRARIES.md).
- [x] **Grade every piece in the app** (built 2026-11-26: Native SE,
      Performance (MIDI), Sequenced (MIDI); a "Sources" filter in the
      Library). Telling SE-converted MIDI and transcriptions apart needs
      the source; to do with the importers. The grade is
      found from the file:
  - timing on a grid means typed in;
  - few distinct loudnesses means typed in;
  - pedal only on or off means typed in or older;
  - the source, where known.

  Then a Library filter: "native SE only", "performances only".
- [x] **A better converter**, one velocity curve per source (built:
      `se.BosendorferCurve`, checked on the Vienna pairs):
  - Bösendorfer's own SE→MIDI curve, fitted from the Vienna pairs and
    turned back, for Batik and similar files;
  - the Disklavier curve, already fitted from Zenph's paired renderings
    (`se.DefaultCurve`), for MAESTRO and the e-Competition;
  - and for every source: continuous sustain-pedal depth (half-pedal),
    the sostenuto and soft pedals, exact timing, re-strikes, and very
    soft notes kept clear of the silent threshold.
- [x] **Bulk import with metadata** (built: MAESTRO from its zip and
      catalogue, Batik, Vienna; each piece records its collection,
      instrument, licence and curve, filterable).
- [ ] Listening tests at the piano once regulated: the same piece as
      native SE and as converted MIDI.

## Idea 5: audio (WAV, MP3, a video) to the piano

**It is possible today, with good accuracy for clear solo piano.**
- The leading open tools: [Transkun](https://pypi.org/project/transkun)
  (2024, best published accuracy on MAESTRO v3), and ByteDance's
  [high-resolution transcription](https://arxiv.org/pdf/2010.01815) (2020,
  notes and sustain pedal; pedal onsets about 92% right). GiantMIDI-Piano
  was made this way, from YouTube audio.
- On clean studio recordings they find about 97% of notes. Loudness comes
  out relative, learned from Disklavier data. The pedal is found as on or
  off, not its depth. Old, noisy or reverberant recordings do worse.
- **Resolution:** the tools learn from 7-bit MIDI loudness, so writing SE
  directly gains little true resolution. Going straight to SE does avoid
  one rounding step, and uses this piano's curve.

To do:
- [ ] **A PC tool `wav2se`:** run Transkun on a recording, convert with the
      Disklavier curve, and upload to the app graded "transcribed". It
      needs Python on the PC. The Pi 4 could do it too, but slowly; fine for
      overnight batches.
- [ ] Later, if wanted: upload audio in the app, and the Pi passes it to
      the PC.
- Rights: use recordings you own or may use. Downloading from YouTube is
  against its terms; the owner decides.

## Idea 6: the piano's architecture and a schematic

The designer has asked for an electrical schematic. The key sensors are
his main concern.

What the firmware already tells us (a block diagram can be drawn now):
- a Z80 CPU board on Multibus, firmware "STAHNKE BOOTSTRAP LOADER V43.47"
  (2009);
- the **sensor scanner** (ports 28H–2FH): key and hammer events, the
  keyboard interlock;
- the **actuator drivers** (ports 30H–34H): 88 keys in groups of 4, five
  registers per group, a 2-bit mode and an 8-bit drive value per key;
- the pedal solenoids and sensors (ports 30H, 31H, 33H/34H);
- an 8254-style timer at 2.048 MHz (38H–3BH) and a second timer (CAH,
  CBH);
- an 8259-style interrupt controller (D8H, D9H);
- two serial channels: the PC link (BAH, BBH) and MIDI (B8H, B9H);
- the 2 KB EEPROM (35H–37H);
- registers not yet identified at 3CH, E0H, F0H.

To do:
- [ ] **System block diagram** from the firmware (above), as a clear page
      to share.
- [ ] **Board photos from the owner:** each board, both sides, straight
      on, in good light, chip markings readable, plus each connector and
      the cables between boards. Name each photo by where the board sits.
- [ ] **Per-board diagrams:** identify the chips from the photos (their
      part numbers), and map connectors to the firmware's ports.
- [ ] **The key-sensor board schematic first:** traced from both sides'
      photos, with a few meter checks by the technician where traces hide.
- [ ] Share with the designer, with the questions under idea 1.

## After the technician (idea 1 continued, and the owner's note on calibration)

- [ ] Back up the table, then **recalibrate** (cal, silent) over the whole
      range.
- [ ] **Repair notes the calibrator cannot do.** The app lists suspect
      keys: today E1 and C#3 sit at 255, the gentlest strike, in the soft
      rows. It proposes values from the neighbours, shows the difference,
      and writes it with a backup and a read-back check. This is
      docs/SETTINGS-EDITOR.md steps 3 and 4, deferred by the owner until
      now.
- [ ] Re-run the Sensor test, and apply the suggested corrections.
- [ ] Formal playback and recording tests.

## Idea 7: play lists you build (asked 2026-11-26)

**What exists:** play lists made from the owner's folders and `.bat`
files (33 on the Pi). The app lists, plays and deletes them, and search
finds them alongside albums ("1 folder or play list"). The service can
already store new ones (`POST /playlists`, `PUT /playlists/{id}`). The
app has no way yet to make one, or to add pieces to one.

To do:
- [x] **New play list** (Library → Play lists): name it. (Built 2026-11-26.)
- [x] **Add to a play list from search:** each search result, and each
      piece's page, gets "Add to play list…", with a menu of the lists and
      "New play list". Several results can be ticked and added at once
      (like Recordings' tick boxes).
- [x] **Edit a play list:** up and down buttons, remove a piece, rename.
      (Dragging to reorder: later, if wanted.)
- [x] **In search, like albums:** play lists you build show in search
      results with "Play all", as the folder ones do now. They are told
      apart from folder lists by a label.
- [x] Play lists keep working when a piece is hidden; the list says so
      ("hidden" beside it).

## Idea 8: backups to the desktop and Google Drive (asked 2026-11-26)

**What exists:** Status → "Back up library" downloads a zip of the
pieces and `settings.json`. It leaves out:
- the play lists;
- voicing and corrections;
- the piano's settings backups (EEPROM);
- the recordings still in staging;
- the installed programs.

**Goal:** everything on the Pi saved to the owner's computer, into a
folder of their choosing. If that folder is inside Google Drive (Google
Drive for desktop keeps a folder such as `G:\My Drive` in step with the
cloud), the backup goes to the cloud too.

To do:
- [x] **"Back up everything"** (built 2026-11-26, docs/BACKUPS.md): one zip with all of it:
  - every piece and its metadata;
  - play lists, settings, voicing, corrections;
  - the EEPROM backups;
  - the installed programs (the owner's own copies; never anywhere
    public);
  - a list of what is inside, with the date.

  The browser asks where to save (set the browser to ask), for example
  the Google Drive folder.
- [x] **Restore from a backup** (built): adds what is missing, never
      changes or removes anything; every file checked first.
- [x] **Automatic backups**: the PC way, set up 2026-11-26 (nightly at 3
      a.m. into `G:\My Drive\Piano backups`). The Pi-to-Drive way stays
      an option:
  - **From the PC:** a small scheduled task on the Windows PC fetches
    the backup each night into the chosen folder (for example in Google
    Drive). Simple, and nothing to sign in to. It runs only when the PC
    is on.
  - **From the Pi, straight to Google Drive** (with `rclone`, after a
    one-time sign-in): nightly, even when the PC is off. Keeps the last
    few.
- [ ] Only what changed: after the first full backup, a nightly run adds
      only new or changed files, so it stays quick (the pieces are small,
      about 48 MB in all on 2026-11-26).
- [x] The Status page shows when the last backup was made, and warns if
      it is more than a week old.

## More ideas

- [ ] **Back up the library automatically**: now idea 8. The Pi's SD card
      holds the only copies of the recordings made here, and SD cards fail.
- [ ] **A settings backup at the start of each session**, kept for a
      while, so any change in the table is caught early.
- [ ] **A heat watch:** a cheap USB temperature probe near the solenoid
      rail, shown in the app, with a warning.
- [ ] **Practice tools:** record, then compare with the original (already
      possible in the editor with "Show the original"); loop a passage.
- [ ] The iPhone app (later, as planned).
