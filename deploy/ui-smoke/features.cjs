// Faz 11 özellikleri için UI duman testi (Playwright gerekir). Kullanım: bkz. pages.cjs.
require('fs').mkdirSync('./shots', { recursive: true });
const { chromium } = require('playwright');
const BASE = process.env.UI_URL || 'http://localhost:3000';
(async () => {
  const b = await chromium.launch();
  let fail = 0; const ok = (c, m) => { console.log(c ? 'PASS' : 'FAIL', m); if (!c) fail++; };
  const login = async (email, vp = { width: 1280, height: 900 }) => { const ctx = await b.newContext({ viewport: vp, acceptDownloads: true }); const p = await ctx.newPage(); await p.goto(BASE); await p.fill('#email', email); await p.fill('#pw', 'Demo!1234'); await p.click('button.primary'); await p.waitForSelector('.shell'); return p; };

  { // X-1: öğrenci kartında gerçek QR (SVG, çok sayıda modül) + .ics indirme
    const p = await login('ogrenci@sinavplan.test', { width: 390, height: 800 });
    await p.waitForSelector('text=Sınav kartı');
    const n = await p.$$eval('.card button.primary:has-text("Sınav kartı"):not([disabled])', (e) => e.length); let found = false;
    for (let i = 0; i < n && !found; i++) { await p.click(`.card button.primary:has-text("Sınav kartı"):not([disabled]) >> nth=${i}`); await p.waitForTimeout(900); found = !!(await p.$('[data-testid=qr-svg]')); }
    ok(found, 'X-1: sınav kartında QR SVG çizildi');
    const modules = await p.$$eval('[data-testid=qr-svg] path, [data-testid=qr-svg] rect', (e) => e.length); ok(modules > 0, 'X-1: QR SVG içeriği var');
    const [dl] = await Promise.all([p.waitForEvent('download'), p.click('text=Takvimime ekle')]); ok(dl.suggestedFilename().endsWith('.ics'), 'X-3: öğrenci .ics indirildi (' + dl.suggestedFilename() + ')');
    await p.screenshot({ path: './shots/qr-mobile.png', fullPage: true });
  }
  { // X-2/X-4/X-5: takvim ekranı
    const p = await login('sekreter@sinavplan.test'); await p.goto(BASE + '/#/takvim'); await p.waitForSelector('text=Sınav oturumları');
    // Faz 16: ders listesi süzgeçleri (fakülte → bölüm → sınıf) tabloyu daraltır
    await p.waitForSelector('tbody tr'); const total = await p.$$eval('tbody tr', (e) => e.length);
    await p.selectOption('select[aria-label=Fakülte]', { label: 'Sağlık Bilimleri Fakültesi' }); await p.selectOption('select[aria-label=Bölüm]', { label: 'Hemşirelik' }); await p.selectOption('select[aria-label=Sınıf]', { label: '4. sınıf' });
    await p.waitForTimeout(300); const rows = await p.$$eval('tbody tr', (e) => e.length); ok(rows === 5 && rows < total, `F16: süzgeç tabloyu daralttı (${total} → ${rows})`);
    ok((await p.textContent('tbody')).includes('Hemşirelik'), 'F16: bölüm sütunu görünüyor'); await p.click('button:has-text("Süzgeci temizle")');
    for (const [label, ext] of [['Excel', '.xlsx'], ['CSV', '.csv'], ['iCal', '.ics']]) { const [d] = await Promise.all([p.waitForEvent('download'), p.click(`button:has-text("${label}")`)]); ok(d.suggestedFilename().endsWith(ext), `X-2/3: ${label} indirildi (${d.suggestedFilename()})`); }
    await p.click('button:has-text("Provayı çalıştır")'); await p.waitForSelector('text=Sert ihlal yok', { timeout: 30000 }); ok(true, 'X-4: tüm kapsamda prova sonucu (sert ihlal yok)');
    // Faz 16: fakülte → bölüm → sınıf kapsamı: Mühendislik, yalnız Yazılım, yalnız 1. sınıf
    await p.click('button:has-text("Mühendislik Fakültesi")'); await p.waitForSelector('text=2 · Bölümler');
    await p.click('button:has-text("Hiçbirini seçme")'); await p.click('label:has-text("Yazılım Mühendisliği") input');
    for (const y of [2, 3, 4]) await p.click(`label:has-text("${y}. sınıf") input`);
    await p.waitForSelector('text=5 sınav provaya girecek'); ok(true, 'F16: kapsam önizlemesi 5 sınav (1 bölüm × 1 sınıf)');
    await p.click('button:has-text("Provayı çalıştır")'); await p.waitForSelector('text=Yazılım Mühendisliği · 1. sınıf', { timeout: 30000 });
    ok((await p.textContent('main')).includes('5 sınav'), 'F16: kapsamlı prova yalnız 5 sınavı hesapladı');
    await p.screenshot({ path: './shots/takvim-tools.png', fullPage: true });
  }
  { // X-6: panelde etkinlik akışı
    const p = await login('sekreter@sinavplan.test'); await p.waitForSelector('text=Son olaylar'); await p.waitForTimeout(1500);
    ok((await p.$$('li:has-text("Takvim")')).length > 0, 'X-6: etkinlik akışında olaylar listelendi');
  }
  { // X-7: editör — denetle (klavye alternatifi) ve ızgara
    const p = await login('sekreter@sinavplan.test'); await p.goto(BASE + '/#/duzenle'); await p.waitForSelector('[draggable=true]', { timeout: 15000 });
    ok((await p.$$('[draggable=true]')).length > 10, 'X-7: sınav çipleri sürüklenebilir');
    await p.selectOption('#pe', { index: 1 });
    // sınavın zaten bulunduğu oturum seçilirse düğme işlem yapmaz; denetim paneli açılana dek sıradaki oturumu dene
    let opened = false;
    for (let si = 2; si < 8 && !opened; si++) { await p.selectOption('#ps', { index: si }); await p.click('button:has-text("Denetle")'); opened = await p.waitForSelector('h2:has-text("→")', { timeout: 6000 }).then(() => true, () => false); }
    if (!opened) await p.waitForSelector('h2:has-text("→")', { timeout: 20000 }); ok(true, 'X-7: denetim paneli açıldı');
    // gerçek sürükle-bırak: ilk çipi başka bir oturum hücresine bırak
    const chip = p.locator('[draggable=true]').first(); const cells = p.locator('td[aria-label*="oturum"]');
    const ci = await cells.evaluateAll((els, id) => els.findIndex((e) => e.contains(document.querySelector('[draggable=true]'))), 0);
    await chip.dragTo(cells.nth((ci + 7) % (await cells.count())));
    await p.waitForSelector('h2:has-text("→")', { timeout: 20000 }); ok(true, 'X-7: sürükle-bırak denetimi tetikledi');
    await p.screenshot({ path: './shots/editor.png', fullPage: false });
  }
  await b.close(); process.exit(fail ? 1 : 0);
})();
