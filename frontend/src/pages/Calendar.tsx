import { useEffect, useMemo, useState } from 'react'
import { get, idemKey, post, fmtDate, fmtTime } from '../api'
import { useAuth } from '../auth'
import { Async, Card, ErrorBox, Empty, Field, Pill, useAction, useAsync } from '../ui'
import { hasRole, ScheduleSelect, useExams, useSchedules, type Exam } from './common'
import { Compare, ExportButtons, Simulator } from './Tools'

export function Calendar() {
  const { user } = useAuth()
  const sch = useSchedules()
  const secretary = hasRole(user!.roles, 'secretary')
  const exams = useExams(sch.sel)
  const [q, setQ] = useState('')
  const filtered = useMemo(() => (exams.data ?? []).filter((e) => `${e.course_code} ${e.course_name}`.toLowerCase().includes(q.toLowerCase())), [exams.data, q])
  const byDay = useMemo(() => {
    const m = new Map<number, Exam[]>()
    filtered.forEach((e) => m.set(e.day, [...(m.get(e.day) ?? []), e]))
    return [...m.entries()].sort((a, b) => a[0] - b[0])
  }, [filtered])

  const publish = useAction(async () => {
    await post(`/schedules/${sch.sel}/publish`, undefined, { 'Idempotency-Key': idemKey(`publish-${sch.sel}`) })
    sch.list.reload()
  })
  const m = sch.current?.metrics

  return (
    <>
      <div className="head">
        <div>
          <div className="caps">Sınav takvimi</div>
          <h1>Takvim ve dağıtım</h1>
        </div>
        <div className="actions">
          <ScheduleSelect s={sch} />
          {secretary && sch.current?.status === 'draft' && (
            <button className="btn primary" disabled={publish.busy} onClick={() => void publish.run()}>
              {publish.busy ? 'Yayınlanıyor…' : 'Bu sürümü yayınla'}
            </button>
          )}
        </div>
      </div>
      {publish.error && <ErrorBox error={publish.error} />}
      <div className="noprint"><ExportButtons schedule={sch.sel} /></div>
      {secretary && <div className="noprint"><Generator onDone={() => (sch.list.reload(), sch.setSel(null))} /></div>}
      {secretary && <div className="noprint"><Simulator periodId={sch.current?.period_id} /></div>}
      {secretary && sch.list.data && sch.list.data.length > 1 && <div className="noprint"><Compare schedules={sch.list.data} current={sch.sel} /></div>}

      {m && (
        <div className="grid g4">
          <Card title="Kalite"><div className="small">Ceza <b>{m.penalty.toLocaleString('tr-TR')}</b> · iterasyon {m.iterations.toLocaleString('tr-TR')}</div><div className="small muted">Ardışık gün: {m.scheduling.students_with_consecutive_day} öğrenci · aynı gün 2 sınav: {m.scheduling.students_with_two_exams_a_day}</div></Card>
          <Card title="Salon"><div className="small"><b>{m.rooms_used}</b> salon kullanımı</div><div className="small muted">Oturtulamayan: {m.unseated} · ihlal: {m.room_violations + m.seat_violations}</div></Card>
          <Card title="Gözetmen"><div className="small"><b>{m.invigilation.assignments}</b> görev · {m.invigilation.invigilators} gözetmen</div><div className="small muted">Yük {m.invigilation.min}–{m.invigilation.max} (fark {m.invigilation.spread})</div></Card>
          <Card title="Durum">{sch.current && <Pill kind={sch.current.status === 'published' ? 'ok' : 'warn'}>{sch.current.status === 'published' ? 'Yayında' : 'Taslak'}</Pill>}<div className="small muted" style={{ marginTop: 6 }}>Sürüm {sch.current?.version} · tohum {sch.current?.seed}</div></Card>
        </div>
      )}

      <Card title="Sınav oturumları" aside={<input className="input" style={{ width: 240 }} placeholder="Ders ara…" aria-label="Ders ara" value={q} onChange={(e) => setQ(e.target.value)} />}>
        {sch.list.data?.length === 0 ? (
          <Empty title="Henüz planlanmış sınav yok" hint={secretary ? 'Yukarıdan "Takvimi oluştur" ile ilk çizelgeyi üretin.' : 'Çizelge yayınlandığında burada görünür.'} />
        ) : (
          <Async s={exams} empty={() => false}>
            {() =>
              byDay.length === 0 ? (
                <Empty title="Eşleşen ders yok" />
              ) : (
                <div className="tablewrap">
                  <table>
                    <thead><tr><th>Tarih</th><th>Saat</th><th>Ders</th><th>Öğrenci</th><th>Salon</th><th>Tür</th></tr></thead>
                    <tbody>
                      {byDay.flatMap(([, list]) =>
                        list.map((e, i) => (
                          <tr key={e.exam_id}>
                            <td>{i === 0 ? fmtDate(e.starts_at) : ''}</td>
                            <td>{fmtTime(e.starts_at)}–{fmtTime(e.ends_at)}</td>
                            <td><b>{e.course_code}</b> {e.course_name}</td>
                            <td>{e.students}</td>
                            <td>{e.rooms}</td>
                            <td>{e.requires_lab ? <Pill kind="info">Laboratuvar</Pill> : <Pill>Klasik</Pill>}</td>
                          </tr>
                        )),
                      )}
                    </tbody>
                  </table>
                </div>
              )
            }
          </Async>
        )}
      </Card>
    </>
  )
}

