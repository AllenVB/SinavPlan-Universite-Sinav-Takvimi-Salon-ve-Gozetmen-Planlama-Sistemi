import { useState } from 'react'
import { download, get, post, fmtDateTime } from '../api'
import { Card, ErrorBox, Pill, TuneFields, useAction, useAsync, Async } from '../ui'
import { useFaculties, type Exam, type Schedule } from './common'

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

type Sim = { result: { penalty: number; construction_penalty: number; iterations: number; duration_ms: number; unplaced: number; violations: number; metrics: { used_slots: number; student_days_with_two_exams: number; students_with_consecutive_day: number } }; scope: { faculty?: string; departments: string[]; years: number[]; exams: number; students: number }; published?: { version: number; metrics: { penalty: number } } }

const yearLabel = (ys: number[]) => (ys.length === 4 ? 'tüm sınıflar' : ys.map((y) => `${y}.`).join(', ') + ' sınıf')

/** Sınav provası: önce fakülte, sonra bölümler, sonra sınıflar seçilir; yalnızca o kapsamdaki sınavlar denenir. Kaydedilmez. */
export function Simulator({ periodId, exams }: { periodId: number | undefined; exams: Exam[] | null }) {
  const fac = useFaculties()
  const [facId, setFacId] = useState(0) // 0 = tüm fakülteler
  const [depts, setDepts] = useState<Set<number>>(new Set())
  const [years, setYears] = useState<Set<number>>(new Set([1, 2, 3, 4]))
  const [seed, setSeed] = useState('7')
  const [iter, setIter] = useState(50_000)
  const faculty = fac.data?.find((f) => f.id === facId)
  const chosenDepts = faculty ? faculty.departments.filter((d) => depts.has(d.id)) : []
  const scope = { faculty_id: facId || undefined, department_ids: faculty ? chosenDepts.map((d) => d.id) : undefined, years: years.size === 4 ? undefined : [...years].sort() }
  // seçimin önizlemesi: mevcut takvimden (gerçek veri) kaç sınav kapsama giriyor
  const inScope = (e: Exam) => (!facId || e.faculty_name === faculty?.name) && (!faculty || chosenDepts.length === 0 || chosenDepts.some((d) => d.id === e.department_id)) && years.has(e.year)
  const preview = exams?.filter(inScope).length ?? null
  const sim = useAction(() => post<Sim>('/schedules/simulate', { period_id: periodId, seed: Number(seed), iterations: iter, scope }))
  const r = sim.result
  const better = r?.published ? r.result.penalty - r.published.metrics.penalty : null
  const pickFaculty = (id: number) => { setFacId(id); setDepts(new Set(fac.data?.find((f) => f.id === id)?.departments.map((d) => d.id) ?? [])); sim.reset() }
  const toggle = <T,>(set: Set<T>, v: T, put: (s: Set<T>) => void) => { const n = new Set(set); n.has(v) ? n.delete(v) : n.add(v); put(n); sim.reset() }
  const summary = [faculty?.name ?? 'Tüm fakülteler', faculty ? (chosenDepts.length === faculty.departments.length ? 'tüm bölümler' : chosenDepts.map((d) => d.name).join(', ') || 'bölüm seçilmedi') : null, yearLabel([...years].sort())].filter(Boolean).join(' · ')
  return (
    <Card title="Sınav provası" aside={<Pill>Kaydedilmez</Pill>}>
      <p className="small muted" style={{ marginBottom: 12 }}>Gerçek takvimi oluşturmadan önce denemek için: <b>1.</b> fakülteyi, <b>2.</b> bölümleri, <b>3.</b> sınıfları seçin. Yalnızca seçilen kapsamdaki sınavlar için takvim hesaplanır; hiçbir sürüm oluşmaz.</p>

      <div className="caps" style={{ marginBottom: 6 }}>1 · Fakülte</div>
      <div className="row" style={{ marginBottom: 14 }}>
        <button className="btn" aria-pressed={facId === 0} onClick={() => pickFaculty(0)} style={facId === 0 ? { borderColor: 'var(--primary)', background: 'var(--primary-fixed)' } : undefined}>Tüm fakülteler</button>
        {fac.data?.map((f) => <button key={f.id} className="btn" aria-pressed={facId === f.id} onClick={() => pickFaculty(f.id)} style={facId === f.id ? { borderColor: 'var(--primary)', background: 'var(--primary-fixed)' } : undefined}>{f.name}</button>)}
      </div>

      {faculty && (
        <>
          <div className="row between"><div className="caps" style={{ marginBottom: 6 }}>2 · Bölümler</div><button className="btn sm" onClick={() => { setDepts(chosenDepts.length === faculty.departments.length ? new Set() : new Set(faculty.departments.map((d) => d.id))); sim.reset() }}>{chosenDepts.length === faculty.departments.length ? 'Hiçbirini seçme' : 'Tümünü seç'}</button></div>
          <div className="row" style={{ marginBottom: 14 }}>
            {faculty.departments.map((d) => (
              <label key={d.id} className="row card" style={{ padding: '8px 12px', gap: 8, borderColor: depts.has(d.id) ? 'var(--primary)' : undefined }}>
                <input type="checkbox" checked={depts.has(d.id)} onChange={() => toggle(depts, d.id, setDepts)} />
                <span><b>{d.name}</b><div className="small muted">{d.students} öğrenci · {d.courses} ders</div></span>
              </label>
            ))}
          </div>
        </>
      )}

      <div className="caps" style={{ marginBottom: 6 }}>{faculty ? '3' : '2'} · Sınıflar</div>
      <div className="row" style={{ marginBottom: 14 }}>
        {[1, 2, 3, 4].map((y) => <label key={y} className="row card" style={{ padding: '8px 14px', gap: 8, borderColor: years.has(y) ? 'var(--primary)' : undefined }}><input type="checkbox" checked={years.has(y)} onChange={() => toggle(years, y, setYears)} /> {y}. sınıf</label>)}
      </div>

      <div className="banner info" style={{ marginBottom: 12 }}>
        <div style={{ flex: 1 }}><b>Kapsam:</b> {summary}<div className="small">{preview === null ? '' : preview === 0 ? 'Bu seçimde sınav yok.' : `${preview} sınav provaya girecek.`}</div></div>
      </div>

      <div className="row" style={{ alignItems: 'flex-end' }}>
        <TuneFields idPrefix="sim" seed={seed} setSeed={setSeed} quality={iter} setQuality={setIter} presets={[{ label: 'Hızlı (yaklaşık 1 sn)', iterations: 20_000 }, { label: 'Dengeli — önerilen', iterations: 50_000 }, { label: 'Detaylı (birkaç sn)', iterations: 200_000 }]} />
        <button className="btn primary" disabled={sim.busy || !periodId || preview === 0 || (!!faculty && chosenDepts.length === 0) || years.size === 0} onClick={() => void sim.run()}>{sim.busy ? 'Hesaplanıyor…' : 'Provayı çalıştır'}</button>
      </div>
      {sim.error && <div style={{ marginTop: 12 }}><ErrorBox error={sim.error} /></div>}
      {r && (
        <div style={{ marginTop: 12 }}>
          <div className="row">
            <Pill kind={r.result.violations === 0 && r.result.unplaced === 0 ? 'ok' : 'err'}>{r.result.violations === 0 && r.result.unplaced === 0 ? 'Sert ihlal yok' : `${r.result.violations} ihlal, ${r.result.unplaced} yerleşemeyen`}</Pill>
            <span><b>{r.scope.exams}</b> sınav · <b>{r.scope.students.toLocaleString('tr-TR')}</b> öğrenci · rahatsızlık puanı <b>{r.result.penalty.toLocaleString('tr-TR')}</b> · {r.result.metrics.used_slots} oturum · aynı gün 2 sınav {r.result.metrics.student_days_with_two_exams.toLocaleString('tr-TR')} · {(r.result.duration_ms / 1000).toFixed(1)} sn</span>
          </div>
          <p className="small muted" style={{ marginTop: 6 }}>{r.scope.faculty ?? 'Tüm fakülteler'} · {r.scope.departments.join(', ')} · {yearLabel(r.scope.years)}</p>
          {r.published && better !== null && (
            <p className="small" style={{ marginTop: 8 }}>
              Yayındaki sürüm {r.published.version}: rahatsızlık puanı {r.published.metrics.penalty.toLocaleString('tr-TR')} →{' '}
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
  ['Rahatsızlık puanı (düşük = iyi)', (m) => m.penalty],
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
              {c.moved.length > 0 && <div className="tablewrap" style={{ maxHeight: 260, overflowY: 'auto' }} tabIndex={0} role="region" aria-label="Slotu değişen sınavlar"><table><thead><tr><th>Ders</th><th>Önce</th><th>Sonra</th></tr></thead><tbody>{c.moved.map((m) => <tr key={m.exam_id}><td><b>{m.course_code}</b> {m.course_name}</td><td>{fmtDateTime(m.from_starts_at)}</td><td>{fmtDateTime(m.to_starts_at)}</td></tr>)}</tbody></table></div>}
            </>
          )}
        </Async>
      )}
    </Card>
  )
}
