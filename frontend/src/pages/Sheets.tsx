import { useEffect, useMemo, useState } from 'react'
import { all, fmtDate, fmtTime, get } from '../api'
import { useAuth } from '../auth'
import { doorLabel } from '../roomgrid'
import { Async, Card, Empty, Field, useAsync } from '../ui'
import { hasRole, ScheduleSelect, useExams, useRooms, useSchedules } from './common'
import { SeatMap, type Seat } from './Rooms'

/**
 * Faz 22 C1 — sınav günü föyleri (tarayıcının "Yazdır / PDF olarak kaydet"i ile; sunucuda PDF üretilmez):
 *  1. Salon kapısı listesi: öğrenci numarasına göre sıralı, koltuk + kapı krokisi (kapıya asılır).
 *  2. Yoklama ve imza föyü: koltuk sırasına göre, imza ve geldi/gelmedi kutuları (gözetmen doldurur).
 *  3. Gözetmen görev kâğıdı: sınavlar, gözetmenler, özel durumlar, talimatlar, olay tutanağı.
 */
type Share = { room_id: number; room_code: string; exam_id: number; course_code: string; students: number }
type SeatRow = { student_id: number; student_no: string; full_name: string; exam_id: number; seat_row: number; seat_col: number; accessible: boolean; disabled_student: boolean }
type Duty = { slot_id: number; room_id: number; room_code: string; starts_at: string; exam_id: number; course_code: string; user_id?: number; full_name?: string }

const seatLabel = (r: number, c: number) => `${String.fromCharCode(65 + (r % 26))}${c + 1}`
const KIND = [['door', 'Salon kapısı listesi'], ['sign', 'Yoklama ve imza föyü'], ['duty', 'Gözetmen görev kâğıdı']] as const
type Kind = (typeof KIND)[number][0]

