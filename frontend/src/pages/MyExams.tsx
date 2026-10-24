import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiError, download, fmtDate, fmtTime, get, post } from '../api'
import { Icon } from '../icons'
import { useNow } from '../live'
import { QR } from '../qr'
import { Async, Banner, Card, Dot, Empty, ErrorBox, Ring, Skeleton, Status, useAction, useAsync } from '../ui'

type Exam = { exam_id: number; course_code: string; course_name: string; starts_at: string; ends_at: string; duration_minutes: number; room_code: string | null; building: string | null; seat_row: number | null; seat_col: number | null }
type CardRes = { qr_token: string; expires_at: string; ttl_seconds: number }
type SeatMap = { room_code: string; building: string; rows: number; cols: number; seats: { row: number; col: number; active: boolean; accessible: boolean }[]; mine_row: number; mine_col: number }
const seat = (e: Pick<Exam, 'seat_row' | 'seat_col'>) => (e.seat_row === null || e.seat_col === null ? '—' : `${String.fromCharCode(65 + (e.seat_row % 26))}-${e.seat_col + 1}`)
const dur = (ms: number) => {
  const s = Math.max(0, Math.floor(ms / 1000)), d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60)
  return d ? `${d} g ${h} sa` : h ? `${h} sa ${m} dk` : `${m} dk ${s % 60} sn`
}

export function MyExams() {
  const list = useAsync(async () => (await get<{ items: Exam[] }>('/me/exams')).items.sort((a, b) => a.starts_at.localeCompare(b.starts_at)))
  const [open, setOpen] = useState<Exam | null>(null)
  const dl = useAction(() => download('/me/exams.ics', 'sinavlarim.ics'))
  const now = useNow()
  const next = list.data?.find((e) => new Date(e.ends_at).getTime() > now)
  const state = (e: Exam): ['Bitti' | 'Sıradaki' | 'Planlandı', 'mute' | 'info' | 'ok'] => (new Date(e.ends_at).getTime() <= now ? ['Bitti', 'mute'] : next?.exam_id === e.exam_id ? ['Sıradaki', 'info'] : ['Planlandı', 'ok'])

  return (
    <>
      <div className="head">
        <div><div className="caps">Öğrenci</div><h1>Sınav programım</h1></div>
        <div className="actions noprint"><button className="btn" onClick={() => void dl.run()} disabled={dl.busy}><Icon name="download" size={16} /> Takvimime ekle (.ics)</button><button className="btn" onClick={() => window.print()}>Yazdır / PDF</button></div>
      </div>
      {dl.error && <ErrorBox error={dl.error} />}
      <Async s={list} empty={(d) => d.length === 0}>
        {(items) => items.length === 0 ? <Card><Empty title="Kayıtlı sınav bulunamadı" hint="Takvim yayınlandığında sınavlarınız burada görünür." /></Card> : (
          <>
            {next ? (
              <div className="hero">
                <div>
                  <div className="caps">Sıradaki sınav</div>
                  <h2 style={{ color: '#fff', fontSize: '1.5rem', lineHeight: '2rem' }}>{next.course_code} — {next.course_name}</h2>
                  <div className="muted">{fmtDate(next.starts_at)} · {fmtTime(next.starts_at)}–{fmtTime(next.ends_at)} · {next.room_code ? `${next.room_code} (${next.building})` : 'salon atanmadı'}</div>
                </div>
                <div style={{ textAlign: 'right' }}>
                  <div className="caps">{new Date(next.starts_at).getTime() > now ? 'Başlamasına' : 'Sınav sürüyor — kalan'}</div>
                  <div className="big" data-testid="countdown">{dur((new Date(next.starts_at).getTime() > now ? new Date(next.starts_at).getTime() : new Date(next.ends_at).getTime()) - now)}</div>
                  <div className="row" style={{ justifyContent: 'flex-end', marginTop: 8 }}><span className="seatbadge" style={{ background: '#fff', color: 'var(--primary)' }}>{seat(next)}</span><button className="btn sm" onClick={() => setOpen(next)}>Sınav kartım</button></div>
                </div>
              </div>
            ) : <Banner tone="ok" icon="checkCircle" title="Tüm sınavlarınız tamamlandı">Başarılar dileriz.</Banner>}

            <div className="grid g2">
              {items.map((e) => {
                const [label, tone] = state(e)
                return (
                  <section className="card" key={e.exam_id} style={next?.exam_id === e.exam_id ? { borderColor: 'var(--primary)' } : undefined}>
                    <div className="ch"><h2>{e.course_code} — {e.course_name}</h2><Status tone={tone}>{label}</Status></div>
                    <p>{fmtDate(e.starts_at)} · {fmtTime(e.starts_at)}–{fmtTime(e.ends_at)} <span className="muted">({e.duration_minutes} dk)</span></p>
                    <div className="row" style={{ marginTop: 8 }}>
                      <span className="seatbadge">{seat(e)}</span>
                      <span className="muted">{e.room_code ? `${e.room_code} · ${e.building}` : 'Salon henüz atanmadı'}</span>
                    </div>
                    <div className="row noprint" style={{ marginTop: 12 }}><button className="btn primary" disabled={label === 'Bitti'} onClick={() => setOpen(e)}><Icon name="qr" size={16} /> Sınav kartı</button></div>
                  </section>
                )
              })}
            </div>
            {open && <ExamCard key={open.exam_id} exam={open} onClose={() => setOpen(null)} />}
          </>
        )}
      </Async>
    </>
  )
}

