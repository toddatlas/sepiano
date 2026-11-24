// Editing operations for the piano-roll editor (editor.js,
// docs/RECORDING-EDITOR.md): time ranges, copy and paste, chords, loudness
// and pedals.
//
// Each works on the editor's document in place:
//   doc.notes  [{key, on, off, vel}] (ticks, 1/800 s; SE velocity)
//   doc.damper, doc.soft  pedal samples, one per doc.dp / doc.sp ticks
//   doc.end    the end of the performance
// A pedal curve that changes gets a new array, so the editor's undo keeps
// the old one. Velocity 0 (no hammer reading) is left alone by the
// loudness tools.

export const CHORD_WINDOW = 56; // ticks (70 ms): presses this close make a chord, as piano.ChordWindow
const MIN_LEN = 4; // ticks: the shortest note
export const SILENT = 640; // this velocity or more is a silent press (play.exe 1044H)

function median(values) {
  if (!values.length) return 0;
  const s = [...values].sort((a, b) => a - b);
  return s.length % 2 ? s[s.length >> 1] : (s[s.length / 2 - 1] + s[s.length / 2]) / 2;
}

// audible keeps a changed loudness audible: below SILENT, so a softer
// shape never turns a note into a silent press.
function audible(v) { return Math.max(4, Math.min(SILENT - 1, Math.round(v))); }

function pedalNames(doc) { return [['damper', doc.dp], ['soft', doc.sp]]; }

// grid is the step both pedals are sampled on (16 ticks, 20 ms: the soft
// pedal's period, two of the damper's). Time edits snap to it, so the
// pedals move by exactly as much as the notes and never drift from them.
function grid(doc) {
  const gcd = (x, y) => (y ? gcd(y, x % y) : x);
  const dp = doc.dp || 8;
  const sp = doc.sp || 16;
  return (dp * sp) / gcd(dp, sp);
}

// snap rounds a time to the pedals' grid.
export function snap(doc, t) {
  const g = grid(doc);
  return Math.round(t / g) * g;
}

// held returns a copy of arr at least n long, holding its last value.
function held(arr, n) {
  const out = arr.slice();
  const last = out.length ? out[out.length - 1] : 0;
  while (out.length < n) out.push(last);
  return out;
}

function lastOff(doc) {
  let t = 0;
  for (const n of doc.notes) t = Math.max(t, n.off);
  return t;
}

// insertTime opens a gap of len ticks at tick at: what comes later moves
// on, a note held across it is held through it, and the pedals keep the
// position they had just before. Both snap to the pedals' grid (20 ms).
export function insertTime(doc, at, len) {
  at = Math.max(0, snap(doc, at));
  if (!(len > 0)) return;
  len = Math.max(grid(doc), snap(doc, len));
  for (const n of doc.notes) {
    if (n.on >= at) { n.on += len; n.off += len; } else if (n.off > at) n.off += len;
  }
  for (const o of doc.others || []) if (o.tick >= at) o.tick += len;
  for (const [name, p] of pedalNames(doc)) {
    const i = Math.floor(at / p);
    const arr = held(doc[name], i);
    const v = i > 0 ? arr[i - 1] : (arr[0] || 0);
    doc[name] = arr.slice(0, i).concat(new Array(Math.round(len / p)).fill(v), arr.slice(i));
  }
  doc.end += len;
}

// deleteTime removes ticks a to b: the notes that start in them go, later
// ones move back, and a note held into them is let go at a. Both ends snap
// to the pedals' grid (20 ms).
export function deleteTime(doc, a, b) {
  a = Math.max(0, snap(doc, a));
  b = snap(doc, b);
  const len = b - a;
  if (len <= 0) return;
  doc.notes = doc.notes.filter((n) => !(n.on >= a && n.on < b));
  for (const n of doc.notes) {
    if (n.on >= b) { n.on -= len; n.off -= len; } else if (n.off > b) n.off -= len;
    else if (n.off > a) n.off = Math.max(n.on + MIN_LEN, a);
  }
  if (doc.others) {
    // Marks in the cut go. A sostenuto release in it moves to a, so a
    // sostenuto put down before the cut still comes up.
    doc.others = doc.others.filter((o) => !(o.tick >= a && o.tick < b) || (o.key === 0 && !o.on));
    for (const o of doc.others) {
      if (o.tick >= b) o.tick -= len; else if (o.tick >= a) o.tick = a;
    }
  }
  for (const [name, p] of pedalNames(doc)) {
    const arr = doc[name];
    doc[name] = arr.slice(0, Math.floor(a / p)).concat(arr.slice(Math.floor(b / p)));
  }
  doc.end = Math.max(doc.end - len, lastOff(doc), MIN_LEN);
}

