// Faz 22 C1 — yazdırılabilir föyler: Salon ve oturma planı → Föyler. Kapı listesi satırı = o salonda oturan
// öğrenci sayısı (API'den bağımsız toplanır), baskı görünümünde yalnız föyler kalır, PDF ≥ 3 sayfa (her föy
// ayrı sayfa), gözetmen yalnız kendi görevlerini seçebilir. PDF ve ekran görüntüsü ./shots/ altına.
const { chromium } = require('playwright');
const BASE = process.env.UI_URL || 'http://localhost:3000';
const API = process.env.API_URL || 'http://localhost:8080';
(async () => {
  const b = await chromium.launch();
  let fail = 0; const ok = (c, m) => { console.log(c ? 'PASS' : 'FAIL', m); if (!c) fail++; };
  const tok = async (email) => (await (await fetch(API + '/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email, password: 'Demo!1234' }) })).json()).access_token;
  const login = async (email) => { const p = await (await b.newContext({ viewport: { width: 1280, height: 1000 } })).newPage(); await p.goto(BASE); await p.fill('#email', email); await p.fill('#pw', 'Demo!1234'); await p.click('button.primary'); await p.waitForSelector('.shell'); return p; };

  const p = await login('seed.mercan@ddo-case.invalid');
  await p.goto(BASE + '/#/salonlar'); await p.click('.tabs button:has-text("Föyler")');
  await p.waitForSelector('[data-testid=sheets] [data-sheet=door] tbody tr', { timeout: 20000 });
  const sched = await p.$eval('select[aria-label="Çizelge sürümü"]', (s) => s.value);
  const slot = await p.$eval('#sh-slot', (s) => s.value); const room = await p.$eval('#sh-room', (s) => s.value);
  const shares = (await (await fetch(`${API}/schedules/${sched}/rooms?slot_id=${slot}&limit=200`, { headers: { Authorization: 'Bearer ' + await tok('seed.mercan@ddo-case.invalid') } })).json()).items.filter((s) => String(s.room_id) === room);
  const want = shares.reduce((a, s) => a + s.students, 0);
  const door = await p.$$eval('[data-sheet=door] tbody tr', (r) => r.length);
  const sign = await p.$$eval('[data-sheet=sign] tbody tr', (r) => r.length);
  ok(door === want && sign === want, `kapı listesi ${door} ve imza föyü ${sign} satır = salonda oturan ${want}`);
  const nos = await p.$$eval('[data-sheet=door] tbody tr td:first-child', (t) => t.map((x) => x.textContent));
  ok(nos.every((n, i) => i === 0 || nos[i - 1].localeCompare(n) <= 0), 'kapı listesi öğrenci numarasına göre sıralı');
  ok(!!(await p.$('[data-sheet=door] [data-testid=room-door]')), 'kapı listesinde kapılı kroki var');
  ok((await p.textContent('[data-sheet=duty]')).includes('Olay tutanağı'), 'gözetmen görev kâğıdı: talimat + tutanak');
  await p.screenshot({ path: './shots/sheets.png', fullPage: false });
  if (process.env.AXE_PATH) {
    await p.addScriptTag({ content: require('fs').readFileSync(process.env.AXE_PATH, 'utf8') });
    const v = await p.evaluate(async () => (await window.axe.run(document, { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa'] } })).violations.filter((x) => x.impact === 'serious' || x.impact === 'critical').map((x) => `${x.id}×${x.nodes.length}`));
    ok(v.length === 0, `föyler a11y ciddi bulgu yok (${v.join(',') || '0'})`);
  }

  await p.emulateMedia({ media: 'print' });
  const vis = await p.evaluate(() => ({ side: getComputedStyle(document.querySelector('.side')).display, card: getComputedStyle(document.querySelector('.page > .card, .page > section.card')).display, sheet: getComputedStyle(document.querySelector('.sheet')).display }));
  ok(vis.side === 'none' && vis.card === 'none' && vis.sheet !== 'none', `baskı görünümünde yalnız föyler (menü ${vis.side}, denetimler ${vis.card})`);
  const pdf = await p.pdf({ format: 'A4', path: './shots/foyler.pdf' });
  const pages = (pdf.toString('latin1').match(/\/Type\s*\/Page[^s]/g) || []).length;
  ok(pages >= 3, `PDF ${pages} sayfa (her föy ayrı sayfa; ≥3)`);
  await p.emulateMedia({ media: 'screen' });

  const g = await login('gozetmen@sinavplan.test');
  await g.goto(BASE + '/#/salonlar'); await g.click('.tabs button:has-text("Föyler")');
  await g.waitForSelector('#sh-slot option', { state: 'attached', timeout: 15000 });
  const opts = await g.$$eval('#sh-slot option', (o) => o.length);
  const mine = new Set((await (await fetch(`${API}/me/duties?limit=200`, { headers: { Authorization: 'Bearer ' + await tok('gozetmen@sinavplan.test') } })).json()).items.map((d) => d.slot_id)).size;
  ok(opts === mine, `gözetmen yalnız kendi ${mine} görev oturumunu görür (seçenek ${opts})`);
  await g.waitForSelector('[data-sheet=door] tbody tr', { timeout: 20000 }).then(() => ok(true, 'gözetmen kendi salonunun föyünü alır'), () => ok(false, 'gözetmen föyü yüklenmedi'));
  await b.close(); process.exit(fail ? 1 : 0);
})();
