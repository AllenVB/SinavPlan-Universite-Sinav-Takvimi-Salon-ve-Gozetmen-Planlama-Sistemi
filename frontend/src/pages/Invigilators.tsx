import { useMemo, useState } from 'react'
import { all, fmtDateTime, idemKey, post } from '../api'
import { useAuth } from '../auth'
import { Async, Card, ErrorBox, Pill, Stat, useAction, useAsync } from '../ui'
import { hasRole, ScheduleSelect, useSchedules } from './common'

type Duty = { id: number; user_id: number; full_name: string; slot_id: number; day: number; session: number; starts_at: string; room_code: string; building: string; exam_id: number; course_code: string }
type Removal = { vacated: number; swaps: number; violations: number; changes: unknown[]; unfilled: unknown[] }

export function Invigilators() {
  const { user } = useAuth()
  const sch = useSchedules()
  const duties = useAsync(() => (sch.sel ? all<Duty>(`/schedules/${sch.sel}/invigilators`) : Promise.resolve([] as Duty[])), [sch.sel])
  const [q, setQ] = useState('')
  const [page, setPage] = useState(0)
  const secretary = hasRole(user!.roles, 'secretary')
  const load = useMemo(() => {
    const m = new Map<number, { name: string; n: number }>()
    duties.data?.forEach((d) => m.set(d.user_id, { name: d.full_name, n: (m.get(d.user_id)?.n ?? 0) + 1 }))
    return m
  }, [duties.data])
  const loads = [...load.values()].map((x) => x.n)
  const rows = useMemo(() => (duties.data ?? []).filter((d) => `${d.full_name} ${d.room_code} ${d.course_code}`.toLowerCase().includes(q.toLowerCase())), [duties.data, q])
  const pages = Math.ceil(rows.length / 50)
  const [removed, setRemoved] = useState<{ name: string; r: Removal } | null>(null)
  const remove = useAction(async (uid: number, name: string) => {
    const r = await post<Removal>(`/schedules/${sch.sel}/invigilators/${uid}/remove`, undefined, { 'Idempotency-Key': idemKey(`remove-${sch.sel}-${uid}`) })
    setRemoved({ name, r })
    duties.reload()
  })
  return (
    <>
      <div className="head">
        <div>
          <div className="caps">Gözetmen yönetimi</div>
          <h1>Gözetmen atama ve takip</h1>
        </div>
        <ScheduleSelect s={sch} />
      </div>
      <div className="grid g4">
        <Stat label="Görev" value={duties.data?.length ?? '—'} />
        <Stat label="Gözetmen" value={load.size || '—'} />
        <Stat label="Yük aralığı" value={loads.length ? `${Math.min(...loads)}–${Math.max(...loads)}` : '—'} note={loads.length ? `fark ${Math.max(...loads) - Math.min(...loads)} (adil dağılım)` : undefined} />
      </div>
      {removed && (
        <div className="alert ok" role="status">
          <div><b>{removed.name} çizelgeden çıkarıldı.</b> {removed.r.vacated} görev boşaldı, {removed.r.changes.length} görev yeniden atandı ({removed.r.swaps} takas), kalan ihlal: {removed.r.violations}, doldurulamayan: {removed.r.unfilled.length}. Diğer tüm atamalar korundu.</div>
        </div>
      )}
      {remove.error && <ErrorBox error={remove.error} />}
      <Card title="Görev listesi" aside={<input className="input" style={{ width: 260 }} placeholder="Gözetmen, salon veya ders ara…" aria-label="Ara" value={q} onChange={(e) => (setQ(e.target.value), setPage(0))} />}>
        <Async s={duties} empty={(d) => d.length === 0}>
          {() => (
            <>
              <div className="tablewrap">
                <table>
                  <thead><tr><th>Gözetmen</th><th>Tarih / saat</th><th>Salon</th><th>Ders</th>{secretary && <th />}</tr></thead>
                  <tbody>
                    {rows.slice(page * 50, page * 50 + 50).map((d) => (
                      <tr key={d.id}>
                        <td>{d.full_name}</td><td>{fmtDateTime(d.starts_at)}</td><td>{d.room_code} <span className="muted small">{d.building}</span></td><td>{d.course_code}</td>
                        {secretary && <td><button className="btn sm danger" disabled={remove.busy} onClick={() => confirm(`${d.full_name} tüm görevlerinden çıkarılsın mı? Boşalan görevler minimum değişiklikle yeniden atanır.`) && void remove.run(d.user_id, d.full_name)}>Hasta / çıkar</button></td>}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <div className="row" style={{ marginTop: 12, justifyContent: 'space-between' }}>
                <span className="small muted">{rows.length} kayıt · sayfa {page + 1}/{Math.max(1, pages)}</span>
                <div className="row"><button className="btn sm" disabled={page === 0} onClick={() => setPage(page - 1)}>Önceki</button><button className="btn sm" disabled={page + 1 >= pages} onClick={() => setPage(page + 1)}>Sonraki</button></div>
              </div>
            </>
          )}
        </Async>
      </Card>
      <Card title="Gözetmen yükleri"><Pill kind="info">{load.size} gözetmen</Pill> <span className="small muted">Yükler görev sayısına göre; adalet hedefi en çok–en az farkını en aza indirmektir.</span></Card>
    </>
  )
}
