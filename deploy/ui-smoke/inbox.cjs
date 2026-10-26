// Bildirim kutusu: her rolün üst çubuğunda; öğrenci için hatırlatma/"yoklamanız alınmadı" üretilir, tıklayınca okundu olur ve ilgili ekrana gider.
const { chromium } = require('playwright'); const BASE = process.env.UI_URL || 'http://localhost:3000';
(async () => {
  require('fs').mkdirSync('./shots', { recursive: true });
  // Hazırlık: öğrenci mazeret bildirir, sekreter karara bağlar → öğrenciye "Mazeretiniz onaylandı" bildirimi düşer
  const API = process.env.API_URL || 'http://localhost:8080';
  const tok = async (email) => (await (await fetch(API + '/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email, password: 'Demo!1234' }) })).json()).access_token;
  const H = (t) => ({ Authorization: 'Bearer ' + t, 'Content-Type': 'application/json' });
  const st = await tok('ogrenci@sinavplan.test'), sc = await tok('seed.mercan@ddo-case.invalid');
  const exams = (await (await fetch(API + '/me/exams?limit=200', { headers: H(st) })).json()).items;
  for (const e of exams.slice().reverse()) {
    const r = await fetch(API + '/excuses', { method: 'POST', headers: H(st), body: JSON.stringify({ exam_id: e.exam_id, reason: 'Duman testi: sağlık raporu' }) });
    if (r.status === 201) { const x = await r.json(); await fetch(API + `/excuses/${x.id}/decision`, { method: 'POST', headers: { ...H(sc), 'Idempotency-Key': 'inbox-' + x.id }, body: JSON.stringify({ decision: 'approved', note: 'Rapor geçerli' }) }); break; }
  }
  const b = await chromium.launch(); let fail = 0; const ok = (c, m) => { console.log(c ? 'PASS' : 'FAIL', m); if (!c) fail++; };
  for (const [who, email] of [['öğrenci', 'ogrenci@sinavplan.test'], ['gözetmen', 'gozetmen@sinavplan.test'], ['sekreter', 'seed.mercan@ddo-case.invalid']]) {
    const p = await (await b.newContext({ viewport: { width: 1280, height: 900 } })).newPage();
    await p.goto(BASE); await p.fill('#email', email); await p.fill('#pw', 'Demo!1234'); await p.click('button.primary'); await p.waitForSelector('.shell');
    ok(await p.waitForSelector('[data-testid=inbox-btn]', { timeout: 8000 }).then(() => true, () => false), `${who}: bildirim kutusu üst çubukta`);
    await p.waitForTimeout(1200);
    const n = Number((await p.textContent('[data-testid=inbox-count]').catch(() => '0')) || 0);
    await p.click('[data-testid=inbox-btn]'); await p.waitForSelector('[data-testid=inbox-panel]');
    const items = await p.$$eval('.inboxitem', (x) => x.map((i) => i.textContent.slice(0, 70)));
    console.log(`  ${who}: ${n} okunmamış, ${items.length} bildirim`, items[0] ?? '');
    if (who === 'öğrenci') await p.screenshot({ path: './shots/inbox.png' });
    if (who === 'öğrenci') ok(items.some((t) => t.includes('Mazeretiniz onaylandı')), 'öğrenci: mazeret kararı bildirimi kutuda');
    if (items.length) {
      await p.click('.inboxitem >> nth=0'); await p.waitForTimeout(700);
      ok((await p.evaluate(() => location.hash)).length > 2, `${who}: bildirime tıklayınca ilgili ekran açıldı (${await p.evaluate(() => location.hash)})`);
      const count = async () => Number((await p.textContent('[data-testid=inbox-count]', { timeout: 500 }).catch(() => '0')) || 0)
      for (let i = 0; i < 10 && (await count()) > Math.max(0, n - 1); i++) await p.waitForTimeout(400) // tazeleme gelsin
      const n2 = await count();
      ok(n2 === Math.max(0, n - 1) || n === 0, `${who}: okunmamış sayısı düştü (${n} → ${n2})`);
    }
    await p.keyboard.press('Escape');
  }
  await b.close(); process.exit(fail ? 1 : 0);
})();
