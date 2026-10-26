// Kapasite panosunda bir derse tıklanınca salon krokisi + öğrenci listesi (koltuk, yoklamada geldi/gelmedi/işaretsiz) açılır;
// gözetmen yalnız görevli olduğu salonun listesini görür (diğerinde anlaşılır mesaj). Kullanım: bkz. pages.cjs.
const { chromium } = require('playwright'); const BASE = process.env.UI_URL || 'http://localhost:3000';
(async () => {
  const b = await chromium.launch(); let fail = 0; const ok = (c, m) => { console.log(c ? 'PASS' : 'FAIL', m); if (!c) fail++; };
  for (const [role, email] of [['gözetmen', 'gozetmen@sinavplan.test'], ['sekreter', 'seed.mercan@ddo-case.invalid']]) {
    const p = await (await b.newContext({ viewport: { width: 1280, height: 1000 } })).newPage();
    await p.goto(BASE); await p.fill('#email', email); await p.fill('#pw', 'Demo!1234'); await p.click('button.primary'); await p.waitForSelector('.shell');
    await p.goto(BASE + '/#/salonlar'); await p.waitForSelector('.roomcard .linkbtn', { timeout: 20000 });
    const n = await p.$$eval('.roomcard .linkbtn', (x) => x.length); ok(n > 0, `${role}: kapasite panosunda tıklanabilir ders (${n})`);
    let got = false;
    if (role === 'gözetmen') {
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
    ok(got, `${role}: tıklayınca derslik krokisi + öğrenci listesi açıldı`);
    if (got) {
      const rows = await p.$$eval('[data-testid=student-roster] tbody tr', (r) => r.map((x) => x.textContent));
      ok(rows.every((t) => /(Geldi|Gelmedi|İşaretsiz)/.test(t)), `${role}: ${rows.length} öğrenci, hepsinde yoklama durumu`);
      ok(await p.$('[data-testid=exam-room-detail] .seatmap, [data-testid=exam-room-detail] [class*=seat]') !== null, `${role}: salon krokisi görünüyor`);
      if (role === 'sekreter') await p.screenshot({ path: './shots/exam-room-detail.png' });
      // Krokiden yoklama: koltuğa tıkla → GELMEDİ işaretle → listede ve sunucuda görünür; sonra GELDİ ile düzelt
      const examId = await p.evaluate(() => performance.getEntriesByType('resource').map((r) => r.name).filter((n) => /\/exams\/\d+\/attendance/.test(n)).map((n) => n.match(/exams\/(\d+)/)[1]).pop());
      await p.click('[data-testid=exam-room-detail] .seat[role=button] >> nth=0');
      const who = (await p.textContent('[data-testid=mark-bar] b')).trim();
      ok(who.length > 3, `${role}: krokide koltuğa tıklayınca öğrenci seçildi (${who})`);
      await p.click('[data-testid=mark-bar] button:has-text("GELMEDİ")'); await p.waitForTimeout(1200);
      const rowTxt = await p.textContent(`[data-testid=student-roster] tr:has-text("${who}")`);
      ok(/Gelmedi/.test(rowTxt), `${role}: ${who} listede "Gelmedi" oldu`);
      const apiTok = (await (await fetch('http://localhost:8080/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email, password: 'Demo!1234' }) })).json()).access_token;
      const att = await (await fetch(`http://localhost:8080/exams/${examId}/attendance?limit=200`, { headers: { Authorization: 'Bearer ' + apiTok } })).json();
      ok(att.items.some((i) => i.status === 'absent' && i.source === 'manual'), `${role}: sunucuda manuel "absent" kaydı var`);
      await p.click('[data-testid=mark-bar] button:has-text("GELDİ")'); await p.waitForTimeout(1200);
      ok(/Geldi/.test(await p.textContent(`[data-testid=student-roster] tr:has-text("${who}")`)), `${role}: GELDİ ile düzeltildi (son kayıt kazanır)`);
      if (role === 'gözetmen') { // çevrimdışı: kayıt cihazdaki kuyruğa yazılır (Yoklama ekranı çevrimiçi olunca eşitler)
        await p.context().setOffline(true); await p.evaluate(() => localStorage.removeItem('sinavplan.queue'));
        await p.click('[data-testid=mark-bar] button:has-text("GELMEDİ")'); await p.waitForTimeout(800);
        const q = await p.evaluate(() => JSON.parse(localStorage.getItem('sinavplan.queue') || '[]'));
        ok(q.length === 1 && q[0].status === 'absent' && q[0].source === 'manual', `${role}: ağ yokken işaret kuyruğa yazıldı (${q.length})`);
        ok(await p.$('text=ağ yok, kuyrukta') !== null, `${role}: ekranda "kuyrukta" uyarısı`);
        await p.context().setOffline(false); await p.evaluate(() => localStorage.removeItem('sinavplan.queue'));
      }
      await p.click('button[aria-label="Detayı kapat"]'); ok(!(await p.$('[data-testid=exam-room-detail]')), `${role}: kapat çalışıyor`);
    }
  }
  await b.close(); process.exit(fail ? 1 : 0);
})();
