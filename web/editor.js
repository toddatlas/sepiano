// The piano-roll editor (docs/RECORDING-EDITOR.md).
//
// A piece is drawn as notes on a roll, keys up the side and time across,
// with lanes below for loudness, the damper pedal and the soft pedal.
// Changes stay in the browser until saved: an original is saved as a new
// edited copy (the original never changes), and an edited copy is saved
// over. "Play from cursor" plays the edited version on the piano, from the
// cursor, without saving it.
//
// Everything is drawn on one canvas. Times are ticks (1/800 s), as in the
// .kb file; velocity is the SE scale (bigger is softer). The helpers come
// from app.js (`ui`), so nothing here builds HTML from strings.

const TICK_HZ = 800;
const LOW = 21;
const HIGH = 108;
const KEYS = HIGH - LOW + 1;
const SILENT = 640; // this velocity or more is a silent press
const MAX_VEL = 1023;
const MIN_LEN = 4; // ticks: the shortest note
const NUDGE = 8; // ticks: 10 ms
const UNDO_MAX = 100;
const PREVIEW_LEAD = TICK_HZ; // api.PreviewLead
const PREVIEW_PERFORMER = 'Editor preview'; // api.PreviewPerformer
const LOUDER = 1 / 1.05; // one step: 5% more hammer speed
const SOFTER = 1.05;

// Canvas layout, in CSS pixels.
const GUTTER = 64;
const RULER = 24;
const VEL_H = 76;
const PED_H = 46;
const GAP = 8;
const OVERVIEW = 30;
const FIXED_H = RULER + GAP + VEL_H + GAP + PED_H + GAP + PED_H + GAP + OVERVIEW;

// Loudness for drawing: log hammer speed (128000 / velocity), 0 to 1.
const L_MIN = Math.log(128000 / MAX_VEL);
const L_MAX = Math.log(128000 / 24);

const STEPS = [0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1, 2, 5, 10, 15, 30, 60, 120, 300, 600];

import * as T from './edittools.js';

const drafts = new Map(); // piece id -> unsaved work, kept while the app is open
const previews = new Map(); // piece id -> the tick its last preview started from
let handoff = null; // the view to open a freshly saved copy at
let clipboard = null; // copied notes (and pedals): pasted into this piece or another

function loud(vel) {
  if (vel <= 0) return 1;
  return Math.min(1, Math.max(0, (Math.log(128000 / vel) - L_MIN) / (L_MAX - L_MIN)));
}

function velOfLoud(f) {
  const l = L_MIN + Math.min(1, Math.max(0, f)) * (L_MAX - L_MIN);
  return clampVel(Math.round(128000 / Math.exp(l)));
}

function clampVel(v) { return Math.max(4, Math.min(MAX_VEL, Math.round(v))); }

// fmtClock(ticks, decimals): m:ss.ddd
function fmtClock(t, dec = 3) {
  const p = 10 ** dec;
  const total = Math.round((Math.max(0, t) / TICK_HZ) * p) / p;
  const m = Math.floor(total / 60);
  const s = total - m * 60;
  return `${m}:${s.toFixed(dec).padStart(dec ? dec + 3 : 2, '0')}`;
}

function msText(ticks) {
  const ms = Math.round((ticks * 1000) / TICK_HZ);
  return `${ms > 0 ? '+' : ms < 0 ? '−' : ''}${Math.abs(ms)} ms`;
}

function median(values) {
  if (!values.length) return 0;
  const s = [...values].sort((a, b) => a - b);
  return s[s.length >> 1];
}

export function editHref(id) { return '#/piece/' + encodeURIComponent(id) + '/edit'; }

