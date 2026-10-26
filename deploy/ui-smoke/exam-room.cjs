// Kapasite panosunda bir derse tıklanınca salon krokisi + öğrenci listesi (koltuk, yoklamada geldi/gelmedi/işaretsiz) açılır;
// gözetmen yalnız görevli olduğu salonun listesini görür (diğerinde anlaşılır mesaj). Kullanım: bkz. pages.cjs.
const { chromium } = require('playwright'); const BASE = process.env.UI_URL || 'http://localhost:3000';
(async () => {
  const b = await chromium.launch(); let fail = 0; const ok = (c, m) => { console.log(c ? 'PASS' : 'FAIL', m); if (!c) fail++; };
  for (const [who, email] of [['gözetmen', 'gozetmen@sinavplan.test'], ['sekreter', 'seed.mercan@ddo-case.invalid']]) {
    const p = await (await b.newContext({ viewport: { width: 1280, height: 1000 } })).newPage();
    await p.goto(BASE); await p.fill('#email', email); await p.fill('#pw', 'Demo!1234'); await p.click('button.primary'); await p.waitForSelector('.shell');
    await p.goto(BASE + '/#/salonlar'); await p.waitForSelector('.roomcard .linkbtn', { timeout: 20000 });
    const n = await p.$$eval('.roomcard .linkbtn', (x) => x.length); ok(n > 0, `${who}: kapasite panosunda tıklanabilir ders (${n})`);
    let got = false;
    if (who === 'gözetmen') {
      await p.click('.roomcard .linkbtn >> nth=0'); await p.waitForSelector('[data-testid=exam-room-detail]');
      ok(await p.waitForSelector('text=Bu sınav sizin göreviniz değil', { timeout: 6000 }).then(() => true, () => false), 'gözetmen: görevi olmayan sınavda anlaşılır mesaj (ham hata yok)');
      const tk = (await (await fetch('http://localhost:8080/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email, password: 'Demo!1234' }) })).json()).access_token;
      const duty = (await (await fetch('http://localhost:8080/me/duties?limit=200', { headers: { Authorization: 'Bearer ' + tk } })).json()).items.sort((x, y) => x.starts_at.localeCompare(y.starts_at))[0];
      await p.selectOption('select[aria-label="Oturum"]', String(duty.slot_id)); await p.waitForTimeout(1200);
      const idx = await p.$$eval('.roomcard', (cs, c) => cs.findIndex((x) => x.querySelector('h2')?.textContent === c), duty.room_code);
      ok(idx >= 0, `gözetmen: görev salonu ${duty.room_code} panoda`);
      if (idx >= 0) { await p.click(`.roomcard >> nth=${idx} >> .linkbtn >> nth=0`); got = await p.waitForSelector('[data-testid=exam-room-detail] [data-testid=student-roster] tbody tr', { timeout: 8000 }).then(() => true, () => false); }
    } else {
      await p.click('.roomcard .linkbtn >> nth=0'); got = await p.waitForSelector('[data-testid=exam-room-detail] [data-testid=student-roster] tbody tr', { timeout: 6000 }).then(() => true, () => false);
    }
    ok(got, `${who}: tıklayınca derslik krokisi + öğrenci listesi açıldı`);
    if (got) {
      const rows = await p.$$eval('[data-testid=student-roster] tbody tr', (r) => r.map((x) => x.textContent));
      ok(rows.every((t) => /(Geldi|Gelmedi|İşaretsiz)/.test(t)), `${who}: ${rows.length} öğrenci, hepsinde yoklama durumu`);
      ok(await p.$('[data-testid=exam-room-detail] .seatmap, [data-testid=exam-room-detail] [class*=seat]') !== null, `${who}: salon krokisi görünüyor`);
      if (who === 'sekreter') await p.screenshot({ path: './shots/exam-room-detail.png' });
      await p.click('button[aria-label="Detayı kapat"]'); ok(!(await p.$('[data-testid=exam-room-detail]')), `${who}: kapat çalışıyor`);
    }
  }
  await b.close(); process.exit(fail ? 1 : 0);
})();
