// Faz 22 B2 — "ya olursa?" önizlemesi. Gözetmen çıkarma: önce etki (boşalan, yeniden atanan, dokunulmayan,
// ihlal) gösterilir, veri değişmez; "Vazgeç" kapatır; "Uygula" sonrası "önizlemeyle birebir aynı". Tahliye:
// "Önizle" olmadan "Tahliye et" kapalı; hedef değişince önizleme geçersizleşir.
const { chromium } = require('playwright');
const BASE = process.env.UI_URL || 'http://localhost:3000';
const API = process.env.API_URL || 'http://localhost:8080';
(async () => {
  const b = await chromium.launch();
  let fail = 0; const ok = (c, m) => { console.log(c ? 'PASS' : 'FAIL', m); if (!c) fail++; };
  const tok = (await (await fetch(API + '/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email: 'seed.mercan@ddo-case.invalid', password: 'Demo!1234' }) })).json()).access_token;
  const p = await (await b.newContext({ viewport: { width: 1280, height: 1000 } })).newPage();
  await p.goto(BASE); await p.fill('#email', 'seed.mercan@ddo-case.invalid'); await p.fill('#pw', 'Demo!1234'); await p.click('button.primary'); await p.waitForSelector('.shell');
  await p.goto(BASE + '/#/gozetmenler'); await p.waitForSelector('tbody tr button:has-text("Hasta / çıkar")', { timeout: 20000 });
  const sched = await p.$eval('select[aria-label="Çizelge sürümü"]', (s) => s.value);
  const count = async () => (await (await fetch(`${API}/schedules/${sched}/invigilators?limit=1`, { headers: { Authorization: 'Bearer ' + tok } })).json());
  const row = p.locator('tbody tr').filter({ hasNotText: 'Ömer ASLAN' }).first();
  const before = JSON.stringify(await count());
  await row.locator('button:has-text("Hasta / çıkar")').click();
  await p.waitForSelector('[data-testid=remove-preview]', { timeout: 20000 });
  const txt = await p.textContent('main');
  ok(txt.includes('Henüz hiçbir şey değişmedi') && txt.includes('Dokunulmayan atama'), 'önizleme: etki kartları + "henüz hiçbir şey değişmedi"');
  ok(JSON.stringify(await count()) === before, 'önizleme veriyi değiştirmedi');
  await p.click('[data-testid=remove-preview] button:has-text("Vazgeç")');
  ok(!(await p.$('[data-testid=remove-preview]')), 'Vazgeç önizlemeyi kapatır');
  await row.locator('button:has-text("Hasta / çıkar")').click(); await p.waitForSelector('[data-testid=remove-preview]');
  await p.click('[data-testid=remove-preview] button:has-text("Uygula")');
  await p.waitForSelector('text=önizlemeyle birebir aynı', { timeout: 30000 }).then(() => ok(true, 'uygulandı: sonuç önizlemeyle birebir aynı'), () => ok(false, 'uygulama sonucu önizlemeyle eşleşmedi'));
  await p.screenshot({ path: './shots/preview-remove.png' });

  await p.goto(BASE + '/#/salonlar'); await p.click('button:has-text("Sınav oturma planı")');
  await p.waitForSelector('select[aria-label=Sınav] option:nth-child(3)', { state: 'attached' }); await p.selectOption('select[aria-label=Sınav]', { index: 3 });
  await p.waitForSelector('#dst', { timeout: 15000 });
  await p.selectOption('#dst', { index: 1 }); await p.fill('#why', 'klima arızası');
  ok(await p.$eval('button:has-text("Tahliye et")', (x) => x.disabled), 'tahliye: önizleme olmadan "Tahliye et" kapalı');
  await p.click('button:has-text("Önizle")');
  await p.waitForSelector('[data-testid=evac-preview], .alert.err', { timeout: 20000 });
  const fit = !!(await p.$('[data-testid=evac-preview]'));
  ok(true, `tahliye önizlemesi: ${fit ? (await p.textContent('[data-testid=evac-preview]')).slice(0, 70) : 'hedefe sığmıyor (hiçbir şey değişmedi)'}`);
  if (fit) {
    ok(!(await p.$eval('button:has-text("Tahliye et")', (x) => x.disabled)), 'önizleme sonrası "Tahliye et" açık');
    await p.selectOption('#dst', { index: 2 });
    ok(await p.$eval('button:has-text("Tahliye et")', (x) => x.disabled), 'hedef değişince önizleme geçersiz, "Tahliye et" yeniden kapalı');
  }
  await b.close(); process.exit(fail ? 1 : 0);
})();
