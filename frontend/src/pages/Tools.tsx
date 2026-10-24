import { useState } from 'react'
import { download, get, post, fmtDateTime } from '../api'
import { Card, ErrorBox, Field, Pill, useAction, useAsync, Async } from '../ui'
import type { Schedule } from './common'

/** Dışa aktarma: Excel, CSV, iCal ve tarayıcıdan PDF (yazdırma stili). */
export function ExportButtons({ schedule }: { schedule: number | null }) {
  const act = useAction((fmt: 'xlsx' | 'csv' | 'ics') => download(fmt === 'ics' ? `/schedules/${schedule}/calendar.ics` : `/schedules/${schedule}/export?format=${fmt}`, `takvim.${fmt}`))
  return (
    <>
      <div className="actions">
        <button className="btn" disabled={!schedule || act.busy} onClick={() => void act.run('xlsx')}>Excel (.xlsx)</button>
        <button className="btn" disabled={!schedule || act.busy} onClick={() => void act.run('csv')}>CSV</button>
        <button className="btn" disabled={!schedule || act.busy} onClick={() => void act.run('ics')}>iCal (.ics)</button>
        <button className="btn" onClick={() => window.print()}>Yazdır / PDF</button>
      </div>
      {act.error && <div style={{ marginTop: 8 }}><ErrorBox error={act.error} /></div>}
    </>
  )
}

type Sim = { result: { penalty: number; construction_penalty: number; iterations: number; duration_ms: number; unplaced: number; violations: number; metrics: { used_slots: number; student_days_with_two_exams: number; students_with_consecutive_day: number } }; published?: { version: number; metrics: { penalty: number } } }

/** Hızlı simülasyon: T-1'i kaydetmeden çalıştırır ve yayındaki sürümle kıyaslar. */
export function Simulator({ periodId }: { periodId: number | undefined }) {
  const [seed, setSeed] = useState('7')
  const [iter, setIter] = useState('50000')
  const sim = useAction(() => post<Sim>('/schedules/simulate', { period_id: periodId, seed: Number(seed), iterations: Number(iter) }))
  const r = sim.result
  const better = r?.published ? r.result.penalty - r.published.metrics.penalty : null
  return (
    <Card title="Hızlı simülasyon" aside={<Pill>Kaydedilmez</Pill>}>
      <p className="small muted" style={{ marginBottom: 12 }}>Farklı bir tohum/iterasyonla takvimi (T-1) hesaplar; hiçbir sürüm oluşturmaz. Beğendiğiniz ayarla "Takvimi oluştur"u kullanın.</p>
      <div className="row" style={{ alignItems: 'flex-end' }}>
        <Field label="Tohum" id="ss"><input id="ss" className="input" inputMode="numeric" value={seed} onChange={(e) => setSeed(e.target.value)} style={{ width: 100 }} /></Field>
        <Field label="İterasyon (≤300.000)" id="si"><input id="si" className="input" inputMode="numeric" value={iter} onChange={(e) => setIter(e.target.value)} style={{ width: 150 }} /></Field>
        <button className="btn primary" disabled={sim.busy || !periodId || !Number(iter)} onClick={() => void sim.run()}>{sim.busy ? 'Hesaplanıyor…' : 'Simüle et'}</button>
      </div>
      {sim.error && <div style={{ marginTop: 12 }}><ErrorBox error={sim.error} /></div>}
      {r && (
        <div style={{ marginTop: 12 }}>
          <div className="row">
            <Pill kind={r.result.violations === 0 && r.result.unplaced === 0 ? 'ok' : 'err'}>{r.result.violations === 0 && r.result.unplaced === 0 ? 'Sert ihlal yok' : `${r.result.violations} ihlal, ${r.result.unplaced} yerleşemeyen`}</Pill>
            <span>Ceza <b>{r.result.penalty.toLocaleString('tr-TR')}</b> · {r.result.metrics.used_slots} slot · aynı gün 2 sınav {r.result.metrics.student_days_with_two_exams.toLocaleString('tr-TR')} · {(r.result.duration_ms / 1000).toFixed(1)} sn</span>
          </div>
          {r.published && better !== null && (
            <p className="small" style={{ marginTop: 8 }}>
              Yayındaki sürüm {r.published.version}: ceza {r.published.metrics.penalty.toLocaleString('tr-TR')} →{' '}
              <b style={{ color: better < 0 ? 'var(--ok)' : better > 0 ? 'var(--error)' : undefined }}>{better === 0 ? 'aynı' : better < 0 ? `${Math.abs(better).toLocaleString('tr-TR')} daha iyi` : `${better.toLocaleString('tr-TR')} daha kötü`}</b>
            </p>
          )}
        </div>
      )}
    </Card>
  )
}

