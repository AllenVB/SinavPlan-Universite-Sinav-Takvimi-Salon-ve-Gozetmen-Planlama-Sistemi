// Elle gözetmen ataması (sekreter): görev listesinde "Değiştir" formu doldurur; önizleme kuralları ve yük farkını
// gösterir; kuralı bozan aday reddedilir; uygun aday "Uygula" ile yazılır ve listede görünür. Kullanım: bkz. pages.cjs.
const { chromium } = require('playwright'); const BASE = process.env.UI_URL || 'http://localhost:3000';
(async () => {
  require('fs').mkdirSync('./shots', { recursive: true });
  const b = await chromium.launch(); let fail = 0; const ok = (c, m) => { console.log(c ? 'PASS' : 'FAIL', m); if (!c) fail++; };
  const p = await (await b.newContext({ viewport: { width: 1280, height: 1000 } })).newPage();
  await p.goto(BASE); await p.fill('#email', 'seed.mercan@ddo-case.invalid'); await p.fill('#pw', 'Demo!1234'); await p.click('button.primary'); await p.waitForSelector('.shell');
  await p.goto(BASE + '/#/gozetmenler'); await p.waitForSelector('[data-testid=manual-assign]', { timeout: 20000 });
  ok(true, 'Gözetmen atama ekranında "Elle atama" kartı var');
  await p.waitForSelector('button[aria-label$="görevini değiştir"]');
  const label = await p.getAttribute('button[aria-label$="görevini değiştir"] >> nth=0', 'aria-label');
  const oldName = label.replace(' görevini değiştir', '');
  await p.click('button[aria-label$="görevini değiştir"] >> nth=0'); await p.waitForTimeout(300);
  ok((await p.$eval('#ma-slot', (s) => s.value)) !== '0' && (await p.$eval('#ma-room', (s) => s.value)) !== '0', 'Değiştir: oturum ve salon dolduruldu');
  ok((await p.$eval('#ma-mode', (s) => s.selectedOptions[0].textContent)).includes(oldName), `Değiştir: "${oldName} yerine" seçildi`);
  const opts = await p.$$eval('#ma-user option', (o) => o.map((x) => x.value).filter((v) => v !== '0'));
  let bad = null, good = null;
  for (const v of opts) {
    if (bad && good) break;
    await p.selectOption('#ma-user', v); await p.click('button:has-text("Önizle")'); await p.waitForSelector('[data-testid=ma-preview]');
    const t = await p.textContent('[data-testid=ma-preview]');
    if (!bad && t.includes('yapılamaz')) bad = { v, t };
    if (!good && t.includes('Kurallara uygun')) good = { v, t };
  }
  ok(!!bad, `kuralı bozan aday reddedildi: ${bad ? bad.t.replace(/\s+/g, ' ').slice(0, 110) : ''}`);
  if (bad) { await p.selectOption('#ma-user', bad.v); await p.click('button:has-text("Önizle")'); await p.waitForSelector('[data-testid=ma-preview]'); ok(await p.$eval('[data-testid=ma-apply]', (x) => x.disabled), 'ihlalde "Uygula" kapalı'); }
  ok(!!good, `uygun aday önizlemesi: ${good ? good.t.replace(/\s+/g, ' ').slice(0, 120) : ''}`);
  if (good) {
    await p.selectOption('#ma-user', good.v); await p.click('button:has-text("Önizle")'); await p.waitForSelector('[data-testid=ma-preview]');
    const newName = (await p.$eval('#ma-user', (s) => s.selectedOptions[0].textContent)).replace(/ \(\d+ görev\)$/, '');
    await p.screenshot({ path: './shots/manual-assign.png' });
    await p.click('[data-testid=ma-apply]'); await p.waitForSelector('text=Atama yapıldı', { timeout: 8000 });
    ok(true, `uygulandı: ${oldName} → ${newName}`);
    await p.waitForTimeout(800);
    const now = await p.textContent('[data-testid=manual-assign]');
    ok(now.includes(newName) && !now.includes(`${oldName},`) , 'görevde artık yeni gözetmen görünüyor');
  }
  await b.close(); process.exit(fail ? 1 : 0);
})();
