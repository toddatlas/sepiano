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
  plus: { stroke: ['M12 5v14M5 12h14'] },
  now: { stroke: ['M9 18V5l11-2v13', 'M6 15a3 3 0 1 0 0 6 3 3 0 0 0 0-6zM17 13a3 3 0 1 0 0 6 3 3 0 0 0 0-6z'] },
  library: { stroke: ['M4 6h16M4 12h16M4 18h10'] },
  record: {
    stroke: ['M12 4a8 8 0 1 0 0 16 8 8 0 0 0 0-16z'],
    fill: ['M12 8.5a3.5 3.5 0 1 0 0 7 3.5 3.5 0 0 0 0-7z'],
  },
  dot: { fill: ['M12 5a7 7 0 1 0 0 14 7 7 0 0 0 0-14z'] },
  status: { stroke: ['M3 12h4l3-7 4 14 3-7h4'] },
  upload: { stroke: ['M12 15V4M7.5 8.5 12 4l4.5 4.5M5 20h14'] },
  download: { stroke: ['M12 4v11M7.5 10.5 12 15l4.5-4.5M5 20h14'] },
  edit: { stroke: ['M4 20h4L19 9a2.8 2.8 0 0 0-4-4L4 16v4z'] },
  trash: { stroke: ['M4 7h16M9 7V4.5h6V7M6.5 7l1 13h9l1-13M10 11v5.5M14 11v5.5'] },
  search: { stroke: ['M11 4a7 7 0 1 0 0 14 7 7 0 0 0 0-14zM20 20l-4-4'] },
  back: { stroke: ['M15 5l-7 7 7 7'] },
  close: { stroke: ['M6 6l12 12M18 6 6 18'] },
  check: { stroke: ['M5 12.5l4.5 4.5L19 7'] },
  refresh: { stroke: ['M20 11a8 8 0 0 0-14.5-4.5L4 8M4 4v4h4M4 13a8 8 0 0 0 14.5 4.5L20 16M20 20v-4h-4'] },
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

function rankRenderings(pieces) {
  const pref = preferredInstrument();
  return pieces
    .map((p, i) => ({ p, i, rank: renderingRank(p.instrument, pref) }))
    .sort((a, b) => a.rank - b.rank || a.i - b.i)
    .map((x) => x.p);
}

function bestRendering(pieces) { return rankRenderings(pieces)[0]; }

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
// Shows Now Playing once the service has taken it.
async function startPlay(body, btn) {
  if (btn) btn.disabled = true;
  const ok = await command('/deck/play', body);
  if (btn) btn.disabled = false;
  if (ok) location.hash = '#/';
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
  rec: { title: '', awaitingSince: 0, saved: null }, // saved: {id, title}
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
  fault: 'Something went wrong',
};
function stateWords(s) { return s ? (STATE_WORDS[s.state] || s.state) : 'Connecting…'; }

