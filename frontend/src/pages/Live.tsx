import { useMemo, useRef, useState } from 'react'
import { fmtDateTime, fmtTime, get } from '../api'
import { LiveBadge, useLive, useNow } from '../live'
import { Async, Banner, Card, Dot, Empty, Pill, SegBar, StatCard, Status, useAsync, type Tone } from '../ui'
import { ScheduleSelect, useSchedules } from './common'

type Room = { room_id: number; room_code: string; building: string; room_type: string; capacity: number; seated: number; present: number; absent: number; unmarked: number; last_event_at: string | null; devices: number; exams: { exam_id: number; course_code: string; course_name: string; seated: number }[]; invigilators: { user_id: number; full_name: string }[] }
type Slot = { id: number; day: number; session: number; starts_at: string; ends_at: string; mode: 'active' | 'upcoming' | 'past' }
type Overview = { slot: Slot; totals: { rooms: number; seated: number; present: number; absent: number; unmarked: number }; rooms: Room[] }
type Ev = { event_id: string; received_at: string; status: 'present' | 'absent'; source: 'qr' | 'manual'; device_id: string; exam_id: number; course_code: string; student_no: string; full_name: string; room_code: string | null }
type Act = { id: number; action: string; entity: string; entity_id: string; actor?: string; detail?: Record<string, unknown>; created_at: string }

const MODE: Record<Slot['mode'], [string, Tone]> = { active: ['Oturum sürüyor', 'ok'], upcoming: ['Sıradaki oturum', 'info'], past: ['Biten oturum', 'mute'] }

