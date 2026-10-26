import { useEffect, useMemo, useState } from 'react'
import { all, fmtDateTime, idemKey, post } from '../api'
import { useAuth } from '../auth'
import { Icon } from '../icons'
import { Async, Bar, Card, ErrorBox, Field, Pill, StatCard, Status, useAction, useAsync } from '../ui'
import { DutyRoster } from './DutyRoster'
import { hasRole, ScheduleSelect, useSchedules } from './common'

type Duty = { id: number; user_id: number; full_name: string; slot_id: number; room_id: number; day: number; session: number; starts_at: string; room_code: string; building: string; exam_id: number; course_code: string }
type Removal = { vacated: number; swaps: number; violations: number; changes: { duty_id: number; from: number; to: number }[]; unfilled: unknown[]; untouched: number; total: number; dry_run: boolean; duties: Record<string, { starts_at: string; room_code: string }> }

type AssignRes = { ok: boolean; dry_run: boolean; violations: { kind: string; detail: string }[]; duty: { starts_at: string; room_code: string }; spread_before: number; spread_after: number; user_duties: number }
const KIND_TR: Record<string, string> = { own_exam: 'Kendi dersi', excused: 'Mazeretli', double_slot: 'Aynı oturumda iki görev', consecutive_session: 'Ardışık oturum', duplicate: 'Zaten bu görevde' }
type DutyPick = { slot: number; room: number; replace: number }

/**
 * Elle atama (sekreter): istenen gözetmeni istenen salona — bir görevin yerine geçerek ya da ek gözetmen olarak.
 * Önce önizleme (kurallar + yük farkı), sonra "Uygula". Kuralı bozan atama sunucuda reddedilir.
 */
