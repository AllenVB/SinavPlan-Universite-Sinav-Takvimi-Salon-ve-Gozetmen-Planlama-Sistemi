// Faz 22 B1 — çakışma çözüm önerisi: Çakışmalar sekmesinde bir öğrencinin satırında "Çöz" → doğrulanmış
// seçenekler (ihlal 0) → sekreter "Uygula" → yeni taslak sürüm. Öğrenci işleri önerileri görür, uygulayamaz.
const { chromium } = require('playwright');
const BASE = process.env.UI_URL || 'http://localhost:3000';
(async () => {
  const b = await chromium.launch();
  let fail = 0; const ok = (c, m) => { console.log(c ? 'PASS' : 'FAIL', m); if (!c) fail++; };
  const login = async (email) => { const p = await (await b.newContext({ viewport: { width: 1280, height: 1000 } })).newPage(); await p.goto(BASE); await p.fill('#email', email); await p.fill('#pw', 'Demo!1234'); await p.click('button.primary'); await p.waitForSelector('.shell'); return p; };
  // başlamış oturumlar kilitli: 4. günden sonraki bir satır seç
  const pickRow = async (p) => { await p.goto(BASE + '/#/cakisma'); await p.waitForSelector('tbody tr'); return p.locator('tbody tr').filter({ has: p.locator('td:nth-child(2)', { hasText: /^([4-9]|1[0-5])\. gün$/ }) }).first(); };

  const a = await login('ogrenciisleri@sinavplan.test');
  await (await pickRow(a)).locator('button:has-text("Çöz")').click();
  await a.waitForSelector('[data-testid=fix-panel] li, [data-testid=fix-panel] .empty', { timeout: 15000 });
  const n = await a.$$eval('[data-testid=fix-panel] li', (l) => l.length);
  ok(n > 0, `öğrenci işleri: ${n} seçenek listelendi`);
  ok((await a.textContent('[data-testid=fix-panel]')).includes('İhlal 0'), 'seçenekler bağımsız doğrulayıcıdan geçti (İhlal 0)');
  ok(!(await a.$('[data-testid=fix-panel] button:has-text("Uygula")')), 'öğrenci işleri uygulayamaz (yetki sekreterde)');

  const s = await login('sekreter@sinavplan.test');
  await (await pickRow(s)).locator('button:has-text("Çöz")').click();
  await s.waitForSelector('[data-testid=fix-panel] button:has-text("Uygula")', { timeout: 15000 });
  await s.click('[data-testid=fix-panel] button:has-text("Uygula") >> nth=0');
  await s.waitForSelector('text=(taslak) oluşturuldu', { timeout: 30000 }).then(() => ok(true, 'sekreter uyguladı: yeni taslak sürüm'), () => ok(false, 'uygulama sonucu görünmedi'));
  await s.screenshot({ path: './shots/fix.png', fullPage: false });
  await b.close(); process.exit(fail ? 1 : 0);
})();