export function editorScreen(root, [id], ui) {
  const { h, fill, setText, icon, api, toast, noteName, isBlackKey, app, isStatus, applyReplyStatus } = ui;
  const path = '/pieces/' + encodeURIComponent(id);
  let alive = true;
  let piece = null;
  let doc = null; // {notes: [{key, on, off, vel}], damper, soft, end, dp, sp, kept}
  let orig = null; // the pedal arrays as loaded: unchanged ones are not sent back
  let sel = new Set();
  let undo = [];
  let redo = [];
  let ver = 0;
  let savedVer = 0;
  let nextVer = 1;
  let cursor = 0;
  let playhead = null;
  let medianVel = 200;
  let saving = false;
  let corr = null; // the recording corrections (GET /corrections)
  let range = null; // a time range {a, b} in ticks: a drag along the ruler
  const view = { t0: 0, ppt: 0.05, rowH: 10, y: 0 };
  let cssW = 0;
  let cssH = 0;
  let dpr = 1;
  let rollH = 200;
  let colors = {};
  let drag = null;
  let hoverKey = null;
  let raf = 0;
  const touches = new Map();

  const canvas = h('canvas', { class: 'ed-canvas', tabindex: '0', 'aria-label': 'Piano roll: notes, loudness and pedals. The keyboard shortcuts are listed in the selection panel.' });
  const ctx = canvas.getContext('2d');
  const wrap = h('div', { class: 'ed-wrap' }, canvas);
  const head = h('div', { class: 'ed-head' });
  const bar = h('div', { class: 'ed-bar', role: 'toolbar', 'aria-label': 'Editor' });
  const insp = h('aside', { class: 'ed-insp', 'aria-label': 'Selection' });
  const statusEl = h('p', { class: 'ed-status', 'aria-live': 'polite' });
  const HELP = [
    ['Select', 'click a note; drag a box; Shift adds. Click a key at the left for all its notes.'],
    ['Change', 'drag a note to move it, its right end to move the release; drag a stalk in Loudness; draw in Damper or Soft.'],
    ['Add', 'double-click on the roll.'],
    ['Range', 'Drag along the time ruler to mark a stretch of time (a click there moves the cursor): play it, copy it, delete it, keep only it, stretch its tempo, or set the pedals over it.'],
    ['Copy', 'Ctrl+C copies the selected notes (or the range, with its pedals); Ctrl+V mixes them in at the cursor, Ctrl+Shift+V opens a gap for them; Ctrl+D duplicates. The copy stays while the app is open, so it can go into another piece.'],
    ['Keys', 'Space plays from the cursor or stops · Delete removes · ←/→ move 10 ms (Shift: 100 ms) · Alt+←/→ move releases · ↑/↓ change key · Ctrl+Z, Ctrl+Y undo and redo · Ctrl+S saves · Esc clears the selection and the range.'],
    ['View', 'wheel scrolls, Shift+wheel goes along; Ctrl+wheel (or pinch) zooms; Alt+wheel makes rows taller; right-drag pans; click the strip at the bottom to jump.'],
  ];

  document.body.classList.add('wide-screen');
  root.append(h('p', { class: 'muted center' }, 'Loading the piece…'));

  api('GET', path + '/performance').then((r) => {
    if (!alive) return;
    try {
      start(r);
    } catch (err) {
      console.error(err);
      fill(root, h('div', { class: 'message message-error' }, h('p', null, 'The editor could not open this piece: ' + err.message)));
    }
  }, (err) => {
    if (alive) fill(root, h('div', { class: 'message message-error' }, h('p', null, err.message)));
  });

  // ------------------------------------------------------------------ load

  function start(r) {
    piece = r.piece;
    const draft = drafts.get(id);
    if (draft && draft.sha === piece.kb_sha256) {
      ({ doc, orig, undo, redo, ver, savedVer, nextVer, cursor } = draft);
      Object.assign(view, draft.view);
      sel = new Set(draft.sel.map((i) => doc.notes[i]).filter(Boolean));
      toast('Your unsaved changes are back.');
    } else {
      drafts.delete(id);
      doc = {
        notes: r.notes.map(([key, on, off, vel]) => ({ key, on, off, vel })),
        damper: r.damper, soft: r.soft, dp: r.damper_period, sp: r.soft_period, end: r.end, kept: r.kept,
        corrected: !!piece.corrected, // the recording corrections are in these notes
      };
      orig = { damper: doc.damper, soft: doc.soft };
    }
    medianVel = median(doc.notes.filter((n) => n.vel > 0 && n.vel < SILENT).map((n) => n.vel)) || 200;
    document.title = `Edit · ${piece.title} · Piano`;
    readColors();
    const narrow = window.matchMedia('(max-width: 700px)').matches;
    fill(root,
      head,
      narrow && h('div', { class: 'message message-warn' }, h('p', null, 'The editor is easier on a computer. Here you can look, select, make small changes and play from a point.')),
      bar,
      h('div', { class: 'ed-main' }, h('div', { class: 'ed-left' }, wrap, statusEl), insp));
    buildBar();
    renderBar();
    renderInspector();
    sizeCanvas();
    if (recordedHere()) {
      api('GET', '/corrections').then((c) => { if (alive) { corr = c; renderBar(); } }, () => {});
    }
    if (!draft || draft.sha !== piece.kb_sha256) initialView();
    if (handoff && handoff.id === id) {
      Object.assign(view, handoff.view);
      cursor = handoff.cursor;
      handoff = null;
    }
    clampView();
    attach();
    renderStatus();
    schedule();
    canvas.focus({ preventScroll: true });
  }

  function initialView() {
    const w = cssW - GUTTER;
    const first = doc.notes.length ? doc.notes[0].on : 0;
    view.t0 = Math.max(0, first - TICK_HZ / 2);
    view.ppt = w / Math.max(TICK_HZ * 2, Math.min(doc.end - view.t0 + TICK_HZ / 2, 20 * TICK_HZ));
    fitRows();
  }

  // Rows fit the keys the piece uses, within limits; the rest scrolls.
  function fitRows() {
    let lo = HIGH;
    let hi = LOW;
    for (const n of doc.notes) { lo = Math.min(lo, n.key); hi = Math.max(hi, n.key); }
    if (lo > hi) { lo = 48; hi = 84; }
    lo = Math.max(LOW, lo - 3);
    hi = Math.min(HIGH, hi + 3);
    view.rowH = Math.max(5, Math.min(16, Math.floor(rollH / (hi - lo + 1))));
    view.y = (HIGH - hi) * view.rowH;
  }

  function readColors() {
    const cs = getComputedStyle(document.documentElement);
    const g = (n) => cs.getPropertyValue(n).trim();
    colors = {
      bg: g('--bg'), elev: g('--bg-elev'), sunk: g('--bg-sunk'), ink: g('--ink'), ink2: g('--ink-2'),
      muted: g('--muted'), line: g('--line'), strong: g('--line-strong'), accent: g('--accent'),
      danger: g('--danger'), rec: g('--rec'), onInk: g('--on-ink'),
    };
  }

  // ------------------------------------------------------------- chrome

  function dirty() { return ver !== savedVer; }
  function isCopy() { return piece.origin === 'edited'; }
  // A recording made on this piano, or a copy of one: the corrections apply.
  function recordedHere() { return piece.origin === 'recorded' || !!piece.from_recording; }
  function corrEmpty() { return !corr || (!corr.fill_zero && !corr.keys.some((k) => k.speed || k.release_ms)); }

  function renderHead() {
    fill(head,
      h('h1', { class: 'ed-title' }, h('a', { href: ui.pieceHref(id) }, piece.title || 'Untitled')),
      h('p', { class: 'ed-sub' },
        isCopy() ? 'Edited copy: Save writes over this copy.' : 'Original: saving makes an edited copy; this piece stays as it is.',
        ` ${doc.notes.length.toLocaleString()} notes · ${fmtClock(doc.end, 0)}`,
        doc.kept ? ` · ${doc.kept} marks outside the keyboard kept as they are` : '',
        dirty() && h('span', { class: 'ed-dirty' }, ' · Unsaved changes')),
      recordedHere() && h('p', { class: 'ed-sub ed-rec' }, 'Recorded on this piano. ',
        doc.corrected ? 'The recording corrections are in these notes.'
          : corr && corr.active ? h('span', null, 'It plays with the ', h('a', { href: '#/record/corrections' }, 'recording corrections'), ` (${corr.summary}); “Apply corrections” puts them into the notes.`)
            : h('span', null, 'No ', h('a', { href: '#/record/corrections' }, 'recording corrections'), ' apply now.')));
  }

  function button(label, onclick, opts = {}) {
    return h('button', { class: `btn btn-small ${opts.cls || ''}`, type: 'button', onclick, disabled: !!opts.disabled, title: opts.title || null, 'aria-label': opts.aria || null },
      opts.icon && icon(opts.icon), label);
  }

  // The toolbar is built once; renderBar only updates it, so a button is
  // never replaced under a click while status events arrive.
  const tb = {};
  function buildBar() {
    tb.undo = button('Undo', doUndo, { icon: 'undo', title: 'Undo (Ctrl+Z)' });
    tb.redo = button('Redo', doRedo, { icon: 'redo', title: 'Redo (Ctrl+Y)' });
    tb.play = button('Play from cursor', playFrom, { icon: 'play', cls: 'btn-primary', title: 'Play the edited version on the piano from the cursor (Space)' });
    tb.stop = button('Stop', stopPlay, { icon: 'stop' });
    tb.save = button('Save', () => save(false), { title: 'Ctrl+S' });
    tb.copy = button('Save as new copy', () => save(true));
    tb.apply = button('Apply corrections', applyCorrections, { title: 'Put the recording corrections into the notes (undoable)' });
    fill(bar,
      h('div', { class: 'ed-group' }, tb.undo, tb.redo),
      h('div', { class: 'ed-group' },
        button('−', () => zoomAt(1 / 1.6, GUTTER + (cssW - GUTTER) / 2), { title: 'Zoom out (−)', aria: 'Zoom out' }),
        button('+', () => zoomAt(1.6, GUTTER + (cssW - GUTTER) / 2), { title: 'Zoom in (+)', aria: 'Zoom in' }),
        button('Whole piece', fitAll, { title: 'Show the whole piece (0)' })),
      h('div', { class: 'ed-group' }, tb.play, tb.stop),
      h('div', { class: 'ed-group' }, tb.apply),
      h('div', { class: 'ed-group ed-save' }, tb.save, tb.copy));
  }

  let headKey = '';
  function renderBar() {
    const s = app.status;
    tb.undo.disabled = !undo.length;
    tb.redo.disabled = !redo.length;
    tb.stop.disabled = !(s && ['playing', 'paused', 'loading', 'stopping'].includes(s.state));
    const label = saving ? 'Saving…' : isCopy() ? 'Save' : 'Save a copy';
    if (tb.save.lastChild.textContent !== label) tb.save.lastChild.textContent = label;
    tb.save.disabled = saving || (isCopy() && !dirty());
    tb.save.classList.toggle('btn-primary', dirty() || !isCopy());
    tb.copy.hidden = !isCopy();
    tb.copy.disabled = saving;
    tb.apply.hidden = !recordedHere() || doc.corrected || corrEmpty();
    const key = `${piece.title}|${piece.origin}|${dirty()}|${doc.notes.length}|${doc.end}|${doc.corrected}|${corr ? corr.summary + corr.on : ''}`;
    if (key !== headKey) { headKey = key; renderHead(); }
  }

  // The selection: what it is, and what can be done to it.
  // A number field with its own Set button.
  function num(label, value, step, apply, unit, set = 'Set') {
    const input = h('input', { class: 'input ed-num-input', type: 'number', step: String(step), value: String(value), 'aria-label': label });
    return h('form', { class: 'ed-num', onsubmit: (e) => { e.preventDefault(); const x = parseFloat(input.value); if (Number.isFinite(x)) apply(x); } },
      h('label', null, label, ' ', input, unit ? ` ${unit}` : ''), h('button', { class: 'btn btn-small', type: 'submit' }, set));
  }

  function group(label, ...items) {
    return h('div', { class: 'ed-group' }, label && h('span', { class: 'ed-label' }, label), items);
  }

  // The panel: what is selected and what can be done to it, then the time
  // range, then the cursor; with nothing chosen, how to use the editor.
  function renderInspector() {
    const parts = [];
    if (sel.size) parts.push(selectionTools());
    if (range) parts.push(rangeTools());
    parts.push(cursorTools());
    if (!sel.size && !range) {
      parts.push(h('dl', { class: 'ed-help' }, HELP.map(([k, v]) => [h('dt', null, k), h('dd', null, v)])));
    }
    fill(insp, parts);
  }

  function selectionTools() {
    const ns = [...sel];
    const single = ns.length === 1 ? ns[0] : null;
    const vels = ns.map((n) => n.vel);
    return h('div', { class: 'ed-section' },
      h('p', { class: 'ed-sel' }, selectionText(ns)),
      h('div', { class: 'ed-tools' },
        group('Move',
          button('−10 ms', () => moveBy(-NUDGE)), button('+10 ms', () => moveBy(NUDGE)),
          num('by', 0, 1, (ms) => moveBy(Math.round((ms * TICK_HZ) / 1000)), 'ms')),
        group('Release',
          button('−10 ms', () => releaseBy(-NUDGE)), button('+10 ms', () => releaseBy(NUDGE)),
          num('by', 0, 1, (ms) => releaseBy(Math.round((ms * TICK_HZ) / 1000)), 'ms')),
        group('Loudness',
          button('Softer', () => scaleVel(SOFTER)), button('Louder', () => scaleVel(LOUDER)),
          num('velocity', Math.min(...vels) === Math.max(...vels) ? vels[0] : medianOf(vels), 1, (x) => setVel(x))),
        single && group('Note',
          num('starts at', (single.on / TICK_HZ).toFixed(3), 0.001, (s) => setStart(single, s), 's'),
          num('length', Math.round(((single.off - single.on) * 1000) / TICK_HZ), 1, (ms) => setLength(single, ms), 'ms')),
        !single && group('Chords',
          button('Start together', () => tool((n) => T.alignStarts(n), 'notes moved'), { title: 'Each chord (presses within 70 ms) starts at the middle of its starts' }),
          button('Let go together', () => tool((n) => T.alignReleases(n), 'releases moved'), { title: 'Each chord lets go at the middle of its releases' })),
        !single && group('Shape', shapeForm(),
          num('even out', 50, 10, (x) => tool((n) => T.evenOut(n, x), 'notes evened out'), '%', 'Apply')),
        !single && group('Lengths',
          num('all', 250, 10, (ms) => tool((n) => { T.setLengths(n, (ms * TICK_HZ) / 1000); return n.length; }, 'notes'), 'ms'),
          button('Legato', () => tool((n) => T.legato(n), 'notes held to the next'), { title: 'Hold each note until the next note starts' })),
        group('Copy',
          button('Copy', copySelection, { title: 'Ctrl+C' }),
          button('Cut', cutSelection, { title: 'Ctrl+X' }),
          button('Duplicate', duplicateSelection, { title: 'Ctrl+D: a copy right after it' }),
          button('Range from it', () => { range = selectionSpan(); renderInspector(); schedule(); }, { title: 'A time range over these notes' })),
        group(null, button('Delete', removeSelected, { icon: 'trash', cls: 'btn-danger' }))));
  }

  function rangeTools() {
    const { a, b } = range;
    return h('div', { class: 'ed-section' },
      h('p', { class: 'ed-sel' }, `Range ${fmtClock(a)} to ${fmtClock(b)} (${((b - a) / TICK_HZ).toFixed(2)} s)`),
      h('div', { class: 'ed-tools' },
        group('Range',
          button('Play it', playRange, { icon: 'play' }),
          button('Select its notes', () => select(doc.notes.filter((n) => n.on >= a && n.on < b), false)),
          button('Clear', () => { range = null; renderInspector(); schedule(); })),
        group('Time',
          button('Copy', () => { clipboard = T.copyRange(doc, a, b); toast('Range copied, notes and pedals.'); renderInspector(); }),
          button('Delete it', () => rangeEdit(() => T.deleteTime(doc, a, b), null, 'Time removed: what came after moves back.'), { title: 'Remove this stretch of time; what comes after moves back' }),
          button('Keep only it', () => rangeEdit(() => T.cropTo(doc, a, b, TICK_HZ / 2), null, 'Cut down to the range.'), { title: 'Crop the piece to this range' })),
        group('Tempo',
          num('stretch to', 100, 1, (pct) => {
            if (!(pct >= 25 && pct <= 400)) { toast('Stretch between 25% and 400%.', 'error'); return; }
            const f = pct / 100;
            rangeEdit(() => T.stretch(doc, a, b, f), { a, b: Math.round(a + (b - a) * f) }, `Range now ${pct}% as long.`);
          }, '%', 'Apply')),
        group('Damper',
          button('Down', () => rangeEdit(() => T.setPedal(doc, 'damper', a, b, 255), range)),
          button('Half', () => rangeEdit(() => T.setPedal(doc, 'damper', a, b, 128), range)),
          button('Up', () => rangeEdit(() => T.setPedal(doc, 'damper', a, b, 0), range))),
        group('Soft',
          button('Down', () => rangeEdit(() => T.setPedal(doc, 'soft', a, b, 255), range)),
          button('Up', () => rangeEdit(() => T.setPedal(doc, 'soft', a, b, 0), range)))));
  }

  function cursorTools() {
    return h('div', { class: 'ed-section' },
      h('p', { class: 'ed-sel' }, `Cursor at ${fmtClock(cursor)}.`),
      h('div', { class: 'ed-tools' },
        group('At the cursor',
          num('insert', 1, 0.1, (s) => {
            if (!(s > 0 && s <= 600)) return;
            rangeEdit(() => T.insertTime(doc, cursor, s * TICK_HZ), null, `${s} s of silence inserted.`);
          }, 's', 'Insert')),
        clipboard && group('Paste',
          button('Mix in', () => pasteClip(false), { title: 'Ctrl+V: add the copied notes at the cursor' }),
          button('Insert', () => pasteClip(true), { title: 'Ctrl+Shift+V: open a gap at the cursor and put the copy (with its pedals) in it' }),
          h('span', { class: 'ed-label' }, `${clipboard.notes.length} notes, ${(clipboard.len / TICK_HZ).toFixed(2)} s`)),
        group('Select',
          button('All', () => select(doc.notes, false)),
          button('Velocity 0', () => select(doc.notes.filter((n) => n.vel === 0), false), { title: 'Presses with no hammer reading' }),
          button('Silent', () => select(doc.notes.filter((n) => n.vel >= SILENT), false), { title: 'Silent presses (velocity 640 or more)' }),
          sel.size > 0 && button('Invert', () => select(doc.notes.filter((n) => !sel.has(n)), false)))));
  }

  // A crescendo or diminuendo: louder (or softer, below zero) by one
  // percentage at the first selected note, gradually to another at the last.
  function shapeForm() {
    const from = h('input', { class: 'input ed-num-input', type: 'number', step: '5', value: '0', 'aria-label': 'Loudness change at the first note (%)' });
    const to = h('input', { class: 'input ed-num-input', type: 'number', step: '5', value: '20', 'aria-label': 'Loudness change at the last note (%)' });
    return h('form', { class: 'ed-num', onsubmit: (e) => {
      e.preventDefault();
      const x = parseFloat(from.value);
      const y = parseFloat(to.value);
      if (Number.isFinite(x) && Number.isFinite(y)) tool((n) => T.shapeLoudness(n, x, y), 'notes reshaped');
    } },
    h('label', null, 'louder from ', from, ' % to ', to, ' %'), h('button', { class: 'btn btn-small', type: 'submit', title: 'Gradually, first note to last: a crescendo (or, with negative numbers, a diminuendo)' }, 'Apply'));
  }

  // tool runs an operation on the selected notes as one undoable step.
  function tool(fn, words) {
    if (!sel.size) return;
    begin();
    const n = fn([...sel]);
    changed();
    if (words && Number.isFinite(n)) toast(`${n} ${words}.`);
  }

  // rangeEdit runs an operation on the whole document as one undoable
  // step, then keeps the selection to notes that still exist.
  function rangeEdit(fn, newRange, words) {
    begin();
    fn();
    const live = new Set(doc.notes);
    sel = new Set([...sel].filter((n) => live.has(n)));
    if (newRange !== undefined) range = newRange;
    changed();
    if (words) toast(words);
  }

  function selectionSpan() {
    let a = Infinity;
    let b = 0;
    for (const n of sel) { a = Math.min(a, n.on); b = Math.max(b, n.off); }
    return a < b ? { a, b } : null;
  }

  function copySelection() {
    if (sel.size) {
      clipboard = T.copyNotes([...sel]);
      toast(`${sel.size} notes copied.`);
    } else if (range) {
      clipboard = T.copyRange(doc, range.a, range.b);
      toast('Range copied, notes and pedals.');
    } else return;
    renderInspector();
  }

  function cutSelection() {
    if (sel.size) {
      clipboard = T.copyNotes([...sel]);
      removeSelected();
      toast('Notes cut: paste them at the cursor.');
    } else if (range) {
      clipboard = T.copyRange(doc, range.a, range.b);
      const { a, b } = range;
      rangeEdit(() => T.deleteTime(doc, a, b), null, 'Range cut: paste it at the cursor.');
    }
  }

  function duplicateSelection() {
    if (!sel.size) return;
    const c = T.copyNotes([...sel]);
    const span = selectionSpan();
    begin();
    const added = T.paste(doc, c, span.b, false);
    sel = new Set(added);
    changed();
  }

  function pasteClip(insert) {
    if (!clipboard) return;
    begin();
    const added = T.paste(doc, clipboard, cursor, insert);
    sel = new Set(added);
    changed();
    toast(insert ? 'Pasted into a gap at the cursor.' : 'Pasted at the cursor.');
  }

  function medianOf(v) { return median(v); }

  function selectionText(ns) {
    let on = Infinity;
    let off = 0;
    const keys = new Set();
    let lo = Infinity;
    let hi = 0;
    let zero = 0;
    for (const n of ns) {
      on = Math.min(on, n.on); off = Math.max(off, n.off); keys.add(n.key);
      if (n.vel === 0) zero++; else { lo = Math.min(lo, n.vel); hi = Math.max(hi, n.vel); }
    }
    const names = [...keys].sort((a, b) => a - b).map(noteName);
    const keyText = names.length <= 6 ? names.join(', ') : `${names.length} keys`;
    const velText = lo === Infinity ? '' : lo === hi ? `velocity ${lo}` : `velocity ${lo}–${hi}`;
    const parts = [
      ns.length === 1 ? '1 note' : `${ns.length.toLocaleString()} notes`, keyText,
      `${fmtClock(on)} to ${fmtClock(off)}`, velText,
      zero && `${zero} with no hammer reading (velocity 0: the player strikes it as hard as it can)`,
      ns.length === 1 && ns[0].vel >= SILENT && 'a silent press',
    ];
    return parts.filter(Boolean).join(' · ');
  }

  function noteText(n) {
    const vel = n.vel === 0 ? 'velocity 0 (no hammer reading)' : n.vel >= SILENT ? `velocity ${n.vel} (silent press)` : `velocity ${n.vel}`;
    return `${noteName(n.key)} · ${fmtClock(n.on)} · ${Math.round(((n.off - n.on) * 1000) / TICK_HZ)} ms long · ${vel}`;
  }

  function renderStatus(extra) {
    const span = (cssW - GUTTER) / view.ppt / TICK_HZ;
    setText(statusEl, [extra || `Cursor ${fmtClock(cursor)}`, `${span < 10 ? span.toFixed(1) : Math.round(span)} s across`].join(' · '));
  }

  // ------------------------------------------------------------- editing

  function snapshot() {
    const index = new Map(doc.notes.map((n, i) => [n, i]));
    return {
      notes: doc.notes.map((n) => [n.key, n.on, n.off, n.vel]),
      damper: doc.damper, soft: doc.soft, end: doc.end, corrected: doc.corrected, ver, range,
      sel: [...sel].map((n) => index.get(n)).filter((i) => i !== undefined),
    };
  }

  function restore(s) {
    doc.notes = s.notes.map(([key, on, off, vel]) => ({ key, on, off, vel }));
    doc.damper = s.damper;
    doc.soft = s.soft;
    doc.end = s.end;
    doc.corrected = s.corrected;
    range = s.range || null;
    ver = s.ver;
    sel = new Set(s.sel.map((i) => doc.notes[i]).filter(Boolean));
  }

  // begin() records the state before an edit; changed() after it.
  function begin() {
    undo.push(snapshot());
    if (undo.length > UNDO_MAX) undo.shift();
    redo = [];
    ver = nextVer++;
  }

  function changed() {
    doc.notes.sort((a, b) => a.on - b.on || a.key - b.key);
    for (const n of doc.notes) doc.end = Math.max(doc.end, n.off);
    renderBar();
    renderInspector();
    schedule();
  }

  function doUndo() {
    if (!undo.length) return;
    redo.push(snapshot());
    restore(undo.pop());
    changed();
  }

  function doRedo() {
    if (!redo.length) return;
    undo.push(snapshot());
    restore(redo.pop());
    changed();
  }

  function edit(fn) {
    if (!sel.size) return;
    begin();
    fn([...sel]);
    changed();
  }

  function moveBy(dt) {
    edit((ns) => {
      const lo = Math.min(...ns.map((n) => n.on));
      const d = Math.max(dt, -lo);
      for (const n of ns) { n.on += d; n.off += d; }
    });
  }

  function releaseBy(dt) { edit((ns) => { for (const n of ns) n.off = Math.max(n.on + MIN_LEN, n.off + dt); }); }

  function transpose(dk) {
    edit((ns) => {
      const lo = Math.min(...ns.map((n) => n.key));
      const hi = Math.max(...ns.map((n) => n.key));
      const d = Math.max(LOW - lo, Math.min(HIGH - hi, dk));
      for (const n of ns) n.key += d;
    });
  }

  function scaleVel(f) { edit((ns) => { for (const n of ns) n.vel = clampVel((n.vel || medianVel) * f); }); }
  function setVel(v) { edit((ns) => { for (const n of ns) n.vel = v <= 0 ? 0 : clampVel(v); }); }

  function setStart(n, s) {
    edit(() => {
      const len = n.off - n.on;
      n.on = Math.max(0, Math.round(s * TICK_HZ));
      n.off = n.on + len;
    });
  }

  function setLength(n, ms) { edit(() => { n.off = n.on + Math.max(MIN_LEN, Math.round((ms * TICK_HZ) / 1000)); }); }

  function removeSelected() {
    if (!sel.size) return;
    begin();
    doc.notes = doc.notes.filter((n) => !sel.has(n));
    sel.clear();
    changed();
  }

  function addNote(key, on) {
    begin();
    const n = { key, on: Math.max(0, Math.round(on)), off: Math.max(0, Math.round(on)) + TICK_HZ / 4, vel: medianVel };
    doc.notes.push(n);
    sel = new Set([n]);
    changed();
  }

  function select(notes, add) {
    if (!add) sel = new Set();
    for (const n of notes) sel.add(n);
    renderInspector();
    schedule();
  }

  // ---------------------------------------------------------- save, play

  function requestBody() {
    return {
      notes: doc.notes.map((n) => [n.key, n.on, n.off, n.vel]),
      damper: doc.damper !== orig.damper ? doc.damper : null,
      soft: doc.soft !== orig.soft ? doc.soft : null,
      end: doc.end,
      corrected: !!doc.corrected,
    };
  }

  // applyCorrections puts the recording corrections into the notes, as
  // the server applies them when a recording plays (one undoable step).
  async function applyCorrections() {
    tb.apply.disabled = true;
    try {
      const r = await api('POST', '/corrections/apply', { notes: doc.notes.map((n) => [n.key, n.on, n.off, n.vel]) });
      begin();
      doc.notes = r.notes.map(([key, on, off, vel]) => ({ key, on, off, vel }));
      doc.corrected = true;
      sel = new Set();
      changed();
      toast(`${r.changed} notes corrected (${r.summary}). Undo takes it back.`);
    } catch (err) {
      toast(err.message, 'error');
    } finally {
      tb.apply.disabled = false;
    }
  }

  async function save(asNew) {
    if (saving) return;
    const replace = isCopy() && !asNew;
    saving = true;
    renderBar();
    try {
      const body = requestBody();
      body.replace = replace;
      const r = await api('POST', path + '/edits', body);
      savedVer = ver;
      drafts.delete(id);
      if (replace) {
        piece = r.piece;
        orig = { damper: doc.damper, soft: doc.soft };
        toast('Saved.');
      } else {
        toast(r.duplicate ? `The library already holds this version: “${r.piece.title}”.` : `Saved as “${r.piece.title}”. You are now editing the copy.`);
        handoff = { id: r.piece.id, view: { ...view }, cursor };
        location.hash = editHref(r.piece.id);
      }
    } catch (err) {
      toast(err.message, 'error');
    } finally {
      saving = false;
      if (alive) renderBar();
    }
  }

  async function playFrom() {
    const from = Math.max(0, Math.min(Math.round(cursor), doc.end));
    const body = requestBody();
    body.from = from;
    previews.set(id, from);
    try {
      const r = await api('POST', path + '/preview', body);
      if (isStatus(r)) applyReplyStatus(r);
      toast(`Playing from ${fmtClock(from, 1)}. The piano starts in a few seconds.`);
    } catch (err) {
      toast(err.message, 'error');
    }
  }

  function stopPlay() { ui.command('/deck/stop'); }

  // playRange plays the time range only, then lets it ring for a second.
  async function playRange() {
    if (!range) return;
    const body = requestBody();
    body.from = range.a;
    body.to = range.b;
    previews.set(id, range.a);
    try {
      const r = await api('POST', path + '/preview', body);
      if (isStatus(r)) applyReplyStatus(r);
      toast(`Playing ${fmtClock(range.a, 1)} to ${fmtClock(range.b, 1)}. The piano starts in a few seconds.`);
    } catch (err) {
      toast(err.message, 'error');
    }
  }

  function ourPreview(s) {
    return !!s && !!s.piece && s.piece.id === id && s.piece.performer === PREVIEW_PERFORMER && (s.state === 'playing' || s.state === 'paused');
  }

  // ----------------------------------------------------------- geometry

  function layout() {
    const rollTop = RULER;
    const rollBottom = RULER + rollH;
    const velTop = rollBottom + GAP;
    const damperTop = velTop + VEL_H + GAP;
    const softTop = damperTop + PED_H + GAP;
    const overTop = softTop + PED_H + GAP;
    return { rollTop, rollBottom, velTop, damperTop, softTop, overTop };
  }

  function xOf(t) { return GUTTER + (t - view.t0) * view.ppt; }
  function tOf(x) { return view.t0 + (x - GUTTER) / view.ppt; }
  function rowTop(key) { return RULER + (HIGH - key) * view.rowH - view.y; }
  function keyOf(y) { return HIGH - Math.floor((y - RULER + view.y) / view.rowH); }
  function minPpt() { return (cssW - GUTTER) / (doc.end + TICK_HZ); }

  function clampView() {
    if (!doc) return;
    view.ppt = Math.max(minPpt(), Math.min(3, view.ppt));
    const span = (cssW - GUTTER) / view.ppt;
    view.t0 = Math.max(0, Math.min(view.t0, Math.max(0, doc.end + TICK_HZ / 2 - span)));
    view.y = Math.max(0, Math.min(view.y, KEYS * view.rowH - rollH));
  }

  function zoomAt(f, x) {
    const t = tOf(x);
    view.ppt *= f;
    clampView();
    view.t0 = t - (x - GUTTER) / view.ppt;
    clampView();
    schedule();
  }

  function fitAll() {
    view.t0 = 0;
    view.ppt = minPpt();
    clampView();
    schedule();
  }

  function sizeCanvas() {
    if (!wrap.isConnected) return;
    const rect = wrap.getBoundingClientRect();
    const main = document.getElementById('view');
    const pad = main ? parseFloat(getComputedStyle(main).paddingBottom) || 0 : 0;
    // Room under the canvas for the status line, and for the selection
    // panel when it sits below rather than beside the roll.
    const below = insp.isConnected && insp.getBoundingClientRect().left < rect.left + 20;
    const avail = window.innerHeight - rect.top - pad - 36 - (below ? 190 : 0);
    cssW = Math.max(320, Math.floor(wrap.clientWidth));
    rollH = Math.max(140, Math.floor(avail - FIXED_H));
    cssH = FIXED_H + rollH;
    dpr = window.devicePixelRatio || 1;
    const w = Math.round(cssW * dpr);
    const hh = Math.round(cssH * dpr);
    if (canvas.width !== w || canvas.height !== hh) {
      canvas.width = w;
      canvas.height = hh;
      canvas.style.width = cssW + 'px';
      canvas.style.height = cssH + 'px';
    }
    if (doc) clampView();
  }

  function region(x, y) {
    const L = layout();
    if (y < RULER) return x < GUTTER ? 'none' : 'ruler';
    if (y < L.rollBottom) return x < GUTTER ? 'keys' : 'roll';
    if (y >= L.velTop && y < L.velTop + VEL_H) return x < GUTTER ? 'none' : 'vel';
    if (y >= L.damperTop && y < L.damperTop + PED_H) return x < GUTTER ? 'none' : 'damper';
    if (y >= L.softTop && y < L.softTop + PED_H) return x < GUTTER ? 'none' : 'soft';
    if (y >= L.overTop && y < L.overTop + OVERVIEW) return 'overview';
    return 'none';
  }

  // The note under a point of the roll, and which part of it.
  function hitNote(x, y, alt) {
    const key = keyOf(y);
    const t = tOf(x);
    const slack = 3 / view.ppt;
    let best = null;
    for (const n of doc.notes) {
      if (n.key !== key || n.on - slack > t || n.off + slack < t) continue;
      if (!best || sel.has(n) || n.on > best.on) best = n;
      if (sel.has(n)) break;
    }
    if (!best) return null;
    const xa = xOf(best.on);
    const xb = xOf(best.off);
    let part = 'body';
    if (xb - xa > 10 && x >= xb - 5) part = 'end';
    else if (xb - xa > 14 && x <= xa + 4) part = 'start';
    else if (alt) part = 'end';
    return { note: best, part };
  }

  // The note whose loudness stalk is nearest a point of the velocity lane.
  function hitStalk(x) {
    let best = null;
    let bestD = 6;
    for (const n of doc.notes) {
      const d = Math.abs(xOf(n.on) - x);
      if (d < bestD || (d <= bestD && best && sel.has(n) && !sel.has(best))) { best = n; bestD = d; }
    }
    return best;
  }

  // -------------------------------------------------------------- input

  function pos(e) {
    const r = canvas.getBoundingClientRect();
    return { x: e.clientX - r.left, y: e.clientY - r.top };
  }

  function onDown(e) {
    if (!doc) return;
    canvas.focus({ preventScroll: true });
    const p = pos(e);
    if (e.pointerType === 'touch') {
      touches.set(e.pointerId, p);
      if (touches.size === 2) {
        const [a, b] = [...touches.values()];
        drag = { mode: 'pinch', d0: Math.hypot(a.x - b.x, a.y - b.y) || 1, ppt0: view.ppt, t: tOf((a.x + b.x) / 2), y0: view.y, my0: (a.y + b.y) / 2 };
        return;
      }
    }
    try {
      canvas.setPointerCapture(e.pointerId);
    } catch {
      // not a live pointer (a synthetic event): no capture needed
    }
    const where = region(p.x, p.y);
    const base = { start: p, moved: false, id: e.pointerId };
    if (e.button === 1 || e.button === 2) {
      drag = { ...base, mode: 'pan', t0: view.t0, y0: view.y };
      e.preventDefault();
      return;
    }
    if (e.button !== 0) return;
    switch (where) {
      case 'ruler': {
        // A drag marks a time range; a click moves the cursor.
        const t = Math.max(0, Math.round(tOf(p.x)));
        drag = { ...base, mode: 'range', t, keep: range };
        break;
      }
      case 'overview':
        drag = { ...base, mode: 'overview' };
        overviewTo(p.x);
        break;
      case 'keys': {
        const key = keyOf(p.y);
        if (key >= LOW && key <= HIGH) select(doc.notes.filter((n) => n.key === key), e.shiftKey || e.ctrlKey || e.metaKey);
        break;
      }
      case 'roll': {
        const hit = hitNote(p.x, p.y, e.altKey);
        if (e.pointerType === 'touch' && !(hit && sel.has(hit.note))) {
          drag = { ...base, mode: 'pan', t0: view.t0, y0: view.y, tap: hit };
          break;
        }
        if (hit) {
          if (e.shiftKey || e.ctrlKey || e.metaKey) {
            if (sel.has(hit.note)) sel.delete(hit.note); else sel.add(hit.note);
            renderInspector();
            schedule();
            break;
          }
          const already = sel.has(hit.note);
          if (!already) select([hit.note], false);
          const from = new Map([...sel].map((n) => [n, { on: n.on, off: n.off, key: n.key }]));
          drag = { ...base, mode: hit.part, note: hit.note, from, axis: null, already };
        } else {
          drag = { ...base, mode: 'box', keep: e.shiftKey || e.ctrlKey || e.metaKey ? new Set(sel) : new Set(), end: p };
          if (!drag.keep.size && sel.size) select([], false);
        }
        break;
      }
      case 'vel': {
        const n = hitStalk(p.x);
        if (!n) break;
        if (!sel.has(n)) select([n], false);
        const from = new Map([...sel].map((m) => [m, m.vel]));
        drag = { ...base, mode: 'vel', note: n, from };
        break;
      }
      case 'damper':
      case 'soft': {
        const name = where;
        drag = { ...base, mode: 'pedal', name, last: null };
        paint(p);
        break;
      }
      default:
        break;
    }
  }

  function onMove(e) {
    if (!doc) return;
    const p = pos(e);
    if (e.pointerType === 'touch' && touches.has(e.pointerId)) touches.set(e.pointerId, p);
    if (!drag) { hover(p, e); return; }
    if (drag.mode === 'pinch') {
      if (touches.size < 2) return;
      const [a, b] = [...touches.values()];
      const mx = (a.x + b.x) / 2;
      view.ppt = drag.ppt0 * (Math.hypot(a.x - b.x, a.y - b.y) / drag.d0);
      clampView();
      view.t0 = drag.t - (mx - GUTTER) / view.ppt;
      view.y = drag.y0 - ((a.y + b.y) / 2 - drag.my0);
      clampView();
      schedule();
      return;
    }
    if (e.pointerId !== drag.id) return;
    const dx = p.x - drag.start.x;
    const dy = p.y - drag.start.y;
    if (!drag.moved && Math.hypot(dx, dy) < 3) return;
    const first = !drag.moved;
    drag.moved = true;
    switch (drag.mode) {
      case 'pan':
        view.t0 = drag.t0 - dx / view.ppt;
        view.y = drag.y0 - dy;
        clampView();
        break;
      case 'scrub':
        cursor = Math.max(0, Math.round(tOf(p.x)));
        renderInspectorIfEmpty();
        break;
      case 'range': {
        const t = Math.max(0, Math.round(tOf(p.x)));
        range = { a: Math.min(drag.t, t), b: Math.max(drag.t, t) };
        renderStatus(`Range ${fmtClock(range.a)} to ${fmtClock(range.b)} (${((range.b - range.a) / TICK_HZ).toFixed(2)} s)`);
        break;
      }
      case 'overview':
        overviewTo(p.x);
        break;
      case 'box':
        drag.end = p;
        boxSelect();
        break;
      case 'body':
      case 'start':
      case 'end': {
        if (first) begin();
        let dt = Math.round(dx / view.ppt);
        let dk = 0;
        if (drag.mode === 'body') {
          if (!drag.axis && Math.hypot(dx, dy) > 5) drag.axis = Math.abs(dx) >= Math.abs(dy) ? 'time' : 'key';
          if (drag.axis === 'key') { dt = 0; dk = -Math.round(dy / view.rowH); } else dk = 0;
        }
        const fr = [...drag.from.values()];
        if (drag.mode === 'body') {
          dt = Math.max(dt, -Math.min(...fr.map((o) => o.on)));
          dk = Math.max(LOW - Math.min(...fr.map((o) => o.key)), Math.min(HIGH - Math.max(...fr.map((o) => o.key)), dk));
        }
        for (const [n, o] of drag.from) {
          if (drag.mode === 'body') { n.on = o.on + dt; n.off = o.off + dt; n.key = o.key + dk; }
          else if (drag.mode === 'end') n.off = Math.max(n.on + MIN_LEN, o.off + dt);
          else n.on = Math.max(0, Math.min(o.off - MIN_LEN, o.on + dt));
        }
        const what = drag.mode === 'body' ? (dk ? `${dk > 0 ? '+' : '−'}${Math.abs(dk)} key${Math.abs(dk) === 1 ? '' : 's'}` : `moved ${msText(dt)}`) : drag.mode === 'end' ? `release ${msText(dt)}` : `start ${msText(dt)}`;
        renderStatus(`${drag.from.size === 1 ? noteText(drag.note) : `${drag.from.size} notes`} · ${what}`);
        break;
      }
      case 'vel': {
        if (first) begin();
        const L = layout();
        const f = (L.velTop + VEL_H - 3 - p.y) / (VEL_H - 8);
        const df = f - loud(drag.from.get(drag.note) || medianVel);
        for (const [n, v0] of drag.from) n.vel = velOfLoud(loud(v0 || medianVel) + df);
        renderStatus(drag.from.size === 1 ? noteText(drag.note) : `${drag.from.size} notes · velocity ${drag.note.vel} for the one you hold`);
        break;
      }
      case 'pedal':
        paint(p);
        break;
      default:
        break;
    }
    schedule();
  }

  function onUp(e) {
    if (e.pointerType === 'touch') touches.delete(e.pointerId);
    if (!drag) return;
    if (drag.mode === 'pinch') {
      if (touches.size < 2) drag = null;
      return;
    }
    if (e.pointerId !== drag.id) return;
    const d = drag;
    drag = null;
    const p = pos(e);
    if (d.mode === 'range') {
      if (!d.moved) {
        // A click on the ruler moves the cursor; the range stays.
        range = d.keep;
        cursor = d.t;
      } else if (range && (range.b - range.a) * view.ppt < 4) {
        range = null;
      }
      renderInspector();
      renderStatus();
      schedule();
      return;
    }
    if (!d.moved) {
      if (d.mode === 'pan' && d.tap !== undefined) {
        if (d.tap) select([d.tap.note], false);
        else { cursor = Math.max(0, Math.round(tOf(p.x))); select([], false); }
      } else if (d.mode === 'box') {
        cursor = Math.max(0, Math.round(tOf(p.x)));
        renderInspector();
      } else if (['body', 'start', 'end'].includes(d.mode) && d.already && sel.size > 1) {
        select([d.note], false); // a click on one of several selected notes
      }
    } else if (['body', 'start', 'end', 'vel'].includes(d.mode)) {
      changed();
    } else if (d.mode === 'box') {
      renderInspector();
    }
    if (d.mode === 'pedal' && d.began) changed();
    renderStatus();
    schedule();
  }

  function renderInspectorIfEmpty() { if (!sel.size) renderInspector(); }

  function boxSelect() {
    const a = drag.start;
    const b = drag.end;
    const t1 = tOf(Math.min(a.x, b.x));
    const t2 = tOf(Math.max(a.x, b.x));
    const k1 = keyOf(Math.max(a.y, b.y));
    const k2 = keyOf(Math.min(a.y, b.y));
    sel = new Set(drag.keep);
    for (const n of doc.notes) {
      if (n.key >= k1 && n.key <= k2 && n.on <= t2 && n.off >= t1) sel.add(n);
    }
  }

  function overviewTo(x) {
    const span = (cssW - GUTTER) / view.ppt;
    const t = ((x - GUTTER) / (cssW - GUTTER)) * (doc.end + TICK_HZ);
    view.t0 = t - span / 2;
    clampView();
    schedule();
  }

  // Draws a pedal curve under the pointer, joining it to the last point.
  function paint(p) {
    const L = layout();
    const top = drag.name === 'damper' ? L.damperTop : L.softTop;
    const period = drag.name === 'damper' ? doc.dp : doc.sp;
    const value = Math.round(Math.max(0, Math.min(1, (top + PED_H - 2 - p.y) / (PED_H - 6))) * 255);
    const t = Math.max(0, tOf(p.x));
    if (!drag.began) {
      begin();
      doc[drag.name] = doc[drag.name].slice(); // copy on write: undo keeps the old one
      drag.began = true;
    }
    const arr = doc[drag.name];
    const i1 = Math.min(Math.floor(t / period), Math.ceil((doc.end + TICK_HZ) / period));
    const last = drag.last || { i: i1, v: value };
    const lo = Math.min(last.i, i1);
    const hi = Math.max(last.i, i1);
    const fill0 = arr.length ? arr[arr.length - 1] : 0;
    while (arr.length <= hi) arr.push(fill0);
    for (let i = lo; i <= hi; i++) {
      const f = hi === lo ? 1 : (i - last.i) / (i1 - last.i);
      arr[i] = Math.round(last.v + (value - last.v) * (Number.isFinite(f) ? f : 1));
    }
    drag.last = { i: i1, v: value };
    renderStatus(`${drag.name === 'damper' ? 'Damper' : 'Soft pedal'} ${Math.round((value / 255) * 100)}% down at ${fmtClock(t)}`);
  }

  function hover(p, e) {
    const where = region(p.x, p.y);
    let cur = 'default';
    let text = null;
    let key = null;
    if (where === 'roll') {
      const hit = hitNote(p.x, p.y, e.altKey);
      if (hit) { cur = hit.part === 'body' ? 'move' : 'ew-resize'; text = noteText(hit.note); }
      else cur = 'crosshair';
      key = keyOf(p.y);
    } else if (where === 'keys') {
      cur = 'pointer';
      key = keyOf(p.y);
      text = key >= LOW && key <= HIGH ? `${noteName(key)}: click for all its notes` : null;
    } else if (where === 'vel') {
      const n = hitStalk(p.x);
      if (n) { cur = 'ns-resize'; text = noteText(n); }
    } else if (where === 'damper' || where === 'soft') {
      cur = 'crosshair';
      text = `Drag to draw the ${where === 'damper' ? 'damper' : 'soft'} pedal: up is down`;
    } else if (where === 'ruler' || where === 'overview') {
      cur = 'pointer';
    }
    canvas.style.cursor = cur;
    if (key !== hoverKey) { hoverKey = key; schedule(); }
    renderStatus(text);
  }

  function onWheel(e) {
    if (!doc) return;
    e.preventDefault();
    const p = pos(e);
    const unit = e.deltaMode === 1 ? 16 : e.deltaMode === 2 ? cssH : 1;
    const dx = e.deltaX * unit;
    const dy = e.deltaY * unit;
    if (e.ctrlKey || e.metaKey) {
      zoomAt(Math.exp(-dy * 0.0025), Math.max(GUTTER, p.x));
      return;
    }
    if (e.altKey) {
      // Taller or shorter rows, keeping the key under the pointer in place.
      const d = dy || dx;
      if (!d) return;
      const old = view.rowH;
      const next = Math.max(4, Math.min(28, d < 0 ? Math.max(old + 1, Math.round(old * 1.15)) : Math.min(old - 1, Math.round(old / 1.15))));
      const at = Math.max(0, p.y - RULER);
      view.y = ((at + view.y) / old) * next - at;
      view.rowH = next;
      clampView();
      schedule();
      return;
    }
    const where = region(p.x, p.y);
    if (e.shiftKey || Math.abs(dx) > Math.abs(dy) || (where !== 'roll' && where !== 'keys')) {
      view.t0 += (Math.abs(dx) > Math.abs(dy) ? dx : dy) / view.ppt;
    } else {
      view.y += dy;
    }
    clampView();
    schedule();
  }

  function onKey(e) {
    if (!doc || e.defaultPrevented) return;
    const t = e.target;
    if (t && t.closest && t.closest('input, textarea, select, dialog')) return;
    const onButton = t && t.closest && t.closest('button, a');
    const mod = e.ctrlKey || e.metaKey;
    const k = e.key;
    let handled = true;
    if (mod && (k === 'z' || k === 'Z')) { if (e.shiftKey) doRedo(); else doUndo(); }
    else if (mod && (k === 'y' || k === 'Y')) doRedo();
    else if (mod && (k === 's' || k === 'S')) save(false);
    else if (mod && (k === 'a' || k === 'A')) select(doc.notes, false);
    else if (mod && (k === 'c' || k === 'C')) copySelection();
    else if (mod && (k === 'x' || k === 'X')) cutSelection();
    else if (mod && (k === 'v' || k === 'V')) pasteClip(e.shiftKey);
    else if (mod && (k === 'd' || k === 'D')) duplicateSelection();
    else if (mod) handled = false;
    else if (k === ' ' && !onButton) { if (ourPreview(app.status)) stopPlay(); else playFrom(); }
    else if ((k === 'Delete' || k === 'Backspace') && !onButton) removeSelected();
    else if (k === 'Escape') { range = null; select([], false); }
    else if (k === 'ArrowLeft' || k === 'ArrowRight') {
      const d = (k === 'ArrowLeft' ? -1 : 1) * (e.shiftKey ? NUDGE * 10 : NUDGE);
      if (!sel.size) { cursor = Math.max(0, cursor + d); renderInspector(); schedule(); }
      else if (e.altKey) releaseBy(d);
      else moveBy(d);
    } else if ((k === 'ArrowUp' || k === 'ArrowDown') && sel.size) transpose((k === 'ArrowUp' ? 1 : -1) * (e.shiftKey ? 12 : 1));
    else if (k === '+' || k === '=') zoomAt(1.6, xOf(cursor) >= GUTTER && xOf(cursor) <= cssW ? xOf(cursor) : GUTTER + (cssW - GUTTER) / 2);
    else if (k === '-' || k === '_') zoomAt(1 / 1.6, GUTTER + (cssW - GUTTER) / 2);
    else if (k === '0') fitAll();
    else if (k === 'Home') { cursor = 0; view.t0 = 0; clampView(); renderInspector(); schedule(); }
    else if (k === 'End') { cursor = doc.end; view.t0 = doc.end; clampView(); renderInspector(); schedule(); }
    else handled = false;
    if (handled) e.preventDefault();
  }

  function onDouble(e) {
    if (!doc) return;
    const p = pos(e);
    if (region(p.x, p.y) !== 'roll' || hitNote(p.x, p.y, false)) return;
    const key = keyOf(p.y);
    if (key >= LOW && key <= HIGH) addNote(key, tOf(p.x));
  }

  function beforeUnload(e) {
    if (doc && dirty()) { e.preventDefault(); e.returnValue = ''; }
  }

  const scheme = window.matchMedia('(prefers-color-scheme: dark)');
  const onScheme = () => { readColors(); schedule(); };
  const ro = new ResizeObserver(() => { sizeCanvas(); schedule(); });
  const onResize = () => { sizeCanvas(); schedule(); };
  const noMenu = (e) => e.preventDefault();

  function attach() {
    canvas.addEventListener('pointerdown', onDown);
    canvas.addEventListener('pointermove', onMove);
    canvas.addEventListener('pointerup', onUp);
    canvas.addEventListener('pointercancel', onUp);
    canvas.addEventListener('pointerleave', () => { if (!drag && hoverKey !== null) { hoverKey = null; schedule(); } });
    canvas.addEventListener('wheel', onWheel, { passive: false });
    canvas.addEventListener('dblclick', onDouble);
    canvas.addEventListener('contextmenu', noMenu);
    document.addEventListener('keydown', onKey);
    window.addEventListener('beforeunload', beforeUnload);
    window.addEventListener('resize', onResize);
    scheme.addEventListener('change', onScheme);
    ro.observe(wrap);
  }

  // -------------------------------------------------------------- drawing

  function schedule() { if (!raf && alive) raf = requestAnimationFrame(draw); }

  function gridStep() {
    for (const s of STEPS) if (s * TICK_HZ * view.ppt >= 64) return s;
    return STEPS[STEPS.length - 1];
  }

  function gridLines(c, top, height, step) {
    const st = step * TICK_HZ;
    const first = Math.floor(Math.max(0, tOf(GUTTER)) / st) * st;
    c.fillStyle = colors.line;
    for (let t = first; t <= tOf(cssW); t += st) {
      const x = Math.round(xOf(t));
      if (x >= GUTTER) c.fillRect(x, top, 1, height);
    }
  }

  function clip(c, x, y, w, hh) {
    c.save();
    c.beginPath();
    c.rect(x, y, w, hh);
    c.clip();
  }

  function draw() {
    raf = 0;
    if (!doc || !cssW || !alive) return;
    const c = ctx;
    const L = layout();
    const step = gridStep();
    const ta = tOf(GUTTER) - 2 / view.ppt;
    const tb = tOf(cssW);
    c.setTransform(dpr, 0, 0, dpr, 0, 0);
    c.fillStyle = colors.bg;
    c.fillRect(0, 0, cssW, cssH);
    const tw = cssW - GUTTER;

    // The roll.
    clip(c, GUTTER, L.rollTop, tw, rollH);
    c.fillStyle = colors.elev;
    c.fillRect(GUTTER, L.rollTop, tw, rollH);
    for (let k = HIGH; k >= LOW; k--) {
      const y = rowTop(k);
      if (y > L.rollBottom || y + view.rowH < L.rollTop) continue;
      if (isBlackKey(k)) {
        c.fillStyle = colors.sunk;
        c.fillRect(GUTTER, y, tw, view.rowH);
      }
      if (k === hoverKey) {
        c.globalAlpha = 0.08;
        c.fillStyle = colors.accent;
        c.fillRect(GUTTER, y, tw, view.rowH);
        c.globalAlpha = 1;
      }
      if (k % 12 === 0) {
        c.fillStyle = colors.strong;
        c.fillRect(GUTTER, y + view.rowH - 1, tw, 1);
      }
    }
    const xe = xOf(doc.end);
    if (xe < cssW) {
      c.globalAlpha = 0.6;
      c.fillStyle = colors.sunk;
      c.fillRect(Math.max(GUTTER, xe), L.rollTop, cssW - Math.max(GUTTER, xe), rollH);
      c.globalAlpha = 1;
    }
    gridLines(c, L.rollTop, rollH, step);
    const rh = Math.max(2, view.rowH - 2);
    for (const pass of [false, true]) {
      for (const n of doc.notes) {
        if (n.on > tb) break;
        if (n.off < ta || sel.has(n) !== pass) continue;
        const y = rowTop(n.key) + 1;
        if (y > L.rollBottom || y + rh < L.rollTop) continue;
        const x1 = xOf(n.on);
        const w = Math.max(2, xOf(n.off) - x1);
        if (pass) {
          c.fillStyle = colors.ink;
          c.fillRect(x1, y, w, rh);
          c.strokeStyle = colors.accent;
          c.lineWidth = 1.5;
          c.strokeRect(x1 + 0.75, y + 0.75, Math.max(0.5, w - 1.5), Math.max(0.5, rh - 1.5));
        } else if (n.vel === 0) {
          c.fillStyle = colors.danger;
          c.fillRect(x1, y, w, rh);
        } else if (n.vel >= SILENT) {
          c.strokeStyle = colors.muted;
          c.lineWidth = 1;
          c.strokeRect(x1 + 0.5, y + 0.5, Math.max(0.5, w - 1), Math.max(0.5, rh - 1));
        } else {
          c.globalAlpha = 0.3 + 0.7 * loud(n.vel);
          c.fillStyle = colors.accent;
          c.fillRect(x1, y, w, rh);
          c.globalAlpha = 1;
        }
      }
    }
    // Note names, when there is room for them.
    if (view.rowH >= 11) {
      c.font = `${Math.min(11, view.rowH - 2)}px system-ui, sans-serif`;
      c.textBaseline = 'middle';
      for (const n of doc.notes) {
        if (n.on > tb) break;
        if (n.off < ta) continue;
        const x1 = Math.max(GUTTER, xOf(n.on));
        const w = xOf(n.off) - x1;
        const y = rowTop(n.key) + 1;
        if (w < 26 || y > L.rollBottom || y + rh < L.rollTop) continue;
        c.fillStyle = sel.has(n) ? colors.onInk : colors.ink;
        c.fillText(noteName(n.key), x1 + 3, y + rh / 2 + 0.5);
      }
    }
    if (drag && drag.mode === 'box' && drag.moved) {
      const a = drag.start;
      const b = drag.end;
      c.setLineDash([4, 3]);
      c.strokeStyle = colors.ink2;
      c.lineWidth = 1;
      c.strokeRect(Math.min(a.x, b.x) + 0.5, Math.min(a.y, b.y) + 0.5, Math.abs(a.x - b.x), Math.abs(a.y - b.y));
      c.setLineDash([]);
    }
    c.restore();

    // Loudness: a stalk per note, taller is louder.
    clip(c, GUTTER, L.velTop, tw, VEL_H);
    c.fillStyle = colors.elev;
    c.fillRect(GUTTER, L.velTop, tw, VEL_H);
    gridLines(c, L.velTop, VEL_H, step);
    const base = L.velTop + VEL_H - 3;
    for (const pass of [false, true]) {
      for (const n of doc.notes) {
        if (n.on > tb) break;
        if (n.on < ta || sel.has(n) !== pass) continue;
        const x = Math.round(xOf(n.on));
        const hh = Math.max(2, loud(n.vel) * (VEL_H - 8));
        c.fillStyle = pass ? colors.ink : n.vel === 0 ? colors.danger : n.vel >= SILENT ? colors.muted : colors.accent;
        c.fillRect(x, base - hh, 1, hh);
        c.fillRect(x - 1, base - hh - 1, 3, 3);
      }
    }
    c.restore();

    drawPedal(c, doc.damper, doc.dp, L.damperTop, step);
    drawPedal(c, doc.soft, doc.sp, L.softTop, step);

    // The ruler.
    c.fillStyle = colors.elev;
    c.fillRect(GUTTER, 0, tw, RULER);
    c.fillStyle = colors.strong;
    c.fillRect(GUTTER, RULER - 1, tw, 1);
    const dec = step >= 1 ? 0 : step >= 0.1 ? 1 : 2;
    const st = step * TICK_HZ;
    c.font = '11px system-ui, sans-serif';
    c.textBaseline = 'middle';
    c.fillStyle = colors.muted;
    for (let t = Math.floor(Math.max(0, tOf(GUTTER)) / st) * st; t <= tb; t += st) {
      const x = Math.round(xOf(t));
      if (x < GUTTER) continue;
      c.fillRect(x, RULER - 7, 1, 6);
      c.fillText(fmtClock(t, dec), x + 3, RULER / 2 - 1);
    }

    // The keyboard and the lane names.
    c.fillStyle = colors.bg;
    c.fillRect(0, 0, GUTTER, cssH);
    clip(c, 0, L.rollTop, GUTTER, rollH);
    const keysWithSel = new Set([...sel].map((n) => n.key));
    for (let k = HIGH; k >= LOW; k--) {
      const y = rowTop(k);
      if (y > L.rollBottom || y + view.rowH < L.rollTop) continue;
      c.fillStyle = isBlackKey(k) ? colors.ink : colors.elev;
      c.fillRect(0, y, isBlackKey(k) ? GUTTER - 22 : GUTTER - 2, view.rowH);
      if (!isBlackKey(k)) {
        c.fillStyle = colors.line;
        c.fillRect(0, y + view.rowH - 1, GUTTER - 2, 1);
      }
      if (k === hoverKey || keysWithSel.has(k)) {
        c.fillStyle = colors.accent;
        c.fillRect(GUTTER - 5, y, 3, view.rowH);
      }
      if (k % 12 === 0 && view.rowH >= 7) {
        c.fillStyle = colors.muted;
        c.font = '10px system-ui, sans-serif';
        c.textBaseline = 'middle';
        c.fillText(noteName(k), GUTTER - 30, y + view.rowH / 2);
      }
    }
    c.restore();
    c.font = '11px system-ui, sans-serif';
    c.textBaseline = 'top';
    c.fillStyle = colors.muted;
    c.fillText('Loudness', 6, L.velTop + 4);
    c.fillText('Damper', 6, L.damperTop + 4);
    c.fillText('Soft', 6, L.softTop + 4);

    // The whole piece, with the part in view.
    c.fillStyle = colors.sunk;
    c.fillRect(GUTTER, L.overTop, tw, OVERVIEW);
    const total = doc.end + TICK_HZ;
    c.fillStyle = colors.ink2;
    c.globalAlpha = 0.55;
    for (const n of doc.notes) {
      c.fillRect(GUTTER + (n.on / total) * tw, L.overTop + 2 + ((HIGH - n.key) / KEYS) * (OVERVIEW - 4), 1, 1);
    }
    c.globalAlpha = 1;
    const vx = GUTTER + (view.t0 / total) * tw;
    const vw = Math.max(3, ((tw / view.ppt) / total) * tw);
    c.strokeStyle = colors.accent;
    c.lineWidth = 1.5;
    c.strokeRect(vx + 0.75, L.overTop + 0.75, Math.min(vw, GUTTER + tw - vx) - 1.5, OVERVIEW - 1.5);
    c.fillStyle = colors.accent;
    c.fillRect(GUTTER + (cursor / total) * tw, L.overTop, 1, OVERVIEW);

    // The time range, shaded across the roll and the lanes.
    if (range) {
      const xa = Math.max(GUTTER, xOf(range.a));
      const xb = Math.min(cssW, xOf(range.b));
      if (xb > xa) {
        const bottomR = L.softTop + PED_H;
        c.globalAlpha = 0.12;
        c.fillStyle = colors.accent;
        c.fillRect(xa, 0, xb - xa, bottomR);
        c.globalAlpha = 1;
        c.fillStyle = colors.accent;
        c.fillRect(xa, 0, 1, bottomR);
        c.fillRect(xb - 1, 0, 1, bottomR);
      }
    }

    // The cursor, and the playhead while a preview plays.
    const bottom = L.softTop + PED_H;
    for (const [t, color] of [[cursor, colors.accent], [playhead, colors.rec]]) {
      if (t == null) continue;
      const x = Math.round(xOf(t));
      if (x < GUTTER || x > cssW) continue;
      c.fillStyle = color;
      c.fillRect(x, RULER, 1.5, bottom - RULER);
      c.beginPath();
      c.moveTo(x - 5, 0);
      c.lineTo(x + 6, 0);
      c.lineTo(x + 0.75, 8);
      c.closePath();
      c.fill();
    }
  }

  function drawPedal(c, arr, period, top, step) {
    const tw = cssW - GUTTER;
    clip(c, GUTTER, top, tw, PED_H);
    c.fillStyle = colors.elev;
    c.fillRect(GUTTER, top, tw, PED_H);
    gridLines(c, top, PED_H, step);
    const base = top + PED_H - 2;
    const hh = PED_H - 6;
    const tpp = 1 / view.ppt;
    const last = arr.length ? arr[arr.length - 1] : 0;
    c.beginPath();
    c.moveTo(GUTTER, base);
    for (let x = GUTTER; x <= cssW; x++) {
      const t = tOf(x);
      let v = 0;
      if (t >= 0) {
        const i0 = Math.floor(t / period);
        const i1 = Math.max(i0, Math.floor((t + tpp) / period) - 1);
        if (i0 >= arr.length) v = last;
        else for (let i = i0; i <= Math.min(i1, arr.length - 1, i0 + 64); i++) v = Math.max(v, arr[i]);
      }
      c.lineTo(x, base - (v / 255) * hh);
    }
    c.lineTo(cssW, base);
    c.closePath();
    c.globalAlpha = 0.28;
    c.fillStyle = colors.accent;
    c.fill();
    c.globalAlpha = 1;
    c.strokeStyle = colors.accent;
    c.lineWidth = 1.25;
    c.stroke();
    c.restore();
  }

  // ------------------------------------------------------------ controller

  return {
    update(s) {
      if (!doc) return;
      if (!ourPreview(s) && playhead !== null) { playhead = null; schedule(); }
      renderBar();
    },
    tick(posMs) {
      if (!doc || !ourPreview(app.status)) return;
      const from = previews.get(id) || 0;
      const t = from + (posMs * TICK_HZ) / 1000 - (from > 0 ? PREVIEW_LEAD : 0);
      if (t < from) { playhead = null; return; }
      playhead = t;
      const x = xOf(t);
      if (x > cssW - 40 || x < GUTTER) { view.t0 = t - ((cssW - GUTTER) * 0.1) / view.ppt; clampView(); }
      schedule();
    },
    unmount() {
      alive = false;
      if (raf) cancelAnimationFrame(raf);
      document.body.classList.remove('wide-screen');
      document.removeEventListener('keydown', onKey);
      window.removeEventListener('beforeunload', beforeUnload);
      window.removeEventListener('resize', onResize);
      scheme.removeEventListener('change', onScheme);
      ro.disconnect();
      if (doc && dirty()) {
        const index = new Map(doc.notes.map((n, i) => [n, i]));
        drafts.set(id, {
          sha: piece.kb_sha256, doc, orig, undo, redo, ver, savedVer, nextVer, cursor, view: { ...view },
          sel: [...sel].map((n) => index.get(n)).filter((i) => i !== undefined),
        });
        toast('Your changes are kept, unsaved: open the editor again to go on.');
      }
    },
  };
}