function ManualAssign({ schedule, duties, pick, onDone }: { schedule: number; duties: Duty[]; pick: DutyPick | null; onDone: () => void }) {
  const slots = useMemo(() => [...new Map(duties.map((d) => [d.slot_id, d])).values()].sort((a, b) => a.starts_at.localeCompare(b.starts_at)), [duties])
  const people = useMemo(() => [...new Map(duties.map((d) => [d.user_id, d.full_name])).entries()].sort((a, b) => a[1].localeCompare(b[1], 'tr')), [duties])
  const load = useMemo(() => { const m = new Map<number, number>(); duties.forEach((d) => m.set(d.user_id, (m.get(d.user_id) ?? 0) + 1)); return m }, [duties])
  const [slot, setSlot] = useState(0)
  const [room, setRoom] = useState(0)
  const [replace, setReplace] = useState(0)
  const [user, setUser] = useState(0)
  useEffect(() => { if (pick) { setSlot(pick.slot); setRoom(pick.room); setReplace(pick.replace); setUser(0) } }, [pick])
  const rooms = useMemo(() => [...new Map(duties.filter((d) => d.slot_id === slot).map((d) => [d.room_id, d])).values()].sort((a, b) => a.room_code.localeCompare(b.room_code)), [duties, slot])
  const current = duties.filter((d) => d.slot_id === slot && d.room_id === room)
  const body = { slot_id: slot, room_id: room, user_id: user, ...(replace ? { replace_user_id: replace } : {}) }
  const [pv, setPv] = useState<AssignRes | null>(null)
  useEffect(() => setPv(null), [slot, room, replace, user])
  const preview = useAction(async () => { const r = await post<AssignRes>(`/schedules/${schedule}/invigilators/assign?dry_run=true`, body); setPv(r); return r })
  const apply = useAction(async () => {
    const r = await post<AssignRes>(`/schedules/${schedule}/invigilators/assign`, body, { 'Idempotency-Key': idemKey(`assign-${schedule}-${slot}-${room}-${user}-${replace}`) })
    setPv(null); setUser(0); onDone(); return r
  })
  const ready = slot > 0 && room > 0 && user > 0
  return (
    <Card title="Elle atama" aside={<span className="small muted">istenen gözetmeni istenen salona</span>}>
      <div data-testid="manual-assign" id="manual-assign">
        <div className="row" style={{ alignItems: 'flex-end' }}>
          <Field label="Oturum" id="ma-slot"><select id="ma-slot" value={slot} onChange={(e) => { setSlot(Number(e.target.value)); setRoom(0); setReplace(0) }} style={{ width: 'auto' }}><option value={0}>Oturum seçin…</option>{slots.map((d) => <option key={d.slot_id} value={d.slot_id}>{fmtDateTime(d.starts_at)}</option>)}</select></Field>
          <Field label="Salon" id="ma-room"><select id="ma-room" value={room} disabled={!slot} onChange={(e) => { setRoom(Number(e.target.value)); setReplace(0) }} style={{ width: 'auto' }}><option value={0}>Salon seçin…</option>{rooms.map((d) => <option key={d.room_id} value={d.room_id}>{d.room_code} · {d.building} · {d.course_code}</option>)}</select></Field>
          <Field label="Nasıl" id="ma-mode"><select id="ma-mode" value={replace} disabled={!room} onChange={(e) => setReplace(Number(e.target.value))} style={{ width: 'auto' }}><option value={0}>Ek gözetmen olarak ekle</option>{current.map((d) => <option key={d.user_id} value={d.user_id}>{d.full_name} yerine</option>)}</select></Field>
          <Field label="Gözetmen" id="ma-user"><select id="ma-user" value={user} disabled={!room} onChange={(e) => setUser(Number(e.target.value))} style={{ width: 'auto' }}><option value={0}>Gözetmen seçin…</option>{people.filter(([id]) => !current.some((d) => d.user_id === id)).map(([id, n]) => <option key={id} value={id}>{n} ({load.get(id) ?? 0} görev)</option>)}</select></Field>
          <button className="btn" disabled={!ready || preview.busy} onClick={() => void preview.run()}>{preview.busy ? 'Denetleniyor…' : 'Önizle'}</button>
          <button className="btn primary" disabled={!ready || !pv?.ok || apply.busy} onClick={() => void apply.run()} data-testid="ma-apply">{apply.busy ? 'Uygulanıyor…' : 'Uygula'}</button>
        </div>
        {room > 0 && <p className="small muted" style={{ margin: '8px 0 0' }}>Bu görevde şu an: {current.map((d) => d.full_name).join(', ') || 'gözetmen yok'}.</p>}
        {pv && (pv.ok ? (
          <div className="alert ok" role="status" data-testid="ma-preview"><div className="small"><b>Kurallara uygun.</b> {pv.duty.room_code} · {fmtDateTime(pv.duty.starts_at)}. Gözetmenin görev sayısı {pv.user_duties} olur; en çok–en az görev farkı {pv.spread_before} → {pv.spread_after}{pv.spread_after > pv.spread_before && ' (adalet biraz bozulur)'}. Henüz hiçbir şey değişmedi.</div></div>
        ) : (
          <div className="alert err" role="alert" data-testid="ma-preview"><div className="small"><b>Bu atama yapılamaz:</b><ul style={{ margin: '4px 0 0' }}>{pv.violations.map((v, i) => <li key={i}>{KIND_TR[v.kind] ?? v.kind}: {v.detail}</li>)}</ul></div></div>
        ))}
        {apply.result && !pv && <div className="alert ok" role="status" style={{ marginTop: 8 }}><div className="small"><b>Atama yapıldı.</b> {apply.result.duty.room_code} · {fmtDateTime(apply.result.duty.starts_at)}. Gözetmenlere bildirim gönderildi.</div></div>}
        {(preview.error || apply.error) && <ErrorBox error={(preview.error ?? apply.error)!} />}
      </div>
    </Card>
  )
}

