// Faz 22 A1 — rehberli tur: 8 adım, gerçek ekranlara gider, başka rolün adımında tek tıkla hesap değiştirir,
// hesap değişince kaldığı yerden sürer, Esc ile kapanır. AXE_PATH verilirse tur açıkken axe taraması da yapılır.
const fs = require('fs');
const { chromium } = require('playwright');
const BASE = process.env.UI_URL || 'http://localhost:3000';
(async () => {
  const b = await chromium.launch();
  let fail = 0; const ok = (c, m) => { console.log(c ? 'PASS' : 'FAIL', m); if (!c) fail++; };
  const p = await (await b.newContext({ viewport: { width: 1280, height: 900 } })).newPage();
  await p.goto(BASE); await p.fill('#email', 'sekreter@sinavplan.test'); await p.fill('#pw', 'Demo!1234'); await p.click('button.primary'); await p.waitForSelector('.shell');
  ok(!(await p.$('[data-testid=tour]')), 'tur kendiliğinden açılmaz');
  await p.click('header button:has-text("Tur")'); await p.waitForSelector('[data-testid=tour]');
  ok((await p.textContent('[data-testid=tour]')).includes('1 / 9'), 'üst çubuktan açıldı: adım 1 / 9');
  await p.click('[data-testid=tour] button:has-text("İleri")');
  await p.waitForSelector('button:has-text("Takvimi oluştur")', { timeout: 8000 });
  ok(p.url().includes('#/takvim?tab=uret'), 'adım 2 takvimin Üret sekmesini açtı');
  for (let k = 0; k < 3; k++) await p.click('[data-testid=tour] button:has-text("İleri")');
  await p.waitForSelector('.roomcard, text=Sınav günü', { timeout: 8000 }).catch(() => {});
  ok(p.url().includes('#/canli'), 'adım 5 canlı izlemeyi açtı');
  await p.click('[data-testid=tour] button:has-text("İleri")');
  await p.waitForSelector('[data-testid=tour-switch]'); ok((await p.textContent('[data-testid=tour]')).includes('gozetmen@sinavplan.test'), 'adım 6 gözetmen hesabını istiyor');
  await p.click('[data-testid=tour-switch]');
  await p.waitForSelector('text=Ömer ASLAN', { timeout: 10000 }).then(() => ok(true, 'tek tıkla gözetmen hesabına geçildi'), () => ok(false, 'gözetmen hesabına geçilemedi'));
  await p.waitForTimeout(800);
  ok(p.url().includes('#/yoklama') && (await p.textContent('[data-testid=tour]')).includes('6 / 9'), 'hesap değişince tur aynı adımdan sürdü (#/yoklama, 6 / 9)');
  if (process.env.AXE_PATH) {
    await p.addScriptTag({ content: fs.readFileSync(process.env.AXE_PATH, 'utf8') });
    const v = await p.evaluate(async () => (await window.axe.run(document, { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa'] } })).violations.filter((x) => x.impact === 'serious' || x.impact === 'critical').map((x) => x.id));
    ok(v.length === 0, `tur açıkken a11y ciddi bulgu yok (${v.join(',') || '0'})`);
  }
  await p.keyboard.press('Escape'); await p.waitForTimeout(200);
  ok(!(await p.$('[data-testid=tour]')), 'Esc turu kapatır');
  const m = await (await b.newContext({ viewport: { width: 390, height: 800 } })).newPage();
  await m.goto(BASE); await m.fill('#email', 'sekreter@sinavplan.test'); await m.fill('#pw', 'Demo!1234'); await m.click('button.primary'); await m.waitForSelector('.shell');
  await m.click('header button:has-text("Tur")'); await m.waitForSelector('[data-testid=tour]');
  ok(!(await m.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1)), 'mobil 390px: tur paneli taşmıyor');
  await m.screenshot({ path: './shots/tour-mobile.png' });
  await p.screenshot({ path: './shots/tour.png' });
  await b.close(); process.exit(fail ? 1 : 0);
})();