// cropTo keeps ticks a to b only, after lead ticks of silence.
export function cropTo(doc, a, b, lead) {
  deleteTime(doc, b, Math.max(b + 1, doc.end + 1));
  deleteTime(doc, 0, a);
  insertTime(doc, 0, lead);
  doc.end = Math.max(lastOff(doc), Math.round(b - a + lead));
}

// stretch makes ticks a to b last f times as long (1.1: 10% slower), and
// moves what comes after; notes and pedals alike.
export function stretch(doc, a, b, f) {
  if (!(b > a) || !(f > 0) || f === 1) return;
  const grow = (b - a) * (f - 1);
  const map = (t) => (t <= a ? t : t <= b ? a + (t - a) * f : t + grow);
  const unmap = (t) => (t <= a ? t : t <= a + (b - a) * f ? a + (t - a) / f : t - grow);
  retime(doc, map, unmap);
}

// tempoRamp changes the tempo gradually across ticks a to b: time there
// passes f0 times as long at a, easing to f1 times at b (1 to 1.3 is a
// ritardando that ends 30% slower; 1 to 0.8 an accelerando). What comes
// after moves with it. It returns where b ends up.
export function tempoRamp(doc, a, b, f0, f1) {
  if (!(b > a) || !(f0 > 0) || !(f1 > 0) || (f0 === 1 && f1 === 1)) return b;
  const len = b - a;
  const k = (f1 - f0) / (2 * len); // map(t) = a + f0·s + k·s², s = t − a
  const newLen = (len * (f0 + f1)) / 2;
  const grow = newLen - len;
  const map = (t) => {
    if (t <= a) return t;
    if (t >= b) return t + grow;
    const s = t - a;
    return a + f0 * s + k * s * s;
  };
  const unmap = (u) => {
    if (u <= a) return u;
    if (u >= a + newLen) return u - grow;
    const v = u - a;
    const s = Math.abs(k) < 1e-12 ? v / f0 : (-f0 + Math.sqrt(Math.max(0, f0 * f0 + 4 * k * v))) / (2 * k);
    return a + s;
  };
  retime(doc, map, unmap);
  return Math.round(a + newLen);
}

// retime moves every note, mark and pedal through map, a time warp that
// only ever moves later times later; unmap is its inverse, for the pedals.
function retime(doc, map, unmap) {
  for (const n of doc.notes) {
    n.on = Math.round(map(n.on));
    n.off = Math.max(n.on + MIN_LEN, Math.round(map(n.off)));
  }
  for (const o of doc.others || []) o.tick = Math.round(map(o.tick));
  const end = Math.round(map(doc.end));
  for (const [name, p] of pedalNames(doc)) {
    const arr = doc[name];
    const out = new Array(Math.floor(end / p) + 1);
    for (let i = 0; i < out.length; i++) {
      out[i] = arr.length ? arr[Math.max(0, Math.min(arr.length - 1, Math.floor(unmap(i * p) / p)))] : 0;
    }
    doc[name] = out;
  }
  doc.end = end;
}

// copyRange copies ticks a to b: the notes that start in them, the pedals,
// and the sostenuto pedal's events and marks in them.
export function copyRange(doc, a, b) {
  a = Math.max(0, snap(doc, a)); // on the pedals' grid, as insertTime and deleteTime
  b = Math.max(a, snap(doc, b));
  return {
    notes: doc.notes.filter((n) => n.on >= a && n.on < b).map((n) => ({ key: n.key, on: n.on - a, off: n.off - a, vel: n.vel })),
    others: (doc.others || []).filter((o) => o.tick >= a && o.tick < b).map((o) => ({ ...o, tick: o.tick - a })),
    len: b - a,
    damper: doc.damper.slice(Math.floor(a / doc.dp), Math.floor(b / doc.dp)),
    soft: doc.soft.slice(Math.floor(a / doc.sp), Math.floor(b / doc.sp)),
    dp: doc.dp,
    sp: doc.sp,
  };
}

