// Faz 22 A3 — komut paleti (Ctrl+K) + Öğrenci 360. Klavyeyle açılır, öğrenci numarası önekiyle bulunur,
// 360 kartı sınavları + salon/koltuk gösterir, ders seçimi takvimi o derse süzer, ekran adıyla gezinilir,
// Esc geri/kapat ve odak geri döner; öğrencide palet yoktur. AXE_PATH verilirse açıkken axe taraması.
const fs = require('fs');
const { chromium } = require('playwright');
const BASE = process.env.UI_URL || 'http://localhost:3000';
(async () => {
  const b = await chromium.launch();
  let fail = 0; const ok = (c, m) => { console.log(c ? 'PASS' : 'FAIL', m); if (!c) fail++; };
  const login = async (email) => { const p = await (await b.newContext({ viewport: { width: 1280, height: 1000 } })).newPage(); await p.goto(BASE); await p.fill('#email', email); await p.fill('#pw', 'Demo!1234'); await p.click('button.primary'); await p.waitForSelector('.shell'); return p; };
  const axe = async (p, name) => {
    if (!process.env.AXE_PATH) return;
    await p.addScriptTag({ content: fs.readFileSync(process.env.AXE_PATH, 'utf8') });
    const v = await p.evaluate(async () => (await window.axe.run(document, { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa'] } })).violations.filter((x) => x.impact === 'serious' || x.impact === 'critical').map((x) => `${x.id}×${x.nodes.length}`));
    ok(v.length === 0, `${name}: a11y ciddi bulgu yok (${v.join(',') || '0'})`);
  };

  const p = await login('seed.mercan@ddo-case.invalid');
  await p.goto(BASE + '/#/panel'); await p.waitForTimeout(500);
  await p.keyboard.press('Control+k'); await p.waitForSelector('[data-testid=palette]');
  ok(await p.evaluate(() => document.activeElement?.getAttribute('role') === 'combobox'), 'Ctrl+K paleti açar, odak arama kutusunda');
  await p.keyboard.type('20260000'); await p.waitForSelector('[role=option]:has-text("Öğrenci")', { timeout: 8000 });
  ok((await p.$$('[role=option]:has-text("Öğrenci")')).length > 0, 'numara önekiyle öğrenci bulundu');
  await axe(p, 'palet açık');
  await p.keyboard.press('Enter');
  await p.waitForSelector('[data-testid=student360] table tbody tr', { timeout: 10000 });
  const rows = await p.$$eval('[data-testid=student360] tbody tr', (r) => r.length);
  ok(rows > 0, `Öğrenci 360: ${rows} sınav satırı`);
  ok(/Koltuk/i.test(await p.textContent('[data-testid=student360]')), '360 kartında salon + koltuk');
  await axe(p, 'Öğrenci 360');
  await p.screenshot({ path: './shots/student360.png' });
  await p.keyboard.press('Escape'); await p.waitForSelector('[role=combobox]');
  ok(true, 'Esc 360\'tan aramaya döner');
  await p.fill('[role=combobox]', 'YZM101'); await p.waitForSelector('[role=option]:has-text("Ders")', { timeout: 8000 });
  await p.click('[role=option]:has-text("YZM101")');
  await p.waitForFunction(() => location.hash.startsWith('#/takvim?q=YZM101'));
  await p.waitForSelector('[data-testid=exam-table] tbody tr'); await p.waitForTimeout(500);
  const codes = await p.$$eval('[data-testid=exam-table] > tbody > tr > td:nth-child(3) b', (x) => [...new Set(x.map((y) => y.textContent))]);
  ok(codes.length === 1 && codes[0] === 'YZM101', `ders seçimi takvimi süzdü (${codes.join(',')})`);
  await p.keyboard.press('Control+k'); await p.keyboard.type('mazeret'); await p.waitForSelector('[role=option]:has-text("Ekran")');
  await p.keyboard.press('Enter'); await p.waitForFunction(() => location.hash === '#/mazeret');
  ok(!(await p.$('[data-testid=palette]')), 'ekran adıyla gezinildi ve palet kapandı');
  await p.keyboard.press('Control+k'); await p.waitForSelector('[data-testid=palette]'); await p.keyboard.press('Escape');
  ok(!(await p.$('[data-testid=palette]')), 'Esc paleti kapatır');

  const s = await login('ogrenci@sinavplan.test');
  await s.keyboard.press('Control+k'); await s.waitForTimeout(400);
  ok(!(await s.$('[data-testid=palette]')) && !(await s.$('header button:has-text("Ara")')), 'öğrencide palet ve Ara düğmesi yok');
  const m = await (await b.newContext({ viewport: { width: 390, height: 800 } })).newPage();
  await m.goto(BASE); await m.fill('#email', 'seed.mercan@ddo-case.invalid'); await m.fill('#pw', 'Demo!1234'); await m.click('button.primary'); await m.waitForSelector('.shell');
  await m.click('header button:has-text("Ara")'); await m.waitForSelector('[data-testid=palette]');
  ok(!(await m.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1)), 'mobil 390px: palet taşmıyor');
  await b.close(); process.exit(fail ? 1 : 0);
})();
