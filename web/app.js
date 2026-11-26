// Piano: the web UI of sepianod.
//
// A vanilla ES module with no build step. The API contract is
// api/openapi.yaml (base /api/v1). Live state comes from one EventSource on
// /api/v1/events; every screen renders from the latest `status` event.
//
// Everything shown on screen is built with DOM calls and text nodes, never
// innerHTML: titles and console lines come from files and from the piano.
//
// Layout of this file:
//   helpers      DOM builder, icons, formatting
//   api          fetch wrapper, commands, uploads
//   app state    the latest status, settings, library cache, recording tracker
//   events       the EventSource and what each event does
//   chrome       header badge, banners, mini player, toasts, confirm dialog
//   ticker       smooth position between status events
//   router       hash routes -> screen controllers
//   screens      nowPlaying, library, albums, playlists, album, playlist,
//                piece, upload, record, status
//   boot

import { editorScreen, editHref } from './editor.js';

const API = '/api/v1';
const MINUS = '−';
const CONSOLE_MAX = 500;
const SAVE_WINDOW_MS = 120000; // a `library` event this soon after `saving` is the new recording
const UPLOAD_EXT = /\.(kb|lp|sp|zip|mid|midi)$/i;

// ===================================================================== helpers

const PROPS = new Set(['value', 'checked', 'disabled', 'hidden', 'selected', 'multiple', 'required']);

// h('a', { href, class, onclick }, ...children) builds an element. Strings
// become text nodes, so nothing passed here is ever parsed as HTML.
function h(tag, props, ...children) {
  const el = document.createElement(tag);
  if (props) {
    for (const [key, value] of Object.entries(props)) {
      if (value == null || value === false) continue;
      if (key === 'class') el.className = value;
      else if (key.startsWith('on') && typeof value === 'function') el.addEventListener(key.slice(2), value);
      else if (PROPS.has(key)) el[key] = value;
      else el.setAttribute(key, value === true ? '' : String(value));
    }
  }
  return fill(el, ...children);
}

// fill(el, ...children) replaces el's children. Arrays are flattened,
// null/false/'' are skipped (so `cond && node` works), and strings are
// appended as text nodes, never parsed as HTML.
function fill(el, ...children) {
  el.replaceChildren(...children.flat(Infinity).filter((c) => c != null && c !== false && c !== ''));
  return el;
}

function setText(el, text) {
  const t = text == null ? '' : String(text);
  if (el.textContent !== t) el.textContent = t;
}

const SVG_NS = 'http://www.w3.org/2000/svg';
const ICONS = {
  play: { fill: ['M8 5.2v13.6c0 .8.9 1.3 1.6.9l10.6-6.8c.6-.4.6-1.4 0-1.8L9.6 4.3C8.9 3.9 8 4.4 8 5.2z'] },
  pause: { fill: ['M7 5h3.5v14H7zM13.5 5H17v14h-3.5z'] },
  stop: { fill: ['M8 6h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2z'] },
  restart: {
    stroke: ['M6 5v14'],
    fill: ['M18 6.2v11.6c0 .8-.9 1.3-1.6.8L8.6 13c-.6-.4-.6-1.3 0-1.7l7.8-5.6c.7-.5 1.6 0 1.6.8z'],
  },
  // Two triangles, so they read differently from `restart`'s single one.
  previous: { stroke: ['M5.5 6v12'], fill: ['M13.5 6.5v11L7.5 12zM19.5 6.5v11L13.5 12z'] },
  next: { stroke: ['M18.5 6v12'], fill: ['M10.5 6.5v11l6-5.5zM4.5 6.5v11l6-5.5z'] },
  minus: { stroke: ['M5 12h14'] },
  'zoom-in': { stroke: ['M10.5 4a6.5 6.5 0 1 0 0 13 6.5 6.5 0 0 0 0-13z', 'M15.3 15.3 20 20', 'M10.5 7.5v6M7.5 10.5h6'] },
  'zoom-out': { stroke: ['M10.5 4a6.5 6.5 0 1 0 0 13 6.5 6.5 0 0 0 0-13z', 'M15.3 15.3 20 20', 'M7.5 10.5h6'] },
  taller: { stroke: ['M12 3v18', 'M8 7l4-4 4 4', 'M8 17l4 4 4-4'] },
  shorter: { stroke: ['M12 3v6M12 15v6', 'M8 5l4 4 4-4', 'M8 19l4-4 4 4', 'M5 12h14'] },
  plus: { stroke: ['M12 5v14M5 12h14'] },
  now: { stroke: ['M9 18V5l11-2v13', 'M6 15a3 3 0 1 0 0 6 3 3 0 0 0 0-6zM17 13a3 3 0 1 0 0 6 3 3 0 0 0 0-6z'] },
  library: { stroke: ['M4 6h16M4 12h16M4 18h10'] },
  record: {
    stroke: ['M12 4a8 8 0 1 0 0 16 8 8 0 0 0 0-16z'],
    fill: ['M12 8.5a3.5 3.5 0 1 0 0 7 3.5 3.5 0 0 0 0-7z'],
  },
  dot: { fill: ['M12 5a7 7 0 1 0 0 14 7 7 0 0 0 0-14z'] },
  status: { stroke: ['M3 12h4l3-7 4 14 3-7h4'] },
  piano: {
    stroke: ['M3.5 5h17v14h-17z', 'M7.75 13v6M12 13v6M16.25 13v6'],
    fill: ['M6.6 5h2.3v8H6.6zM10.85 5h2.3v8h-2.3zM15.1 5h2.3v8h-2.3z'],
  },
  upload: { stroke: ['M12 15V4M7.5 8.5 12 4l4.5 4.5M5 20h14'] },
  download: { stroke: ['M12 4v11M7.5 10.5 12 15l4.5-4.5M5 20h14'] },
  edit: { stroke: ['M4 20h4L19 9a2.8 2.8 0 0 0-4-4L4 16v4z'] },
  trash: { stroke: ['M4 7h16M9 7V4.5h6V7M6.5 7l1 13h9l1-13M10 11v5.5M14 11v5.5'] },
  undo: { stroke: ['M9 14L4 9l5-5M4 9h10.5a5.5 5.5 0 0 1 0 11H11'] },
  redo: { stroke: ['M15 14l5-5-5-5M20 9H9.5a5.5 5.5 0 0 0 0 11H13'] },
  search: { stroke: ['M11 4a7 7 0 1 0 0 14 7 7 0 0 0 0-14zM20 20l-4-4'] },
  back: { stroke: ['M15 5l-7 7 7 7'] },
  forward: { stroke: ['M9 5l7 7-7 7'] },
  close: { stroke: ['M6 6l12 12M18 6 6 18'] },
  check: { stroke: ['M5 12.5l4.5 4.5L19 7'] },
  refresh: { stroke: ['M20 11a8 8 0 0 0-14.5-4.5L4 8M4 4v4h4M4 13a8 8 0 0 0 14.5 4.5L20 16M20 20v-4h-4'] },
  star: { fill: ['M12 3.2l2.7 5.6 6.1.8-4.5 4.2 1.1 6.1L12 17l-5.4 2.9 1.1-6.1-4.5-4.2 6.1-.8z'] },
  'star-outline': { stroke: ['M12 3.2l2.7 5.6 6.1.8-4.5 4.2 1.1 6.1L12 17l-5.4 2.9 1.1-6.1-4.5-4.2 6.1-.8z'] },
};

function icon(name, cls = 'icon') {
  const spec = ICONS[name] || {};
  const svg = document.createElementNS(SVG_NS, 'svg');
  svg.setAttribute('viewBox', '0 0 24 24');
  svg.setAttribute('class', cls);
  svg.setAttribute('aria-hidden', 'true');
  svg.setAttribute('focusable', 'false');
  for (const d of spec.fill || []) {
    const p = document.createElementNS(SVG_NS, 'path');
    p.setAttribute('d', d);
    p.setAttribute('fill', 'currentColor');
    svg.append(p);
  }
  for (const d of spec.stroke || []) {
    const p = document.createElementNS(SVG_NS, 'path');
    p.setAttribute('d', d);
    p.setAttribute('fill', 'none');
    p.setAttribute('stroke', 'currentColor');
    p.setAttribute('stroke-width', '2');
    p.setAttribute('stroke-linecap', 'round');
    p.setAttribute('stroke-linejoin', 'round');
    svg.append(p);
  }
  return svg;
}

function fmtTime(ms) {
  if (!Number.isFinite(ms) || ms < 0) ms = 0;
  const total = Math.floor(ms / 1000);
  const hours = Math.floor(total / 3600);
  const mins = Math.floor((total % 3600) / 60);
  const secs = String(total % 60).padStart(2, '0');
  return hours ? `${hours}:${String(mins).padStart(2, '0')}:${secs}` : `${mins}:${secs}`;
}

function levelText(n) {
  if (!Number.isInteger(n)) return '—';
  if (n === 0) return 'As recorded';
  return n < 0 ? MINUS + Math.abs(n) : '+' + n;
}

function fmtBytes(n) {
  if (!Number.isFinite(n)) return '';
  if (n < 1024) return `${n} B`;
  const units = ['KB', 'MB', 'GB'];
  let v = n / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
  return `${v.toFixed(v < 10 ? 1 : 0)} ${units[i]}`;
}

function fmtDateTime(iso) {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return String(iso);
  return d.toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' });
}

function fmtRecorded(s) {
  const ym = /^(\d{4})-(\d{2})$/.exec(s || '');
  if (ym) {
    const d = new Date(Number(ym[1]), Number(ym[2]) - 1, 1);
    return Number.isNaN(d.getTime()) ? s : d.toLocaleDateString(undefined, { year: 'numeric', month: 'long' });
  }
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(s || '');
  if (!m) return s || '';
  const d = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]));
  if (Number.isNaN(d.getTime())) return s;
  return d.toLocaleDateString(undefined, { year: 'numeric', month: 'long', day: 'numeric' });
}

function instrumentLabel(s) { return s ? String(s).replace(/_/g, ' ') : ''; }

const ORIGINS = {
  import: 'Imported',
  upload: 'Uploaded',
  midi: 'Converted from MIDI',
  recorded: 'Recorded on this piano',
  edited: 'Edited copy',
};
function originText(p) {
  let t = ORIGINS[p.origin] || p.origin || '';
  if (p.has_midi_source) t += t ? ' (original MIDI kept)' : 'Original MIDI kept';
  return t;
}

const collator = new Intl.Collator(undefined, { sensitivity: 'base', numeric: true });
function cmpText(a, b) { return collator.compare(a || '', b || ''); }

// Composer, then title. Pieces without a composer go last.
function cmpPieces(a, b) {
  const ca = a.composer || '';
  const cb = b.composer || '';
  if (!ca !== !cb) return ca ? -1 : 1;
  return cmpText(ca, cb) || cmpText(a.title, b.title) || cmpText(a.performer, b.performer);
}

function uniqSorted(values) { return [...new Set(values.filter(Boolean))].sort(cmpText); }

// Lower case without accents, so a typed "dvorak" finds "Dvořák".
function fold(s) {
  return String(s || '').normalize('NFD').replace(/[̀-ͯ]/g, '').toLowerCase();
}

function num0(n) { return Number.isFinite(n) ? n : 0; }

function byline(p) { return [p.composer, p.performer].filter(Boolean).join(' · '); }
function tracksText(n) { return Number.isInteger(n) ? (n === 1 ? '1 track' : `${n} tracks`) : ''; }
function pieceHref(id) { return '#/piece/' + encodeURIComponent(id); }
function pieceKeysHref(id) { return '#/piece/' + encodeURIComponent(id) + '/keys'; }
function piecePath(id) { return '/pieces/' + encodeURIComponent(id); }
function albumHref(name) { return '#/album/' + encodeURIComponent(name); }
function albumPath(name) { return '/albums/' + encodeURIComponent(name); }
function playlistHref(id) { return '#/playlist/' + encodeURIComponent(id); }
function playlistPath(id) { return '/playlists/' + encodeURIComponent(id); }

// The page of the album or play list being played; '' for a plain list.
function collectionHref(c) {
  if (!c || !c.id) return '';
  if (c.kind === 'album') return albumHref(c.id);
  if (c.kind === 'playlist') return playlistHref(c.id);
  return '';
}

// Renderings of one performance (same perf_key), best first. The owner's
// piano is a Steinway and the other renderings were tuned for other
// instruments, so: the preferred instrument, then its variants (e.g.
// "Steinway_SED133291_2011NovTemp"), then any Steinway, then renderings with
// no instrument, then the rest. Within a rank the given order is kept.
const DEFAULT_INSTRUMENT = 'Steinway_SED133291';

function preferredInstrument() {
  return (app.settings && app.settings.preferred_instrument) || DEFAULT_INSTRUMENT;
}

function renderingRank(instrument, pref) {
  const inst = instrument || '';
  if (pref && inst === pref) return 0;
  if (pref && inst.startsWith(pref)) return 1;
  if (inst.startsWith('Steinway')) return 2;
  if (!inst) return 3;
  return 4;
}

// A starred rendering is the owner's choice, so it comes first.
function rankRenderings(pieces) {
  const pref = preferredInstrument();
  return pieces
    .map((p, i) => ({ p, i, rank: renderingRank(p.instrument, pref) }))
    .sort((a, b) => (b.p.favorite ? 1 : 0) - (a.p.favorite ? 1 : 0) || a.rank - b.rank || a.i - b.i)
    .map((x) => x.p);
}

function bestRendering(pieces) { return rankRenderings(pieces)[0]; }

// ----------------------------------------------------------------- favorites

// "Favorites first" is on unless switched off on this device.
function favFirstOn() {
  try { return localStorage.getItem('sepiano.favFirst') !== '0'; } catch { return true; }
}
function setFavFirst(on) {
  try { localStorage.setItem('sepiano.favFirst', on ? '1' : '0'); } catch { /* private browsing */ }
}

// How good a piece's data is for this Steinway, best first: SE files
// rendered for a Steinway (Zenph), other SE files, then MIDI sources by grade.
const GRADE_ORDER = { native: 1, 'se-midi': 2, performance: 3, roll: 4, transcribed: 5, sequenced: 6 };
function qualityTier(p) {
  const g = p.grade || 'native';
  if (g === 'native') return (p.instrument || '').toLowerCase().startsWith('steinway') ? 0 : 1;
  return GRADE_ORDER[g] ?? 3;
}
function cmpBest(a, b) { return qualityTier(a) - qualityTier(b) || cmpPieces(a, b); }

// withFavFirst puts favorites first (when "Favorites first" is on), then cmp.
function withFavFirst(cmp, fav = (x) => x.favorite) {
  const on = favFirstOn();
  return (a, b) => (on ? (fav(b) ? 1 : 0) - (fav(a) ? 1 : 0) : 0) || cmp(a, b);
}

// favFirstChip is the "Favorites first" toggle; onchange re-sorts.
function favFirstChip(onchange) {
  const b = h('button', { class: 'chip chip-toggle', type: 'button', title: 'List favorites before everything else',
    onclick: () => { setFavFirst(!favFirstOn()); sync(); onchange(); } }, icon('star', 'icon chip-icon'), 'Favorites first');
  function sync() {
    b.classList.toggle('active', favFirstOn());
    b.setAttribute('aria-pressed', String(favFirstOn()));
  }
  sync();
  return b;
}

// starButton stars or un-stars a piece, album or play list (`kind`, `id`);
// item.favorite is kept in step. `large` makes a page button with words.
function starButton(kind, id, item, label, opts = {}) {
  const btn = h('button', { class: opts.large ? 'btn btn-large star-btn' : 'row-star', type: 'button', onclick: toggle });
  function sync() {
    const on = !!item.favorite;
    btn.classList.toggle('on', on);
    btn.setAttribute('aria-pressed', String(on));
    btn.setAttribute('aria-label', on ? `Remove ${label} from favorites` : `Add ${label} to favorites`);
    btn.title = on ? 'A favorite (tap to remove)' : 'Add to favorites';
    fill(btn, icon(on ? 'star' : 'star-outline'), opts.large ? (on ? 'Favorite' : 'Add to favorites') : null);
  }
  async function toggle(e) {
    e.preventDefault();
    e.stopPropagation();
    const on = !item.favorite;
    item.favorite = on;
    sync();
    try {
      await api('POST', '/favorites', { kind, id, favorite: on });
      if (kind === 'piece' && app.pieces) {
        const p = app.pieces.find((x) => x.id === id);
        if (p) p.favorite = on;
      }
      if (opts.onchange) opts.onchange(on);
    } catch (err) {
      item.favorite = !on;
      sync();
      toast(err.message, 'error');
    }
  }
  sync();
  return btn;
}

function debounce(fn, ms) {
  let timer = 0;
  return (...args) => {
    clearTimeout(timer);
    timer = setTimeout(() => fn(...args), ms);
  };
}

// rows: [label, value, class]. A value is text, or a node (e.g. a link).
function metaList(rows) {
  return h('dl', { class: 'meta' },
    rows
      .filter(([, value]) => value != null && value !== '' && value !== false)
      .map(([label, value, cls]) => h('div', { class: 'meta-row' },
        h('dt', null, label),
        h('dd', { class: cls || null }, value instanceof Node ? value : String(value)))));
}

// ========================================================================= api

class ApiError extends Error {
  constructor(status, message) {
    super(message);
    this.status = status;
  }
}

// api('GET', '/pieces') -> parsed JSON (null for 204). Non-2xx throws an
// ApiError carrying the server's {"error": "..."} text.
async function api(method, path, body, { contentType } = {}) {
  const init = { method, headers: { Accept: 'application/json' }, cache: 'no-store' };
  if (body !== undefined) {
    if (contentType) {
      init.headers['Content-Type'] = contentType;
      init.body = body;
    } else {
      init.headers['Content-Type'] = 'application/json';
      init.body = JSON.stringify(body);
    }
  }
  let res;
  try {
    res = await fetch(API + path, init);
  } catch {
    throw new ApiError(0, 'Cannot reach the piano service.');
  }
  if (res.status === 204) return null;
  const text = await res.text().catch(() => '');
  let data = null;
  if (text) {
    try { data = JSON.parse(text); } catch { /* not JSON */ }
  }
  if (!res.ok) {
    const msg = (data && typeof data.error === 'string' && data.error)
      || text.trim().slice(0, 200)
      || `Request failed (${res.status}).`;
    throw new ApiError(res.status, msg);
  }
  return data;
}

function isStatus(x) { return !!x && typeof x === 'object' && typeof x.state === 'string'; }

// A deck or recorder command (POST). Errors become a toast. The reply's Status
// is only rendered when it is newer than what the event stream already gave
// us; otherwise we wait for the `status` event.
async function command(path, body) {
  try {
    const reply = await api('POST', path, body);
    if (isStatus(reply)) applyReplyStatus(reply);
    return reply ?? true;
  } catch (err) {
    toast(err.message, 'error');
    return null;
  }
}

function applyReplyStatus(s) {
  const cur = app.status;
  if (!cur || Date.parse(s.since) > Date.parse(cur.since)) applyStatus(s);
}

// body: {piece}, {album} or {playlist}, with an optional 0-based `start`.
// Shows Now Playing once the service has taken it. While something is
// playing it asks first: play it now, next, or at the end of Up next (a
// track chosen inside an album or play list, `start`, plays at once).
async function startPlay(body, btn) {
  const s = app.status;
  const busyNow = s && ['playing', 'paused', 'loading', 'stopping'].includes(s.state);
  if (busyNow && !Number.isInteger(body.start)) {
    const what = await pickDialog({
      title: 'The piano is playing',
      items: [
        { label: 'Play now', sub: 'Stops what is playing', value: 'now' },
        { label: 'Play next', sub: 'When the piece playing ends', value: 'next' },
        { label: 'Add to Up next', sub: upNextText(), value: 'end' },
      ],
    });
    if (!what) return;
    if (what !== 'now') {
      if (btn) btn.disabled = true;
      try {
        const r = await api('POST', '/queue' + (what === 'next' ? '?next=1' : ''), body);
        if (r && Array.isArray(r.items)) setUpNext(r.items);
        toast(what === 'next' ? 'Plays next.' : 'Added to Up next.');
      } catch (err) {
        toast(err.message, 'error');
      } finally {
        if (btn) btn.disabled = false;
      }
      return;
    }
  }
  if (btn) btn.disabled = true;
  const ok = await command('/deck/play', body);
  if (btn) btn.disabled = false;
  if (ok) location.hash = '#/';
}

// Up next: what plays when the current piece ends (api/queue.go).
function setUpNext(items) {
  app.upNext = Array.isArray(items) ? items : [];
  if (current.onQueue) current.onQueue();
}
function loadUpNext() {
  api('GET', '/queue').then((r) => setUpNext(r && r.items), () => {});
}
function upNextText() {
  const n = (app.upNext || []).length;
  return n ? `After the ${n === 1 ? 'one' : n} already waiting` : 'When the piece playing ends';
}

function playPiece(id, btn) { return startPlay({ piece: id }, btn); }

// Multipart upload with progress (fetch cannot report upload progress).
function postPieces(files, title, onProgress) {
  return new Promise((resolve, reject) => {
    const form = new FormData();
    if (title) form.append('title', title); // before the files, for streaming parsers
    for (const f of files) form.append('file', f, f.name);
    const xhr = new XMLHttpRequest();
    xhr.open('POST', API + '/pieces');
    xhr.setRequestHeader('Accept', 'application/json');
    xhr.upload.onprogress = (e) => { if (e.lengthComputable && e.total) onProgress(e.loaded / e.total); };
    xhr.onload = () => {
      let data = null;
      try { data = JSON.parse(xhr.responseText); } catch { /* not JSON */ }
      if (xhr.status >= 200 && xhr.status < 300) resolve(data || {});
      else reject(new ApiError(xhr.status, (data && data.error) || `Upload failed (${xhr.status}).`));
    };
    xhr.onerror = () => reject(new ApiError(0, 'Cannot reach the piano service.'));
    xhr.send(form);
  });
}

async function saveSettings(patch, quiet = false) {
  try {
    const res = await api('PUT', '/settings', patch);
    app.settings = { ...(app.settings || {}), ...(res || patch) };
    if (!quiet) toast('Settings saved.');
    if (current.onSettings) current.onSettings();
    return true;
  } catch (err) {
    toast(err.message, 'error');
    return false;
  }
}

async function loadSettings() {
  try {
    app.settings = (await api('GET', '/settings')) || {};
    if (current.onSettings) current.onSettings();
  } catch {
    // Tried again whenever the event stream reconnects.
  }
}

// The whole library, unfiltered, cached until the next `library` event.
let piecesPromise = null;
function getAllPieces() {
  if (app.pieces && !app.piecesStale) return Promise.resolve(app.pieces);
  if (!piecesPromise) {
    const gen = app.piecesGen;
    piecesPromise = api('GET', '/pieces')
      .then((res) => {
        const list = (res && res.pieces) || [];
        app.pieces = list;
        if (gen === app.piecesGen) app.piecesStale = false;
        return list;
      })
      .finally(() => { piecesPromise = null; });
  }
  return piecesPromise;
}

// =================================================================== app state

const app = {
  status: null, // latest Status
  statusAt: 0, // performance.now() when it arrived
  conn: 'init', // EventSource: init | connecting | open
  everOpen: false,
  settings: null, // Settings
  pieces: null, // unfiltered library list
  piecesStale: true,
  piecesGen: 0,
  consoleLines: [],
  upNext: [], // what plays when the current piece ends
  rec: { title: '', awaitingSince: 0, saved: null, thenKeys: false }, // saved: {id, title}; thenKeys: open the Key check once saved
};

const PLAY_STATES = new Set(['playing', 'paused', 'stopping']);
const RECORD_STATES = new Set(['armed', 'recording', 'saving']);

// `loading` is shared by play and arm. Play sets `piece`; the recorder has none.
function isRecorderLoading(s) { return s.state === 'loading' && !s.piece; }
function isRecordSession(s) { return !!s && (RECORD_STATES.has(s.state) || isRecorderLoading(s)); }
function isPlaySession(s) { return !!s && (PLAY_STATES.has(s.state) || (s.state === 'loading' && !!s.piece)); }

const STATE_WORDS = {
  offline: 'Piano offline',
  connecting: 'Connecting…',
  idle: 'Ready',
  loading: 'Loading…',
  playing: 'Playing',
  paused: 'Paused',
  stopping: 'Stopping…',
  armed: 'Ready to record',
  recording: 'Recording',
  saving: 'Saving…',
  maintenance: 'Reading the piano’s settings…',
  fault: 'Something went wrong',
};
function stateWords(s) {
  if (!s) return 'Connecting…';
  if (s.state === 'maintenance' && s.program && !s.program.includes('RETRIEVER')) return 'Running a tool on the piano';
  return STATE_WORDS[s.state] || s.state;
}

// An album or play list plays as one performance; `tracks` says where each
// track lies in it. The main progress bar shows the current track's stretch
// of it, or the whole performance for a single piece.
// A track started from a point on the bar (from_ms) still shows times in its
// whole piece: the span reaches back to the piece's beginning.
function trackSpan(s) {
  const tracks = s && Array.isArray(s.tracks) ? s.tracks : null;
  const t = tracks && (s.collection ? tracks[s.track_index] : tracks.length === 1 ? tracks[0] : null);
  if (t) {
    const from = Math.max(0, num0(t.from_ms));
    return { start: num0(t.start_ms) - from, dur: Math.max(0, num0(t.duration_ms)) + from };
  }
  return { start: 0, dur: s && s.duration_ms > 0 ? s.duration_ms : 0 };
}

// Position within a span. Between status events the extrapolated position
// can run past the end of a track; it stays there until the server moves on.
function spanPosition(span, pos) {
  const p = Math.max(0, pos - span.start);
  return span.dur ? Math.min(p, span.dur) : p;
}

// "Track n of m" counts in the whole album or play list (s.collection set).
function trackOf(s) {
  const c = s.collection;
  return { n: num0(c.start) + num0(s.track_index) + 1, m: num0(c.count) };
}

// Index in the whole album or play list (kind, id) of the track playing now,
// or -1 when that collection is not playing.
function playingIndexIn(kind, id) {
  const s = app.status;
  const c = isPlaySession(s) ? s.collection : null;
  if (!c || !id || c.kind !== kind || c.id !== id) return -1;
  return num0(c.start) + num0(s.track_index);
}

function applyStatus(s) {
  const prev = app.status;
  app.status = s;
  app.statusAt = performance.now();
  if (app.settings && prev && s.target_level !== prev.target_level && Number.isInteger(s.target_level)) {
    app.settings.level = s.target_level; // Settings.level is the same value
  }
  trackRecording(prev, s);
  renderChrome();
  if (current.update) current.update(s, prev);
  ensureTicker();
}

// Follows the recorder so the Record screen can link to the saved piece:
// after `saving`, the next `library` event with an id is the new recording.
function trackRecording(prev, s) {
  const r = app.rec;
  const was = prev ? prev.state : '';
  if (s.state === 'armed' && was !== 'armed') {
    r.saved = null;
    r.awaitingSince = 0;
    r.title = s.recording_title || '';
  }
  if (s.recording_title && RECORD_STATES.has(s.state)) r.title = s.recording_title;
  if (s.state === 'saving' && was !== 'saving') r.awaitingSince = Date.now();
  if (s.state === 'idle' && was === 'recording') r.awaitingSince = 0; // discarded
}

function recordingSaved(id) {
  const r = app.rec;
  r.awaitingSince = 0;
  r.saved = { id, title: r.title || 'Your recording' };
  if (r.thenKeys) { r.thenKeys = false; location.hash = pieceKeysHref(id); } // a sensor test: straight to its Key check
  api('GET', piecePath(id))
    .then((p) => {
      if (p && r.saved && r.saved.id === id) {
        r.saved = { id, title: p.title || r.saved.title };
        if (current.update && app.status) current.update(app.status, app.status);
      }
    })
    .catch(() => {});
}

// ====================================================================== events

let es = null;
let esTimer = 0;
let esFailures = 0;

function connectEvents() {
  clearTimeout(esTimer);
  if (es) {
    es.onopen = null;
    es.onerror = null;
    es.close();
  }
  setConn('connecting');
  const source = new EventSource(API + '/events');
  es = source;

  source.onopen = () => {
    if (es !== source) return;
    esFailures = 0;
    const again = app.everOpen;
    app.everOpen = true;
    setConn('open');
    loadUpNext();
    if (again) { // things may have changed while we were away
      libraryChanged('');
      loadSettings();
    }
  };
  source.onerror = () => {
    if (es !== source) return;
    setConn('connecting');
    if (source.readyState === EventSource.CLOSED) { // the browser gave up; retry ourselves
      const delay = Math.min(30000, 1000 * 2 ** Math.min(esFailures, 5));
      esFailures++;
      esTimer = setTimeout(connectEvents, delay);
    }
  };

  source.addEventListener('status', (e) => {
    const s = parseData(e);
    if (isStatus(s)) applyStatus(s);
  });
  source.addEventListener('console', (e) => {
    const d = parseData(e);
    if (d && typeof d.line === 'string') consoleLine(d.line);
  });
  source.addEventListener('library', (e) => {
    const d = parseData(e) || {};
    libraryChanged(typeof d.id === 'string' ? d.id : '');
  });
  source.addEventListener('queue', (e) => {
    const d = parseData(e);
    if (d) setUpNext(d.items);
  });
  source.addEventListener('notice', (e) => {
    const d = parseData(e);
    if (d && d.text) toast(d.text, d.level);
  });
}