export function Sheets() {
  const { user } = useAuth()
  const invigilatorOnly = hasRole(user!.roles, 'invigilator') && !hasRole(user!.roles, 'secretary', 'student_affairs')
  const sch = useSchedules()
  const exams = useExams(sch.sel)
  const rooms = useRooms()
  const duties = useAsync(async () => (invigilatorOnly ? (await get<{ items: Duty[] }>('/me/duties?limit=200')).items : null), [invigilatorOnly])
  const [slot, setSlot] = useState(0)
  const [room, setRoom] = useState(0)
  const [show, setShow] = useState<Record<Kind, boolean>>({ door: true, sign: true, duty: true })

  // seçilebilir oturumlar: gözetmen yalnız kendi görevleri, diğerleri sınavı olan tüm oturumlar
  const slots = useMemo(() => {
    const m = new Map<number, string>()
    if (invigilatorOnly) duties.data?.forEach((d) => m.set(d.slot_id, d.starts_at))
    else exams.data?.forEach((e) => m.set(e.slot_id, e.starts_at))
    return [...m.entries()].sort((a, b) => a[1].localeCompare(b[1]))
  }, [invigilatorOnly, duties.data, exams.data])
  useEffect(() => {
    if (slot || slots.length === 0) return
    const next = slots.find(([, at]) => Date.parse(at) + 150 * 60_000 > Date.now()) ?? slots[0]
    setSlot(next[0])
  }, [slots, slot])

  const shares = useAsync(async () => (sch.sel && slot ? all<Share>(`/schedules/${sch.sel}/rooms?slot_id=${slot}`) : []), [sch.sel, slot])
  const roomIds = useMemo(() => {
    const ids = invigilatorOnly ? (duties.data ?? []).filter((d) => d.slot_id === slot).map((d) => d.room_id) : (shares.data ?? []).map((s) => s.room_id)
    return [...new Set(ids)]
  }, [invigilatorOnly, duties.data, shares.data, slot])
  useEffect(() => { if (roomIds.length && !roomIds.includes(room)) setRoom(roomIds[0]) }, [roomIds, room])
  const roomOf = new Map((rooms.data ?? []).map((r) => [r.id, r]))

  const detail = useAsync(async () => {
    if (!sch.sel || !slot || !room || !shares.data) return null
    const mine = shares.data.filter((s) => s.room_id === room)
    const [seats, inv, layout] = await Promise.all([
      Promise.all(mine.map((s) => all<SeatRow>(`/schedules/${sch.sel}/seating?exam_id=${s.exam_id}&room_id=${room}`))).then((x) => x.flat()),
      all<Duty>(`/schedules/${sch.sel}/invigilators?slot_id=${slot}`).then((x) => x.filter((d) => d.room_id === room)).catch(() => [] as Duty[]),
      get(`/rooms/${room}`).then((r) => r.seats as Seat[]),
    ])
    return { mine, seats, inv, layout }
  }, [sch.sel, slot, room, shares.data])

  const r = roomOf.get(room)
  const exam = new Map((exams.data ?? []).map((e) => [e.exam_id, e]))
  const at = slots.find(([s]) => s === slot)?.[1]
  const head = (title: string) => (
    <header className="sheethead">
      <div><div className="caps">SınavPlan · {title}</div><h2>{r?.code ?? '—'} <span className="muted small">{r ? `Bina ${r.building} · Kat ${r.floor} · Kapı: ${doorLabel(r.door_side, r.door_pos)}` : ''}</span></h2></div>
      <div className="small" style={{ textAlign: 'right' }}>{at && <><b>{fmtDate(at)}</b><br />{fmtTime(at)}</>}</div>
    </header>
  )

  return (
    <>
      <Card title="Yazdırılabilir föyler" aside={<ScheduleSelect s={sch} />}>
        <div className="row noprint" style={{ alignItems: 'flex-end' }}>
          <Field label="Oturum" id="sh-slot">
            <select id="sh-slot" value={slot} onChange={(e) => { setSlot(Number(e.target.value)); setRoom(0) }} style={{ width: 'auto' }}>
              {slots.map(([id, t]) => <option key={id} value={id}>{fmtDate(t)} {fmtTime(t)}</option>)}
            </select>
          </Field>
          <Field label="Salon" id="sh-room">
            <select id="sh-room" value={room} onChange={(e) => setRoom(Number(e.target.value))} style={{ width: 'auto' }}>
              {roomIds.map((id) => <option key={id} value={id}>{roomOf.get(id)?.code ?? `#${id}`}</option>)}
            </select>
          </Field>
          <fieldset className="row" style={{ border: 0, padding: 0, margin: 0 }}>
            <legend className="sr-only">Yazdırılacak föyler</legend>
            {KIND.map(([k, l]) => <label key={k} className="small row" style={{ gap: 4 }}><input type="checkbox" checked={show[k]} onChange={(e) => setShow({ ...show, [k]: e.target.checked })} />{l}</label>)}
          </fieldset>
          <button className="btn primary" onClick={() => print()} disabled={!detail.data}>Yazdır / PDF</button>
        </div>
        <p className="small muted noprint" style={{ margin: '10px 0 0' }}>Her föy ayrı A4 sayfasına basılır. PDF için yazdırma penceresinde "PDF olarak kaydet"i seçin.</p>
        {invigilatorOnly && slots.length === 0 && duties.data && <Empty title="Görev yok" hint="Size atanmış bir sınav görevi bulunmuyor." />}
      </Card>

      <Async s={detail} empty={(d) => !d || d.seats.length === 0}>
        {(d) => !d ? null : (
          <div className="sheets" data-testid="sheets">
            {show.door && (
              <section className="sheet" data-sheet="door">
                {head('Salon kapısı listesi')}
                <p className="small">Numaranızı bulun, koltuğunuza oturun. Koltuk harfi sırayı (A = tahtaya en yakın), sayı sütunu gösterir.</p>
                <table className="sheettable">
                  <thead><tr><th>Öğrenci no</th><th>Ad soyad</th><th>Ders</th><th>Koltuk</th></tr></thead>
                  <tbody>
                    {[...d.seats].sort((a, b) => a.student_no.localeCompare(b.student_no)).map((s) => (
                      <tr key={`${s.exam_id}-${s.student_id}`}><td className="mono">{s.student_no}</td><td>{s.full_name}</td><td>{exam.get(s.exam_id)?.course_code ?? s.exam_id}</td><td><b>{seatLabel(s.seat_row, s.seat_col)}</b>{s.disabled_student ? ' ♿' : ''}</td></tr>
                    ))}
                  </tbody>
                </table>
                {r && <div className="sheetmap"><SeatMap rows={r.rows} cols={r.cols} seats={d.layout} doorSide={r.door_side} doorPos={r.door_pos} /></div>}
              </section>
            )}
            {show.sign && (
              <section className="sheet" data-sheet="sign">
                {head('Yoklama ve imza föyü')}
                <table className="sheettable">
                  <thead><tr><th>Koltuk</th><th>Öğrenci no</th><th>Ad soyad</th><th>Ders</th><th style={{ width: '22%' }}>İmza</th><th>Geldi</th><th>Gelmedi</th></tr></thead>
                  <tbody>
                    {[...d.seats].sort((a, b) => a.seat_row - b.seat_row || a.seat_col - b.seat_col).map((s) => (
                      <tr key={`${s.exam_id}-${s.student_id}`}><td><b>{seatLabel(s.seat_row, s.seat_col)}</b></td><td className="mono">{s.student_no}</td><td>{s.full_name}</td><td>{exam.get(s.exam_id)?.course_code}</td><td /><td className="box">☐</td><td className="box">☐</td></tr>
                    ))}
                  </tbody>
                </table>
                <p className="small">Toplam {d.seats.length} öğrenci · Gelen: ____ · Gelmeyen: ____ · Gözetmen imzası: ______________</p>
              </section>
            )}
            {show.duty && (
              <section className="sheet" data-sheet="duty">
                {head('Gözetmen görev kâğıdı')}
                <h3>Sınavlar</h3>
                <table className="sheettable">
                  <thead><tr><th>Ders</th><th>Öğretim üyesi</th><th>Süre</th><th>Bu salonda</th></tr></thead>
                  <tbody>{d.mine.map((s) => { const e = exam.get(s.exam_id); return <tr key={s.exam_id}><td><b>{s.course_code}</b> {e?.course_name}</td><td>{e?.lecturer_name || '—'}</td><td>{e ? `${fmtTime(e.starts_at)}–${fmtTime(e.ends_at)} (${e.duration_minutes} dk)` : '—'}</td><td>{s.students} öğrenci</td></tr> })}</tbody>
                </table>
                <h3>Gözetmenler</h3>
                <p>{d.inv.length ? d.inv.map((x) => x.full_name).join(', ') : '—'} {d.seats.length > 50 && <span className="small muted">(50'den fazla öğrenci: en az 2 gözetmen)</span>}</p>
                <h3>Özel durumlar</h3>
                <p>{d.seats.filter((s) => s.disabled_student).map((s) => `${s.full_name} (${seatLabel(s.seat_row, s.seat_col)}, erişilebilir koltuk)`).join('; ') || 'Yok'}</p>
                <h3>Talimatlar</h3>
                <ol className="small">
                  <li>Sınavdan 30 dk önce salonda olun; SınavPlan → Yoklama ve görevlerim → görevi seçip "Yeni QR oluştur" deyin. QR çalışmazsa ekrandaki 6 haneli kodu söyleyin.</li>
                  <li>Ağ kesilirse yoklamayı tablette elle işaretleyin; bağlantı gelince "Senkronla" deyin (olaylar kaybolmaz).</li>
                  <li>Aynı dersin öğrencileri yan yana oturmaz; yer değişikliğine izin vermeyin.</li>
                  <li>Tahliye (yangın tatbikatı vb.) gerekirse sekreterliği arayın; toplu salon değişikliği sistemden yapılır ve kaydedilir.</li>
                </ol>
                <h3>Olay tutanağı</h3>
                <div className="lines" aria-hidden><i /><i /><i /><i /><i /></div>
                <p className="small" style={{ marginTop: 18 }}>Gözetmen 1: ______________________ &nbsp; Gözetmen 2: ______________________ &nbsp; Saat: ________</p>
              </section>
            )}
          </div>
        )}
      </Async>
    </>
  )
}

