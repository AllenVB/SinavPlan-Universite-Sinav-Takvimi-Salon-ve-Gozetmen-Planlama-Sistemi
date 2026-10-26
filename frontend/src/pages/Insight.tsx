import { useState } from 'react'
import { fmtDate, fmtTime, get } from '../api'
import { Async, Card, Pill, useAsync } from '../ui'

/**
 * Faz 22 B3 — çizelgenin okunabilirliği:
 *  - LoadHeatmap: gün × oturum yoğunluğu ve gün başına "aynı gün 2 sınav" / "ardışık gün"; toplamlar SQL ile
 *    hesaplanır ve çözücünün metrikleriyle karşılaştırılır.
 *  - WhyPanel: "bu sınav neden burada?" — her oturumun durumu (çakışma, günlük sınır, kapasite, uygun + ceza farkı).
 */
type Cell = { slot_id: number; day: number; session: number; exams: number; students: number }
type Day = { day: number; two_exams: number; consecutive: number }
type Load = { cells: Cell[]; days: Day[]; totals: { student_days_with_two_exams: number; consecutive_day_pairs: number }; totals_match?: boolean }
type Opt = { slot_id: number; starts_at: string; day: number; session: number; status: 'current' | 'ok' | 'conflict' | 'daily_limit' | 'capacity'; conflicts: { exam_id: number; shared: number }[]; daily_limit: number; delta: number }
type Why = { course_code: string; options: Opt[]; counts: Record<string, number>; best_alternative: Opt | null; codes: Record<string, string> }

const SESSIONS = ['Sabah', 'Öğle', 'Akşam']
const n = (x: number) => x.toLocaleString('tr-TR')

export function LoadHeatmap({ schedule }: { schedule: number | null }) {
  const load = useAsync(() => (schedule ? get<Load>(`/schedules/${schedule}/load`) : Promise.resolve(null)), [schedule])
  return (
    <Card title="Yoğunluk haritası" aside={load.data && <Pill kind={load.data.totals_match ? 'ok' : 'warn'}>{load.data.totals_match ? 'SQL toplamı = çözücü metriği' : 'toplamlar farklı'}</Pill>}>
      <Async s={load} empty={(d) => !d || d.cells.length === 0}>
        {(d) => {
          if (!d) return null
          const days = [...new Set(d.cells.map((c) => c.day))].sort((a, b) => a - b)
          const max = Math.max(1, ...d.cells.map((c) => c.students))
          const at = new Map(d.cells.map((c) => [`${c.day}:${c.session}`, c]))
          const dmax = Math.max(1, ...d.days.map((x) => x.two_exams))
          return (
            <div data-testid="heatmap">
              <div className="tablewrap" tabIndex={0} role="region" aria-label="Gün ve oturum yoğunluğu">
                <table className="heat">
                  <thead><tr><th />{days.map((x) => <th key={x}>{x + 1}</th>)}</tr></thead>
                  <tbody>
                    {SESSIONS.map((s, si) => (
                      <tr key={s}><th scope="row">{s}</th>{days.map((x) => {
                        const c = at.get(`${x}:${si}`)
                        const v = c ? c.students / max : 0
                        return <td key={x} title={c ? `${x + 1}. gün ${s}: ${c.exams} sınav, ${n(c.students)} öğrenci` : ''} style={{ background: c && c.exams ? `rgba(70, 72, 212, ${0.06 + v * 0.49})` : undefined }}>{c && c.exams ? c.exams : ''}</td>
                      })}</tr>
                    ))}
                    <tr><th scope="row" title="o gün 2 sınava giren öğrenci">Aynı gün 2</th>{days.map((x) => {
                      const dd = d.days.find((y) => y.day === x)
                      const v = dd ? dd.two_exams / dmax : 0
                      return <td key={x} title={`${x + 1}. gün: ${dd?.two_exams ?? 0} öğrenci aynı gün 2 sınava giriyor`} style={{ background: v ? `rgba(230, 150, 20, ${0.08 + v * 0.47})` : undefined }}>{dd?.two_exams || ''}</td>
                    })}</tr>
                    <tr><th scope="row" title="o gün ve ertesi gün sınavı olan öğrenci">Ardışık</th>{days.map((x) => {
                      const dd = d.days.find((y) => y.day === x)
                      return <td key={x} style={{ background: dd?.consecutive ? 'rgba(200, 40, 40, .25)' : undefined }}>{dd?.consecutive || ''}</td>
                    })}</tr>
                  </tbody>
                </table>
              </div>
              <p className="small muted" style={{ margin: '8px 0 0' }}>Hücrede o oturumdaki sınav sayısı; renk öğrenci yoğunluğu. Toplam: aynı gün 2 sınav {n(d.totals.student_days_with_two_exams)} öğrenci-gün, ardışık gün {n(d.totals.consecutive_day_pairs)}. Toplamlar SQL ile hesaplanır ve çözücünün kendi metrikleriyle karşılaştırılır.</p>
            </div>
          )
        }}
      </Async>
    </Card>
  )
}

