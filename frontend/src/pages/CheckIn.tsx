import { useState } from 'react'
import { post } from '../api'
import { Icon } from '../icons'
import { CameraScanner } from '../scan'
import { Banner, Card, ErrorBox, Field, useAction } from '../ui'

type Result = { exam_id: number; course_code: string; course_name: string; room_code: string; already_present: boolean; method: 'qr' | 'code' }

/** Öğrenci: gözetmenin gösterdiği QR'ı okut ya da 6 haneli yedek kodu gir → sınavda "var" sayıl. */
export function CheckIn() {
  const [cam, setCam] = useState(false)
  const [code, setCode] = useState('')
  const send = useAction(async (body: { token?: string; code?: string }) => {
    const r = await post<Result>('/me/checkin', body)
    setCam(false)
    setCode('')
    return r
  })
  const r = send.result
  return (
    <Card title="Sınava giriş — yoklamaya katıl" aside={<button className={`btn ${cam ? 'danger' : 'primary'}`} onClick={() => setCam(!cam)}><Icon name="camera" size={16} /> {cam ? 'Kamerayı kapat' : 'QR okut'}</button>}>
      <p className="small muted">Sınav salonunda gözetmenin ekranındaki QR'ı okutun. QR çalışmazsa gözetmenin söylediği <b>6 haneli kodu</b> girin. Başarılı olunca yoklamada otomatik <b>geldi</b> görünürsünüz.</p>
      {cam && <CameraScanner onCode={(t) => void send.run({ token: t })} onClose={() => setCam(false)} />}
      <div className="row" style={{ alignItems: 'flex-end' }}>
        <Field label="6 haneli kod" id="chk"><input id="chk" className="input mono" inputMode="numeric" autoComplete="one-time-code" maxLength={6} style={{ width: 160, fontSize: '1.4rem', letterSpacing: '.2em' }} value={code} onChange={(e) => setCode(e.target.value.replace(/\D/g, '').slice(0, 6))} placeholder="000000" /></Field>
        <button className="btn primary" disabled={send.busy || code.length !== 6} onClick={() => void send.run({ code })}>{send.busy ? 'Gönderiliyor…' : 'Kodu gönder'}</button>
      </div>
      {r && <div style={{ marginTop: 12 }}><Banner tone="ok" icon="checkCircle" title={r.already_present ? 'Yoklamanız zaten alınmıştı' : 'Yoklamanız alındı — sınavda var sayıldınız'}>{r.course_code} — {r.course_name} · {r.room_code} ({r.method === 'qr' ? 'QR ile' : 'kod ile'})</Banner></div>}
      {send.error && <div style={{ marginTop: 12 }}>{send.error.code === 'too_many_attempts' ? <Banner tone="err" icon="alert" title="Çok fazla hatalı deneme">{send.error.message}</Banner> : <ErrorBox error={send.error} />}</div>}
    </Card>
  )
}
