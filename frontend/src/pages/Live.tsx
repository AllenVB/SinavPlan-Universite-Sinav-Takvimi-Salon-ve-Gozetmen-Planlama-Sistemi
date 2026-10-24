import { useEffect, useState } from 'react'
import { get, fmtTime } from '../api'
import { Async, Bar, Card, Empty, Pill, Stat, useAsync } from '../ui'
import { ScheduleSelect, useExams, useSchedules } from './common'

type Row = { student_id: number; status: string; source: string; device_id: string; updated_at: string }
type Att = { summary: { enrolled: number; present: number; absent: number; conflicts: number }; items: Row[] }

export function Live() {
  const sch = useSchedules()
  const exams = useExams(sch.sel)
  const [exam, setExam] = useState(0)
  const [auto, setAuto] = useState(true)
  const att = useAsync(async () => {
    if (!exam) return null
    const first = await get<Att & { next_cursor?: string }>(`/exams/${exam}/attendance?limit=200`)
    return { summary: first.summary, items: first.items }
  }, [exam])
  useEffect(() => {
    if (!auto || !exam) return
    const t = setInterval(att.reload, 5000)
    return () => clearInterval(t)
  }, [auto, exam, att.reload])
  const s = att.data?.summary
  const pct = s && s.enrolled ? (s.present / s.enrolled) * 100 : 0
  return (
    <>
      <div className="head">
        <div><div className="caps">Sınav günü</div><h1>Canlı yoklama izleme</h1></div>
        <div className="actions"><ScheduleSelect s={sch} /><label className="row small"><input type="checkbox" checked={auto} onChange={(e) => setAuto(e.target.checked)} />5 sn’de bir yenile</label></div>
      </div>
      <Card title="Sınav">
        <Async s={exams} empty={(d) => d.length === 0}>
          {(list) => (
            <select aria-label="Sınav" value={exam} onChange={(e) => setExam(Number(e.target.value))} style={{ maxWidth: 560 }}>
              <option value={0}>Sınav seçin…</option>
              {list.map((e) => <option key={e.exam_id} value={e.exam_id}>{fmtTime(e.starts_at)} · {e.course_code} — {e.course_name}</option>)}
            </select>
          )}
        </Async>
      </Card>
      {!exam ? <Card><Empty title="İzlemek için bir sınav seçin" /></Card> : (
        <Async s={att}>
          {(d) => (
            <>
              <div className="grid g4">
                <Stat label="Kayıtlı" value={d.summary.enrolled} />
                <Stat label="Geldi" value={d.summary.present} note={`%${pct.toFixed(0)} katılım`} />
                <Stat label="Gelmedi" value={d.summary.absent} />
                <Stat label="Çakışma" value={d.summary.conflicts} note={d.summary.conflicts ? <a href="#/cakisma">İncele →</a> : 'yok'} />
              </div>
              <Card title="Katılım"><Bar pct={pct} /></Card>
              <Card title="Yoklama kayıtları" aside={<span className="small muted">en son durum, öğrenci başına</span>}>
                {d.items.length === 0 ? <Empty title="Henüz yoklama girilmedi" hint="Gözetmenler kart okuttukça veya cihaz senkronladıkça burada görünür." /> : (
                  <div className="tablewrap" style={{ maxHeight: 420, overflowY: 'auto' }}><table>
                    <thead><tr><th>Öğrenci</th><th>Durum</th><th>Kaynak</th><th>Cihaz</th><th>Güncelleme</th></tr></thead>
                    <tbody>{d.items.map((r) => <tr key={r.student_id}><td>#{r.student_id}</td><td><Pill kind={r.status === 'present' ? 'ok' : 'err'}>{r.status === 'present' ? 'Geldi' : 'Gelmedi'}</Pill></td><td>{r.source === 'qr' ? 'QR kart' : 'Elle'}</td><td className="mono small">{r.device_id}</td><td>{fmtTime(r.updated_at)}</td></tr>)}</tbody>
                  </table></div>
                )}
              </Card>
            </>
          )}
        </Async>
      )}
    </>
  )
}