export function Invigilators() {
  const { user } = useAuth()
  const sch = useSchedules()
  const duties = useAsync(() => (sch.sel ? all<Duty>(`/schedules/${sch.sel}/invigilators`) : Promise.resolve([] as Duty[])), [sch.sel])
  const [q, setQ] = useState('')
  const [page, setPage] = useState(0)
  const secretary = hasRole(user!.roles, 'secretary')
  const load = useMemo(() => {
    const m = new Map<number, { name: string; n: number }>()
    duties.data?.forEach((d) => m.set(d.user_id, { name: d.full_name, n: (m.get(d.user_id)?.n ?? 0) + 1 }))
    return m
  }, [duties.data])
  const loads = [...load.values()].map((x) => x.n)
  const spread = loads.length ? Math.max(...loads) - Math.min(...loads) : 0
  const hist = useMemo(() => { const m = new Map<number, number>(); loads.forEach((n) => m.set(n, (m.get(n) ?? 0) + 1)); return [...m.entries()].sort((a, b) => b[0] - a[0]) }, [loads.join(',')]) // eslint-disable-line react-hooks/exhaustive-deps
  const pend = useAsync(async () => (await all<{ user_id?: number; kind: string }>('/excuses?status=pending')).filter((e) => e.kind === 'invigilator' && e.user_id).map((e) => e.user_id!))
  const pendingUsers = new Set(pend.data ?? [])
  const violations = useMemo(() => {
    if (!duties.data) return 0
    let v = 0
    const slot = new Map<string, number>(), byUserDay = new Map<string, Set<number>>()
    duties.data.forEach((d) => {
      slot.set(`${d.user_id}:${d.slot_id}`, (slot.get(`${d.user_id}:${d.slot_id}`) ?? 0) + 1)
      const k = `${d.user_id}:${d.day}`; (byUserDay.get(k) ?? byUserDay.set(k, new Set()).get(k)!).add(d.session)
    })
    slot.forEach((n) => { if (n > 1) v++ })
    byUserDay.forEach((ss) => ss.forEach((x) => { if (ss.has(x + 1)) v++ }))
    return v
  }, [duties.data])
  const rows = useMemo(() => (duties.data ?? []).filter((d) => `${d.full_name} ${d.room_code} ${d.course_code}`.toLowerCase().includes(q.toLowerCase())), [duties.data, q])
  const pages = Math.ceil(rows.length / 50)
  const [removed, setRemoved] = useState<{ name: string; r: Removal; same: boolean } | null>(null)
  // Faz 22 B2: önce önizleme (?dry_run=true — aynı işlem koşar ve geri alınır), sonra "Uygula"
  const [pv, setPv] = useState<{ uid: number; name: string; r: Removal } | null>(null)
  const preview = useAction(async (uid: number, name: string) => {
    setRemoved(null)
    const r = await post<Removal>(`/schedules/${sch.sel}/invigilators/${uid}/remove?dry_run=true`)
    setPv({ uid, name, r })
  })
  const remove = useAction(async (uid: number, name: string) => {
    const r = await post<Removal>(`/schedules/${sch.sel}/invigilators/${uid}/remove`, undefined, { 'Idempotency-Key': idemKey(`remove-${sch.sel}-${uid}`) })
    const same = !!pv && pv.r.vacated === r.vacated && pv.r.changes.length === r.changes.length && pv.r.swaps === r.swaps && JSON.stringify(pv.r.changes) === JSON.stringify(r.changes)
    setRemoved({ name, r, same })
    setPv(null)
    duties.reload()
  })
  const [pick, setPick] = useState<DutyPick | null>(null)
  const who = useMemo(() => new Map((duties.data ?? []).map((d) => [d.user_id, d.full_name])), [duties.data])
  return (
    <>
      <div className="head">
        <div>
          <div className="caps">Gözetmen yönetimi</div>
          <h1>Gözetmen atama ve takip</h1>
        </div>
        <ScheduleSelect s={sch} />
      </div>
      <div className="grid g4">
        <StatCard icon="list" tone="info" label="Toplam görev" value={duties.data?.length ?? '—'} />
        <StatCard icon="users" tone="info" label="Gözetmen" value={load.size || '—'} note={pendingUsers.size ? `${pendingUsers.size} gözetmenin mazereti bekliyor` : undefined} />
        <StatCard icon="shield" tone={loads.length && spread <= 2 ? 'ok' : 'warn'} label="Maksimum görev farkı" value={loads.length ? spread : '—'} note={loads.length ? `en çok ${Math.max(...loads)} – en az ${Math.min(...loads)} görev` : undefined} progress={loads.length ? Math.max(0, 100 - spread * 25) : 0} />
        <StatCard icon="checkCircle" tone={violations === 0 ? 'ok' : 'err'} label="Kural ihlali" value={violations} note="aynı slot / ardışık oturum" />
      </div>
      <div className="grid g2">
        <Card title="Görev sayısı dağılımı">
          {hist.length === 0 ? <div className="skel" /> : <div className="hist">{hist.map(([n, c]) => <div className="hrow" key={n}><span>{n} görev</span><Bar pct={(c / load.size) * 100} label={`${n} görevli gözetmen oranı`} /><span className="muted">{c} kişi (%{Math.round((c / load.size) * 100)})</span></div>)}</div>}
        </Card>
        <Card title="Aktif atama kuralları" aside={<Pill kind={violations === 0 ? 'ok' : 'err'}>{violations === 0 ? 'Hepsi sağlanıyor' : `${violations} ihlal`}</Pill>}>
          <ul className="checks">
            <li><Icon name="check" size={16} /> Bir gözetmen aynı oturumda yalnızca tek salonda görevlendirilir.</li>
            <li><Icon name="check" size={16} /> Aynı gün art arda iki oturumda görev verilmez.</li>
            <li><Icon name="check" size={16} /> 50'den fazla öğrencili salonlara en az 2 gözetmen atanır.</li>
            <li><Icon name="check" size={16} /> Onaylı mazereti olan gözetmen o oturuma atanmaz.</li>
            <li><Icon name="check" size={16} /> Görev yükü eşitliği: en çok–en az fark en aza indirilir.</li>
          </ul>
        </Card>
      </div>
      {pv && (
        <Card title={`Önizleme: ${pv.name} çıkarılırsa`} aside={<Pill kind="info">Henüz hiçbir şey değişmedi</Pill>}>
          <div data-testid="remove-preview">
            <div className="grid g4">
              <StatCard icon="alert" tone="warn" label="Boşalan görev" value={pv.r.vacated} />
              <StatCard icon="users" tone="info" label="Yeniden atanan" value={pv.r.changes.length} note={`${pv.r.swaps} takas`} />
              <StatCard icon="shield" tone="ok" label="Dokunulmayan atama" value={`${pv.r.untouched} / ${pv.r.total}`} note="minimum değişiklik" />
              <StatCard icon="checkCircle" tone={pv.r.violations === 0 && pv.r.unfilled.length === 0 ? 'ok' : 'err'} label="İhlal / doldurulamayan" value={`${pv.r.violations} / ${pv.r.unfilled.length}`} />
            </div>
            <div className="tablewrap" style={{ maxHeight: 240, overflowY: 'auto', marginTop: 12 }} tabIndex={0} role="region" aria-label="Değişecek görevler">
              <table>
                <thead><tr><th>Görev</th><th>Salon</th><th>Şimdiki</th><th>Yeni gözetmen</th></tr></thead>
                <tbody>{pv.r.changes.map((c, i) => { const d = pv.r.duties[String(c.duty_id)]; return <tr key={i}><td>{d ? fmtDateTime(d.starts_at) : c.duty_id}</td><td>{d?.room_code}</td><td>{who.get(c.from) ?? `#${c.from}`}</td><td><b>{who.get(c.to) ?? `#${c.to}`}</b></td></tr> })}</tbody>
              </table>
            </div>
            <div className="row" style={{ marginTop: 12 }}>
              <button className="btn danger" disabled={remove.busy} onClick={() => void remove.run(pv.uid, pv.name)}>{remove.busy ? 'Uygulanıyor…' : 'Uygula'}</button>
              <button className="btn" onClick={() => setPv(null)}>Vazgeç</button>
            </div>
          </div>
        </Card>
      )}
      {removed && (
        <div className="alert ok" role="status">
          <div><b>{removed.name} çizelgeden çıkarıldı.</b> {removed.r.vacated} görev boşaldı, {removed.r.changes.length} görev yeniden atandı ({removed.r.swaps} takas), kalan ihlal: {removed.r.violations}, doldurulamayan: {removed.r.unfilled.length}. {removed.r.untouched} atama korundu.{removed.same && ' Sonuç önizlemeyle birebir aynı.'}</div>
        </div>
      )}
      {(remove.error || preview.error) && <ErrorBox error={(remove.error ?? preview.error)!} />}
      {secretary && sch.sel && duties.data && <ManualAssign schedule={sch.sel} duties={duties.data} pick={pick} onDone={() => duties.reload()} />}
      <DutyRoster schedule={sch.sel} />
      <Card title="Görev listesi" aside={<input className="input" style={{ width: 260 }} placeholder="Gözetmen, salon veya ders ara…" aria-label="Ara" value={q} onChange={(e) => (setQ(e.target.value), setPage(0))} />}>
        <Async s={duties} empty={(d) => d.length === 0}>
          {() => (
            <>
              <div className="tablewrap">
                <table>
                  <thead><tr><th>Gözetmen</th><th>Tarih / saat</th><th>Salon</th><th>Ders</th>{secretary && <th />}</tr></thead>
                  <tbody>
                    {rows.slice(page * 50, page * 50 + 50).map((d) => (
                      <tr key={d.id}>
                        <td>{d.full_name}</td><td>{fmtDateTime(d.starts_at)}</td><td>{d.room_code} <span className="muted small">{d.building}</span></td><td>{d.course_code}</td>
                        {secretary && <td><div className="row" style={{ flexWrap: 'nowrap' }}><button className="btn sm" onClick={() => { setPick({ slot: d.slot_id, room: d.room_id, replace: d.user_id }); setTimeout(() => document.getElementById('manual-assign')?.scrollIntoView({ behavior: 'smooth', block: 'center' }), 50) }} aria-label={`${d.full_name} görevini değiştir`}>Değiştir</button><button className="btn sm danger" disabled={remove.busy || preview.busy} onClick={() => void preview.run(d.user_id, d.full_name)}>{preview.busy ? 'Hesaplanıyor…' : 'Hasta / çıkar'}</button></div></td>}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <div className="row" style={{ marginTop: 12, justifyContent: 'space-between' }}>
                <span className="small muted">{rows.length} kayıt · sayfa {page + 1}/{Math.max(1, pages)}</span>
                <div className="row"><button className="btn sm" disabled={page === 0} onClick={() => setPage(page - 1)}>Önceki</button><button className="btn sm" disabled={page + 1 >= pages} onClick={() => setPage(page + 1)}>Sonraki</button></div>
              </div>
            </>
          )}
        </Async>
      </Card>
      <Card title="Gözetmen yükleri" aside={<span className="small muted">çubuk: en yüklü gözetmene göre</span>}>
        <div className="tablewrap" style={{ maxHeight: 360, overflowY: 'auto' }} tabIndex={0} role="region" aria-label="Gözetmen yükleri tablosu"><table>
          <thead><tr><th>Gözetmen</th><th style={{ width: '45%' }}>Görev yükü</th><th>Durum</th></tr></thead>
          <tbody>{[...load.entries()].sort((a, b) => b[1].n - a[1].n || a[1].name.localeCompare(b[1].name, 'tr')).map(([id, v]) => (
            <tr key={id} className={pendingUsers.has(id) ? 'tone-warn' : ''}><td>{v.name}</td><td><div className="row" style={{ flexWrap: 'nowrap' }}><div style={{ flex: 1 }}><Bar pct={(v.n / Math.max(...loads)) * 100} label={`${v.name} görev yükü`} /></div><b>{v.n}</b></div></td><td>{pendingUsers.has(id) ? <Status tone="warn">Mazeret bekliyor</Status> : <Status tone="ok">Onaylandı</Status>}</td></tr>
          ))}</tbody>
        </table></div>
      </Card>
    </>
  )
}
