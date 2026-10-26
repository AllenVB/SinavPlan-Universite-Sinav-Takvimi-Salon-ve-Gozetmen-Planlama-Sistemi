import { useState } from 'react'
import { get } from '../api'
import { Async, Card, Pill, Tabs, useAsync } from '../ui'
import { Imports } from './Imports'
import { ScheduleSelect, useSchedules } from './common'

/**
 * Faz 22 A2 — Kanıtlar: zor görevlerin kısıtları, kayıtlı çizelge üzerinde çözücüden bağımsız SQL ile CANLI denetlenir.
 * Her denetim iki kez koşar: gerçek plan (0 ihlal beklenir) ve bilerek bozulmuş sanal plan (yakalanmalı) —
 * "doğrulayıcı gerçekten bir şey yakalıyor mu?" sorusunun cevabı. Veritabanına hiçbir şey yazılmaz.
 */
type Proof = { id: string; task: string; title: string; found: number; mutation: string; caught: number; ok: boolean; detail?: string; ms: number }
type Resp = { schedule_id: number; version: number; exams: number; seats: number; duties: number; items: Proof[]; total_ms: number }

const TASK: Record<string, string> = {
  'T-1': 'Sınav takvimi çizelgeleme', 'T-2': 'Salon atama (paketleme)', 'T-3': 'Kopya önleyici oturma', 'T-4': 'Adil gözetmen atama', 'T-5': 'Yoklama ve denetim kaydı',
}
const n = (x: number) => x.toLocaleString('tr-TR')

/** Menüde tek "Yönetim" öğesi: veri içe aktarma + kanıtlar (her rolde ≤7 menü öğesi korunur). */
export function AdminDesk({ initialTab = 'import' }: { initialTab?: 'import' | 'proofs' }) {
  const [tab, setTab] = useState(initialTab)
  return (
    <>
      <div className="noprint" style={{ marginBottom: 12 }}><Tabs value={tab} onChange={setTab} items={[['import', 'Veri içe aktarma'], ['proofs', 'Kanıtlar (canlı doğrulama)']]} /></div>
      {tab === 'import' ? <Imports /> : <Proofs />}
    </>
  )
}

export function Proofs() {
  const sch = useSchedules()
  const [run, setRun] = useState(0)
  const res = useAsync(() => (sch.sel ? get<Resp>(`/schedules/${sch.sel}/proofs`) : Promise.resolve(null)), [sch.sel, run])
  return (
    <>
      <div className="head">
        <div>
          <div className="caps">Kanıtlar</div>
          <h1>Kısıtlar canlı doğrulanıyor</h1>
          <p className="muted">Seçili sürümün takvimi, salonları, oturma planı ve gözetmenleri veritabanından okunur; kurallar çözücü kodundan <b>bağımsız</b> SQL sorgularıyla yeniden sayılır.</p>
        </div>
        <div className="actions">
          <ScheduleSelect s={sch} />
          <button className="btn primary" onClick={() => setRun((x) => x + 1)} disabled={res.loading}>{res.loading ? 'Çalışıyor…' : 'Yeniden çalıştır'}</button>
        </div>
      </div>
      <Async s={res} empty={(d) => !d}>
        {(d) => {
          if (!d) return null
          const pass = d.items.filter((p) => p.ok).length
          const tasks = [...new Set(d.items.map((p) => p.task))]
          return (
            <>
              <div className={`alert ${pass === d.items.length ? 'ok' : 'err'}`} role="status" data-testid="proofs-summary">
                <div><b>{pass} / {d.items.length} denetim geçti</b> · Sürüm {d.version} · {n(d.exams)} sınav, {n(d.seats)} koltuk ataması, {n(d.duties)} gözetmen görevi · {n(d.total_ms)} ms</div>
              </div>
              <p className="small muted">Her satır iki kez koşar: <b>gerçek plan</b> (ihlal 0 olmalı) ve <b>bilerek bozulmuş sanal plan</b> (sorgu parametresiyle; veritabanına yazılmaz) — doğrulayıcı bozulmayı yakalamalı. Yakalamayan bir doğrulayıcı, 0 ihlalin de anlamsız olduğunu gösterirdi.</p>
              <div className="grid" style={{ gap: 12 }}>
                {tasks.map((t) => (
                  <Card key={t} title={`${t} · ${TASK[t] ?? ''}`} aside={<Pill kind={d.items.filter((p) => p.task === t).every((p) => p.ok) ? 'ok' : 'err'}>{d.items.filter((p) => p.task === t && p.ok).length} / {d.items.filter((p) => p.task === t).length}</Pill>}>
                    <div className="tablewrap"><table>
                      <thead><tr><th>Kural</th><th>Gerçek plan</th><th>Bozulmuş sanal plan</th><th>Süre</th></tr></thead>
                      <tbody>
                        {d.items.filter((p) => p.task === t).map((p) => (
                          <tr key={p.id} className={p.ok ? '' : 'tone-err'} data-proof={p.id}>
                            <td><span className="mono small">{p.id}</span> {p.title}</td>
                            <td><Pill kind={p.found === 0 ? 'ok' : 'err'}>{p.task === 'T-5' ? '—' : `${n(p.found)} ihlal`}</Pill></td>
                            <td className="small">{p.mutation} → <Pill kind={p.caught > 0 ? 'ok' : 'warn'}>{p.task === 'T-5' ? (p.caught ? 'reddedildi' : 'kabul edildi!') : `${n(p.caught)} yakalandı`}</Pill>{p.detail && <div className="muted">{p.detail}</div>}</td>
                            <td className="small">{n(p.ms)} ms</td>
                          </tr>
                        ))}
                      </tbody>
                    </table></div>
                  </Card>
                ))}
              </div>
              <Card title="Komutla yeniden üret">
                <ul className="small" style={{ margin: 0 }}>
                  <li><span className="mono">make verify</span> — 35 kimlik, API üzerinden uçtan uca (PASS/FAIL tablosu)</li>
                  <li><span className="mono">make prove-all</span> — T-1..T-4 tam ölçek kanıtları (300 sınav / 8.000 öğrenci), bozuk plan sayaç sınaması</li>
                  <li><span className="mono">make test</span> — 211 otomatik test, yarış dedektörüyle; domain kapsamı ≥ %94</li>
                </ul>
              </Card>
            </>
          )
        }}
      </Async>
    </>
  )
}
