# Stahnke's performance editor (`edit.exe`)

Decoded 2026-11-24 from its Z80 program (a helper session traced it; the
EEPROM writes at 680FH, 682DH and 685DH and the silent path for velocity 0
in `play.exe` at 1044H were checked again by hand). The app runs it as a
tool (Piano → Tools → Stahnke's editor), with its command reference beside
the console, as a **settings-changing** tool: its LOUDER, SOFTER and LEVEL
commands write the per-key trims. Corrections it brought to the other docs
are made there (velocity 0 is a silent press; key 0 is the sostenuto pedal;
header byte 1 is read).

## As its code reads

How Stahnke's INTERACTIVE PERFORMANCE EDITOR V50.45 behaves, read from its
Z80 program (27,406 bytes at 0100H–6C0DH). Its commands are reached through
a table, so a plain trace covers 14% of it. With the table's 45 handlers,
the player's interrupt routine and its two pedal tasks given as entry
points, `sesuite` traces 95%:

```
sesuite -dis edit -entries 31E6,0A65,0C48,335B,0D05,3473,1EA4,3A0C,3C20,0EFC,0746,08A9,11B4,1224,1F63,12AC,1341,483E,4344,45F8,1460,13F5,1822,187A,1BCA,18B6,18DD,1DF1,1962,3F14,196B,1856,1A77,4966,4A87,4D15,19E2,1AB3,1BF1,1C1E,2021,2028,202F,28E7,2B9B,2CD5,30BF SUITE
```

What is left untraced is text, the command tables and a few bytes of data.
Addresses below are in the editor unless they say `play.exe`.

**In short.** The editor works through a performance from start to end, a
"workspace" of up to about 28 seconds at a time. It plays the piano: note by
note, chord by chord, or in real time with both pedals. It edits single
events (loudness, key, time, delete, insert), inserts sostenuto and cue
events, cuts or stretches time, and edits the pedal samples. **Three
commands, and the Up and Down arrow keys, write the piano's EEPROM**
(the per-key velocity trims). At the end it either saves the whole
performance over the original (`END`) or throws the edit away (`QUIT`).

### Starting

1. It never asks for a file name and takes no argument. It edits the
   performance the host was started with (only file requests 10H–16H; no
   set-name, get-name or get-arg).
2. It prints its banner, opens the performance (10H, 0196H) and reads the
   two header bytes (0199H–01A2H). If either of the top two bits of byte 0
   is set, it quits at once, without a message (01A8H).
3. It skips the header's extension (01ADH–01C0H) and reads the header text
   up to the first `7F`, **printing it as it goes** (01C3H–01ECH). The text
   is kept, at most 512 bytes, as the "heading".
4. It creates `name.kb$`, `.lp$`, `.sp$` (12H, 01EFH) at once, before any
   command, and writes the two header bytes and `00 00` (01F2H–0206H). The
   input's extension bytes are not copied.
5. It fills the first workspace (218BH), prints the status line (the `D`
   command, 0EFCH) and the prompt `*` (02B9H).

### The workspace and segments

The workspace is three buffers in the Z80's memory, holding the data in file
form:

| Buffer | Where | Size | Filled up to |
|--------|-------|-----:|--------------|
| key events | 6C77H, count at 7253H | 1,500 bytes | 1,245 bytes |
| loud (damper) pedal | 7255H, count at 7E0DH | 3,000 samples (30 s) | 2,777 samples (27.8 s) |
| soft pedal | 7E0FH, count at 83EBH | 1,500 samples (30 s) | 1,389 samples (27.8 s) |

`fillWS` (218BH) reads one key event at a time (read-perf 11H, in blocks of
1024, 512 and 256 bytes) until a buffer reaches its limit. After each event
it reads the pedal samples up to that time. So a workspace holds about 28
seconds, or less when the playing is dense: 1,245 bytes is roughly 200–250
notes. Reading the end marker `FF` sets "last segment" (21C3H).

Three times describe it, all in .kb ticks counted from the start of the
piece:

- **FIRST** (83EFH): the time of the first event in the workspace;
- **LAST** (83F7H): the end of the workspace. The status line shows LAST − 1;
- **TIME** (83F3H): the cursor. Most commands act at TIME. It must stay in
  FIRST…LAST − 1.

**It only goes forward.** `FILL` and `W` write everything before a time to
the output (`flushTo`, 2265H), load more after it, and add one to the
**segment** number (83FCH). Written data can't be reached again in this
run. The first event of the workspace is locked too: the event before it
has already been written. So "… IMPOSSIBLE AT FRONT OF WORKSPACE" is
refused at TIME = FIRST, or on that first event.

### Saving

- Output goes to `name.kb$`, `.lp$`, `.sp$` as the workspace moves on
  (write-perf 13H, 1 KB blocks for the keys).
- **`END`** (11B4H): it writes the heading (if still in segment 0), then
  moves through every remaining segment unchanged, writes the last
  workspace, closes (14H) and **commits (15H): the edit replaces the
  original files**.
- **`QUIT`** (1962H): it closes (14H) and **aborts (16H)**. The `$` files
  are deleted and the original is untouched.
- A fatal error (017BH, firmware entry FW#2) ends it without a commit.

What the output looks like:

- **It is always scrambled, with a random seed.** The seed is four bytes
  made from the Z80's R register (5888H–58CFH). Only the fourth byte is
  never 0, so the normal keystream (not the "alt" one) applies. The
  scrambler is the one in docs/SE-FILE-FORMAT.md. `se` reads such files.
- The two header bytes are copied; the extension is dropped; the heading
  is written when segment 0 leaves the workspace.
- It writes events up to and including `FF`. Zenph's 19 padding bytes are
  not copied.
- The pedal files end with the sample that covers the end marker, about
  1 s shorter than Zenph's.

### Typing commands

The line reader (0467H):

- **Enter** ends the line.
- **Esc** cancels it. It echoes `$` and prompts again.
- **Backspace** erases one character.
- `\` is kept and starts a new screen line (used for headings).
- `[ ] { }` and control characters are ignored.
- A line holds at most 512 characters; past that it beeps.
- Lower case is fine.

Commands are matched against the table at 0318H, in order (02C2H–02F5H).
There are two kinds:

- **Exact**: the whole line is the command, e.g. `S`, `END`, `SEARCH`.
- **With a value**: the command's letters are followed directly by the
  value (`T`, `W`, `R`, `B`, `F`, `AI`, `AN`, `AT`, `IN`, `IR`, `ALERT`,
  `PREFACE`, `SPLICE`, `HEADING`).

An empty line does nothing. Anything else prints "ILLEGAL COMMAND".

**Spaces matter.** The number reader (0662H) doesn't skip spaces before a
number, and the line must end right after the value. So:

- write `T100` or `T=100`, not `T 100` or `T= 100`;
- `IN=60,80,50` works, `IN 60, 80, 50` doesn't;
- leave no space at the end of the line: `S ` is "ILLEGAL COMMAND".

**Times** (in `T`, `W`, `AT`, `IN`, `IR`, `SPLICE`, `PREFACE`, and wherever
TIME is printed) are in **hundredths of a second** while MASK is on, which
is the default. The editor multiplies by 8 to get ticks (0607H) and
divides by 8 to print (2033H). With MASK off, times are raw 1/800 s ticks.

**Special keys** (0x00 then a scan code) type ready-made text (054FH,
059BH). A text ending in Enter is sent at once:

| Key | Types | Does |
|-----|-------|------|
| F1 | `p` Enter | play (`P`) |
| F2 | `255` Enter | enters 255 (full pedal in `AL`/`AS`, or after `AI`) |
| F3, F4 | `at-1`, `at+1` Enter | moves the selected event 1/100 s earlier or later |
| F5, F6, F7, F8 | `bbbbbbbbbb`, `bbbbb`, `bb`, `b` Enter | back 10, 5, 2, 1 s |
| F9, F10 | `f`, `ff` Enter | forward 1, 2 s |
| codes 05H, 06H (probably F11, F12) | `fffff`, `ffffffffff` Enter | forward 5, 10 s |
| **Up** | `LOUDER` Enter | **writes the EEPROM** (below) |
| **Down** | `SOFTER` Enter | **writes the EEPROM** |

The reader masks every key with 7FH (6B45H). F11 and F12 arrive from a PC as
0x00 0x85 and 0x00 0x86, so they would become 05H and 06H; that is why I
think 05H and 06H are F11 and F12. `host/keys.go` maps F1–F10 only.

### The status line (`D`)

For example (the numbers are made up; the layout is the code's):

```
SEGMENT=0, TIME=0, FIRST=0, LAST=2776
LP=0, SP=0, RATE=100, SHARPS, LOUD, MASK
```

`LP` and `SP` are the pedal samples at TIME. After them it prints the
note-name setting, then ", EVENT" when an event is selected, then the
switches that are on: LOUD, MASK, MUTE, SOFT, UNIFORM. "LAST SEGMENT" follows
on its own line once the end of the piece is in the workspace. It is
printed at start, after `FILL` and after `W`.

Defaults (0209H–02A5H):

| Setting | Default |
|---------|---------|
| LOUD (damper pedal plays) | on |
| SOFT (soft pedal plays) | **off** |
| MASK (times in 1/100 s) | on |
| MUTE, UNIFORM | off |
| note names | sharps |
| RATE | 100 |
| PREFACE | 4 s |
| ALERT | 400 |

### Moving around

| Command | Does |
|---------|------|
| `T` | prints TIME |
| `Tt`, `T=t` | TIME = t (must be in FIRST…LAST − 1, else "ILLEGAL TIME"). `T0` goes to FIRST |
| `T+t`, `T-t` | moves TIME by t |
| `B`, `BB`, `BBB`… | back 1 s per B, not before FIRST (1EA4H; step 800 ticks at 8819H) |
| `F`, `FF`… | forward 1 s per F, not past LAST − 1 (1F63H) |
| `FILL` | next segment: writes all but the last 4 s, loads more, shows the status line. "LAST SEGMENT" at the end (1224H) |
| `Wt`, `W=t` | winds forward to time t: writes everything up to 4 s before t, loads, TIME = t, shows the status line. Within reach it acts like `T`. It can't go back (1C1EH) |

`T`, `B` and `F` unselect the selected event and print `TIME=`. `FILL`
and `W` print the status line; a selected event stays selected while it is
still in the workspace (2378H).

### Looking and listening: the editor plays the piano

The editor calls the actuator, sensor and pedal entries of the firmware:

- FW#16–#22, the actuators;
- FW#6–#14, the sensors;
- FW#24 and FW#26, the pedals;
- FW#71, the velocity lookup.

So **it moves the piano**. There are two ways to listen.

**Step by step: `S`, `G` (or `L`), `SEARCH`** (step engine 24EDH).

- Stepping starts at the selected event, or at the first event at or after
  TIME.
- Each event is printed:
  - a press: `TIME=1234, NOTE=60 (C), IHV=120 (P)`;
  - a release: `TIME=1250, NOTE=60 (C)`;
  - a marker: `TIME=…, SOSTENUTO ACTUATE`, `…, SOSTENUTO RELEASE`,
    `…, CUE MARK`.
- Fillers (key 127) are skipped.
- **IHV** is the event's SE velocity ("ILLEGAL IHV" names it so). Its
  label comes from a table at 29D6H. An IHV of 0 is shown as "(SILENT)":

  | IHV | Label |
  |-----|-------|
  | 256 or more | (PPP) |
  | 181–255 | (PP) |
  | 128–180 | (P) |
  | 91–127 | (MP) |
  | 64–90 | (MF) |
  | 45–63 | (F) |
  | 32–44 | (FF) |
  | 1–31 | (FFF) |

- The note name is in sharps, in flats (`K`), or in German (Cis, Des, H,
  B), set by the table's "German note names" setting (section 13).
- Each press is **really struck**, then held down:
  - it uses the "uniform" velocity of the table's section 11 (80 on this
    piano), plus the offset (16) when MUTE is on, **not the recorded IHV**
    (2814H–2875H);
  - the key comes back up at its release event (2774H–27ACH).

  Pedals are not played while stepping.
- `S` (19E2H) shows one event per step.
- `G` and `L` (both 12ACH) show a chord per step:
  - presses within 48 ticks (60 ms) of the first (the word at 24EBH), until
    a key repeats;
  - or a run of releases.
- Keys while stepping (289FH):
  - **Space**: next;
  - **Enter**: stop and **select** the event just shown (TIME = its time;
    prints `TIME=… EVENT`);
  - **Esc**: stop, TIME unchanged.

  After 30 s with no key it stops as if Esc. At the end of the workspace it
  prints "END OF WORKSPACE". When stepping ends, every key is let go.
- `SEARCH` (1856H → 4BA8H) finds the first press at or after TIME with IHV 0
  or IHV over 400 (fixed: 4C9EH). It moves TIME there and starts `S` on
  it. Otherwise it prints "NO SOFT OR SILENT NOTES". Press Enter on the
  found note to select it, then fix it with `AI`.

**In real time: `P` and `I`** (2A10H).

- Both play the workspace through a copy of `play.exe`'s scheduler
  (5C3FH).
- Notes use their recorded IHV, the table's lookup, silent presses and the
  release levels.
- Separate tasks feed the damper pedal (2CD5H) and the soft pedal (2E56H),
  smoothed in four steps per sample. Section 11's words 2 (22 ticks) and
  4 (120 ticks) send each pedal that much early.
- Sostenuto events are obeyed (below).
- Playing starts PREFACE before TIME (never before FIRST) and runs to the
  end of the workspace.
- `I` (13F5H) also pauses by itself on reaching TIME (2C78H), so you hear
  the lead-in to the cursor.
- Keys while playing (2BCAH–2C33H):
  - **Space**: pause. All keys let go; prints `STOP AT TIME=t`. Then
    Space plays on, Enter or Esc ends; 30 s idle is Esc;
  - **Enter**: stop, and TIME becomes the stop time;
  - **Esc**: stop, TIME unchanged.

Settings that change only how it sounds, never the data:

| Command | Does |
|---------|------|
| `LOUD` | damper pedal on/off while playing ("DISABLE/ENABLE LOUD PEDAL"; 2E12H) |
| `SOFT` | soft pedal on/off while playing ("ENABLE/DISABLE SOFT PEDAL"; off at start; 2F9CH) |
| `U` | UNIFORM: every note at section 11's uniform velocity, 80 (5EB9H) |
| `M` | MUTE: every velocity + section 11's offset, 16, so all a little softer; also in stepping (5EC7H, 281BH) |
| `R`, `R=n` | RATE, tempo in percent, 40–120 ("ILLEGAL RATE"). The clock adds RATE each tick and advances one tick per 100 (2C97H) |
| `PREFACE`, `PREFACE=t` | how far before TIME `P` and `I` start: 0–16 s, default 4 s |
| `ALERT`, `ALERT=n` | while playing, a note with IHV over n or IHV 0 also plays a short note on the top key (C8 here) at velocity 80, as an audible flag (30F2H–317DH). 0–1023; 0 turns it off; default 400 |
| `K` | note names in sharps or flats ("DISPLAY SHARPS/FLATS") |
| `MASK` | times in 1/100 s (on) or in ticks (off) ("MASK ON/OFF") |

### Changing notes

Most edits need a **selected event** (83EDH, ", EVENT" on the status line):

- choose it with Enter while stepping (`S`, `G`, `SEARCH`);
- `IN`, `IC`, the sostenuto commands, `AT` and `RESTORE` select what they
  made;
- moving TIME unselects it.

| Command | Needs | Does to the data |
|---------|-------|------------------|
| `AIn`, `AI=n` | a press | sets its IHV to n: 0 or 4–1023 ("ILLEGAL INTENSITY"). Stored in 1 or 2 bytes as the format says (31E6H) |
| `ANn`, `AN=n` | a press on the keyboard | changes the note to key n (bottom…top of the piano's table, 21–108 here), and its release too. "RELEASE EVENT NOT IN WORKSPACE" if the release isn't loaded (335BH) |
| `ATt`, `AT+t`, `AT-t` | any event but a filler | moves **that one event** (a press or a release, not the whole note) to t, or TIME ± t. It stays selected and TIME follows it. Within the workspace, else "WRONG SEGMENT". Among events at the same instant it goes in key order (3473H) |
| `DE` | any event but a filler | deletes that one event (3A0CH) |
| `DN` | a press | deletes the press and its release. "NOTE EXTENDS BEYOND WORKSPACE", "MISSING RELEASE EVENT" (3C20H) |
| `RESTORE` | an earlier `DE` or `DN` | puts the deleted event(s) back and selects them. One level of undo (3F14H) |
| `INk,v,d` (`IN=k,v,d`, or spaces instead of commas) | — | inserts note k at TIME, IHV v, lasting d, and selects it (4344H) |
| `IRk`, `IRk,d` | — | inserts a release of key k at TIME (+ d) (45F8H) |

Deleting or moving an event adds its gap to the one before. A gap over 255
ticks gets a `7F` filler, as the original encoder does.

### Markers: sostenuto and cue

These are the answers for docs/SE-FILE-FORMAT.md. Each is a 2-byte event,
inserted at TIME and then selected:

| Command | Writes | Meaning |
|---------|--------|---------|
| `SOSTENUTO ACTUATE` | **`80` Δ**: key 0, press form, no velocity (4A2FH) | sostenuto pedal down |
| `SOSTENUTO RELEASE` | **`00` Δ**: key 0, release form (4B50H) | sostenuto pedal up |
| `IC` (insert cue) | **`7E` Δ**: key 126, release form (490EH). TIME is first rounded down to a multiple of 16 ticks | a cue mark; the step display calls key 126 "CUE MARK" in either form |
| (automatic) | `7F` Δ | filler, when a gap would pass 255 |

**How the players use key 0.** The SE has no sostenuto actuator; the player
holds keys instead (623EH–6290H, 6215H). It keeps two bits per key: "down"
and "held by sostenuto".

- At `80`, every key that is down gets the held bit.
- A held key's release is put off.
- At `00`, the put-off releases happen.

This is the right sostenuto behaviour: only notes down at the moment of the
press are kept. The same code is in `play.exe` (1604H), `playmidi` (1804H)
and `midiin` (176FH).

**Keys outside the keyboard are ignored by the players.** Both the editor's
player and `play.exe` skip events below the bottom note or above the top
note (5C87H–5C99H; `play.exe` 1007H–1019H). So these never sound:

- cue marks (126) and fillers (127);
- Zenph's key-11 marks;
- the Bösendorfer bass keys 12–20 on this piano.

The editor has no command for key 11; it is not an editor marker.

### Time and pedals

| Command | Does to the data |
|---------|------------------|
| `IT` | inserts 16 ticks (1/50 s) of time at TIME. The gap before the first event at or after TIME grows by 16. Two loud-pedal samples and one soft-pedal sample (copies of the one at TIME) are inserted. LAST grows by 16. Repeat for more (1460H) |
| `SPLICEt`, `SPLICE=t` | cuts out TIME…t, rounded to a multiple of 16 ticks. Every key event in it is deleted, with the pedal samples, and the rest moves earlier. A note that starts before the cut and ends in it loses its release, or the other way round. The code does no repair (4D15H) |
| `DL`, `DS` | lists the loud or soft pedal from TIME: 20 lines of 10 samples (0746H, 08A9H) |
| `AL`, `AS` | goes through the loud or soft pedal from TIME one sample at a time. It prints `t: v` and waits: a number 0–255 replaces the sample, Enter keeps it, Esc stops (0A65H, 0D05H) |

### Heading

- `HEADING` prints the heading, or "HEADING DOES NOT EXIST".
- `HEADING=text` (or `HEADINGtext`) replaces it. `\` becomes a line break
  (CR LF), and `HEADING=` alone empties it.
- It works only in segment 0 ("SET HEADING IN SEGMENT ZERO"), because the
  heading is written when segment 0 leaves (1341H).

### Voicing: `LOUDER`, `SOFTER`, `LEVEL` write the EEPROM

These don't change the performance. They change the piano's per-key
**velocity trims** in the EEPROM, **at once and for good** (678BH–68E4H):

- Firmware entry FW#184 gives an EEPROM address. 0 means "VOICING DISABLED".
- FW#42 reads the byte there: the trims' on/off flag. If it is 0, the
  answer is again "VOICING DISABLED".
- Key i's trim is at that address + 1 + i, read with FW#42 and written
  with FW#43. This is exactly `lut.Table.TrimAt` (section 9: flag, then a
  signed trim per key), and I think FW#184 returns that header word.

| Command | Does |
|---------|------|
| `LOUDER` (and the **Up** arrow) | the selected press's key: trim + 1, up to +127 ("VOICING AT MAXIMUM"). Needs a selected press on the keyboard, else "EVENT NOT SELECTED", "ILLEGAL EVENT" |
| `SOFTER` (and the **Down** arrow) | trim − 1, down to −128 ("VOICING AT MINIMUM") |
| `LEVEL` | **sets every key's trim to 0**, with no question asked. On this piano that would erase G3's +7 |

"LOUDER" adding 1 agrees with `lut.TrimVelocity`: a positive trim is a
faster hammer. **So `edit` is a "W" program, not "M".** The app should
treat it like `setup`: confirm, back up before and after, or refuse those
three commands and the arrow keys.

### What this means for the format (and two corrections)

1. **Key 0 is the sostenuto pedal:** `80` Δ is down, `00` Δ is up, and
   neither has velocity bytes. **Key 126 (`7E` Δ) is a cue mark.** Key 127
   is a filler. Players play only keys from the table's bottom note to its
   top note.
2. **Velocity 0 is a silent press, not the hardest strike.** In `play.exe`
   (1042H–1047H) and the editor (5CC2H–5CC7H), velocity 0 goes to the
   silent-press path, the same one as 640 or more (FW#77, the silent
   level). It is skipped when "discard silent notes" is on. The editor
   shows IHV 0 as "(SILENT)", and `AI`/`IN` accept 0 on purpose.
   docs/SUITE-PROGRAMS.md ("Velocity 0") and docs/RECORDING-EDITOR.md say
   such a note plays as loud as the piano can. That came from FW#71, but
   FW#71 is never reached for velocity 0. So the recorder's velocity-0
   presses replay as silent key presses, and "fill in" turns them into
   sounding notes.
3. **Header byte 1 is read.** docs/SE-FILE-FORMAT.md says no code reads it.
   - The player looks it up in section 11's thresholds at bytes 58–65
     (40 11 10 07 05 02 01 00). The match gives a velocity factor from words
     74–89 (5E7EH–5EB6H; `play.exe` passes 2186H to it at 096CH, compared
     at 11FEH):

     | Byte 1 | Factor | Effect |
     |--------|--------|--------|
     | 40H or more | 240/256 | louder |
     | 02H–3FH (Zenph's 11H, the recorder's serial 08) | 256/256 | unchanged |
     | 01H | 288/256 | softer |
     | 00H | 480/256 | much softer |

   - It also picks a signed offset for the damper pedal from bytes 66–73
     (2E25H). All 0 in the prototype tables.
4. Section 11's words 2 and 4 are the pedal lead times in the editor's
   player. The current doc says they are unused.
5. The editor always writes scrambled files with a random seed, and drops
   the extension and the padding (above).

### How sure

**Traced and certain:**

- the command table and every handler's syntax and messages;
- the workspace sizes and the filling rule;
- when it reads, writes, commits and aborts;
- the random seed;
- the bytes for sostenuto (80/00) and cue (7E);
- the sostenuto holding logic, in the editor and in `play.exe`;
- velocity 0 taking the silent path, in both;
- that `S`, `G`, `P` and `I` drive the actuators and pedals;
- the step mode's fixed velocity;
- the meaning of UNIFORM, MUTE, LOUD, SOFT, RATE, PREFACE and ALERT;
- that LOUDER, SOFTER and LEVEL write EEPROM bytes through FW#43.

**Inferred:**

- That FW#184 returns the trim area (`TrimAt`, 067EH on this piano). The
  code reads a flag there and writes per-key bytes after it, but FW#184
  itself is not in the firmware notes.
- That key codes 05H and 06H are F11 and F12.
- The tick rate of the play clock. RATE is certainly a percentage, but I
  didn't check FW#32's argument against the 800 Hz clock.
- What happens when stepping reaches a key outside the keyboard, such as a
  key-11 mark: the step code calls FW#70 and FW#74 on it, and FW#70 is
  fatal outside the keyboard, **so `S`/`G` probably stop the editor
  there**. `P` skips such keys.
- The mnemonics: G, L, I, W and MASK are my guesses at Stahnke's names.
  The behaviour is not.
- What `SPLICE` does to notes cut in half: I read the byte moves, not a
  case on the piano.

### Command reference for the app

Type a command and press Enter. Times are in hundredths of a second
(`T1500` is 15 s) unless MASK is off. **Put no spaces between a command and
its value, and none at the end.** The cursor (TIME) only moves forward
through the piece, about 28 s at a time. `END` saves over the piece; `QUIT`
throws the edit away.

| Command | Type | What it does |
|---------|------|--------------|
| Status | `D` | Segment, time, workspace start and end, pedals, settings |
| Where am I | `T` | Prints the cursor time |
| Go to | `T1500`, `T=1500`, `T+50`, `T-50`, `T0` | Moves the cursor in the loaded part (`T0` = its start) |
| Back / forward | `B`, `BB`… / `F`, `FF`… | 1 s back or forward per letter |
| Next part | `FILL` | Keeps the last 4 s, loads the next part of the piece |
| Wind to | `W=6000` | Moves forward to a time, loading as needed; no way back |
| Play | `P` (F1) | Plays from 4 s before the cursor. Space pause/resume, Enter stop here, Esc stop |
| Play into cursor | `I` | Same, but pauses at the cursor |
| Step notes | `S` | Shows and strikes one event at a time. Space next, Enter select it, Esc stop |
| Step chords | `G` or `L` | The same, a chord at a time |
| Find soft notes | `SEARCH` | Next silent or very soft note (IHV 0 or over 400); then as `S` |
| Loudness | `AI=120` | Selected note's SE velocity: 0 silent, 4–1023, bigger is softer |
| Change key | `AN=62` | Selected note becomes key 62 (MIDI number) |
| Move event | `AT=1520`, `AT+5`, `AT-5` (F3/F4 = ∓1) | Moves the selected press or release |
| Delete | `DE` / `DN` | Delete the selected event / the whole selected note |
| Undo delete | `RESTORE` | Puts the last deleted event or note back |
| Insert note | `IN=60,80,50` | Key 60, velocity 80, 0.5 s long, at the cursor |
| Insert release | `IR=60` or `IR=60,20` | Releases key 60 at the cursor (+0.2 s) |
| Sostenuto | `SOSTENUTO ACTUATE` / `SOSTENUTO RELEASE` | Puts the sostenuto pedal down / up at the cursor |
| Cue mark | `IC` | Puts a cue mark at the cursor |
| Insert time | `IT` | Adds 0.02 s of silence at the cursor (repeat for more) |
| Cut time | `SPLICE=1800` | Removes everything from the cursor to 18.00 s |
| Show pedals | `DL` / `DS` | Damper / soft pedal values from the cursor |
| Edit pedals | `AL` / `AS` | Each sample in turn: type 0–255, Enter keep, Esc stop (F2 = 255) |
| Heading | `HEADING` / `HEADING=Title\Performer` | Show / set the text shown at start (first part only; `\` = new line) |
| Damper on/off | `LOUD` | Whether playing uses the damper pedal (on at start) |
| Soft pedal on/off | `SOFT` | Whether playing uses the soft pedal (**off** at start) |
| Same loudness | `U` | Play every note at one loudness (on/off) |
| A bit softer | `M` | Play everything slightly softer (on/off) |
| Speed | `R` / `R=80` | Tempo in percent, 40–120 |
| Lead-in | `PREFACE` / `PREFACE=200` | How long before the cursor `P` and `I` start (0–16 s) |
| Soft-note click | `ALERT` / `ALERT=400` / `ALERT=0` | While playing, very soft or silent notes also tick the top key; 0 = off |
| Note names | `K` | Sharps or flats |
| Time units | `MASK` | 1/100 s (on) or 1/800 s ticks (off) |
| Save and finish | `END` | Writes the whole piece, replacing the original |
| Discard and finish | `QUIT` | Throws all changes away |
| ⚠ Voicing | `LOUDER`, `SOFTER` (Up/Down arrows), `LEVEL` | **Changes the piano's stored per-key trims (EEPROM)**, not the piece. `LEVEL` resets all trims to 0 |

**Keys the console should offer:** Enter, Esc, Space and Backspace. F1–F12
are optional shortcuts. Leave Up and Down out, or treat them as settings
changes. Esc on a half-typed line cancels it.
