import { useState } from 'react'
import { all, get, idemKey, post, fmtDateTime } from '../api'
import { useAuth } from '../auth'
import { Async, Card, Empty, ErrorBox, Field, StatCard, Status, Tabs, useAction, useAsync } from '../ui'
import { hasRole } from './common'
import { Conflicts } from './Conflicts'

type Excuse = { id: number; kind: string; exam_id?: number; slot_id?: number; reason: string; status: 'pending' | 'approved' | 'rejected'; decision_note?: string; created_at: string; person?: string; target?: string }
const ST = { pending: ['warn', 'Bekliyor'], approved: ['ok', 'Onaylandı'], rejected: ['err', 'Reddedildi'] } as const
const KIND_TR: Record<string, string> = { student: 'Öğrenci', invigilator: 'Gözetmen' }

export function Excuses() {
  const { user } = useAuth()
  return hasRole(user!.roles, 'student_affairs') || hasRole(user!.roles, 'secretary') ? <Desk /> : <Mine student={hasRole(user!.roles, 'student')} />
}

/** Menü tek öğedir: karar yetkilisi için Mazeretler + Çakışmalar sekmeleri; diğer roller yalnız mazeret ekranını görür. */
export function ExcuseDesk({ initialTab = 'mazeret' }: { initialTab?: 'mazeret' | 'cakisma' }) {
  const { user } = useAuth()
  const [tab, setTab] = useState(initialTab)
  if (!(hasRole(user!.roles, 'student_affairs') || hasRole(user!.roles, 'secretary'))) return <Excuses />
  return (
    <>
      <div className="noprint" style={{ marginBottom: 12 }}><Tabs value={tab} onChange={setTab} items={[['mazeret', 'Mazeretler'], ['cakisma', 'Çakışmalar']]} /></div>
      {tab === 'mazeret' ? <Excuses /> : <Conflicts />}
    </>
  )
}

function Desk() {
  const [status, setStatus] = useState<'pending' | 'approved' | 'rejected'>('pending')
  const list = useAsync(() => all<Excuse>(`/excuses?status=${status}`), [status])
  const counts = useAsync(async () => ({ pending: (await all<Excuse>('/excuses?status=pending')).length, approved: (await all<Excuse>('/excuses?status=approved')).length, rejected: (await all<Excuse>('/excuses?status=rejected')).length }))
  const [note, setNote] = useState<Record<number, string>>({})
  const decide = useAction(async (id: number, decision: 'approved' | 'rejected') => {
    await post(`/excuses/${id}/decision`, { decision, note: note[id] ?? '' }, { 'Idempotency-Key': idemKey(`decide-${id}`) })
    list.reload()
    counts.reload()
  })
  return (
    <>
      <div className="head"><div><div className="caps">Karar yetkilisi</div><h1>Mazeret masası</h1></div><Tabs value={status} onChange={setStatus} items={[['pending', 'Bekleyen'], ['approved', 'Onaylanan'], ['rejected', 'Reddedilen']]} /></div>
      <div className="grid g4">
        <StatCard icon="clock" tone="warn" label="Bekleyen" value={counts.data?.pending ?? '—'} note="karar bekliyor" />
        <StatCard icon="checkCircle" tone="ok" label="Onaylanan" value={counts.data?.approved ?? '—'} note="sınav kartı verilmez / görev dışı" />
        <StatCard icon="x" tone="err" label="Reddedilen" value={counts.data?.rejected ?? '—'} />
      </div>
      {decide.error && <ErrorBox error={decide.error} />}
      <Card title="Mazeretler">
        <Async s={list} empty={(d) => d.length === 0}>
          {(d) => (
            <div className="tablewrap"><table>
              <thead><tr><th>Tür</th><th>Kim</th><th>Hedef</th><th>Gerekçe</th><th>Tarih</th><th>{status === 'pending' ? 'Karar' : 'Not'}</th></tr></thead>
              <tbody>{d.map((e) => (
                <tr key={e.id}>
                  <td><Status tone={e.kind === 'student' ? 'info' : 'warn'}>{KIND_TR[e.kind] ?? e.kind}</Status></td>
                  <td>{e.person || '—'}</td>
                  <td>{e.target || (e.exam_id ? `Sınav #${e.exam_id}` : `Slot #${e.slot_id}`)}</td>
                  <td>{e.reason}</td><td>{fmtDateTime(e.created_at)}</td>
                  <td>{status === 'pending' ? (
                    <div className="row" style={{ flexWrap: 'nowrap' }}>
                      <input className="input" style={{ width: 160 }} placeholder="Karar notu" aria-label="Karar notu" value={note[e.id] ?? ''} onChange={(ev) => setNote({ ...note, [e.id]: ev.target.value })} />
                      <button className="btn sm primary" disabled={decide.busy} onClick={() => void decide.run(e.id, 'approved')}>Onayla</button>
                      <button className="btn sm danger" disabled={decide.busy} onClick={() => void decide.run(e.id, 'rejected')}>Reddet</button>
                    </div>) : (e.decision_note || '—')}</td>
                </tr>))}</tbody>
            </table></div>
          )}
        </Async>
      </Card>
    </>
  )
}

