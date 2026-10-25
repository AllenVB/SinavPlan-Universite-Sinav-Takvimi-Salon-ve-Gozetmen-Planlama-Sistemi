import jsQR from 'jsqr'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { all, download, fmtDateTime, fmtTime, get, idemKey, post } from '../api'
import { Icon } from '../icons'
import { LiveBadge, useLive, useNow } from '../live'
import { CheckinPanel } from './CheckinPanel'
import { Async, Banner, Card, Empty, ErrorBox, Field, Pill, Ring, StatCard, Status, Tabs, useAction, useAsync } from '../ui'

type Duty = { id: number; slot_id: number; room_id: number; room_code: string; building: string; starts_at: string; exam_id: number; course_code: string }
type Seat = { student_id: number; student_no: string; full_name: string; room_id: number; seat_row: number; seat_col: number; exam_id: number; disabled_student: boolean }
type Ev = { event_id: string; device_id: string; exam_id: number; student_id: number; status: 'present' | 'absent'; source: 'manual'; hlc: { wall: number; counter: number } }
type Att = { student_id: number; status: 'present' | 'absent'; source: string }
type Cal = { exam_id: number; ends_at: string }
type Room = { id: number; code: string; building: string; type: string; capacity: number }

const QKEY = 'sinavplan.queue'
const DKEY = 'sinavplan.device'
const load = <T,>(k: string, d: T): T => { try { return JSON.parse(localStorage.getItem(k) ?? '') as T } catch { return d } }
const store = (k: string, v: unknown) => { try { localStorage.setItem(k, JSON.stringify(v)) } catch { /* depolama kapalı */ } }
const seatName = (r: number, c: number) => `${String.fromCharCode(65 + (r % 26))}-${c + 1}`

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
  const now = useNow()
  const duties = useAsync(async () => (await all<Duty>('/me/duties')).sort((a, b) => a.starts_at.localeCompare(b.starts_at)))
  const [duty, setDuty] = useState<Duty | null>(null)
  const [online, setOnline] = useState(navigator.onLine)
  const [queue, setQueue] = useState<Ev[]>(() => load(QKEY, []))
  const [device] = useState(() => load<string>(DKEY, '') || `tablet-${Math.random().toString(36).slice(2, 7)}`)
  const counter = useRef(0)
  useEffect(() => { store(DKEY, device) }, [device])
  useEffect(() => { store(QKEY, queue) }, [queue])
  useEffect(() => {
    const on = () => setOnline(true), off = () => setOnline(false)
    addEventListener('online', on); addEventListener('offline', off)
    return () => { removeEventListener('online', on); removeEventListener('offline', off) }
  }, [])

  const sched = useAsync(async () => ((await get('/schedules?limit=100')).items as { id: number; status: string }[]).find((s) => s.status === 'published')?.id ?? null)
  const cal = useAsync(async () => (sched.data ? all<Cal>(`/schedules/${sched.data}/exams`) : []), [sched.data])
  const endOf = (examId: number) => { const e = cal.data?.find((x) => x.exam_id === examId); return e ? new Date(e.ends_at).getTime() : null }

  const roster = useAsync(async () => {
    if (!duty || !sched.data) return null
    return (await all<Seat>(`/schedules/${sched.data}/seating?exam_id=${duty.exam_id}&room_id=${duty.room_id}`)).sort((a, b) => a.seat_row - b.seat_row || a.seat_col - b.seat_col)
  }, [duty?.id, sched.data])
  // sunucudaki yoklama durumu (5 sn'de bir) — yerel kuyruk üstüne biner
  const att = useLive(async () => (duty ? all<Att>(`/exams/${duty.exam_id}/attendance`) : []), [duty?.id], 5000, !!duty)

  const local = useMemo(() => new Map(queue.filter((e) => e.exam_id === duty?.exam_id).map((e) => [e.student_id, e.status])), [queue, duty])
  const serverMap = useMemo(() => new Map((att.data ?? []).map((a) => [a.student_id, a])), [att.data])
  const statusOf = (id: number): 'present' | 'absent' | null => local.get(id) ?? serverMap.get(id)?.status ?? null

  const mark = (s: Seat, status: 'present' | 'absent') =>
    setQueue((q) => [...q, { event_id: uuidv7(), device_id: device, exam_id: duty!.exam_id, student_id: s.student_id, status, source: 'manual', hlc: { wall: Date.now(), counter: counter.current++ } }])

  const sync = useAction(async () => {
    const batch = queue.slice(0, 1000)
    const r = await post<{ accepted: number; duplicates: number; rejected: unknown[]; new_conflicts: number }>('/attendance/sync', { events: batch })
    setQueue((q) => q.slice(batch.length))
    void att.reload()
    return r
  })
  // ağ geri gelince kuyruk kendiliğinden eşitlenir
  const wasOffline = useRef(false)
  useEffect(() => {
    if (!online) wasOffline.current = true
    else if (wasOffline.current && queue.length && !sync.busy) { wasOffline.current = false; void sync.run() }
    else if (online) wasOffline.current = false
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [online])

  const [filter, setFilter] = useState<'all' | 'present' | 'missing' | 'none'>('all')
  const [q, setQ] = useState('')
  const [page, setPage] = useState(0)
  const rows = roster.data ?? []
  const counts = { present: rows.filter((r) => statusOf(r.student_id) === 'present').length, absent: rows.filter((r) => statusOf(r.student_id) === 'absent').length }
  const none = rows.length - counts.present - counts.absent
  const shown = rows.filter((r) => {
    const st = statusOf(r.student_id)
    return (filter === 'all' || (filter === 'present' && st === 'present') || (filter === 'missing' && st === 'absent') || (filter === 'none' && st === null)) && (!q || `${r.full_name} ${r.student_no} ${seatName(r.seat_row, r.seat_col)}`.toLowerCase().includes(q.toLowerCase()))
  })
  const pages = Math.ceil(shown.length / 15)
  const end = duty ? endOf(duty.exam_id) : null
  const startMs = duty ? new Date(duty.starts_at).getTime() : 0
  const phase = !duty ? null : now < startMs ? 'upcoming' : end && now > end ? 'past' : 'active'
  const total = end && duty ? end - startMs : 0

  const scan = useAction(async (token: string) => post<{ student_id: number; exam_id: number }>('/attendance/qr', { token: token.trim(), device_id: device }, { 'Idempotency-Key': idemKey('scan') }).then((r) => { void att.reload(); return r }))
  const dutyOn = (d: Duty) => setDuty(d)

  return (
    <>
      <div className="head">
        <div><div className="caps">Gözetmen tableti</div><h1>Yoklama ve görevlerim</h1></div>
        <div className="actions">
          <span className="pill"><Icon name="clock" size={14} /> <span className="mono">{new Date(now).toLocaleTimeString('tr-TR', { timeZone: 'Europe/Istanbul' })}</span></span>
          <Status tone={online ? 'ok' : 'err'}>{online ? 'Çevrimiçi' : 'Çevrimdışı'}</Status>
          <Pill kind={queue.length ? 'warn' : ''}>{queue.length} senkron bekliyor</Pill>
          <button className="btn sm" onClick={() => void download('/me/duties.ics', 'gorevlerim.ics').catch(() => undefined)}><Icon name="download" size={14} /> Görevlerim (.ics)</button>
        </div>
      </div>

      {!online && <Banner tone="warn" icon="wifiOff" title={`Çevrimdışı mod — ${queue.length} yoklama kaydı bu cihazda saklanıyor`} actions={<button className="btn sm" disabled>Ağ bekleniyor…</button>}>Kayıtlar tarayıcı belleğinde tutulur; ağ gelince otomatik eşitlenir. QR okutma çevrimiçi gerektirir.</Banner>}
      {online && queue.length > 0 && <Banner tone="info" icon="refresh" title={`${queue.length} kayıt eşitlenmeyi bekliyor`} actions={<button className="btn sm primary" disabled={sync.busy} onClick={() => void sync.run()}>{sync.busy ? 'Eşitleniyor…' : 'Şimdi eşitle'}</button>}>Aynı olay tekrar gönderilse bile sunucu tekilleştirir.</Banner>}
      {sync.result && sync.result.new_conflicts > 0 && <Banner tone="warn" icon="alert" title={`Senkronizasyon uyuşmazlığı: ${sync.result.new_conflicts} kayıt`}>Aynı öğrenci için farklı cihazlar çelişen durum bildirdi; QR okutma üstün sayılır, aksi halde daha yeni saat damgası. <a href="#/cakisma">Çakışmaları incele</a></Banner>}
      {sync.result && sync.result.new_conflicts === 0 && <Banner tone="ok" icon="checkCircle" title={`${sync.result.accepted} kayıt eşitlendi`}>{sync.result.duplicates} tekrar yok sayıldı, {sync.result.rejected.length} reddedildi.</Banner>}
      {sync.error && <ErrorBox error={sync.error} retry={() => void sync.run()} />}

      <Card title="Görevlerim">
        <Async s={duties} empty={(d) => d.length === 0}>
          {(list) => list.length === 0 ? <Empty title="Atanmış göreviniz yok" /> : (
            <div className="tablewrap"><table>
              <thead><tr><th>Tarih / saat</th><th>Salon</th><th>Ders</th><th /></tr></thead>
              <tbody>{list.map((d) => <tr key={d.id} className={duty?.id === d.id ? 'tone-ok' : ''}><td>{fmtDateTime(d.starts_at)}</td><td>{d.room_code} <span className="muted small">{d.building}</span></td><td>{d.course_code}</td><td><button className="btn sm" aria-pressed={duty?.id === d.id} onClick={() => dutyOn(d)}>{duty?.id === d.id ? 'Seçili' : 'Yoklama al'}</button></td></tr>)}</tbody>
            </table></div>
          )}
        </Async>
      </Card>

      {duty && (
        <>
          <div className="card livecard row between" style={{ flexWrap: 'wrap' }}>
            <div>
              <div className="caps">Aktif salon</div>
              <h2>{duty.room_code} · {duty.course_code}</h2>
              <div className="small muted">{duty.building} · {fmtDateTime(duty.starts_at)}{end ? `–${fmtTime(new Date(end).toISOString())}` : ''}</div>
            </div>
            <div className="row">
              <Status tone={phase === 'active' ? 'ok' : phase === 'upcoming' ? 'info' : 'mute'}>{phase === 'active' ? 'Sınav sürüyor' : phase === 'upcoming' ? 'Başlamadı' : 'Bitti'}</Status>
              {phase === 'active' && end && <Ring fraction={total ? (end - now) / total : 0} size={64}>{Math.max(0, Math.ceil((end - now) / 60000))}</Ring>}
              {phase === 'active' && <span className="small muted">dk kaldı</span>}
              <LiveBadge updatedAt={att.updatedAt} error={att.error} />
            </div>
          </div>

          <div className="grid g4">
            <StatCard icon="users" tone="info" label="Kayıtlı aday" value={rows.length} note="bu salonda" />
            <StatCard icon="checkCircle" tone="ok" label="Salonda (geldi)" value={counts.present} progress={rows.length ? (counts.present / rows.length) * 100 : 0} note={`%${rows.length ? Math.round((counts.present / rows.length) * 100) : 0} katılım`} />
            <StatCard icon="alert" tone={counts.absent ? 'err' : 'mute'} label="Gelmedi" value={counts.absent} progress={rows.length ? (counts.absent / rows.length) * 100 : 0} />
            <StatCard icon="clock" tone="mute" label="İşaretlenmemiş" value={none} progress={rows.length ? (none / rows.length) * 100 : 0} />
          </div>

          <CheckinPanel duty={duty} onAttendance={() => void att.reload()} />

          <Scanner onToken={(t) => void scan.run(t)} busy={scan.busy} online={online} />
          {scan.result && <Banner tone="ok" icon="checkCircle" title={`Öğrenci #${scan.result.student_id} geldi olarak kaydedildi`}>Kart tek kullanımlıktı ve artık geçersiz.</Banner>}
          {scan.error && (scan.error.code === 'qr_already_used' ? <Banner tone="err" icon="alert" title="Bu kart daha önce okutuldu">Öğrenciden yeni kart almasını isteyin.</Banner> : scan.error.code === 'qr_expired' ? <Banner tone="warn" icon="clock" title="Kartın süresi dolmuş veya yenilenmiş">Öğrencinin ekranındaki güncel kodu okutun (kod kısa sürede yenilenir).</Banner> : <ErrorBox error={scan.error} />)}

          <Card title="Yoklama listesi" aside={<div className="row"><button className="btn primary sm" disabled={!online || !queue.length || sync.busy} onClick={() => void sync.run()}>{sync.busy ? 'Eşitleniyor…' : `Senkronla (${queue.length})`}</button></div>}>
            <div className="row" style={{ marginBottom: 12 }}>
              <Tabs value={filter} onChange={(v) => (setFilter(v), setPage(0))} items={[['all', `Tümü (${rows.length})`], ['present', `Gelenler (${counts.present})`], ['missing', `Gelmedi (${counts.absent})`], ['none', `İşaretsiz (${none})`]]} />
              <input className="input" style={{ maxWidth: 240 }} placeholder="Ad, no veya koltuk ara…" aria-label="Ara" value={q} onChange={(e) => (setQ(e.target.value), setPage(0))} />
            </div>
            <Async s={roster} empty={(d) => d.length === 0}>
              {() => shown.length === 0 ? <Empty title="Eşleşen aday yok" /> : (
                <>
                  <div className="tablewrap"><table>
                    <thead><tr><th>Koltuk</th><th>Aday</th><th>Durum</th><th style={{ textAlign: 'right' }}>Yoklama</th></tr></thead>
                    <tbody>{shown.slice(page * 15, page * 15 + 15).map((s) => {
                      const st = statusOf(s.student_id)
                      const queued = local.has(s.student_id)
                      return (
                        <tr key={s.student_id} className={st === 'absent' ? 'tone-err' : st === 'present' ? 'tone-ok' : ''}>
                          <td><span className={`seatbadge ${st === 'absent' ? 'err' : st === 'present' ? 'ok' : ''}`}>{seatName(s.seat_row, s.seat_col)}</span></td>
                          <td><b>{s.full_name}</b>{s.disabled_student && <Pill kind="info">Erişilebilir</Pill>}<div className="small muted">{s.student_no}</div></td>
                          <td>{st ? <Status tone={st === 'present' ? 'ok' : 'err'}>{st === 'present' ? 'Geldi' : 'Gelmedi'}</Status> : <span className="muted">—</span>}{queued && <span className="small muted"> · eşitlenmedi</span>}</td>
                          <td><div className="row" style={{ justifyContent: 'flex-end' }}><button className={`btn sm ${st === 'present' ? 'ok' : ''}`} onClick={() => mark(s, 'present')}>GELDİ</button><button className={`btn sm ${st === 'absent' ? 'err' : ''}`} onClick={() => mark(s, 'absent')}>GELMEDİ</button></div></td>
                        </tr>
                      )
                    })}</tbody>
                  </table></div>
                  <div className="row between" style={{ marginTop: 12 }}><span className="small muted">{shown.length} aday · sayfa {page + 1}/{Math.max(1, pages)}</span><div className="row"><button className="btn sm" disabled={page === 0} onClick={() => setPage(page - 1)}>Önceki</button><button className="btn sm" disabled={page + 1 >= pages} onClick={() => setPage(page + 1)}>Sonraki</button></div></div>
                </>
              )}
            </Async>
          </Card>

          <Evacuation scheduleId={sched.data} duty={duty} seated={rows.length} onDone={() => { void roster.reload(); void att.reload() }} />
        </>
      )}
    </>
  )
}

