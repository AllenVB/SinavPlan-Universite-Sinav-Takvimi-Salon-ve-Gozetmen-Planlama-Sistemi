// Bildirimler tıklanabilir: ana panel olay satırları ilgili ekranı açar; canlı izleme uyarıları salon kartına odaklanır.
const { chromium } = require('playwright'); const BASE = process.env.UI_URL || 'http://localhost:3000';
(async () => {
  const b = await chromium.launch(); let fail=0; const ok=(c,m)=>{console.log(c?'PASS':'FAIL',m); if(!c) fail++;};
  const p = await (await b.newContext({ viewport: { width: 1280, height: 1000 } })).newPage();
  await p.goto(BASE); await p.fill('#email','seed.mercan@ddo-case.invalid'); await p.fill('#pw','Demo!1234'); await p.click('button.primary'); await p.waitForSelector('.shell');
  await p.waitForSelector('.feedlink'); const h = await p.$$eval('.feedlink', (a) => a.map((x) => x.getAttribute('href')));
  ok(h.length > 0 && h.every((x) => /^#\/[a-z-]+$/.test(x)), `ana panel olayları bağlantı: ${[...new Set(h)].join(' ')}`);
  const want = h[0]; await p.click('.feedlink >> nth=0'); await p.waitForTimeout(600); ok((await p.evaluate(() => location.hash)) === want, `olaya tıklayınca ${want} açıldı`);
  await p.goto(BASE + '/#/canli'); await p.waitForSelector('[data-testid=live-alerts]');
  const rows = await p.$$('.alertrow:not([disabled])'); ok(rows.length > 0, `uyarı satırı tıklanabilir (${rows.length})`);
  const code = await p.textContent('.alertrow:not([disabled]) .linkbtn'); await rows[0].click(); await p.waitForTimeout(900);
  ok(await p.$(`#room-${code}.flashcard`) !== null || await p.$(`#room-${code}`) !== null, `${code} salon kartı gösterildi/odaklandı`);
  ok(await p.evaluate((c) => document.activeElement?.id === `room-${c}`, code), 'odak salon kartında');
  await p.screenshot({ path: './shots/notif.png' });
  const ev = await p.$$('.feedlink:not([disabled])'); if (ev.length) { await ev[0].click(); await p.waitForTimeout(500); ok((await p.inputValue('input[aria-label=Ara]')) !== '', 'olaya tıklayınca liste o salona süzüldü'); }
  await b.close(); process.exit(fail?1:0);
})();
