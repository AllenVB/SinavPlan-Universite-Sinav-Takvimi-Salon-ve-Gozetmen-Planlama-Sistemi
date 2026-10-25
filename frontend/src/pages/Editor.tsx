import { useMemo, useState, type DragEvent } from 'react'
import { fmtShort, fmtTime, get, post } from '../api'
import { Async, Card, Empty, ErrorBox, Field, Pill, useAction, useAsync } from '../ui'
import { examLabel, ScheduleSelect, useExamFilter, useExams, useSchedules, type Exam } from './common'

type Slot = { id: number; day: number; session: number; starts_at: string; ends_at: string }
type Check = { ok: boolean; violations: { kind: string; detail: string }[]; violation_count: number; penalty_before: number; penalty_after: number }
const KIND: Record<string, string> = { conflict: 'Çakışma', daily_limit: 'Günlük sınır', slot_capacity: 'Slot kapasitesi', invalid_slot: 'Geçersiz slot', unplaced: 'Yerleşmemiş', unknown_exam: 'Bilinmeyen sınav' }
/** Aynı türde ve metinde ihlalleri gruplar ("×N"): 30 satır yerine okunur bir özet. */
const group = (v: { kind: string; detail: string }[]) => {
  const m = new Map<string, number>()
  v.forEach((x) => m.set(`${KIND[x.kind] ?? x.kind}: ${x.detail}`, (m.get(`${KIND[x.kind] ?? x.kind}: ${x.detail}`) ?? 0) + 1))
  return [...m.entries()]
}
type Pending = { exam: Exam; slot: Slot; check: Check }