/** Dinamik sınav kartı: QR kısa ömürlüdür ve ömrünün %60'ında kendiliğinden yenilenir (eski kod iptal olur). */
function ExamCard({ exam, onClose }: { exam: Exam; onClose: () => void }) {
  const [card, setCard] = useState<{ token: string; expires: number; ttl: number; fetched: number } | null>(null)
  const [error, setError] = useState<ApiError | null>(null)
  const [excused, setExcused] = useState(false)
  const [refreshing, setRefreshing] = useState(false)
  const [count, setCount] = useState(0)
  const now = useNow(500)
  const busy = useRef(false)

  const refresh = useCallback(async () => {
    if (busy.current) return
    busy.current = true
    setRefreshing(true)
    try {
      const r = await post<CardRes>(`/me/exams/${exam.exam_id}/card`)
      setCard({ token: r.qr_token, expires: new Date(r.expires_at).getTime(), ttl: r.ttl_seconds, fetched: Date.now() })
      setCount((c) => c + 1)
      setError(null)
    } catch (e) {
      const err = e instanceof ApiError ? e : new ApiError(0, 'error', String(e))
      if (err.code === 'excused') setExcused(true)
      setError(err)
    } finally {
      busy.current = false
      setRefreshing(false)
    }
  }, [exam.exam_id])

  useEffect(() => { void refresh() }, [refresh])
  // ömrün %60'ında yenile (sekme gizliyse görününce hemen)
  useEffect(() => {
    if (!card || excused) return
    const due = card.fetched + card.ttl * 600
    if (Date.now() >= due && document.visibilityState === 'visible') { void refresh(); return }
    const t = setTimeout(() => document.visibilityState === 'visible' && void refresh(), Math.max(1000, due - Date.now()))
    const onVis = () => document.visibilityState === 'visible' && Date.now() >= due && void refresh()
    document.addEventListener('visibilitychange', onVis)
    return () => { clearTimeout(t); document.removeEventListener('visibilitychange', onVis) }
  }, [card, excused, refresh])

  const sm = useAsync(() => get<SeatMap>(`/me/exams/${exam.exam_id}/seatmap`).catch(() => null), [exam.exam_id])
  const left = card ? card.expires - now : 0
  const expired = !!card && left <= 0
  const nextIn = card ? Math.max(0, card.fetched + card.ttl * 600 - now) : 0

  return (
    <article className="examcard livecard" aria-label="Dijital sınav kartı">
      <div className="hd"><span>Dijital sınav giriş kartı</span><span className="row"><Dot tone={error && !excused ? 'err' : expired ? 'warn' : 'ok'} pulse={!error && !expired} />{excused ? 'Mazeretli' : expired ? 'Süresi doldu' : error ? 'Yenilenemedi' : 'Canlı kod'}</span></div>
      <div className="bd">
        <div className="row between"><h2>{exam.course_name}</h2><button className="btn sm noprint" onClick={onClose}>Kapat</button></div>
        <div className="grid g2">
          <div className="qrbox"><div className="caps">Salon</div><b style={{ fontSize: '1.25rem' }}>{exam.room_code ?? '—'}</b><div className="small muted">{exam.building}</div><div className="small muted" style={{ marginTop: 6 }}>{fmtDate(exam.starts_at)} · {fmtTime(exam.starts_at)}</div></div>
          <div className="seatbig"><span className="caps">Koltuk</span><b>{seat(exam)}</b></div>
        </div>

        {excused ? <Banner tone="warn" icon="alert" title="Bu sınav için onaylanmış mazeretiniz var">Sınav kartı verilmez.</Banner> : (
          <>
            {!card && !error && <Skeleton rows={3} />}
            {error && !excused && <Banner tone={card ? 'warn' : 'err'} icon="wifiOff" title={card ? 'Kod yenilenemedi — bağlantıyı kontrol edin' : 'Sınav kartı alınamadı'} actions={<button className="btn sm" onClick={() => void refresh()}>Tekrar dene</button>}>{error.message}</Banner>}
            {card && (
              <div style={{ position: 'relative' }}>
                <div style={{ opacity: expired ? 0.25 : 1, transition: 'opacity .3s' }} data-expired={expired}><QR value={card.token} /></div>
                {expired && <div className="alert err" style={{ marginTop: 10 }}><div>Bu kodun süresi doldu; görevliye göstermeyin. <button className="btn sm" onClick={() => void refresh()}>Yenile</button></div></div>}
                <div className="row between" style={{ marginTop: 12 }}>
                  <div className="row">
                    <Ring fraction={card.ttl ? left / (card.ttl * 1000) : 0}>{Math.max(0, Math.ceil(left / 1000))}</Ring>
                    <div className="small"><b>{expired ? 'Süresi doldu' : 'Kod geçerli'}</b><div className="muted">{refreshing ? 'Yenileniyor…' : `Sonraki yenileme: ${Math.ceil(nextIn / 1000)} sn`}</div></div>
                  </div>
                  <div className="small muted" style={{ textAlign: 'right' }}>Yenileme sayısı: <b data-testid="qr-rotations">{count}</b><div>Her yenileme eski kodu iptal eder.</div></div>
                </div>
                <details style={{ marginTop: 8 }}><summary className="small muted">Jeton metni</summary><div className="qrbox mono" data-testid="qr-token">{card.token}</div></details>
              </div>
            )}
          </>
        )}

        <Card title="Koltuk krokisi">
          <Async s={sm}>{(m) => m ? <Croki m={m} /> : <Empty title="Kroki yok" hint="Salon planı henüz yayınlanmadı." />}</Async>
        </Card>
      </div>
    </article>
  )
}

function Croki({ m }: { m: SeatMap }) {
  const at = new Map(m.seats.map((s) => [`${s.row}:${s.col}`, s]))
  const cells = []
  for (let r = 0; r < m.rows; r++) for (let c = 0; c < m.cols; c++) {
    const s = at.get(`${r}:${c}`)
    const mine = r === m.mine_row && c === m.mine_col
    cells.push(<div key={`${r}:${c}`} className={`seat ${s && !s.active ? 'off' : ''} ${s?.accessible ? 'acc' : ''} ${mine ? 'taken mine' : ''}`} title={mine ? 'Sizin koltuğunuz' : undefined}>{s && !s.active ? '' : mine ? 'SİZ' : `${String.fromCharCode(65 + (r % 26))}${c + 1}`}</div>)
  }
  return (
    <>
      <div className="small muted" style={{ marginBottom: 6 }}>{m.room_code} · {m.building} — tahta / ön taraf yukarıda</div>
      <div className="seatmap" style={{ gridTemplateColumns: `repeat(${m.cols}, minmax(34px, 1fr))` }} role="img" aria-label={`Koltuğunuz ${String.fromCharCode(65 + (m.mine_row % 26))}-${m.mine_col + 1}`}>{cells}</div>
    </>
  )
}