type Cmp = { a: Schedule; b: Schedule; moved_count: number; moved: { exam_id: number; course_code: string; course_name: string; from_starts_at: string; to_starts_at: string }[] }

const metricRows: [string, (m: any) => number | undefined][] = [
  ['Ceza', (m) => m.penalty],
  ['Aynı gün 2 sınav (öğrenci-gün)', (m) => m.scheduling?.student_days_with_two_exams],
  ['Ardışık günü olan öğrenci', (m) => m.scheduling?.students_with_consecutive_day],
  ['Kullanılan slot', (m) => m.scheduling?.used_slots],
  ['Salon kullanımı', (m) => m.rooms_used],
  ['Gözetmen yük farkı', (m) => m.invigilation?.spread],
]

/** Plan karşılaştırma: iki sürümün metrik farkı ve taşınan sınavlar. */
export function Compare({ schedules, current }: { schedules: Schedule[]; current: number | null }) {
  const [b, setB] = useState(0)
  const a = current ?? 0
  const cmp = useAsync(async () => (a && b && a !== b ? get<Cmp>(`/schedules/compare?a=${a}&b=${b}`) : null), [a, b])
  const others = schedules.filter((s) => s.id !== a)
  return (
    <Card title="Sürüm karşılaştırma" aside={<select aria-label="Karşılaştırılacak sürüm" value={b} onChange={(e) => setB(Number(e.target.value))} style={{ width: 'auto' }}><option value={0}>Sürüm seçin…</option>{others.map((s) => <option key={s.id} value={s.id}>Sürüm {s.version} ({s.status === 'published' ? 'yayında' : 'taslak'})</option>)}</select>}>
      {!b ? <p className="muted small">Seçili sürümü başka bir sürümle kıyaslamak için sağdan seçin.</p> : (
        <Async s={cmp}>
          {(c) => (
            <>
              <div className="tablewrap"><table>
                <thead><tr><th>Ölçüt</th><th>Sürüm {c.a.version}</th><th>Sürüm {c.b.version}</th><th>Fark</th></tr></thead>
                <tbody>{metricRows.map(([label, f]) => {
                  const x = f(c.a.metrics), y = f(c.b.metrics)
                  const d = x !== undefined && y !== undefined ? y - x : undefined
                  return <tr key={label}><td>{label}</td><td>{x?.toLocaleString('tr-TR') ?? '—'}</td><td>{y?.toLocaleString('tr-TR') ?? '—'}</td><td style={{ color: d === undefined || d === 0 ? undefined : d < 0 ? 'var(--ok)' : 'var(--error)' }}>{d === undefined ? '—' : d > 0 ? `+${d.toLocaleString('tr-TR')}` : d.toLocaleString('tr-TR')}</td></tr>
                })}</tbody>
              </table></div>
              <p style={{ margin: '12px 0 6px' }}><b>{c.moved_count}</b> sınavın slotu farklı{c.moved_count > c.moved.length ? ` (ilk ${c.moved.length} gösteriliyor)` : ''}.</p>
              {c.moved.length > 0 && <div className="tablewrap" style={{ maxHeight: 260, overflowY: 'auto' }}><table><thead><tr><th>Ders</th><th>Önce</th><th>Sonra</th></tr></thead><tbody>{c.moved.map((m) => <tr key={m.exam_id}><td><b>{m.course_code}</b> {m.course_name}</td><td>{fmtDateTime(m.from_starts_at)}</td><td>{fmtDateTime(m.to_starts_at)}</td></tr>)}</tbody></table></div>}
            </>
          )}
        </Async>
      )}
    </Card>
  )
}
