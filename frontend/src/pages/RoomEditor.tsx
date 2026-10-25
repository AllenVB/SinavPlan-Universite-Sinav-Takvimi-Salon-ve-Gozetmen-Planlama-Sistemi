import { useEffect, useMemo, useState } from 'react'
import { get, request } from '../api'
import { DOOR_SIDES, RoomGrid } from '../roomgrid'
import { Banner, Card, ErrorBox, Field, useAction } from '../ui'
import type { Room } from './common'

type Cell = 'on' | 'off' | 'acc'
type Detail = { seats: { row: number; col: number; active: boolean; accessible: boolean }[] }
const NEXT: Record<Cell, Cell> = { on: 'off', off: 'acc', acc: 'on' }
const rowLetter = (r: number) => String.fromCharCode(65 + (r % 26))

/** Sekreter: salon ekle / düzenle / sil. Koltuğa tıkla: açık → kullanım dışı → erişilebilir → açık. */
export function RoomEditor({ room, onDone }: { room: Room | null; onDone: (changed: boolean) => void }) {
  const [code, setCode] = useState(room?.code ?? '')
  const [building, setBuilding] = useState(room?.building ?? 'A')
  const [floor, setFloor] = useState(room?.floor ?? 0)
  const [type, setType] = useState(room?.type ?? 'classic')
  const [rows, setRows] = useState(room?.rows ?? 5)
  const [cols, setCols] = useState(room?.cols ?? 8)
  const [doorSide, setDoorSide] = useState(room?.door_side ?? 'front')
  const [doorPos, setDoorPos] = useState(room?.door_pos ?? 0)
  const [cells, setCells] = useState<Map<string, Cell>>(new Map())
  const [loaded, setLoaded] = useState(!room)

  useEffect(() => {
    if (!room) return
    void get<Detail>(`/rooms/${room.id}`).then((d) => {
      const m = new Map<string, Cell>()
      d.seats.forEach((s) => { if (!s.active) m.set(`${s.row}:${s.col}`, 'off'); else if (s.accessible) m.set(`${s.row}:${s.col}`, 'acc') })
      setCells(m)
      setLoaded(true)
    })
  }, [room])

  const wallLen = doorSide === 'left' || doorSide === 'right' ? rows : cols
  const pos = Math.min(doorPos, wallLen - 1)
  const counts = useMemo(() => {
    let off = 0, acc = 0
    cells.forEach((v, k) => { const [r, c] = k.split(':').map(Number); if (r < rows && c < cols) { if (v === 'off') off++; else acc++ } })
    return { off, acc, active: rows * cols - off }
  }, [cells, rows, cols])

  const body = () => {
    const inactive: [number, number][] = [], accessible: [number, number][] = []
    cells.forEach((v, k) => { const [r, c] = k.split(':').map(Number); if (r < rows && c < cols) (v === 'off' ? inactive : accessible).push([r, c]) })
    return { code: code.trim(), building: building.trim(), floor, type, rows, cols, door_side: doorSide, door_pos: pos, inactive_seats: inactive, accessible_seats: accessible }
  }
  const save = useAction(async () => {
    await request(room ? 'PUT' : 'POST', room ? `/rooms/${room.id}` : '/rooms', body())
    onDone(true)
  })
  const del = useAction(async () => { await request('DELETE', `/rooms/${room!.id}`); onDone(true) })
  const conflict = save.error?.status === 409

  return (
    <Card title={room ? `${room.code} salonunu düzenle` : 'Yeni salon'} aside={<button className="btn sm" onClick={() => onDone(false)}>Vazgeç</button>}>
      <div className="row" style={{ alignItems: 'flex-end' }}>
        <Field label="Kod" id="re-code" error={save.error?.fields?.code}><input id="re-code" className="input" style={{ width: 110 }} value={code} onChange={(e) => setCode(e.target.value)} maxLength={20} /></Field>
        <Field label="Bina" id="re-b" error={save.error?.fields?.building}><input id="re-b" className="input" style={{ width: 80 }} value={building} onChange={(e) => setBuilding(e.target.value)} /></Field>
        <Field label="Kat" id="re-f"><input id="re-f" className="input" type="number" style={{ width: 70 }} value={floor} onChange={(e) => setFloor(Number(e.target.value))} /></Field>
        <Field label="Tür" id="re-t"><select id="re-t" value={type} onChange={(e) => setType(e.target.value)}><option value="classic">Derslik</option><option value="lab">Laboratuvar</option></select></Field>
        <Field label="Sıra" id="re-r" error={save.error?.fields?.rows}><input id="re-r" className="input" type="number" min={1} max={40} style={{ width: 70 }} value={rows} onChange={(e) => setRows(Math.max(1, Math.min(40, Number(e.target.value))))} /></Field>
        <Field label="Sütun" id="re-c" error={save.error?.fields?.cols}><input id="re-c" className="input" type="number" min={1} max={40} style={{ width: 70 }} value={cols} onChange={(e) => setCols(Math.max(1, Math.min(40, Number(e.target.value))))} /></Field>
      </div>
      <div className="row" style={{ alignItems: 'flex-end', marginTop: 8 }}>
        <Field label="Kapı hangi duvarda" id="re-ds"><select id="re-ds" value={doorSide} onChange={(e) => setDoorSide(e.target.value)}>{DOOR_SIDES.map(([k, l]) => <option key={k} value={k}>{l}</option>)}</select></Field>
        <Field label={doorSide === 'left' || doorSide === 'right' ? 'Kapı kaçıncı sırada' : 'Kapı kaçıncı sütunda'} id="re-dp" error={save.error?.fields?.door_pos}>
          <select id="re-dp" value={pos} onChange={(e) => setDoorPos(Number(e.target.value))}>{Array.from({ length: wallLen }, (_, i) => <option key={i} value={i}>{i + 1}</option>)}</select>
        </Field>
        <span className="small muted">{counts.active} kullanılabilir koltuk · {counts.acc} erişilebilir · {counts.off} kullanım dışı</span>
      </div>
      <p className="small muted" style={{ margin: '10px 0 6px' }}>Koltuğa tıklayın: <b>açık</b> → <b>kullanım dışı</b> → <b>erişilebilir</b> → açık. Erişilebilir koltukları kapıya yakın ve birbirine komşu olmayacak biçimde işaretlemeniz önerilir.</p>
      {loaded && (
        <RoomGrid rows={rows} cols={cols} doorSide={doorSide} doorPos={pos} label="Salon düzeni düzenleyici">
          {(r, c) => {
            const v = cells.get(`${r}:${c}`) ?? 'on'
            return (
              <button type="button" className={`seat ${v === 'off' ? 'off' : ''} ${v === 'acc' ? 'acc' : ''}`} style={{ cursor: 'pointer', width: '100%' }} aria-label={`${rowLetter(r)}${c + 1}: ${v === 'off' ? 'kullanım dışı' : v === 'acc' ? 'erişilebilir' : 'açık'}`}
                onClick={() => setCells((m) => { const n = new Map(m); const nx = NEXT[m.get(`${r}:${c}`) ?? 'on']; if (nx === 'on') n.delete(`${r}:${c}`); else n.set(`${r}:${c}`, nx); return n })}>
                {v === 'off' ? '' : `${rowLetter(r)}${c + 1}`}
              </button>
            )
          }}
        </RoomGrid>
      )}
      {save.error && <div style={{ marginTop: 12 }}>{conflict ? <Banner tone="err" icon="alert" title="Bu salon kodu zaten var">Başka bir kod seçin.</Banner> : <ErrorBox error={save.error} />}</div>}
      {del.error && <div style={{ marginTop: 12 }}><ErrorBox error={del.error} /></div>}
      <div className="row" style={{ marginTop: 14 }}>
        <button className="btn primary" disabled={save.busy || !code.trim() || !building.trim() || counts.active < 1} onClick={() => void save.run()}>{save.busy ? 'Kaydediliyor…' : room ? 'Değişiklikleri kaydet' : 'Salonu oluştur'}</button>
        {room && <button className="btn danger" disabled={del.busy} onClick={() => confirm(`${room.code} salonu silinsin mi? Çizelgede kullanılan salon silinemez.`) && void del.run()}>{del.busy ? 'Siliniyor…' : 'Salonu sil'}</button>}
      </div>
      <p className="small muted" style={{ marginTop: 8 }}>Çizelgede kullanılan bir salonun sıra/sütun düzeni değiştirilemez; sunucu bunu reddeder ve hiçbir şey değişmez.</p>
    </Card>
  )
}
