import { useMemo, useState } from 'react'
import { all, get, idemKey, post } from '../api'
import { useAuth } from '../auth'
import { Async, Card, Empty, ErrorBox, Field, Pill, Tabs, useAction, useAsync } from '../ui'
import { hasRole, ScheduleSelect, useExams, useRooms, useSchedules, type Room } from './common'

type Seat = { row: number; col: number; active: boolean; accessible: boolean }
type SeatRow = { student_id: number; student_no: string; full_name: string; exam_id: number; room_id: number; seat_row: number; seat_col: number; accessible: boolean; disabled_student: boolean }

const rowLetter = (r: number) => String.fromCharCode(65 + (r % 26))

function SeatMap({ rows, cols, seats, taken, mine }: { rows: number; cols: number; seats?: Seat[]; taken?: Map<string, SeatRow>; mine?: string }) {
  const at = new Map(seats?.map((s) => [`${s.row}:${s.col}`, s]))
  const cells = []
  for (let r = 0; r < rows; r++)
    for (let c = 0; c < cols; c++) {
      const k = `${r}:${c}`
      const s = at.get(k)
      const t = taken?.get(k)
      const cls = ['seat', s && !s.active ? 'off' : '', s?.accessible ? 'acc' : '', t ? (t.disabled_student ? 'dis' : 'taken') : '', mine === k ? 'mine' : ''].join(' ')
      cells.push(
        <div key={k} className={cls} title={t ? `${t.full_name} (${t.student_no})${t.disabled_student ? ' · erişilebilir koltuk' : ''}` : s && !s.active ? 'Kullanım dışı' : s?.accessible ? 'Erişilebilir koltuk' : 'Boş koltuk'}>
          {s && !s.active ? '' : `${rowLetter(r)}${c + 1}`}
        </div>,
      )
    }
  return (
    <div className="seatmap" style={{ gridTemplateColumns: `repeat(${cols}, minmax(38px, 1fr))` }} role="img" aria-label={`${rows} sıra, ${cols} sütunlu oturma düzeni`}>
      {cells}
    </div>
  )
}

const Legend = ({ withStudents }: { withStudents?: boolean }) => (
  <div className="legend">
    {withStudents && <span><i style={{ background: 'var(--primary)' }} />Dolu</span>}
    {withStudents && <span><i style={{ background: 'var(--ok)' }} />Engelli öğrenci (erişilebilir koltuk)</span>}
    <span><i style={{ borderColor: 'var(--ok)' }} />Erişilebilir koltuk</span>
    <span><i style={{ background: 'var(--high)', borderStyle: 'dashed' }} />Kullanım dışı</span>
    <span><i />Boş</span>
  </div>
)

export function Rooms() {
  const [tab, setTab] = useState<'inv' | 'plan'>('plan')
  return (
    <>
      <div className="head">
        <div>
          <div className="caps">Derslik ve salon</div>
          <h1>Salon ve oturma planı</h1>
        </div>
        <Tabs value={tab} onChange={setTab} items={[['plan', 'Sınav oturma planı'], ['inv', 'Salon envanteri']]} />
      </div>
      {tab === 'inv' ? <Inventory /> : <Plan />}
    </>
  )
}

