import { useMemo, useState } from 'react'
import { markAttendance } from '../attendance'
import { useLive } from '../live'
import { all, fmtDateTime, get, idemKey, post, request } from '../api'
import { useAuth } from '../auth'
import { doorLabel, doorShort, RoomGrid } from '../roomgrid'
import { RoomEditor } from './RoomEditor'
import { Sheets } from './Sheets'
import { Async, Bar, Card, Empty, ErrorBox, Field, Pill, StatCard, Status, Tabs, useAction, useAsync } from '../ui'
import { examLabel, hasRole, ScheduleSelect, useExamFilter, useExams, useRooms, useSchedules, type Room } from './common'

export type Seat = { row: number; col: number; active: boolean; accessible: boolean }
type SeatRow = { student_id: number; student_no: string; full_name: string; exam_id: number; room_id: number; seat_row: number; seat_col: number; accessible: boolean; disabled_student: boolean }

type Att = { student_id: number; status: 'present' | 'absent'; source: string; updated_at: string }

const rowLetter = (r: number) => String.fromCharCode(65 + (r % 26))

export function SeatMap({ rows, cols, seats, taken, mine, doorSide, doorPos, picked, onPick, stateOf }: { rows: number; cols: number; seats?: Seat[]; taken?: Map<string, SeatRow>; mine?: string; doorSide?: string; doorPos?: number; picked?: string; onPick?: (t: SeatRow) => void; stateOf?: (t: SeatRow) => 'present' | 'absent' | 'none' }) {
  const at = new Map(seats?.map((s) => [`${s.row}:${s.col}`, s]))
  return (
    <RoomGrid rows={rows} cols={cols} doorSide={doorSide} doorPos={doorPos} label={`${rows} sıra, ${cols} sütunlu oturma düzeni`}>
      {(r, c) => {
        const k = `${r}:${c}`
        const s = at.get(k)
        const t = taken?.get(k)
        const st = t && stateOf ? stateOf(t) : 'none'
        const cls = ['seat', s && !s.active ? 'off' : '', s?.accessible ? 'acc' : '', t ? (t.disabled_student ? 'dis' : 'taken') : '', mine === k ? 'mine' : '', st === 'present' ? 'here' : st === 'absent' ? 'gone' : '', picked === k ? 'picked' : ''].join(' ')
        const act = t && onPick ? { role: 'button', tabIndex: 0, onClick: () => onPick(t), onKeyDown: (e: React.KeyboardEvent) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onPick(t) } } } : {}
        return (
          <div className={cls} {...act} title={t ? `${t.full_name} (${t.student_no})${t.disabled_student ? ' · erişilebilir koltuk' : ''}` : s && !s.active ? 'Kullanım dışı' : s?.accessible ? 'Erişilebilir koltuk' : 'Boş koltuk'}>
            {s && !s.active ? '' : `${rowLetter(r)}${c + 1}`}
          </div>
        )
      }}
    </RoomGrid>
  )
}

const Legend = ({ withStudents }: { withStudents?: boolean }) => (
  <div className="legend">
    {withStudents && <span><i style={{ background: 'var(--primary)' }} />Dolu</span>}
    {withStudents && <span><i style={{ background: 'var(--ok)' }} />Engelli öğrenci (erişilebilir koltuk)</span>}
    <span><i style={{ borderColor: 'var(--ok)' }} />Erişilebilir koltuk</span>
    <span><i style={{ background: 'var(--high)', borderStyle: 'dashed' }} />Kullanım dışı</span>
    <span><i />Boş</span>
    <span><i style={{ background: 'var(--tertiary, #b45309)' }} />Kapı</span>
  </div>
)