function parseData(e) {
  try { return JSON.parse(e.data); } catch { return null; }
}

function consoleLine(line) {
  app.consoleLines.push(line);
  if (app.consoleLines.length > CONSOLE_MAX) app.consoleLines.splice(0, app.consoleLines.length - CONSOLE_MAX);
  if (current.onConsole) current.onConsole(line);
}

function libraryChanged(id) {
  app.piecesStale = true;
  app.piecesGen++;
  if (id && app.rec.awaitingSince && Date.now() - app.rec.awaitingSince < SAVE_WINDOW_MS) recordingSaved(id);
  if (current.onLibrary) current.onLibrary(id);
}

// ====================================================================== chrome

const els = {};
let connTimer = 0;
let connBannerDue = false;

function setConn(c) {
  if (c === app.conn) return;
  app.conn = c;
  clearTimeout(connTimer);
  if (c === 'open') {
    connBannerDue = false;
  } else {
    // Only complain if the stream stays down for a moment.
    connTimer = setTimeout(() => { connBannerDue = true; renderBanner(); }, 1500);
  }
  renderBanner();
  ensureTicker();
}

function renderChrome() {
  const s = app.status;
  els.sim.hidden = !(s && s.simulated);
  renderBanner();
  renderMini();
}

function renderBanner() {
  const s = app.status;
  let text = '';
  let cls = '';
  if (app.conn !== 'open') {
    if (connBannerDue) {
      text = app.everOpen ? 'Reconnecting…' : 'Connecting…';
      cls = 'banner-conn';
    }
  } else if (s && (!s.online || s.state === 'offline')) {
    text = s.state === 'connecting' ? 'Piano offline — connecting…' : 'Piano offline — is it switched on?';
    cls = 'banner-offline';
  }
  els.banner.hidden = !text;
  els.banner.className = 'banner ' + cls;
  setText(els.banner, text);
}

// The mini player shows what the piano is doing while you browse elsewhere.
const mini = { mode: '', iconName: '', busy: false };

function buildMini() {
  mini.bar = h('span', { class: 'mini-bar' });
  mini.title = h('span', { class: 'mini-title' });
  mini.sub = h('span', { class: 'mini-sub' });
  mini.link = h('a', { class: 'mini-link', href: '#/' }, mini.title, mini.sub);
  mini.btn = h('button', { class: 'mini-btn', type: 'button', onclick: miniToggle });
  els.mini.append(mini.bar, mini.link, mini.btn);
}

function renderMini() {
  const s = app.status;
  const tab = currentRoute ? currentRoute.tab : '';
  let mode = '';
  if (isPlaySession(s) && tab !== 'now') mode = 'play';
  else if (isRecordSession(s) && tab !== 'record') mode = 'rec';
  mini.mode = mode;
  els.mini.hidden = !mode;
  document.body.classList.toggle('has-mini', !!mode);
  els.mini.classList.toggle('recording', mode === 'rec' && s.state === 'recording');
  if (!mode) return;

  if (mode === 'play') {
    const paused = s.state === 'paused';
    const c = s.collection;
    mini.link.href = '#/';
    setText(mini.title, (s.piece && s.piece.title) || 'Untitled');
    setText(mini.sub, [stateWords(s), c && (c.title || c.id)].filter(Boolean).join(' · '));
    mini.btn.hidden = false;
    const name = paused ? 'play' : 'pause';
    if (name !== mini.iconName) { fill(mini.btn, icon(name)); mini.iconName = name; }
    mini.btn.setAttribute('aria-label', paused ? 'Resume' : 'Pause');
    mini.btn.disabled = mini.busy || !(s.state === 'playing' || paused);
  } else {
    mini.link.href = '#/record';
    setText(mini.title, s.recording_title || app.rec.title || 'Recorder');
    setText(mini.sub, stateWords(s));
    mini.btn.hidden = true;
  }
  miniTick(currentPosition());
}

function miniTick(pos) {
  if (!mini.mode) return;
  const s = app.status;
  if (mini.mode === 'play') {
    const span = trackSpan(s);
    mini.bar.style.transform = `scaleX(${span.dur ? Math.min(1, spanPosition(span, pos) / span.dur) : 0})`;
  } else {
    mini.bar.style.transform = 'scaleX(0)';
    if (s.state === 'recording') setText(mini.sub, `Recording · ${fmtTime(pos)}`);
  }
}

async function miniToggle() {
  const s = app.status;
  if (!s || mini.busy) return;
  const path = s.state === 'playing' ? '/deck/pause' : s.state === 'paused' ? '/deck/resume' : '';
  if (!path) return;
  mini.busy = true;
  renderMini();
  await command(path);
  mini.busy = false;
  renderMini();
}

function toast(text, level = 'info') {
  if (!text) return;
  const lvl = level === 'error' || level === 'warn' ? level : 'info';
  const el = h('div', { class: `toast toast-${lvl}`, role: lvl === 'error' ? 'alert' : 'status' }, String(text));
  let gone = false;
  const dismiss = () => {
    if (gone) return;
    gone = true;
    el.classList.add('leaving');
    setTimeout(() => el.remove(), 220);
  };
  el.addEventListener('click', dismiss);
  els.toasts.append(el);
  while (els.toasts.children.length > 3) els.toasts.firstElementChild.remove();
  setTimeout(dismiss, lvl === 'error' ? 8000 : lvl === 'warn' ? 6000 : 4000);
}

// confirmDialog({title, text, ok, danger}) -> Promise<boolean>
// With `phrase`, the OK button only works once that word is typed (any
// case), for things that cannot be undone.
function confirmDialog({ title, text, ok = 'OK', danger = false, phrase = '', input = null }) {
  const dlg = els.confirm;
  if (!dlg || typeof dlg.showModal !== 'function') {
    if (phrase) return Promise.resolve((window.prompt(`${title}\n\n${text}\n\nType ${phrase} to confirm.`) || '').trim().toUpperCase() === phrase.toUpperCase());
    if (input) return Promise.resolve((window.prompt(`${title}\n\n${text}`, input.value || '') || '').trim() || null);
    return Promise.resolve(window.confirm(`${title}\n\n${text}`));
  }
  if (dlg.open) return Promise.resolve(input ? null : false);
  setText(els.confirmTitle, title);
  setText(els.confirmText, text);
  setText(els.confirmOk, ok);
  els.confirmOk.className = 'btn ' + (danger ? 'btn-danger-solid' : 'btn-primary');
  const phraseInput = phrase ? h('input', {
    class: 'input confirm-phrase', type: 'text', autocomplete: 'off', autocapitalize: 'characters', spellcheck: 'false',
    'aria-label': `Type ${phrase} to confirm`, placeholder: `Type ${phrase}`,
  }) : null;
  const phraseRow = phrase ? h('label', { class: 'confirm-phrase-row' }, `Type ${phrase} to confirm:`, phraseInput) : null;
  if (phraseRow) els.confirmText.after(phraseRow);
  // With `input` ({ value, placeholder, label }) it asks for a line of text,
  // and resolves to it (trimmed), or to null when cancelled.
  const textInput = input ? h('input', {
    class: 'input confirm-phrase', type: 'text', autocomplete: 'off', value: input.value || '',
    placeholder: input.placeholder || '', 'aria-label': input.label || title,
  }) : null;
  const textRow = input ? h('label', { class: 'confirm-phrase-row' }, input.label || '', textInput) : null;
  if (textRow) els.confirmText.after(textRow);
  const typed = () => (!phrase || phraseInput.value.trim().toUpperCase() === phrase.toUpperCase())
    && (!input || textInput.value.trim() !== '');
  const check = () => { els.confirmOk.disabled = !typed(); };
  check();
  for (const el of [phraseInput, textInput]) if (el) el.addEventListener('input', check);
  if (textInput) textInput.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); els.confirmOk.click(); } });
  return new Promise((resolve) => {
    // Answer straight from the buttons; `close` (fired later, and also by
    // Escape or a backdrop tap) only counts as a cancel.
    const finish = (answer) => {
      els.confirmOk.removeEventListener('click', onOk);
      els.confirmCancel.removeEventListener('click', onCancel);
      dlg.removeEventListener('close', onCancel);
      if (phraseRow) phraseRow.remove();
      if (textRow) textRow.remove();
      els.confirmOk.disabled = false;
      if (dlg.open) dlg.close();
      resolve(input ? (answer ? textInput.value.trim() : null) : answer);
    };
    const onOk = () => { if (typed()) finish(true); };
    const onCancel = () => finish(false);
    els.confirmOk.addEventListener('click', onOk);
    els.confirmCancel.addEventListener('click', onCancel);
    dlg.addEventListener('close', onCancel);
    dlg.showModal();
    (textInput || phraseInput || els.confirmCancel).focus();
    if (textInput) textInput.select();
  });
}

// ================================================================ Play lists
// Choosing a play list to add pieces to (the Library's Select, a piece's
// page), or making a new one with them.

async function addToPlaylist(ids, what) {
  if (!ids.length) return;
  let lists;
  try {
    const r = await api('GET', '/playlists');
    lists = (r && r.playlists) || [];
  } catch (err) {
    toast(err.message, 'error');
    return;
  }
  // Your own lists first, then those imported from folders.
  lists.sort((a, b) => (!!a.source - !!b.source) || cmpText(a.name, b.name));
  const choice = await pickDialog({
    title: 'Add to a play list',
    text: what,
    newLabel: 'New play list…',
    items: lists.map((l) => ({ value: l.id, label: l.name, sub: [tracksText(l.tracks), l.source ? 'from a folder' : 'yours'].join(' · ') })),
  });
  if (choice === null) return;
  try {
    if (choice === '') {
      const name = await confirmDialog({ title: 'New play list', text: what, ok: 'Make it', input: { label: 'Name', placeholder: 'For example: Sunday evening' } });
      if (!name) return;
      const pl = await api('POST', '/playlists', { name, pieces: ids });
      toast(`Made “${pl.name}” with ${tracksText(ids.length)}.`);
    } else {
      const before = lists.find((l) => l.id === choice);
      const pl = await api('POST', playlistPath(choice) + '/pieces', { pieces: ids });
      const added = (pl.pieces || []).length - (before ? before.tracks : 0);
      toast(added > 0 ? `Added ${tracksText(added)} to “${pl.name}”.` : `Already in “${pl.name}”.`);
    }
  } catch (err) {
    toast(err.message, 'error');
  }
}

// pickDialog shows a list to choose from; it resolves to the chosen value,
// '' for the "new" button, or null when cancelled.
function pickDialog({ title, text, items, newLabel }) {
  return new Promise((resolve) => {
    let done = false;
    const finish = (v) => { if (done) return; done = true; if (dlg.open) dlg.close(); dlg.remove(); resolve(v); };
    const list = h('ul', { class: 'pick-list', role: 'list' },
      items.map((it) => h('li', null, h('button', { type: 'button', class: 'pick-item', onclick: () => finish(it.value) },
        h('span', { class: 'pick-label' }, it.label), it.sub && h('span', { class: 'pick-sub' }, it.sub)))));
    const dlg = h('dialog', { class: 'confirm pick', 'aria-label': title },
      h('h2', null, title),
      text && h('p', null, text),
      newLabel && h('button', { type: 'button', class: 'btn btn-primary pick-new', onclick: () => finish('') }, icon('plus'), newLabel),
      items.length ? list : h('p', { class: 'muted small' }, 'No play lists yet.'),
      h('div', { class: 'confirm-actions' }, h('button', { type: 'button', class: 'btn', onclick: () => finish(null) }, 'Cancel')));
    dlg.addEventListener('close', () => finish(null));
    document.body.append(dlg);
    if (typeof dlg.showModal === 'function') dlg.showModal(); else dlg.setAttribute('open', '');
  });
}

// How the level is applied, to compare the two (remembered on the Pi):
// "On the Pi" softens the notes as the Pi sends them (heard a few seconds
// later); "Piano's own" uses the player's level command (heard at once).
// A change while a piece plays applies from the next piece.
function levelMethodToggle() {
  const pi = h('button', { type: 'button', onclick: () => choose('pi') }, 'On the Pi');
  const piano = h('button', { type: 'button', onclick: () => choose('piano') }, 'Piano’s own');
  const note = h('p', { class: 'level-hint level-method-note', hidden: true });
  const el = h('div', { class: 'level-method' },
    h('span', { class: 'level-method-label' }, 'Volume set by'),
    h('div', { class: 'segmented level-method-seg', role: 'group', 'aria-label': 'Volume set by' }, pi, piano),
    note);
  async function choose(m) {
    if (current() === m) return;
    pi.disabled = piano.disabled = true;
    await saveSettings({ level_method: m }, true);
    pi.disabled = piano.disabled = false;
    render();
  }
  function current() { return app.settings && app.settings.level_method === 'piano' ? 'piano' : 'pi'; }
  function render() {
    const m = current();
    pi.setAttribute('aria-pressed', String(m === 'pi'));
    piano.setAttribute('aria-pressed', String(m === 'piano'));
    const s = app.status;
    const pending = !!(s && s.level_method_pending);
    note.hidden = !pending;
    setText(note, pending ? 'Applies when the next piece starts.' : '');
  }
  render();
  return { el, render };
}

// A − readout + control. onStep(delta) does the work; render() keeps the
// buttons honest: + stops at 0, − stops at min_level once it is known.
function levelStepper(onStep, labelText = 'Level') {
  const minus = h('button', { class: 'round-btn', type: 'button', 'aria-label': 'Softer', onclick: () => onStep(-1) }, icon('minus'));
  const plus = h('button', { class: 'round-btn', type: 'button', 'aria-label': 'Louder', onclick: () => onStep(1) }, icon('plus'));
  const readout = h('output', { class: 'level-readout', 'aria-live': 'polite' }, '—');
  const el = h('div', { class: 'level', role: 'group', 'aria-label': labelText },
    minus,
    h('div', { class: 'level-center' }, h('span', { class: 'level-label' }, labelText), readout),
    plus);
  return {
    el,
    render(value, min, busy) {
      const known = Number.isInteger(value);
      setText(readout, levelText(value));
      plus.disabled = !!busy || !known || value >= 0;
      minus.disabled = !!busy || !known || (Number.isInteger(min) && value <= min);
    },
  };
}

// ====================================================================== ticker
// Between status events (about 5 a second) the position is extrapolated so the
// progress bar moves smoothly. Each new status snaps it back to the truth.

let rafId = 0;

function isMoving(s) { return !!s && (s.state === 'playing' || s.state === 'recording'); }

function currentPosition() {
  const s = app.status;
  if (!s) return 0;
  let pos = Number.isFinite(s.position_ms) ? s.position_ms : 0;
  if (isMoving(s) && app.conn === 'open') {
    pos += Math.min(performance.now() - app.statusAt, 3000); // never run far ahead of the piano
  }
  if (s.duration_ms > 0) pos = Math.min(pos, s.duration_ms);
  return Math.max(0, pos);
}

function ensureTicker() {
  if (!rafId && isMoving(app.status) && app.conn === 'open') rafId = requestAnimationFrame(frame);
}

function frame() {
  rafId = 0;
  const pos = currentPosition();
  if (current.tick) current.tick(pos);
  miniTick(pos);
  ensureTicker();
}

// ====================================================================== router

// What the editor (editor.js) uses from this file.
const UI = { h, fill, setText, icon, api, command, toast, noteName, isBlackKey, app, isStatus, applyReplyStatus, pieceHref };

// `back: true` goes to the library section last shown; an object names its own.
const ROUTES = [
  { name: 'now', re: /^\/$/, tab: 'now', title: 'Now Playing', screen: nowPlayingScreen },
  { name: 'library', re: /^\/library$/, tab: 'library', title: 'Library', screen: libraryScreen },
  { name: 'favorites', re: /^\/favorites$/, tab: 'library', title: 'Favorites', screen: (root, params, ctx) => libraryScreen(root, params, ctx, { favorites: true }) },
  { name: 'albums', re: /^\/albums$/, tab: 'library', title: 'Albums', screen: albumsScreen },
  { name: 'playlists', re: /^\/playlists$/, tab: 'library', title: 'Play lists', screen: playlistsScreen },
  { name: 'recordings', re: /^\/recordings$/, tab: 'library', title: 'Recordings', screen: recordingsScreen },
  { name: 'album', re: /^\/album\/([^/]+)$/, tab: 'library', title: 'Album', back: { href: '#/albums', label: 'Albums' }, screen: albumScreen },
  { name: 'playlist', re: /^\/playlist\/([^/]+)$/, tab: 'library', title: 'Play list', back: { href: '#/playlists', label: 'Play lists' }, screen: playlistScreen },
  { name: 'piece', re: /^\/piece\/([^/]+)$/, tab: 'library', title: 'Piece', back: true, screen: pieceScreen },
  { name: 'piece-keys', re: /^\/piece\/([^/]+)\/keys$/, tab: 'library', title: 'Key check', back: true, screen: pieceKeysScreen },
  { name: 'edit', re: /^\/piece\/([^/]+)\/edit$/, tab: 'library', title: 'Editor', back: true, screen: (root, params) => editorScreen(root, params, UI) },
  { name: 'upload', re: /^\/upload$/, tab: 'library', title: 'Upload', back: true, screen: uploadScreen },
  { name: 'record', re: /^\/record$/, tab: 'record', title: 'Record', screen: recordScreen },
  { name: 'corrections', re: /^\/record\/corrections$/, tab: 'record', title: 'Corrections', back: { href: '#/record', label: 'Record' }, screen: correctionsScreen },
  { name: 'sensor-test', re: /^\/record\/test$/, tab: 'record', title: 'Sensor test', back: { href: '#/record', label: 'Record' }, screen: sensorTestScreen },
  { name: 'keys-many', re: /^\/keys\/([^/]+)$/, tab: 'record', title: 'Key check', back: { href: '#/record/test', label: 'Sensor test' }, screen: keysManyScreen },
  { name: 'piano', re: /^\/piano$/, tab: 'piano', title: 'Piano', screen: pianoScreen },
  { name: 'piano-key', re: /^\/piano\/key\/(\d+)$/, tab: 'piano', title: 'Key', back: { href: '#/piano', label: 'Piano' }, screen: pianoKeyScreen },
  { name: 'piano-fits', re: /^\/piano\/fits$/, tab: 'piano', title: 'Fit by collection', back: { href: '#/piano', label: 'Piano' }, screen: fitsScreen },
  { name: 'piano-backups', re: /^\/piano\/backups$/, tab: 'piano', title: 'Backups', back: { href: '#/piano', label: 'Piano' }, screen: pianoBackupsScreen },
  { name: 'piano-tool', re: /^\/piano\/tool\/([a-z0-9]+)(?:\/([^/]+))?$/, tab: 'piano', title: 'Tool', back: { href: '#/piano', label: 'Piano' }, screen: toolScreen },
  { name: 'status', re: /^\/status$/, tab: 'status', title: 'Status', screen: statusScreen },
];

// The segmented control at the top of the library: [route name, hash, label].
const LIB_SECTIONS = [
  ['library', '#/library', 'Pieces'],
  ['favorites', '#/favorites', 'Favorites'],
  ['albums', '#/albums', 'Albums'],
  ['playlists', '#/playlists', 'Play lists'],
  ['recordings', '#/recordings', 'Recordings'],
];

let current = {}; // the mounted screen's controller: update, tick, onLibrary, onConsole, onSettings, onVisible, unmount
let currentRoute = null;
let currentPath = '';
let libSection = '#/library'; // the Library tab returns to the section last shown

// Where the top bar's back link goes. A piece opened from an album or play
// list goes back to it.
function backLink(r, prev, prevPath, params) {
  if (!r.back) return null;
  if (r.name === 'edit') return { href: pieceHref(params[0]), label: 'Piece' };
  if (r.name === 'piece' && prev && (prev.name === 'album' || prev.name === 'playlist')) {
    return { href: '#' + prevPath, label: prev.title };
  }
  if (r.name === 'piece-keys' && prev && prev.name === 'piece') {
    return { href: '#' + prevPath, label: 'Piece' };
  }
  if (r.name === 'piece-keys' && prev && prev.name === 'record') {
    return { href: '#/record', label: 'Record' };
  }
  return r.back === true ? { href: libSection, label: 'Library' } : r.back;
}