export function Live() {
  const sch = useSchedules()
  const [slotId, setSlotId] = useState(0) // 0 = sunucu seçsin (süren / sıradaki / son)
  const slots = useAsync(async () => (sch.current ? ((await get<{ items: { id: number; day: number; session: number; starts_at: string }[] }>(`/periods/${sch.current.period_id}/slots`)).items) : []), [sch.current?.period_id])
  const ov = useLive(() => get<Overview>(`/schedules/${sch.sel}/live${slotId ? `?slot_id=${slotId}` : ''}`), [sch.sel, slotId], 5000, !!sch.sel)
  const slot = ov.data?.slot
  const now = useNow()

  // olay akışı: since ile artımlı çekilir, birikir (en çok 80)
  const events = useRef<Ev[]>([])
  const lastSeen = useRef<string | undefined>()
  const [fresh, setFresh] = useState<Set<string>>(new Set())
  const feed = useLive(async () => {
    if (!sch.sel || !slot) return events.current
    const q = new URLSearchParams({ slot_id: String(slot.id), limit: '40' })
    if (lastSeen.current) q.set('since', lastSeen.current)
    const r = await get<{ items: Ev[] }>(`/schedules/${sch.sel}/live/events?${q}`)
    if (r.items.length) {
      lastSeen.current = r.items[0].received_at
      const known = new Set(events.current.map((e) => e.event_id))
      const add = r.items.filter((e) => !known.has(e.event_id))
      events.current = [...add, ...events.current].slice(0, 80)
      if (add.length && known.size) {
        setFresh(new Set(add.map((e) => e.event_id)))
        setTimeout(() => setFresh(new Set()), 2600)
      }
    }
    return events.current
  }, [sch.sel, slot?.id], 3000, !!sch.sel && !!slot)

  // slot değişince akışı sıfırla
  const lastSlot = useRef(0)
  if (slot && lastSlot.current !== slot.id) { lastSlot.current = slot.id; events.current = []; lastSeen.current = undefined }

  const [q, setQ] = useState('')
  const [building, setBuilding] = useState('')
  const [state, setState] = useState('')
  const rooms = ov.data?.rooms ?? []
  const roomState = (r: Room): [string, Tone, string] => (r.seated === 0 ? ['Boş', 'mute', 'idle'] : r.unmarked === 0 ? ['Yoklama tamam', 'ok', 'done'] : r.present + r.absent === 0 ? ['Başlamadı', 'mute', 'idle'] : ['Yoklama sürüyor', 'info', 'run'])
  const buildings = [...new Set(rooms.map((r) => r.building))].sort()
  const shown = useMemo(() => rooms.filter((r) => (!q || `${r.room_code} ${r.building} ${r.exams.map((e) => e.course_code + e.course_name).join(' ')}`.toLowerCase().includes(q.toLowerCase())) && (!building || r.building === building) && (!state || roomState(r)[2] === state)), [rooms, q, building, state])

  const t = ov.data?.totals
  const pct = t && t.seated ? (t.present / t.seated) * 100 : 0
  const devices = rooms.reduce((a, r) => a + r.devices, 0)
  const silent = slot?.mode === 'active' ? rooms.filter((r) => r.seated > 0 && !r.last_event_at && now - new Date(slot.starts_at).getTime() > 10 * 60_000) : []
  const untilStart = slot?.mode === 'upcoming' ? new Date(slot.starts_at).getTime() - now : 0
  const countdown = (ms: number) => { const s = Math.max(0, Math.floor(ms / 1000)); const d = Math.floor(s / 86400); const h = Math.floor((s % 86400) / 3600); const m = Math.floor((s % 3600) / 60); return `${d ? d + ' g ' : ''}${h} sa ${m} dk ${s % 60} sn` }

  return (
    <>
      <div className="head">
        <div>
          <div className="caps">Sınav günü</div>
          <h1>Canlı operasyon ve izleme masası</h1>
          <p className="muted">Salon bazlı yoklama, cihaz olayları ve denetim kaydı — veriler birkaç saniyede bir kendiliğinden yenilenir.</p>
        </div>
        <div className="actions">
          <LiveBadge updatedAt={ov.updatedAt} error={ov.error} />
          <ScheduleSelect s={sch} />
          <select aria-label="Oturum" value={slotId} onChange={(e) => setSlotId(Number(e.target.value))} style={{ width: 'auto' }}>
            <option value={0}>Otomatik (süren / sıradaki)</option>
            {slots.data?.map((s) => <option key={s.id} value={s.id}>{s.day + 1}. gün · {s.session + 1}. oturum · {fmtDateTime(s.starts_at)}</option>)}
          </select>
        </div>
      </div>

      {ov.error && <Banner tone="err" icon="wifiOff" title="Canlı veri akışı kesildi" actions={<button className="btn sm" onClick={() => void ov.reload()}>Yeniden dene</button>}>{ov.error.message} Otomatik olarak yeniden denenecek; son bilinen veri aşağıda gösteriliyor.</Banner>}
      {slot && <Banner tone={slot.mode === 'active' ? 'ok' : slot.mode === 'upcoming' ? 'info' : 'warn'} icon="clock" title={`${MODE[slot.mode][0]}: ${slot.day + 1}. gün, ${slot.session + 1}. oturum — ${fmtDateTime(slot.starts_at)}–${fmtTime(slot.ends_at)}`}>
        {slot.mode === 'upcoming' ? `Başlamasına ${countdown(untilStart)} var.` : slot.mode === 'active' ? 'Yoklama alınıyor.' : 'Bu oturum bitti; kayıtlar arşiv niteliğindedir.'}
      </Banner>}
      {silent.length > 0 && <Banner tone="warn" icon="alert" title={`${silent.length} salondan hiç yoklama gelmedi`}>Oturum başlayalı 10 dakikadan fazla oldu: {silent.slice(0, 6).map((r) => r.room_code).join(', ')}{silent.length > 6 ? '…' : ''}. Gözetmen tabletleri çevrimdışı olabilir.</Banner>}

      <div className="grid g4">
        <StatCard icon="building" tone="info" label="Aktif salon" value={t?.rooms ?? '—'} note={`${rooms.filter((r) => roomState(r)[2] === 'done').length} salonda yoklama tamam`} />
        <StatCard icon="users" tone={pct >= 80 ? 'ok' : pct > 0 ? 'info' : 'mute'} label="Katılım" value={t ? `${t.present} / ${t.seated}` : '—'} note={`%${pct.toFixed(1)} geldi`} progress={pct} />
        <StatCard icon="alert" tone={t && t.absent ? 'err' : 'ok'} label="Gelmedi" value={t?.absent ?? '—'} note={`${t?.unmarked ?? 0} öğrenci henüz işaretlenmedi`} />
        <StatCard icon="zap" tone="info" label="Aktif cihaz" value={devices} note="yoklama gönderen tablet (salon başına)" />
      </div>

      <div className="grid" style={{ gridTemplateColumns: 'minmax(0, 2fr) minmax(280px, 1fr)', alignItems: 'start' }}>
        <div className="grid" style={{ gap: 16 }}>
          <div className="row noprint">
            <input className="input" style={{ maxWidth: 260 }} placeholder="Salon, bina veya ders ara…" aria-label="Ara" value={q} onChange={(e) => setQ(e.target.value)} />
            <select aria-label="Bina" value={building} onChange={(e) => setBuilding(e.target.value)} style={{ width: 'auto' }}><option value="">Tüm binalar</option>{buildings.map((b) => <option key={b}>{b}</option>)}</select>
            <select aria-label="Durum" value={state} onChange={(e) => setState(e.target.value)} style={{ width: 'auto' }}><option value="">Tüm durumlar</option><option value="run">Yoklama sürüyor</option><option value="done">Yoklama tamam</option><option value="idle">Başlamadı</option></select>
            <span className="small muted">{shown.length} / {rooms.length} salon</span>
          </div>
          <Async s={{ ...ov, reload: ov.reload, setData: () => undefined } as never} empty={(d: Overview) => d.rooms.length === 0}>
            {() => shown.length === 0 ? <Card><Empty title="Eşleşen salon yok" /></Card> : (
              <div className="grid g3">
                {shown.map((r) => {
                  const [label, tone] = roomState(r)
                  const ago = r.last_event_at ? Math.max(0, Math.round((now - new Date(r.last_event_at).getTime()) / 1000)) : null
                  return (
                    <section className="card roomcard" key={r.room_id}>
                      <div className="row between">
                        <div><h2>{r.room_code}</h2><div className="small muted">{r.building} · {r.room_type === 'lab' ? 'Laboratuvar' : 'Derslik'} · kapasite {r.capacity}</div></div>
                        <Status tone={tone}>{label}</Status>
                      </div>
                      <div className="small">{r.exams.map((e) => <div key={e.exam_id}><b>{e.course_code}</b> {e.course_name} <span className="muted">({e.seated})</span></div>)}</div>
                      <SegBar parts={[{ value: r.present, tone: 'ok', label: 'Geldi' }, { value: r.absent, tone: 'err', label: 'Gelmedi' }, { value: r.unmarked, tone: 'mute', label: 'İşaretsiz' }]} />
                      <div className="meta">
                        <div><b style={{ color: 'var(--ok)' }}>{r.present}</b><span className="small muted">Geldi</span></div>
                        <div><b style={{ color: r.absent ? 'var(--error)' : undefined }}>{r.absent}</b><span className="small muted">Gelmedi</span></div>
                        <div><b>{r.unmarked}</b><span className="small muted">İşaretsiz</span></div>
                      </div>
                      <div className="small muted">{r.invigilators.map((i) => i.full_name).join(', ') || 'Gözetmen atanmamış'}</div>
                      <div className="small muted">{ago === null ? 'Henüz olay yok' : `Son olay: ${ago} sn önce`}{r.devices ? ` · ${r.devices} cihaz` : ''}</div>
                    </section>
                  )
                })}
              </div>
            )}
          </Async>
        </div>

        <Card title="Canlı olay akışı" aside={<LiveBadge updatedAt={feed.updatedAt} error={feed.error} />}>
          {feed.data && feed.data.length > 0 ? (
            <ul className="feed" style={{ maxHeight: 520, overflowY: 'auto' }} tabIndex={0} aria-label="Yoklama olay akışı">
              {feed.data.map((e) => {
                const tone: Tone = e.status === 'present' ? 'ok' : 'err'
                const title = e.status === 'present' ? (e.source === 'qr' ? 'QR okutuldu' : 'Elle: geldi') : 'Gelmedi işaretlendi'
                return (
                  <li key={e.event_id} className={fresh.has(e.event_id) ? 'new' : ''}>
                    <Dot tone={tone} />
                    <span><b>{title}</b><div className="small">{e.full_name} <span className="muted">({e.student_no})</span></div><div className="small muted">{e.course_code}{e.room_code ? ` · ${e.room_code}` : ''} · {e.device_id}</div></span>
                    <span className="small muted mono">{new Date(e.received_at).toLocaleTimeString('tr-TR', { timeZone: 'Europe/Istanbul' })}</span>
                  </li>
                )
              })}
            </ul>
          ) : <Empty title="Henüz yoklama olayı yok" hint="Gözetmenler kart okuttukça veya cihaz senkronladıkça burada canlı görünür." />}
        </Card>
      </div>

      <AuditTable />
    </>
  )
}