/** Sürükle-bırak takvim editörü: sınavı slota taşır; kısıt ihlalinde reddeder, başarılıysa yeni taslak sürüm üretir. */
export function Editor() {
  const sch = useSchedules()
  const exams = useExams(sch.sel)
  const slots = useAsync(async () => (sch.current ? ((await get<{ items: Slot[] }>(`/periods/${sch.current.period_id}/slots`)).items) : []), [sch.current?.period_id])
  const [q, setQ] = useState('')
  const filt = useExamFilter()
  const [drag, setDrag] = useState<Exam | null>(null)
  const [over, setOver] = useState<number | null>(null)
  const [pending, setPending] = useState<Pending | null>(null)
  const [done, setDone] = useState<{ version: number; before: number; after: number } | null>(null)
  const [pick, setPick] = useState({ exam: 0, slot: 0 })

  const days = useMemo(() => [...new Set(slots.data?.map((s) => s.day))].sort((a, b) => a - b), [slots.data])
  const sessions = useMemo(() => [...new Set(slots.data?.map((s) => s.session))].sort((a, b) => a - b), [slots.data])
  const slotAt = (d: number, s: number) => slots.data?.find((x) => x.day === d && x.session === s)
  const bySlot = useMemo(() => {
    const m = new Map<number, Exam[]>()
    exams.data?.forEach((e) => m.set(e.slot_id, [...(m.get(e.slot_id) ?? []), e]))
    return m
  }, [exams.data])
  const match = (e: Exam) => filt.match(e) && (!q || `${e.course_code} ${e.course_name} ${e.department_name}`.toLowerCase().includes(q.toLowerCase()))
  const focus = !!q || filt.active

  const check = useAction(async (exam: Exam, slot: Slot) => {
    const c = await post<Check>(`/schedules/${sch.sel}/exams/${exam.exam_id}/move`, { slot_id: slot.id, dry_run: true })
    setPending({ exam, slot, check: c })
  })
  const apply = useAction(async () => {
    const r = await post<{ schedule_id: number; version: number; penalty_before: number; penalty_after: number }>(`/schedules/${sch.sel}/exams/${pending!.exam.exam_id}/move`, { slot_id: pending!.slot.id })
    setDone({ version: r.version, before: r.penalty_before, after: r.penalty_after })
    setPending(null)
    sch.list.reload()
    sch.setSel(r.schedule_id)
  })

  const onDrop = (e: DragEvent, slot: Slot) => {
    e.preventDefault()
    setOver(null)
    if (drag && drag.slot_id !== slot.id) void check.run(drag, slot)
    setDrag(null)
  }
  const label = (s: Slot) => `${fmtShort(s.starts_at)} ${fmtTime(s.starts_at)}`

  return (
    <>
      <div className="head">
        <div><div className="caps">Takvim düzenleme</div><h1>Sürükle-bırak editör</h1><p className="muted">Bir sınavı başka oturuma sürükleyin. Sert kısıtlar bozuluyorsa taşıma reddedilir; uygunsa salon, oturma ve gözetmen planı yeniden kurulur ve <b>yeni bir taslak sürüm</b> oluşur (mevcut sürüm değişmez).</p></div>
        <div className="actions"><ScheduleSelect s={sch} />{filt.element}<input className="input" style={{ width: 200 }} placeholder="Ders ara / vurgula…" aria-label="Ders ara" value={q} onChange={(e) => setQ(e.target.value)} /></div>
      </div>
      {done && <div className="alert ok" role="status"><div><b>Sürüm {done.version} (taslak) oluşturuldu.</b> Ceza {done.before.toLocaleString('tr-TR')} → {done.after.toLocaleString('tr-TR')}. Karşılaştırmak için Sınav takvimi ekranındaki "Sürüm karşılaştırma"yı kullanın.</div></div>}
      {check.error && <ErrorBox error={check.error} />}
      {apply.error && <ErrorBox error={apply.error} />}

      {pending && (
        <Card title={`${pending.exam.course_code} → ${label(pending.slot)}`} aside={<Pill kind={pending.check.ok ? 'ok' : 'err'}>{pending.check.ok ? 'Uygun' : `${pending.check.violation_count} ihlal`}</Pill>}>
          {pending.check.ok ? (
            <p>Sert kısıtlar sağlanıyor. Ceza {pending.check.penalty_before.toLocaleString('tr-TR')} → <b style={{ color: pending.check.penalty_after <= pending.check.penalty_before ? 'var(--ok)' : 'var(--error)' }}>{pending.check.penalty_after.toLocaleString('tr-TR')}</b>.</p>
          ) : (
            <ul style={{ margin: 0, paddingLeft: 18 }}>{group(pending.check.violations).map(([t, n]) => <li key={t}>{t}{n > 1 && <b> ×{n}</b>}</li>)}{pending.check.violation_count > pending.check.violations.length && <li className="muted">… ve {pending.check.violation_count - pending.check.violations.length} ihlal daha (listelenmedi)</li>}</ul>
          )}
          <div className="row" style={{ marginTop: 12 }}>
            <button className="btn primary" disabled={!pending.check.ok || apply.busy} onClick={() => void apply.run()}>{apply.busy ? 'Plan yeniden kuruluyor…' : 'Uygula (yeni taslak)'}</button>
            <button className="btn" onClick={() => setPending(null)}>Vazgeç</button>
          </div>
        </Card>
      )}

      <Card title="Oturum ızgarası" aside={check.busy ? <Pill kind="info">Denetleniyor…</Pill> : <span className="small muted">sürükle → bırak</span>}>
        <Async s={slots} empty={(d) => d.length === 0}>
          {() => exams.data && exams.data.length === 0 ? <Empty title="Bu sürümde sınav yok" /> : (
            <div className="tablewrap">
              <table style={{ tableLayout: 'fixed', minWidth: days.length * 150 + 90 }}>
                <thead><tr><th style={{ width: 90 }}>Oturum</th>{days.map((d) => <th key={d}>{d + 1}. gün<div className="small muted" style={{ textTransform: 'none', letterSpacing: 0 }}>{fmtShort(slotAt(d, sessions[0])?.starts_at ?? '')}</div></th>)}</tr></thead>
                <tbody>
                  {sessions.map((s) => (
                    <tr key={s}>
                      <th>{s + 1}. oturum<div className="small muted" style={{ textTransform: 'none', letterSpacing: 0 }}>{fmtTime(slotAt(days[0], s)?.starts_at ?? '')}</div></th>
                      {days.map((d) => {
                        const slot = slotAt(d, s)
                        if (!slot) return <td key={d} />
                        const list = bySlot.get(slot.id) ?? []
                        return (
                          <td key={d} onDragOver={(e) => { e.preventDefault(); setOver(slot.id) }} onDragLeave={() => setOver(null)} onDrop={(e) => onDrop(e, slot)}
                            aria-label={`${d + 1}. gün ${s + 1}. oturum, ${list.length} sınav`}
                            style={{ verticalAlign: 'top', background: over === slot.id ? 'var(--primary-fixed)' : undefined, outline: over === slot.id ? '2px dashed var(--primary)' : undefined }}>
                            <div className="small muted">{list.length} sınav</div>
                            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4, maxHeight: 130, overflowY: 'auto', marginTop: 4 }}>
                              {list.map((e) => (
                                <span key={e.exam_id} draggable onDragStart={(ev) => { setDrag(e); ev.dataTransfer.setData('text/plain', String(e.exam_id)); ev.dataTransfer.effectAllowed = 'move' }} onDragEnd={() => { setDrag(null); setOver(null) }}
                                  title={`${examLabel(e)} · ${e.students} öğrenci`} className="pill info" style={{ cursor: 'grab', opacity: focus && !match(e) ? 0.2 : 1, outline: focus && match(e) ? '2px solid var(--tertiary)' : undefined }}>{e.course_code}</span>
                              ))}
                            </div>
                          </td>
                        )
                      })}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Async>
      </Card>

      <Card title="Klavye / dokunmatik alternatifi">
        <p className="small muted" style={{ marginBottom: 12 }}>Sürükleyemiyorsanız sınavı ve hedef oturumu listeden seçin.</p>
        <div className="row" style={{ alignItems: 'flex-end' }}>
          <Field label="Sınav" id="pe"><select id="pe" value={pick.exam} onChange={(e) => setPick({ ...pick, exam: Number(e.target.value) })}><option value={0}>Seçin…</option>{exams.data?.filter(filt.match).map((e) => <option key={e.exam_id} value={e.exam_id}>{examLabel(e)}</option>)}</select></Field>
          <Field label="Hedef oturum" id="ps"><select id="ps" value={pick.slot} onChange={(e) => setPick({ ...pick, slot: Number(e.target.value) })}><option value={0}>Seçin…</option>{slots.data?.map((s) => <option key={s.id} value={s.id}>{label(s)}</option>)}</select></Field>
          <button className="btn" disabled={!pick.exam || !pick.slot || check.busy} onClick={() => { const e = exams.data!.find((x) => x.exam_id === pick.exam)!; const s = slots.data!.find((x) => x.id === pick.slot)!; if (e.slot_id !== s.id) void check.run(e, s) }}>Denetle</button>
        </div>
      </Card>
    </>
  )
}
