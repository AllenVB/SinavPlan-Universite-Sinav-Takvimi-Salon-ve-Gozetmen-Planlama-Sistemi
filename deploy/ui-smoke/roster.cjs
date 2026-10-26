// Gözetmen programı (ana panel + gözetmen ekranı), gözetmenin "Kod üret" düğmesi (QR'sız 6 haneli kod) ve
// öğrencinin demo kodu 111111 ile yoklamaya yazılması duman testi. Önkoşul: yığın ayakta, DEMO_CHECKIN_CODE açık,
// süren bir sınav (make reset-demo). Kullanım: bkz. pages.cjs.
const { chromium } = require('playwright');
const API = process.env.API_URL || 'http://localhost:8080', BASE = process.env.UI_URL || 'http://localhost:3000';
require('fs').mkdirSync('./shots', { recursive: true });
const parts = [
(async () => {
  const b = await chromium.launch(); let fail = 0; const ok = (c, m) => { console.log(c ? 'PASS' : 'FAIL', m); if (!c) fail++; };
  const login = async (email, vp = { width: 1280, height: 1000 }) => { const p = await (await b.newContext({ viewport: vp })).newPage(); await p.goto(BASE); await p.fill('#email', email); await p.fill('#pw', 'Demo!1234'); await p.click('button.primary'); await p.waitForSelector('.shell'); return p; };
  const s = await login('ogrenci@sinavplan.test', { width: 420, height: 900 });
  await s.goto(BASE + '/#/programim'); await s.waitForSelector('[data-testid=demo-code-hint]', { timeout: 10000 });
  ok(true, 'öğrenci ekranında demo kodu yayınlandı');
  await s.click('[data-testid=demo-code-hint] button'); ok((await s.inputValue('#chk')) === '111111', 'koda tıklayınca alan doluyor');
  await s.click('text=Kodu gönder'); await s.waitForSelector('text=/var sayıldınız|zaten alınmıştı/', { timeout: 10000 }); ok(true, '111111 → yoklamada var sayıldı');
  await s.screenshot({ path: './shots/demo-student.png' });
  const m = await login('seed.mercan@ddo-case.invalid');
  await m.waitForSelector('[data-testid=duty-roster] tbody tr', { timeout: 15000 }); const n1 = await m.$$eval('[data-testid=duty-roster] tbody tr', (r) => r.length);
  ok(n1 > 0 && n1 <= 8, `ana panelde gözetmen programı (${n1} satır, özet)`);
  ok(/\d{2}\.\d{2}/.test(await m.textContent('[data-testid=duty-roster] tbody tr')), 'satırda tarih/saat + salon var');
  await m.screenshot({ path: './shots/demo-dash.png', fullPage: true });
  await m.goto(BASE + '/#/gozetmenler'); await m.waitForSelector('[data-testid=duty-roster] tbody tr', { timeout: 15000 });
  const n2 = await m.$$eval('[data-testid=duty-roster] tbody tr', (r) => r.length); ok(n2 > 8, `gözetmen ekranında tam liste (${n2} gözetmen)`);
  await m.fill('input[aria-label="Gözetmen programında ara"]', 'Ömer'); await m.waitForTimeout(300);
  ok((await m.$$eval('[data-testid=duty-roster] tbody tr', (r) => r.length)) >= 1, 'arama çalışıyor');
  await m.screenshot({ path: './shots/demo-inv.png' });
  await b.close(); return fail;
}),
(async () => {
  const b = await chromium.launch(); let fail=0; const ok=(c,m)=>{console.log(c?'PASS':'FAIL',m); if(!c) fail++;};
  const tok = async (email) => (await (await fetch(API + '/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email, password: 'Demo!1234' }) })).json()).access_token;
  const duties = (await (await fetch(API + '/me/duties?limit=200', { headers: { Authorization: 'Bearer ' + await tok('gozetmen@sinavplan.test') } })).json()).items.sort((a, c) => a.starts_at.localeCompare(c.starts_at));
  const idx = duties.findIndex((d) => Date.parse(d.starts_at) <= Date.now() && Date.now() < Date.parse(d.starts_at) + 90 * 60000);
  ok(idx >= 0, 'süren görev var');
  const p = await (await b.newContext({ viewport: { width: 1280, height: 1000 } })).newPage();
  await p.goto(BASE); await p.fill('#email', 'gozetmen@sinavplan.test'); await p.fill('#pw', 'Demo!1234'); await p.click('button.primary'); await p.waitForSelector('.shell');
  await p.goto(BASE + '/#/yoklama'); await p.waitForSelector('table button.btn.sm'); await p.click(`table button.btn.sm >> nth=${idx}`);
  await p.click('[data-testid=gen-code]'); await p.waitForSelector('[data-testid=checkin-code]', { timeout: 15000 });
  const code1 = (await p.textContent('[data-testid=checkin-code]')).replace(/\s/g, '');
  ok(/^\d{6}$/.test(code1), `6 haneli kod üretildi (${code1})`);
  ok(!(await p.$('[data-testid=checkin-qr]')), 'kod modunda QR gösterilmiyor');
  await p.click('button:has-text("Yeni kod üret")'); await p.waitForFunction((c) => document.querySelector("[data-testid=checkin-code]").textContent.replace(/\s/g,"") !== c, code1, { timeout: 8000 }).catch(() => {});
  const code2 = (await p.textContent('[data-testid=checkin-code]')).replace(/\s/g, ''); ok(code2 !== code1 && /^\d{6}$/.test(code2), `yeni kod üretildi (${code2})`);
  await p.click('text=QR göster'); await p.waitForSelector('[data-testid=checkin-qr] svg'); ok(true, '"QR göster" ile QR açıldı');
  await p.screenshot({ path: './shots/code-mode.png' }); await b.close(); return fail;
}),
(async () => {
  // Çan: "Çakışmalar" sekmesindeyken de bekleyen mazeretlere döner
  const b = await chromium.launch(); let fail = 0; const ok = (c, m) => { console.log(c ? 'PASS' : 'FAIL', m); if (!c) fail++; };
  const p = await (await b.newContext({ viewport: { width: 1280, height: 900 } })).newPage();
  await p.goto(BASE); await p.fill('#email', 'seed.mercan@ddo-case.invalid'); await p.fill('#pw', 'Demo!1234'); await p.click('button.primary'); await p.waitForSelector('.shell');
  await p.click('a.bellbtn'); await p.waitForSelector('.tabs'); await p.click('.tabs button:has-text("Çakışmalar")'); await p.waitForSelector('h1:has-text("Çakışmalar")');
  await p.click('a.bellbtn'); await p.waitForSelector('h1:has-text("Mazeret masası")', { timeout: 5000 }).then(() => ok(true, 'çan: Çakışmalar sekmesinden Mazeret masasına döner'), () => ok(false, 'çan: Mazeret masasına dönmedi'));
  await b.close(); return fail;
}),
];
(async () => { let f = 0; for (const p of parts) f += await p(); process.exit(f ? 1 : 0); })();
