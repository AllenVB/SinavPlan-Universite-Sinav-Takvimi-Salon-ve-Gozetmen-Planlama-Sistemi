import { useMemo, useState } from 'react'
import { all, fmtDateTime } from '../api'
import { Async, Card, Pill, useAsync } from '../ui'

type Duty = { id: number; user_id: number; full_name: string; starts_at: string; room_code: string; building: string; course_code: string }

/**
 * Gözetmen programı: her gözetmen için hangi salona, hangi gün ve saatte gideceği.
 * `compact` (ana panel): ilk birkaç gözetmen + "Tümü" bağlantısı; tam hâli Gözetmen atama ekranında.
 */
export function DutyRoster({ schedule, compact = false }: { schedule: number | null; compact?: boolean }) {
  const duties = useAsync(() => (schedule ? all<Duty>(`/schedules/${schedule}/invigilators`) : Promise.resolve([] as Duty[])), [schedule])
  const [q, setQ] = useState('')
  const people = useMemo(() => {
    const m = new Map<number, { name: string; duties: Duty[] }>()
    for (const d of duties.data ?? []) (m.get(d.user_id) ?? m.set(d.user_id, { name: d.full_name, duties: [] }).get(d.user_id)!).duties.push(d)
    m.forEach((p) => p.duties.sort((a, b) => a.starts_at.localeCompare(b.starts_at)))
    return [...m.entries()].map(([id, p]) => ({ id, ...p })).sort((a, b) => a.name.localeCompare(b.name, 'tr'))
  }, [duties.data])
  const t = q.trim().toLocaleLowerCase('tr')
  const shown = people.filter((p) => !t || p.name.toLocaleLowerCase('tr').includes(t) || p.duties.some((d) => d.room_code.toLowerCase().includes(t)))
  const rows = compact ? shown.slice(0, 8) : shown
  return (
    <Card title="Gözetmen programı" aside={compact
      ? <a className="small" href="#/gozetmenler">Tümü ({people.length}) →</a>
      : <input className="input" style={{ width: 260 }} placeholder="Gözetmen veya salon ara…" aria-label="Gözetmen programında ara" value={q} onChange={(e) => setQ(e.target.value)} />}>
      <Async s={duties} empty={(d) => d.length === 0}>
        {() => (
          <div className="tablewrap" tabIndex={0} role="region" aria-label="Gözetmen programı" style={{ maxHeight: compact ? undefined : 420, overflowY: 'auto' }} data-testid="duty-roster">
            <table>
              <thead><tr><th>Gözetmen</th><th>Görev</th><th>Nereye / ne zaman</th></tr></thead>
              <tbody>
                {rows.map((p) => (
                  <tr key={p.id}>
                    <td><b>{p.name}</b></td>
                    <td><Pill kind="info">{p.duties.length}</Pill></td>
                    <td>
                      <ul className="checks" style={{ gap: 2 }}>
                        {(compact ? p.duties.slice(0, 2) : p.duties).map((d) => (
                          <li key={d.id}><span className="small">{fmtDateTime(d.starts_at)} · <b>{d.room_code}</b> <span className="muted">{d.building} · {d.course_code}</span></span></li>
                        ))}
                        {compact && p.duties.length > 2 && <li className="small muted">+{p.duties.length - 2} görev daha</li>}
                      </ul>
                    </td>
                  </tr>
                ))}
                {rows.length === 0 && <tr><td colSpan={3} className="muted small">Eşleşen gözetmen yok.</td></tr>}
              </tbody>
            </table>
          </div>
        )}
      </Async>
    </Card>
  )
}
