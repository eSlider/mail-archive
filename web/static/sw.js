// Mail Archive — minimal service worker for PWA installability
// Caches static assets for offline shell; API calls remain network-first.

const CACHE_NAME = 'mail-archive-v2';
const STATIC_PRECACHE = [
  '/',
  '/static/css/app.css',
  '/static/favicon.svg',
  '/manifest.webmanifest',
  '/static/js/vendor/vue-3.5.13.global.prod.js',
  '/static/js/app/main.js'
];
// Vue *.template.vue files are cached on first fetch via /static/ prefix
const CACHE_EXACT = ['/', '/login', '/register'];
const CACHE_PREFIXES = ['/static/'];

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(CACHE_NAME).then((cache) => cache.addAll(STATIC_PRECACHE))
      .then(() => self.skipWaiting())
  );
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(keys.filter((k) => k !== CACHE_NAME).map((k) => caches.delete(k)))
    ).then(() => self.clients.claim())
  );
});

self.addEventListener('fetch', (event) => {
  const { request } = event;
  const url = new URL(request.url);
  // API and form posts always go to network
  if (url.pathname.startsWith('/api/') || request.method !== 'GET') {
    return;
  }
  const shouldCache = CACHE_EXACT.includes(url.pathname) ||
    CACHE_PREFIXES.some((p) => url.pathname.startsWith(p));
  if (shouldCache) {
    event.respondWith(
      caches.match(request).then((cached) => cached || fetch(request))
    );
  }
});