function route() {
  const path = location.hash.replace(/^#/, '') || '/';
  let r = null;
  let m = null;
  for (const candidate of ROUTES) {
    m = candidate.re.exec(path);
    if (m) { r = candidate; break; }
  }
  if (!r) { location.replace('#/'); return; }
  let params;
  try {
    params = m.slice(1).map((x) => (x === undefined ? undefined : decodeURIComponent(x)));
  } catch {
    location.replace('#/library');
    return;
  }

  const prev = currentRoute;
  const prevPath = currentPath;
  const from = prev ? prev.name : '';
  if (current.unmount) current.unmount();
  currentRoute = r;
  currentPath = path;
  fill(els.view);
  window.scrollTo(0, 0);
  document.title = r.name === 'now' ? 'Piano' : `${r.title} · Piano`;
  if (LIB_SECTIONS.some(([name]) => name === r.name)) {
    libSection = '#' + path;
    els.libTab.href = libSection;
  }
  for (const a of els.tabs) {
    const on = a.dataset.tab === r.tab;
    a.classList.toggle('active', on);
    if (on) a.setAttribute('aria-current', 'page');
    else a.removeAttribute('aria-current');
  }
  const back = backLink(r, prev, prevPath, params);
  els.back.hidden = !back;
  if (back) {
    els.back.href = back.href;
    setText(els.backLabel, back.label);
  }

  current = r.screen(els.view, params, { from }) || {};
  if (app.status && current.update) current.update(app.status, null);
  renderChrome();
}

// ================================================================= Now Playing

function nowPlayingScreen(root) {
  const busy = new Set();
  let alive = true;
  let lastSec = -1;
  let lastAllSec = -1;
  let mainIconName = '';

  const stateEl = h('p', { class: 'np-state' });
  // Album or play list: its title (a link to its page) and "Track n of m".
  const collLink = h('a');
  const collEl = h('p', { class: 'np-collection', hidden: true }, collLink);
  const trackEl = h('p', { class: 'np-track', hidden: true });
  const titleLink = h('a');
  const titleEl = h('h1', { class: 'np-title' }, titleLink);
  const composerEl = h('p', { class: 'np-composer' });
  const performerEl = h('p', { class: 'np-performer' });
  const fileEl = h('p', { class: 'np-file' }); // the file playing, to check what is what
  const editLink = h('a', { href: '#/' }, 'Open in the editor');
  const editLinkEl = h('p', { class: 'np-edit small', hidden: true }, editLink);
  const voicedEl = h('p', { class: 'np-voiced', hidden: true }, h('a', { class: 'pill pill-warn', href: '#/piano' }, 'Voicing on: trims tried by ear')); // software voicing applied
  const correctedEl = h('p', { class: 'np-voiced', hidden: true }, h('a', { class: 'pill', href: '#/record/corrections' }, 'Recording corrections on')); // a recording made here, corrected
  const fittedEl = h('p', { class: 'np-voiced', hidden: true }, h('a', { class: 'pill', href: '#/piano/fits' }, 'Fitted to this piano')); // its collection's fit
  const hintEl = h('p', { class: 'np-hint' });
  const messageEl = h('p', { class: 'message message-warn', role: 'status' });
  const progFill = h('div', { class: 'progress-fill' });
  // The bar is also a slider: tap or drag to a point and the piece plays
  // from there (the piano takes the player again, a few seconds). Arrow
  // keys choose a point 10 s (Shift: 30 s) at a time; Enter plays it.
  const progress = h('div', { class: 'seek', role: 'slider', tabindex: '0', 'aria-label': 'Position in the piece', 'aria-valuemin': '0',
    title: 'Tap or drag to play from that point' }, h('div', { class: 'progress' }, progFill));
  let seekAt = null; // ms into the piece while a point is being chosen
  const canSeek = () => {
    const s = app.status;
    return !!(s && (s.state === 'playing' || s.state === 'paused') && s.piece && trackSpan(s).dur > 0 && !busy.has('seek'));
  };
  const seekPos = (e) => {
    const r = progress.getBoundingClientRect();
    const f = r.width ? Math.min(1, Math.max(0, (e.clientX - r.left) / r.width)) : 0;
    return Math.round(f * trackSpan(app.status).dur);
  };
  const showSeek = () => {
    const dur = trackSpan(app.status).dur;
    progress.classList.add('seeking');
    progFill.style.transform = `scaleX(${dur ? seekAt / dur : 0})`;
    elapsedEl.textContent = fmtTime(seekAt);
    progress.setAttribute('aria-valuetext', `Play from ${fmtTime(seekAt)}`);
  };
  const endSeek = (play) => {
    const at = seekAt;
    seekAt = null;
    progress.classList.remove('seeking');
    lastSec = -1;
    tick(currentPosition());
    if (play && at !== null && app.status && app.status.piece) {
      toast(`Playing from ${fmtTime(at)}: the piano takes a few seconds.`);
      run('seek', '/deck/seek', { piece: app.status.piece.id, position_ms: at });
    }
  };
  progress.addEventListener('pointerdown', (e) => {
    if (!canSeek()) return;
    progress.setPointerCapture(e.pointerId);
    seekAt = seekPos(e);
    showSeek();
  });
  progress.addEventListener('pointermove', (e) => { if (seekAt !== null) { seekAt = seekPos(e); showSeek(); } });
  progress.addEventListener('pointerup', (e) => { if (seekAt !== null) { seekAt = seekPos(e); endSeek(true); } });
  progress.addEventListener('pointercancel', () => endSeek(false));
  progress.addEventListener('keydown', (e) => {
    if (!canSeek()) return;
    const span = trackSpan(app.status);
    const from = seekAt ?? spanPosition(span, currentPosition());
    const step = e.shiftKey ? 30000 : 10000;
    if (e.key === 'ArrowRight' || e.key === 'ArrowUp') seekAt = Math.min(span.dur - 1000, from + step);
    else if (e.key === 'ArrowLeft' || e.key === 'ArrowDown') seekAt = Math.max(0, from - step);
    else if (e.key === 'Home') seekAt = 0;
    else if (e.key === 'Enter' && seekAt !== null) { e.preventDefault(); endSeek(true); return; }
    else if (e.key === 'Escape' && seekAt !== null) { endSeek(false); return; }
    else return;
    e.preventDefault();
    seekAt = Math.max(0, seekAt);
    showSeek();
  });
  progress.addEventListener('blur', () => { if (seekAt !== null) endSeek(false); });
  const elapsedEl = h('span', null, '0:00');
  const totalEl = h('span');
  // Album or play list: a thin second bar for the whole performance.
  const allFill = h('div', { class: 'progress-fill' });
  const allText = h('p', { class: 'np-all-text' });
  const allEl = h('div', { class: 'np-all', hidden: true },
    h('div', { class: 'progress progress-thin', 'aria-hidden': 'true' }, allFill),
    allText);
  const restartBtn = h('button', { class: 'round-btn', type: 'button', 'aria-label': 'Restart from the beginning', onclick: () => run('restart', '/deck/restart') }, icon('restart'));
  const prevBtn = h('button', { class: 'round-btn', type: 'button', 'aria-label': 'Previous track', onclick: () => run('previous', '/deck/previous') }, icon('previous'));
  const mainBtn = h('button', { class: 'round-btn round-btn-main', type: 'button', onclick: pauseResume });
  const mainLabel = h('span', { class: 'transport-label' }, 'Pause');
  const nextBtn = h('button', { class: 'round-btn', type: 'button', 'aria-label': 'Next track', onclick: () => run('next', '/deck/next') }, icon('next'));
  const stopBtn = h('button', { class: 'round-btn', type: 'button', 'aria-label': 'Stop', onclick: () => run('stop', '/deck/stop') }, icon('stop'));
  const prevItem = h('div', { class: 'transport-item', hidden: true }, prevBtn, h('span', { class: 'transport-label' }, 'Previous'));
  const nextItem = h('div', { class: 'transport-item', hidden: true }, nextBtn, h('span', { class: 'transport-label' }, 'Next'));
  const transport = h('div', { class: 'transport' },
    h('div', { class: 'transport-item' }, restartBtn, h('span', { class: 'transport-label' }, 'Restart')),
    prevItem,
    h('div', { class: 'transport-item' }, mainBtn, mainLabel),
    nextItem,
    h('div', { class: 'transport-item' }, stopBtn, h('span', { class: 'transport-label' }, 'Stop')));

  // Up next: what plays when this piece ends, in order.
  // When the piano is idle with something waiting (after Stop), Play starts it.
  const upNextPlay = h('button', { class: 'btn btn-small btn-primary', type: 'button', hidden: true, onclick: playUpNextNow }, icon('play'), 'Play');
  const upNextList = h('ul', { class: 'list list-compact', role: 'list' });
  const upNextEl = h('section', { class: 'card upnext', hidden: true },
    h('div', { class: 'card-head' },
      h('h2', { class: 'card-title' }, 'Up next'),
      upNextPlay,
      h('button', { class: 'btn btn-small', type: 'button', onclick: clearUpNext }, 'Clear')),
    upNextList,
    h('p', { class: 'muted small' }, 'Each starts when the one before it ends (the piano takes a few seconds). Stop leaves the list waiting. While a piece plays, a Play button elsewhere offers “Play next” or “Add to Up next”.'));
  function renderUpNext() {
    const items = app.upNext || [];
    upNextEl.hidden = !items.length;
    upNextPlay.hidden = !(app.status && app.status.state === 'idle');
    fill(upNextList, items.map((it, i) => h('li', { class: 'row' },
      h('span', { class: 'row-num' }, String(i + 1)),
      h('a', { class: 'row-main', href: it.kind === 'piece' ? pieceHref(it.id) : it.kind === 'album' ? albumHref(it.id) : playlistHref(it.id) },
        h('span', { class: 'row-title' }, it.title || 'Untitled'),
        h('span', { class: 'row-sub' }, [it.kind === 'album' ? 'Album' : it.kind === 'playlist' ? 'Play list' : '', it.detail].filter(Boolean).join(' · '))),
      h('button', { class: 'row-star', type: 'button', 'aria-label': `Take ${it.title || 'this'} out of Up next`, title: 'Take it out of Up next',
        onclick: () => removeUpNext(i, it) }, icon('close')))));
  }
  async function removeUpNext(i, it) {
    try {
      const r = await api('DELETE', `/queue/${i}?kind=${encodeURIComponent(it.kind)}&id=${encodeURIComponent(it.id)}`);
      setUpNext(r && r.items);
    } catch (err) {
      toast(err.message, 'error');
      loadUpNext();
    }
  }
  async function playUpNextNow() {
    try {
      await api('POST', '/queue/play');
    } catch (err) {
      toast(err.message, 'error');
    }
  }
  async function clearUpNext() {
    try {
      const r = await api('DELETE', '/queue');
      setUpNext(r && r.items);
    } catch (err) {
      toast(err.message, 'error');
    }
  }

  const playPanel = h('section', { class: 'np' },
    stateEl, collEl, trackEl, titleEl, composerEl, performerEl, fileEl, voicedEl, correctedEl, fittedEl, editLinkEl, hintEl, messageEl,
    h('div', { class: 'np-progress' }, progress, h('div', { class: 'times' }, elapsedEl, totalEl), allEl),
    transport);

  const emptyTitle = h('h1', { class: 'empty-title' });
  const emptyText = h('p', { class: 'empty-text' });
  const emptyPanel = h('section', { class: 'empty' },
    h('img', { class: 'empty-art', src: '/icon.svg', alt: '', width: '84', height: '84' }),
    emptyTitle, emptyText,
    h('a', { class: 'btn btn-primary btn-large', href: '#/library' }, icon('library'), 'Choose a piece'));

  const recText = h('p', { class: 'empty-text' });
  const recPanel = h('section', { class: 'empty' },
    h('p', { class: 'np-state' }, h('span', { class: 'rec-dot' }), 'Recorder in use'),
    h('h1', { class: 'empty-title' }, 'Recording'),
    recText,
    h('a', { class: 'btn btn-primary btn-large', href: '#/record' }, icon('record'), 'Go to Record'));

  const level = levelStepper((delta) => run('level', '/deck/level', { delta }));
  const levelNote = h('p', { class: 'level-hint' });
  const levelMethod = levelMethodToggle();
  const levelCard = h('section', { class: 'card level-card' }, level.el, levelNote, levelMethod.el);

  root.append(playPanel, emptyPanel, recPanel, upNextEl, levelCard);
  update(app.status);

  async function run(name, path, body) {
    if (busy.has(name)) return null;
    busy.add(name);
    update(app.status);
    try {
      return await command(path, body);
    } finally {
      busy.delete(name);
      if (alive) update(app.status);
    }
  }

  function pauseResume() {
    const s = app.status;
    if (!s) return;
    if (s.state === 'playing') run('toggle', '/deck/pause');
    else if (s.state === 'paused') run('toggle', '/deck/resume');
  }

  function update(s) {
    upNextPlay.hidden = !(s && s.state === 'idle');
    if (!s) {
      playPanel.hidden = true;
      recPanel.hidden = true;
      levelCard.hidden = true;
      emptyPanel.hidden = false;
      setText(emptyTitle, 'Connecting…');
      setText(emptyText, 'Looking for the piano service on your network.');
      return;
    }
    const rec = isRecordSession(s);
    const play = isPlaySession(s) || (s.state === 'fault' && !!s.piece);
    playPanel.hidden = !play;
    recPanel.hidden = !rec;
    emptyPanel.hidden = play || rec;
    levelCard.hidden = rec;

    if (play) renderPlaying(s);
    else if (rec) setText(recText, recordWords(s));
    else renderEmpty(s);

    const live = s.state === 'playing' || s.state === 'paused';
    level.render(live ? s.level : s.target_level, s.min_level, busy.has('level'));
    levelMethod.render();
    if (s.level_on_pi) {
      // The Pi softens the notes it sends; the piano reads a few seconds ahead.
      setText(levelNote, live ? 'Heard after a few seconds: the piano reads a little ahead.' : 'Used when the next piece starts.');
      levelNote.hidden = false;
    } else {
      setText(levelNote, live ? '' : 'Used when the next piece starts.');
      levelNote.hidden = live;
    }
  }

  function renderPlaying(s) {
    const p = s.piece || {};
    stateEl.dataset.state = s.state;
    setText(stateEl, stateWords(s));
    setText(titleLink, p.title || 'Untitled');
    if (p.id) titleLink.href = pieceHref(p.id);
    else titleLink.removeAttribute('href');
    setText(composerEl, p.composer);
    composerEl.hidden = !p.composer;
    setText(performerEl, p.performer);
    performerEl.hidden = !p.performer;
    setText(fileEl, p.source_name);
    fileEl.hidden = !p.source_name;
    const cur = (s.tracks && s.tracks[s.track_index] && s.tracks[s.track_index].piece) || p; // in an album, the track playing now
    editLinkEl.hidden = !cur.id || p.performer === 'Editor preview';
    if (cur.id) editLink.href = editHref(cur.id);
    voicedEl.hidden = !s.voiced;
    correctedEl.hidden = !s.corrected;
    fittedEl.hidden = !s.fitted;

    let hint = '';
    if (s.state === 'loading') hint = 'Sending the player to the piano — about 2 seconds.';
    else if (s.state === 'stopping') hint = 'Waiting for the piano to finish.';
    setText(hintEl, hint);
    hintEl.hidden = !hint;
    setText(messageEl, s.message);
    messageEl.hidden = !s.message;

    const playing = s.state === 'playing';
    const paused = s.state === 'paused';
    const iconName = paused ? 'play' : 'pause';
    if (iconName !== mainIconName) { fill(mainBtn, icon(iconName)); mainIconName = iconName; }
    mainBtn.setAttribute('aria-label', paused ? 'Resume' : 'Pause');
    setText(mainLabel, paused ? 'Resume' : 'Pause');
    mainBtn.disabled = !(playing || paused) || busy.has('toggle');
    stopBtn.disabled = !(playing || paused || s.state === 'loading') || busy.has('stop');
    restartBtn.disabled = !(playing || paused) || busy.has('restart');

    // An album or play list: with none, all of this stays hidden and the
    // screen is the single-piece one.
    const c = s.collection || null;
    collEl.hidden = !c;
    trackEl.hidden = !c;
    allEl.hidden = !c;
    prevItem.hidden = !c;
    nextItem.hidden = !c;
    transport.classList.toggle('has-skip', !!c);
    progress.setAttribute('aria-label', c ? 'Position in the track' : 'Position in the piece');
    if (c) {
      setText(collLink, c.title || c.id || 'Untitled');
      const href = collectionHref(c);
      if (href) collLink.href = href;
      else collLink.removeAttribute('href');
      const { n, m } = trackOf(s);
      setText(trackEl, m ? `Track ${n} of ${m}` : `Track ${n}`);
      prevBtn.disabled = !(playing || paused) || busy.has('previous');
      nextBtn.disabled = !(playing || paused) || (m > 0 && n >= m) || busy.has('next');
    }

    const span = trackSpan(s);
    setText(totalEl, span.dur > 0 ? fmtTime(span.dur) : '');
    lastSec = -1;
    lastAllSec = -1;
    tick(currentPosition());
  }

  function renderEmpty(s) {
    let title = 'The piano is resting';
    let text = 'Choose a piece from the library and it will play here.';
    if (s.state === 'offline') {
      title = 'The piano is offline';
      text = 'Switch the piano on. This page reconnects by itself.';
    } else if (s.state === 'connecting') {
      title = 'Connecting to the piano…';
      text = 'This takes a moment after the piano is switched on.';
    } else if (s.state === 'fault') {
      title = 'Something went wrong';
      text = `${s.message || 'The piano reported a problem.'} It should recover by itself.`;
    }
    setText(emptyTitle, title);
    setText(emptyText, text);
  }

  // pos is in the whole performance; the main bar shows the current track.
  function tick(pos) {
    const s = app.status;
    if (!s || playPanel.hidden) return;
    const span = trackSpan(s);
    const dur = span.dur;
    const at = spanPosition(span, pos);
    if (seekAt !== null) return; // a point is being chosen on the bar
    progFill.style.transform = `scaleX(${dur ? Math.min(1, at / dur) : 0})`;
    const sec = Math.floor(at / 1000);
    if (sec !== lastSec) {
      lastSec = sec;
      elapsedEl.textContent = fmtTime(at);
      progress.setAttribute('aria-valuemax', String(Math.floor(dur / 1000)));
      progress.setAttribute('aria-valuenow', String(sec));
      progress.setAttribute('aria-valuetext', dur ? `${fmtTime(at)} of ${fmtTime(dur)}` : fmtTime(at));
    }
    if (s.collection) {
      const all = s.duration_ms > 0 ? s.duration_ms : 0;
      allFill.style.transform = `scaleX(${all ? Math.min(1, pos / all) : 0})`;
      const allSec = Math.floor(pos / 1000);
      if (allSec !== lastAllSec) {
        lastAllSec = allSec;
        setText(allText, all ? `${fmtTime(pos)} of ${fmtTime(all)}` : fmtTime(pos));
      }
    }
  }

  renderUpNext();

  return {
    update,
    tick,
    onQueue: renderUpNext,
    unmount() { alive = false; },
  };
}

function recordWords(s) {
  switch (s.state) {
    case 'loading': return 'Preparing the recorder…';
    case 'armed': return 'The recorder is ready and waiting to start.';
    case 'recording': return 'A recording is in progress.';
    case 'saving': return 'Saving the recording…';
    default: return '';
  }
}

// ===================================================================== Library

// The top of every library section: the title, Upload, and the segmented
// control (links, so each section has its own route).
function libraryHead(active) {
  return [
    h('div', { class: 'screen-head' },
      h('h1', { class: 'screen-title' }, 'Library'),
      h('a', { class: 'btn btn-small', href: '#/upload' }, icon('upload'), 'Upload')),
    h('nav', { class: 'segmented lib-tabs', 'aria-label': 'Library sections' },
      LIB_SECTIONS.map(([name, href, label]) => h('a', { href, 'aria-current': name === active ? 'page' : null }, label))),
  ];
}

// Kept across visits so coming back from a piece restores search and scroll.
const libState = { q: '', composer: '', performer: '', instrument: '', origin: '', grade: '', collection: '', sort: 'best', hidden: false, scroll: 0 };
const favState = { ...libState };

// Where a piece's data comes from (library.Grade*), best first.
const GRADES = { native: 'Native SE', 'se-midi': 'SE, via MIDI', performance: 'Performance (MIDI)', roll: 'Piano roll (MIDI)', transcribed: 'Transcribed from audio', sequenced: 'Sequenced (MIDI)' };

// When a piece last changed: saved over (an edited copy), or else added.
function pieceTime(p) { return Date.parse(p.updated_at || p.added_at) || 0; }

// The Pieces section, and with `favorites` the Favorites section: the starred
// pieces, albums and play lists.
function libraryScreen(root, _params, ctx, opts = {}) {
  const favs = !!opts.favorites;
  const st = favs ? favState : libState;
  let alive = true;
  let pieces = null;
  let colls = null; // albums and play lists matching the search (all starred ones, in Favorites)
  let seq = 0;
  let restoreScroll = ctx.from === 'piece';
  let playingId = null;

  const search = h('input', {
    class: 'search',
    type: 'search',
    placeholder: favs ? 'Search the favorites…' : 'Search pieces, albums, folders…',
    'aria-label': favs ? 'Search the favorites' : 'Search the library',
    autocomplete: 'off',
    autocorrect: 'off',
    autocapitalize: 'off',
    spellcheck: 'false',
    enterkeyhint: 'search',
  });
  search.value = st.q;
  search.addEventListener('input', debounce(() => {
    if (!alive || search.value === st.q) return;
    st.q = search.value;
    load();
  }, 300));
  search.addEventListener('keydown', (e) => { if (e.key === 'Enter') search.blur(); });

  const filters = [
    filterSelect('composer', 'All composers'),
    filterSelect('performer', 'All performers'),
    filterSelect('instrument', 'All instruments'),
    filterSelect('origin', 'All kinds'),
    filterSelect('grade', 'All sources'),
    filterSelect('collection', 'All collections'),
  ];
  const clearBtn = h('button', { class: 'chip-clear', type: 'button', hidden: true, onclick: clearAll }, 'Clear');
  const sortSel = h('select', {
    class: 'chip-select', 'aria-label': 'Order',
    onchange: () => { st.sort = sortSel.value; render(); },
  }, h('option', { value: 'best', title: 'Native SE files for a Steinway first, then other SE files, then MIDI sources' }, 'Best first'),
  h('option', { value: 'az' }, 'A to Z'), h('option', { value: 'new' }, 'Newest first'));
  sortSel.value = st.sort;
  const sortChip = h('label', { class: 'chip' }, sortSel);
  const favChip = !favs && favFirstChip(() => render());
  // Hidden pieces: a chip that shows them instead, when there are any.
  const hiddenBtn = h('button', { class: 'chip chip-toggle', type: 'button', hidden: true, 'aria-pressed': 'false',
    onclick: () => { st.hidden = !st.hidden; applyHidden(); render(); } });
  const countEl = h('p', { class: 'lib-count', 'aria-live': 'polite' });
  const list = h('ul', { class: 'list', role: 'list' });
  const body = h('div', null, h('p', { class: 'muted center' }, 'Loading…'));
  let allPieces = null; // hidden ones included

  // Select: tick pieces (from a search, say) and add them to a play list.
  let selecting = false;
  const picked = new Set();
  const selectBtn = h('button', { class: 'chip chip-toggle', type: 'button', 'aria-pressed': 'false', title: 'Tick pieces to add them to a play list',
    onclick: () => { selecting = !selecting; if (!selecting) picked.clear(); render(); } }, 'Select');
  const selCount = h('span', { class: 'sel-count' });
  const selAdd = h('button', { class: 'btn btn-primary btn-small', type: 'button', disabled: true,
    onclick: () => addToPlaylist([...picked], picked.size === 1 ? '1 piece' : `${picked.size} pieces`).then(() => { picked.clear(); render(); }) },
  icon('plus'), 'Add to play list…');
  const selBar = h('div', { class: 'sel-bar', hidden: true },
    selCount,
    h('button', { class: 'btn btn-small', type: 'button', onclick: () => { for (const r of rows()) picked.add(r.piece.id); render(); } }, 'All shown'),
    selAdd,
    h('button', { class: 'btn btn-small', type: 'button', onclick: () => { selecting = false; picked.clear(); render(); } }, 'Done'));

  root.append(
    ...libraryHead(favs ? 'favorites' : 'library'),
    h('div', { class: 'search-wrap' }, icon('search', 'icon search-icon'), search),
    h('div', { class: 'filters' }, selectBtn, favChip, filters.map((f) => f.el), sortChip, hiddenBtn, clearBtn),
    countEl,
    body,
    selBar);

  function renderSelect() {
    selectBtn.classList.toggle('active', selecting);
    selectBtn.setAttribute('aria-pressed', String(selecting));
    selBar.hidden = !selecting;
    setText(selCount, picked.size === 1 ? '1 selected' : `${picked.size} selected`);
    selAdd.disabled = !picked.size;
  }

  load();

  function filterSelect(key, allLabel) {
    const sel = h('select', {
      class: 'chip-select',
      'aria-label': allLabel.replace(/^All /, 'Filter by '),
      onchange: () => { st[key] = sel.value; render(); },
    });
    return { key, allLabel, sel, el: h('label', { class: 'chip' }, sel) };
  }

  function fillSelect(f, values) {
    const cur = st[f.key];
    const all = cur && !values.includes(cur) ? [cur, ...values] : values;
    fill(f.sel,
      h('option', { value: '' }, f.allLabel),
      all.map((v) => h('option', { value: v }, f.key === 'instrument' ? instrumentLabel(v) : f.key === 'origin' ? (ORIGINS[v] || v) : f.key === 'grade' ? (GRADES[v] || v) : v)));
    f.sel.value = cur;
    f.el.classList.toggle('active', !!cur);
  }

  // Does p pass every active filter except `except`?
  function matches(p, except) {
    return (except === 'composer' || !st.composer || p.composer === st.composer)
      && (except === 'performer' || !st.performer || p.performer === st.performer)
      && (except === 'instrument' || !st.instrument || p.instrument === st.instrument)
      && (except === 'origin' || !st.origin || p.origin === st.origin)
      && (except === 'grade' || !st.grade || p.grade === st.grade)
      && (except === 'collection' || !st.collection || p.collection === st.collection);
  }

  async function load() {
    const my = ++seq;
    const q = st.q.trim();
    try {
      // A search also looks through the albums and the folders' play lists;
      // Favorites always lists the starred ones.
      const withColls = q || favs;
      const [res, al, pl] = await Promise.all([
        api('GET', '/pieces?hidden=all' + (q ? '&q=' + encodeURIComponent(q) : '')),
        withColls ? api('GET', '/albums').catch(() => null) : null,
        withColls ? api('GET', '/playlists').catch(() => null) : null,
      ]);
      if (!alive || my !== seq) return;
      let albums = (al && al.albums) || [];
      let lists = (pl && pl.playlists) || [];
      if (favs) {
        albums = albums.filter((a) => a.favorite);
        lists = lists.filter((p) => p.favorite);
      }
      colls = withColls ? matchCollections(q, albums, lists) : null;
      allPieces = (res && res.pieces) || [];
      if (favs) allPieces = allPieces.filter((p) => p.favorite);
      else if (!q) {
        app.pieces = allPieces.filter((p) => !p.hidden);
        app.piecesStale = false;
      }
      applyHidden();
      render();
    } catch (err) {
      if (!alive || my !== seq) return;
      if (pieces) { toast(err.message, 'error'); return; }
      fill(body, h('div', { class: 'empty small' },
        h('p', { class: 'empty-text' }, `Could not load the library. ${err.message}`),
        h('button', { class: 'btn', type: 'button', onclick: load }, icon('refresh'), 'Try again')));
    }
  }

  // applyHidden picks the shown or the hidden pieces, and labels the chip.
  function applyHidden() {
    if (!allPieces) return;
    const nHidden = allPieces.filter((p) => p.hidden).length;
    if (!nHidden) st.hidden = false;
    pieces = allPieces.filter((p) => !!p.hidden === st.hidden);
    hiddenBtn.hidden = !nHidden;
    hiddenBtn.classList.toggle('active', st.hidden);
    hiddenBtn.setAttribute('aria-pressed', String(st.hidden));
    setText(hiddenBtn, st.hidden ? `Showing hidden (${nHidden}) ✕` : `Hidden (${nHidden})`);
  }

  // One row per performance (perf_key): the filtered instrument's rendering,
  // or else the best one (rankRenderings: Steinway first).
  function rows() {
    const groups = new Map();
    for (const p of pieces) {
      if (!matches(p, 'instrument')) continue;
      const key = p.perf_key ? 'k:' + p.perf_key : 'id:' + p.id;
      if (!groups.has(key)) groups.set(key, []);
      groups.get(key).push(p);
    }
    const out = [];
    for (const group of groups.values()) {
      let pick;
      if (st.instrument) {
        pick = group.find((p) => p.instrument === st.instrument);
        if (!pick) continue;
      } else {
        pick = bestRendering(group);
      }
      out.push({ piece: pick, count: group.length });
    }
    // Favorites first (if on), then the chosen order. "Best first" puts
    // native SE files for a Steinway above everything else (qualityTier).
    const order = st.sort === 'new'
      ? (a, b) => pieceTime(b) - pieceTime(a) || cmpPieces(a, b)
      : st.sort === 'az' ? cmpPieces : cmpBest;
    const cmp = withFavFirst(order);
    out.sort((a, b) => cmp(a.piece, b.piece));
    return out;
  }

  function render() {
    if (!pieces || !alive) return;
    for (const f of filters) fillSelect(f, uniqSorted(pieces.filter((p) => matches(p, f.key)).map((p) => p[f.key])));
    const anyFilter = !!(st.composer || st.performer || st.instrument || st.origin || st.grade || st.collection || st.hidden);
    clearBtn.hidden = !anyFilter;

    const items = rows();
    renderSelect();
    setText(countEl, items.length === 1 ? '1 performance' : `${items.length} performances`);
    countEl.hidden = !items.length;

    const collFirst = withFavFirst(() => 0);
    const albums = ((colls && colls.albums) || []).slice().sort(collFirst);
    const lists = ((colls && colls.playlists) || []).slice().sort(collFirst);
    const found = albums.length || lists.length
      ? [
        albums.length && h('h2', { class: 'list-head' }, albums.length === 1 ? '1 album' : `${albums.length} albums`),
        albums.length && h('ul', { class: 'list', role: 'list' }, albums.map(albumRow)),
        lists.length && h('h2', { class: 'list-head' }, lists.length === 1 ? '1 folder or play list' : `${lists.length} folders and play lists`),
        lists.length && h('ul', { class: 'list', role: 'list' }, lists.map(playlistRow)),
        items.length && h('h2', { class: 'list-head' }, 'Pieces'),
      ]
      : [];
    if (!items.length && !found.length) {
      fill(body, emptyState(anyFilter || !!st.q.trim()));
    } else {
      fill(list, ...items.map(row));
      fill(body, ...found, items.length ? list : null);
      playingId = null;
      markPlaying();
    }
    if (restoreScroll) {
      restoreScroll = false;
      window.scrollTo(0, st.scroll);
    }
  }

  function emptyState(filtered) {
    if (filtered) {
      return h('div', { class: 'empty small' },
        h('p', { class: 'empty-text' }, 'Nothing matches.'),
        h('button', { class: 'btn', type: 'button', onclick: clearAll }, 'Show everything'));
    }
    if (favs) {
      return h('div', { class: 'empty small fav-empty' },
        h('h2', { class: 'empty-title' }, 'No favorites yet'),
        h('p', { class: 'empty-text' }, 'Tap the star ', icon('star-outline'), ' beside a piece, an album or a play list, or on its page. Favorites gather here and come first everywhere in the library.'));
    }
    return h('div', { class: 'empty small' },
      h('h2', { class: 'empty-title' }, 'The library is empty'),
      h('p', { class: 'empty-text' }, 'Upload a performance to begin.'),
      h('a', { class: 'btn btn-primary', href: '#/upload' }, icon('upload'), 'Upload'));
  }

  // Search hits among the albums and play lists. Tapping opens the page with
  // "Play all"; the button here plays all at once.
  function matchCollections(q, albums, lists) {
    const words = fold(q).split(/\s+/).filter(Boolean);
    const hit = (text) => { const t = fold(text); return words.every((w) => t.includes(w)); };
    return {
      albums: albums.filter((a) => hit([a.name, a.performer, a.composer].join(' '))),
      playlists: lists.filter((p) => hit([p.name, p.source].join(' '))),
    };
  }

  function albumRow(a) {
    const sub = [a.performer, tracksText(a.tracks), a.duration_ms > 0 && fmtTime(a.duration_ms)].filter(Boolean).join(' · ');
    return h('li', { class: 'row' },
      h('a', { class: 'row-main', href: albumHref(a.name) },
        h('span', { class: 'row-title' }, a.name),
        sub && h('span', { class: 'row-sub' }, sub)),
      starButton('album', a.name, a, `the album ${a.name}`),
      h('button', {
        class: 'row-play', type: 'button', 'aria-label': `Play all of ${a.name}`,
        onclick: (e) => startPlay({ album: a.name }, e.currentTarget),
      }, icon('play')));
  }

  function playlistRow(pl) {
    const sub = [tracksText(pl.tracks), pl.duration_ms > 0 && fmtTime(pl.duration_ms)].filter(Boolean).join(' · ');
    return h('li', { class: 'row' },
      h('a', { class: 'row-main', href: playlistHref(pl.id) },
        h('span', { class: 'row-title' }, pl.name),
        sub && h('span', { class: 'row-sub' }, sub),
        h('span', { class: 'row-file' }, pl.source || 'Your play list')),
      starButton('playlist', pl.id, pl, `the play list ${pl.name}`),
      h('button', {
        class: 'row-play', type: 'button', 'aria-label': `Play all of ${pl.name}`,
        onclick: (e) => startPlay({ playlist: pl.id }, e.currentTarget),
      }, icon('play')));
  }

  function row({ piece: p, count }) {
    const sub = byline(p);
    const box = selecting && h('input', { type: 'checkbox', 'aria-label': `Select ${p.title || 'Untitled'}`, checked: picked.has(p.id),
      onchange: (e) => { if (e.target.checked) picked.add(p.id); else picked.delete(p.id); renderSelect(); } });
    return h('li', { class: 'row' + (selecting ? ' rec-row' : ''), 'data-id': p.id },
      box && h('label', { class: 'rec-check' }, box),
      h('a', { class: 'row-main', href: pieceHref(p.id) },
        h('span', { class: 'row-title' }, p.title || 'Untitled'),
        sub && h('span', { class: 'row-sub' }, sub),
        p.source_name && h('span', { class: 'row-file' }, p.source_name),
        h('span', { class: 'row-meta' },
          fmtTime(p.duration_ms),
          st.sort === 'new' && h('span', { class: 'row-date' }, fmtDateTime(p.updated_at || p.added_at)),
          p.grade && p.grade !== 'native' && h('span', { class: 'hint-pill grade-pill', title: p.grade_why || '' }, GRADES[p.grade] || p.grade),
          count > 1 && h('span', { class: 'hint-pill' }, `${count} renderings`))),
      starButton('piece', p.id, p, `“${p.title || 'Untitled'}”`),
      h('button', {
        class: 'row-play',
        type: 'button',
        'aria-label': `Play ${p.title || 'this piece'}`,
        onclick: (e) => playPiece(p.id, e.currentTarget),
      }, icon('play')));
  }

  function markPlaying() {
    const s = app.status;
    const id = isPlaySession(s) && s.piece ? s.piece.id : '';
    if (id === playingId) return;
    playingId = id;
    for (const li of list.children) li.classList.toggle('playing', !!id && li.dataset.id === id);
  }

  function clearAll() {
    st.composer = '';
    st.performer = '';
    st.instrument = '';
    st.origin = '';
    st.grade = '';
    st.collection = '';
    st.hidden = false;
    applyHidden();
    if (st.q) {
      st.q = '';
      search.value = '';
      load();
    } else {
      render();
    }
  }

  return {
    update: markPlaying,
    onLibrary: load,
    onSettings: render,
    unmount() {
      alive = false;
      st.scroll = window.scrollY;
    },
  };
}

// ================================================================ Recordings

// The recordings made on this piano and the copies made from them, newest
// first, for tidying up: tick several and hide them. Nothing is deleted;
// the Hidden list shows them again.
const recListState = { hidden: false };

function recordingsScreen(root) {
  let alive = true;
  let all = null;
  let shownList = [];
  const checked = new Set();
  const body = h('div', null, h('p', { class: 'muted center' }, 'Loading…'));
  const action = h('button', { class: 'btn btn-primary', type: 'button', disabled: true, onclick: apply });
  const allBox = h('input', { type: 'checkbox', 'aria-label': 'Select all', onchange: () => {
    checked.clear();
    if (allBox.checked) for (const p of shownList) checked.add(p.id);
    for (const box of body.querySelectorAll('input[data-id]')) box.checked = checked.has(box.dataset.id);
    updateBar();
  } });
  root.append(...libraryHead('recordings'), body);
  load();

  async function load() {
    try {
      const r = await api('GET', '/pieces?hidden=all');
      if (!alive) return;
      all = ((r && r.pieces) || []).filter((p) => p.origin === 'recorded' || p.from_recording);
      render();
    } catch (err) {
      if (alive) fill(body, h('p', { class: 'message message-error' }, err.message));
    }
  }

  function kind(p) {
    if (p.origin === 'recorded') return 'Recorded here';
    return p.corrected ? 'Corrected copy' : 'Edited copy';
  }

  function render() {
    const shown = all.filter((p) => !p.hidden);
    const hidden = all.filter((p) => p.hidden);
    if (!hidden.length) recListState.hidden = false;
    shownList = (recListState.hidden ? hidden : shown).slice().sort((a, b) => pieceTime(b) - pieceTime(a));
    for (const id of [...checked]) if (!shownList.some((p) => p.id === id)) checked.delete(id);
    const pick = (hiddenView) => () => { recListState.hidden = hiddenView; checked.clear(); render(); };
    fill(body,
      h('p', { class: 'muted small' }, recListState.hidden
        ? 'Hidden recordings are kept, out of the library’s list and search. Tick any to bring them back.'
        : 'Recordings made on this piano, and the copies made from them, newest first. Tick the ones you no longer need and hide them. Nothing is deleted; they can be shown again from “Hidden”.'),
      h('div', { class: 'filters' },
        h('button', { class: 'chip chip-toggle' + (recListState.hidden ? '' : ' active'), type: 'button', 'aria-pressed': String(!recListState.hidden), onclick: pick(false) }, `Shown (${shown.length})`),
        h('button', { class: 'chip chip-toggle' + (recListState.hidden ? ' active' : ''), type: 'button', 'aria-pressed': String(recListState.hidden), disabled: !hidden.length, onclick: pick(true) }, `Hidden (${hidden.length})`)),
      shownList.length
        ? [h('div', { class: 'rec-bar' }, h('label', { class: 'rec-all' }, allBox, ' All'), action),
          h('ul', { class: 'list', role: 'list' }, shownList.map(row))]
        : h('p', { class: 'muted' }, recListState.hidden ? 'Nothing is hidden.' : 'No recordings yet: record one from the Record tab.'));
    updateBar();
  }

  function updateBar() {
    const n = checked.size;
    allBox.checked = n > 0 && n === shownList.length;
    allBox.indeterminate = n > 0 && n < shownList.length;
    action.disabled = !n;
    setText(action, recListState.hidden ? (n ? `Show ${n} again` : 'Show again') : (n ? `Hide ${n}` : 'Hide'));
  }

  function row(p) {
    const box = h('input', { type: 'checkbox', 'data-id': p.id, 'aria-label': `Select ${p.title || 'Untitled'}`,
      onchange: () => { if (box.checked) checked.add(p.id); else checked.delete(p.id); updateBar(); } });
    box.checked = checked.has(p.id);
    return h('li', { class: 'row rec-row' },
      h('label', { class: 'rec-check' }, box),
      h('a', { class: 'row-main', href: pieceHref(p.id) },
        h('span', { class: 'row-title' }, p.title || 'Untitled'),
        h('span', { class: 'row-sub' }, [kind(p), fmtDateTime(p.updated_at || p.added_at)].join(' · ')),
        h('span', { class: 'row-meta' }, fmtTime(p.duration_ms))),
      h('button', { class: 'row-play', type: 'button', 'aria-label': `Play ${p.title || 'this recording'}`,
        onclick: (e) => playPiece(p.id, e.currentTarget) }, icon('play')));
  }

  async function apply() {
    const ids = [...checked];
    const hide = !recListState.hidden;
    action.disabled = true;
    let done = 0;
    for (const id of ids) {
      try {
        await api('PATCH', piecePath(id), { hidden: hide });
        done++;
      } catch (err) {
        toast(err.message, 'error');
        break;
      }
    }
    checked.clear();
    app.piecesStale = true;
    const what = done === 1 ? '1 recording' : `${done} recordings`;
    toast(hide ? `${what} hidden. “Hidden” shows them again.` : `${what} back in the library.`);
    if (alive) load();
  }

  return {
    onLibrary: load,
    unmount() { alive = false; },
  };
}

// ======================================================== Albums and play lists

// Kept across visits, like libState.
const albumsState = { q: '', scroll: 0 };
const playlistsState = { q: '', scroll: 0 };

function albumsScreen(root, _params, ctx) {
  return collectionListScreen(root, ctx, {
    section: 'albums',
    detail: 'album',
    kind: 'album',
    path: '/albums',
    field: 'albums',
    state: albumsState,
    placeholder: 'Search name, performer, composer…',
    searchLabel: 'Search the albums',
    noun: 'albums',
    count: (n) => (n === 1 ? '1 album' : `${n} albums`),
    key: (a) => a.name,
    title: (a) => a.name,
    words: (a) => [a.name, a.performer, a.composer],
    sub: (a) => [a.performer, tracksText(a.tracks), a.duration_ms > 0 && fmtTime(a.duration_ms)],
    note: () => '',
    href: (a) => albumHref(a.name),
    play: (a) => ({ album: a.name }),
    emptyTitle: 'No albums yet',
    emptyText: 'Pieces that have an album name are gathered here, in track order. You can set a piece’s album with Edit on its page.',
  });
}

// newPlaylist asks for a name, makes an empty play list and opens it.
async function newPlaylist() {
  const name = await confirmDialog({ title: 'New play list', text: 'Then add pieces from the Library: Select, or a piece’s “Add to play list”.', ok: 'Make it', input: { label: 'Name', placeholder: 'For example: Sunday evening' } });
  if (!name) return;
  try {
    const pl = await api('POST', '/playlists', { name, pieces: [] });
    location.hash = playlistHref(pl.id);
  } catch (err) {
    toast(err.message, 'error');
  }
}

function playlistsScreen(root, _params, ctx) {
  return collectionListScreen(root, ctx, {
    section: 'playlists',
    detail: 'playlist',
    kind: 'playlist',
    path: '/playlists',
    field: 'playlists',
    state: playlistsState,
    placeholder: 'Search play lists…',
    searchLabel: 'Search the play lists',
    noun: 'play lists',
    count: (n) => (n === 1 ? '1 play list' : `${n} play lists`),
    key: (p) => p.id,
    title: (p) => p.name,
    words: (p) => [p.name, p.source],
    sub: (p) => [tracksText(p.tracks), p.duration_ms > 0 && fmtTime(p.duration_ms)],
    note: (p) => p.source || 'Your play list',
    href: (p) => playlistHref(p.id),
    play: (p) => ({ playlist: p.id }),
    emptyTitle: 'No play lists yet',
    emptyText: 'Make one with New play list, or tick pieces in Pieces (Select) and add them.',
    action: () => h('button', { class: 'btn btn-primary', type: 'button', onclick: newPlaylist }, icon('plus'), 'New play list'),
  });
}

// The Albums and Play lists sections: a list fetched whole, sorted by name,
// filtered as you type. `cfg` says what to fetch and how to show an item.
function collectionListScreen(root, ctx, cfg) {
  const st = cfg.state;
  let alive = true;
  let items = null;
  let seq = 0;
  let restoreScroll = ctx.from === cfg.detail;
  let playingKey = null;

  const search = h('input', {
    class: 'search',
    type: 'search',
    placeholder: cfg.placeholder,
    'aria-label': cfg.searchLabel,
    autocomplete: 'off',
    autocorrect: 'off',
    autocapitalize: 'off',
    spellcheck: 'false',
    enterkeyhint: 'search',
  });
  search.value = st.q;
  search.addEventListener('input', () => {
    if (!alive || search.value === st.q) return;
    st.q = search.value;
    render();
  });
  search.addEventListener('keydown', (e) => { if (e.key === 'Enter') search.blur(); });

  const countEl = h('p', { class: 'lib-count', 'aria-live': 'polite' });
  const list = h('ul', { class: 'list', role: 'list' });
  const body = h('div', null, h('p', { class: 'muted center' }, 'Loading…'));

  root.append(
    ...libraryHead(cfg.section),
    h('div', { class: 'search-wrap' }, icon('search', 'icon search-icon'), search),
    h('div', { class: 'filters' }, favFirstChip(() => render())),
    cfg.action && h('div', { class: 'coll-action' }, cfg.action()),
    countEl,
    body);

  load();

  async function load() {
    const my = ++seq;
    try {
      const res = await api('GET', cfg.path);
      if (!alive || my !== seq) return;
      const all = res && Array.isArray(res[cfg.field]) ? res[cfg.field] : [];
      items = all.slice().sort((a, b) => cmpText(cfg.title(a), cfg.title(b)));
      render();
    } catch (err) {
      if (!alive || my !== seq) return;
      if (items) { toast(err.message, 'error'); return; }
      fill(body, h('div', { class: 'empty small' },
        h('p', { class: 'empty-text' }, `Could not load the ${cfg.noun}. ${err.message}`),
        h('button', { class: 'btn', type: 'button', onclick: load }, icon('refresh'), 'Try again')));
    }
  }

  function render() {
    if (!items || !alive) return;
    const words = fold(st.q).split(/\s+/).filter(Boolean);
    const shown = (words.length
      ? items.filter((it) => {
        const text = fold(cfg.words(it).filter(Boolean).join(' '));
        return words.every((w) => text.includes(w));
      })
      : items.slice()).sort(withFavFirst(() => 0));
    setText(countEl, cfg.count(shown.length));
    countEl.hidden = !shown.length;

    if (!shown.length) {
      fill(body, emptyState(items.length > 0));
    } else {
      fill(list, ...shown.map(row));
      fill(body, list);
      playingKey = null;
      markPlaying();
    }
    if (restoreScroll) {
      restoreScroll = false;
      window.scrollTo(0, st.scroll);
    }
  }

  function emptyState(filtered) {
    if (filtered) {
      return h('div', { class: 'empty small' },
        h('p', { class: 'empty-text' }, 'Nothing matches.'),
        h('button', { class: 'btn', type: 'button', onclick: clearSearch }, 'Show everything'));
    }
    return h('div', { class: 'empty small' },
      h('h2', { class: 'empty-title' }, cfg.emptyTitle),
      h('p', { class: 'empty-text' }, cfg.emptyText));
  }

  function row(it) {
    const title = cfg.title(it) || 'Untitled';
    const sub = cfg.sub(it).filter(Boolean).join(' · ');
    const note = cfg.note(it);
    return h('li', { class: 'row', 'data-key': cfg.key(it) },
      h('a', { class: 'row-main', href: cfg.href(it) },
        h('span', { class: 'row-title' }, title),
        sub && h('span', { class: 'row-sub' }, sub),
        note && h('span', { class: 'row-note' }, note)),
      starButton(cfg.kind, cfg.key(it), it, `${cfg.kind === 'album' ? 'the album' : 'the play list'} ${title}`),
      h('button', {
        class: 'row-play',
        type: 'button',
        'aria-label': `Play ${title}`,
        onclick: (e) => startPlay(cfg.play(it), e.currentTarget),
      }, icon('play')));
  }

  function markPlaying() {
    const s = app.status;
    const c = isPlaySession(s) ? s.collection : null;
    const key = (c && c.kind === cfg.kind && c.id) || '';
    if (key === playingKey) return;
    playingKey = key;
    for (const li of list.children) li.classList.toggle('playing', !!key && li.dataset.key === key);
  }

  function clearSearch() {
    st.q = '';
    search.value = '';
    render();
  }

  return {
    update: markPlaying,
    onLibrary: load,
    unmount() {
      alive = false;
      st.scroll = window.scrollY;
    },
  };
}

// One track of an album or play list. Tapping it opens the piece; its play
// button plays the album or play list from this track (`body`).
function trackRow({ index, num, piece: p, sub, pill, body }) {
  const title = p.title || 'Untitled';
  const main = [
    h('span', { class: 'row-title' }, title),
    sub && h('span', { class: 'row-sub' }, sub),
    p.source_name && h('span', { class: 'row-file' }, p.source_name),
    h('span', { class: 'row-meta' },
      fmtTime(p.duration_ms),
      pill && h('span', { class: 'hint-pill' }, pill)),
  ];
  return h('li', { class: 'row', 'data-index': String(index) },
    h('span', { class: 'row-num' }, num),
    p.id ? h('a', { class: 'row-main', href: pieceHref(p.id) }, main) : h('div', { class: 'row-main' }, main),
    h('button', {
      class: 'row-play',
      type: 'button',
      'aria-label': `Play from ${title}`,
      onclick: (e) => startPlay(body, e.currentTarget),
    }, icon('play')));
}

function markPlayingRow(list, index) {
  for (const li of list.children) li.classList.toggle('playing', index >= 0 && li.dataset.index === String(index));
}

function collectionMeta(count, durationMs) {
  return [tracksText(count), durationMs > 0 && fmtTime(durationMs)].filter(Boolean).join(' · ');
}

function notFound(title, text, href, label) {
  return h('div', { class: 'empty' },
    h('h1', { class: 'empty-title' }, title),
    h('p', { class: 'empty-text' }, text),
    h('a', { class: 'btn btn-primary', href }, icon('library'), label));
}

function loadFailed(what, err, retry) {
  return h('div', { class: 'empty' },
    h('p', { class: 'empty-text' }, `Could not load ${what}. ${err.message}`),
    h('button', { class: 'btn', type: 'button', onclick: retry }, icon('refresh'), 'Try again'));
}

// ======================================================================= Album

function albumScreen(root, [name]) {
  let alive = true;
  let album = null;
  let seq = 0;
  let playingIndex = null;
  const list = h('ul', { class: 'list', role: 'list' });

  root.append(h('p', { class: 'muted center' }, 'Loading…'));
  load();

  async function load() {
    const my = ++seq;
    try {
      const a = await api('GET', albumPath(name));
      if (!alive || my !== seq) return;
      album = a || {};
      render();
    } catch (err) {
      if (!alive || my !== seq) return;
      if (err.status === 404) {
        album = null;
        fill(root, notFound('Album not found', 'No piece in the library belongs to this album any more.', '#/albums', 'All albums'));
      } else if (!album) {
        fill(root, loadFailed('this album', err, load));
      } else {
        toast(err.message, 'error');
      }
    }
  }

  function albumName() { return (album && album.name) || name; }

  function render() {
    const a = album;
    const title = albumName();
    const tracks = Array.isArray(a.tracks) ? a.tracks : [];
    document.title = `${title} · Piano`;
    fill(list, tracks.map((t, i) => {
      const p = t.piece || {};
      return trackRow({
        index: i,
        num: t.track > 0 ? String(t.track) : '',
        piece: p,
        sub: p.composer && p.composer !== a.composer ? p.composer : '',
        pill: t.renderings > 1 ? `${t.renderings} versions` : '',
        body: { album: title, start: i },
      });
    }));
    fill(root,
      h('header', { class: 'piece-head' },
        h('h1', { class: 'piece-title' }, title),
        a.composer && h('p', { class: 'piece-composer' }, a.composer),
        a.performer && h('p', { class: 'piece-performer' }, a.performer),
        h('p', { class: 'collection-meta' }, collectionMeta(tracks.length, a.duration_ms))),
      h('div', { class: 'piece-actions' },
        h('button', {
          class: 'btn btn-primary btn-large',
          type: 'button',
          disabled: !tracks.length,
          onclick: (e) => startPlay({ album: title }, e.currentTarget),
        }, icon('play'), 'Play all'),
        starButton('album', title, a, `the album ${title}`, { large: true })),
      list);
    playingIndex = null;
    markPlaying();
  }

  function markPlaying() {
    const idx = album ? playingIndexIn('album', albumName()) : -1;
    if (idx === playingIndex) return;
    playingIndex = idx;
    markPlayingRow(list, idx);
  }

  return {
    update: markPlaying,
    onLibrary: load,
    unmount() { alive = false; },
  };
}

// =================================================================== Play list

function playlistScreen(root, [id]) {
  let alive = true;
  let pl = null;
  let editing = false;
  let seq = 0;
  let playingIndex = null;
  const list = h('ul', { class: 'list', role: 'list' });

  root.append(h('p', { class: 'muted center' }, 'Loading…'));
  load();

  async function load() {
    const my = ++seq;
    try {
      const p = await api('GET', playlistPath(id));
      if (!alive || my !== seq) return;
      pl = p || {};
      render();
    } catch (err) {
      if (!alive || my !== seq) return;
      if (err.status === 404) {
        pl = null;
        fill(root, notFound('Play list not found', 'This play list was deleted or never existed.', '#/playlists', 'All play lists'));
      } else if (!pl) {
        fill(root, loadFailed('this play list', err, load));
      } else {
        toast(err.message, 'error');
      }
    }
  }

  function playlistId() { return (pl && pl.id) || id; }

  function render() {
    const name = pl.name || 'Untitled play list';
    const pieces = Array.isArray(pl.pieces) ? pl.pieces : [];
    const plId = playlistId();
    document.title = `${name} · Piano`;
    fill(list, pieces.map((p, i) => {
      const li = trackRow({
        index: i,
        num: String(i + 1),
        piece: p || {},
        sub: byline(p || {}),
        pill: p && p.hidden ? 'hidden' : '',
        body: { playlist: plId, start: i },
      });
      if (editing) {
        const title = (p && p.title) || 'Untitled';
        li.classList.add('pl-edit-row');
        li.append(h('div', { class: 'pl-edit' },
          h('button', { class: 'ed-lanebtn pl-btn', type: 'button', disabled: i === 0, 'aria-label': `Move ${title} up`, title: 'Move up', onclick: () => move(i, -1) }, '↑'),
          h('button', { class: 'ed-lanebtn pl-btn', type: 'button', disabled: i === pieces.length - 1, 'aria-label': `Move ${title} down`, title: 'Move down', onclick: () => move(i, 1) }, '↓'),
          h('button', { class: 'ed-lanebtn pl-btn', type: 'button', 'aria-label': `Remove ${title} from the list`, title: 'Remove from the list (the piece stays in the library)', onclick: () => drop(i) }, icon('close'))));
      }
      return li;
    }));
    fill(root,
      h('header', { class: 'piece-head' },
        h('h1', { class: 'piece-title' }, name),
        pl.source ? h('p', { class: 'collection-source' }, `Imported from ${pl.source}`) : h('p', { class: 'collection-source' }, 'Your play list'),
        h('p', { class: 'collection-meta' }, collectionMeta(pieces.length, pl.duration_ms))),
      h('div', { class: 'piece-actions' },
        h('button', {
          class: 'btn btn-primary btn-large',
          type: 'button',
          disabled: !pieces.length,
          onclick: (e) => startPlay({ playlist: plId }, e.currentTarget),
        }, icon('play'), 'Play all'),
        starButton('playlist', plId, pl, `the play list ${name}`, { large: true }),
        h('button', { class: 'btn btn-large', type: 'button', onclick: rename }, icon('edit'), 'Rename'),
        h('button', { class: 'btn btn-large' + (editing ? ' btn-primary' : ''), type: 'button', disabled: !pieces.length && !editing,
          onclick: () => { editing = !editing; render(); } }, editing ? 'Done' : 'Edit order')),
      editing && h('p', { class: 'muted small' }, 'Move pieces up or down, or take them out of the list (they stay in the library). Add pieces from the Library: Select, or a piece’s “Add to play list”.'),
      pieces.length ? list : h('p', { class: 'muted center' }, 'This play list is empty. Add pieces from the Library: tap Select, tick some, then “Add to play list”.'),
      h('div', { class: 'danger-zone' },
        h('button', { class: 'btn btn-danger', type: 'button', onclick: remove }, icon('trash'), 'Delete play list')));
    playingIndex = null;
    markPlaying();
  }

  // save sends the list's new order; the reply is the list as stored.
  async function save(ids) {
    try {
      const p = await api('PUT', playlistPath(playlistId()), { pieces: ids });
      if (alive) { pl = p || pl; render(); }
    } catch (err) {
      toast(err.message, 'error');
    }
  }

  function ids() { return (pl.pieces || []).map((p) => p.id); }

  function move(i, d) {
    const a = ids();
    const j = i + d;
    if (j < 0 || j >= a.length) return;
    [a[i], a[j]] = [a[j], a[i]];
    save(a);
  }

  function drop(i) {
    const a = ids();
    a.splice(i, 1);
    save(a);
  }

  async function rename() {
    const name = await confirmDialog({ title: 'Rename play list', text: '', ok: 'Rename', input: { label: 'Name', value: pl.name || '' } });
    if (!name || name === pl.name) return;
    try {
      const p = await api('PUT', playlistPath(playlistId()), { name });
      if (alive) { pl = p || pl; render(); }
    } catch (err) {
      toast(err.message, 'error');
    }
  }

  function markPlaying() {
    const idx = pl ? playingIndexIn('playlist', playlistId()) : -1;
    if (idx === playingIndex) return;
    playingIndex = idx;
    markPlayingRow(list, idx);
  }

  async function remove(e) {
    const btn = e.currentTarget;
    const name = pl.name || 'Untitled play list';
    const ok = await confirmDialog({
      title: 'Delete this play list?',
      text: `“${name}” will be deleted. Only the list goes: its pieces stay in the library.`,
      ok: 'Delete',
      danger: true,
    });
    if (!ok || !alive) return;
    btn.disabled = true;
    try {
      await api('DELETE', playlistPath(playlistId()));
      toast(`Deleted “${name}”.`);
      location.hash = '#/playlists';
    } catch (err) {
      toast(err.message, 'error');
      btn.disabled = false;
    }
  }

  return {
    update: markPlaying,
    onLibrary: load,
    unmount() { alive = false; },
  };
}

// ======================================================================= Piece

// The piece an edited or corrected copy was made from, and the copies made
// from a piece. Hidden when there are none.
function versionsCard(p) {
  const body = h('div');
  const card = h('section', { class: 'card', hidden: true },
    h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Versions')), body);
  getAllPieces().then((pieces) => {
    const from = p.edited_from ? pieces.find((x) => x.id === p.edited_from) : null;
    const copies = pieces.filter((x) => x.edited_from === p.id).sort((a, b) => cmpText(b.added_at, a.added_at));
    if (!p.edited_from && !copies.length) return;
    fill(body,
      from && h('p', null, 'Made from ', h('a', { href: pieceHref(from.id) }, from.title || 'Untitled'), '.'),
      p.edited_from && !from && h('p', { class: 'muted small' }, 'Made from a piece that is no longer in the library.'),
      copies.length > 0 && h('p', { class: 'label' }, copies.length === 1 ? 'A copy made from it' : `${copies.length} copies made from it`),
      copies.length > 0 && h('ul', { class: 'corr-rec-list' }, copies.map((c) => h('li', null,
        h('a', { href: pieceHref(c.id) }, c.title || 'Untitled'),
        h('span', { class: 'muted small' }, ` · ${c.corrected ? 'corrected' : 'edited'} · ${fmtDateTime(c.added_at)}`)))));
    card.hidden = false;
  }, () => {});
  return card;
}

function pieceScreen(root, [id]) {
  let alive = true;
  let piece = null;
  let editing = false;
  let seq = 0;

  const renderingsSection = h('section', { class: 'card', hidden: true });
  root.append(h('p', { class: 'muted center' }, 'Loading…'));
  load();

  async function load() {
    const my = ++seq;
    try {
      const p = await api('GET', piecePath(id));
      if (!alive || my !== seq) return;
      piece = p;
      if (!editing) render();
      loadRenderings();
    } catch (err) {
      if (!alive || my !== seq) return;
      if (err.status === 404) renderGone();
      else if (!piece) renderError(err);
      else toast(err.message, 'error');
    }
  }

  function render() {
    const p = piece;
    document.title = `${p.title || 'Piece'} · Piano`;
    const playBtn = h('button', { class: 'btn btn-primary btn-large', type: 'button', onclick: (e) => playPiece(id, e.currentTarget) }, icon('play'), 'Play');
    const downloadBase = API + piecePath(id) + '/download?format=';
    fill(root,
      h('header', { class: 'piece-head' },
        h('h1', { class: 'piece-title' }, p.title || 'Untitled'),
        p.composer && h('p', { class: 'piece-composer' }, p.composer),
        p.performer && h('p', { class: 'piece-performer' }, p.performer)),
      p.hidden && h('p', { class: 'message message-info hidden-note' }, 'Hidden from the library and search. ',
        h('button', { class: 'btn btn-small', type: 'button', onclick: () => setHidden(false) }, 'Show it again')),
      h('div', { class: 'piece-actions' },
        playBtn,
        starButton('piece', id, p, `“${p.title || 'Untitled'}”`, { large: true }),
        h('button', { class: 'btn btn-large', type: 'button', onclick: startEdit }, icon('edit'), 'Edit'),
        h('button', { class: 'btn btn-large', type: 'button', onclick: () => addToPlaylist([id], `“${p.title || 'Untitled'}”`) }, icon('plus'), 'Add to play list')),
      metaList([
        ['Album', p.album ? h('a', { href: albumHref(p.album) }, p.track ? `${p.album}, track ${p.track}` : p.album) : ''],
        ['Recorded', fmtRecorded(p.recorded_on)],
        ['Instrument', instrumentLabel(p.instrument)],
        ['Length', fmtTime(p.duration_ms)],
        ['Origin', originText(p)],
        ['Source', p.grade ? `${GRADES[p.grade] || p.grade}: ${p.grade_why || ''}` : ''],
        ['Collection', p.collection || ''],
        ['Captured on', p.captured_on || ''],
        ['Converted with', p.curve ? (p.curve === 'bosendorfer' ? 'Bösendorfer’s curve' : 'the Disklavier curve') : ''],
        ['Licence', p.licence || ''],
        ['Source file', p.source_name, 'mono'],
        ['Header', p.header_text, 'pre'],
        ['Notes', p.notes, 'pre'],
        ['Added', fmtDateTime(p.added_at)],
        ['Changed', p.updated_at ? fmtDateTime(p.updated_at) : ''],
      ]),
      h('section', { class: 'card' },
        h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Download')),
        h('div', { class: 'btn-row' },
          h('a', { class: 'btn', href: downloadBase + 'se', download: '' }, icon('download'), 'SE files (.zip)'),
          h('a', { class: 'btn', href: downloadBase + 'midi', download: '' }, icon('download'), 'MIDI'))),
      h('section', { class: 'card' },
        h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Piano roll')),
        h('p', { class: 'muted small' }, p.origin === 'edited'
          ? 'Change notes, timing, loudness and pedals, and hear it from any point. Saving writes over this edited copy.'
          : 'Change notes, timing, loudness and pedals, and hear it from any point. Saving makes an edited copy; this piece stays as it is. Easiest on a computer.'),
        h('a', { class: 'btn', href: editHref(id) }, icon('edit'), 'Open in the editor')),
      h('section', { class: 'card' },
        h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Key check')),
        h('p', { class: 'muted small' }, 'How loudly each key plays in this piece, and in chords, when each key lets go. For a recording, what each key’s sensors measured.'),
        h('a', { class: 'btn', href: pieceKeysHref(id) }, 'Open')),
      (p.origin === 'recorded' || p.from_recording) && correctionsCard(p),
      h('section', { class: 'card' },
        h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Stahnke’s programs')),
        h('p', { class: 'muted small' }, 'The suite’s own programs can work on this piece too.'),
        h('div', { class: 'btn-row' },
          h('a', { class: 'btn btn-small', href: toolHref('edit', id) }, 'His editor (edit.exe)'),
          p.has_midi_source && h('a', { class: 'btn btn-small', href: toolHref('playmidi', id) }, 'Play the original MIDI with his MIDI player'),
          h('a', { class: 'btn btn-small', href: toolHref('dump', id) }, 'Dump to tape'))),
      versionsCard(p),
      renderingsSection,
      h('section', { class: 'card hide-card' },
        h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, p.hidden ? 'Hidden' : 'Hide')),
        h('p', { class: 'muted small' }, p.hidden
          ? 'This piece is hidden: it is not in the library’s list or search. Its files are kept.'
          : 'Takes this piece out of the library’s list and search, without deleting anything. Library → Recordings, or the Library’s “Hidden” filter, shows it again.'),
        h('div', { class: 'btn-row' },
          h('button', { class: 'btn', type: 'button', onclick: () => setHidden(!p.hidden) }, p.hidden ? 'Show it in the library again' : 'Hide from the library'))),
      h('div', { class: 'danger-zone' },
        h('button', { class: 'btn btn-danger', type: 'button', onclick: remove }, icon('trash'), 'Delete for good…')));
  }

  async function setHidden(hidden) {
    try {
      piece = await api('PATCH', piecePath(id), { hidden });
      app.piecesStale = true;
      toast(hidden ? `“${piece.title || 'Untitled'}” is hidden. Library → Recordings or the “Hidden” filter shows it again.` : `“${piece.title || 'Untitled'}” is back in the library.`);
      render();
    } catch (err) {
      toast(err.message, 'error');
    }
  }

  function renderGone() {
    fill(root, h('div', { class: 'empty' },
      h('h1', { class: 'empty-title' }, 'Not in the library'),
      h('p', { class: 'empty-text' }, 'This piece was deleted or never existed.'),
      h('a', { class: 'btn btn-primary', href: '#/library' }, icon('library'), 'Back to the library')));
  }

  function renderError(err) {
    fill(root, h('div', { class: 'empty' },
      h('p', { class: 'empty-text' }, `Could not load this piece. ${err.message}`),
      h('button', { class: 'btn', type: 'button', onclick: load }, icon('refresh'), 'Try again')));
  }

  async function loadRenderings() {
    if (!piece || !piece.perf_key) { renderingsSection.hidden = true; return; }
    let all;
    try { all = await getAllPieces(); } catch { return; }
    if (!alive || !piece) return;
    // Best first (Steinway first); by instrument name within a rank.
    const others = rankRenderings(all
      .filter((o) => o.perf_key === piece.perf_key && o.id !== piece.id)
      .sort((a, b) => cmpText(a.instrument, b.instrument)));
    if (!others.length) { renderingsSection.hidden = true; return; }
    const pref = preferredInstrument();
    renderingsSection.hidden = false;
    fill(renderingsSection,
      h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Other renderings')),
      h('p', { class: 'hint' }, 'The same performance, prepared for other instruments.'),
      h('ul', { class: 'list list-compact', role: 'list' }, others.map((o) => {
        const name = instrumentLabel(o.instrument) || o.title || o.id;
        return h('li', { class: 'row' },
          h('a', { class: 'row-main', href: pieceHref(o.id) },
            h('span', { class: 'row-title' }, name),
            h('span', { class: 'row-meta' },
              fmtTime(o.duration_ms),
              o.instrument && o.instrument === pref && h('span', { class: 'hint-pill' }, 'preferred'))),
          h('button', { class: 'row-play', type: 'button', 'aria-label': `Play the ${name} rendering`, onclick: (e) => playPiece(o.id, e.currentTarget) }, icon('play')));
      })));
  }

  function startEdit() {
    editing = true;
    const fields = [
      ['title', 'Title'],
      ['composer', 'Composer'],
      ['performer', 'Performer'],
      ['album', 'Album'],
      ['recorded_on', 'Recorded on', 'YYYY-MM-DD, YYYY-MM or YYYY'],
      ['instrument', 'Instrument'],
      ['notes', 'Notes'],
    ];
    const inputs = {};
    const instruments = uniqSorted((app.pieces || []).map((p) => p.instrument));
    const datalist = h('datalist', { id: 'dl-instruments' }, instruments.map((v) => h('option', { value: v })));
    const saveBtn = h('button', { class: 'btn btn-primary', type: 'submit' }, 'Save');

    const form = h('form', { class: 'edit-form', onsubmit: save },
      h('h1', { class: 'screen-title' }, 'Edit details'),
      fields.map(([key, label, placeholder]) => {
        const fid = 'edit-' + key;
        const input = key === 'notes'
          ? h('textarea', { class: 'input', id: fid, rows: '5' })
          : h('input', {
            class: 'input',
            id: fid,
            type: 'text',
            autocomplete: 'off',
            placeholder: placeholder || null,
            inputmode: key === 'recorded_on' ? 'numeric' : null,
            list: key === 'instrument' ? 'dl-instruments' : null,
          });
        input.value = piece[key] || '';
        inputs[key] = input;
        return h('div', { class: 'field' }, h('label', { for: fid }, label), input);
      }),
      datalist,
      h('div', { class: 'btn-row' },
        h('button', { class: 'btn', type: 'button', onclick: cancel }, 'Cancel'),
        saveBtn));
    fill(root, form);
    window.scrollTo(0, 0);
    inputs.title.focus({ preventScroll: true });

    function cancel() {
      editing = false;
      render();
      loadRenderings();
    }

    async function save(e) {
      e.preventDefault();
      const patch = {};
      for (const [key, input] of Object.entries(inputs)) {
        const value = key === 'notes' ? input.value : input.value.trim();
        if (value !== (piece[key] || '')) patch[key] = value;
      }
      if ('title' in patch && !patch.title) {
        toast('The title cannot be empty.', 'error');
        inputs.title.focus();
        return;
      }
      if (patch.recorded_on && !/^\d{4}(-\d{2}(-\d{2})?)?$/.test(patch.recorded_on)) {
        toast('Write the date as YYYY-MM-DD, YYYY-MM or YYYY, or leave it empty.', 'error');
        inputs.recorded_on.focus();
        return;
      }
      if (!Object.keys(patch).length) { cancel(); return; }
      saveBtn.disabled = true;
      try {
        const updated = await api('PATCH', piecePath(id), patch);
        if (!alive) return;
        piece = updated || { ...piece, ...patch };
        toast('Saved.');
        cancel();
      } catch (err) {
        toast(err.message, 'error');
        saveBtn.disabled = false;
      }
    }
  }

  async function remove() {
    const ok = await confirmDialog({
      title: 'Delete this piece for good?',
      text: `“${piece.title || 'Untitled'}” and its files will be removed from the piano. This cannot be undone. To keep it but out of the way, hide it instead.`,
      ok: 'Delete',
      danger: true,
      phrase: 'DELETE',
    });
    if (!ok) return;
    try {
      await api('DELETE', piecePath(id));
      toast(`Deleted “${piece.title || 'Untitled'}”.`);
      location.hash = '#/library';
    } catch (err) {
      toast(err.message, 'error');
    }
  }

  return {
    onLibrary(changed) {
      if (editing) return;
      if (!changed || changed === id) load();
      else loadRenderings();
    },
    onSettings() { if (!editing && piece) loadRenderings(); },
    unmount() { alive = false; },
  };
}

