// Faz 12-14 UI duman testi: canlı izleme güncellenir, dinamik QR döner, kamera tarayıcı açılır.
// Önkoşul: API'de QR_TTL=20s (döndürme ~12 sn'de olur) ve yayınlanmış çizelge. Kullanım: bkz. pages.cjs.
require('fs').mkdirSync('./shots', { recursive: true });
const { chromium } = require('playwright');
const BASE = process.env.UI_URL || 'http://localhost:3000';
const API = process.env.API_URL || 'http://localhost:8080';
(async () => {
  const b = await chromium.launch({ args: ['--use-fake-ui-for-media-stream', '--use-fake-device-for-media-stream'] });
  let fail = 0; const ok = (c, m) => { console.log(c ? 'PASS' : 'FAIL', m); if (!c) fail++; };
  const login = async (email, vp = { width: 1280, height: 900 }) => { const ctx = await b.newContext({ viewport: vp, permissions: ['camera'] }); const p = await ctx.newPage(); await p.goto(BASE); await p.fill('#email', email); await p.fill('#pw', 'Demo!1234'); await p.click('button.primary'); await p.waitForSelector('.shell'); return p; };

  { // F13: canlı izleme — salon kartları + "Son güncelleme" sayacı gerçekten ilerliyor + yenileme olayı gerçek istek
    const p = await login('sekreter@sinavplan.test'); let liveCalls = 0; p.on('request', (r) => { if (r.url().includes('/live')) liveCalls++; });
    await p.goto(BASE + '/#/canli'); await p.waitForSelector('.roomcard', { timeout: 15000 });
    ok((await p.$$('.roomcard')).length > 0, 'F13: salon kartları çizildi');
    ok(await p.$('.segbar') !== null, 'F13: yoklama çubukları var');
    const a = liveCalls; await p.waitForTimeout(11000); ok(liveCalls >= a + 3, `F13: canlı veri kendiliğinden yenileniyor (${liveCalls - a} istek / 11 sn)`);
    const txt = await p.textContent('.livebadge'); ok(/Son güncelleme: \d+ sn önce/.test(txt || ''), 'F13: "Son güncelleme" göstergesi var');
    await p.screenshot({ path: './shots/live.png', fullPage: false });
  }
  { // F14: dinamik QR — jeton kendiliğinden yenilenir, eski jeton iptal olur
    const p = await login('ogrenci@sinavplan.test', { width: 420, height: 900 });
    await p.waitForSelector('.card button.primary'); const n = await p.$$eval('.card button.primary:not([disabled])', (e) => e.length); let found = false;
    for (let i = 0; i < n && !found; i++) { await p.click(`.card button.primary:not([disabled]) >> nth=${i}`); await p.waitForTimeout(1200); found = !!(await p.$('[data-testid=qr-svg]')); }
    ok(found, 'F14: QR çizildi'); if (found) {
      await p.evaluate(() => document.querySelector('details')?.setAttribute('open', ''));
      const t1 = await p.textContent('[data-testid=qr-token]'); const r1 = Number(await p.textContent('[data-testid=qr-rotations]'));
      await p.waitForFunction((r) => Number(document.querySelector('[data-testid=qr-rotations]').textContent) > r, r1, { timeout: 40000 });
      const t2 = await p.textContent('[data-testid=qr-token]'); ok(t1 !== t2, 'F14: QR jetonu kendiliğinden yenilendi');
      const tokenOf = async (email) => (await (await fetch(API + '/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email, password: 'Demo!1234' }) })).json()).access_token;
      const inv = await tokenOf('gozetmen@sinavplan.test');
      const scan = async (tok) => (await fetch(API + '/attendance/qr', { method: 'POST', headers: { Authorization: 'Bearer ' + inv, 'Content-Type': 'application/json', 'Idempotency-Key': 'smoke-' + Math.random() }, body: JSON.stringify({ token: tok, device_id: 'smoke' }) })).status;
      ok(await scan(t1) === 409, 'F14: yenilenen kartın eski jetonu geçersiz (409)');
      ok(await scan(t2) === 200, 'F14: güncel jeton çalışıyor (200)');
    }
    await p.screenshot({ path: './shots/qr-dynamic.png', fullPage: true });
  }
  { // F14: tablet kamera tarayıcı — sahte kamera ile video akışı açılır
    const p = await login('gozetmen@sinavplan.test', { width: 1100, height: 900 }); await p.goto(BASE + '/#/yoklama'); await p.waitForSelector('text=Yoklama al'); await p.click('text=Yoklama al >> nth=0'); await p.waitForSelector('text=Öğrenci kartı tara');
    await p.click('button:has-text("Kamerayı aç")'); await p.waitForFunction(() => { const v = document.querySelector('[data-testid=scanner-video]'); return v && v.srcObject && v.readyState >= 2; }, null, { timeout: 10000 });
    ok(true, 'F14: kamera akışı açıldı (sahte aygıt)'); await p.click('button:has-text("Kamerayı kapat")');
  }
  await b.close(); process.exit(fail ? 1 : 0);
})();