// An album or play list plays as one performance; `tracks` says where each
// track lies in it. The main progress bar shows the current track's stretch
// of it, or the whole performance for a single piece.
function trackSpan(s) {
  const t = s && s.collection && Array.isArray(s.tracks) ? s.tracks[s.track_index] : null;
  if (t) return { start: Math.max(0, num0(t.start_ms)), dur: Math.max(0, num0(t.duration_ms)) };
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
function confirmDialog({ title, text, ok = 'OK', danger = false }) {
  const dlg = els.confirm;
  if (!dlg || typeof dlg.showModal !== 'function') {
    return Promise.resolve(window.confirm(`${title}\n\n${text}`));
  }
  if (dlg.open) return Promise.resolve(false);
  setText(els.confirmTitle, title);
  setText(els.confirmText, text);
  setText(els.confirmOk, ok);
  els.confirmOk.className = 'btn ' + (danger ? 'btn-danger-solid' : 'btn-primary');
  return new Promise((resolve) => {
    // Answer straight from the buttons; `close` (fired later, and also by
    // Escape or a backdrop tap) only counts as a cancel.
    const finish = (answer) => {
      els.confirmOk.removeEventListener('click', onOk);
      els.confirmCancel.removeEventListener('click', onCancel);
      dlg.removeEventListener('close', onCancel);
      if (dlg.open) dlg.close();
      resolve(answer);
    };
    const onOk = () => finish(true);
    const onCancel = () => finish(false);
    els.confirmOk.addEventListener('click', onOk);
    els.confirmCancel.addEventListener('click', onCancel);
    dlg.addEventListener('close', onCancel);
    dlg.showModal();
    els.confirmCancel.focus();
  });
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

// `back: true` goes to the library section last shown; an object names its own.
const ROUTES = [
  { name: 'now', re: /^\/$/, tab: 'now', title: 'Now Playing', screen: nowPlayingScreen },
  { name: 'library', re: /^\/library$/, tab: 'library', title: 'Library', screen: libraryScreen },
  { name: 'albums', re: /^\/albums$/, tab: 'library', title: 'Albums', screen: albumsScreen },
  { name: 'playlists', re: /^\/playlists$/, tab: 'library', title: 'Play lists', screen: playlistsScreen },
  { name: 'album', re: /^\/album\/([^/]+)$/, tab: 'library', title: 'Album', back: { href: '#/albums', label: 'Albums' }, screen: albumScreen },
  { name: 'playlist', re: /^\/playlist\/([^/]+)$/, tab: 'library', title: 'Play list', back: { href: '#/playlists', label: 'Play lists' }, screen: playlistScreen },
  { name: 'piece', re: /^\/piece\/([^/]+)$/, tab: 'library', title: 'Piece', back: true, screen: pieceScreen },
  { name: 'upload', re: /^\/upload$/, tab: 'library', title: 'Upload', back: true, screen: uploadScreen },
  { name: 'record', re: /^\/record$/, tab: 'record', title: 'Record', screen: recordScreen },
  { name: 'status', re: /^\/status$/, tab: 'status', title: 'Status', screen: statusScreen },
];

// The segmented control at the top of the library: [route name, hash, label].
const LIB_SECTIONS = [
  ['library', '#/library', 'Pieces'],
  ['albums', '#/albums', 'Albums'],
  ['playlists', '#/playlists', 'Play lists'],
];

let current = {}; // the mounted screen's controller: update, tick, onLibrary, onConsole, onSettings, onVisible, unmount
let currentRoute = null;
let currentPath = '';
let libSection = '#/library'; // the Library tab returns to the section last shown

// Where the top bar's back link goes. A piece opened from an album or play
// list goes back to it.
function backLink(r, prev, prevPath) {
  if (!r.back) return null;
  if (r.name === 'piece' && prev && (prev.name === 'album' || prev.name === 'playlist')) {
    return { href: '#' + prevPath, label: prev.title };
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
    params = m.slice(1).map(decodeURIComponent);
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
  const back = backLink(r, prev, prevPath);
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
  const hintEl = h('p', { class: 'np-hint' });
  const messageEl = h('p', { class: 'message message-warn', role: 'status' });
  const progFill = h('div', { class: 'progress-fill' });
  const progress = h('div', { class: 'progress', role: 'progressbar', 'aria-label': 'Position in the piece', 'aria-valuemin': '0' }, progFill);
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

  const playPanel = h('section', { class: 'np' },
    stateEl, collEl, trackEl, titleEl, composerEl, performerEl, fileEl, hintEl, messageEl,
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
  const levelCard = h('section', { class: 'card level-card' }, level.el, levelNote);

  root.append(playPanel, emptyPanel, recPanel, levelCard);
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
    setText(levelNote, live ? '' : 'Used when the next piece starts.');
    levelNote.hidden = live;
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

  return {
    update,
    tick,
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
const libState = { q: '', composer: '', performer: '', instrument: '', scroll: 0 };

function libraryScreen(root, _params, ctx) {
  let alive = true;
  let pieces = null;
  let colls = null; // albums and play lists matching the search
  let seq = 0;
  let restoreScroll = ctx.from === 'piece';
  let playingId = null;

  const search = h('input', {
    class: 'search',
    type: 'search',
    placeholder: 'Search pieces, albums, folders…',
    'aria-label': 'Search the library',
    autocomplete: 'off',
    autocorrect: 'off',
    autocapitalize: 'off',
    spellcheck: 'false',
    enterkeyhint: 'search',
  });
  search.value = libState.q;
  search.addEventListener('input', debounce(() => {
    if (!alive || search.value === libState.q) return;
    libState.q = search.value;
    load();
  }, 300));
  search.addEventListener('keydown', (e) => { if (e.key === 'Enter') search.blur(); });

  const filters = [
    filterSelect('composer', 'All composers'),
    filterSelect('performer', 'All performers'),
    filterSelect('instrument', 'All instruments'),
  ];
  const clearBtn = h('button', { class: 'chip-clear', type: 'button', hidden: true, onclick: clearAll }, 'Clear');
  const countEl = h('p', { class: 'lib-count', 'aria-live': 'polite' });
  const list = h('ul', { class: 'list', role: 'list' });
  const body = h('div', null, h('p', { class: 'muted center' }, 'Loading…'));

  root.append(
    ...libraryHead('library'),
    h('div', { class: 'search-wrap' }, icon('search', 'icon search-icon'), search),
    h('div', { class: 'filters' }, filters.map((f) => f.el), clearBtn),
    countEl,
    body);

  load();

  function filterSelect(key, allLabel) {
    const sel = h('select', {
      class: 'chip-select',
      'aria-label': allLabel.replace(/^All /, 'Filter by '),
      onchange: () => { libState[key] = sel.value; render(); },
    });
    return { key, allLabel, sel, el: h('label', { class: 'chip' }, sel) };
  }

  function fillSelect(f, values) {
    const cur = libState[f.key];
    const all = cur && !values.includes(cur) ? [cur, ...values] : values;
    fill(f.sel,
      h('option', { value: '' }, f.allLabel),
      all.map((v) => h('option', { value: v }, f.key === 'instrument' ? instrumentLabel(v) : v)));
    f.sel.value = cur;
    f.el.classList.toggle('active', !!cur);
  }

  // Does p pass every active filter except `except`?
  function matches(p, except) {
    return (except === 'composer' || !libState.composer || p.composer === libState.composer)
      && (except === 'performer' || !libState.performer || p.performer === libState.performer)
      && (except === 'instrument' || !libState.instrument || p.instrument === libState.instrument);
  }

  async function load() {
    const my = ++seq;
    const q = libState.q.trim();
    try {
      // A search also looks through the albums and the folders' play lists.
      const [res, al, pl] = await Promise.all([
        api('GET', '/pieces' + (q ? '?q=' + encodeURIComponent(q) : '')),
        q ? api('GET', '/albums').catch(() => null) : null,
        q ? api('GET', '/playlists').catch(() => null) : null,
      ]);
      if (!alive || my !== seq) return;
      colls = q ? matchCollections(q, (al && al.albums) || [], (pl && pl.playlists) || []) : null;
      pieces = (res && res.pieces) || [];
      if (!q) {
        app.pieces = pieces;
        app.piecesStale = false;
      }
      render();
    } catch (err) {
      if (!alive || my !== seq) return;
      if (pieces) { toast(err.message, 'error'); return; }
      fill(body, h('div', { class: 'empty small' },
        h('p', { class: 'empty-text' }, `Could not load the library. ${err.message}`),
        h('button', { class: 'btn', type: 'button', onclick: load }, icon('refresh'), 'Try again')));
    }
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
      if (libState.instrument) {
        pick = group.find((p) => p.instrument === libState.instrument);
        if (!pick) continue;
      } else {
        pick = bestRendering(group);
      }
      out.push({ piece: pick, count: group.length });
    }
    out.sort((a, b) => cmpPieces(a.piece, b.piece));
    return out;
  }

  function render() {
    if (!pieces || !alive) return;
    for (const f of filters) fillSelect(f, uniqSorted(pieces.filter((p) => matches(p, f.key)).map((p) => p[f.key])));
    const anyFilter = !!(libState.composer || libState.performer || libState.instrument);
    clearBtn.hidden = !anyFilter;

    const items = rows();
    setText(countEl, items.length === 1 ? '1 performance' : `${items.length} performances`);
    countEl.hidden = !items.length;

    const albums = (colls && colls.albums) || [];
    const lists = (colls && colls.playlists) || [];
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
      fill(body, emptyState(anyFilter || !!libState.q.trim()));
    } else {
      fill(list, ...items.map(row));
      fill(body, ...found, items.length ? list : null);
      playingId = null;
      markPlaying();
    }
    if (restoreScroll) {
      restoreScroll = false;
      window.scrollTo(0, libState.scroll);
    }
  }

  function emptyState(filtered) {
    if (filtered) {
      return h('div', { class: 'empty small' },
        h('p', { class: 'empty-text' }, 'Nothing matches.'),
        h('button', { class: 'btn', type: 'button', onclick: clearAll }, 'Show everything'));
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
        pl.source && h('span', { class: 'row-file' }, pl.source)),
      h('button', {
        class: 'row-play', type: 'button', 'aria-label': `Play all of ${pl.name}`,
        onclick: (e) => startPlay({ playlist: pl.id }, e.currentTarget),
      }, icon('play')));
  }

  function row({ piece: p, count }) {
    const sub = byline(p);
    return h('li', { class: 'row', 'data-id': p.id },
      h('a', { class: 'row-main', href: pieceHref(p.id) },
        h('span', { class: 'row-title' }, p.title || 'Untitled'),
        sub && h('span', { class: 'row-sub' }, sub),
        p.source_name && h('span', { class: 'row-file' }, p.source_name),
        h('span', { class: 'row-meta' },
          fmtTime(p.duration_ms),
          count > 1 && h('span', { class: 'hint-pill' }, `${count} renderings`))),
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
    libState.composer = '';
    libState.performer = '';
    libState.instrument = '';
    if (libState.q) {
      libState.q = '';
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
      libState.scroll = window.scrollY;
    },
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
    note: (p) => p.source,
    href: (p) => playlistHref(p.id),
    play: (p) => ({ playlist: p.id }),
    emptyTitle: 'No play lists yet',
    emptyText: 'Play lists imported with your collection appear here.',
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
    const shown = words.length
      ? items.filter((it) => {
        const text = fold(cfg.words(it).filter(Boolean).join(' '));
        return words.every((w) => text.includes(w));
      })
      : items;
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
        }, icon('play'), 'Play all')),
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
    fill(list, pieces.map((p, i) => trackRow({
      index: i,
      num: String(i + 1),
      piece: p || {},
      sub: byline(p || {}),
      body: { playlist: plId, start: i },
    })));
    fill(root,
      h('header', { class: 'piece-head' },
        h('h1', { class: 'piece-title' }, name),
        pl.source && h('p', { class: 'collection-source' }, `Imported from ${pl.source}`),
        h('p', { class: 'collection-meta' }, collectionMeta(pieces.length, pl.duration_ms))),
      h('div', { class: 'piece-actions' },
        h('button', {
          class: 'btn btn-primary btn-large',
          type: 'button',
          disabled: !pieces.length,
          onclick: (e) => startPlay({ playlist: plId }, e.currentTarget),
        }, icon('play'), 'Play all')),
      pieces.length ? list : h('p', { class: 'muted center' }, 'This play list is empty.'),
      h('div', { class: 'danger-zone' },
        h('button', { class: 'btn btn-danger', type: 'button', onclick: remove }, icon('trash'), 'Delete play list')));
    playingIndex = null;
    markPlaying();
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
      h('div', { class: 'piece-actions' },
        playBtn,
        h('button', { class: 'btn btn-large', type: 'button', onclick: startEdit }, icon('edit'), 'Edit')),
      metaList([
        ['Album', p.album ? h('a', { href: albumHref(p.album) }, p.track ? `${p.album}, track ${p.track}` : p.album) : ''],
        ['Recorded', fmtRecorded(p.recorded_on)],
        ['Instrument', instrumentLabel(p.instrument)],
        ['Length', fmtTime(p.duration_ms)],
        ['Origin', originText(p)],
        ['Source file', p.source_name, 'mono'],
        ['Header', p.header_text, 'pre'],
        ['Notes', p.notes, 'pre'],
        ['Added', fmtDateTime(p.added_at)],
      ]),
      h('section', { class: 'card' },
        h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Download')),
        h('div', { class: 'btn-row' },
          h('a', { class: 'btn', href: downloadBase + 'se', download: '' }, icon('download'), 'SE files (.zip)'),
          h('a', { class: 'btn', href: downloadBase + 'midi', download: '' }, icon('download'), 'MIDI'))),
      renderingsSection,
      h('div', { class: 'danger-zone' },
        h('button', { class: 'btn btn-danger', type: 'button', onclick: remove }, icon('trash'), 'Delete this piece')));
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
      title: 'Delete this piece?',
      text: `“${piece.title || 'Untitled'}” and its files will be removed from the piano. This cannot be undone.`,
      ok: 'Delete',
      danger: true,
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
    armBtn);

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
        nodes = [h('p', { class: 'lead' }, 'Record what you play on this piano. It is saved to the library as a new piece.'), armForm];
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
        h('a', { class: 'btn', href: pieceHref(sv.id) }, 'Details')));
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
    if (ok) run('discard', '/record/discard');
  }

  return {
    update,
    tick,
    onLibrary() { refresh(app.status); },
    unmount() { alive = false; },
  };
}