// ====================================================================== Upload

function uploadScreen(root) {
  let alive = true;
  let files = [];
  let uploading = false;

  const input = h('input', {
    class: 'visually-hidden',
    id: 'up-files',
    type: 'file',
    multiple: true,
    // No `accept` filter: iOS greys out extensions it does not know (.kb,
    // .lp, .sp). The server rejects anything else with a clear message.
    onchange: () => {
      addFiles(input.files);
      input.value = '';
    },
  });
  const zone = h('label', { class: 'dropzone', for: 'up-files' },
    icon('upload', 'icon dz-icon'),
    h('span', { class: 'dz-main' }, 'Choose files'),
    h('span', { class: 'dz-sub' }, 'or drop them here'));
  const fileList = h('ul', { class: 'file-list', role: 'list' });
  const warnEl = h('div', { class: 'message message-warn', hidden: true });
  const titleInput = h('input', { class: 'input', id: 'up-title', type: 'text', autocomplete: 'off', placeholder: 'Taken from the file if left empty' });
  const titleField = h('div', { class: 'field', hidden: true },
    h('label', { for: 'up-title' }, 'Title for this MIDI file (optional)'),
    titleInput);
  const bar = h('div', { class: 'progress-fill' });
  const progress = h('div', { class: 'progress', hidden: true, role: 'progressbar', 'aria-label': 'Upload progress', 'aria-valuemin': '0', 'aria-valuemax': '100' }, bar);
  const uploadLabel = h('span', null, 'Upload');
  const uploadBtn = h('button', { class: 'btn btn-primary btn-large btn-block', type: 'button', disabled: true, onclick: upload }, icon('upload'), uploadLabel);
  const results = h('div', { 'aria-live': 'polite' });

  root.append(
    h('h1', { class: 'screen-title' }, 'Upload'),
    h('p', { class: 'lead' }, 'Add performances to the library.'),
    h('ul', { class: 'explain' },
      h('li', null, 'A performance is three files with the same name: ',
        h('code', null, '.kb'), ', ', h('code', null, '.lp'), ' and ', h('code', null, '.sp'), '. Choose all three.'),
      h('li', null, 'Or a ', h('code', null, '.zip'), ' holding one or more such sets.'),
      h('li', null, 'Or a MIDI file (', h('code', null, '.mid'), '). It is converted, and the original is kept.')),
    input, zone, fileList, warnEl, titleField,
    h('div', { class: 'upload-actions' }, progress, uploadBtn),
    results);

  // Drops anywhere on this screen count, so a near miss does not open the file.
  const hasFiles = (e) => !!e.dataTransfer && Array.from(e.dataTransfer.types || []).includes('Files');
  const onDragOver = (e) => {
    if (!hasFiles(e)) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = uploading ? 'none' : 'copy';
    zone.classList.add('over');
  };
  const onDragLeave = (e) => { if (!e.relatedTarget) zone.classList.remove('over'); };
  const onDrop = (e) => {
    if (!hasFiles(e)) return;
    e.preventDefault();
    zone.classList.remove('over');
    if (!uploading) addFiles(e.dataTransfer.files);
  };
  document.addEventListener('dragenter', onDragOver);
  document.addEventListener('dragover', onDragOver);
  document.addEventListener('dragleave', onDragLeave);
  document.addEventListener('drop', onDrop);

  function addFiles(list) {
    const skipped = [];
    for (const f of Array.from(list || [])) {
      if (!UPLOAD_EXT.test(f.name)) { skipped.push(f.name); continue; }
      if (!files.some((g) => g.name === f.name && g.size === f.size)) files.push(f);
    }
    if (skipped.length) toast(`Skipped ${skipped.join(', ')}: not a .kb, .lp, .sp, .zip or MIDI file.`, 'warn');
    if (files.length) fill(results);
    render();
  }

  function render() {
    fill(fileList, ...files.map((f, i) => h('li', { class: 'file' },
      h('span', { class: 'file-name' }, f.name),
      h('span', { class: 'file-size' }, fmtBytes(f.size)),
      h('button', {
        class: 'icon-btn',
        type: 'button',
        'aria-label': `Remove ${f.name}`,
        disabled: uploading,
        onclick: () => { files.splice(i, 1); render(); },
      }, icon('close')))));
    const warnings = incompleteSets(files);
    warnEl.hidden = !warnings.length;
    fill(warnEl, ...warnings.map((w) => h('p', null, w)));
    titleField.hidden = !(files.length === 1 && /\.midi?$/i.test(files[0].name));
    uploadBtn.disabled = uploading || !files.length;
    input.disabled = uploading;
    zone.classList.toggle('disabled', uploading);
    if (!uploading) setText(uploadLabel, files.length > 1 ? `Upload ${files.length} files` : 'Upload');
  }

  function setProgress(frac) {
    bar.style.transform = `scaleX(${frac})`;
    progress.setAttribute('aria-valuenow', String(Math.round(frac * 100)));
    setText(uploadLabel, frac >= 1 ? 'Processing…' : `Uploading… ${Math.round(frac * 100)}%`);
  }

  async function upload() {
    if (!files.length || uploading) return;
    uploading = true;
    const title = titleField.hidden ? '' : titleInput.value.trim();
    fill(results);
    progress.hidden = false;
    render();
    setProgress(0);
    try {
      const res = await postPieces(files, title, setProgress);
      if (!alive) return;
      files = [];
      titleInput.value = '';
      showResults(res);
    } catch (err) {
      if (!alive) return;
      toast(err.message, 'error');
      fill(results, h('div', { class: 'message message-error' }, err.message));
    } finally {
      uploading = false;
      if (alive) {
        progress.hidden = true;
        render();
      }
    }
  }

  function showResults(res) {
    const added = Array.isArray(res.added) ? res.added : [];
    const dups = Array.isArray(res.duplicates) ? res.duplicates : [];
    const errors = Array.isArray(res.errors) ? res.errors : [];
    const parts = [];
    if (added.length) {
      parts.push(resultGroup(added.length === 1 ? 'Added 1 piece' : `Added ${added.length} pieces`, 'ok', added.map(pieceItem)));
    }
    if (dups.length) parts.push(resultGroup('Already in the library', 'dup', dups.map(pieceItem)));
    if (errors.length) {
      parts.push(resultGroup('Could not add', 'error', errors.map((e) => h('li', null,
        h('strong', null, e.name || 'Upload'), ': ', e.error || 'unknown error'))));
    }
    if (!parts.length) parts.push(h('p', { class: 'muted' }, 'Nothing was added.'));
    fill(results, ...parts);
  }

  function pieceItem(p) {
    const sub = byline(p);
    return h('li', null,
      h('a', { href: pieceHref(p.id) }, p.title || p.id),
      sub && h('span', { class: 'muted' }, ` · ${sub}`));
  }

  function resultGroup(title, kind, items) {
    return h('section', { class: `result result-${kind}` },
      h('h2', { class: 'result-title' }, title),
      h('ul', { role: 'list' }, items));
  }

  return {
    unmount() {
      alive = false;
      document.removeEventListener('dragenter', onDragOver);
      document.removeEventListener('dragover', onDragOver);
      document.removeEventListener('dragleave', onDragLeave);
      document.removeEventListener('drop', onDrop);
    },
  };
}

