// Faz 22 C3 — tablet PWA: ÜRETİM derlemesinde (service worker yalnız orada kaydedilir) çalıştırın:
//   cd frontend && npm run build && npx vite preview --port 4173   (API localhost:8080'de; /api vekillenir)
//   NODE_PATH=$(npm root -g) UI_URL=http://localhost:4173 node deploy/ui-smoke/pwa.cjs
// Kurulabilirlik (manifest + ikonlar + service worker), ağ YOKKEN sayfa yenilenince tablet açılır, son görev
// listesi ve yoklama listesi cihazdan gelir, işaretleme kuyruğa girer; ağ gelince kendiliğinden senkronlanır
// ve sunucudaki yoklamada görünür.
const { chromium } = require('playwright');
const BASE = process.env.UI_URL || 'http://localhost:4173';
(async () => {
  const b = await chromium.launch();
  let fail = 0; const ok = (c, m) => { console.log(c ? 'PASS' : 'FAIL', m); if (!c) fail++; };
  const ctx = await b.newContext({ viewport: { width: 1024, height: 800 } });
  const p = await ctx.newPage();
  await p.goto(BASE);
  const mf = await p.$eval('link[rel=manifest]', (l) => l.href);
  const man = await (await fetch(mf)).json();
  const icons = await Promise.all(man.icons.map(async (i) => (await fetch(new URL(i.src, BASE))).status));
  ok(man.display === 'standalone' && man.icons.some((i) => i.sizes === '512x512') && icons.every((s) => s === 200), `manifest: ${man.short_name}, ${man.icons.length} ikon (hepsi 200), standalone`);
  await p.fill('#email', 'gozetmen@sinavplan.test'); await p.fill('#pw', 'Demo!1234'); await p.click('button.primary'); await p.waitForSelector('.shell');
  ok(await p.evaluate(async () => !!(await navigator.serviceWorker.ready)), 'service worker kaydedildi ve hazır');
  await p.goto(BASE + '/#/yoklama'); await p.reload(); await p.waitForSelector('.shell');
  ok(await p.evaluate(() => !!navigator.serviceWorker.controller), 'sayfa service worker denetiminde');
  await p.waitForSelector('text=Yoklama al'); await p.click('text=Yoklama al >> nth=0');
  await p.waitForSelector('button:text-is("GELDİ")', { timeout: 10000 }); // liste bir kez çevrimiçi yüklendi (cihaza kaydedildi)

  await ctx.setOffline(true);
  await p.reload(); await p.waitForSelector('.shell', { timeout: 15000 }).then(() => ok(true, 'ağ YOKKEN sayfa yenilendi: uygulama açıldı (giriş ekranına düşmedi)'), () => ok(false, 'çevrimdışı yenilemede uygulama açılmadı'));
  ok((await p.textContent('main')).includes('Çevrimdışı'), 'çevrimdışı durumu gösteriliyor');
  await p.waitForSelector('text=Yoklama al', { timeout: 10000 }); await p.click('text=Yoklama al >> nth=0');
  await p.waitForSelector('button:text-is("GELDİ")', { timeout: 10000 }).then(() => ok(true, 'görev ve yoklama listesi cihazdan açıldı'), () => ok(false, 'çevrimdışı liste açılmadı'));
  await p.click('button:text-is("GELDİ") >> nth=0');
  await p.waitForSelector('text=1 yoklama kaydı bu cihazda saklanıyor', { timeout: 5000 }).then(() => ok(true, 'işaretleme cihazdaki kuyruğa girdi'), () => ok(false, 'kuyruk bandı görünmedi'));
  await p.screenshot({ path: './shots/pwa-offline.png' });

  await ctx.setOffline(false);
  await p.waitForFunction(() => !document.body.textContent.includes('bu cihazda saklanıyor') && !document.body.textContent.includes('eşitlenmeyi bekliyor'), null, { timeout: 20000 })
    .then(() => ok(true, 'ağ gelince kuyruk kendiliğinden senkronlandı'), () => ok(false, 'kuyruk boşalmadı'));
  const before = await p.evaluate(() => Object.keys(localStorage).filter((k) => k.startsWith('sinavplan-cache:')).length);
  await p.click('header button:has-text("Çıkış")'); await p.waitForSelector('#email');
  const after = await p.evaluate(() => Object.keys(localStorage).filter((k) => k.startsWith('sinavplan-cache:')).length);
  ok(before > 0 && after === 0, `çıkışta cihazdaki liste önbelleği silindi (${before} → ${after}; paylaşılan tablette sonraki kişi görmez)`);
  await b.close(); process.exit(fail ? 1 : 0);
})();