const ACTION_TR: Record<string, string> = { 'schedule.create': 'Takvim oluşturuldu', 'schedule.publish': 'Takvim yayınlandı', 'excuse.create': 'Mazeret bildirildi', 'excuse.decide': 'Mazeret kararı', 'room.create': 'Salon eklendi', 'room.update': 'Salon güncellendi', 'room.delete': 'Salon silindi', 'room.change': 'Salon tahliyesi', 'invigilator.remove': 'Gözetmen çıkarıldı' }

function AuditTable() {
  const [prefix, setPrefix] = useState('')
  const [older, setOlder] = useState<Act[]>([])
  const [cursor, setCursor] = useState<number | null>(null)
  const first = useLive(async () => {
    const r = await get<{ items: Act[]; next_before: number | null }>(`/activity?limit=15${prefix ? `&action=${encodeURIComponent(prefix)}` : ''}`)
    setCursor((c) => (older.length ? c : r.next_before))
    return r.items
  }, [prefix], 10_000)
  const more = async () => {
    const r = await get<{ items: Act[]; next_before: number | null }>(`/activity?limit=15&before_id=${cursor}${prefix ? `&action=${encodeURIComponent(prefix)}` : ''}`)
    setOlder((o) => [...o, ...r.items])
    setCursor(r.next_before)
  }
  const rows = [...(first.data ?? []), ...older]
  return (
    <Card title="Denetim kaydı" aside={<div className="row"><LiveBadge updatedAt={first.updatedAt} error={first.error} /><select aria-label="Olay türü" value={prefix} onChange={(e) => (setPrefix(e.target.value), setOlder([]))} style={{ width: 'auto' }}><option value="">Tüm olaylar</option><option value="schedule.">Takvim</option><option value="excuse.">Mazeret</option><option value="room.">Salon</option><option value="invigilator.">Gözetmen</option><option value="import.">İçe aktarma</option></select></div>}>
      <Async s={{ ...first, reload: first.reload, setData: () => undefined } as never} empty={(d: Act[]) => d.length === 0}>
        {() => (
          <>
            <div className="tablewrap"><table>
              <thead><tr><th>Zaman</th><th>Aktör</th><th>Olay</th><th>Hedef</th><th>Ayrıntı</th></tr></thead>
              <tbody>{rows.map((a) => (
                <tr key={a.id} className={a.action === 'room.change' || a.action.endsWith('delete') ? 'tone-err' : a.action === 'schedule.publish' ? 'tone-ok' : ''}>
                  <td className="mono small">{fmtDateTime(a.created_at)}</td>
                  <td>{a.actor ?? <span className="muted">sistem / öğrenci</span>}</td>
                  <td><Pill kind={a.action === 'room.change' ? 'err' : a.action === 'schedule.publish' ? 'ok' : 'info'}>{a.action.startsWith('import.') ? `İçe aktarma: ${a.entity_id}` : ACTION_TR[a.action] ?? a.action}</Pill></td>
                  <td className="small">{a.entity} #{a.entity_id}</td>
                  <td className="small muted mono" style={{ maxWidth: 320, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{a.detail ? JSON.stringify(a.detail) : '—'}</td>
                </tr>))}</tbody>
            </table></div>
            {cursor && <div style={{ marginTop: 12 }}><button className="btn" onClick={() => void more()}>Daha eski kayıtlar</button></div>}
          </>
        )}
      </Async>
    </Card>
  )
}