// Warn about .kb/.lp/.sp sets that are missing a file (the server decides).
function incompleteSets(files) {
  const sets = new Map();
  for (const f of files) {
    const m = /^(.*)\.(kb|lp|sp)$/i.exec(f.name);
    if (!m) continue;
    const key = m[1].toLowerCase();
    if (!sets.has(key)) sets.set(key, { base: m[1], exts: new Set() });
    sets.get(key).exts.add(m[2].toLowerCase());
  }
  const out = [];
  for (const { base, exts } of sets.values()) {
    const missing = ['kb', 'lp', 'sp'].filter((x) => !exts.has(x));
    if (missing.length) out.push(`“${base}” is missing ${missing.map((x) => '.' + x).join(' and ')}.`);
  }
  return out;
}

// ====================================================================== Record

function recordMode(s) {
  if (!s) return 'unknown';
  switch (s.state) {
    case 'idle': return 'idle';
    case 'loading': return s.piece ? 'busy' : 'loading';
    case 'armed':
    case 'recording':
    case 'saving':
      return s.state;
    case 'playing':
    case 'paused':
    case 'stopping':
      return 'busy';
    case 'offline':
    case 'connecting':
      return 'offline';
    case 'fault': return 'fault';
    default: return 'unknown';
  }
}

function recordScreen(root) {
  const busy = new Set();
  let alive = true;
  let mode = '';
  let armError = null;
  let shownError = null;
  let savedKey = null;

  const titleInput = h('input', {
    class: 'input',
    id: 'rec-title',
    type: 'text',
    placeholder: 'For example: Sunday practice',
    autocomplete: 'off',
    autocapitalize: 'sentences',
    enterkeyhint: 'go',
    maxlength: '120',
  });
  const armBtn = h('button', { class: 'btn btn-primary btn-large btn-block', type: 'submit' }, 'Get ready');
  const armForm = h('form', { onsubmit: (e) => { e.preventDefault(); arm(); } },
    h('div', { class: 'field' },
      h('label', { for: 'rec-title' }, 'Title'),
      titleInput,
      h('p', { class: 'hint' }, 'Optional. You can rename it later.')),
    armBtn,
    h('p', { class: 'hint rec-check-hint' }, 'To check the key sensors, record each key a few times at the same loudness, one after another, and chords pressed and let go together, several times each. Then open the recording’s Key check: the keys that stand out are listed, with corrections to use.'));

  // The per-key corrections recordings play with.
  const corrText = h('span', { class: 'muted small' });
  const corrLink = h('div', { class: 'rec-links' },
    h('a', { class: 'rec-corr', href: '#/record/test' }, h('strong', null, 'Sensor test'), h('span', { class: 'muted small' }, ': two guided recordings that show which keys’ sensors read wrong')),
    h('a', { class: 'rec-corr', href: '#/record/corrections' }, h('strong', null, 'Corrections'), corrText));
  const offCorr = onCorrections(showCorr);
  loadCorrections(false).then(showCorr, () => {});
  function showCorr() {
    const c = corrState.c;
    if (!c) return;
    setText(corrText, !c.on ? ': off' : c.active ? `: ${c.summary}` : ': none yet');
  }

  const stage = h('div', { class: 'rec-stage' });
  const errorBox = h('div', { class: 'message message-error', role: 'alert', hidden: true });
  const savedBox = h('section', { class: 'card', hidden: true });
  const timerEl = h('div', { class: 'rec-timer', role: 'timer' }, '0:00');
  const recTitleEl = h('p', { class: 'rec-title' });
  const faultEl = h('p', { class: 'message message-error' });
  const startBtn = h('button', { class: 'rec-big', type: 'button', 'aria-label': 'Start recording', onclick: () => run('start', '/record/start') }, icon('dot'));
  const stopBtn = h('button', { class: 'rec-big rec-big-stop', type: 'button', 'aria-label': 'Stop and save', onclick: () => run('stop', '/record/stop') }, icon('stop'));
  const discardBtn = h('button', { class: 'btn btn-danger', type: 'button', onclick: discard }, icon('trash'), 'Discard');

  root.append(h('h1', { class: 'screen-title' }, 'Record'), stage, errorBox, savedBox);
  update(app.status);

  function update(s) {
    const m = recordMode(s);
    if (m !== mode) {
      mode = m;
      build();
    }
    refresh(s);
  }

  function spinner(label) {
    return h('div', { class: 'spinner', role: 'progressbar', 'aria-label': label });
  }

  function build() {
    let nodes;
    switch (mode) {
      case 'idle':
        nodes = [h('p', { class: 'lead' }, 'Record what you play on this piano. It is saved to the library as a new piece.'), armForm, corrLink];
        break;
      case 'loading':
        nodes = [spinner('Preparing'), h('p', { class: 'rec-caption' }, 'Preparing the recorder…'), h('p', { class: 'muted' }, 'This takes a couple of seconds.')];
        break;
      case 'armed':
        nodes = [
          h('p', { class: 'np-state' }, 'Ready to record'),
          recTitleEl,
          startBtn,
          h('p', { class: 'rec-caption' }, 'Start recording'),
          h('p', { class: 'muted' }, 'Press Start, then play. Press Stop and save when you are done.'),
          h('div', { class: 'rec-secondary' }, discardBtn),
        ];
        break;
      case 'recording':
        nodes = [
          h('p', { class: 'np-state rec-live' }, h('span', { class: 'rec-dot' }), 'Recording'),
          timerEl,
          recTitleEl,
          stopBtn,
          h('p', { class: 'rec-caption' }, 'Stop and save'),
          h('div', { class: 'rec-secondary' }, discardBtn),
        ];
        break;
      case 'saving':
        nodes = [spinner('Saving'), h('p', { class: 'rec-caption' }, 'Saving the recording…')];
        break;
      case 'busy':
        nodes = [
          h('p', { class: 'lead' }, 'The piano is playing a piece right now.'),
          h('p', { class: 'muted' }, 'Stop it to make a recording.'),
          h('a', { class: 'btn', href: '#/' }, icon('now'), 'Go to Now Playing'),
        ];
        break;
      case 'offline':
        nodes = [
          h('p', { class: 'lead' }, 'The piano is not connected.'),
          h('p', { class: 'muted' }, 'Switch the piano on to record. This page updates by itself.'),
        ];
        break;
      case 'fault':
        nodes = [
          h('p', { class: 'lead' }, 'The piano reported a problem.'),
          faultEl,
          h('p', { class: 'muted' }, 'It should recover by itself in a moment.'),
        ];
        break;
      default:
        nodes = [spinner('Connecting'), h('p', { class: 'muted' }, 'Connecting…')];
    }
    stage.className = 'rec-stage rec-' + mode;
    fill(stage, ...nodes);
  }

  function refresh(s) {
    const title = (s && s.recording_title) || app.rec.title;
    setText(recTitleEl, title ? `“${title}”` : 'Untitled recording');
    setText(faultEl, (s && s.message) || 'Unknown problem.');
    armBtn.disabled = busy.has('arm');
    setText(armBtn, busy.has('arm') ? 'Getting ready…' : 'Get ready');
    startBtn.disabled = busy.has('start');
    stopBtn.disabled = busy.has('stop');
    discardBtn.disabled = busy.has('discard');

    const err = mode === 'idle' ? armError : null;
    if (err !== shownError) {
      shownError = err;
      errorBox.hidden = !err;
      if (err) {
        fill(errorBox,
          h('p', null, err.message),
          err.status === 503 && h('p', null, 'Check the ', h('a', { href: '#/status' }, 'Status'), ' screen: the piano must be online and record.exe installed.'));
      }
    }
    renderSaved();
    if (mode === 'recording') tick(currentPosition());
  }

  function renderSaved() {
    const sv = app.rec.saved;
    const visible = !!sv && !['loading', 'armed', 'recording', 'saving'].includes(mode);
    const key = visible ? `${sv.id}\n${sv.title}` : '';
    if (key === savedKey) return;
    savedKey = key;
    savedBox.hidden = !visible;
    if (!visible) return;
    fill(savedBox,
      h('p', { class: 'saved-label' }, icon('check'), 'Recording saved'),
      h('a', { class: 'saved-title', href: pieceHref(sv.id) }, sv.title),
      h('div', { class: 'btn-row' },
        h('button', { class: 'btn btn-primary', type: 'button', onclick: (e) => playPiece(sv.id, e.currentTarget) }, icon('play'), 'Play it'),
        h('a', { class: 'btn', href: pieceHref(sv.id) }, 'Details'),
        h('a', { class: 'btn', href: pieceKeysHref(sv.id) }, 'Key check'),
        h('a', { class: 'btn', href: editHref(sv.id) }, 'Edit')));
  }

  function tick(pos) {
    if (mode === 'recording') setText(timerEl, fmtTime(pos));
  }

  async function run(name, path) {
    if (busy.has(name)) return null;
    busy.add(name);
    refresh(app.status);
    try {
      return await command(path);
    } finally {
      busy.delete(name);
      if (alive) refresh(app.status);
    }
  }

  async function arm() {
    if (busy.has('arm')) return;
    armError = null;
    busy.add('arm');
    refresh(app.status);
    const title = titleInput.value.trim();
    try {
      app.rec.thenKeys = false; // an ordinary recording: no Key check afterwards
      const reply = await api('POST', '/record/arm', title ? { title } : {});
      if (isStatus(reply)) applyReplyStatus(reply);
      app.rec.saved = null;
      if (title) app.rec.title = title;
      titleInput.value = '';
    } catch (err) {
      armError = err;
      if (err.status !== 503) toast(err.message, 'error');
    } finally {
      busy.delete('arm');
      if (alive) refresh(app.status);
    }
  }

  async function discard() {
    const ok = await confirmDialog({
      title: 'Discard this recording?',
      text: 'Nothing will be saved.',
      ok: 'Discard',
      danger: true,
    });
    if (ok) {
      app.rec.thenKeys = false;
      run('discard', '/record/discard');
    }
  }

  return {
    update,
    tick,
    onLibrary() { refresh(app.status); },
    unmount() { alive = false; offCorr(); },
  };
}

// ======================================================================= Piano

// The piano's stored settings (its EEPROM table), read only for now: see
// docs/SETTINGS-EDITOR.md. A backup is taken with getlut.exe on the piano.

const pianoCache = { view: null, at: 0 };

async function loadPianoView(force) {
  if (!force && pianoCache.view && Date.now() - pianoCache.at < 60000) return pianoCache.view;
  const v = await api('GET', '/piano/table');
  pianoCache.view = v;
  pianoCache.at = Date.now();
  return v;
}

const NOTE_NAMES = ['C', 'C#', 'D', 'D#', 'E', 'F', 'F#', 'G', 'G#', 'A', 'A#', 'B'];
function noteName(n) { return NOTE_NAMES[n % 12] + (Math.floor(n / 12) - 1); }
function isBlackKey(n) { return [1, 3, 6, 8, 10].includes(n % 12); }
function keyHref(note) { return '#/piano/key/' + note; }
function signedNum(n) { return n > 0 ? `+${n}` : n < 0 ? `${MINUS}${-n}` : '0'; }
function signedPct(x) { return `${x > 0 ? '+' : x < 0 ? MINUS : ''}${Math.abs(x).toFixed(1)}%`; }

const BACKUP_REASONS = { manual: 'by hand', imported: 'imported', 'before-write': 'before a write', 'after-write': 'after a write', 'before-tool': 'before a tool', 'after-tool': 'after a tool' };
function reasonText(r) { return BACKUP_REASONS[r] || r || ''; }

function curveAt(view, k, velocity, i) {
  const j = view.velocities.indexOf(velocity);
  return j >= 0 && k.curve[j] && k.curve[j][i] >= 0 ? k.curve[j][i] : null;
}

// The measures the keyboard strip can show.
const PIANO_METRICS = [
  { id: 'strike', label: 'Strike', hint: 'Strike level at medium loudness (velocity 100): smaller is a harder strike.', value: (v, k) => curveAt(v, k, 100, 1) },
  { id: 'lead', label: 'Lead', hint: 'Lead time at medium loudness: ticks from the strike command to the hammer.', value: (v, k) => curveAt(v, k, 100, 0) },
  { id: 'release', label: 'Release', hint: 'Free release time: ticks for the key to come back up when let go.', value: (v, k) => k.free_release },
  { id: 'silent', label: 'Silent', hint: 'Silent press level: the gentle press for notes too soft to sound.', value: (v, k) => k.silent_level },
  { id: 'trim', label: 'Trim', hint: 'The trims stored in the piano: positive is louder.', value: (v, k) => k.trim, signed: true },
  { id: 'byear', label: 'By ear', hint: 'Your trims tried by ear, with the whole-piano setting: not written to the piano.', value: (v, k) => (voicingState.v ? voicingState.v.offset || 0 : 0) + voicingTrim(k.note), signed: true },
];

function pianoNote(n) {
  const cls = n.level === 'danger' ? 'message message-error' : n.level === 'warn' ? 'message message-warn' : 'message message-info';
  return h('div', { class: cls }, h('p', null, n.text));
}

function changeCount(d) {
  const n = d.changes.length + (d.other ? 1 : 0);
  return n === 1 ? '1 change' : `${n} changes`;
}

// A button that reads the piano's settings now. after(reply) runs on success.
function backupButton(after, primary = true) {
  const label = 'Back up now';
  const btn = h('button', { class: primary ? 'btn btn-primary' : 'btn', type: 'button', onclick: run }, icon('download'), label);
  async function run() {
    btn.disabled = true;
    fill(btn, 'Reading the piano’s settings…');
    try {
      const r = await api('POST', '/piano/backups');
      pianoCache.view = null;
      let text = 'Backed up the piano’s settings.';
      if (r.problem) text = `Backed up, but: ${r.problem}`;
      else if (r.same_as_previous) text = 'Backed up: nothing has changed since the last backup.';
      else if (r.changes) text = `Backed up: ${changeCount(r.changes)} since the last backup.`;
      toast(text, r.problem ? 'error' : 'info');
      if (after) after(r);
    } catch (err) {
      toast(err.message, 'error');
    } finally {
      btn.disabled = false;
      fill(btn, icon('download'), label);
    }
  }
  return btn;
}

const BACKUP_HINT = 'A backup runs getlut.exe on the piano. It only reads: nothing on the piano moves or changes. It takes a few seconds, and the piano can’t play meanwhile.';

// Software voicing: per-key trims and a whole-piano offset that the service
// applies to every piece it plays, with the firmware's own trim formula.
// Nothing is written to the piano (docs/SETTINGS-EDITOR.md, step 2).

const voicingState = { v: null, pending: false, timer: 0, listeners: new Set() };

async function loadVoicing(force) {
  if (voicingState.v && !force) return voicingState.v;
  voicingState.v = await api('GET', '/piano/voicing');
  return voicingState.v;
}

function voicingTrim(note) {
  const v = voicingState.v;
  return v && v.trims ? (v.trims[String(note)] || 0) : 0;
}

// changeVoicing applies fn to a copy of the voicing, shows it at once, and
// saves it shortly after (several quick steps make one save).
function changeVoicing(fn) {
  const cur = voicingState.v || { on: true, offset: 0, trims: {} };
  const next = { on: cur.on, offset: cur.offset, trims: { ...(cur.trims || {}) } };
  fn(next);
  for (const [k, t] of Object.entries(next.trims)) if (!t) delete next.trims[k];
  voicingState.v = { ...cur, ...next };
  for (const l of voicingState.listeners) l();
  voicingState.pending = true;
  clearTimeout(voicingState.timer);
  voicingState.timer = setTimeout(saveVoicing, 400);
}

// flushVoicing saves a change still waiting, before something plays.
async function flushVoicing() {
  if (!voicingState.pending) return;
  clearTimeout(voicingState.timer);
  await saveVoicing();
}

async function saveVoicing() {
  voicingState.pending = false;
  const v = voicingState.v;
  try {
    voicingState.v = await api('PUT', '/piano/voicing', { on: v.on, offset: v.offset, trims: v.trims });
  } catch (err) {
    toast(err.message, 'error');
    try { voicingState.v = await api('GET', '/piano/voicing'); } catch { /* keep what we have */ }
  }
  for (const l of voicingState.listeners) l();
}

function onVoicing(fn) {
  voicingState.listeners.add(fn);
  return () => voicingState.listeners.delete(fn);
}

// The firmware's trim arithmetic (lut.TrimVelocity): velocity → hammer speed
// 128000 / velocity, plus 5 per step, and back, rounding to nearest.
function divRound(a, b) { return Math.floor((2 * a + b) / (2 * b)); }
function trimVelocity(vel, t) {
  if (!t || vel < 2) return vel;
  const speed = divRound(128000, vel) + 5 * t;
  return speed < 126 ? 1023 : divRound(128000, speed);
}

const TRIM_DYNAMICS = [['loud', 64], ['medium', 100], ['soft', 256], ['very soft', 400]];

function trimEffectTable(t) {
  return h('table', { class: 'piano-table' },
    h('thead', null, h('tr', null, h('th', null, 'Played as'), h('th', null, 'Velocity'), h('th', null, 'Becomes'), h('th', null, 'Hammer speed'))),
    h('tbody', null, TRIM_DYNAMICS.map(([label, vel]) => {
      const b = trimVelocity(vel, t);
      return h('tr', null, h('td', null, label), h('td', null, String(vel)), h('td', null, String(b)), h('td', null, signedPct((vel / b - 1) * 100)));
    })));
}

function trimWords(t) {
  if (!t) return 'as calibrated';
  const pct = (100 / trimVelocity(100, t) - 1) * 100;
  return `${t > 0 ? 'louder' : 'softer'}, ${signedPct(pct)} hammer speed at medium`;
}

