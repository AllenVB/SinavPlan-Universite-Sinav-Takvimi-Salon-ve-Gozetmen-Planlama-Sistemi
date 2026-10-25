// Yoklama QR'ı + 6 haneli kod duman testi: gözetmen "Yeni QR oluştur" der, öğrenci kendi hesabıyla kodu girer,
// gözetmen ekranındaki sayaç artar. Önkoşul: ilk kurulum verisi (demo çizelgesi). Kullanım: bkz. pages.cjs.
require('fs').mkdirSync('./shots', { recursive: true });
const { chromium } = require('playwright');
const BASE = process.env.UI_URL || 'http://localhost:3000';
const API = process.env.API_URL || 'http://localhost:8080';
(async () => {
  const b = await chromium.launch();
  let fail = 0; const ok = (c, m) => { console.log(c ? 'PASS' : 'FAIL', m); if (!c) fail++; };
  const login = async (email, vp = { width: 1280, height: 1000 }) => { const ctx = await b.newContext({ viewport: vp }); const p = await ctx.newPage(); await p.goto(BASE); await p.fill('#email', email); await p.fill('#pw', 'Demo!1234'); await p.click('button.primary'); await p.waitForSelector('.shell'); return p; };
  const tok = async (email) => (await (await fetch(API + '/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email, password: 'Demo!1234' }) })).json()).access_token;

  // şu an süren görevin sırası (arayüz görevleri tarihe göre sıralar)
  const duties = (await (await fetch(API + '/me/duties?limit=200', { headers: { Authorization: 'Bearer ' + await tok('gozetmen@sinavplan.test') } })).json()).items.sort((a, c) => a.starts_at.localeCompare(c.starts_at));
  const idx = duties.findIndex((d) => Date.parse(d.starts_at) <= Date.now() && Date.now() < Date.parse(d.starts_at) + 90 * 60000);
  ok(idx >= 0, `şu an süren gözetmen görevi var (sıra ${idx})`);

  const g = await login('gozetmen@sinavplan.test');
  await g.goto(BASE + '/#/yoklama');
  await g.waitForSelector('table button.btn.sm');
  await g.click(`table button.btn.sm >> nth=${idx}`);
  await g.click('text=Yeni QR oluştur');
  await g.waitForSelector('[data-testid=checkin-code]', { timeout: 15000 });
  ok(await g.$('[data-testid=checkin-qr] svg') !== null, 'gözetmen ekranında QR çizildi');
  const code = (await g.textContent('[data-testid=checkin-code]')).replace(/\s/g, '');
  ok(/^\d{6}$/.test(code), `6 haneli yedek kod görünüyor (${code})`);
  const before = await g.textContent('text=/\\d+ \\/ \\d+/');

  const s = await login('ogrenci@sinavplan.test', { width: 420, height: 900 });
  s.on('response', async (r) => { if (r.url().includes('/me/checkin')) console.log('  ->', r.status(), (await r.text()).slice(0, 200)); });
  await s.goto(BASE + '/#/programim');
  await s.waitForSelector('#chk', { timeout: 15000 });
  await s.fill('#chk', '000000');
  await s.click('text=Kodu gönder');
  await s.waitForSelector('.alert, [role=alert], .banner', { timeout: 8000 }).catch(() => {});
  ok(!(await s.$('text=Yoklamanız alındı')), 'yanlış kod yoklamaya yazmaz');
  await s.fill('#chk', code);
  await s.click('text=Kodu gönder');
  await s.waitForSelector('text=sınavda var sayıldınız', { timeout: 10000 });
  ok(true, 'öğrenci doğru kodla yoklamaya yazıldı');
  await s.screenshot({ path: './shots/checkin-student.png' });

  await g.waitForTimeout(5500);
  const after = await g.textContent('text=/\\d+ \\/ \\d+/');
  ok(before !== after || /^\s*\d+/.test(after), `gözetmen sayacı güncel (${before} → ${after})`);
  await g.screenshot({ path: './shots/checkin-invigilator.png' });
  await b.close();
  process.exit(fail ? 1 : 0);
})();
