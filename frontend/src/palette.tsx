import { useEffect, useMemo, useRef, useState } from 'react'
import { fmtDate, fmtTime, get } from './api'
import { Icon } from './icons'
import { SeatMap, type Seat } from './pages/Rooms'
import { useRooms } from './pages/common'
import { Async, Pill, useAsync } from './ui'

/**
 * Faz 22 A3 — komut paleti (Ctrl+K / ⌘K): ekranlara git, öğrenci (numara/ad), ders (kod/ad) ya da salon (kod) ara.
 * Öğrenci seçilince "Öğrenci 360" açılır: kimlik, sınavları, salon + koltuk, yoklama, mazeret, sıradaki sınavın
 * krokisi. Arama sunucuda yetkilidir (yalnız sekreter ve öğrenci işleri); palet bu rollere gösterilir.
 */
type Hit = { kind: 'page' | 'student' | 'course' | 'room'; id: number | string; title: string; sub: string }
type Exam360 = { exam_id: number; course_code: string; course_name: string; starts_at: string; ends_at: string; room_code: string | null; building: string | null; seat_row: number | null; seat_col: number | null }
type Overview = {
  student: { id: number; student_no: string; full_name: string; department: string; faculty: string; disability_flag: boolean
    attendance: Record<string, { status: 'present' | 'absent'; source: string }>; excuses: { id: number; exam_id: number | null; course_code: string | null; reason: string; status: string }[] }
  exams: Exam360[]
}

export const openPalette = () => dispatchEvent(new Event('sinavplan-palette-open'))

const KIND_TR: Record<Hit['kind'], string> = { page: 'Ekran', student: 'Öğrenci', course: 'Ders', room: 'Salon' }
const seatLabel = (r: number, c: number) => `${String.fromCharCode(65 + (r % 26))}${c + 1}`

export function PaletteButton() {
  return <button className="btn sm" onClick={openPalette} title="Öğrenci, ders, salon ara (Ctrl+K)"><Icon name="search" size={15} /> Ara <kbd className="kbd">Ctrl K</kbd></button>
}

export function Palette({ pages }: { pages: { path: string; label: string }[] }) {
  const [open, setOpen] = useState(false)
  const [q, setQ] = useState('')
  const [hits, setHits] = useState<Hit[]>([])
  const [act, setAct] = useState(0)
  const [student, setStudent] = useState<number | null>(null)
  const input = useRef<HTMLInputElement>(null)
  const back = useRef<HTMLElement | null>(null)

  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); show() }
    }
    const ev = () => show()
    addEventListener('keydown', key)
    addEventListener('sinavplan-palette-open', ev)
    return () => { removeEventListener('keydown', key); removeEventListener('sinavplan-palette-open', ev) }
  }, [])
  const show = () => { back.current = document.activeElement as HTMLElement; setOpen(true); setStudent(null); setQ(''); setAct(0) }
  const close = () => { setOpen(false); setStudent(null); back.current?.focus?.() }
  useEffect(() => { if (open && student === null) input.current?.focus() }, [open, student])

  const pageHits = useMemo<Hit[]>(() => {
    const t = q.trim().toLocaleLowerCase('tr')
    return pages.filter((p) => !t || p.label.toLocaleLowerCase('tr').includes(t)).slice(0, t ? 4 : 8).map((p) => ({ kind: 'page', id: p.path, title: p.label, sub: 'Ekrana git' }))
  }, [q, pages])
  useEffect(() => {
    const t = q.trim()
    if (t.length < 2) { setHits([]); return }
    const h = setTimeout(() => {
      get<{ items: Hit[] }>(`/search?q=${encodeURIComponent(t)}`).then((r) => setHits(r.items)).catch(() => setHits([]))
    }, 180)
    return () => clearTimeout(h)
  }, [q])
  const all = [...pageHits, ...hits]
  useEffect(() => { setAct(0) }, [q, hits.length])

  const choose = (h: Hit) => {
    if (h.kind === 'student') { setStudent(Number(h.id)); return }
    if (h.kind === 'page') location.hash = `/${h.id}`
    if (h.kind === 'course') location.hash = `/takvim?q=${encodeURIComponent(h.title.split(' · ')[0])}`
    if (h.kind === 'room') location.hash = '/salonlar'
    close()
  }
  if (!open) return null
  return (
    <div className="modalback noprint" onMouseDown={(e) => e.target === e.currentTarget && close()} onKeyDown={(e) => e.key === 'Escape' && (student !== null ? setStudent(null) : close())}>
      <div className="palette" role="dialog" aria-modal="true" aria-label={student !== null ? 'Öğrenci 360' : 'Komut paleti'} data-testid="palette">
        {student !== null ? (
          <Student360 id={student} onBack={() => setStudent(null)} onClose={close} />
        ) : (
          <>
            <div className="palin">
              <Icon name="search" size={18} />
              <input ref={input} className="input" value={q} onChange={(e) => setQ(e.target.value)} placeholder="Öğrenci no / ad, ders kodu, salon ya da ekran adı…"
                role="combobox" aria-expanded={all.length > 0} aria-controls="pal-list" aria-activedescendant={all[act] ? `pal-${act}` : undefined} aria-autocomplete="list" aria-label="Ara"
                onKeyDown={(e) => {
                  if (e.key === 'ArrowDown') { e.preventDefault(); setAct((a) => Math.min(a + 1, all.length - 1)) }
                  if (e.key === 'ArrowUp') { e.preventDefault(); setAct((a) => Math.max(a - 1, 0)) }
                  if (e.key === 'Enter' && all[act]) { e.preventDefault(); choose(all[act]) }
                }} />
              <kbd className="kbd">Esc</kbd>
            </div>
            <ul id="pal-list" role="listbox" className="pallist" aria-label="Sonuçlar">
              {all.map((h, i) => (
                <li key={`${h.kind}-${h.id}`} id={`pal-${i}`} role="option" aria-selected={i === act} className={i === act ? 'on' : ''} onMouseEnter={() => setAct(i)} onClick={() => choose(h)}>
                  <span className="pill">{KIND_TR[h.kind]}</span>
                  <span className="grow"><b>{h.title}</b> <span className="small muted">{h.sub}</span></span>
                </li>
              ))}
              {q.trim().length >= 2 && hits.length === 0 && <li className="small muted" role="presentation">Öğrenci, ders ya da salon bulunamadı.</li>}
            </ul>
            <p className="small muted" style={{ margin: '6px 4px 0' }}>↑ ↓ seç · Enter aç · Esc kapat</p>
          </>
        )}
      </div>
    </div>
  )
}