// A −/+ control for trim steps.
function trimStepper(label, onStep) {
  const minus = h('button', { class: 'round-btn', type: 'button', 'aria-label': `${label}: softer`, onclick: () => onStep(-1) }, icon('minus'));
  const plus = h('button', { class: 'round-btn', type: 'button', 'aria-label': `${label}: louder`, onclick: () => onStep(1) }, icon('plus'));
  const readout = h('output', { class: 'level-readout', 'aria-live': 'polite' }, '0');
  const el = h('div', { class: 'level', role: 'group', 'aria-label': label },
    minus, h('div', { class: 'level-center' }, h('span', { class: 'level-label' }, label), readout), plus);
  return {
    el,
    render(t, max) {
      setText(readout, signedNum(t));
      minus.disabled = t <= -max;
      plus.disabled = t >= max;
    },
  };
}

function libraryPlaying() {
  const s = app.status;
  return !!(s && isPlaySession(s) && s.piece && s.piece.id);
}

async function replayCurrent(btn) {
  if (btn) btn.disabled = true;
  await flushVoicing();
  try {
    const reply = await api('POST', '/deck/replay');
    if (isStatus(reply)) applyReplyStatus(reply);
    toast('Playing the current track again, with your voicing.');
  } catch (err) {
    toast(err.message, 'error');
  } finally {
    if (btn) btn.disabled = false;
  }
}

async function auditionKey(note, btn) {
  if (btn) btn.disabled = true;
  await flushVoicing();
  try {
    const reply = await api('POST', '/piano/audition', { note });
    if (isStatus(reply)) applyReplyStatus(reply);
    toast(`Playing ${noteName(note)}: each dynamic as recorded, then with the trim.`);
  } catch (err) {
    toast(err.message, 'error');
  } finally {
    if (btn) btn.disabled = false;
  }
}

// ====================================================== Fit by collection
// Each imported collection's fit to this piano (docs/LIBRARIES.md):
// loudness and pedal depth, set by ear, applied as its pieces play. The
// files never change.

function fitsCard() {
  return h('section', { class: 'card' },
    h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Fit by collection')),
    h('p', { class: 'muted small' }, 'Pieces from other pianos and sources (MAESTRO, Vienna, Batik …) can play a little loud, soft, or heavy on the pedal here. Set each collection’s loudness and pedal depth by ear; the files never change.'),
    h('a', { class: 'btn', href: '#/piano/fits' }, 'Open'));
}

function fitsScreen(root) {
  let alive = true;
  let data = null;
  let saveTimer = 0;
  const body = h('div', null, h('p', { class: 'muted center' }, 'Loading…'));
  root.append(h('h1', { class: 'screen-title' }, 'Fit by collection'),
    h('p', { class: 'muted small' }, 'Loudness is in % hammer speed (+10 is a little louder). Pedal depth is in % of the recorded depth (80 lifts the dampers less, 120 more). They apply to the collection’s pieces as they play, albums included; Now Playing says “Fitted to this piano”.'),
    body);
  load();

  async function load() {
    try {
      data = await api('GET', '/fits');
      if (alive) render();
    } catch (err) {
      if (alive) fill(body, h('p', { class: 'message message-error' }, err.message));
    }
  }

  function render() {
    const L = data.limits;
    if (!data.collections.length) {
      fill(body, h('div', { class: 'empty small' }, h('p', { class: 'empty-text' }, 'No collections yet. Pieces imported from MAESTRO, Vienna 4x22 or Batik appear here by collection.')));
      return;
    }
    const onBox = h('input', { type: 'checkbox', checked: data.on, onchange: () => { data.on = onBox.checked; save(); } });
    fill(body,
      h('label', { class: 'fit-on' }, onBox, ' Fits on'),
      data.collections.map((c) => h('section', { class: 'card fit-card' },
        h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, c.name)),
        h('p', { class: 'muted small' }, [`${c.pieces} piece${c.pieces === 1 ? '' : 's'}`, c.captured_on && `captured on a ${c.captured_on}`, c.licence].filter(Boolean).join(' · ')),
        stepper('Loudness', c.fit, 'speed', 5, L.min_speed, L.max_speed, (v) => (v > 0 ? `+${v}%` : `${v}%`)),
        stepper('Damper pedal depth', c.fit, 'damper', 5, L.min_pedal, L.max_pedal, (v) => `${v}%`),
        stepper('Soft pedal depth', c.fit, 'soft', 5, L.min_pedal, L.max_pedal, (v) => `${v}%`),
        h('div', { class: 'btn-row' },
          c.sample && h('button', { class: 'btn btn-small', type: 'button', onclick: (e) => playPiece(c.sample, e.currentTarget) }, icon('play'), 'Play a piece from it'),
          h('button', { class: 'btn btn-small', type: 'button', onclick: () => { c.fit = { speed: 0, damper: 100, soft: 100 }; save(); render(); } }, 'As recorded')))));
  }

  function stepper(label, fit, key, step, lo, hi, fmt) {
    const val = h('span', { class: 'fit-val' }, fmt(fit[key]));
    const set = (d) => {
      const v = Math.max(lo, Math.min(hi, fit[key] + d));
      if (v === fit[key]) return;
      fit[key] = v;
      setText(val, fmt(v));
      save();
    };
    return h('div', { class: 'fit-row' },
      h('span', { class: 'fit-label' }, label),
      h('button', { class: 'round-btn fit-btn', type: 'button', 'aria-label': `${label} lower`, onclick: () => set(-step) }, icon('minus')),
      val,
      h('button', { class: 'round-btn fit-btn', type: 'button', 'aria-label': `${label} higher`, onclick: () => set(step) }, icon('plus')));
  }

  // save sends every collection's fit, a moment after the last change.
  function save() {
    clearTimeout(saveTimer);
    saveTimer = setTimeout(async () => {
      const collections = {};
      for (const c of data.collections) collections[c.name] = c.fit;
      try {
        await api('PUT', '/fits', { on: data.on, collections });
      } catch (err) {
        toast(err.message, 'error');
      }
    }, 400);
  }

  return { unmount() { alive = false; } };
}

function pianoScreen(root) {
  let alive = true;
  let view = null;
  let metric = localStorage.getItem('pianoMetric') || 'strike';
  let voice = null;
  const keysBox = h('div');
  const off = onVoicing(() => {
    if (voice) voice.update();
    if (view && metric === 'byear') fill(keysBox, keysCard());
  });
  root.append(h('h1', { class: 'screen-title' }, 'Piano'), h('p', { class: 'muted center' }, 'Loading…'));
  load(false);

  async function load(force) {
    try {
      const [v] = await Promise.all([loadPianoView(force), loadVoicing(false).catch(() => null)]);
      if (!alive) return;
      view = v;
      render();
    } catch (err) {
      if (!alive) return;
      if (err.status === 404) renderEmpty();
      else fill(root, h('h1', { class: 'screen-title' }, 'Piano'), h('div', { class: 'message message-error' }, h('p', null, err.message)));
    }
  }

  function renderEmpty() {
    fill(root,
      h('h1', { class: 'screen-title' }, 'Piano'),
      h('section', { class: 'card' },
        h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'The piano’s settings')),
        h('p', null, 'The piano keeps its calibration and settings in its own memory. Back them up to see them here.'),
        h('div', { class: 'btn-row' }, backupButton(() => load(true))),
        h('p', { class: 'hint' }, BACKUP_HINT, ' Install getlut.exe on the Status page first.')),
      fitsCard(),
      toolsCard());
  }

  function render() {
    const v = view;
    const s = v.settings;
    const range = s.first_note === s.last_note ? `${noteName(s.first_note)} only` : `${noteName(s.first_note)} to ${noteName(s.last_note)}`;
    voice = voicingCard();
    fill(keysBox, keysCard());
    fill(root,
      h('h1', { class: 'screen-title' }, 'Piano'),
      h('p', { class: 'piano-sub' }, `${v.name} · serial ${s.serial} · ${noteName(v.bottom)} to ${noteName(v.top)}, ${v.keys.length} keys`),
      !v.version_ok && pianoNote({ level: 'danger', text: `Table version ${v.version}: not the 30.17 play.exe expects.` }),
      v.notes.length > 0 && h('section', { class: 'card' },
        h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Notes')),
        h('div', { class: 'piano-notes' }, v.notes.map(pianoNote))),
      voice.el,
      fitsCard(),
      keysBox,
      h('section', { class: 'card' },
        h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Switches')),
        metaList([
          ['Silent notes', s.discard_silent ? 'Left out' : 'Played as silent key presses'],
          ['Calibration range', range],
          ['First calibration', s.first_calibration ? 'Not done yet' : 'Done'],
          ['Velocity trims', v.trims_on ? 'On' : 'Off'],
          ['Hold level', String(v.hold_level)],
          ['German note names', s.german_names ? 'On' : 'Off'],
        ])),
      toolsCard(),
      playerCard(),
      h('section', { class: 'card' },
        h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Backups')),
        metaList([
          ['Shown here', `${fmtDateTime(v.backup.time)}, ${reasonText(v.backup.reason)}`],
          ['Table', `version ${v.version}, ${v.length} bytes`],
        ]),
        h('div', { class: 'btn-row' }, backupButton(() => load(true)), h('a', { class: 'btn', href: '#/piano/backups' }, 'All backups')),
        h('p', { class: 'hint' }, BACKUP_HINT)),
      h('p', { class: 'hint center' }, 'Read only for now. Trims come next, tried by ear before anything is written to the piano.'));
  }

  function keysCard() {
    const v = view;
    const m = PIANO_METRICS.find((x) => x.id === metric) || PIANO_METRICS[0];
    const vals = v.keys.map((k) => m.value(v, k));
    const nums = vals.filter((x) => Number.isFinite(x));
    let lo = Math.min(...nums);
    let hi = Math.max(...nums);
    if (m.signed) { hi = Math.max(Math.abs(lo), Math.abs(hi), 1); lo = 0; }
    const span = hi - lo || 1;
    const strip = h('div', { class: 'kb-strip', role: 'list', 'aria-label': m.hint });
    v.keys.forEach((k, i) => {
      const x = vals[i];
      let pct = 0;
      if (Number.isFinite(x)) pct = m.signed ? (x === 0 ? 0 : 10 + 90 * Math.abs(x) / span) : 10 + 90 * (x - lo) / span;
      const cls = ['kb-key', isBlackKey(k.note) && 'kb-black', (k.flags || []).length > 0 && 'kb-flag', m.signed && x < 0 && 'kb-neg'].filter(Boolean).join(' ');
      const text = `${k.name}: ${Number.isFinite(x) ? (m.signed ? signedNum(x) : x) : 'n/a'}`;
      strip.append(h('a', { class: cls, href: keyHref(k.note), role: 'listitem', title: text, 'aria-label': text },
        h('span', { class: 'kb-bar', style: `height:${pct.toFixed(1)}%` })));
    });
    const seg = h('div', { class: 'segmented piano-metrics', role: 'group', 'aria-label': 'Measure shown' },
      PIANO_METRICS.map((x) => h('button', {
        type: 'button', 'aria-pressed': x.id === m.id ? 'true' : 'false',
        onclick: () => { metric = x.id; localStorage.setItem('pianoMetric', metric); fill(keysBox, keysCard()); },
      }, x.label)));
    const range = m.signed ? '' : ` Range ${lo} to ${hi}.`;
    return h('section', { class: 'card' },
      h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Keys')),
      seg, strip,
      h('div', { class: 'kb-axis' }, h('span', null, noteName(v.bottom)), h('span', null, 'C4'), h('span', null, noteName(v.top))),
      h('p', { class: 'hint' }, `${m.hint}${range} Tap a key for its details; keys with a note are marked.`));
  }

  function playerCard() {
    const rows = view.player.map((c) => h('div', { class: 'player-row' },
      h('span', { class: 'player-label' }, c.label),
      h('span', { class: 'player-value' }, String(c.value),
        c.factory != null && c.factory !== c.value && h('span', { class: 'pill pill-warn piano-factory' }, `factory ${c.factory}`)),
      c.explain && h('span', { class: 'player-explain' }, c.explain)));
    return h('section', { class: 'card' },
      h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'How notes are played')),
      h('div', { class: 'player-list' }, rows),
      h('p', { class: 'hint' }, 'The player’s settings, kept in the piano. Times are in ticks of its 800 Hz clock (1.25 ms).'));
  }

  // The "Trying by ear" card: built once, updated in place so the controls
  // keep their focus.
  function voicingCard() {
    const onBtn = h('button', { type: 'button', onclick: () => changeVoicing((x) => { x.on = true; }) }, 'On');
    const offBtn = h('button', { type: 'button', onclick: () => changeVoicing((x) => { x.on = false; }) }, 'Off');
    const whole = trimStepper('Whole piano', (d) => changeVoicing((x) => {
      const max = (voicingState.v && voicingState.v.max_trim) || 60;
      x.offset = Math.max(-max, Math.min(max, (x.offset || 0) + d));
    }));
    const wholeText = h('p', { class: 'hint trim-words' });
    const keysEl = h('div', { class: 'voiced-keys' });
    const clearBtn = h('button', { class: 'btn btn-small', type: 'button', onclick: clearAll }, 'Clear all');
    const replayBtn = h('button', { class: 'btn', type: 'button', onclick: (e) => replayCurrent(e.currentTarget) }, icon('restart'), 'Replay current piece');
    const el = h('section', { class: 'card' },
      h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Trying by ear')),
      h('p', null, 'Make keys louder or softer and hear it in any piece. Applied as each piece plays; nothing is written to the piano.'),
      h('div', { class: 'segmented voicing-switch', role: 'group', 'aria-label': 'Trying by ear' }, onBtn, offBtn),
      whole.el,
      wholeText,
      h('p', { class: 'label' }, 'Keys you have trimmed'),
      keysEl,
      h('div', { class: 'btn-row' }, replayBtn, clearBtn),
      h('p', { class: 'hint' }, 'Open a key below to trim it and play test notes. Changes apply from the next piece, or at once with Replay.'));

    async function clearAll() {
      const ok = await confirmDialog({ title: 'Clear all trims?', text: 'Your by-ear trims and the whole-piano setting go back to zero. Nothing on the piano changes.', ok: 'Clear' });
      if (ok) changeVoicing((x) => { x.offset = 0; x.trims = {}; });
    }

    function update() {
      const vs = voicingState.v || { on: false, offset: 0, trims: {}, max_trim: 60 };
      onBtn.setAttribute('aria-pressed', vs.on ? 'true' : 'false');
      offBtn.setAttribute('aria-pressed', vs.on ? 'false' : 'true');
      whole.render(vs.offset || 0, vs.max_trim || 60);
      setText(wholeText, vs.offset ? `Every key ${signedNum(vs.offset)}: ${trimWords(vs.offset)}.` : 'Every key as calibrated.');
      const keys = Object.entries(vs.trims || {}).map(([n, t]) => [Number(n), t]).filter(([, t]) => t).sort((a, b) => a[0] - b[0]);
      fill(keysEl, keys.length
        ? keys.map(([n, t]) => h('a', { class: 'pill voiced-key', href: keyHref(n) }, `${noteName(n)} ${signedNum(t)}`))
        : h('span', { class: 'muted small' }, 'None yet.'));
      clearBtn.hidden = !keys.length && !vs.offset;
      replayBtn.hidden = !libraryPlaying();
    }
    update();
    return { el, update };
  }

  return {
    update() { if (voice) voice.update(); },
    unmount() { alive = false; off(); },
  };
}

function svgEl(tag, attrs, ...children) {
  const el = document.createElementNS(SVG_NS, tag);
  for (const [k, v] of Object.entries(attrs || {})) el.setAttribute(k, String(v));
  for (const c of children.flat(Infinity)) if (c != null && c !== false) el.append(c);
  return el;
}

// A small chart of a key's strike level and lead time against velocity.
function strikeChart(v, k) {
  const W = 330, H = 170, L = 30, R = 8, T = 10, B = 26;
  const n = v.velocities.length;
  const x = (i) => L + (W - L - R) * i / (n - 1);
  const y = (val) => T + (H - T - B) * (1 - val / 255);
  const path = (idx) => k.curve.map((p, i) => `${i ? 'L' : 'M'}${x(i).toFixed(1)} ${y(Math.max(p[idx], 0)).toFixed(1)}`).join(' ');
  const grid = [0, 128, 255].map((g) => [
    svgEl('line', { x1: L, x2: W - R, y1: y(g), y2: y(g), class: 'chart-grid' }),
    svgEl('text', { x: L - 6, y: y(g) + 4, 'text-anchor': 'end', class: 'chart-label' }, String(g)),
  ]);
  const ticks = [0, v.velocities.indexOf(100), v.velocities.indexOf(400), n - 1].filter((i) => i >= 0).map((i) =>
    svgEl('text', { x: x(i), y: H - 8, 'text-anchor': 'middle', class: 'chart-label' }, String(v.velocities[i])));
  return svgEl('svg', { viewBox: `0 0 ${W} ${H}`, class: 'piano-chart', role: 'img',
    'aria-label': `${k.name}: strike level and lead time from velocity ${v.velocities[0]} (loud) to ${v.velocities[n - 1]} (soft)` },
  grid, ticks,
  svgEl('path', { d: path(1), class: 'chart-level' }),
  svgEl('path', { d: path(0), class: 'chart-lead' }));
}

function pianoKeyScreen(root, [noteStr]) {
  let alive = true;
  let trimCard = null;
  const note = Number(noteStr);
  const off = onVoicing(() => { if (trimCard) trimCard.update(); });
  root.append(h('p', { class: 'muted center' }, 'Loading…'));
  Promise.all([loadPianoView(false), loadVoicing(false).catch(() => null)]).then(([v]) => { if (alive) render(v); }, (err) => {
    if (!alive) return;
    fill(root, h('div', { class: 'message message-error' }, h('p', null, err.status === 404 ? 'There is no backup of the piano’s settings yet.' : err.message)));
  });

  // The trim card: the trim stored in the piano, and the one tried by ear.
  function keyTrimCard(v, k) {
    const stored = k.trim === 0
      ? 'Stored in the piano: none.'
      : `Stored in the piano: ${signedNum(k.trim)}, ${trimWords(k.trim)}${v.trims_on ? '' : ' (trims are switched off there)'}.`;
    const stepper = trimStepper(`${k.name} by ear`, (d) => changeVoicing((x) => {
      const max = (voicingState.v && voicingState.v.max_trim) || 60;
      x.trims[String(k.note)] = Math.max(-max, Math.min(max, (Number(x.trims[String(k.note)]) || 0) + d));
    }));
    const words = h('p', { class: 'trim-words' });
    const table = h('div');
    const offNote = h('div', { class: 'message message-warn' },
      h('p', null, 'Trying by ear is switched off, so pieces play without your trims.'),
      h('button', { class: 'btn btn-small', type: 'button', onclick: () => changeVoicing((x) => { x.on = true; }) }, 'Switch on'));
    const testBtn = h('button', { class: 'btn btn-primary', type: 'button', onclick: (e) => auditionKey(k.note, e.currentTarget) }, icon('play'), 'Play test notes');
    const replayBtn = h('button', { class: 'btn', type: 'button', onclick: (e) => replayCurrent(e.currentTarget) }, icon('restart'), 'Replay current piece');
    const el = h('section', { class: 'card' },
      h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Loudness')),
      h('p', { class: 'muted small' }, stored),
      stepper.el,
      words,
      table,
      offNote,
      h('div', { class: 'btn-row' }, testBtn, replayBtn),
      h('p', { class: 'hint' }, 'Test notes play this key at four dynamics, each as recorded and then with your trim. Your trims apply to every piece as it plays; nothing is written to the piano, whose own trim applies on top.'));
    function update() {
      const vs = voicingState.v || { on: false, offset: 0, trims: {}, max_trim: 60 };
      const mine = voicingTrim(k.note);
      const total = mine + (vs.offset || 0);
      stepper.render(mine, vs.max_trim || 60);
      const whole = vs.offset ? `, with the whole piano at ${signedNum(vs.offset)}` : '';
      setText(words, total === 0 ? 'By ear: plays as calibrated.' : `By ear ${signedNum(mine)}${whole}: ${trimWords(total)}.`);
      fill(table, total !== 0 && trimEffectTable(total));
      offNote.hidden = vs.on || total === 0;
      replayBtn.hidden = !libraryPlaying();
    }
    update();
    return { el, update };
  }

  function render(v) {
    const i = v.keys.findIndex((k) => k.note === note);
    if (i < 0) {
      fill(root, h('p', { class: 'muted center' }, 'This piano has no such key.'));
      return;
    }
    const k = v.keys[i];
    document.title = `${k.name} · Piano`;
    const prev = v.keys[i - 1];
    const next = v.keys[i + 1];
    const notes = v.notes.filter((n) => (n.keys || []).includes(k.name));
    trimCard = keyTrimCard(v, k);
    fill(root,
      h('header', { class: 'key-head' },
        prev ? h('a', { class: 'btn btn-small', href: keyHref(prev.note), 'aria-label': `Previous key, ${prev.name}` }, icon('back'), prev.name) : h('span'),
        h('h1', { class: 'key-title' }, k.name, h('span', { class: 'key-midi' }, `MIDI ${k.note}`)),
        next ? h('a', { class: 'btn btn-small', href: keyHref(next.note), 'aria-label': `Next key, ${next.name}` }, next.name, icon('forward')) : h('span')),
      notes.map(pianoNote),
      trimCard.el,
      h('section', { class: 'card' },
        h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Strike')),
        strikeChart(v, k),
        h('p', { class: 'chart-legend' }, h('span', { class: 'legend-level' }, 'Strike level'), h('span', { class: 'legend-lead' }, 'Lead time (ticks)')),
        h('table', { class: 'piano-table' },
          h('thead', null, h('tr', null, h('th', null, 'Velocity'), h('th', null, 'Lead time'), h('th', null, 'Strike level'))),
          h('tbody', null, v.breakpoints.map((bp, r) => h('tr', null,
            h('td', null, String(bp)), h('td', null, String(k.lead[r])), h('td', { class: k.level[r] === 255 ? 'at-limit' : null }, String(k.level[r])))))),
        h('p', { class: 'hint' }, 'Measured by the calibrator at each velocity: loud on the left, soft on the right. A smaller strike level is a harder strike; 255 is the gentlest the table can hold.')),
      h('section', { class: 'card' },
        h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Release and silent press')),
        metaList([
          ['Free release time', `${k.free_release} ticks`],
          ['60-tick release level', String(k.release[0])],
          ['120-tick release level', String(k.release[1])],
          ['Silent press level', String(k.silent_level)],
        ]),
        h('p', { class: 'hint' }, 'A bigger level is less force. A tick is 1.25 ms.')),
      h('p', { class: 'hint center' }, `From the backup of ${fmtDateTime(v.backup.time)}. Read only for now.`));
  }

  return {
    update() { if (trimCard) trimCard.update(); },
    unmount() { alive = false; off(); },
  };
}

function pianoBackupsScreen(root) {
  let alive = true;
  let list = [];
  const listEl = h('ul', { class: 'list' });
  const fromSel = h('select', { class: 'input', id: 'cmp-from' });
  const toSel = h('select', { class: 'input', id: 'cmp-to' });
  const result = h('div', { class: 'piano-compare' });
  const compareCard = h('section', { class: 'card', hidden: true },
    h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Compare')),
    h('div', { class: 'field' }, h('label', { for: 'cmp-from' }, 'From'), h('div', { class: 'select-wrap' }, fromSel)),
    h('div', { class: 'field' }, h('label', { for: 'cmp-to' }, 'To'), h('div', { class: 'select-wrap' }, toSel)),
    h('div', { class: 'btn-row' }, h('button', { class: 'btn', type: 'button', onclick: compare }, 'Compare')),
    result);
  root.append(h('h1', { class: 'screen-title' }, 'Backups'),
    h('section', { class: 'card' },
      h('p', null, 'Every backup of the piano’s settings is kept.'),
      h('div', { class: 'btn-row' }, backupButton(() => load(true))),
      h('p', { class: 'hint' }, BACKUP_HINT)),
    compareCard,
    h('section', { class: 'card' }, h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'All backups')), listEl));
  load(false);

  async function load(compareNewest) {
    try {
      const r = await api('GET', '/piano/backups');
      if (!alive) return;
      list = r.backups || [];
      render();
      if (compareNewest && list.length > 1) compare();
    } catch (err) {
      if (alive) fill(listEl, h('li', { class: 'message message-error' }, h('p', null, err.message)));
    }
  }

  function label(b) { return `${fmtDateTime(b.time)} (${reasonText(b.reason)})`; }

  function render() {
    compareCard.hidden = list.length < 2;
    fill(fromSel, list.map((b, i) => h('option', { value: b.id, selected: i === 1 }, label(b))));
    fill(toSel, list.map((b, i) => h('option', { value: b.id, selected: i === 0 }, label(b))));
    if (!list.length) {
      fill(listEl, h('li', { class: 'muted' }, 'No backups yet.'));
      return;
    }
    fill(listEl, list.map((b) => h('li', { class: 'row' },
      h('div', { class: 'row-main' },
        h('div', { class: 'row-title' }, fmtDateTime(b.time)),
        h('div', { class: 'row-sub' }, [reasonText(b.reason), b.model, b.valid ? `version ${b.version}` : b.problem].filter(Boolean).join(' · ')),
        h('div', { class: 'row-file' }, b.id)),
      h('div', { class: 'backup-actions' },
        b.valid && h('a', { class: 'btn btn-small', href: toolHref('putlut', b.id), 'aria-label': `Restore ${b.id} to the piano` }, 'Restore…'),
        h('a', { class: 'btn btn-small', href: `${API}/piano/backups/${encodeURIComponent(b.id)}`, download: '', 'aria-label': `Download ${b.id}` }, icon('download'), '.lut')))));
  }

  async function compare() {
    fill(result, h('p', { class: 'muted' }, 'Comparing…'));
    try {
      const r = await api('GET', `/piano/compare?from=${encodeURIComponent(fromSel.value)}&to=${encodeURIComponent(toSel.value)}`);
      if (!alive) return;
      const d = r.diff;
      if (d.bytes === 0) {
        fill(result, h('p', null, 'No differences: the two tables are identical.'));
        return;
      }
      fill(result,
        h('p', null, `${changeCount(d)} (${d.bytes} ${d.bytes === 1 ? 'byte' : 'bytes'} differ).`),
        d.changes.length > 0 && h('table', { class: 'piano-table' },
          h('thead', null, h('tr', null, h('th', null, 'Key'), h('th', null, 'What'), h('th', null, 'From'), h('th', null, 'To'))),
          h('tbody', null, d.changes.map((c) => h('tr', null, h('td', null, c.key || ''), h('td', null, c.what), h('td', null, c.from), h('td', null, c.to))))),
        d.other > 0 && h('p', { class: 'hint' }, `${d.other} other ${d.other === 1 ? 'byte differs' : 'bytes differ'} in parts not yet named.`));
    } catch (err) {
      if (alive) fill(result, h('div', { class: 'message message-error' }, h('p', null, err.message)));
    }
  }

  return { unmount() { alive = false; } };
}

// =================================================================== Key check

// How each key plays in a piece. For a recording, it is what each key's
// sensors measured. Velocity: record each key a few times at the same
// loudness, and the keys that record louder or softer than their neighbours
// stand out. Release: record chords pressed and let go together, and the
// keys that come up regularly before or after the rest stand out.
// Several recordings checked together: #/keys/id1,id2,…
function keysManyScreen(root, [ids]) { return pieceKeysScreen(root, [ids], null, true); }