function Inventory() {
  const rooms = useRooms()
  const [type, setType] = useState('')
  const [open, setOpen] = useState<Room | null>(null)
  const list = useMemo(() => (rooms.data ?? []).filter((r) => !type || r.type === type), [rooms.data, type])
  const detail = useAsync(async () => (open ? ((await get(`/rooms/${open.id}`)).seats as Seat[]) : null), [open?.id])
  return (
    <div className="grid g2">
      <Card title="Salonlar" aside={<select aria-label="Salon türü" value={type} onChange={(e) => setType(e.target.value)} style={{ width: 'auto' }}><option value="">Tümü</option><option value="classic">Klasik</option><option value="lab">Laboratuvar</option></select>}>
        <Async s={rooms} empty={(d) => d.length === 0}>
          {() => (
            <div className="tablewrap" style={{ maxHeight: 520, overflowY: 'auto' }}>
              <table>
                <thead><tr><th>Kod</th><th>Bina</th><th>Tür</th><th>Kapasite</th><th>Erişilebilir</th><th /></tr></thead>
                <tbody>
                  {list.map((r) => (
                    <tr key={r.id}>
                      <td><b>{r.code}</b></td><td>{r.building}</td>
                      <td><Pill kind={r.type === 'lab' ? 'info' : ''}>{r.type === 'lab' ? 'Lab' : 'Klasik'}</Pill></td>
                      <td>{r.capacity}</td><td>{r.accessible_seats}</td>
                      <td><button className="btn sm" onClick={() => setOpen(r)}>Düzeni gör</button></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Async>
      </Card>
      <Card title={open ? `${open.code} koltuk düzeni` : 'Koltuk düzeni'}>
        {!open ? <Empty title="Bir salon seçin" hint="Koltuk düzenini görmek için soldan “Düzeni gör”e basın." /> : (
          <Async s={detail}>{(seats) => (<><SeatMap rows={open.rows} cols={open.cols} seats={seats} /><div style={{ marginTop: 12 }}><Legend /></div><p className="small muted" style={{ marginTop: 8 }}>{open.rows} sıra × {open.cols} sütun · {open.capacity} kullanılabilir koltuk</p></>)}</Async>
        )}
      </Card>
    </div>
  )
}

function Plan() {
  const { user } = useAuth()
  const sch = useSchedules()
  const exams = useExams(sch.sel)
  const rooms = useRooms()
  const [exam, setExam] = useState<number | null>(null)
  const ex = exams.data?.find((e) => e.exam_id === exam)
  const seating = useAsync(async () => (sch.sel && exam ? all<SeatRow>(`/schedules/${sch.sel}/seating?exam_id=${exam}`) : null), [sch.sel, exam])
  const byRoom = useMemo(() => {
    const m = new Map<number, SeatRow[]>()
    seating.data?.forEach((r) => m.set(r.room_id, [...(m.get(r.room_id) ?? []), r]))
    return [...m.entries()]
  }, [seating.data])
  const secretary = hasRole(user!.roles, 'secretary')
  return (
    <>
      <Card title="Sınav seçin" aside={<ScheduleSelect s={sch} />}>
        <Async s={exams} empty={(d) => d.length === 0}>
          {(list) => (
            <div className="row">
              <select aria-label="Sınav" value={exam ?? ''} onChange={(e) => setExam(e.target.value ? Number(e.target.value) : null)} style={{ maxWidth: 520 }}>
                <option value="">Sınav seçin…</option>
                {list.map((e) => <option key={e.exam_id} value={e.exam_id}>{e.course_code} — {e.course_name} ({e.students} öğrenci)</option>)}
              </select>
              {ex && <span className="small muted">{ex.rooms} salon · {ex.requires_lab ? 'laboratuvar' : 'klasik'} · oturum {ex.session + 1}</span>}
            </div>
          )}
        </Async>
      </Card>
      {!exam ? <Card><Empty title="Oturma planı için bir sınav seçin" hint="Öğretim üyeleri yalnızca kendi derslerinin planını, gözetmenler yalnızca görevli oldukları salonlardaki sınavları görebilir." /></Card> : (
        <Async s={seating} empty={(d) => d.length === 0}>
          {(rows) => (
            <>
              <p className="small muted">{rows.length} öğrenci, {byRoom.length} salonda. Aynı sınavın öğrencileri yan yana oturmaz; engelli öğrenciler erişilebilir koltuktadır.</p>
              {byRoom.map(([rid, list]) => <RoomPlan key={rid} room={rooms.data?.find((r) => r.id === rid)} rows={list} />)}
              {secretary && ex && <Evacuate scheduleId={sch.sel!} slotId={ex.slot_id} from={byRoom.map(([r]) => r)} rooms={rooms.data ?? []} onDone={seating.reload} />}
            </>
          )}
        </Async>
      )}
    </>
  )
}

function RoomPlan({ room, rows }: { room?: Room; rows: SeatRow[] }) {
  const seats = useAsync(async () => (room ? ((await get(`/rooms/${room.id}`)).seats as Seat[]) : []), [room?.id])
  const taken = new Map(rows.map((r) => [`${r.seat_row}:${r.seat_col}`, r]))
  return (
    <Card title={`${room?.code ?? 'Salon'} · ${room?.building ?? ''}`} aside={<Pill>{rows.length} öğrenci</Pill>}>
      {!room ? <Empty title="Salon bilgisi yok" /> : <Async s={seats}>{(s) => (<><SeatMap rows={room.rows} cols={room.cols} seats={s} taken={taken} /><div style={{ marginTop: 10 }}><Legend withStudents /></div></>)}</Async>}
    </Card>
  )
}

function Evacuate({ scheduleId, slotId, from, rooms, onDone }: { scheduleId: number; slotId: number; from: number[]; rooms: Room[]; onDone: () => void }) {
  const [src, setSrc] = useState(from[0] ?? 0)
  const [dst, setDst] = useState(0)
  const [reason, setReason] = useState('')
  const act = useAction(async () => {
    const r = await post(`/schedules/${scheduleId}/room-change`, { slot_id: slotId, from_room_id: src, to_room_id: dst, reason }, { 'Idempotency-Key': idemKey('room-change') })
    onDone()
    return r as { moved: number }
  })
  const name = (id: number) => rooms.find((r) => r.id === id)?.code ?? `#${id}`
  const full = act.error?.code === 'insufficient_capacity'
  return (
    <Card title="Toplu salon değişikliği (tahliye)">
      <p className="small muted" style={{ marginBottom: 12 }}>Kaynak salondaki bu oturumun tüm öğrencileri tek işlemde hedef salona taşınır; hedefe sığmazsa hiçbir şey değişmez.</p>
      <div className="row" style={{ alignItems: 'flex-end' }}>
        <Field label="Kaynak salon" id="src"><select id="src" value={src} onChange={(e) => setSrc(Number(e.target.value))}>{from.map((id) => <option key={id} value={id}>{name(id)}</option>)}</select></Field>
        <Field label="Hedef salon" id="dst"><select id="dst" value={dst} onChange={(e) => setDst(Number(e.target.value))}><option value={0}>Seçin…</option>{rooms.filter((r) => r.id !== src).map((r) => <option key={r.id} value={r.id}>{r.code} ({r.capacity})</option>)}</select></Field>
        <Field label="Gerekçe" id="why"><input id="why" className="input" value={reason} onChange={(e) => setReason(e.target.value)} placeholder="örn. klima arızası" /></Field>
        <button className="btn danger" disabled={act.busy || !dst || !reason.trim()} onClick={() => confirm('Öğrenciler hedef salona taşınacak. Onaylıyor musunuz?') && void act.run()}>{act.busy ? 'Taşınıyor…' : 'Tahliye et'}</button>
      </div>
      {act.result && <div className="alert ok" style={{ marginTop: 12 }} role="status">{act.result.moved} öğrenci taşındı. Gözetmenler ve oturma planı güncellendi.</div>}
      {act.error && <div style={{ marginTop: 12 }}>{full ? <div className="alert err" role="alert"><div><b>Hedef salona sığmıyor.</b> {act.error.message} Hiçbir kayıt değişmedi.</div></div> : <ErrorBox error={act.error} />}</div>}
    </Card>
  )
}