// ====================================================================== Status

function statusScreen(root) {
  let alive = true;
  let inst = null;
  let instError = '';
  let seeded = false;
  let connKey = '';
  let progKey = '';
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
    h('p', { class: 'muted small' }, 'Stahnke’s play.exe and record.exe run on the piano. They are not part of this software: install your own copies.'),
    progBody);

  const consoleEl = h('pre', { class: 'console', tabindex: '0', 'aria-label': 'Piano console output' });
  const consoleCard = h('section', { class: 'card' },
    h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Console')),
    consoleEl);

  const prefSel = h('select', { class: 'input', id: 'set-pref', onchange: () => saveSettings({ preferred_instrument: prefSel.value }) });
  const defLevel = levelStepper(stepDefault, 'Default level');
  const settingsCard = h('section', { class: 'card' },
    h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Settings')),
    h('div', { class: 'field' },
      h('label', { for: 'set-pref' }, 'Preferred instrument'),
      h('div', { class: 'select-wrap' }, prefSel),
      h('p', { class: 'hint' }, 'When a performance has several renderings, the library shows this one.')),
    defLevel.el,
    h('p', { class: 'level-hint' }, 'The level each piece starts at.'));

  const rescanBtn = h('button', { class: 'btn', type: 'button', onclick: rescan }, icon('refresh'), 'Rescan library folder');
  const libCard = h('section', { class: 'card' },
    h('div', { class: 'card-head' }, h('h2', { class: 'card-title' }, 'Library')),
    h('div', { class: 'btn-col' },
      h('a', { class: 'btn', href: API + '/export', download: '' }, icon('download'), 'Back up library'),
      rescanBtn),
    h('p', { class: 'hint' }, 'The backup is one .zip of every piece. Rescan after copying files into the library folder by hand.'));

  root.append(h('h1', { class: 'screen-title' }, 'Status'), connCard, simCard, progCard, consoleCard, settingsCard, libCard);

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
    const key = JSON.stringify([!!inst, progs, [...installing]]);
    if (key === progKey) return;
    progKey = key;
    fill(progBody, programRow('play', progs.play), programRow('record', progs.record));
  }

  function programRow(name, prog) {
    const file = `${name}.exe`;
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
      if (inst && prog) inst.programs = { ...(inst.programs || {}), [name]: prog };
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
