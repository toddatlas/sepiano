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
const MAX_VEL = 1023;

function median(values) {
  if (!values.length) return 0;
  const s = [...values].sort((a, b) => a - b);
  return s.length % 2 ? s[s.length >> 1] : (s[s.length / 2 - 1] + s[s.length / 2]) / 2;
}

function clampVel(v) { return Math.max(4, Math.min(MAX_VEL, Math.round(v))); }

function pedalNames(doc) { return [['damper', doc.dp], ['soft', doc.sp]]; }

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
// position they had just before.
export function insertTime(doc, at, len) {
  at = Math.max(0, Math.round(at));
  len = Math.round(len);
  if (len <= 0) return;
  for (const n of doc.notes) {
    if (n.on >= at) { n.on += len; n.off += len; } else if (n.off > at) n.off += len;
  }
  for (const [name, p] of pedalNames(doc)) {
    const i = Math.floor(at / p);
    const arr = held(doc[name], i);
    const v = i > 0 ? arr[i - 1] : (arr[0] || 0);
    doc[name] = arr.slice(0, i).concat(new Array(Math.round(len / p)).fill(v), arr.slice(i));
  }
  doc.end += len;
}

// deleteTime removes ticks a to b: the notes that start in them go, later
// ones move back, and a note held into them is let go at a.
export function deleteTime(doc, a, b) {
  a = Math.max(0, Math.round(a));
  b = Math.round(b);
  const len = b - a;
  if (len <= 0) return;
  doc.notes = doc.notes.filter((n) => !(n.on >= a && n.on < b));
  for (const n of doc.notes) {
    if (n.on >= b) { n.on -= len; n.off -= len; } else if (n.off > b) n.off -= len;
    else if (n.off > a) n.off = Math.max(n.on + MIN_LEN, a);
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
  for (const n of doc.notes) {
    n.on = Math.round(map(n.on));
    n.off = Math.max(n.on + MIN_LEN, Math.round(map(n.off)));
  }
  const end = Math.round(map(doc.end));
  for (const [name, p] of pedalNames(doc)) {
    const arr = doc[name];
    const out = new Array(Math.floor(end / p) + 1);
    for (let i = 0; i < out.length; i++) {
      out[i] = arr.length ? arr[Math.min(arr.length - 1, Math.floor(unmap(i * p) / p))] : 0;
    }
    doc[name] = out;
  }
  doc.end = end;
}

// copyRange copies ticks a to b: the notes that start in them, and the
// pedals.
export function copyRange(doc, a, b) {
  return {
    notes: doc.notes.filter((n) => n.on >= a && n.on < b).map((n) => ({ key: n.key, on: n.on - a, off: n.off - a, vel: n.vel })),
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
  at = Math.max(0, Math.round(at));
  if (insert) insertTime(doc, at, clip.len);
  const added = clip.notes.map((n) => ({ key: n.key, on: n.on + at, off: n.off + at, vel: n.vel }));
  doc.notes.push(...added);
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
  const live = notes.filter((n) => n.vel > 0);
  if (!live.length) return 0;
  let t0 = Infinity;
  let t1 = -Infinity;
  for (const n of live) { t0 = Math.min(t0, n.on); t1 = Math.max(t1, n.on); }
  for (const n of live) {
    const f = t1 > t0 ? (n.on - t0) / (t1 - t0) : 0;
    const pct = fromPct + (toPct - fromPct) * f;
    n.vel = clampVel(n.vel / (1 + pct / 100));
  }
  return live.length;
}

// evenOut brings the notes' loudness strength% of the way to their
// median (in hammer speed): 100 makes them all alike.
export function evenOut(notes, strength) {
  const live = notes.filter((n) => n.vel > 0);
  if (!live.length) return 0;
  const s = Math.max(0, Math.min(100, strength)) / 100;
  const logs = live.map((n) => Math.log(128000 / n.vel));
  const mid = median(logs);
  live.forEach((n, i) => { n.vel = clampVel(128000 / Math.exp(logs[i] + (mid - logs[i]) * s)); });
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