function Mine({ student }: { student: boolean }) {
  const mine = useAsync(() => all<Excuse>('/me/excuses'))
  const targets = useAsync(async () => {
    if (student) return ((await get('/me/exams')).items as { exam_id: number; course_code: string; course_name: string }[]).map((x) => ({ id: x.exam_id, label: `${x.course_code} — ${x.course_name}` }))
    return ((await get('/me/duties')).items as { slot_id: number; starts_at: string; room_code: string }[]).map((x) => ({ id: x.slot_id, label: `${fmtDateTime(x.starts_at)} · ${x.room_code}` }))
  }, [student])
  const [target, setTarget] = useState(0)
  const [reason, setReason] = useState('')
  const create = useAction(async () => {
    await post('/excuses', student ? { exam_id: target, reason } : { slot_id: target, reason })
    setReason('')
    mine.reload()
  })
  return (
    <>
      <div className="head"><div><div className="caps">Mazeret</div><h1>Mazeret bildir</h1></div></div>
      <Card title="Yeni mazeret">
        <div className="row" style={{ alignItems: 'flex-end' }}>
          <Field label={student ? 'Sınav' : 'Görev'} id="t"><select id="t" value={target} onChange={(e) => setTarget(Number(e.target.value))}><option value={0}>Seçin…</option>{targets.data?.map((t) => <option key={t.id} value={t.id}>{t.label}</option>)}</select></Field>
          <Field label="Gerekçe" id="r"><input id="r" className="input" value={reason} onChange={(e) => setReason(e.target.value)} style={{ minWidth: 260 }} /></Field>
          <button className="btn primary" disabled={create.busy || !target || !reason.trim()} onClick={() => void create.run()}>{create.busy ? 'Gönderiliyor…' : 'Gönder'}</button>
        </div>
        {create.error && <div style={{ marginTop: 12 }}>{create.error.status === 409 ? <div className="alert warn"><div>Bu hedef için zaten bir mazeret var.</div></div> : <ErrorBox error={create.error} />}</div>}
      </Card>
      <Card title="Mazeretlerim">
        <Async s={mine} empty={(d) => d.length === 0}>
          {(d) => d.length === 0 ? <Empty title="Mazeret yok" /> : <div className="tablewrap"><table><thead><tr><th>Hedef</th><th>Gerekçe</th><th>Durum</th><th>Not</th></tr></thead><tbody>{d.map((e) => <tr key={e.id}><td>{e.target || (e.exam_id ? `Sınav #${e.exam_id}` : `Slot #${e.slot_id}`)}</td><td>{e.reason}</td><td><Status tone={ST[e.status][0]}>{ST[e.status][1]}</Status></td><td>{e.decision_note ?? '—'}</td></tr>)}</tbody></table></div>}
        </Async>
        {!mine.data?.length && !mine.loading && <Empty title="Henüz mazeret bildirmediniz" />}
      </Card>
    </>
  )
}
