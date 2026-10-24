import { useEffect, useMemo, useState } from 'react'
import { all, fmtDateTime, get, idemKey, post } from '../api'
import { Async, Card, Empty, ErrorBox, Field, Pill, useAction, useAsync } from '../ui'

type Duty = { id: number; slot_id: number; room_id: number; room_code: string; building: string; starts_at: string; exam_id: number; course_code: string }
type Seat = { student_id: number; student_no: string; full_name: string; room_id: number }
type Ev = { event_id: string; device_id: string; exam_id: number; student_id: number; status: 'present' | 'absent'; source: 'manual'; hlc: { wall: number; counter: number } }

const QKEY = 'sinavplan.queue'
const DKEY = 'sinavplan.device'
const load = <T,>(k: string, d: T): T => { try { return JSON.parse(localStorage.getItem(k) ?? '') as T } catch { return d } }
const store = (k: string, v: unknown) => { try { localStorage.setItem(k, JSON.stringify(v)) } catch { /* depolama kapalı */ } }

// UUIDv7: 48 bit zaman + rastgele; olay kimliği istemcide üretilir (idempotent senkron için).
function uuidv7() {
  const b = crypto.getRandomValues(new Uint8Array(16))
  const ms = BigInt(Date.now())
  for (let i = 0; i < 6; i++) b[i] = Number((ms >> BigInt(8 * (5 - i))) & 0xffn)
  b[6] = (b[6] & 0x0f) | 0x70
  b[8] = (b[8] & 0x3f) | 0x80
  const h = [...b].map((x) => x.toString(16).padStart(2, '0')).join('')
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`
}

export function Tablet() {
  const duties = useAsync(() => all<Duty>('/me/duties'))
  const [duty, setDuty] = useState<Duty | null>(null)
  const [online, setOnline] = useState(navigator.onLine)
  const [queue, setQueue] = useState<Ev[]>(() => load(QKEY, []))
  const [device] = useState(() => load<string>(DKEY, '') || `tablet-${Math.random().toString(36).slice(2, 7)}`)
  const [counter, setCounter] = useState(0)
  useEffect(() => { store(DKEY, device) }, [device])
  useEffect(() => { store(QKEY, queue) }, [queue])
  useEffect(() => {
    const on = () => setOnline(true), off = () => setOnline(false)
    addEventListener('online', on); addEventListener('offline', off)
    return () => { removeEventListener('online', on); removeEventListener('offline', off) }
  }, [])
  const roster = useAsync(async () => {
    if (!duty) return null
    const sched = (await get('/schedules?limit=100')).items.find((s: { status: string }) => s.status === 'published')
    if (!sched) return []
    return (await all<Seat>(`/schedules/${sched.id}/seating?exam_id=${duty.exam_id}&room_id=${duty.room_id}`)).sort((a, b) => a.student_no.localeCompare(b.student_no))
  }, [duty?.id])
  const marks = useMemo(() => new Map(queue.filter((e) => e.exam_id === duty?.exam_id).map((e) => [e.student_id, e.status])), [queue, duty])
  const mark = (s: Seat, status: 'present' | 'absent') => {
    setCounter((c) => c + 1)
    setQueue((q) => [...q, { event_id: uuidv7(), device_id: device, exam_id: duty!.exam_id, student_id: s.student_id, status, source: 'manual', hlc: { wall: Date.now(), counter } }])
  }
  const sync = useAction(async () => {
    const r = await post<{ accepted: number; duplicates: number; rejected: unknown[] }>('/attendance/sync', { events: queue.slice(0, 1000) })
    setQueue((q) => q.slice(1000))
    return r
  })

  const [token, setToken] = useState('')
  const scan = useAction(async () => {
    const r = await post<{ student_id: number; exam_id: number }>('/attendance/qr', { token: token.trim(), device_id: device }, { 'Idempotency-Key': idemKey('scan') })
    setToken('')
    return r
  })

  return (
    <>
      <div className="head">
        <div><div className="caps">Gözetmen tableti</div><h1>Yoklama ve görevlerim</h1></div>
        <div className="actions"><Pill kind={online ? 'ok' : 'err'}>{online ? 'Çevrimiçi' : 'Çevrimdışı'}</Pill><Pill kind={queue.length ? 'warn' : ''}>{queue.length} senkron bekliyor</Pill></div>
      </div>
      <Card title="Görevlerim">
        <Async s={duties} empty={(d) => d.length === 0}>
          {(list) => list.length === 0 ? <Empty title="Atanmış göreviniz yok" /> : (
            <div className="tablewrap"><table>
              <thead><tr><th>Tarih / saat</th><th>Salon</th><th>Ders</th><th /></tr></thead>
              <tbody>{list.map((d) => <tr key={d.id}><td>{fmtDateTime(d.starts_at)}</td><td>{d.room_code} <span className="muted small">{d.building}</span></td><td>{d.course_code}</td><td><button className="btn sm" aria-pressed={duty?.id === d.id} onClick={() => setDuty(d)}>{duty?.id === d.id ? 'Seçili' : 'Yoklama al'}</button></td></tr>)}</tbody>
            </table></div>
          )}
        </Async>
      </Card>
      {duty && (
        <>
          <Card title="Sınav kartı okut (QR)" aside={<span className="small muted">cihaz: <span className="mono">{device}</span></span>}>
            <div className="row" style={{ alignItems: 'flex-end' }}>
              <Field label="Kart jetonu" id="tok"><input id="tok" className="input mono" style={{ minWidth: 280 }} value={token} onChange={(e) => setToken(e.target.value)} placeholder="Okutucu buraya yazar veya yapıştırın" autoComplete="off" /></Field>
              <button className="btn primary" disabled={scan.busy || !token.trim() || !online} onClick={() => void scan.run()}>{scan.busy ? 'Okunuyor…' : 'Okut'}</button>
            </div>
            {!online && <p className="small muted" style={{ marginTop: 8 }}>QR okutma çevrimiçi gerektirir (tek kullanımlık doğrulama sunucuda yapılır). Çevrimdışıyken aşağıdan elle işaretleyin.</p>}
            {scan.result && <div className="alert ok" style={{ marginTop: 12 }} role="status">Öğrenci #{scan.result.student_id} geldi olarak kaydedildi.</div>}
            {scan.error && <div style={{ marginTop: 12 }}>{scan.error.code === 'qr_already_used' ? <div className="alert err" role="alert"><div><b>Bu kart daha önce okutuldu.</b> Öğrenciden yeni kart almasını isteyin.</div></div> : <ErrorBox error={scan.error} />}</div>}
          </Card>
          <Card title={`${duty.course_code} · ${duty.room_code} yoklama listesi`} aside={<button className="btn primary" disabled={!online || !queue.length || sync.busy} onClick={() => void sync.run()}>{sync.busy ? 'Senkronlanıyor…' : `Senkronla (${queue.length})`}</button>}>
            {sync.result && <div className="alert ok" role="status" style={{ marginBottom: 12 }}>{sync.result.accepted} kabul edildi, {sync.result.duplicates} tekrar yok sayıldı, {sync.result.rejected.length} reddedildi.</div>}
            {sync.error && <div style={{ marginBottom: 12 }}><ErrorBox error={sync.error} /></div>}
            <Async s={roster} empty={(d) => d.length === 0}>
              {(rows) => (
                <div className="tablewrap" style={{ maxHeight: 480, overflowY: 'auto' }}><table>
                  <thead><tr><th>No</th><th>Ad soyad</th><th>Durum (bu cihaz)</th><th /></tr></thead>
                  <tbody>{rows.map((s) => <tr key={s.student_id}><td>{s.student_no}</td><td>{s.full_name}</td><td>{marks.has(s.student_id) ? <Pill kind={marks.get(s.student_id) === 'present' ? 'ok' : 'err'}>{marks.get(s.student_id) === 'present' ? 'Geldi' : 'Gelmedi'}</Pill> : '—'}</td><td><div className="row"><button className="btn sm" onClick={() => mark(s, 'present')}>Geldi</button><button className="btn sm" onClick={() => mark(s, 'absent')}>Gelmedi</button></div></td></tr>)}</tbody>
                </table></div>
              )}
            </Async>
          </Card>
        </>
      )}
    </>
  )
}
