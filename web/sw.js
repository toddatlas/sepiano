// Service worker for the piano UI.
//
// It keeps the app shell available when the Pi is slow or briefly out of
// reach: shell files are fetched network first (so a new sepianod build shows
// up at once) and fall back to the cache. Everything under /api/ is left
// alone, so the event stream, uploads and downloads always go straight to the
// network.

const VERSION = 'v24';
const PREFIX = 'piano-shell-';
const CACHE = PREFIX + VERSION;
const SHELL = ['/', '/app.js', '/editor.js', '/edittools.js', '/style.css', '/icon.svg', '/manifest.webmanifest'];
const NETWORK_TIMEOUT_MS = 4000;

self.addEventListener('install', (event) => {
  event.waitUntil((async () => {
    const cache = await caches.open(CACHE);
    // One missing file must not stop the worker from installing.
    await Promise.all(SHELL.map((url) =>
      cache.add(new Request(url, { cache: 'reload' })).catch(() => {})));
    await self.skipWaiting();
  })());
});

self.addEventListener('activate', (event) => {
  event.waitUntil((async () => {
    const keys = await caches.keys();
    await Promise.all(keys
      .filter((key) => key.startsWith(PREFIX) && key !== CACHE)
      .map((key) => caches.delete(key)));
    await self.clients.claim();
  })());
});

self.addEventListener('fetch', (event) => {
  const req = event.request;
  if (req.method !== 'GET') return;
  const url = new URL(req.url);
  if (url.origin !== self.location.origin) return;
  if (url.pathname.startsWith('/api/')) return; // SSE, uploads, downloads: never intercepted

  let key = null;
  if (req.mode === 'navigate') {
    if (url.pathname === '/' || url.pathname === '/index.html') key = '/';
  } else if (SHELL.includes(url.pathname)) {
    key = url.pathname;
  }
  if (!key) return; // anything else goes to the network untouched

  event.respondWith(networkFirst(req, key));
});

async function networkFirst(req, key) {
  const cache = await caches.open(CACHE);
  const network = fetch(req).then((res) => {
    if (res.ok && res.type === 'basic' && !res.redirected) {
      cache.put(key, res.clone()).catch(() => {});
    }
    return res;
  });
  network.catch(() => {}); // handled below; avoid an unhandled rejection
  try {
    return await withTimeout(network, NETWORK_TIMEOUT_MS);
  } catch {
    const cached = await cache.match(key);
    if (cached) return cached;
    return network; // nothing cached yet: wait for the network after all
  }
}

function withTimeout(promise, ms) {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('timeout')), ms);
    promise.then(
      (value) => { clearTimeout(timer); resolve(value); },
      (err) => { clearTimeout(timer); reject(err); },
    );
  });
}