function pieceKeysScreen(root, [id], _ctx, many = false) {
  let alive = true;
  root.append(h('h1', { class: 'screen-title' }, 'Key check'), h('p', { class: 'muted center' }, 'Loading…'));
  api('GET', many ? '/keycheck?pieces=' + encodeURIComponent(id) : piecePath(id) + '/keys').then((r) => { if (alive) render(r); }, (err) => {
    if (alive) fill(root, h('h1', { class: 'screen-title' }, 'Key check'), h('div', { class: 'message message-error' }, h('p', null, err.message)));
  });

  function signedMs(ms) { return `${ms > 0 ? '+' : ms < 0 ? MINUS : ''}${Math.abs(ms)} ms`; }

  function releaseCard(rc) {
    const head = h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Release timing'));
    if (!rc || !rc.chords) {
      return h('section', { class: 'card' }, head,
        h('p', null, 'No chords of three or more keys in this piece.'),
        h('p', { class: 'hint' }, 'To check the release sensors, record chords pressed and let go together, several times each.'));
    }
    const flagged = new Set(rc.outliers.map((k) => k.note));
    const outliers = rc.outliers.map((k) => h('div', { class: 'message message-warn' }, h('p', null,
      `${k.name} lets go ${Math.abs(k.median_ms)} ms ${k.median_ms < 0 ? 'before' : 'after'} the rest of its chords (typically; ${k.count} chords, from ${signedMs(k.min_ms)} to ${signedMs(k.max_ms)}).`)));
    return h('section', { class: 'card' }, head,
      outliers.length ? h('div', { class: 'piano-notes' }, outliers) : h('p', null, `No key lets go ${rc.threshold_ms} ms or more before or after the rest of its chords.`),
      h('table', { class: 'piano-table' },
        h('thead', null, h('tr', null, h('th', null, 'Key'), h('th', null, 'Chords'), h('th', null, 'Typical'), h('th', null, 'Range'))),
        h('tbody', null, rc.keys.map((k) => h('tr', null,
          h('td', null, h('a', { href: keyHref(k.note) }, k.name)),
          h('td', null, String(k.count)),
          h('td', { class: flagged.has(k.note) ? 'at-limit' : null }, signedMs(k.median_ms)),
          h('td', null, k.min_ms === k.max_ms ? signedMs(k.min_ms) : `${signedMs(k.min_ms)} to ${signedMs(k.max_ms)}`))))),
      h('p', { class: 'hint' }, `${rc.chords} chords (keys pressed within ${rc.window_ms} ms of each other). Each key’s release is compared with the middle release of its chord: minus is early. A sensor that fires early or late does it every time; a finger varies. To check the release sensors, record chords pressed and let go together, several times each.`, !many && [' In the ', h('a', { href: editHref(id) }, 'editor'), ', a key’s releases can be moved: click the key at the left for all its notes, then Release.']));
  }

  // Press timing: the same comparison as releases, on when each key goes
  // down. Shown only when there are chords.
  function pressCard(pc) {
    if (!pc || !pc.chords) return null;
    const flagged = new Set(pc.outliers.map((k) => k.note));
    const outliers = pc.outliers.map((k) => h('div', { class: 'message message-warn' }, h('p', null,
      `${k.name} goes down ${Math.abs(k.median_ms)} ms ${k.median_ms < 0 ? 'before' : 'after'} the rest of its chords (typically; ${k.count} chords, from ${signedMs(k.min_ms)} to ${signedMs(k.max_ms)}).`)));
    const late = pc.keys.filter((k) => Math.abs(k.median_ms) >= pc.threshold_ms / 2);
    return h('section', { class: 'card' },
      h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Press timing')),
      outliers.length ? h('div', { class: 'piano-notes' }, outliers) : h('p', null, `No key goes down ${pc.threshold_ms} ms or more before or after the rest of its chords.`),
      late.length > 0 && h('table', { class: 'piano-table' },
        h('thead', null, h('tr', null, h('th', null, 'Key'), h('th', null, 'Chords'), h('th', null, 'Typical'), h('th', null, 'Range'))),
        h('tbody', null, late.map((k) => h('tr', null,
          h('td', null, h('a', { href: keyHref(k.note) }, k.name)),
          h('td', null, String(k.count)),
          h('td', { class: flagged.has(k.note) ? 'at-limit' : null }, signedMs(k.median_ms)),
          h('td', null, k.min_ms === k.max_ms ? signedMs(k.min_ms) : `${signedMs(k.min_ms)} to ${signedMs(k.max_ms)}`))))),
      h('p', { class: 'hint' }, `Each key’s press is compared with the middle press of its chord (keys pressed within ${pc.window_ms} ms of each other): minus is early. A careful hand spreads a chord by 10 to 20 ms; a key regularly ${pc.threshold_ms} ms or more apart may have a press sensor that fires early or late. The table lists the keys ${pc.threshold_ms / 2} ms or more apart.`));
  }

  function render(r) {
    const c = r.check;
    const title = many ? `${r.pieces.length} recordings together` : r.piece.title || 'Untitled';
    document.title = `Key check · ${title} · Piano`;
    if (!c.notes && !(c.no_reading || []).length) {
      fill(root, h('h1', { class: 'screen-title' }, 'Key check'), h('p', { class: 'muted' }, 'This piece has no notes.'));
      return;
    }
    const byNote = new Map(c.keys.map((k) => [k.note, k]));
    const maxDev = Math.max(c.threshold * 2, ...c.keys.map((k) => Math.abs(k.deviation)));
    const strip = h('div', { class: 'kb-strip', role: 'list', 'aria-label': 'How far each key stands from its neighbours' });
    for (let n = 21; n <= 108; n++) {
      const k = byNote.get(n);
      const dev = k ? k.deviation : 0;
      const pct = k ? (dev === 0 ? 3 : 8 + 92 * Math.abs(dev) / maxDev) : 0;
      const out = k && Math.abs(dev) >= c.threshold && k.count >= 2;
      const cls = ['kb-key', isBlackKey(n) && 'kb-black', out && 'kb-flag', dev < 0 && 'kb-neg', !k && 'kb-none'].filter(Boolean).join(' ');
      const text = k ? `${k.name}: ${signedPct(dev)} hammer speed against its neighbours, ${k.count} notes` : `${noteName(n)}: not played`;
      strip.append(h('span', { class: cls, role: 'listitem', title: text, 'aria-label': text }, h('span', { class: 'kb-bar', style: `height:${pct.toFixed(1)}%` })));
    }
    const outliers = c.outliers.map((k) => h('div', { class: 'message message-warn' }, h('p', null,
      `${k.name} records ${k.deviation > 0 ? 'louder' : 'softer'} than its neighbours: ${signedPct(k.deviation)} hammer speed (typical velocity ${k.median}, ${k.count} notes).`)));
    const none = c.no_reading || [];
    fill(root,
      h('h1', { class: 'screen-title' }, 'Key check'),
      many
        ? h('p', { class: 'piano-sub' }, r.pieces.map((p, i) => [i ? ', ' : '', h('a', { href: pieceKeysHref(p.id) }, p.title || 'Untitled')]))
        : h('p', { class: 'piano-sub' }, h('a', { href: pieceHref(id) }, title), r.origin === 'recorded' ? ' · recorded on this piano' : '', ' · ', h('a', { href: editHref(id) }, 'open in the editor')),
      none.length > 0 && h('div', { class: 'message message-error' },
        h('p', null, `${none.reduce((a, k) => a + k.count, 0)} notes have no hammer reading (velocity 0): ${none.map((k) => (k.count > 1 ? `${k.name} ×${k.count}` : k.name)).join(', ')}. The key went down but the recorder measured no hammer speed, and on playback the player moves such a key down without sounding it (a silent press). If the note sounded when you played it, its hammer sensor missed it: give it a loudness in the editor, or turn on “fill in” in Corrections.`)),
      h('section', { class: 'card' },
        h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Velocity: keys that stand out')),
        outliers.length ? h('div', { class: 'piano-notes' }, outliers) : h('p', null, `No key stands more than ${c.threshold}% apart from its neighbours.`),
        h('p', { class: 'hint' }, `${c.notes} notes on ${c.keys.length} keys; typical velocity ${c.median}. Velocity is the SE scale: bigger is softer. A key is compared with up to four played keys on each side, in hammer speed (128000 ÷ velocity).`)),
      h('section', { class: 'card' },
        h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Velocity against the neighbours')),
        strip,
        h('div', { class: 'kb-axis' }, h('span', null, 'A0'), h('span', null, 'C4'), h('span', null, 'C8')),
        h('p', { class: 'hint' }, 'Bar height: how far each key stands from its neighbours. Blue: softer; red: beyond the limit.')),
      releaseCard(r.releases),
      pressCard(r.presses),
      r.recorded_here && !r.corrected && suggestionsCard(r),
      h('section', { class: 'card' },
        h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Velocity, every key')),
        h('table', { class: 'piano-table' },
          h('thead', null, h('tr', null, h('th', null, 'Key'), h('th', null, 'Notes'), h('th', null, 'Typical'), h('th', null, 'Range'), h('th', null, 'Vs neighbours'))),
          h('tbody', null, c.keys.map((k) => h('tr', null,
            h('td', null, h('a', { href: keyHref(k.note) }, k.name)),
            h('td', null, String(k.count)),
            h('td', null, String(k.median)),
            h('td', null, k.min === k.max ? String(k.min) : `${k.min}–${k.max}`),
            h('td', { class: Math.abs(k.deviation) >= c.threshold && k.count >= 2 ? 'at-limit' : null }, signedPct(k.deviation)))))),
        h('p', { class: 'hint' }, 'To check the sensors, record each key a few times at the same loudness, one after another. A wide range on one key means its readings vary.')));
  }

  return { unmount() { alive = false; } };
}

// ======================================================= Recording corrections

// Per-key corrections for recordings made on this piano (docs/RECORDING-
// EDITOR.md): a key's loudness (% hammer speed) and when it lets go, plus
// filling in presses with no hammer reading. Applied when such a recording
// plays; the recordings themselves never change.

const corrState = { c: null, timer: 0, listeners: new Set() };

async function loadCorrections(force) {
  if (!corrState.c || force) corrState.c = await api('GET', '/corrections');
  return corrState.c;
}

function onCorrections(fn) {
  corrState.listeners.add(fn);
  return () => corrState.listeners.delete(fn);
}

function corrNotify() { for (const fn of corrState.listeners) fn(); }

// changeCorrections edits the corrections here at once and saves them soon.
function changeCorrections(fn) {
  if (!corrState.c) return;
  fn(corrState.c);
  corrNotify();
  clearTimeout(corrState.timer);
  corrState.timer = setTimeout(saveCorrections, 400);
}

async function saveCorrections() {
  clearTimeout(corrState.timer);
  const c = corrState.c;
  if (!c) return;
  try {
    corrState.c = await api('PUT', '/corrections', {
      on: c.on, fill_zero: c.fill_zero,
      keys: c.keys.map((k) => ({ note: k.note, speed: k.speed, press_ms: k.press_ms || 0, release_ms: k.release_ms })),
    });
  } catch (err) {
    toast(err.message, 'error');
    corrState.c = await api('GET', '/corrections').catch(() => c);
  }
  corrNotify();
}

function speedWords(s) { return s > 0 ? `${s}% louder` : s < 0 ? `${-s}% softer` : 'as recorded'; }
function releaseWords(ms) { return ms > 0 ? `${ms} ms later` : ms < 0 ? `${-ms} ms earlier` : 'as recorded'; }

function correctionsScreen(root) {
  let alive = true;
  let off = () => {};
  root.append(h('h1', { class: 'screen-title' }, 'Corrections'), h('p', { class: 'muted center' }, 'Loading…'));

  const onBtn = h('button', { type: 'button', onclick: () => changeCorrections((c) => { c.on = true; }) }, 'On');
  const offBtn = h('button', { type: 'button', onclick: () => changeCorrections((c) => { c.on = false; }) }, 'Off');
  const fillBox = h('input', { type: 'checkbox', id: 'corr-fill', onchange: () => changeCorrections((c) => { c.fill_zero = fillBox.checked; }) });
  const summaryEl = h('p', { class: 'corr-summary' });
  const listEl = h('div', { class: 'corr-list' });
  const addSelect = h('select', { class: 'input corr-add-select', 'aria-label': 'Key to correct' });
  for (let n = 108; n >= 21; n--) addSelect.append(h('option', { value: String(n) }, noteName(n)));
  addSelect.value = '60';

  loadCorrections(true).then(() => { if (alive) render(); }, (err) => {
    if (alive) fill(root, h('h1', { class: 'screen-title' }, 'Corrections'), h('div', { class: 'message message-error' }, h('p', null, err.message)));
  });

  function render() {
    fill(root,
      h('h1', { class: 'screen-title' }, 'Corrections'),
      h('p', { class: 'lead' }, 'For recordings made on this piano. A key whose sensors read wrong records too loud or too soft, or lets go too early or too late. A correction puts that right when the recording plays.'),
      h('section', { class: 'card' },
        h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Correct recordings when they play')),
        h('div', { class: 'segmented voicing-switch', role: 'group', 'aria-label': 'Correct recordings when they play' }, onBtn, offBtn),
        summaryEl,
        h('label', { class: 'corr-check', for: 'corr-fill' }, fillBox,
          h('span', null, h('strong', null, 'Fill in notes with no hammer reading. '),
            'A press recorded with velocity 0 (the hammer sensor did not time it) plays as a silent press, so a note that sounded goes missing. This gives it the loudness of the notes around it, so it sounds. Leave it off if you make deliberate silent presses.')),
        h('p', { class: 'hint' }, 'The recordings themselves never change, so the Key check always shows what the sensors measured. “Save a corrected copy” on a recording’s page puts the corrections into a copy.')),
      h('section', { class: 'card' },
        h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Keys')),
        listEl,
        h('div', { class: 'corr-add' }, addSelect, h('button', { class: 'btn btn-small', type: 'button', onclick: addKey }, icon('plus'), 'Add a key')),
        h('p', { class: 'hint' }, 'Loudness is in hammer speed: +20% makes the key’s notes 20% faster, so louder. Press moves when the key goes down, and Release when it lets go: plus is later. Silent presses stay silent. A recording’s Key check suggests all three.')),
      h('section', { class: 'card' },
        h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Recordings made here')),
        h('p', { class: 'muted small' }, 'Open a recording’s Key check to see which keys stand out, and use its suggestions; or tick several and check them together. The ', h('a', { href: '#/record/test' }, 'sensor test'), ' guides the recordings to make.'),
        recordingsChecklist(/^Sensor test/)));
    renderList();
    update();
    off = onCorrections(() => { if (alive) update(); });
  }

  function update() {
    const c = corrState.c;
    if (!c) return;
    onBtn.setAttribute('aria-pressed', c.on ? 'true' : 'false');
    offBtn.setAttribute('aria-pressed', c.on ? 'false' : 'true');
    fillBox.checked = !!c.fill_zero;
    setText(summaryEl, !c.on ? 'Off: recordings play as they were recorded.'
      : c.active ? `On: ${c.summary}.` : 'On, but nothing to correct yet.');
    if (listEl.dataset.keys !== c.keys.map((k) => k.note).join(',')) renderList();
  }

  // The rows are rebuilt only when keys come or go, so a field being typed
  // in is never replaced.
  function renderList() {
    const c = corrState.c;
    listEl.dataset.keys = c.keys.map((k) => k.note).join(',');
    if (!c.keys.length) {
      fill(listEl, h('p', { class: 'muted small' }, 'No keys yet.'));
      return;
    }
    fill(listEl,
      h('div', { class: 'corr-row corr-head', 'aria-hidden': 'true' }, h('span', null, 'Key'), h('span', null, 'Loudness'), h('span', null, 'Press'), h('span', null, 'Release'), h('span')),
      c.keys.map((k) => row(k)));
  }

  function row(k) {
    const speedWordsEl = h('span', { class: 'corr-words' }, speedWords(k.speed));
    const relWordsEl = h('span', { class: 'corr-words' }, releaseWords(k.release_ms));
    const pressWordsEl = h('span', { class: 'corr-words' }, releaseWords(k.press_ms || 0));
    const field = (label, value, step, min, max, unit, set, wordsEl, words) => {
      const input = h('input', { class: 'input corr-num', type: 'number', step: String(step), min: String(min), max: String(max), value: String(value), 'aria-label': `${noteName(k.note)}: ${label}` });
      input.addEventListener('change', () => {
        const x = Math.round(Number(input.value));
        if (!Number.isFinite(x)) return;
        const v = Math.max(min, Math.min(max, x));
        input.value = String(v);
        setText(wordsEl, words(v));
        changeCorrections((c) => { const e = c.keys.find((q) => q.note === k.note); if (e) set(e, v); });
      });
      return h('span', { class: 'corr-field' }, input, h('span', { class: 'corr-unit' }, unit), wordsEl);
    };
    return h('div', { class: 'corr-row' },
      h('a', { class: 'corr-key', href: keyHref(k.note) }, k.name || noteName(k.note)),
      field('loudness', k.speed, 5, -50, 100, '%', (e, v) => { e.speed = v; }, speedWordsEl, speedWords),
      field('press', k.press_ms || 0, 5, -300, 300, 'ms', (e, v) => { e.press_ms = v; }, pressWordsEl, releaseWords),
      field('release', k.release_ms, 10, -1000, 1000, 'ms', (e, v) => { e.release_ms = v; }, relWordsEl, releaseWords),
      h('button', { class: 'btn btn-small btn-danger', type: 'button', 'aria-label': `Remove ${noteName(k.note)}`,
        onclick: () => changeCorrections((c) => { c.keys = c.keys.filter((q) => q.note !== k.note); }) }, 'Remove'));
  }

  function addKey() {
    const n = Number(addSelect.value);
    changeCorrections((c) => {
      if (!c.keys.some((k) => k.note === n)) {
        c.keys.push({ note: n, name: noteName(n), speed: 0, press_ms: 0, release_ms: 0 });
        c.keys.sort((a, b) => a.note - b.note);
      }
    });
  }

  return {
    unmount() {
      alive = false;
      off();
      if (corrState.timer) saveCorrections();
    },
  };
}

// The Key check's suggested corrections, for a recording made here.
function suggestionsCard(r) {
  const none = (r.check && r.check.no_reading) || [];
  const boxes = [];
  const card = h('section', { class: 'card' },
    h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Suggested corrections')));
  loadCorrections(false).then((c) => {
    const items = r.suggestions.map((s) => {
      const box = h('input', { type: 'checkbox', checked: true });
      boxes.push({ box, s });
      const what = [s.speed && `loudness ${s.speed > 0 ? '+' : MINUS}${Math.abs(s.speed)}% (${speedWords(s.speed)})`,
        s.press_ms && `press ${s.press_ms > 0 ? '+' : MINUS}${Math.abs(s.press_ms)} ms (${releaseWords(s.press_ms)})`,
        s.release_ms && `release ${s.release_ms > 0 ? '+' : MINUS}${Math.abs(s.release_ms)} ms (${releaseWords(s.release_ms)})`].filter(Boolean).join(', ');
      const cur = c.keys.find((k) => k.note === s.note);
      return h('label', { class: 'corr-check' }, box, h('span', null, h('strong', null, `${s.name}: ${what}. `), `It ${s.why}.`,
        cur ? ` Now: loudness ${signedNum(cur.speed)}%, press ${signedNum(cur.press_ms || 0)} ms, release ${signedNum(cur.release_ms)} ms.` : ''));
    });
    let fillBox = null;
    if (none.length && !c.fill_zero) {
      fillBox = h('input', { type: 'checkbox' });
      items.push(h('label', { class: 'corr-check' }, fillBox, h('span', null, h('strong', null, 'Fill in notes with no hammer reading. '),
        `${none.reduce((a, k) => a + k.count, 0)} here. They play as silent presses; tick this only if those notes sounded when you played them.`)));
    }
    if (!items.length) {
      card.append(h('p', null, 'No key stands out enough to suggest a correction.'), h('a', { class: 'btn btn-small', href: '#/record/corrections' }, 'Corrections'));
      return;
    }
    const useBtn = h('button', { class: 'btn btn-primary btn-small', type: 'button', onclick: use }, 'Use the checked ones');
    card.append(
      h('p', { class: 'muted small' }, 'Corrections that would bring the keys that stand out back in line with their neighbours. A suggestion replaces the key’s current correction. A key heard only two or three times is thin evidence: record it several times first.'),
      h('div', { class: 'corr-suggestions' }, items),
      h('div', { class: 'btn-row' }, useBtn, h('a', { class: 'btn btn-small', href: '#/record/corrections' }, 'Corrections')));

    function use() {
      const chosen = boxes.filter((b) => b.box.checked).map((b) => b.s);
      const fillZero = !!(fillBox && fillBox.checked);
      if (!chosen.length && !fillZero) { toast('Nothing is checked.'); return; }
      changeCorrections((cc) => {
        for (const s of chosen) {
          const k = cc.keys.find((q) => q.note === s.note);
          if (k) { k.speed = s.speed; k.press_ms = s.press_ms || 0; k.release_ms = s.release_ms; } else cc.keys.push({ note: s.note, name: s.name, speed: s.speed, press_ms: s.press_ms || 0, release_ms: s.release_ms });
        }
        cc.keys.sort((a, b) => a.note - b.note);
        if (fillZero) cc.fill_zero = true;
        cc.on = true;
      });
      saveCorrections().then(() => toast('Corrections saved. Recordings made here play with them.'));
      useBtn.disabled = true;
    }
  }, (err) => card.append(h('p', { class: 'message message-error' }, err.message)));
  return card;
}

// On a recording's page: how the corrections apply to it.
function correctionsCard(p) {
  const body = h('div', null, h('p', { class: 'muted small' }, 'Loading…'));
  const card = h('section', { class: 'card' },
    h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Recording corrections')), body);
  if (p.corrected) {
    fill(body, h('p', { class: 'muted small' }, 'The corrections are in this copy, so they are not applied again when it plays.'),
      h('a', { class: 'btn btn-small', href: '#/record/corrections' }, 'Corrections'));
    return card;
  }
  loadCorrections(false).then((c) => {
    const empty = !c.fill_zero && !c.keys.some((k) => k.speed || k.press_ms || k.release_ms);
    const copyBtn = h('button', { class: 'btn btn-small', type: 'button', disabled: empty, onclick: copy }, 'Save a corrected copy');
    fill(body,
      h('p', { class: 'muted small' }, !c.on ? 'Corrections are off: it plays as recorded.'
        : c.active ? `It plays corrected: ${c.summary}.` : 'No corrections yet: it plays as recorded.'),
      h('div', { class: 'btn-row' }, copyBtn, h('a', { class: 'btn btn-small', href: '#/record/corrections' }, 'Corrections')));
    async function copy() {
      copyBtn.disabled = true;
      try {
        const r = await api('POST', piecePath(p.id) + '/corrected');
        toast(r.duplicate ? `The library already holds it: “${r.piece.title}”.` : `Saved “${r.piece.title}” (${r.changed} notes corrected).`);
        location.hash = pieceHref(r.piece.id);
      } catch (err) {
        toast(err.message, 'error');
        copyBtn.disabled = false;
      }
    }
  }, (err) => fill(body, h('p', { class: 'message message-error' }, err.message)));
  return card;
}

// ================================================================= Sensor test

// A guided recording for checking the key sensors: one for loudness, one
// for releases, each opening its Key check once saved; and several such
// recordings checked together, for firmer suggestions.

// recordingsChecklist lists the recordings made on this piano with a box
// each (those whose title matches `preselect` ticked) and a button that
// opens the Key check of the ticked ones together.
function recordingsChecklist(preselect) {
  const el = h('div', { class: 'rec-checklist' }, h('p', { class: 'muted small' }, 'Loading…'));
  getAllPieces().then((pieces) => {
    const recs = pieces.filter((p) => p.origin === 'recorded').sort((a, b) => cmpText(b.added_at, a.added_at)).slice(0, 30);
    if (!recs.length) {
      fill(el, h('p', { class: 'muted small' }, 'No recordings yet.'));
      return;
    }
    const boxes = recs.map((p) => ({ p, box: h('input', { type: 'checkbox', checked: !!(preselect && preselect.test(p.title || '')) }) }));
    const go = h('button', { class: 'btn btn-small btn-primary', type: 'button', onclick: () => {
      const ids = boxes.filter((b) => b.box.checked).map((b) => b.p.id);
      if (!ids.length) { toast('Tick the recordings to check.'); return; }
      location.hash = ids.length === 1 ? pieceKeysHref(ids[0]) : '#/keys/' + ids.map(encodeURIComponent).join(',');
    } }, 'Check the ticked ones together');
    fill(el,
      h('div', { class: 'rec-check-list' }, boxes.map(({ p, box }) => h('label', { class: 'corr-check' }, box,
        h('span', null, h('a', { href: pieceKeysHref(p.id) }, p.title || 'Untitled'), h('span', { class: 'muted small' }, ` · ${fmtDateTime(p.added_at)} · ${fmtTime(p.duration_ms)}`))))),
      h('div', { class: 'btn-row' }, go));
  }, () => fill(el, h('p', { class: 'muted small' }, 'Could not load the library.')));
  return el;
}

const SENSOR_TESTS = [
  {
    title: 'Loudness by key',
    name: 'Sensor test: loudness',
    does: 'Finds the keys whose hammer sensor reads too fast or too slow, so they record too loud or too soft.',
    steps: [
      'Tap “Get ready” below; the Record screen opens, ready.',
      'Tap the round Start button.',
      'From the lowest key to the highest, play each key four times at the same medium loudness (mezzo-forte), about one a second. Black keys too. Keep the pedals up.',
      'Tap Stop and save. The Key check opens by itself.',
    ],
  },
  {
    title: 'Release timing',
    name: 'Sensor test: releases',
    does: 'Finds the keys whose release sensor fires early or late, so they let go before or after the rest of a chord.',
    steps: [
      'Tap “Get ready” below; the Record screen opens, ready.',
      'Tap the round Start button.',
      'Play chords of three or four keys (triads are fine), hold each about a second, and let go of all the keys at once. Play each chord three times, then move up the keyboard, until every key has been in chords. Keep the pedals up.',
      'Tap Stop and save. The Key check opens by itself.',
    ],
  },
];

function sensorTestScreen(root) {
  async function getReady(t, btn) {
    btn.disabled = true;
    const title = `${t.name}, ${new Date().toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })}`;
    try {
      const reply = await api('POST', '/record/arm', { title });
      if (isStatus(reply)) applyReplyStatus(reply);
      app.rec.saved = null;
      app.rec.title = title;
      app.rec.thenKeys = true; // open the Key check once it is saved
      location.hash = '#/record';
    } catch (err) {
      toast(err.message, 'error');
      btn.disabled = false;
    }
  }
  fill(root,
    h('h1', { class: 'screen-title' }, 'Sensor test'),
    h('p', { class: 'lead' }, 'Two short recordings that show which keys’ sensors read wrong. Do them when the piano can make noise: the piano itself only listens, it does not play.'),
    SENSOR_TESTS.map((t) => {
      const btn = h('button', { class: 'btn btn-primary', type: 'button', onclick: () => getReady(t, btn) }, 'Get ready');
      return h('section', { class: 'card' },
        h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, t.title)),
        h('p', { class: 'muted small' }, t.does),
        h('ol', { class: 'test-steps' }, t.steps.map((s) => h('li', null, s))),
        btn);
    }),
    h('section', { class: 'card' },
      h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Check several together')),
      h('p', { class: 'muted small' }, 'Two or three takes of each test, checked together, give firmer suggestions than one. The sensor tests are ticked.'),
      recordingsChecklist(/^Sensor test/)),
    h('p', { class: 'hint' }, 'The Key check then suggests ', h('a', { href: '#/record/corrections' }, 'corrections'), ' for the keys that stand out.'));
  return {};
}

// ======================================================================= Tools

const RISK_WORDS = { reads: 'Reads the sensors', moves: 'Moves the piano', writes: 'Changes settings', erases: 'Erases calibration' };
const KEY_LABELS = { space: 'Space', enter: 'Enter', esc: 'Esc', backspace: 'Backspace', up: 'Up', down: 'Down', left: 'Left', right: 'Right' };

let toolsCache = null;
let toolGroups = [];
async function loadTools(force) {
  if (!toolsCache || force) {
    const r = await api('GET', '/tools');
    toolsCache = r.tools || [];
    toolGroups = r.groups || [];
  }
  return toolsCache;
}

function toolHref(name, arg) { return `#/piano/tool/${name}` + (arg ? '/' + encodeURIComponent(arg) : ''); }

function toolsCard() {
  const list = h('div', { class: 'tool-list' }, h('p', { class: 'muted small' }, 'Loading…'));
  loadTools(false).then((tools) => {
    const groups = toolGroups.length ? toolGroups : [...new Set(tools.map((t) => t.group))];
    fill(list, groups.map((g) => {
      const ts = tools.filter((t) => t.group === g);
      if (!ts.length) return null;
      return h('div', { class: 'tool-group' },
        h('p', { class: 'label' }, g),
        ts.map((t) => h('a', { class: 'tool-row', href: toolHref(t.name) },
          h('span', { class: 'tool-title' }, t.title),
          h('span', { class: `pill ${t.risk === 'writes' ? 'pill-warn' : t.risk === 'erases' ? 'pill-danger' : ''}` }, RISK_WORDS[t.risk] || t.risk),
          !t.present && h('span', { class: 'pill pill-off' }, 'Not installed'))));
    }));
  }, (err) => fill(list, h('p', { class: 'message message-error' }, err.message)));
  return h('section', { class: 'card' },
    h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Tools')),
    h('p', { class: 'muted small' }, 'Every one of Stahnke’s programs: sensor tests, the dampers, the players, the calibrators, restoring a backup, his editor and the tape programs. They run on the piano while it is idle.'),
    list);
}

// A small search over the library, for a tool that works on a piece.
function piecePicker(onPick, filter) {
  const input = h('input', { class: 'input', type: 'search', placeholder: 'Search the library', 'aria-label': 'Search for a piece', autocomplete: 'off' });
  const results = h('div', { class: 'pick-results' });
  let pieces = [];
  getAllPieces().then((ps) => { pieces = filter ? ps.filter(filter) : ps; show(); }, () => {});
  input.addEventListener('input', debounce(show, 150));
  function show() {
    const q = fold(input.value.trim());
    const hits = pieces.filter((p) => !q || fold([p.title, p.composer, p.performer, p.source_name].join(' ')).includes(q)).slice(0, 8);
    fill(results, hits.length
      ? hits.map((p) => h('button', { class: 'pick-row', type: 'button', onclick: () => onPick(p) },
        h('span', { class: 'pick-title' }, p.title || 'Untitled'), h('span', { class: 'muted small' }, [byline(p), p.source_name].filter(Boolean).join(' · '))))
      : h('p', { class: 'muted small' }, pieces.length ? 'No match.' : 'No pieces to choose from.'));
  }
  return h('div', { class: 'piece-picker' }, input, results);
}

