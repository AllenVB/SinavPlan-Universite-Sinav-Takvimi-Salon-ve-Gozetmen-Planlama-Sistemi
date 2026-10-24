// README için ekran görüntüleri üretir (docs/screens/). Önkoşul: yayınlanmış çizelge + (isteğe bağlı) `make demo`.
const fs = require('fs'); const out = process.env.OUT || './docs/screens'; fs.mkdirSync(out, { recursive: true });
const { chromium } = require('playwright');
const BASE = process.env.UI_URL || 'http://localhost:3000';
(async () => {
  const b = await chromium.launch({ args: ['--use-fake-ui-for-media-stream', '--use-fake-device-for-media-stream'] });
  const mk = async (email, vp) => { const ctx = await b.newContext({ viewport: vp, permissions: ['camera'] }); const p = await ctx.newPage(); await p.goto(BASE); await p.fill('#email', email); await p.fill('#pw', 'Demo!1234'); await p.click('button.primary'); await p.waitForSelector('.shell'); return p; };
  const D = { width: 1280, height: 900 };
  let p = await mk('sekreter@sinavplan.test', D);
  await p.waitForTimeout(2500); await p.screenshot({ path: `${out}/01-ana-panel.png` });
  await p.goto(BASE + '/#/canli'); await p.waitForSelector('.roomcard'); await p.waitForTimeout(3500); await p.screenshot({ path: `${out}/02-canli-izleme.png` });
  await p.goto(BASE + '/#/salonlar'); await p.waitForSelector('.roomcard'); await p.waitForTimeout(1500); await p.screenshot({ path: `${out}/03-kapasite-panosu.png` });
  await p.goto(BASE + '/#/gozetmenler'); await p.waitForSelector('.hist'); await p.waitForTimeout(1500); await p.screenshot({ path: `${out}/04-gozetmen-atama.png` });
  await p.goto(BASE + '/#/duzenle'); await p.waitForSelector('[draggable=true]', { timeout: 15000 }); await p.screenshot({ path: `${out}/05-surukle-birak-editor.png` });
  p = await mk('gozetmen@sinavplan.test', { width: 1100, height: 1000 }); await p.goto(BASE + '/#/yoklama'); await p.waitForSelector('text=Yoklama al'); await p.click('text=Yoklama al >> nth=0'); await p.waitForSelector('text=Yoklama listesi'); await p.waitForTimeout(2500); await p.screenshot({ path: `${out}/06-gozetmen-tableti.png` });
  p = await mk('ogrenci@sinavplan.test', { width: 420, height: 900 }); await p.waitForSelector('.card button.primary'); const n = await p.$$eval('.card button.primary', (e) => e.length);
  for (let i = 0; i < n; i++) { await p.click(`.card button.primary >> nth=${i}`); await p.waitForTimeout(1200); if (await p.$('[data-testid=qr-svg]')) break; }
  await p.waitForTimeout(500); await p.screenshot({ path: `${out}/07-ogrenci-dinamik-qr.png`, fullPage: true });
  await b.close();
})();
