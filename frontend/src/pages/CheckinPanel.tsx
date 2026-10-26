import { useState } from 'react'
import { get, post } from '../api'
import { Icon } from '../icons'
import { LiveBadge, useLive } from '../live'
import { QR } from '../qr'
import { Banner, Card, ErrorBox, StatCard, useAction } from '../ui'

type Session = { id: number; room_code: string; code: string; code_expires_at: string; ends_at: string; present: number; expected: number; token: string; token_expires_at: string }
type Duty = { slot_id: number; room_id: number; room_code: string }

const grouped = (c: string) => `${c.slice(0, 3)} ${c.slice(3)}`

/** Gözetmen: "Yeni QR oluştur" → dönen QR + yedek 6 haneli kod; "Kod üret" → yalnız 6 haneli kod (QR'sız). Öğrenci kendi hesabıyla okutur/girer. */
export function CheckinPanel({ duty, onAttendance }: { duty: Duty; onAttendance: () => void }) {
  const [id, setId] = useState<number | null>(null)
  const [codeOnly, setCodeOnly] = useState(false)
  const start = useAction(async (onlyCode: boolean) => {
    setCodeOnly(onlyCode)
    const s = await post<Session>('/checkin-sessions', { slot_id: duty.slot_id, room_id: duty.room_id })
    setId(s.id)
    return s
  })
  const rotate = useAction(async () => post<Session>(`/checkin-sessions/${id}/code`, {}).then((s) => { void live.reload(); return s }))
  const close = useAction(async () => { await post(`/checkin-sessions/${id}/close`, {}); setId(null) })
  // QR jetonu 30 sn'de bir değişir; ekran 4 sn'de bir taze jeton + sayaç ister
  const live = useLive(async () => {
    const s = await get<Session>(`/checkin-sessions/${id}`)
    onAttendance()
    return s
  }, [id], 4000, id !== null)
  const s = live.data
  const ended = id !== null && live.error && (live.error.code === 'checkin_closed' || live.error.status === 404)

  return (
    <Card title="Yoklama QR'ı (öğrenciler kendi hesabıyla okutur)" aside={id === null || ended ? (
      <div className="row">
        <button className="btn primary" disabled={start.busy} onClick={() => void start.run(false)}><Icon name="qr" size={16} /> {start.busy ? 'Oluşturuluyor…' : 'Yeni QR oluştur'}</button>
        <button className="btn" disabled={start.busy} onClick={() => void start.run(true)} data-testid="gen-code">Kod üret (6 haneli)</button>
      </div>
    ) : (
      <div className="row"><button className="btn" disabled={start.busy} onClick={() => setCodeOnly(!codeOnly)}>{codeOnly ? 'QR göster' : 'Yalnız kod göster'}</button><button className="btn" disabled={start.busy} onClick={() => void start.run(codeOnly)}>{codeOnly ? 'Kodu baştan oluştur' : 'QR\'ı baştan oluştur'}</button><button className="btn danger" disabled={close.busy} onClick={() => void close.run()}>Oturumu kapat</button></div>
    )}>
      {id === null && !start.error && <p className="small muted">Sınav başlamadan 30 dk öncesinden bitişinden 15 dk sonrasına kadar açılabilir. "Yeni QR oluştur" der demez salon ekranında QR ve yedek kod görünür; "Kod üret" yalnız 6 haneli kodu verir (tahtaya yazın, öğrenciler kodu girer); öğrenciler giriş yapıp kendi telefonlarıyla QR'ı okutunca otomatik <b>geldi</b> yazılır.</p>}
      {start.error && <ErrorBox error={start.error} retry={() => void start.run(codeOnly)} />}
      {ended && <Banner tone="warn" icon="clock" title="Yoklama oturumu sona erdi">Gerekirse "Yeni QR oluştur" ile yenisini açın.</Banner>}
      {id !== null && !ended && s && (
        <div className={codeOnly ? 'grid' : 'grid g2'} style={{ alignItems: 'center' }}>
          {!codeOnly && <div style={{ textAlign: 'center' }} data-testid="checkin-qr">
            <QR value={s.token} size={260} />
            <p className="small muted" style={{ marginTop: 8 }}>QR her 30 saniyede kendini yeniler; ekran görüntüsü işe yaramaz. <LiveBadge updatedAt={live.updatedAt} error={live.error} /></p>
          </div>}
          <div>
            <div className="caps">{codeOnly ? 'Yoklama kodu' : 'QR okunamazsa — yedek kod'}</div>
            <div className="mono" data-testid="checkin-code" style={{ fontSize: '3rem', fontWeight: 700, letterSpacing: '.15em', margin: '6px 0' }}>{grouped(s.code)}</div>
            <p className="small muted">Kodu salona söyleyin veya tahtaya yazın. {new Date(s.code_expires_at).toLocaleTimeString('tr-TR', { hour: '2-digit', minute: '2-digit', timeZone: 'Europe/Istanbul' })}'e kadar geçerli; yeni kod üretince eskisi geçersiz olur.</p>
            <button className="btn" disabled={rotate.busy} onClick={() => void rotate.run()}><Icon name="refresh" size={16} /> Yeni kod üret</button>
            {rotate.error && <ErrorBox error={rotate.error} />}
            <div style={{ marginTop: 14 }}>
              <StatCard icon="checkCircle" tone="ok" label={`${s.room_code} salonunda geldi`} value={`${s.present} / ${s.expected}`} progress={s.expected ? (s.present / s.expected) * 100 : 0} />
            </div>
          </div>
        </div>
      )}
    </Card>
  )
}
