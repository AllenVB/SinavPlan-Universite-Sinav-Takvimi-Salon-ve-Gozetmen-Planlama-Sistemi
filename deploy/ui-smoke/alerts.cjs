// Faz 22 C2 — "Dikkat gerektirenler": Sınav günü izleme ekranında sunucunun kural tabanlı uyarıları
// (API'deki alerts ile aynı sayıda) gösterilir; salon koduna tıklayınca salon listesi o salona süzülür.
const { chromium } = require('playwright');
const BASE = process.env.UI_URL || 'http://localhost:3000';
const API = process.env.API_URL || 'http://localhost:8080';
(async () => {
  const b = await chromium.launch();
  let fail = 0; const ok = (c, m) => { console.log(c ? 'PASS' : 'FAIL', m); if (!c) fail++; };
  const tok = (await (await fetch(API + '/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email: 'seed.mercan@ddo-case.invalid', password: 'Demo!1234' }) })).json()).access_token;
  const p = await (await b.newContext({ viewport: { width: 1280, height: 1000 } })).newPage();
  await p.goto(BASE); await p.fill('#email', 'seed.mercan@ddo-case.invalid'); await p.fill('#pw', 'Demo!1234'); await p.click('button.primary'); await p.waitForSelector('.shell');
  await p.goto(BASE + '/#/canli'); await p.waitForSelector('[data-testid=live-alerts]', { timeout: 20000 });
  const sched = await p.$eval('select[aria-label="Çizelge sürümü"]', (s) => s.value);
  const api = await (await fetch(`${API}/schedules/${sched}/live`, { headers: { Authorization: 'Bearer ' + tok } })).json();
  const shown = await p.$$eval('[data-testid=live-alerts] li', (l) => l.length);
  ok(shown === Math.min(api.alerts.length, 12) + (api.alerts.length > 12 ? 1 : 0), `ekrandaki uyarı ${shown} = API ${api.alerts.length}`);
  const withRoom = api.alerts.find((a) => a.room_code);
  if (withRoom) {
    await p.click(`[data-testid=live-alerts] button.linkbtn:has-text("${withRoom.room_code}")`); await p.waitForTimeout(400);
    const cards = await p.$$eval('.roomcard h2, .roomcard h3', (h) => h.map((x) => x.textContent));
    ok(cards.length >= 1 && cards.every((c) => c.includes(withRoom.room_code)), `salon koduna tıklama listeyi süzdü (${withRoom.room_code}: ${withRoom.message})`);
  } else ok(true, 'salona bağlı uyarı yok (her şey yolunda)');
  await p.screenshot({ path: './shots/alerts.png' });
  await b.close(); process.exit(fail ? 1 : 0);
})();