function Student360({ id, onBack, onClose }: { id: number; onBack: () => void; onClose: () => void }) {
  const ov = useAsync(() => get<Overview>(`/students/${id}/overview`), [id])
  const rooms = useRooms()
  const head = useRef<HTMLHeadingElement>(null)
  useEffect(() => { head.current?.focus() }, [ov.data])
  const next = ov.data?.exams.filter((e) => Date.parse(e.ends_at) > Date.now()).sort((a, b) => a.starts_at.localeCompare(b.starts_at))[0]
  const room = next?.room_code ? rooms.data?.find((r) => r.code === next.room_code) : undefined
  const layout = useAsync(async () => (room ? ((await get(`/rooms/${room.id}`)).seats as Seat[]) : null), [room?.id])
  return (
    <div data-testid="student360">
      <div className="row between">
        <button className="btn sm" onClick={onBack}>← Aramaya dön</button>
        <button className="btn sm" onClick={onClose} aria-label="Kapat">✕</button>
      </div>
      <Async s={ov}>
        {(d) => {
          const s = d.student
          const excused = new Map(s.excuses.filter((x) => x.exam_id).map((x) => [x.exam_id!, x.status]))
          return (
            <div style={{ display: 'grid', gap: 10, marginTop: 8 }}>
              <div>
                <div className="caps">Öğrenci 360</div>
                <h2 ref={head} tabIndex={-1} style={{ margin: '2px 0', outline: 'none' }}>{s.full_name} <span className="mono small muted">{s.student_no}</span></h2>
                <div className="small muted">{s.faculty} · {s.department}{s.disability_flag && <> · <Pill kind="info">Erişilebilir koltuk</Pill></>}</div>
              </div>
              <div className="tablewrap" tabIndex={0} role="region" aria-label="Sınavları" style={{ maxHeight: 260, overflowY: 'auto' }}>
                <table>
                  <thead><tr><th>Tarih</th><th>Ders</th><th>Salon</th><th>Koltuk</th><th>Durum</th></tr></thead>
                  <tbody>
                    {d.exams.map((e) => {
                      const a = s.attendance[String(e.exam_id)]
                      const x = excused.get(e.exam_id)
                      return (
                        <tr key={e.exam_id} className={e === next ? 'tone-info' : ''}>
                          <td className="small">{fmtDate(e.starts_at)} {fmtTime(e.starts_at)}</td>
                          <td><b>{e.course_code}</b> <span className="small">{e.course_name}</span></td>
                          <td>{e.room_code ? `${e.room_code} (${e.building})` : '—'}</td>
                          <td>{e.seat_row !== null && e.seat_col !== null ? <b>{seatLabel(e.seat_row, e.seat_col)}</b> : '—'}</td>
                          <td>{x === 'approved' ? <Pill kind="info">Mazeretli</Pill> : a ? <Pill kind={a.status === 'present' ? 'ok' : 'err'}>{a.status === 'present' ? `Geldi${a.source === 'qr' ? ' (QR/kod)' : ''}` : 'Gelmedi'}</Pill> : Date.parse(e.ends_at) < Date.now() ? <Pill kind="warn">İşaretsiz</Pill> : e === next ? <Pill kind="info">Sıradaki</Pill> : <span className="small muted">—</span>}</td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
              {s.excuses.length > 0 && <div className="small"><b>Mazeretler:</b> {s.excuses.map((x) => `${x.course_code ?? 'görev'}: ${{ pending: 'bekliyor', approved: 'onaylandı', rejected: 'reddedildi' }[x.status] ?? x.status}`).join(' · ')}</div>}
              {next && room && next.seat_row !== null && next.seat_col !== null && (
                <div>
                  <div className="small"><b>Sıradaki sınav:</b> {next.course_code}, {fmtDate(next.starts_at)} {fmtTime(next.starts_at)} · {room.code}, koltuk <b>{seatLabel(next.seat_row, next.seat_col)}</b></div>
                  {layout.data && <div style={{ maxHeight: 260, overflow: 'auto', marginTop: 6 }} tabIndex={0} role="region" aria-label="Salon krokisi"><SeatMap rows={room.rows} cols={room.cols} seats={layout.data} mine={`${next.seat_row}:${next.seat_col}`} doorSide={room.door_side} doorPos={room.door_pos} /></div>}
                </div>
              )}
            </div>
          )
        }}
      </Async>
    </div>
  )
}
