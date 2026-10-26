// UI duman testi (Playwright gerekir: npm i -g playwright). Çalıştırma: NODE_PATH=$(npm root -g) node deploy/ui-smoke/<dosya>.cjs; ekran görüntüleri ./shots/ altına yazılır.
require('fs').mkdirSync('./shots', { recursive: true });
const { chromium } = require('playwright');
const BASE = process.env.UI_URL || 'http://localhost:3000';
const roles = [
  ['seed.mercan@ddo-case.invalid', ['panel','takvim','duzenle','salonlar','gozetmenler','cakisma','ice-aktar','canli']],
  ['hoca@sinavplan.test', ['takvim','salonlar']],
  ['gozetmen@sinavplan.test', ['yoklama','mazeret']],
  ['ogrenciisleri@sinavplan.test', ['mazeret','cakisma','canli']],
  ['ogrenci@sinavplan.test', ['programim','mazeret']],
];
(async () => {
  const b = await chromium.launch();
  let fail = 0;
  for (const [email, pages] of roles) {
    const ctx = await b.newContext({ viewport: { width: 1280, height: 900 } });
    const p = await ctx.newPage();
    const errs = [];
    p.on('pageerror', e => errs.push('pageerror ' + e.message));
    p.on('console', m => { if (m.type() === 'error' && !/401|403|404|409/.test(m.text())) errs.push('console ' + m.text()); });
    await p.goto(BASE);
    await p.fill('#email', email); await p.fill('#pw', 'Demo!1234');
    await p.click('button.primary');
    await p.waitForSelector('.shell', { timeout: 8000 });
    for (const pg of pages) {
      await p.goto(`${BASE}/#/${pg}`);
      await p.waitForTimeout(1800);
      const alerts = await p.$$eval('.alert.err', els => els.map(e => e.textContent.trim().slice(0, 140)));
      const name = `${email.split('@')[0]}-${pg}`;
      await p.screenshot({ path: `./shots/${name}.png`, fullPage: true });
      console.log(name, alerts.length ? 'HATA-KUTUSU: ' + alerts.join(' | ') : 'ok');
      if (alerts.length) fail++;
    }
    if (errs.length) { console.log(email, errs.slice(0, 4)); fail++; }
    await ctx.close();
  }
  await b.close();
  process.exit(fail ? 1 : 0);
})();
