import { useState } from 'react'
import { download, fmtDate, fmtTime, get, post } from '../api'
import { QR } from '../qr'
import { Async, Card, Empty, ErrorBox, Pill, useAction, useAsync } from '../ui'

type Exam = { exam_id: number; course_code: string; course_name: string; starts_at: string; ends_at: string; duration_minutes: number; room_code: string | null; building: string | null; seat_row: number | null; seat_col: number | null }
type CardRes = { qr_token: string; expires_at: string }
const seat = (e: Exam) => (e.seat_row === null || e.seat_col === null ? '—' : `${String.fromCharCode(65 + e.seat_row)}-${e.seat_col + 1}`)

export function MyExams() {
  const list = useAsync(async () => (await get<{ items: Exam[] }>('/me/exams')).items)
  const [open, setOpen] = useState<Exam | null>(null)
  const dl = useAction(() => download('/me/exams.ics', 'sinavlarim.ics'))
  const card = useAction(async (e: Exam) => post<CardRes>(`/me/exams/${e.exam_id}/card`))
  const show = (e: Exam) => {
    setOpen(e)
    card.reset()
    void card.run(e)
  }
  return (
    <>
      <div className="head">
        <div><div className="caps">Öğrenci</div><h1>Sınav programım</h1></div>
        <div className="actions"><button className="btn" onClick={() => void dl.run()} disabled={dl.busy}>Takvimime ekle (.ics)</button><button className="btn" onClick={() => window.print()}>Yazdır / PDF</button></div>
      </div>
      {dl.error && <ErrorBox error={dl.error} />}
      <Async s={list} empty={(d) => d.length === 0}>
        {(items) => items.length === 0 ? <Card><Empty title="Kayıtlı sınav bulunamadı" hint="Takvim yayınlandığında sınavlarınız burada görünür." /></Card> : (
          <>
            <div className="grid g2">
              {items.map((e) => (
                <section className="card" key={e.exam_id}>
                  <div className="ch"><h2>{e.course_code} — {e.course_name}</h2><Pill kind="info">Koltuk {seat(e)}</Pill></div>
                  <p>{fmtDate(e.starts_at)} · {fmtTime(e.starts_at)}–{fmtTime(e.ends_at)} <span className="muted">({e.duration_minutes} dk)</span></p>
                  <p className="muted">{e.room_code ? `${e.room_code} · ${e.building}` : 'Salon henüz atanmadı'}</p>
                  <div style={{ marginTop: 12 }}><button className="btn primary" onClick={() => show(e)}>Sınav kartı</button></div>
                </section>
              ))}
            </div>
            {open && (
              <article className="examcard" aria-label="Sınav kartı">
                <div className="hd"><span>Dijital sınav giriş kartı</span><span>{open.course_code}</span></div>
                <div className="bd">
                  <h2>{open.course_name}</h2>
                  <div className="grid g2">
                    <div className="qrbox"><div className="caps">Salon</div><b style={{ fontSize: '1.25rem' }}>{open.room_code ?? '—'}</b><div className="small muted">{open.building}</div></div>
                    <div className="seatbig"><span className="caps">Koltuk</span><b>{seat(open)}</b></div>
                  </div>
                  <p className="muted small">{fmtDate(open.starts_at)} · {fmtTime(open.starts_at)}</p>
                  {card.busy && <div className="skel" />}
                  {card.error && (card.error.code === 'excused' ? <div className="alert warn"><div><b>Bu sınav için onaylanmış mazeretiniz var;</b> sınav kartı verilmez.</div></div> : <ErrorBox error={card.error} retry={() => void card.run(open)} />)}
                  {card.result && (
                    <>
                      <div className="caps">Tek kullanımlık QR kodu</div>
                      <QR value={card.result.qr_token} />
                      <details><summary className="small muted">Jeton metni</summary><div className="qrbox mono" data-testid="qr-token">{card.result.qr_token}</div></details>
                      <p className="small muted">Bu jetonu yoklama tabletine okutun. Tek kullanımlıktır; yeni kart almak eskisini geçersiz kılar. Son kullanma: {fmtTime(card.result.expires_at)}.</p>
                    </>
                  )}
                </div>
              </article>
            )}
          </>
        )}
      </Async>
    </>
  )
}
