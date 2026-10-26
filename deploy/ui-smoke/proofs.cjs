// Faz 22 A2 — Kanıtlar sayfası: Veri ve kanıtlar → Kanıtlar. 12 denetimin hepsi geçer (gerçek plan 0 ihlal,
// bozulmuş sanal plan yakalanır, T-5 yazma reddi), yeniden çalıştırılabilir. AXE_PATH verilirse axe taraması.
const fs = require('fs');
const { chromium } = require('playwright');
const BASE = process.env.UI_URL || 'http://localhost:3000';
(async () => {
  const b = await chromium.launch();
  let fail = 0; const ok = (c, m) => { console.log(c ? 'PASS' : 'FAIL', m); if (!c) fail++; };
  const p = await (await b.newContext({ viewport: { width: 1280, height: 1000 } })).newPage();
  await p.goto(BASE); await p.fill('#email', 'sekreter@sinavplan.test'); await p.fill('#pw', 'Demo!1234'); await p.click('button.primary'); await p.waitForSelector('.shell');
  await p.goto(BASE + '/#/ice-aktar'); await p.click('.tabs button:has-text("Kanıtlar")');
  await p.waitForSelector('[data-testid=proofs-summary]', { timeout: 30000 });
  const sum = await p.textContent('[data-testid=proofs-summary]');
  const m = sum.match(/(\d+) \/ (\d+) denetim geçti/);
  ok(m && m[1] === m[2] && Number(m[2]) >= 12, `özet: ${sum.trim().slice(0, 90)}`);
  const rows = await p.$$eval('tr[data-proof]', (r) => r.map((x) => [x.dataset.proof, x.className.includes('tone-err')]));
  ok(rows.length >= 12 && rows.every(([, bad]) => !bad), `${rows.length} kural satırı, kırmızı yok`);
  ok(['T-1', 'T-2', 'T-3', 'T-4', 'T-5'].every((t) => rows.some(([id]) => id.startsWith(t))), 'T-1..T-5 hepsi temsil ediliyor');
  ok((await p.textContent('main')).includes('reddedildi'), 'T-5: uygulama rolünün yazma denemesi reddedildi');
  await p.click('button:has-text("Yeniden çalıştır")'); await p.waitForSelector('button:has-text("Yeniden çalıştır"):not([disabled])', { timeout: 30000 });
  ok((await p.textContent('[data-testid=proofs-summary]')).includes('denetim geçti'), 'yeniden çalıştırıldı');
  await p.goto(BASE + '/#/kanitlar'); await p.waitForSelector('[data-testid=proofs-summary]', { timeout: 30000 });
  ok(true, '#/kanitlar doğrudan bağlantısı Kanıtlar sekmesini açar');
  await p.screenshot({ path: './shots/proofs.png', fullPage: false });
  if (process.env.AXE_PATH) {
    await p.addScriptTag({ content: fs.readFileSync(process.env.AXE_PATH, 'utf8') });
    const v = await p.evaluate(async () => (await window.axe.run(document, { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa'] } })).violations.filter((x) => x.impact === 'serious' || x.impact === 'critical').map((x) => `${x.id}×${x.nodes.length}`));
    ok(v.length === 0, `kanıtlar a11y ciddi bulgu yok (${v.join(',') || '0'})`);
  }
  await b.close(); process.exit(fail ? 1 : 0);
})();
