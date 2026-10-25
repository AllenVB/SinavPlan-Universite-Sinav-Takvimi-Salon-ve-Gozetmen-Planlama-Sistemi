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
  await p.goto(BASE + '/#/takvim'); await p.waitForSelector('tbody tr'); await p.selectOption('select[aria-label=Fakülte]', { label: 'Mühendislik Fakültesi' }); await p.selectOption('select[aria-label=Bölüm]', { label: 'Bilgisayar Mühendisliği' }); await p.selectOption('select[aria-label=Sınıf]', { label: '2. sınıf' }); await p.waitForTimeout(500);
  await p.locator('text=Sınav oturumları').first().scrollIntoViewIfNeeded(); await p.screenshot({ path: `${out}/08-takvim-fakulte-bolum-sinif.png` });
  await p.click('.tabs button:has-text("Prova")'); await p.locator('text=Sınav provası').first().scrollIntoViewIfNeeded(); await p.click('button:has-text("Mühendislik Fakültesi") >> nth=0').catch(() => {}); await p.waitForTimeout(400); await p.screenshot({ path: `${out}/09-sinav-provasi-kapsam.png` });
  await p.goto(BASE + '/#/canli'); await p.waitForSelector('.roomcard'); await p.waitForTimeout(3500); await p.screenshot({ path: `${out}/02-canli-izleme.png` });
  await p.goto(BASE + '/#/salonlar'); await p.waitForSelector('.roomcard'); await p.waitForTimeout(1500); await p.screenshot({ path: `${out}/03-kapasite-panosu.png` });
  await p.goto(BASE + '/#/gozetmenler'); await p.waitForSelector('.hist'); await p.waitForTimeout(1500); await p.screenshot({ path: `${out}/04-gozetmen-atama.png` });
  await p.goto(BASE + '/#/duzenle'); await p.waitForSelector('[draggable=true]', { timeout: 15000 }); await p.screenshot({ path: `${out}/05-surukle-birak-editor.png` });
  p = await mk('gozetmen@sinavplan.test', { width: 1100, height: 1000 }); await p.goto(BASE + '/#/yoklama'); await p.waitForSelector('text=Yoklama al'); await p.click('text=Yoklama al >> nth=0'); await p.waitForSelector('text=Yoklama listesi'); await p.waitForTimeout(2500); await p.screenshot({ path: `${out}/06-gozetmen-tableti.png` });
  p = await mk('ogrenci@sinavplan.test', { width: 420, height: 900 }); await p.waitForSelector('.card button.primary'); const n = await p.$$eval('.card button.primary:not([disabled])', (e) => e.length);
  for (let i = 0; i < n; i++) { await p.click(`.card button.primary:not([disabled]) >> nth=${i}`); await p.waitForTimeout(1200); if (await p.$('[data-testid=qr-svg]')) break; }
  await p.waitForTimeout(500); await p.screenshot({ path: `${out}/07-ogrenci-dinamik-qr.png`, fullPage: true });

  // --- Faz 17–19 ekranları: kapı krokisi, salon düzenleyici, QR + kod, mazeret masası, takvim süzgeçleri
  const API = process.env.API_URL || 'http://localhost:8080';
  const tok = async (email) => (await (await fetch(API + '/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email, password: 'Demo!1234' }) })).json()).access_token;
  p = await mk('sekreter@sinavplan.test', D);
  await p.goto(BASE + '/#/salonlar'); await p.click('button:has-text("Salon envanteri")'); await p.waitForSelector('text=Düzeni gör'); await p.waitForTimeout(800);
  await p.click('button:has-text("Düzeni gör") >> nth=0'); await p.waitForSelector('[data-testid=room-door]'); await p.locator('[data-testid=room-door]').scrollIntoViewIfNeeded(); await p.waitForTimeout(500);
  await p.screenshot({ path: `${out}/10-salon-kapisi-krokisi.png` });
  await p.click('button:has-text("Düzenle") >> nth=0'); await p.waitForSelector('text=salonunu düzenle'); await p.waitForTimeout(600);
  await p.screenshot({ path: `${out}/11-salon-duzenleyici.png`, fullPage: true });
  // gözetmen: şu an süren görev için QR + 6 haneli kod
  const duties = (await (await fetch(API + '/me/duties?limit=200', { headers: { Authorization: 'Bearer ' + await tok('gozetmen@sinavplan.test') } })).json()).items.sort((a, c) => a.starts_at.localeCompare(c.starts_at));
  const idx = duties.findIndex((d) => Date.parse(d.starts_at) <= Date.now() && Date.now() < Date.parse(d.starts_at) + 90 * 60000);
  if (idx >= 0) {
    const g = await mk('gozetmen@sinavplan.test', { width: 1100, height: 1000 }); await g.goto(BASE + '/#/yoklama');
    await g.waitForSelector('table button.btn.sm'); await g.click(`table button.btn.sm >> nth=${idx}`); await g.click('text=Yeni QR oluştur'); await g.waitForSelector('[data-testid=checkin-code]', { timeout: 15000 }); await g.waitForTimeout(800);
    await g.screenshot({ path: `${out}/12-gozetmen-qr-kod.png`, fullPage: true });
    const code = (await g.textContent('[data-testid=checkin-code]')).replace(/\s/g, '');
    const st = await mk('ogrenci@sinavplan.test', { width: 420, height: 900 }); await st.goto(BASE + '/#/programim'); await st.waitForSelector('#chk'); await st.fill('#chk', code); await st.screenshot({ path: `${out}/13-ogrenci-kod-girisi.png`, fullPage: true });
  } else console.log('UYARI: şu an süren görev yok; 12/13 atlandı (make reset-demo)');
  // sekreter: bekleyen mazeret + karar masası
  const st = await tok('ogrenci@sinavplan.test');
  const mine = (await (await fetch(API + '/me/exams?limit=200', { headers: { Authorization: 'Bearer ' + st } })).json()).items;
  const fut = mine.filter((e) => Date.parse(e.starts_at) > Date.now() + 86400000).pop();
  if (fut) await fetch(API + '/excuses', { method: 'POST', headers: { Authorization: 'Bearer ' + st, 'Content-Type': 'application/json' }, body: JSON.stringify({ exam_id: fut.exam_id, reason: 'Hastane raporum var' }) });
  await p.goto(BASE + '/#/mazeret'); await p.waitForSelector('text=Mazeretler'); await p.waitForTimeout(1500); await p.screenshot({ path: `${out}/14-mazeret-masasi.png` });
  await p.goto(BASE + '/#/takvim'); await p.waitForSelector('tbody tr');
  await p.selectOption('select[aria-label="Sınav türü"]', { index: 1 }).catch(() => {}); await p.selectOption('select[aria-label="Saat dilimi"]', { index: 1 }).catch(() => {}); await p.waitForTimeout(500);
  await p.locator('text=Sınav oturumları').first().scrollIntoViewIfNeeded(); await p.screenshot({ path: `${out}/15-takvim-suzgecleri.png` });
  await b.close();
})();
