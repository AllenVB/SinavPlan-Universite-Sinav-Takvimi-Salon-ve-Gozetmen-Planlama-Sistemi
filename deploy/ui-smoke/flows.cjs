// UI duman testi (Playwright gerekir: npm i -g playwright). Çalıştırma: NODE_PATH=$(npm root -g) node deploy/ui-smoke/<dosya>.cjs; ekran görüntüleri ./shots/ altına yazılır.
require('fs').mkdirSync('./shots', { recursive: true });
const { chromium } = require('playwright');
const BASE = process.env.UI_URL || 'http://localhost:3000';
(async () => {
  const b = await chromium.launch();
  let fail = 0; const ok = (c, m) => { console.log(c ? 'PASS' : 'FAIL', m); if (!c) fail++; };
  const login = async (email, vp) => { const ctx = await b.newContext({ viewport: vp }); const p = await ctx.newPage(); await p.goto(BASE); await p.fill('#email', email); await p.fill('#pw', 'Demo!1234'); await p.click('button.primary'); await p.waitForSelector('.shell'); return p; };
  // yanlış parola
  { const ctx = await b.newContext(); const p = await ctx.newPage(); await p.goto(BASE); await p.fill('#email', 'sekreter@sinavplan.test'); await p.fill('#pw', 'yanlis'); await p.click('button.primary'); await p.waitForSelector('.alert.err'); ok((await p.textContent('.alert.err')).includes('hatalı'), 'yanlış parola hata kutusu'); await ctx.close(); }
  // öğrenci kartı (mobil genişlik, yatay taşma yok)
  { const p = await login('ogrenci@sinavplan.test', { width: 390, height: 800 }); await p.waitForSelector('.card button.primary'); await p.click('.card button.primary >> nth=-1'); await p.waitForSelector('[data-testid=qr-svg], .examcard .banner', { timeout: 8000 });
    const tok = await p.$('[data-testid=qr-svg]'); const al = await p.$('.examcard .banner');
    ok(!!tok || !!al, 'sınav kartı: jeton ya da mazeret uyarısı'); 
    const ov = await p.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1); ok(!ov, 'mobil 390px: yatay taşma yok');
    await p.screenshot({ path: './shots/mobile-student.png', fullPage: true }); }
  // sekreter: oturma planı sınav seçimi
  { const p = await login('sekreter@sinavplan.test', { width: 1280, height: 900 }); await p.goto(BASE + '/#/salonlar'); await p.click('button:has-text("Sınav oturma planı")'); await p.waitForSelector('select[aria-label=Sınav] option:nth-child(3)', { state: 'attached' }); await p.selectOption('select[aria-label=Sınav]', { index: 3 }); await p.waitForSelector('.seatmap', { timeout: 8000 });
    ok((await p.$$('.seat.taken, .seat.dis')).length > 0, 'oturma planında dolu koltuklar çizildi'); await p.screenshot({ path: './shots/sekreter-plan.png', fullPage: false });
    // içe aktarma: hatalı CSV
    await p.goto(BASE + '/#/ice-aktar'); await p.fill('textarea', 'student_no,full_name,department_code,disability\n9,Hatalı,BLM,false\n'); await p.click('text=İçe aktar'); await p.waitForSelector('text=İçe aktarma raporu'); ok((await p.textContent('main')).includes('1 hata'), 'içe aktarma satır hatası raporlandı'); }
  // gözetmen tableti: görev seç, liste yüklensin, çevrimdışı kuyruk, senkron
  { const p = await login('gozetmen@sinavplan.test', { width: 1024, height: 800 }); await p.goto(BASE + '/#/yoklama'); await p.waitForSelector('text=Yoklama al'); await p.click('text=Yoklama al >> nth=0'); await p.waitForSelector('text=Yoklama listesi'); await p.waitForSelector('button:text-is("GELDİ")', { timeout: 8000 }); ok(true, 'tablet: görev seçilince liste geldi');
    await p.click('button:text-is("GELDİ") >> nth=0'); ok((await p.textContent('.head')).includes('1 senkron bekliyor'), 'çevrimdışı kuyruğa olay eklendi');
    await p.click('button:has-text("Senkronla")'); await p.waitForSelector('text=kayıt eşitlendi', { timeout: 8000 }); ok(true, 'senkron tamamlandı'); }
  await b.close(); process.exit(fail ? 1 : 0);
})();
