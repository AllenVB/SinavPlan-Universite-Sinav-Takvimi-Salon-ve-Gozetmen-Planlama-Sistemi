import { useState } from 'react'
import { request } from '../api'
import { Card, ErrorBox, Pill, Tabs, useAction } from '../ui'

type Kind = 'students' | 'courses' | 'sections' | 'enrollments'
type Report = { kind: string; rows: number; valid: number; created: number; updated: number; unchanged: number; errors: { line: number; field?: string; message: string }[]; errors_truncated?: boolean }
const HEAD: Record<Kind, string> = {
  students: 'student_no,full_name,department_code,disability',
  courses: 'code,name,department_code,year,requires_lab',
  sections: 'course_code,section_code,lecturer_email',
  enrollments: 'student_no,course_code,section_code',
}
const NAME: Record<Kind, string> = { students: 'Öğrenciler', courses: 'Dersler', sections: 'Şubeler', enrollments: 'Ders kayıtları' }

export function Imports() {
  const [kind, setKind] = useState<Kind>('students')
  const [text, setText] = useState('')
  const [file, setFile] = useState('')
  const act = useAction(() => request<Report>('POST', `/imports/${kind}`, text, { ctype: 'text/csv' }))
  const r = act.result
  const onFile = async (f?: File) => {
    if (!f) return
    setFile(f.name)
    setText(await f.text())
    act.reset()
  }
  return (
    <>
      <div className="head"><div><div className="caps">Veri yönetimi</div><h1>Veri içe aktarma</h1><p className="muted">CSV yükleyin; geçerli satırlar yazılır, hatalı satırlar satır numarasıyla raporlanır. Aynı dosyayı tekrar yüklemek yeni kayıt oluşturmaz.</p></div></div>
      <Card title="CSV yükle" aside={<Tabs value={kind} onChange={(k) => (setKind(k), act.reset())} items={(Object.keys(NAME) as Kind[]).map((k) => [k, NAME[k]])} />}>
        <p className="small muted">Beklenen başlık: <span className="mono">{HEAD[kind]}</span></p>
        <div className="row" style={{ margin: '12px 0' }}>
          <input type="file" accept=".csv,text/csv" aria-label="CSV dosyası" onChange={(e) => void onFile(e.target.files?.[0])} />
          {file && <Pill>{file}</Pill>}
          <button className="btn" onClick={() => (setText(`${HEAD[kind]}\n`), setFile(''))}>Boş şablon</button>
        </div>
        <textarea className="input mono" rows={8} aria-label="CSV içeriği" value={text} onChange={(e) => setText(e.target.value)} placeholder={`${HEAD[kind]}\n…`} />
        <div style={{ marginTop: 12 }}><button className="btn primary" disabled={act.busy || !text.trim()} onClick={() => void act.run()}>{act.busy ? 'Yükleniyor…' : 'İçe aktar'}</button></div>
        {act.error && <div style={{ marginTop: 12 }}><ErrorBox error={act.error} /></div>}
      </Card>
      {r && (
        <Card title="İçe aktarma raporu" aside={<Pill kind={r.errors.length ? 'warn' : 'ok'}>{r.errors.length ? `${r.errors.length} hata` : 'Hatasız'}</Pill>}>
          <p>{r.rows} satır · {r.valid} geçerli · <b>{r.created}</b> yeni · {r.updated} güncellendi{kind === 'enrollments' ? ` · ${r.unchanged} zaten kayıtlı` : ''}</p>
          {r.errors.length > 0 && (
            <div className="tablewrap" style={{ marginTop: 12 }}><table>
              <thead><tr><th>Satır</th><th>Alan</th><th>Hata</th></tr></thead>
              <tbody>{r.errors.map((e, i) => <tr key={i}><td>{e.line}</td><td>{e.field ?? '—'}</td><td>{e.message}</td></tr>)}</tbody>
            </table>{r.errors_truncated && <p className="small muted">Hata listesi kısaltıldı.</p>}</div>
          )}
        </Card>
      )}
    </>
  )
}
