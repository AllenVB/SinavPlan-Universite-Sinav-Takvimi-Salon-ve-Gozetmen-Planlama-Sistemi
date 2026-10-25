// Erişilebilirlik taraması (axe-core, WCAG 2.1 A/AA) + klavye denetimi. Her rolün ekranları taranır;
// "serious" ve "critical" bulgu varsa çıkış kodu 1. axe-core global değilse: AXE_PATH=/yol/axe.min.js
// Kullanım: NODE_PATH=$(npm root -g) AXE_PATH=… UI_URL=http://localhost:3000 node deploy/ui-smoke/a11y.cjs
const fs = require('fs');
const { chromium } = require('playwright');
const BASE = process.env.UI_URL || 'http://localhost:3000';
const AXE = process.env.AXE_PATH || require.resolve('axe-core/axe.min.js');
const roles = [
  ['sekreter@sinavplan.test', ['panel', 'takvim', 'takvim?tab=uret', 'takvim?tab=prova', 'salonlar', 'gozetmenler', 'canli', 'mazeret', 'cakisma', 'ice-aktar']],
  ['hoca@sinavplan.test', ['takvim', 'salonlar']],
  ['gozetmen@sinavplan.test', ['yoklama', 'salonlar', 'mazeret']],
  ['ogrenciisleri@sinavplan.test', ['mazeret', 'canli']],
  ['ogrenci@sinavplan.test', ['programim', 'mazeret']],
];
(async () => {
  const axe = fs.readFileSync(AXE, 'utf8');
  const b = await chromium.launch();
  let fail = 0; const ok = (c, m) => { console.log(c ? 'PASS' : 'FAIL', m); if (!c) fail++; };
  const scan = async (p, name) => {
    await p.addScriptTag({ content: axe });
    const r = await p.evaluate(async () => (await window.axe.run(document, { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'] } })).violations
      .map((v) => ({ id: v.id, impact: v.impact, n: v.nodes.length, sample: v.nodes[0]?.target?.join(' ') })));
    const bad = r.filter((v) => v.impact === 'serious' || v.impact === 'critical');
    ok(bad.length === 0, `${name}: ciddi bulgu ${bad.length}${r.length ? ` (tüm: ${r.map((v) => `${v.id}×${v.n}/${v.impact}`).join(', ')})` : ''}`);
    bad.forEach((v) => console.log('   ', v.id, v.impact, v.n, v.sample));
  };
  // giriş ekranı: tarama + klavyeyle doldurup gönderme
  { const p = await (await b.newContext()).newPage(); await p.goto(BASE); await p.waitForSelector('#email'); await scan(p, 'giriş');
    await p.focus('#email'); await p.keyboard.type('sekreter@sinavplan.test'); await p.keyboard.press('Tab');
    ok(await p.evaluate(() => document.activeElement?.id === 'pw'), 'klavye: Tab e-postadan parolaya geçer');
    await p.keyboard.type('Demo!1234'); await p.keyboard.press('Enter'); await p.waitForSelector('.shell', { timeout: 8000 }).then(() => ok(true, 'klavye: Enter ile giriş'), () => ok(false, 'klavye: Enter ile giriş'));
    await p.keyboard.press('Tab');
    const ring = await p.evaluate(() => { const e = document.activeElement; const s = e && getComputedStyle(e); return !!s && (s.outlineStyle !== 'none' && parseFloat(s.outlineWidth) > 0 || s.boxShadow !== 'none'); });
    ok(ring, 'klavye: odaklanan öğede görünür odak halkası var');
  }
  for (const [email, pages] of roles) {
    const ctx = await b.newContext({ viewport: { width: 1280, height: 900 } }); const p = await ctx.newPage();
    await p.goto(BASE); await p.fill('#email', email); await p.fill('#pw', 'Demo!1234'); await p.click('button.primary'); await p.waitForSelector('.shell');
    for (const pg of pages) { await p.goto(`${BASE}/#/${pg}`); await p.waitForTimeout(1800); await scan(p, `${email.split('@')[0]} ${pg}`); }
    await ctx.close();
  }
  await b.close(); process.exit(fail ? 1 : 0);
})();
