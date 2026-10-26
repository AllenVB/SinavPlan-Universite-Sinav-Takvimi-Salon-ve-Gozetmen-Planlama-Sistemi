// Faz 22 B3 — yoğunluk haritası + "bu sınav neden burada?". Harita SQL toplamı = çözücü metriği rozetini
// gösterir; bir sınav satırında "Neden burada?" → oturum ızgarası (şu an / uygun / çakışma / günlük sınır),
// açıklama cümlesi, çakışma hücresine tıklayınca ortak öğrencili sınavlar. AXE_PATH verilirse axe taraması.
const fs = require('fs');
const { chromium } = require('playwright');
const BASE = process.env.UI_URL || 'http://localhost:3000';
(async () => {
  const b = await chromium.launch();
  let fail = 0; const ok = (c, m) => { console.log(c ? 'PASS' : 'FAIL', m); if (!c) fail++; };
  const p = await (await b.newContext({ viewport: { width: 1280, height: 1000 } })).newPage();
  await p.goto(BASE); await p.fill('#email', 'seed.mercan@ddo-case.invalid'); await p.fill('#pw', 'Demo!1234'); await p.click('button.primary'); await p.waitForSelector('.shell');
  await p.goto(BASE + '/#/takvim'); await p.waitForSelector('[data-testid=heatmap]', { timeout: 20000 });
  ok((await p.textContent('main')).includes('SQL toplamı = çözücü metriği'), 'ısı haritası: SQL toplamı çözücü metriğiyle aynı');
  const cells = await p.$$eval('[data-testid=heatmap] tbody tr', (r) => r.length);
  ok(cells === 5, `ısı haritası 3 oturum + 2 günlük satır (${cells})`);
  await p.click('[data-testid=exam-table] button:has-text("Neden burada?") >> nth=0');
  await p.waitForSelector('[data-testid=why-panel] .cellbtn', { timeout: 15000 });
  const kinds = await p.$$eval('[data-testid=why-panel] td', (t) => [...new Set(t.map((x) => x.className).filter(Boolean))]);
  ok(kinds.includes('w-cur') && kinds.includes('w-bad'), `oturum ızgarası durumları: ${kinds.join(' ')}`);
  ok(/uygun\./.test(await p.textContent('[data-testid=why-panel]')), 'açıklama cümlesi var');
  await p.click('[data-testid=why-panel] td.w-bad .cellbtn >> nth=0');
  ok((await p.textContent('[data-testid=why-panel] [role=status]')).includes('Ortak öğrenci'), 'çakışma hücresi: ortak öğrencili sınavlar listelendi');
  await p.screenshot({ path: './shots/why.png' });
  if (process.env.AXE_PATH) {
    await p.addScriptTag({ content: fs.readFileSync(process.env.AXE_PATH, 'utf8') });
    const v = await p.evaluate(async () => (await window.axe.run(document, { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa'] } })).violations.filter((x) => x.impact === 'serious' || x.impact === 'critical').map((x) => `${x.id}×${x.nodes.length}`));
    ok(v.length === 0, `harita + neden paneli a11y ciddi bulgu yok (${v.join(',') || '0'})`);
  }
  await b.close(); process.exit(fail ? 1 : 0);
})();