// copyNotes copies notes, from the first one's start; no pedals.
export function copyNotes(notes) {
  if (!notes.length) return null;
  let t0 = Infinity;
  let t1 = 0;
  for (const n of notes) { t0 = Math.min(t0, n.on); t1 = Math.max(t1, n.off); }
  return { notes: notes.map((n) => ({ key: n.key, on: n.on - t0, off: n.off - t0, vel: n.vel })), len: t1 - t0 };
}

// paste puts a copy at tick at, mixed in with what is there or, with
// insert, into a gap opened for it (its pedals too). It returns the new
// notes.
export function paste(doc, clip, at, insert) {
  // Into a gap, on the pedals' grid, so its pedals line up with its notes.
  at = Math.max(0, insert ? snap(doc, at) : Math.round(at));
  if (insert) insertTime(doc, at, clip.len);
  const added = clip.notes.map((n) => ({ key: n.key, on: n.on + at, off: n.off + at, vel: n.vel }));
  doc.notes.push(...added);
  if (doc.others && clip.others) doc.others.push(...clip.others.map((o) => ({ ...o, tick: o.tick + at })));
  if (insert) {
    for (const [name, p] of pedalNames(doc)) {
      const src = clip[name];
      if (!src || !src.length || clip[name === 'damper' ? 'dp' : 'sp'] !== p) continue;
      const i = Math.floor(at / p);
      const arr = held(doc[name], i + src.length);
      for (let k = 0; k < src.length; k++) arr[i + k] = src[k];
      doc[name] = arr;
    }
  }
  doc.end = Math.max(doc.end, lastOff(doc));
  return added;
}

// setPedal holds a pedal at value (0 up to 255 down) from tick a to b.
export function setPedal(doc, name, a, b, value) {
  const p = name === 'damper' ? doc.dp : doc.sp;
  const i = Math.max(0, Math.floor(a / p));
  const j = Math.ceil(b / p);
  const arr = held(doc[name], j);
  for (let k = i; k < j; k++) arr[k] = value;
  doc[name] = arr;
}

// chords groups notes into chords: presses within CHORD_WINDOW of the
// chord's first.
export function chords(notes) {
  const s = [...notes].sort((a, b) => a.on - b.on);
  const out = [];
  for (let i = 0; i < s.length;) {
    let j = i + 1;
    while (j < s.length && s[j].on - s[i].on <= CHORD_WINDOW) j++;
    out.push(s.slice(i, j));
    i = j;
  }
  return out;
}

// alignStarts makes each chord of notes start together, at the middle of
// its starts; it returns how many notes moved.
export function alignStarts(notes) {
  let moved = 0;
  for (const c of chords(notes)) {
    if (c.length < 2) continue;
    const t = Math.round(median(c.map((n) => n.on)));
    for (const n of c) {
      if (n.on !== t) moved++;
      n.on = t;
      n.off = Math.max(n.off, t + MIN_LEN);
    }
  }
  return moved;
}

// alignReleases makes each chord of notes let go together, at the middle
// of its releases.
export function alignReleases(notes) {
  let moved = 0;
  for (const c of chords(notes)) {
    if (c.length < 2) continue;
    const t = Math.round(median(c.map((n) => n.off)));
    for (const n of c) {
      const off = Math.max(n.on + MIN_LEN, t);
      if (n.off !== off) moved++;
      n.off = off;
    }
  }
  return moved;
}

// shapeLoudness changes loudness gradually across the notes, by fromPct at
// the first to toPct at the last (in hammer speed: +10 is 10% louder), for
// a crescendo or diminuendo.
export function shapeLoudness(notes, fromPct, toPct) {
  const live = notes.filter((n) => n.vel > 0 && n.vel < SILENT); // silent presses stay silent
  if (!live.length) return 0;
  let t0 = Infinity;
  let t1 = -Infinity;
  for (const n of live) { t0 = Math.min(t0, n.on); t1 = Math.max(t1, n.on); }
  for (const n of live) {
    const f = t1 > t0 ? (n.on - t0) / (t1 - t0) : 0;
    const pct = fromPct + (toPct - fromPct) * f;
    n.vel = audible(n.vel / (1 + pct / 100));
  }
  return live.length;
}