/** Asenkron takvim işi: başlat, durumu yokla, bitince sürümleri yenile. */
function Generator({ onDone }: { onDone: () => void }) {
  const period = useAsync(async () => (await get('/periods')).items?.[0] as { id: number; name: string } | undefined)
  const [seed, setSeed] = useState('42')
  const [iter, setIter] = useState('300000')
  const [job, setJob] = useState<number | null>(null)
  const status = useAsync(async () => (job ? get(`/schedule-jobs/${job}`) : null), [job])
  const start = useAction(async () => {
    const r = await post('/schedules', { period_id: period.data!.id, seed: Number(seed), iterations: Number(iter) })
    setJob(r.job_id)
  })
  const st: string | undefined = status.data?.status
  useEffect(() => {
    if (!job || st === 'succeeded' || st === 'failed') return
    const t = setTimeout(status.reload, 1500)
    return () => clearTimeout(t)
  }, [job, st, status.data, status.reload])
  useEffect(() => {
    if (st === 'succeeded') onDone()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [st])
  const running = job !== null && st !== 'succeeded' && st !== 'failed'
  return (
    <Card title="Takvimi oluştur" aside={st && <Pill kind={st === 'succeeded' ? 'ok' : st === 'failed' ? 'err' : 'info'}>{{ queued: 'Sırada', running: 'Çalışıyor…', succeeded: 'Tamamlandı', failed: 'Başarısız' }[st]}</Pill>}>
      <div className="row" style={{ alignItems: 'flex-end' }}>
        <Field label="Tohum" id="seed"><input id="seed" className="input" inputMode="numeric" value={seed} onChange={(e) => setSeed(e.target.value)} style={{ width: 110 }} /></Field>
        <Field label="İterasyon" id="iter"><input id="iter" className="input" inputMode="numeric" value={iter} onChange={(e) => setIter(e.target.value)} style={{ width: 140 }} /></Field>
        <button className="btn primary" disabled={start.busy || running || !period.data || !Number(seed) || !Number(iter)} onClick={() => void start.run()}>
          {running ? 'Üretiliyor…' : 'Takvimi oluştur'}
        </button>
        <span className="small muted">Aynı tohum ve iterasyonla sonuç her zaman aynıdır. {period.data?.name}</span>
      </div>
      {start.error && <div style={{ marginTop: 12 }}><ErrorBox error={start.error} /></div>}
      {st === 'failed' && (
        <div className="alert err" style={{ marginTop: 12 }} role="alert">
          <div><b>Çizelge üretilemedi.</b> Çözümsüzlük raporu: <span className="mono small">{String(status.data?.error ?? '').slice(0, 400)}</span></div>
        </div>
      )}
    </Card>
  )
}