export function Rooms() {
  const [tab, setTab] = useState<'cap' | 'inv' | 'plan' | 'sheets'>('cap')
  return (
    <>
      <div className="head">
        <div>
          <div className="caps">Derslik ve salon</div>
          <h1>Salon ve oturma planı</h1>
        </div>
        <Tabs value={tab} onChange={setTab} items={[['cap', 'Kapasite panosu'], ['plan', 'Sınav oturma planı'], ['sheets', 'Föyler'], ['inv', 'Salon envanteri']]} />
      </div>
      {tab === 'inv' ? <Inventory /> : tab === 'cap' ? <Capacity /> : tab === 'sheets' ? <Sheets /> : <Plan />}
    </>
  )
}

function Inventory() {
  const rooms = useRooms()
  const [type, setType] = useState('')
  const { user } = useAuth()
  const secretary = hasRole(user!.roles, 'secretary')
  const [open, setOpen] = useState<Room | null>(null)
  const [edit, setEdit] = useState<Room | 'new' | null>(null)
  const list = useMemo(() => (rooms.data ?? []).filter((r) => !type || r.type === type), [rooms.data, type])
  const detail = useAsync(async () => (open ? ((await get(`/rooms/${open.id}`)).seats as Seat[]) : null), [open?.id])
  if (edit) return <RoomEditor room={edit === 'new' ? null : edit} onDone={(changed) => { setEdit(null); if (changed) { setOpen(null); void rooms.reload() } }} />
  return (
    <div className="grid" style={{ gridTemplateColumns: '1fr', gap: 16 }}>
      <Card title="Salonlar" aside={<div className="row">{secretary && <button className="btn primary sm" onClick={() => setEdit('new')}>+ Yeni salon</button>}<select aria-label="Salon türü" value={type} onChange={(e) => setType(e.target.value)} style={{ width: 'auto' }}><option value="">Tümü</option><option value="classic">Klasik</option><option value="lab">Laboratuvar</option></select></div>}>
        <Async s={rooms} empty={(d) => d.length === 0}>
          {() => (
            <div className="tablewrap" style={{ maxHeight: 360, overflowY: 'auto' }} tabIndex={0} role="region" aria-label="Salon listesi">
              <table>
                <thead><tr><th>Kod</th><th>Bina</th><th>Tür</th><th>Kapasite</th><th>Erişilebilir</th><th>Kapı</th><th /></tr></thead>
                <tbody>
                  {list.map((r) => (
                    <tr key={r.id}>
                      <td><b>{r.code}</b></td><td>{r.building}</td>
                      <td><Pill kind={r.type === 'lab' ? 'info' : ''}>{r.type === 'lab' ? 'Lab' : 'Klasik'}</Pill></td>
                      <td>{r.capacity}</td><td>{r.accessible_seats}</td><td className="small" style={{ whiteSpace: 'nowrap' }} title={doorLabel(r.door_side, r.door_pos)}>{doorShort(r.door_side, r.door_pos)}</td>
                      <td><div className="row" style={{ flexWrap: 'nowrap', whiteSpace: 'nowrap' }}><button className="btn sm" onClick={() => setOpen(r)}>Düzeni gör</button>{secretary && <button className="btn sm" onClick={() => setEdit(r)}>Düzenle</button>}</div></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Async>
      </Card>
      <Card title={open ? `${open.code} koltuk düzeni` : 'Koltuk düzeni'}>
        {!open ? <Empty title="Bir salon seçin" hint="Koltuk düzenini ve kapı konumunu görmek için yukarıdan “Düzeni gör”e basın." /> : (
          <Async s={detail}>{(seats) => (<><SeatMap rows={open.rows} cols={open.cols} seats={seats} doorSide={open.door_side} doorPos={open.door_pos} /><div style={{ marginTop: 12 }}><Legend /></div><p className="small muted" style={{ marginTop: 8 }}>{open.rows} sıra × {open.cols} sütun · {open.capacity} kullanılabilir koltuk · kapı: {doorLabel(open.door_side, open.door_pos)}</p></>)}</Async>
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
  const filt = useExamFilter()
  const [exam, setExam] = useState<number | null>(null)
  const ex = exams.data?.find((e) => e.exam_id === exam)
  const seating = useAsync(async () => (sch.sel && exam ? all<SeatRow>(`/schedules/${sch.sel}/seating?exam_id=${exam}`) : null), [sch.sel, exam])
  // Yoklama durumu (var/yok) 5 sn'de bir tazelenir; kaydı olmayan öğrenci "işaretsiz"dir
  const att = useLive(async () => new Map((await all<Att>(`/exams/${exam}/attendance`)).map((a) => [a.student_id, a])), [exam], 5000, !!exam)
  const byRoom = useMemo(() => {
    const m = new Map<number, SeatRow[]>()
    seating.data?.forEach((r) => m.set(r.room_id, [...(m.get(r.room_id) ?? []), r]))
    return [...m.entries()]
  }, [seating.data])
  const secretary = hasRole(user!.roles, 'secretary')
  const canMark = hasRole(user!.roles, 'secretary', 'invigilator')
  return (
    <>
      <Card title="Sınav seçin" aside={<ScheduleSelect s={sch} />}>
        <Async s={exams} empty={(d) => d.length === 0}>
          {(list) => (
            <div className="row">
              {filt.element}
              <select aria-label="Sınav" value={exam ?? ''} onChange={(e) => setExam(e.target.value ? Number(e.target.value) : null)} style={{ maxWidth: 520 }}>
                <option value="">Sınav seçin ({list.filter(filt.match).length})…</option>
                {list.filter(filt.match).map((e) => <option key={e.exam_id} value={e.exam_id}>{examLabel(e)} · {e.students} öğrenci</option>)}
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
              <p className="small muted">{rows.length} öğrenci, {byRoom.length} salonda. Aynı sınavın öğrencileri yan yana oturmaz; engelli öğrenciler erişilebilir koltuktadır. Aşağıda her salonun krokisi ve öğrenci listesi, yoklamadaki durumlarıyla birlikte (geldi / gelmedi / işaretsiz) görünür.</p>
              {byRoom.map(([rid, list]) => <RoomPlan key={rid} room={rooms.data?.find((r) => r.id === rid)} rows={list} att={att.data} canMark={canMark} onMarked={() => void att.reload()} />)}
              {secretary && ex && <Evacuate scheduleId={sch.sel!} slotId={ex.slot_id} from={byRoom.map(([r]) => r)} rooms={rooms.data ?? []} onDone={seating.reload} />}
            </>
          )}
        </Async>
      )}
    </>
  )
}

function RoomPlan({ room, rows, att, canMark = false, onMarked }: { room?: Room; rows: SeatRow[]; att: Map<number, Att> | null; canMark?: boolean; onMarked?: () => void }) {
  const seats = useAsync(async () => (room ? ((await get(`/rooms/${room.id}`)).seats as Seat[]) : []), [room?.id])
  const taken = new Map(rows.map((r) => [`${r.seat_row}:${r.seat_col}`, r]))
  const roster = useMemo(() => [...rows].sort((a, b) => a.seat_row - b.seat_row || a.seat_col - b.seat_col), [rows])
  // krokiden/listeden işaretlenenler (ağ yoksa kuyrukta bekler) sunucu yanıtından önce de görünsün
  const [local, setLocal] = useState<Map<number, 'present' | 'absent'>>(new Map())
  const [queued, setQueued] = useState<Set<number>>(new Set())
  const [pick, setPick] = useState<SeatRow | null>(null)
  const mark = useAction(async (r: SeatRow, status: 'present' | 'absent') => {
    const how = await markAttendance(r.exam_id, r.student_id, status)
    setLocal((m) => new Map(m).set(r.student_id, status))
    setQueued((q) => { const n = new Set(q); if (how === 'queued') n.add(r.student_id); else n.delete(r.student_id); return n })
    if (how === 'synced') onMarked?.()
    return how
  })
  const state = (r: SeatRow): 'present' | 'absent' | 'none' => local.get(r.student_id) ?? att?.get(r.student_id)?.status ?? 'none'
  const here = roster.filter((r) => state(r) === 'present').length, gone = roster.filter((r) => state(r) === 'absent').length
  return (
    <Card title={`${room?.code ?? 'Salon'} · ${room?.building ?? ''}`} aside={<div className="row"><Pill kind="ok">{here} geldi</Pill><Pill kind={gone ? 'err' : ''}>{gone} gelmedi</Pill><Pill>{roster.length - here - gone} işaretsiz</Pill></div>}>
      {!room ? <Empty title="Salon bilgisi yok" /> : <Async s={seats}>{(s) => (<><SeatMap rows={room.rows} cols={room.cols} seats={s} taken={taken} doorSide={room.door_side} doorPos={room.door_pos} stateOf={state} picked={pick ? `${pick.seat_row}:${pick.seat_col}` : undefined} onPick={canMark ? setPick : undefined} /><div style={{ marginTop: 10 }}><Legend withStudents /></div></>)}</Async>}
      {canMark && (
        <div className="markbar" role="status" data-testid="mark-bar">
          {pick ? (
            <>
              <span><b>{pick.full_name}</b> <span className="mono small muted">{pick.student_no}</span> · koltuk <b>{rowLetter(pick.seat_row)}{pick.seat_col + 1}</b> · şu an: <b>{{ present: 'Geldi', absent: 'Gelmedi', none: 'İşaretsiz' }[state(pick)]}</b>{queued.has(pick.student_id) && <span className="small muted"> (ağ yok, kuyrukta)</span>}</span>
              <span className="row"><button className="btn sm ok" disabled={mark.busy} onClick={() => void mark.run(pick, 'present')}>GELDİ</button><button className="btn sm danger" disabled={mark.busy} onClick={() => void mark.run(pick, 'absent')}>GELMEDİ</button><button className="btn sm" onClick={() => setPick(null)}>Bırak</button></span>
            </>
          ) : <span className="small muted">Yoklama için krokide bir koltuğa ya da listede bir öğrenciye tıklayın; geldi/gelmedi işaretleyin. Ağ yoksa kayıt cihazda saklanır ve Yoklama ekranından eşitlenir.</span>}
          {mark.error && <ErrorBox error={mark.error} />}
        </div>
      )}
      <div className="tablewrap" style={{ marginTop: 12, maxHeight: 360, overflowY: 'auto' }} tabIndex={0} role="region" aria-label={`${room?.code ?? 'Salon'} öğrenci listesi`} data-testid="student-roster">
        <table>
          <thead><tr><th>Koltuk</th><th>Öğrenci</th><th>No</th><th>Yoklama</th>{canMark && <th />}</tr></thead>
          <tbody>
            {roster.map((r) => {
              const a = att?.get(r.student_id)
              const cur = state(r)
              return (
                <tr key={r.student_id} className={pick?.student_id === r.student_id ? 'tone-info' : ''} onClick={canMark ? () => setPick(r) : undefined}>
                  <td><b>{rowLetter(r.seat_row)}{r.seat_col + 1}</b>{r.accessible && <> <Pill kind="info">erişilebilir</Pill></>}</td>
                  <td>{r.full_name}</td>
                  <td className="mono small">{r.student_no}</td>
                  <td>{cur !== 'none' ? <Pill kind={cur === 'present' ? 'ok' : 'err'}>{cur === 'present' ? `Geldi${a?.status === 'present' && a.source === 'qr' && !local.has(r.student_id) ? ' (QR)' : ''}` : 'Gelmedi'}</Pill> : <span className="small muted">İşaretsiz</span>}{queued.has(r.student_id) && <span className="small muted"> · kuyrukta</span>}</td>
                  {canMark && <td><div className="row" style={{ justifyContent: 'flex-end', flexWrap: 'nowrap' }}><button className="btn sm ok" disabled={mark.busy} onClick={(e) => { e.stopPropagation(); void mark.run(r, 'present') }} aria-label={`${r.full_name} geldi`}>GELDİ</button><button className="btn sm danger" disabled={mark.busy} onClick={(e) => { e.stopPropagation(); void mark.run(r, 'absent') }} aria-label={`${r.full_name} gelmedi`}>GELMEDİ</button></div></td>}
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </Card>
  )
}

function Evacuate({ scheduleId, slotId, from, rooms, onDone }: { scheduleId: number; slotId: number; from: number[]; rooms: Room[]; onDone: () => void }) {
  const [src, setSrc] = useState(from[0] ?? 0)
  const [dst, setDst] = useState(0)
  const [reason, setReason] = useState('')
  // Faz 22 B2: "Önizle" aynı işlemi koşturup geri alır (?dry_run=true); "Tahliye et" uygular.
  // önizleme yalnız alındığı (kaynak, hedef) çifti için geçerli: seçim değişirse "Tahliye et" yeniden önizleme ister
  const pair = `${src}-${dst}`
  const [pvFor, setPvFor] = useState('')
  const pv = useAction(async () => {
    const r = (await post(`/schedules/${scheduleId}/room-change?dry_run=true`, { slot_id: slotId, from_room_id: src, to_room_id: dst, reason })) as { moved: number }
    setPvFor(pair)
    return r
  })
  const fresh = pvFor === pair && !!pv.result
  const act = useAction(async () => {
    const r = await post(`/schedules/${scheduleId}/room-change`, { slot_id: slotId, from_room_id: src, to_room_id: dst, reason }, { 'Idempotency-Key': idemKey('room-change') })
    onDone()
    return r as { moved: number }
  })
  const name = (id: number) => rooms.find((r) => r.id === id)?.code ?? `#${id}`
  const full = act.error?.code === 'insufficient_capacity'
  const pvFull = pv.error?.code === 'insufficient_capacity'
  return (
    <Card title="Toplu salon değişikliği (tahliye)">
      <p className="small muted" style={{ marginBottom: 12 }}>Kaynak salondaki bu oturumun tüm öğrencileri tek işlemde hedef salona taşınır; hedefe sığmazsa hiçbir şey değişmez.</p>
      <div className="row" style={{ alignItems: 'flex-end' }}>
        <Field label="Kaynak salon" id="src"><select id="src" value={src} onChange={(e) => setSrc(Number(e.target.value))}>{from.map((id) => <option key={id} value={id}>{name(id)}</option>)}</select></Field>
        <Field label="Hedef salon" id="dst"><select id="dst" value={dst} onChange={(e) => setDst(Number(e.target.value))}><option value={0}>Seçin…</option>{rooms.filter((r) => r.id !== src).map((r) => <option key={r.id} value={r.id}>{r.code} ({r.capacity})</option>)}</select></Field>
        <Field label="Gerekçe" id="why"><input id="why" className="input" value={reason} onChange={(e) => setReason(e.target.value)} placeholder="örn. klima arızası" /></Field>
        <button className="btn" disabled={pv.busy || !dst || !reason.trim()} onClick={() => void pv.run()}>{pv.busy ? 'Hesaplanıyor…' : 'Önizle'}</button>
        <button className="btn danger" disabled={act.busy || !fresh || !dst || !reason.trim()} onClick={() => void act.run()}>{act.busy ? 'Taşınıyor…' : 'Tahliye et'}</button>
      </div>
      {fresh && !act.result && pv.result && <div className="alert info" style={{ marginTop: 12 }} role="status" data-testid="evac-preview"><div><b>Önizleme:</b> {pv.result.moved} öğrenci {name(src)} → {name(dst)} taşınacak; kopya kuralı (yan yana aynı sınav yok) ve engelli koltukları korunarak yerleşiyor. Henüz hiçbir şey değişmedi.</div></div>}
      {pv.error && <div style={{ marginTop: 12 }}>{pvFull ? <div className="alert err" role="alert"><div><b>Önizleme: hedef salona sığmıyor.</b> {pv.error.message}</div></div> : <ErrorBox error={pv.error} />}</div>}
      {act.result && <div className="alert ok" style={{ marginTop: 12 }} role="status">{act.result.moved} öğrenci taşındı. Gözetmenler ve oturma planı güncellendi.</div>}
      {act.error && <div style={{ marginTop: 12 }}>{full ? <div className="alert err" role="alert"><div><b>Hedef salona sığmıyor.</b> {act.error.message} Hiçbir kayıt değişmedi.</div></div> : <ErrorBox error={act.error} />}</div>}
    </Card>
  )
}

type SlotRoom = { room_id: number; room_code: string; room_type: string; building: string; exam_id: number; course_code: string; students: number }

/** Bir oturumdaki salon doluluğu: renkli çubuk (yeşil/sarı/kırmızı) ve atanan sınavlar. */
/** Kapasite panosunda bir sınava tıklanınca: o salonun krokisi + öğrenci listesi (koltuk, yoklamada geldi/gelmedi/işaretsiz). */
function ExamRoomDetail({ scheduleId, examId, roomId, title, onClose }: { scheduleId: number; examId: number; roomId: number; title: string; onClose: () => void }) {
  const { user } = useAuth()
  const rooms = useRooms()
  const seating = useAsync(async () => (await all<SeatRow>(`/schedules/${scheduleId}/seating?exam_id=${examId}`)).filter((r) => r.room_id === roomId), [scheduleId, examId, roomId])
  const att = useLive(async () => new Map((await all<Att>(`/exams/${examId}/attendance`)).map((a) => [a.student_id, a])), [examId], 5000)
  return (
    <div data-testid="exam-room-detail" id="exam-room-detail" tabIndex={-1}>
      <div className="row between" style={{ marginBottom: 8 }}><h2 style={{ margin: 0 }}>{title}</h2><button className="btn sm" onClick={onClose} aria-label="Detayı kapat">✕ Kapat</button></div>
      {seating.error?.status === 403
        ? <Card><Empty title="Bu sınav sizin göreviniz değil" hint="Gözetmenler yalnızca görevli oldukları salonların öğrenci listesini görebilir. Görevlerinizi Yoklama ekranında bulabilirsiniz." /></Card>
        : <Async s={seating} empty={(d) => d.length === 0}>{(rows) => <RoomPlan room={rooms.data?.find((r) => r.id === roomId)} rows={rows} att={att.data} canMark={hasRole(user!.roles, 'secretary', 'invigilator')} onMarked={() => void att.reload()} />}</Async>}
    </div>
  )
}

type Block = { id: number; room_id: number; room_code: string; building: string; slot_id: number; day: number; session: number; starts_at: string; reason: string; affected_exams: number }

/** Salon–oturum uygunluğu: salonu seçili oturumda kapat (bakım, başka etkinlik). Bir sonraki takvim/salon planı o salonu o oturumda kullanmaz. */
function RoomBlocks({ slot, rooms }: { slot: number; rooms: Room[] }) {
  const blocks = useAsync(async () => (await get<{ items: Block[] }>('/room-blocks')).items)
  const [room, setRoom] = useState(0)
  const [reason, setReason] = useState('')
  const add = useAction(async () => {
    const b = await post<Block>('/room-blocks', { room_id: room, slot_id: slot, reason })
    setReason('')
    blocks.reload()
    return b
  })
  const del = useAction(async (id: number) => { await request('DELETE', `/room-blocks/${id}`); blocks.reload() })
  return (
    <Card title="Kapalı salonlar (bakım / başka etkinlik)" aside={<Pill kind={blocks.data?.length ? 'warn' : ''}>{blocks.data?.length ?? 0} kayıt</Pill>}>
      <p className="small muted" style={{ marginTop: 0 }}>Seçili oturumda kullanılamayacak salonu işaretleyin. <b>Bir sonraki takvim üretiminde</b> salon planı o salonu o oturumda kullanmaz; mevcut yayındaki çizelge değişmez.</p>
      <div className="row noprint" style={{ alignItems: 'flex-end' }}>
        <Field label="Salon" id="blk-room"><select id="blk-room" value={room} onChange={(e) => setRoom(Number(e.target.value))} style={{ width: 'auto' }}><option value={0}>Salon seçin…</option>{rooms.map((r) => <option key={r.id} value={r.id}>{r.code} · {r.building}</option>)}</select></Field>
        <Field label="Gerekçe (isteğe bağlı)" id="blk-reason"><input id="blk-reason" className="input" maxLength={200} value={reason} onChange={(e) => setReason(e.target.value)} style={{ width: 240 }} /></Field>
        <button className="btn" disabled={!room || !slot || add.busy} onClick={() => void add.run()} data-testid="block-add">Seçili oturumda kapat</button>
      </div>
      {add.error && <div style={{ marginTop: 8 }}><ErrorBox error={add.error} /></div>}
      {add.result && add.result.affected_exams > 0 && <div className="alert warn" role="status" style={{ marginTop: 8 }}><div className="small"><b>{add.result.room_code}</b> yayındaki çizelgede bu oturumda {add.result.affected_exams} sınava ev sahipliği yapıyor; yeni bir takvim üretilene kadar o plan geçerlidir.</div></div>}
      {del.error && <ErrorBox error={del.error} />}
      <Async s={blocks} empty={(d) => d.length === 0}>
        {(list) => (
          <div className="tablewrap" style={{ marginTop: 10 }}><table data-testid="block-list">
            <thead><tr><th>Salon</th><th>Oturum</th><th>Gerekçe</th><th /></tr></thead>
            <tbody>{list.map((b) => (
              <tr key={b.id}><td><b>{b.room_code}</b> <span className="muted small">{b.building}</span></td><td>{b.day + 1}. gün · {b.session + 1}. oturum <span className="muted small">{fmtDateTime(b.starts_at)}</span></td><td className="small">{b.reason || '—'}{b.affected_exams > 0 && <> <Pill kind="warn">{b.affected_exams} sınav yerleşik</Pill></>}</td><td><button className="btn sm" disabled={del.busy} onClick={() => void del.run(b.id)} aria-label={`${b.room_code} kapatmasını kaldır`}>Kaldır</button></td></tr>
            ))}</tbody>
          </table></div>
        )}
      </Async>
    </Card>
  )
}

function Capacity() {
  const { user } = useAuth()
  const sch = useSchedules()
  const rooms = useRooms()
  const slots = useAsync(async () => (sch.current ? ((await get<{ items: { id: number; day: number; session: number; starts_at: string }[] }>(`/periods/${sch.current.period_id}/slots`)).items) : []), [sch.current?.period_id])
  const [slot, setSlot] = useState(0)
  const cur = slot || slots.data?.[0]?.id || 0
  const items = useAsync(async () => (sch.sel && cur ? (await get<{ items: SlotRoom[] }>(`/schedules/${sch.sel}/rooms?slot_id=${cur}`)).items : []), [sch.sel, cur])
  const [type, setType] = useState('')
  const [pick, setPick] = useState<{ exam: number; room: number; title: string } | null>(null)
  const cards = useMemo(() => {
    const m = new Map<number, { room: Room | undefined; list: SlotRoom[]; total: number }>()
    items.data?.forEach((x) => {
      const e = m.get(x.room_id) ?? { room: rooms.data?.find((r) => r.id === x.room_id), list: [], total: 0 }
      e.list.push(x); e.total += x.students; m.set(x.room_id, e)
    })
    return [...m.entries()].map(([id, v]) => ({ id, ...v, pct: v.room ? (v.total / v.room.capacity) * 100 : 0 })).filter((c) => !type || c.room?.type === type).sort((a, b) => b.pct - a.pct)
  }, [items.data, rooms.data, type])
  const used = cards.length
  const assigned = cards.reduce((a, c) => a + c.total, 0)
  const capUsed = cards.reduce((a, c) => a + (c.room?.capacity ?? 0), 0)
  const critical = cards.filter((c) => c.pct > 90).length
  const free = (rooms.data?.length ?? 0) - (items.data ? new Set(items.data.map((x) => x.room_id)).size : 0)
  const tone = (p: number) => (p > 90 ? 'err' : p > 70 ? 'warn' : 'ok')
  return (
    <>
      <div className="row noprint">
        <ScheduleSelect s={sch} />
        <select aria-label="Oturum" value={cur} onChange={(e) => setSlot(Number(e.target.value))} style={{ width: 'auto' }}>{slots.data?.map((s) => <option key={s.id} value={s.id}>{s.day + 1}. gün · {s.session + 1}. oturum · {new Date(s.starts_at).toLocaleString('tr-TR', { timeZone: 'Europe/Istanbul', day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' })}</option>)}</select>
        <select aria-label="Salon türü" value={type} onChange={(e) => setType(e.target.value)} style={{ width: 'auto' }}><option value="">Tüm türler</option><option value="classic">Derslik</option><option value="lab">Laboratuvar</option></select>
      </div>
      <div className="grid g4">
        <StatCard icon="building" tone="info" label="Kullanılan salon" value={used} note={`${Math.max(0, free)} salon bu oturumda boş`} />
        <StatCard icon="users" tone="info" label="Atanan öğrenci" value={assigned.toLocaleString('tr-TR')} note={`${capUsed.toLocaleString('tr-TR')} koltuk kapasitesi`} />
        <StatCard icon="seat" tone={tone(capUsed ? (assigned / capUsed) * 100 : 0)} label="Genel doluluk" value={`%${capUsed ? Math.round((assigned / capUsed) * 100) : 0}`} progress={capUsed ? (assigned / capUsed) * 100 : 0} />
        <StatCard icon="alert" tone={critical ? 'err' : 'ok'} label="Kritik doluluk (>%90)" value={critical} note={critical ? 'yoğun salonlar kırmızı' : 'tüm salonlar güvenli'} />
      </div>
      {pick && sch.sel && <ExamRoomDetail scheduleId={sch.sel} examId={pick.exam} roomId={pick.room} title={pick.title} onClose={() => setPick(null)} />}
      <p className="small muted noprint">Bir dersin koduna tıklayın: o sınavın salon krokisi ve öğrenci listesi (yoklamada geldi / gelmedi) açılır.</p>
      {hasRole(user!.roles, 'secretary') && <RoomBlocks slot={cur} rooms={rooms.data ?? []} />}
      <Async s={items} empty={() => cards.length === 0}>
        {() => (
          <div className="grid g3">
            {cards.map((c) => (
              <section className="card roomcard" key={c.id}>
                <div className="row between"><div><h2>{c.room?.code ?? `#${c.id}`}</h2><div className="small muted">{c.room?.building} · {c.room?.type === 'lab' ? 'Laboratuvar' : 'Derslik'}</div></div><Status tone={tone(c.pct)}>%{Math.round(c.pct)} dolu</Status></div>
                <div className="small">{c.list.map((x) => <div key={x.exam_id}><button className="linkbtn" aria-pressed={pick?.exam === x.exam_id && pick.room === c.id} onClick={() => { setPick({ exam: x.exam_id, room: c.id, title: `${x.course_code} · ${c.room?.code ?? c.id}` }); setTimeout(() => document.getElementById('exam-room-detail')?.scrollIntoView({ behavior: 'smooth', block: 'start' }), 80) }} title="Salon krokisi ve öğrenci listesini aç">{x.course_code}</button> <span className="muted">({x.students} öğrenci)</span></div>)}</div>
                <Bar pct={c.pct} label={`${c.room?.code ?? c.id} doluluk`} />
                <div className="meta"><div><b>{c.room?.capacity ?? '—'}</b><span className="small muted">Kapasite</span></div><div><b>{c.total}</b><span className="small muted">Atanan</span></div><div><b>{(c.room?.capacity ?? 0) - c.total}</b><span className="small muted">Boş</span></div></div>
              </section>
            ))}
          </div>
        )}
      </Async>
    </>
  )
}
void Field
