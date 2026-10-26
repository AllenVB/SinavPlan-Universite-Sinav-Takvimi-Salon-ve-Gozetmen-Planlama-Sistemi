// k6 yük testi. Okuma ağırlıklı karışım (salt-okunur uçlar) + yoklama senkronu (yazma).
// Çalıştırma: ./deploy/k6/run_load.sh  (özet: docs/load/results.md, results.json)
import http from 'k6/http';
import { check } from 'k6';
import { Trend } from 'k6/metrics';

const BASE = __ENV.BASE_URL || 'http://api:8080';
const PASS = __ENV.DEMO_PASSWORD || 'Demo!1234';
const DUR = __ENV.DURATION || '40s';

const tCalendar = new Trend('lat_takvim_okuma', true);
const tMyExams = new Trend('lat_ogrenci_programi', true);
const tRooms = new Trend('lat_salon_listesi', true);
const tSeating = new Trend('lat_oturma_plani', true);
const tSync = new Trend('lat_yoklama_sync', true);

export const options = {
  scenarios: {
    okuma: { executor: 'constant-vus', vus: 30, duration: DUR, exec: 'reads' },
    yazma: { executor: 'constant-vus', vus: 10, duration: DUR, exec: 'writes' },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(95)<800'],
  },
  summaryTrendStats: ['count', 'med', 'p(95)', 'p(99)', 'max'],
};

function login(email) {
  const r = http.post(`${BASE}/auth/login`, JSON.stringify({ email, password: PASS }), { headers: { 'Content-Type': 'application/json' } });
  if (r.status !== 200) throw new Error(`giriş ${email}: HTTP ${r.status}`);
  return r.json('access_token');
}

export function setup() {
  const sec = login('seed.mercan@ddo-case.invalid');
  const h = (t) => ({ headers: { Authorization: `Bearer ${t}` } });
  const list = http.get(`${BASE}/schedules?limit=100`, h(sec)).json('items') || [];
  const pub = list.filter((s) => s.status === 'published').sort((a, b) => b.id - a.id)[0];
  if (!pub) throw new Error('yayınlanmış çizelge yok; önce `make verify` çalıştırın');
  const cal = http.get(`${BASE}/schedules/${pub.id}/exams?limit=50`, h(sec)).json('items');
  return {
    schedule: pub.id,
    exams: cal.map((e) => e.exam_id),
    sec, stu: login('ogrenci@sinavplan.test'), inv: login('gozetmen@sinavplan.test'),
    student: http.get(`${BASE}/me`, h(login('ogrenci@sinavplan.test'))).json('student_id'),
  };
}

const auth = (t) => ({ headers: { Authorization: `Bearer ${t}` } });

export function reads(d) {
  const pick = d.exams[Math.floor(Math.random() * d.exams.length)];
  let r = http.get(`${BASE}/schedules/${d.schedule}/exams?limit=50`, auth(d.sec));
  tCalendar.add(r.timings.duration); check(r, { 'takvim 200': (x) => x.status === 200 });
  r = http.get(`${BASE}/me/exams`, auth(d.stu));
  tMyExams.add(r.timings.duration); check(r, { 'program 200': (x) => x.status === 200 });
  r = http.get(`${BASE}/rooms?limit=50`, auth(d.sec));
  tRooms.add(r.timings.duration); check(r, { 'salon 200': (x) => x.status === 200 });
  r = http.get(`${BASE}/schedules/${d.schedule}/seating?exam_id=${pick}&limit=100`, auth(d.sec));
  tSeating.add(r.timings.duration); check(r, { 'oturma 200': (x) => x.status === 200 });
}

function uuidv7() {
  const ms = Date.now().toString(16).padStart(12, '0');
  const rnd = (n) => Array.from({ length: n }, () => Math.floor(Math.random() * 16).toString(16)).join('');
  return `${ms.slice(0, 8)}-${ms.slice(8)}-7${rnd(3)}-${'89ab'[Math.floor(Math.random() * 4)]}${rnd(3)}-${rnd(12)}`;
}

export function writes(d) {
  const ev = (i) => ({ event_id: uuidv7(), device_id: `k6-${__VU}`, exam_id: d.exams[0], student_id: d.student + i, status: 'present', source: 'manual', hlc: { wall: Date.now(), counter: i } });
  const body = JSON.stringify({ events: [ev(0), ev(1), ev(2), ev(3), ev(4)] });
  const r = http.post(`${BASE}/attendance/sync`, body, { headers: { Authorization: `Bearer ${d.inv}`, 'Content-Type': 'application/json' } });
  tSync.add(r.timings.duration);
  check(r, { 'sync 200': (x) => x.status === 200 });
}

export function handleSummary(data) {
  const rows = [['Uç / senaryo', 'istek', 'p50 (ms)', 'p95 (ms)', 'p99 (ms)']];
  const m = data.metrics;
  const f = (v) => (v === undefined ? '-' : v.toFixed(1));
  for (const [name, label] of [['lat_takvim_okuma', 'GET /schedules/{id}/exams'], ['lat_ogrenci_programi', 'GET /me/exams'], ['lat_salon_listesi', 'GET /rooms'], ['lat_oturma_plani', 'GET /schedules/{id}/seating'], ['lat_yoklama_sync', 'POST /attendance/sync (5 olay)']]) {
    const t = m[name];
    if (t) rows.push([label, String(t.values.count ?? '-'), f(t.values.med), f(t.values['p(95)']), f(t.values['p(99)'])]);
  }
  const all = m.http_req_duration.values;
  rows.push(['**Tümü**', String(m.http_reqs.values.count), f(all.med), f(all['p(95)']), f(all['p(99)'])]);
  const md = rows.map((r, i) => `| ${r.join(' | ')} |` + (i === 0 ? `\n|${r.map(() => '---').join('|')}|` : '')).join('\n');
  const text = `${md}\n\nHata oranı (http_req_failed): ${(m.http_req_failed.values.rate * 100).toFixed(3)} % · toplam ${m.http_reqs.values.count} istek · ${(m.http_reqs.values.rate).toFixed(0)} istek/sn · 30 okuma + 10 yazma VU, ${DUR}\n`;
  return { '/out/results.md': text, '/out/results.json': JSON.stringify(data, null, 1), stdout: text };
}
