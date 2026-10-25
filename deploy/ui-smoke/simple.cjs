// Faz 20 (sade kullanım) duman testi: menü grupları ve ≤7 öğe, başlangıç rehberi, takvim sekmeleri,
// "Gelişmiş ayarlar" kapalı başlar, "Öğretim üyesi" sütunu + aramada hoca adı. Kullanım: bkz. pages.cjs.
require('fs').mkdirSync('./shots', { recursive: true });
const { chromium } = require('playwright');
const BASE = process.env.UI_URL || 'http://localhost:3000';
(async () => {
  const b = await chromium.launch();
  let fail = 0; const ok = (c, m) => { console.log(c ? 'PASS' : 'FAIL', m); if (!c) fail++; };
  const login = async (email, vp = { width: 1280, height: 1000 }) => { const ctx = await b.newContext({ viewport: vp }); const p = await ctx.newPage(); await p.goto(BASE); await p.fill('#email', email); await p.fill('#pw', 'Demo!1234'); await p.click('button.primary'); await p.waitForSelector('.shell'); return p; };

  for (const e of ['sekreter', 'hoca', 'gozetmen', 'ogrenciisleri', 'ogrenci']) {
    const p = await login(`${e}@sinavplan.test`);
    const n = await p.$$eval('aside.side nav a', (a) => a.length);
    ok(n <= 7, `20.1: ${e} menüsünde ${n} öğe (≤7)`);
    if (e === 'sekreter') {
      const heads = await p.$$eval('aside.side .navhead', (h) => h.map((x) => x.textContent.trim()));
      ok(['Planlama', 'Sınav günü', 'Yönetim'].every((g) => heads.includes(g)), `20.1: menü grupları ${heads.join(' / ')}`);
    }
  }

  const s = await login('sekreter@sinavplan.test');
  await s.waitForSelector('text=Başlangıç rehberi');
  const steps = await s.$$eval('.guide li', (l) => l.map((x) => x.classList.contains('done')));
  ok(steps.length === 4, `20.2: rehberde 4 adım (✓ durumları: ${steps.map((d) => (d ? '✓' : '○')).join(' ')})`);
  ok(steps[0] === true && steps[1] === true, '20.2: çizelge var + yayınlandı adımları ✓ (gerçek veriden)');

  await s.goto(BASE + '/#/takvim'); await s.waitForSelector('.tabs');
  const tabs = await s.$$eval('.tabs button', (x) => x.map((t) => t.textContent.trim()));
  ok(['Takvim', 'Üret', 'Prova', 'Karşılaştır'].every((t) => tabs.includes(t)), `20.3: sekmeler ${tabs.join(' / ')}`);
  ok(!(await s.$('button:has-text("Takvimi oluştur")')), '20.3: Takvim sekmesinde üretim formu yok');
  await s.click('.tabs button:has-text("Üret")');
  await s.waitForSelector('button:has-text("Takvimi oluştur")');
  ok(!(await s.$('#gen-seed:visible')), '20.4: Varyasyon no varsayılan olarak gizli');
  await s.click('details.adv summary');
  ok(!!(await s.$('#gen-seed:visible')) && !!(await s.$('#gen-q:visible')), '20.4: Gelişmiş ayarlar açılınca Varyasyon no + Hesaplama kalitesi görünür');
  await s.click('.tabs button:has-text("Karşılaştır")'); await s.waitForSelector('main .card');
  await s.click('.tabs button:has-text("Takvim")'); await s.waitForSelector('th:has-text("Öğretim üyesi")');
  const lect = await s.$$eval('tbody tr', (r) => r.slice(0, 20).map((x) => x.children[3]?.textContent.trim()));
  ok(lect.every((t) => t && t !== '—'), `20.5: öğretim üyesi sütunu dolu (${lect[0]})`);
  await s.fill('input[aria-label="Ders ara"]', lect[0].split(' ').pop()); await s.waitForTimeout(300);
  const rows = await s.$$eval('tbody tr', (r) => r.length);
  ok(rows > 0 && rows < 301, `20.5: hoca adıyla arama çalışıyor (${rows} satır)`);
  await s.goto(BASE + '/#/duzenle'); await s.waitForSelector('[draggable=true]', { timeout: 15000 }); ok(true, '20.3: eski #/duzenle bağlantısı editörü açıyor');
  await s.goto(BASE + '/#/cakisma'); await s.waitForSelector('text=Çakışmalar'); ok(true, '20.1: eski #/cakisma bağlantısı çakışma sekmesini açıyor');
  const m = await login('sekreter@sinavplan.test', { width: 390, height: 800 });
  ok(!(await m.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1)), 'mobil 390px: yatay taşma yok (ana panel)');
  await m.goto(BASE + '/#/takvim'); await m.waitForSelector('.tabs'); await m.waitForTimeout(500);
  ok(!(await m.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1)), 'mobil 390px: takvim sekmeleri taşmıyor');
  await s.goto(BASE + '/#/panel'); await s.waitForTimeout(1500); await s.screenshot({ path: './shots/panel-rehber.png' });
  await b.close(); process.exit(fail ? 1 : 0);
})();
