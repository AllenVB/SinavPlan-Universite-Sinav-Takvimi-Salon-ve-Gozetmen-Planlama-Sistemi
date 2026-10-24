import { useState } from 'react'
import { get } from '../api'
import { Async, Card, Empty, Pill, Tabs, useAsync } from '../ui'
import { ScheduleSelect, useExams, useSchedules } from './common'

type SC = { student_id: number; student_no: string; day: number; exam_ids: number[] }
type AC = { id: number; exam_id: number; student_id: number; rule: string; detected_at: string }
const RULE: Record<string, string> = { qr_present: 'QR okutma üstün', hlc: 'Daha yeni saat damgası', device: 'Cihaz sırası', event: 'Olay sırası' }

export function Conflicts() {
  const [tab, setTab] = useState<'plan' | 'att'>('plan')
  return (
    <>
      <div className="head">
        <div><div className="caps">Çakışma ve uyarı masası</div><h1>Çakışmalar</h1></div>
        <Tabs value={tab} onChange={setTab} items={[['plan', 'Takvim çakışmaları'], ['att', 'Yoklama çakışmaları']]} />
      </div>
      {tab === 'plan' ? <PlanConflicts /> : <AttendanceConflicts />}
    </>
  )
}

function PlanConflicts() {
  const sch = useSchedules()
  const exams = useExams(sch.sel)
  const [items, setItems] = useState<SC[]>([])
  const [cursor, setCursor] = useState<string | null>(null)
  const first = useAsync(async () => {
    if (!sch.sel) return null
    const r = await get<{ items: SC[]; next_cursor: string | null }>(`/schedules/${sch.sel}/conflicts?limit=100`)
    setItems(r.items)
    setCursor(r.next_cursor)
    return r
  }, [sch.sel])
  const code = new Map(exams.data?.map((e) => [e.exam_id, e.course_code]))
  const loadMore = async () => {
    const r = await get<{ items: SC[]; next_cursor: string | null }>(`/schedules/${sch.sel}/conflicts?limit=100&after=${cursor}`)
    setItems((x) => [...x, ...r.items])
    setCursor(r.next_cursor)
  }
  return (
    <Card title="Aynı gün birden çok sınavı olan öğrenciler" aside={<ScheduleSelect s={sch} />}>
      <p className="small muted" style={{ marginBottom: 12 }}>Bunlar yumuşak hedef ihlalleridir: aynı slotta çakışma yoktur (sert kısıt), ancak öğrenci aynı gün iki sınava girer.</p>
      <Async s={first} empty={() => items.length === 0}>
        {() => (
          <>
            <div className="tablewrap"><table>
              <thead><tr><th>Öğrenci no</th><th>Gün</th><th>Sınavlar</th></tr></thead>
              <tbody>{items.map((c) => <tr key={`${c.student_id}-${c.day}`}><td>{c.student_no}</td><td>{c.day + 1}. gün</td><td>{c.exam_ids.map((id) => <Pill key={id}>{code.get(id) ?? `#${id}`}</Pill>)}</td></tr>)}</tbody>
            </table></div>
            {cursor && <div style={{ marginTop: 12 }}><button className="btn" onClick={() => void loadMore()}>Daha fazla yükle</button></div>}
          </>
        )}
      </Async>
    </Card>
  )
}

function AttendanceConflicts() {
  const list = useAsync(async () => (await get<{ items: AC[] }>('/attendance/conflicts?limit=100')).items)
  return (
    <Card title="Yoklama çakışmaları" aside={<span className="small muted">iki cihaz farklı durum bildirdi</span>}>
      <Async s={list} empty={(d) => d.length === 0}>
        {(d) => d.length === 0 ? <Empty title="Çakışma yok" /> : (
          <div className="tablewrap"><table>
            <thead><tr><th>Sınav</th><th>Öğrenci</th><th>Çözüm kuralı</th><th>Tespit</th></tr></thead>
            <tbody>{d.map((c) => <tr key={c.id}><td>#{c.exam_id}</td><td>#{c.student_id}</td><td><Pill kind="info">{RULE[c.rule] ?? c.rule}</Pill></td><td>{new Date(c.detected_at).toLocaleString('tr-TR', { timeZone: 'Europe/Istanbul' })}</td></tr>)}</tbody>
          </table></div>
        )}
      </Async>
    </Card>
  )
}
