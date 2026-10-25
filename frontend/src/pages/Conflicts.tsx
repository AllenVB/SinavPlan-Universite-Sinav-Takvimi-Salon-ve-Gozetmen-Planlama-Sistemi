import { Fragment, useState } from 'react'
import { fmtDateTime, get, post } from '../api'
import { useAuth } from '../auth'
import { Async, Card, Empty, ErrorBox, Pill, Tabs, useAction, useAsync } from '../ui'
import { hasRole, ScheduleSelect, useExams, useSchedules } from './common'

type SC = { student_id: number; student_no: string; day: number; exam_ids: number[] }
type AC = { id: number; exam_id: number; student_id: number; rule: string; detected_at: string }
type Metrics = { penalty: number; student_days_with_two_exams: number; students_with_consecutive_day: number }
type Fix = { suggestion: { exam_id: number; from_slot_id: number; to_slot_id: number; from_starts_at: string; to_starts_at: string; delta: number; violations: number; metrics_after: Metrics }; course_code: string; course_name: string }
type FixResp = { mode: string; metrics_before: Metrics; student_days: number[]; items: Fix[] }
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
  const [open, setOpen] = useState<string | null>(null)
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
              <thead><tr><th>Öğrenci no</th><th>Gün</th><th>Sınavlar</th><th /></tr></thead>
              <tbody>{items.map((c) => {
                const k = `${c.student_id}-${c.day}`
                return (
                  <Fragment key={k}>
                    <tr><td>{c.student_no}</td><td>{c.day + 1}. gün</td><td>{c.exam_ids.map((id) => <Pill key={id}>{code.get(id) ?? `#${id}`}</Pill>)}</td>
                      <td><button className="btn sm" aria-expanded={open === k} onClick={() => setOpen(open === k ? null : k)}>{open === k ? 'Kapat' : 'Çöz'}</button></td></tr>
                    {open === k && sch.sel && <tr><td colSpan={4}><FixPanel schedule={sch.sel} student={c.student_id} onApplied={() => sch.list.reload()} /></td></tr>}
                  </Fragment>
                )
              })}</tbody>
            </table></div>
            {cursor && <div style={{ marginTop: 12 }}><button className="btn" onClick={() => void loadMore()}>Daha fazla yükle</button></div>}
          </>
        )}
      </Async>
    </Card>
  )
}

/** Faz 22 B1: öğrencinin aynı gün iki sınavını ayıran taşıma seçenekleri; sekreter tek tıkla yeni taslak sürüm üretir. */
function FixPanel({ schedule, student, onApplied }: { schedule: number; student: number; onApplied: () => void }) {
  const { user } = useAuth()
  const secretary = hasRole(user!.roles, 'secretary')
  const fx = useAsync(() => get<FixResp>(`/schedules/${schedule}/fix-suggestions?student_id=${student}&limit=3`), [schedule, student])
  const apply = useAction(async (f: Fix) => {
    const r = await post<{ schedule_id: number; version: number }>(`/schedules/${schedule}/exams/${f.suggestion.exam_id}/move`, { slot_id: f.suggestion.to_slot_id })
    onApplied()
    return r
  })
  const sign = (n: number) => (n > 0 ? `+${n}` : String(n))
  return (
    <div className="fixpanel" data-testid="fix-panel">
      <Async s={fx} empty={(d) => d.items.length === 0}>
        {(d) => d.items.length === 0 ? <Empty title="Uygun taşıma yok" hint="Öğrencinin sınavları, kısıtları bozmadan boş bir güne taşınamıyor (başlamış oturumlar kilitli)." /> : (
          <>
            <p className="small muted" style={{ margin: '0 0 8px' }}>Öğrencinin sınavlarından biri, başka sınavı olmayan bir güne taşınır. Her seçenek bağımsız doğrulayıcıdan geçti; <b>rahatsızlık</b> tüm öğrencilerin toplam cezasıdır (düşük = iyi).</p>
            <ol className="fixlist">
              {d.items.map((f, i) => {
                const g = f.suggestion
                return (
                  <li key={`${g.exam_id}-${g.to_slot_id}`}>
                    <div><b>{f.course_code}</b> {f.course_name}</div>
                    <div className="small">{fmtDateTime(g.from_starts_at)} → <b>{fmtDateTime(g.to_starts_at)}</b></div>
                    <div className="row small" style={{ gap: 6 }}>
                      <Pill kind={g.violations === 0 ? 'ok' : 'err'}>{g.violations === 0 ? 'İhlal 0 · doğrulandı' : `${g.violations} ihlal`}</Pill>
                      <Pill kind={g.delta <= 0 ? 'ok' : 'warn'}>Rahatsızlık {sign(g.delta)}</Pill>
                      <span className="muted">aynı gün 2 sınav: {d.metrics_before.student_days_with_two_exams} → {g.metrics_after.student_days_with_two_exams}</span>
                    </div>
                    {secretary && <button className={`btn sm ${i === 0 ? 'primary' : ''}`} disabled={apply.busy} onClick={() => void apply.run(f)}>{apply.busy ? 'Uygulanıyor…' : 'Uygula (yeni taslak)'}</button>}
                  </li>
                )
              })}
            </ol>
            {!secretary && <p className="small muted">Uygulama yetkisi bölüm sekreterindedir.</p>}
          </>
        )}
      </Async>
      {apply.error && <ErrorBox error={apply.error} />}
      {apply.result && <div className="alert ok" role="status"><div><b>Sürüm {apply.result.version} (taslak) oluşturuldu.</b> Yayınlamak için <a href="#/takvim">Sınav takvimi</a> ekranında bu sürümü seçin.</div></div>}
    </div>
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