const ST: Record<Opt['status'], [string, string]> = {
  current: ['Şu anki oturum', 'cur'], ok: ['Uygun', 'ok'], conflict: ['Çakışma', 'bad'], daily_limit: ['Günlük sınır', 'lim'], capacity: ['Kapasite', 'cap'],
}

export function WhyPanel({ schedule, exam }: { schedule: number; exam: number }) {
  const why = useAsync(() => get<Why>(`/schedules/${schedule}/exams/${exam}/why`), [schedule, exam])
  const [pick, setPick] = useState<number | null>(null)
  return (
    <div className="whypanel" data-testid="why-panel">
      <Async s={why}>
        {(d) => {
          const days = [...new Set(d.options.map((o) => o.day))].sort((a, b) => a - b)
          const at = new Map(d.options.map((o) => [`${o.day}:${o.session}`, o]))
          const cur = d.options.find((o) => o.status === 'current')!
          const best = d.best_alternative
          const sel = pick !== null ? d.options.find((o) => o.slot_id === pick) : null
          const total = d.options.length - 1
          return (
            <>
              <p className="small" style={{ margin: 0 }}>
                <b>{d.course_code}</b> için {total} başka oturum: <b>{d.counts.conflict ?? 0}</b> çakışma (ortak öğrencili sınav var), <b>{d.counts.daily_limit ?? 0}</b> günlük sınır (bazı öğrenciler günde 3. sınava girerdi){d.counts.capacity ? <>, <b>{d.counts.capacity}</b> kapasite yetersiz</> : null}, <b>{d.counts.ok ?? 0}</b> uygun.{' '}
                {best ? (best.delta >= 0
                  ? <>Uygunların en iyisi bile toplam rahatsızlığı <b>+{best.delta}</b> artırır: <b>şu anki oturum uygunlar arasında en iyisi.</b></>
                  : <>Bir oturum cezayı <b>{best.delta}</b> düşürürdü ({fmtDate(best.starts_at)} {fmtTime(best.starts_at)}).</>) : <>Başka uygun oturum yok: sınav başka yere konamaz.</>}
              </p>
              <div className="tablewrap" tabIndex={0} role="region" aria-label="Oturum durumları">
                <table className="heat why">
                  <thead><tr><th />{days.map((x) => <th key={x}>{x + 1}</th>)}</tr></thead>
                  <tbody>
                    {SESSIONS.map((s, si) => (
                      <tr key={s}><th scope="row">{s}</th>{days.map((x) => {
                        const o = at.get(`${x}:${si}`)
                        if (!o) return <td key={x} />
                        const label = o.status === 'ok' ? (o.delta > 0 ? `+${o.delta}` : String(o.delta)) : o.status === 'current' ? '●' : o.status === 'conflict' ? '✕' : o.status === 'daily_limit' ? '3' : '▲'
                        return <td key={x} className={`w-${ST[o.status][1]}`}><button className="cellbtn" aria-pressed={pick === o.slot_id} onClick={() => setPick(o.slot_id)} title={`${fmtDate(o.starts_at)} ${fmtTime(o.starts_at)}: ${ST[o.status][0]}`}>{label}</button></td>
                      })}</tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <div className="row small" style={{ gap: 10 }}>
                <span><i className="sw w-cur" /> şu an ({fmtDate(cur.starts_at)} {fmtTime(cur.starts_at)})</span>
                <span><i className="sw w-ok" /> uygun (+N: rahatsızlık farkı)</span><span><i className="sw w-bad" /> ✕ çakışma</span><span><i className="sw w-lim" /> 3: günlük sınır</span>
              </div>
              {sel && (
                <div className="small" role="status">
                  <b>{fmtDate(sel.starts_at)} {fmtTime(sel.starts_at)} — {ST[sel.status][0]}.</b>{' '}
                  {sel.status === 'conflict' && <>Ortak öğrenci: {sel.conflicts.slice(0, 6).map((c) => `${d.codes[String(c.exam_id)] ?? c.exam_id} (${c.shared})`).join(', ')}{sel.conflicts.length > 6 ? ` … +${sel.conflicts.length - 6}` : ''}. Aynı oturuma konursa bu öğrenciler iki sınava aynı anda girerdi.</>}
                  {sel.status === 'daily_limit' && <>{sel.daily_limit} öğrenci o gün zaten iki sınava giriyor; buraya taşınırsa günde 3 sınav olurdu.</>}
                  {sel.status === 'capacity' && <>Oturumun toplam koltuk kapasitesi yetmez.</>}
                  {sel.status === 'ok' && <>Sert kısıtlar sağlanıyor; taşınırsa toplam rahatsızlık {sel.delta > 0 ? `+${sel.delta}` : sel.delta} değişir (aynı gün 2 sınav / ardışık gün).</>}
                  {sel.status === 'current' && <>Sınavın şu anki yeri.</>}
                </div>
              )}
            </>
          )
        }}
      </Async>
    </div>
  )
}
