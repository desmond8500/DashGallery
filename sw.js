// Service worker DashGallery : garde la galerie (et Vue) en cache pour l'usage hors ligne.
const CACHE = 'dashgallery-v1';
const SHELL = ['./', 'index.html', 'manifest.webmanifest', 'icon.svg'];
const OPTIONAL = ['vendor/vue.global.prod.js', 'https://unpkg.com/vue@3.5.13/dist/vue.global.prod.js'];

self.addEventListener('install', (e) => {
  e.waitUntil((async () => {
    const c = await caches.open(CACHE);
    await c.addAll(SHELL).catch(() => {});
    await Promise.all(OPTIONAL.map(u => c.add(u).catch(() => {})));
    self.skipWaiting();
  })());
});

self.addEventListener('activate', (e) => {
  e.waitUntil((async () => {
    for (const k of await caches.keys()) if (k !== CACHE) await caches.delete(k);
    await self.clients.claim();
  })());
});

self.addEventListener('fetch', (e) => {
  const req = e.request;
  if (req.method !== 'GET') return;
  const url = new URL(req.url);
  if (url.origin === location.origin && url.pathname.startsWith('/api/')) return;   // API : toujours réseau

  if (url.hostname === 'unpkg.com') {   // CDN : cache d'abord
    e.respondWith(caches.match(req).then(hit => hit || fetch(req).then(res => {
      if (res.ok) caches.open(CACHE).then(c => c.put(req, res.clone()));
      return res;
    })));
    return;
  }
  if (url.origin !== location.origin) return;

  // Fichiers de l'appli : réseau d'abord (mises à jour), cache si hors ligne ou serveur arrêté
  e.respondWith(fetch(req).then(res => {
    if (res.ok) { const copy = res.clone(); caches.open(CACHE).then(c => c.put(req, copy)); }
    return res;
  }).catch(async () =>
    (await caches.match(req, { ignoreSearch: true })) ||
    (req.mode === 'navigate' ? caches.match('index.html') : Response.error())
  ));
});