/** Kamera ile QR tarama (jsQR); kamera yoksa/engelliyse elle kod girişi. */
function Scanner({ onToken, busy, online }: { onToken: (t: string) => void; busy: boolean; online: boolean }) {
  const [on, setOn] = useState(false)
  const [camErr, setCamErr] = useState<string | null>(null)
  const [token, setToken] = useState('')
  const video = useRef<HTMLVideoElement>(null)
  const canvas = useRef<HTMLCanvasElement>(null)
  const last = useRef({ t: '', at: 0 })

  const stop = useCallback(() => { const v = video.current; (v?.srcObject as MediaStream | null)?.getTracks().forEach((t) => t.stop()); if (v) v.srcObject = null }, [])
  useEffect(() => {
    if (!on) return
    let raf = 0, stopped = false
    ;(async () => {
      try {
        const stream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: 'environment' }, audio: false })
        if (stopped) return stream.getTracks().forEach((t) => t.stop())
        const v = video.current!
        v.srcObject = stream
        await v.play()
        const tick = () => {
          if (stopped) return
          const c = canvas.current!, ctx = c.getContext('2d', { willReadFrequently: true })!
          if (v.readyState >= 2 && v.videoWidth) {
            c.width = v.videoWidth; c.height = v.videoHeight
            ctx.drawImage(v, 0, 0)
            const img = ctx.getImageData(0, 0, c.width, c.height)
            const code = jsQR(img.data, img.width, img.height, { inversionAttempts: 'dontInvert' })
            if (code?.data && (code.data !== last.current.t || Date.now() - last.current.at > 4000)) { last.current = { t: code.data, at: Date.now() }; onToken(code.data) }
          }
          raf = window.setTimeout(tick, 120)
        }
        tick()
      } catch (e) {
        setCamErr((e as Error).name === 'NotAllowedError' ? 'Kamera izni reddedildi. Tarayıcı ayarlarından izin verin veya kodu elle girin.' : 'Kamera açılamadı. Kodu elle girebilirsiniz.')
        setOn(false)
      }
    })()
    return () => { stopped = true; clearTimeout(raf); stop() }
  }, [on, onToken, stop])

  return (
    <Card title="Öğrenci kartı tara (eski yöntem: kart QR'ı)" aside={<button className={`btn ${on ? 'danger' : 'primary'}`} disabled={!online && !on} onClick={() => (setCamErr(null), setOn(!on))}><Icon name="camera" size={16} /> {on ? 'Kamerayı kapat' : 'Kamerayı aç'}</button>}>
      {camErr && <Banner tone="err" icon="camera" title="Kamera kullanılamıyor">{camErr}</Banner>}
      <div style={{ display: on ? 'block' : 'none', maxWidth: 420, margin: '0 auto 12px', position: 'relative' }}>
        <video ref={video} playsInline muted style={{ width: '100%', borderRadius: 12, background: '#000' }} data-testid="scanner-video" />
        <div style={{ position: 'absolute', inset: '15%', border: '3px solid rgba(255,255,255,.85)', borderRadius: 12, pointerEvents: 'none' }} />
        <canvas ref={canvas} hidden />
        <p className="small muted" style={{ textAlign: 'center' }}>QR kodu çerçeveye hizalayın; kod okununca otomatik gönderilir.</p>
      </div>
      <div className="row" style={{ alignItems: 'flex-end' }}>
        <Field label="Elle kod" id="tok"><input id="tok" className="input mono" style={{ minWidth: 260 }} value={token} onChange={(e) => setToken(e.target.value)} placeholder="Okutucu buraya yazar veya yapıştırın" autoComplete="off" /></Field>
        <button className="btn primary" disabled={busy || !token.trim() || !online} onClick={() => { onToken(token); setToken('') }}>{busy ? 'Okunuyor…' : 'Okut'}</button>
      </div>
      {!online && <p className="small muted" style={{ marginTop: 8 }}>QR okutma çevrimiçi gerektirir (tek kullanımlık doğrulama sunucuda yapılır). Çevrimdışıyken listeden elle işaretleyin.</p>}
    </Card>
  )
}

