import type { CSSProperties, ReactNode } from 'react'

export type DoorSide = 'front' | 'back' | 'left' | 'right'
export const DOOR_SIDES: [DoorSide, string][] = [['front', 'Ön duvar (tahta tarafı)'], ['back', 'Arka duvar'], ['left', 'Sol duvar'], ['right', 'Sağ duvar']]
export const doorLabel = (side: string, pos: number) => `${DOOR_SIDES.find(([k]) => k === side)?.[1].split(' (')[0] ?? 'Ön duvar'}, ${side === 'left' || side === 'right' ? `${pos + 1}. sıra` : `${pos + 1}. sütun`}`

/**
 * Salon krokisi: koltuk ızgarasının etrafında dört duvar şeridi; kapı, bulunduğu duvarda KAPI işaretiyle gösterilir.
 * Satır 0 ön duvara (tahta), sütun 0 sol duvara bitişiktir.
 */
/** Tablo için kısa gösterim: "Ön · 1". */
export const doorShort = (side: string, pos: number) => `${{ front: 'Ön', back: 'Arka', left: 'Sol', right: 'Sağ' }[side] ?? 'Ön'} · ${pos + 1}`

export function RoomGrid({ rows, cols, doorSide, doorPos, minCell = 38, children, label }: { rows: number; cols: number; doorSide?: string; doorPos?: number; minCell?: number; children: (r: number, c: number) => ReactNode; label: string }) {
  const side = (doorSide || 'front') as DoorSide
  const pos = doorPos ?? 0
  const place = (row: number, col: number): CSSProperties => ({ gridRow: row, gridColumn: col })
  const wall = (s: DoorSide, key: string, style: CSSProperties, text: string) => (
    <div key={key} className={`wall ${s === 'left' || s === 'right' ? 'v' : ''}`} style={style}>{text}</div>
  )
  const door = (s: DoorSide) => {
    const style = s === 'front' ? place(1, pos + 2) : s === 'back' ? place(rows + 2, pos + 2) : s === 'left' ? place(pos + 2, 1) : place(pos + 2, cols + 2)
    return <div key="door" className={`doormark ${s === 'left' || s === 'right' ? 'v' : ''}`} style={style} title="Salon kapısı" data-testid="room-door"><span>KAPI</span></div>
  }
  const seats = []
  for (let r = 0; r < rows; r++) for (let c = 0; c < cols; c++) seats.push(<div key={`${r}:${c}`} style={{ ...place(r + 2, c + 2), display: 'grid' }}>{children(r, c)}</div>)
  return (
    <div
      className="roomgrid"
      style={{ gridTemplateColumns: `26px repeat(${cols}, minmax(${minCell}px, 1fr)) 26px`, gridTemplateRows: `26px repeat(${rows}, auto) 26px` }}
      role="img"
      aria-label={`${label}. Kapı: ${doorLabel(side, pos)}`}
    >
      {wall('front', 'wf', { gridRow: 1, gridColumn: `2 / ${cols + 2}` }, 'ÖN · TAHTA')}
      {wall('back', 'wb', { gridRow: rows + 2, gridColumn: `2 / ${cols + 2}` }, 'ARKA')}
      {wall('left', 'wl', { gridRow: `2 / ${rows + 2}`, gridColumn: 1 }, 'SOL')}
      {wall('right', 'wr', { gridRow: `2 / ${rows + 2}`, gridColumn: cols + 2 }, 'SAĞ')}
      {door(side)}
      {seats}
    </div>
  )
}
