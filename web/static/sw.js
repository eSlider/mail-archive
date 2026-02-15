// Mail Archive — minimal service worker for PWA installability
// Caches static assets for offline shell; API calls remain network-first.

const CACHE_NAME = 'mail-archive-v1';
const STATIC_ASSETS = [
  '/',
  '/static/css/app.css',
  '/static/favicon.svg',
  '/manifest.webmanifest',
  '/static/js/vendor/vue-3.5.13.global.prod.js',
  '/static/js/app/main.js',
  '/static/js/app/main.template.vue'
];

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(CACHE_NAME).then((cache) => cache.addAll(STATIC_ASSETS))
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
  // Static assets: cache-first
  if (url.pathname.startsWith('/static/') || url.pathname === '/' || url.pathname === '/login' || url.pathname === '/register') {
    event.respondWith(
      caches.match(request).then((cached) => cached || fetch(request))
    );
  }
});