/** Salon tahliye protokolü: neden seç, uygun boş salon seç, onayla → tek işlemde taşı. */
function Evacuation({ scheduleId, duty, seated, onDone }: { scheduleId: number | null; duty: Duty; seated: number; onDone: () => void }) {
  const [open, setOpen] = useState(false)
  const [reason, setReason] = useState('Klima / elektrik arızası')
  const [target, setTarget] = useState(0)
  const [ok, setOk] = useState(false)
  const rooms = useAsync(async () => {
    if (!open || !scheduleId) return null
    const [allRooms, used] = await Promise.all([all<Room>('/rooms'), get<{ items: { room_id: number }[] }>(`/schedules/${scheduleId}/rooms?slot_id=${duty.slot_id}`)])
    const busy = new Set(used.items.map((x) => x.room_id))
    const cur = allRooms.find((r) => r.id === duty.room_id)
    return allRooms.filter((r) => !busy.has(r.id) && r.type === (cur?.type ?? 'classic') && r.capacity >= seated * 2).sort((a, b) => a.capacity - b.capacity).slice(0, 6)
  }, [open, scheduleId, duty.slot_id, seated])
  const act = useAction(async () => {
    const r = await post<{ moved: number }>(`/schedules/${scheduleId}/room-change`, { slot_id: duty.slot_id, from_room_id: duty.room_id, to_room_id: target, reason }, { 'Idempotency-Key': idemKey('evac') })
    onDone()
    return r
  })
  return (
    <Card title="Salon tahliye ve transfer protokolü" aside={<button className="btn danger" onClick={() => setOpen(!open)}><Icon name="alert" size={16} /> {open ? 'Vazgeç' : 'Tahliye başlat'}</button>}>
      {!open ? <p className="small muted">Beklenmeyen teknik arıza, su baskını veya güvenlik sebebiyle sınavı başka bir salona taşımak için kullanın. Öğrenciler ve gözetmenler tek işlemde taşınır.</p> : (
        <div className="grid g2">
          <div>
            <div className="caps" style={{ marginBottom: 8 }}>Adım 1 · Taşıma sebebi</div>
            {['Klima / elektrik arızası', 'Fiziksel hasar (su baskını, gürültü)', 'Yangın / alarm'].map((r) => <label key={r} className="row" style={{ marginBottom: 6 }}><input type="radio" name="why" checked={reason === r} onChange={() => setReason(r)} /> {r}</label>)}
            <p className="small muted" style={{ marginTop: 10 }}>Bu salondaki {seated} aday hedef salona yerleştirilir; sığmazsa hiçbir şey değişmez.</p>
          </div>
          <div>
            <div className="caps" style={{ marginBottom: 8 }}>Adım 2 · Hedef salon (boş ve yeterli kapasiteli)</div>
            <Async s={rooms} empty={(d) => !d || d.length === 0}>
              {(list) => (list ?? []).map((r) => <label key={r.id} className="row card" style={{ padding: 10, marginBottom: 6, borderColor: target === r.id ? 'var(--primary)' : undefined }}><input type="radio" name="to" checked={target === r.id} onChange={() => setTarget(r.id)} /><b>{r.code}</b><span className="muted small">{r.building} · kapasite {r.capacity}</span></label>)}
            </Async>
            <label className="row" style={{ margin: '10px 0' }}><input type="checkbox" checked={ok} onChange={(e) => setOk(e.target.checked)} /> Öğrencilerin hedef salona taşınacağını onaylıyorum.</label>
            <button className="btn err" disabled={!target || !ok || act.busy} onClick={() => void act.run()}>{act.busy ? 'Taşınıyor…' : 'Tahliyeyi onayla ve taşı'}</button>
            {act.result && <div style={{ marginTop: 10 }}><Banner tone="ok" icon="checkCircle" title={`${act.result.moved} öğrenci taşındı`}>Salon planı ve gözetmenler güncellendi.</Banner></div>}
            {act.error && <div style={{ marginTop: 10 }}>{act.error.code === 'insufficient_capacity' ? <Banner tone="err" icon="alert" title="Hedef salona sığmıyor">{act.error.message} Hiçbir kayıt değişmedi.</Banner> : <ErrorBox error={act.error} />}</div>}
          </div>
        </div>
      )}
    </Card>
  )
}