function toolScreen(root, [name, arg]) {
  let alive = true;
  let tool = null;
  const choice = { backup: '', piece: null, file: '' };
  const consoleEl = h('pre', { class: 'console', tabindex: '0', 'aria-label': 'What the program prints' });
  const stateEl = h('p', { class: 'tool-state' });
  const startBtn = h('button', { class: 'btn btn-primary', type: 'button', onclick: start }, icon('play'), 'Start');
  const stopBtn = h('button', { class: 'btn btn-danger', type: 'button', onclick: stop }, icon('stop'), 'Stop');
  const keysEl = h('div', { class: 'btn-row tool-keys' });
  const textInput = h('input', { class: 'input', type: 'text', autocomplete: 'off', autocapitalize: 'characters', 'aria-label': 'Answer or command to type', maxlength: '60' });
  const textForm = h('form', { class: 'tool-text', hidden: true, onsubmit: (e) => { e.preventDefault(); sendText(); } },
    textInput, h('button', { class: 'btn', type: 'submit' }, 'Send'));
  const needsEl = h('div', { class: 'tool-needs' });
  const phraseInput = h('input', { class: 'input tool-phrase', type: 'text', autocomplete: 'off', autocapitalize: 'characters', 'aria-label': 'Confirmation word' });
  let lines = [];
  root.append(h('p', { class: 'muted center' }, 'Loading…'));

  loadTools(false).then((tools) => {
    if (!alive) return;
    tool = tools.find((t) => t.name === name);
    if (!tool) {
      fill(root, h('p', { class: 'muted center' }, 'No such tool.'));
      return;
    }
    if (arg && tool.needs === 'backup') choice.backup = arg;
    render();
    if (arg && (tool.needs === 'piece' || tool.needs === 'midi')) {
      api('GET', piecePath(arg)).then((p) => { if (alive) { choice.piece = p; renderNeeds(); update(); } }, () => {});
    }
  }, (err) => { if (alive) fill(root, h('div', { class: 'message message-error' }, h('p', null, err.message))); });

  function running() {
    const s = app.status;
    return !!(s && s.state === 'maintenance' && tool && s.program && tool.banner && s.program === tool.banner);
  }

  function render() {
    document.title = `${tool.title} · Piano`;
    fill(keysEl, tool.keys.map((k) => h('button', { class: 'btn', type: 'button', onclick: () => sendKey(k) }, KEY_LABELS[k] || k)));
    fill(root,
      h('h1', { class: 'screen-title' }, tool.title),
      h('p', { class: 'piano-sub' }, `${tool.file} · ${tool.group} · `, RISK_WORDS[tool.risk] || tool.risk),
      h('p', null, tool.does),
      tool.warning && h('div', { class: `message ${tool.risk === 'erases' ? 'message-error' : 'message-warn'}` }, h('p', null, tool.warning)),
      !tool.present && h('div', { class: 'message message-error' }, h('p', null, `${tool.file} is not installed. Install it on the Status page.`)),
      tool.name === 'edit' && h('p', { class: 'hint' }, 'The ', h('a', { href: '#/piano' }, 'piano-roll editor'), ' (a piece’s page → Open in the editor) does most of this more easily.'),
      needsEl,
      h('section', { class: 'card' },
        stateEl,
        tool.phrase && h('label', { class: 'tool-phrase-label' }, `Type ${tool.phrase} to confirm: `, phraseInput),
        h('div', { class: 'btn-row' }, startBtn, stopBtn),
        keysEl,
        textForm,
        consoleEl,
        h('p', { class: 'hint' }, tool.keys.length ? 'The program’s keys are the buttons above. Stop ends it at once; quitting with its own key is gentler.' : 'This program needs no keys: it runs to its end by itself.')),
      tool.help && h('section', { class: 'card' },
        h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Commands')),
        h('p', { class: 'muted small' }, 'Type a command in the box above and press Send. No spaces between a command and its value.'),
        h('table', { class: 'piano-table tool-help' },
          h('tbody', null, tool.help.map(([cmd, what]) => h('tr', null, h('td', null, h('code', null, cmd)), h('td', null, what)))))));
    phraseInput.addEventListener('input', update);
    renderNeeds();
    update();
  }

  // What the program needs before it starts: a backup, a piece or a file.
  function renderNeeds() {
    const need = tool.needs;
    if (!need) { fill(needsEl); return; }
    const card = h('section', { class: 'card' });
    fill(needsEl, card);
    if (need === 'backup') {
      card.append(h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Backup to restore')), h('p', { class: 'muted small' }, 'Loading…'));
      api('GET', '/piano/backups').then((r) => {
        if (!alive) return;
        const newest = (r.backups || [])[0];
        const backups = (r.backups || []).filter((b) => b.valid);
        if (!choice.backup && backups.length) choice.backup = backups[0].id;
        const diffEl = h('div', { class: 'restore-diff' });
        const sel = h('select', { class: 'input', 'aria-label': 'Backup', onchange: () => { choice.backup = sel.value; update(); showDiff(); } },
          backups.map((b) => h('option', { value: b.id, selected: b.id === choice.backup }, `${fmtDateTime(b.time)} · ${reasonText(b.reason)} · ${b.model}`)));
        fill(card, h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Backup to restore')),
          backups.length ? sel : h('p', { class: 'muted' }, 'No backups yet.'),
          diffEl,
          h('p', { class: 'hint' }, 'Only a whole, valid table for a model this piano has had can be restored. Compare any two backups on the ', h('a', { href: '#/piano/backups' }, 'Backups'), ' screen.'));
        update();
        showDiff();

        // showDiff says what restoring the chosen backup would change,
        // against the newest backup (the piano as last read).
        async function showDiff() {
          const id = choice.backup;
          if (!id || !newest) { fill(diffEl); return; }
          const when = `the newest backup (${fmtDateTime(newest.time)}, ${reasonText(newest.reason).toLowerCase()})`;
          if (id === newest.id) {
            fill(diffEl, h('p', { class: 'muted small' }, `This is ${when}, so restoring it changes nothing unless the settings changed since.`));
            return;
          }
          fill(diffEl, h('p', { class: 'muted small' }, 'Comparing with the newest backup…'));
          try {
            const c = await api('GET', `/piano/compare?from=${encodeURIComponent(newest.id)}&to=${encodeURIComponent(id)}`);
            if (!alive || choice.backup !== id) return;
            const d = c.diff;
            if (!d.bytes) {
              fill(diffEl, h('p', { class: 'muted small' }, `The same as ${when}: restoring it changes nothing.`));
              return;
            }
            const shown = d.changes.slice(0, 8);
            const more = d.changes.length - shown.length;
            fill(diffEl,
              h('p', { class: 'small' }, `Restoring it would make ${changeCount(d)} from ${when}:`),
              shown.length > 0 && h('ul', { class: 'restore-changes small' }, shown.map((x) => h('li', null,
                [x.key, x.what].filter(Boolean).join(' '), `: ${x.from} → ${x.to}`))),
              (more > 0 || d.other > 0) && h('p', { class: 'muted small' },
                [more > 0 && `and ${more} more`, d.other > 0 && `${d.other} other bytes`].filter(Boolean).join('; ') + ' (the Backups screen shows them all).'));
          } catch (err) {
            if (alive) fill(diffEl, h('p', { class: 'muted small' }, 'Could not compare: ' + err.message));
          }
        }
      }, (err) => fill(card, h('p', { class: 'message message-error' }, err.message)));
      return;
    }
    const title = need === 'file' ? 'File to play' : need === 'midi' ? 'MIDI file to play' : 'Piece';
    card.append(h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, title)));
    if (choice.piece) {
      card.append(h('p', null, h('strong', null, choice.piece.title || 'Untitled'), ' ',
        h('button', { class: 'btn btn-small', type: 'button', onclick: () => { choice.piece = null; renderNeeds(); update(); } }, 'Change')));
    } else if (choice.file) {
      card.append(h('p', null, h('strong', null, choice.file), ' ',
        h('button', { class: 'btn btn-small', type: 'button', onclick: () => { choice.file = ''; renderNeeds(); update(); } }, 'Change')));
    } else {
      if (need === 'piece' || need === 'midi') {
        if (need === 'midi') card.append(h('p', { class: 'muted small' }, 'A piece converted from MIDI keeps its original file:'));
        card.append(piecePicker((p) => { choice.piece = p; renderNeeds(); update(); }, need === 'midi' ? (p) => p.has_midi_source : null));
      }
      if (need === 'file' || need === 'midi') {
        const fileInput = h('input', { type: 'file', accept: tool.accept || '', 'aria-label': 'File to upload' });
        fileInput.addEventListener('change', async () => {
          const f = fileInput.files && fileInput.files[0];
          if (!f) return;
          try {
            const r = await api('POST', '/tools/files?name=' + encodeURIComponent(f.name), f, { contentType: 'application/octet-stream' });
            choice.file = r.file;
            renderNeeds();
            update();
          } catch (err) {
            toast(err.message, 'error');
          }
        });
        card.append(h('p', { class: 'muted small' }, need === 'midi' ? 'Or upload a MIDI file:' : 'Upload the file:'), fileInput);
      }
    }
  }

  function ready() {
    if (!tool.needs) return true;
    if (tool.needs === 'backup') return !!choice.backup;
    if (tool.needs === 'piece') return !!choice.piece;
    return !!(choice.piece || choice.file);
  }

  function update() {
    if (!tool) return;
    const on = running();
    const s = app.status;
    let words = 'Not running.';
    if (on) words = 'Running on the piano.';
    else if (s && s.state === 'maintenance') words = `The piano is busy with ${s.program || 'another program'}.`;
    else if (s && s.state !== 'idle') words = `The piano is ${stateWords(s).toLowerCase()}; the tool starts once it is idle.`;
    else if (!ready()) words = 'Choose what it works on, above, to start.';
    setText(stateEl, words);
    startBtn.hidden = on;
    const phraseOk = !tool.phrase || phraseInput.value.trim().toUpperCase() === tool.phrase;
    startBtn.disabled = !tool.present || !(s && s.state === 'idle') || !ready() || !phraseOk;
    stopBtn.hidden = !on;
    for (const b of keysEl.children) b.disabled = !on;
    textForm.hidden = !tool.input || !on;
  }

  function addLine(line) {
    lines.push(line);
    if (lines.length > 400) lines = lines.slice(-400);
    setText(consoleEl, lines.join('\n'));
    consoleEl.scrollTop = consoleEl.scrollHeight;
  }

  async function start() {
    const writes = tool.risk === 'writes' || tool.risk === 'erases';
    if (writes) {
      const ok = await confirmDialog({ title: `Start ${tool.title}?`, text: tool.warning, ok: 'Back up and start', danger: true });
      if (!ok) return;
    }
    startBtn.disabled = true;
    lines = [];
    setText(consoleEl, writes ? 'Backing up the piano’s settings first…' : 'Starting…');
    const body = { confirm: writes, phrase: phraseInput.value };
    if (tool.needs === 'backup') body.backup = choice.backup;
    if (choice.piece) body.piece = choice.piece.id;
    if (choice.file) body.file = choice.file;
    try {
      const r = await api('POST', `/tools/${tool.name}/start`, body);
      if (r && r.status && isStatus(r.status)) applyReplyStatus(r.status);
      if (r && r.backup) { pianoCache.view = null; toast(`Settings backed up (${fmtDateTime(r.backup.time)}).`); }
    } catch (err) {
      toast(err.message, 'error');
      setText(consoleEl, '');
    }
    update();
  }

  async function stop() {
    try {
      await api('POST', '/tools/stop');
    } catch (err) {
      toast(err.message, 'error');
    }
  }

  async function sendKey(k) {
    try {
      await api('POST', '/tools/key', { key: k });
    } catch (err) {
      toast(err.message, 'error');
    }
  }

  async function sendText() {
    const text = textInput.value.trim();
    if (!text) return;
    try {
      await api('POST', '/tools/key', { text });
      addLine('> ' + text);
      textInput.value = '';
    } catch (err) {
      toast(err.message, 'error');
    }
  }

  return {
    update,
    onConsole(line) { if (running() || (app.status && app.status.state === 'maintenance')) addLine(line); },
    unmount() { alive = false; },
  };
}

// ====================================================================== Status

// ====================================================================== Backups

// The Status page's Backups card (docs/BACKUPS.md): one file with
// everything on the Pi, when the last one was made, and restoring from one.
function backupCard() {
  const lastEl = h('div', { class: 'backup-last' }, 'Checking…');
  const report = h('div', { class: 'backup-report' });
  const fileInput = h('input', { type: 'file', accept: '.zip,application/zip', hidden: true, 'aria-label': 'Backup file to restore from' });
  const restoreBtn = h('button', { class: 'btn', type: 'button', onclick: () => fileInput.click() }, icon('upload'), 'Restore from a backup…');
  fileInput.addEventListener('change', () => { const f = fileInput.files && fileInput.files[0]; fileInput.value = ''; if (f) restore(f); });
  const card = h('section', { class: 'card' },
    h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Backups')),
    lastEl,
    h('div', { class: 'btn-col' },
      h('a', { class: 'btn btn-primary', href: API + '/backup', download: '', onclick: () => setTimeout(refresh, 4000) }, icon('download'), 'Back up now'),
      restoreBtn, fileInput),
    h('p', { class: 'hint' }, 'Back up now saves one .zip file with everything on the Pi (every piece and its details, play lists, settings, voicing, corrections, the piano’s settings backups, and the installed programs) to the device you are using. On a computer, choose a folder in Google Drive to keep it in the cloud too. The PC’s automatic backup (every 3 days) goes to Google Drive by itself.'),
    h('p', { class: 'hint' }, 'Restoring adds what a backup has and this Pi lacks. Nothing here is changed or removed.'),
    report);
  refresh();
  return card;

  async function refresh() {
    try {
      const s = await api('GET', '/backup/status');
      const size = s.data_bytes ? ` Everything is about ${fmtBytes(s.data_bytes)}.` : '';
      if (!s.last) {
        lastEl.className = 'backup-last message message-warn';
        fill(lastEl, h('p', null, 'No full backup yet.' + size + ' The only copies of recordings made here are on the Pi’s memory card.'));
        return;
      }
      const old = s.age_days >= 7;
      lastEl.className = 'backup-last ' + (old ? 'message message-warn' : 'muted small');
      const by = s.by === 'pc' ? ' by the PC’s automatic backup' : ' from the app';
      fill(lastEl, h('p', null, `Last full backup: ${fmtDateTime(s.last)}${by}, ${s.pieces} pieces.` + (old ? ` That is ${s.age_days} days ago: time for another.` : '') + size));
    } catch (err) {
      fill(lastEl, err.message);
    }
  }

  async function restore(file) {
    const ok = await confirmDialog({
      title: 'Restore from this backup?',
      text: `“${file.name}”: what it has and this Pi lacks is added. Nothing here is changed or removed.`,
      ok: 'Restore',
    });
    if (!ok) return;
    restoreBtn.disabled = true;
    fill(report, h('p', { class: 'muted' }, 'Restoring…'));
    try {
      const form = new FormData();
      form.append('file', file);
      const res = await fetch(API + '/backup/restore', { method: 'POST', body: form });
      const r = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(r.error || `HTTP ${res.status}`);
      const added = [
        r.pieces && `${r.pieces} piece${r.pieces === 1 ? '' : 's'}`,
        r.playlists && `${r.playlists} play list${r.playlists === 1 ? '' : 's'}`,
        r.backups && `${r.backups} piano settings backup${r.backups === 1 ? '' : 's'}`,
        r.programs && `${r.programs} program${r.programs === 1 ? '' : 's'}`,
        ...(r.restored || []),
      ].filter(Boolean);
      fill(report,
        h('div', { class: r.errors && r.errors.length ? 'message message-warn' : 'message message-info' },
          h('p', null, `From the backup of ${fmtDateTime(r.created)}: ` + (added.length ? `added ${added.join(', ')}.` : 'nothing was missing here.') +
            (r.kept ? ` ${r.kept} already here, left as they were.` : '')),
          r.errors && r.errors.length > 0 && h('ul', { class: 'small' }, r.errors.map((e) => h('li', null, e)))));
      if (r.pieces || r.playlists) app.piecesStale = true;
    } catch (err) {
      fill(report, h('p', { class: 'message message-error' }, err.message));
    } finally {
      restoreBtn.disabled = false;
    }
  }
}

function statusScreen(root) {
  let alive = true;
  let inst = null;
  let instError = '';
  let seeded = false;
  let connKey = '';
  let progKey = '';
  let toolList = null; // the tools (the suite's other programs), installed here too
  loadTools(true).then((t) => { toolList = t; if (alive) renderPrograms(); }, () => {}); // runs once the screen is built
  let levelBusy = false;
  let simBusy = false;
  let consoleDrawn = false;
  const installing = new Set();

  const connCard = h('section', { class: 'card' });

  const simOn = h('button', { type: 'button', onclick: () => simPower(true) }, 'On');
  const simOff = h('button', { type: 'button', onclick: () => simPower(false) }, 'Off');
  const simCard = h('section', { class: 'card', hidden: true },
    h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Simulated piano'), h('span', { class: 'badge' }, 'Simulator')),
    h('p', { class: 'muted small' }, 'The service is talking to its built-in simulator, not the real piano. Switch its power to try going offline and back.'),
    h('div', { class: 'segmented', role: 'group', 'aria-label': 'Simulated power' }, simOn, simOff));

  const progBody = h('div');
  const progCard = h('section', { class: 'card' },
    h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Programs')),
    h('p', { class: 'muted small' }, 'Stahnke’s play.exe, record.exe and getlut.exe (which reads the piano’s settings) run on the piano. They are not part of this software: install your own copies.'),
    progBody);

  const consoleEl = h('pre', { class: 'console', tabindex: '0', 'aria-label': 'Piano console output' });
  const consoleCard = h('section', { class: 'card' },
    h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Console')),
    consoleEl);

  const prefSel = h('select', { class: 'input', id: 'set-pref', onchange: () => saveSettings({ preferred_instrument: prefSel.value }) });
  const defLevel = levelStepper(stepDefault, 'Default level');
  const defMethod = levelMethodToggle();
  const settingsCard = h('section', { class: 'card' },
    h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Settings')),
    h('div', { class: 'field' },
      h('label', { for: 'set-pref' }, 'Preferred instrument'),
      h('div', { class: 'select-wrap' }, prefSel),
      h('p', { class: 'hint' }, 'When a performance has several renderings, the library shows this one.')),
    defLevel.el,
    h('p', { class: 'level-hint' }, 'The level each piece starts at.'),
    defMethod.el,
    h('p', { class: 'hint' }, 'On the Pi: the Pi makes the notes softer as it sends them, and the piano’s own level command stays at 0; a change is heard after a few seconds. Piano’s own: the piano’s level command, heard at once (other owners report it overheating the piano). Try both and compare.'));

  const rescanBtn = h('button', { class: 'btn', type: 'button', onclick: rescan }, icon('refresh'), 'Rescan library folder');
  const libCard = h('section', { class: 'card' },
    h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Library')),
    h('div', { class: 'btn-col' }, rescanBtn),
    h('p', { class: 'hint' }, 'Rescan after copying files into the library folder by hand.'));

  root.append(h('h1', { class: 'screen-title' }, 'Status'), connCard, simCard, progCard, consoleCard, settingsCard, backupCard(), libCard);

  renderConn();
  renderSim();
  renderPrograms();
  renderConsole();
  fillPref();
  renderDefLevel();
  refresh();
  const timer = setInterval(() => { if (!document.hidden) refresh(); }, 4000);

  async function refresh() {
    try {
      const data = await api('GET', '/instrument');
      if (!alive) return;
      inst = data || {};
      instError = '';
      if (!seeded) {
        seeded = true;
        if (Array.isArray(inst.console)) {
          app.consoleLines = inst.console.slice(-CONSOLE_MAX);
          renderConsole();
        }
      }
    } catch (err) {
      if (!alive) return;
      instError = `Could not read the connection details: ${err.message}`;
    }
    renderConn();
    renderPrograms();
  }

  function renderConn() {
    const s = app.status;
    const online = s ? !!s.online : !!(inst && inst.online);
    const simulated = !!((s && s.simulated) || (inst && inst.simulated));
    const link = (inst && inst.link) || {};
    const rows = [
      ['State', s ? stateWords(s) : ''],
      ['Message', s && s.message],
      ['Running', s && s.program, 'mono'],
      ['Port', inst && inst.port, 'mono'],
      ['Speed', inst && inst.baud ? `${inst.baud} baud` : ''],
      ['Connected since', fmtDateTime(link.connected_since)],
      ['Received', Number.isFinite(link.rx_bytes) ? fmtBytes(link.rx_bytes) : ''],
      ['Sent', Number.isFinite(link.tx_bytes) ? fmtBytes(link.tx_bytes) : ''],
      ['Retransmits', link.retransmits],
      ['NAKs', link.naks],
      ['Bad frames', link.bad_frames],
      ['Last error', link.last_error],
      ['Service version', inst && inst.version, 'mono'],
    ];
    const key = JSON.stringify([online, simulated, rows, instError]);
    if (key === connKey) return;
    connKey = key;
    fill(connCard,
      h('div', { class: 'card-head' },
        h('h2', { class: 'card-title' }, 'Piano'),
        h('span', { class: `pill ${online ? 'pill-ok' : 'pill-off'}` }, online ? 'Online' : 'Offline')),
      metaList(rows),
      instError && h('p', { class: 'message message-error' }, instError));
  }

  function renderSim() {
    const s = app.status;
    const sim = !!(s && s.simulated);
    simCard.hidden = !sim;
    if (!sim) return;
    const on = s.state !== 'offline'; // the API has no power reading; infer it
    simOn.setAttribute('aria-pressed', String(on));
    simOff.setAttribute('aria-pressed', String(!on));
    simOn.disabled = simBusy;
    simOff.disabled = simBusy;
  }

  async function simPower(on) {
    if (simBusy) return;
    simBusy = true;
    renderSim();
    await command('/sim/power', { on });
    simBusy = false;
    if (alive) renderSim();
  }


  function renderPrograms() {
    const progs = (inst && inst.programs) || {};
    const key = JSON.stringify([!!inst, progs, [...installing], toolList && toolList.map((t) => [t.present, t.banner])]);
    if (key === progKey) return;
    progKey = key;
    const open = !!progBody.querySelector('details[open]');
    fill(progBody, programRow('play', progs.play), programRow('record', progs.record), programRow('getlut', progs.getlut),
      toolList && h('details', { class: 'prog-more', open },
        h('summary', null, `The other ${toolList.length} programs (${toolList.filter((t) => t.present).length} installed)`),
        toolList.map((t) => programRow(t.name, { present: t.present, banner: t.banner, error: t.error }, t.file))));
  }

  function programRow(name, prog, fileName) {
    const file = fileName || `${name}.exe`;
    const fid = `prog-${name}`;
    const busyNow = installing.has(name);
    // No `accept` filter: iOS greys out extensions it does not know.
    const input = h('input', { class: 'visually-hidden', id: fid, type: 'file', disabled: busyNow, onchange: () => install(name, input) });
    let pill;
    if (!inst) pill = h('span', { class: 'pill' }, 'Checking…');
    else if (prog && prog.present) pill = h('span', { class: 'pill pill-ok' }, 'Installed');
    else pill = h('span', { class: 'pill pill-off' }, 'Missing');
    let label = `Install ${file}`;
    if (busyNow) label = 'Installing…';
    else if (prog && prog.present) label = `Replace ${file}`;
    return h('div', { class: 'prog' },
      h('div', { class: 'prog-head' }, h('span', { class: 'prog-name' }, file), pill),
      prog && prog.banner && h('p', { class: 'prog-banner' }, prog.banner),
      prog && prog.error && h('p', { class: 'message message-error' }, prog.error),
      input,
      h('label', { class: 'btn btn-small', for: fid, 'aria-disabled': busyNow ? 'true' : null }, icon('upload'), label));
  }

  async function install(name, input) {
    const f = input.files && input.files[0];
    if (!f || installing.has(name)) return;
    installing.add(name);
    renderPrograms();
    try {
      const prog = await api('PUT', `/programs/${name}`, f, { contentType: 'application/octet-stream' });
      toast(prog && prog.banner ? `${name}.exe installed: ${prog.banner}` : `${name}.exe installed.`);
      if (inst && prog && ['play', 'record', 'getlut'].includes(name)) inst.programs = { ...(inst.programs || {}), [name]: prog };
      const tool = toolList && toolList.find((t) => t.name === name);
      if (tool && prog) { tool.present = prog.present; tool.banner = prog.banner; tool.error = prog.error; }
    } catch (err) {
      toast(err.message, 'error');
    } finally {
      installing.delete(name);
      input.value = '';
      if (alive) {
        renderPrograms();
        refresh();
      }
    }
  }

  function renderConsole() {
    const nearBottom = consoleEl.scrollHeight - consoleEl.scrollTop - consoleEl.clientHeight < 40;
    consoleEl.textContent = app.consoleLines.join('\n');
    if (nearBottom || !consoleDrawn) consoleEl.scrollTop = consoleEl.scrollHeight;
    consoleDrawn = true;
  }

  async function fillPref() {
    let list = [];
    try { list = uniqSorted((await getAllPieces()).map((p) => p.instrument)); } catch { /* keep empty */ }
    if (!alive) return;
    const cur = (app.settings && app.settings.preferred_instrument) || '';
    if (cur && !list.includes(cur)) list.unshift(cur);
    const opts = list.map((v) => h('option', { value: v }, instrumentLabel(v)));
    if (!list.length) opts.push(h('option', { value: '' }, 'No instruments in the library yet'));
    else if (!cur) opts.unshift(h('option', { value: '', disabled: true }, 'Choose an instrument'));
    fill(prefSel, ...opts);
    prefSel.value = cur;
    prefSel.disabled = !list.length;
  }

  function defaultLevel() {
    if (app.settings && Number.isInteger(app.settings.level)) return app.settings.level;
    return app.status ? app.status.target_level : undefined;
  }

  function renderDefLevel() {
    defMethod.render();
    defLevel.render(defaultLevel(), app.status ? app.status.min_level : null, levelBusy);
  }

  async function stepDefault(delta) {
    const cur = defaultLevel();
    if (!Number.isInteger(cur) || levelBusy) return;
    levelBusy = true;
    renderDefLevel();
    await saveSettings({ level: Math.min(0, cur + delta) }, true);
    levelBusy = false;
    if (alive) renderDefLevel();
  }

  async function rescan() {
    rescanBtn.disabled = true;
    try {
      const res = await api('POST', '/library/rescan');
      const n = res && Number.isInteger(res.pieces) ? res.pieces : null;
      toast(n == null ? 'Library folder rescanned.' : `Library folder rescanned: ${n} ${n === 1 ? 'piece' : 'pieces'}.`);
    } catch (err) {
      toast(err.message, 'error');
    } finally {
      rescanBtn.disabled = false;
    }
  }

  return {
    update() {
      renderConn();
      renderSim();
      renderDefLevel();
    },
    onConsole: renderConsole,
    onLibrary: fillPref,
    onSettings() {
      fillPref();
      renderDefLevel();
    },
    onVisible: refresh,
    unmount() {
      alive = false;
      clearInterval(timer);
    },
  };
}

// ======================================================================== boot

let hiddenAt = 0;

function onVisibility() {
  if (document.hidden) {
    hiddenAt = Date.now();
    return;
  }
  const away = hiddenAt ? Date.now() - hiddenAt : 0;
  hiddenAt = 0;
  // iOS can leave a stream silently dead after the app sat in the background.
  if (!es || es.readyState === EventSource.CLOSED || away > 30000) connectEvents();
  if (current.onVisible) current.onVisible();
}

function registerServiceWorker() {
  if (!('serviceWorker' in navigator)) return;
  const register = () => navigator.serviceWorker.register('/sw.js').catch(() => {});
  if (document.readyState === 'complete') register();
  else window.addEventListener('load', register, { once: true });
}

function boot() {
  els.view = document.getElementById('view');
  els.banner = document.getElementById('banner');
  els.sim = document.getElementById('sim-badge');
  els.back = document.getElementById('back');
  els.mini = document.getElementById('mini');
  els.toasts = document.getElementById('toasts');
  els.confirm = document.getElementById('confirm');
  els.confirmTitle = document.getElementById('confirm-title');
  els.confirmText = document.getElementById('confirm-text');
  els.confirmOk = document.getElementById('confirm-ok');
  els.confirmCancel = document.getElementById('confirm-cancel');
  els.tabs = Array.from(document.querySelectorAll('.tabbar a'));
  els.libTab = els.tabs.find((a) => a.dataset.tab === 'library');

  for (const a of els.tabs) {
    a.prepend(icon(a.dataset.icon));
    a.addEventListener('click', () => {
      // Tapping the tab you are on scrolls back to the top.
      if (a.getAttribute('href') === (location.hash || '#/')) window.scrollTo({ top: 0, behavior: 'smooth' });
    });
  }
  // The back link's text depends on the route (route() sets it).
  els.backLabel = h('span', null, els.back.textContent.trim() || 'Library');
  fill(els.back, icon('back'), els.backLabel);
  // A tap on the dim backdrop cancels the dialog.
  els.confirm.addEventListener('click', (e) => { if (e.target === els.confirm) els.confirm.close('cancel'); });

  buildMini();
  window.addEventListener('hashchange', route);
  document.addEventListener('visibilitychange', onVisibility);
  window.addEventListener('pageshow', (e) => { if (e.persisted) connectEvents(); });

  connectEvents();
  loadSettings();
  route();
  registerServiceWorker();
}

boot();