// evenOut brings the notes' loudness strength% of the way to their
// median (in hammer speed): 100 makes them all alike.
export function evenOut(notes, strength) {
  const live = notes.filter((n) => n.vel > 0 && n.vel < SILENT); // silent presses stay silent
  if (!live.length) return 0;
  const s = Math.max(0, Math.min(100, strength)) / 100;
  const logs = live.map((n) => Math.log(128000 / n.vel));
  const mid = median(logs);
  live.forEach((n, i) => { n.vel = audible(128000 / Math.exp(logs[i] + (mid - logs[i]) * s)); });
  return live.length;
}

// setLength gives every note the same length, in ticks.
export function setLengths(notes, len) {
  for (const n of notes) n.off = n.on + Math.max(MIN_LEN, Math.round(len));
}

// legato holds each note until the next note on any key starts (keeping
// at least its own length): a smoother line from detached notes.
export function legato(notes) {
  const s = [...notes].sort((a, b) => a.on - b.on);
  let changed = 0;
  for (let i = 0; i < s.length; i++) {
    let j = i + 1;
    while (j < s.length && s[j].on === s[i].on) j++;
    if (j < s.length && s[j].on > s[i].off) { s[i].off = s[j].on; changed++; }
  }
  return changed;
}

// The sostenuto pedal: key-0 events in doc.others ({tick, key, on, vel}),
// on: true puts it down, false lets it up.

// sostenutoDownAt reports whether the sostenuto pedal is down at tick t.
export function sostenutoDownAt(doc, t) {
  let down = false;
  for (const o of [...(doc.others || [])].sort((x, y) => x.tick - y.tick)) {
    if (o.tick > t) break;
    if (o.key === 0) down = o.on;
  }
  return down;
}

// holdSostenuto puts the sostenuto pedal down at a and lets it up at b,
// replacing its events between them.
export function holdSostenuto(doc, a, b) {
  doc.others = (doc.others || []).filter((o) => !(o.key === 0 && o.tick >= a && o.tick <= b));
  doc.others.push({ tick: Math.round(a), key: 0, on: true, vel: 0 }, { tick: Math.round(b), key: 0, on: false, vel: 0 });
}

// clearSostenuto removes the sostenuto pedal's events from a to b; if it
// was down at a, it comes up there.
export function clearSostenuto(doc, a, b) {
  const wasDown = sostenutoDownAt(doc, a - 1);
  doc.others = (doc.others || []).filter((o) => !(o.key === 0 && o.tick >= a && o.tick <= b));
  if (wasDown) doc.others.push({ tick: Math.round(a), key: 0, on: false, vel: 0 });
}

// CUE_KEY is the key a cue mark is written on (edit.exe's IC).
export const CUE_KEY = 126;

// addCue puts a cue mark at t, rounded down to 16 ticks as edit.exe does.
export function addCue(doc, t) {
  doc.others = doc.others || [];
  doc.others.push({ tick: Math.floor(Math.max(0, t) / 16) * 16, key: CUE_KEY, on: false, vel: 0 });
}

// clearCues removes the cue marks from a to b.
export function clearCues(doc, a, b) {
  doc.others = (doc.others || []).filter((o) => !(o.key === CUE_KEY && o.tick >= a && o.tick <= b));
}

// sostenutoSpans returns the stretches the sostenuto pedal is down:
// [[from, to], …], to the end if it never comes up.
export function sostenutoSpans(doc) {
  const out = [];
  let from = null;
  for (const o of [...(doc.others || [])].filter((x) => x.key === 0).sort((x, y) => x.tick - y.tick)) {
    if (o.on && from === null) from = o.tick;
    else if (!o.on && from !== null) { out.push([from, o.tick]); from = null; }
  }
  if (from !== null) out.push([from, doc.end]);
  return out;
}
