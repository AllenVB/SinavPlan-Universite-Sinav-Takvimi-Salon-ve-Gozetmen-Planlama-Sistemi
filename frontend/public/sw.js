// SınavPlan service worker (Faz 22 C3): uygulama kabuğunu önbellekte tutar; gözetmen tableti ağ yokken de açılır.
// API (/api/) ASLA önbelleğe alınmaz: yoklama verisi tablet uygulamasının kendi kuyruğunda (localStorage) durur
// ve ağ gelince idempotent senkronla gönderilir. Sürüm değişince eski önbellek silinir.
const CACHE = 'sinavplan-shell-v1'
const SHELL = ['/', '/index.html', '/manifest.webmanifest', '/icon-192.png', '/icon-512.png']

self.addEventListener('install', (e) => {
  e.waitUntil((async () => {
    const c = await caches.open(CACHE)
    await c.addAll(SHELL)
    // index.html'in başvurduğu (hash'li) JS/CSS dosyalarını da önceden al
    const html = await (await fetch('/index.html', { cache: 'no-store' })).text()
    const assets = [...html.matchAll(/(?:src|href)="(\/assets\/[^"]+)"/g)].map((m) => m[1])
    if (assets.length) await c.addAll(assets)
    await self.skipWaiting()
  })())
})

self.addEventListener('activate', (e) => {
  e.waitUntil((async () => {
    for (const k of await caches.keys()) if (k !== CACHE) await caches.delete(k)
    await self.clients.claim()
  })())
})

self.addEventListener('fetch', (e) => {
  const req = e.request
  const url = new URL(req.url)
  if (req.method !== 'GET' || url.origin !== self.location.origin || url.pathname.startsWith('/api/')) return
  if (req.mode === 'navigate') {
    // ağ önce (güncel sürüm), yoksa önbellekteki kabuk
    e.respondWith(fetch(req).then((r) => {
      const copy = r.clone()
      caches.open(CACHE).then((c) => c.put('/index.html', copy))
      return r
    }).catch(() => caches.match('/index.html')))
    return
  }
  if (url.pathname.startsWith('/assets/') || SHELL.includes(url.pathname)) {
    // hash'li dosyalar değişmez: önbellek önce
    e.respondWith(caches.match(req).then((hit) => hit || fetch(req).then((r) => {
      const copy = r.clone()
      caches.open(CACHE).then((c) => c.put(req, copy))
      return r
    })))
  }
})
