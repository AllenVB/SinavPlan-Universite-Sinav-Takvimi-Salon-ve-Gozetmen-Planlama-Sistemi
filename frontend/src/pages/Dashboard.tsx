import { useEffect, useRef, useState } from 'react'
import { all, fmtDateTime, get } from '../api'
import { LiveBadge, useLive } from '../live'
import { Async, Banner, Card, Dot, Empty, Pill, StackedBars, StatCard, type Tone } from '../ui'
import { useAsync } from '../ui'
import { ScheduleSelect, useExams, useFaculties, useRooms, useSchedules } from './common'

type Act = { id: number; action: string; entity: string; entity_id: string; actor?: string; created_at: string }
const LABEL: Record<string, [string, Tone]> = {
  'schedule.create': ['Takvim sürümü oluşturuldu', 'info'], 'schedule.publish': ['Takvim yayınlandı', 'ok'],
  'excuse.create': ['Mazeret bildirildi', 'warn'], 'excuse.decide': ['Mazeret karara bağlandı', 'ok'],
  'room.create': ['Salon eklendi', 'info'], 'room.update': ['Salon güncellendi', 'info'], 'room.delete': ['Salon silindi', 'err'],
  'room.change': ['Toplu salon değişikliği (tahliye)', 'err'], 'invigilator.remove': ['Gözetmen çizelgeden çıkarıldı', 'warn'],
}
const actInfo = (a: Act): [string, Tone] => (a.action.startsWith('import.') ? [`Veri içe aktarıldı: ${a.entity_id}`, 'info'] : LABEL[a.action] ?? [a.action, 'info'])

export function Dashboard() {
  const sch = useSchedules()
  const exams = useExams(sch.sel)
  const rooms = useRooms()
  const fac = useFaculties()
  const periods = useAsync(async () => (await get('/periods')).items as { id: number; name: string; exams: number; days: number; sessions_per_day: number }[])
  const pending = useLive(async () => (await all('/excuses?status=pending')).length, [], 30_000)
  const feed = useLive(async () => (await get<{ items: Act[] }>('/activity?limit=12')).items, [], 10_000)

  // yeni gelen olayları kısa süre vurgula
  const seen = useRef<Set<number>>(new Set())
  const [fresh, setFresh] = useState<Set<number>>(new Set())
  useEffect(() => {
    if (!feed.data) return
    const first = seen.current.size === 0
    const added = new Set(feed.data.filter((a) => !seen.current.has(a.id)).map((a) => a.id))
    feed.data.forEach((a) => seen.current.add(a.id))
    if (!first && added.size) {
      setFresh(added)
      const t = setTimeout(() => setFresh(new Set()), 2600)
      return () => clearTimeout(t)
    }
  }, [feed.data])

  const m = sch.current?.metrics
  const cap = rooms.data?.reduce((a, r) => a + r.capacity, 0)
  const hard = m ? m.room_violations + m.seat_violations + m.unseated : 0

  const days = new Map<number, number[]>()
  exams.data?.forEach((e) => {
    const a = days.get(e.day) ?? [0, 0, 0]
    a[e.session] = (a[e.session] ?? 0) + 1
    days.set(e.day, a)
  })
  const bars = [...days.entries()].sort((a, b) => a[0] - b[0]).map(([d, v]) => ({ label: `${d + 1}`, values: v }))
  const busiest = bars.reduce((b, x) => (x.values.reduce((a, c) => a + c, 0) > (b?.values.reduce((a, c) => a + c, 0) ?? -1) ? x : b), bars[0])

  return (
    <>
      <div className="head">
        <div>
          <div className="caps">Fakülte koordinasyonu</div>
          <h1>Bölüm sekreteri ana paneli</h1>
          <p className="muted">{periods.data?.[0]?.name ?? 'Dönem'} · sınav takvimi ve operasyon özeti</p>
        </div>
        <div className="actions">
          <LiveBadge updatedAt={feed.updatedAt} error={feed.error} />
          <ScheduleSelect s={sch} />
          <a className="btn primary" href="#/takvim">Takvim oluştur / yönet</a>
        </div>
      </div>

      {m && hard > 0 && <Banner tone="err" icon="alert" title={`${hard} sert kısıt ihlali`} actions={<a className="btn sm" href="#/takvim">Takvime git</a>}>Bu sürümde salon/oturma planı eksik; yayınlamadan önce yeniden üretin.</Banner>}
      {sch.current?.status === 'draft' && <Banner tone="warn" icon="clock" title="Seçili sürüm taslak" actions={<a className="btn sm" href="#/takvim">Yayınla</a>}>Öğrenciler, gözetmenler ve öğretim üyeleri yalnızca yayınlanmış sürümü görür.</Banner>}

      <div className="grid g4">
        <StatCard icon="calendar" tone="info" label="Sınav sayısı" value={periods.data?.[0]?.exams ?? '—'} note={periods.data?.[0] && `${periods.data[0].days} gün × ${periods.data[0].sessions_per_day} oturum`} progress={m ? 100 : 0} />
        <StatCard icon="building" tone="info" label="Salon" value={rooms.data?.length ?? '—'} note={cap ? `${cap.toLocaleString('tr-TR')} koltuk kapasitesi` : undefined} />
        <StatCard icon="users" tone={m && m.scheduling.student_days_with_two_exams > 0 ? 'warn' : 'ok'} label="Aynı gün 2 sınav" value={m ? m.scheduling.student_days_with_two_exams.toLocaleString('tr-TR') : '—'} note={m ? <>öğrenci-gün · sert ihlal <b style={{ color: hard ? 'var(--error)' : 'var(--ok)' }}>{hard}</b></> : undefined} />
        <StatCard icon="bell" tone={pending.data ? 'warn' : 'ok'} label="Bekleyen mazeret" value={pending.data ?? '—'} note={<a href="#/mazeret">Karara bağla →</a>} />
      </div>

      <Card title="Fakülteler ve bölümler" aside={<span className="small muted">her bölümde 1–4. sınıf</span>}>
        <Async s={fac} empty={(d) => d.length === 0}>
          {(list) => (
            <div className="grid g2">
              {list.map((f) => (
                <section className="card" key={f.id} style={{ boxShadow: 'none' }}>
                  <div className="row between"><h3>{f.name}</h3><Pill kind="info">{f.departments.length} bölüm</Pill></div>
                  <ul className="checks" style={{ marginTop: 10 }}>
                    {f.departments.map((d) => (
                      <li key={d.id} style={{ justifyContent: 'space-between' }}>
                        <span><b>{d.name}</b> <span className="muted small">({d.code})</span></span>
                        <span className="small muted">{d.students.toLocaleString('tr-TR')} öğrenci · {d.courses} ders</span>
                      </li>
                    ))}
                  </ul>
                  <details style={{ marginTop: 10 }}><summary className="small muted">Öğretim üyeleri ({f.lecturers.length})</summary><div className="row" style={{ marginTop: 8 }}>{f.lecturers.map((l) => <span key={l.id} className="pill">{l.full_name}</span>)}</div></details>
                </section>
              ))}
            </div>
          )}
        </Async>
      </Card>

      <Card title="Takvim durumu" aside={sch.current && <Pill kind={sch.current.status === 'published' ? 'ok' : 'warn'}><Dot tone={sch.current.status === 'published' ? 'ok' : 'warn'} />{sch.current.status === 'published' ? 'Yayında' : 'Taslak'}</Pill>}>
        <Async s={sch.list} empty={(d) => d.length === 0}>
          {() =>
            m ? (
              <div className="steps">
                <Step ok label="1. Veri ve salonlar" note={`${rooms.data?.length ?? 0} salon, ${periods.data?.[0]?.exams ?? 0} sınav`} />
                <Step ok label="2. Takvim" note={`rahatsızlık puanı ${m.penalty.toLocaleString('tr-TR')} · ${m.scheduling.used_slots} slot`} />
                <Step ok={m.unseated === 0 && hard === 0} label="3. Salon ve oturma" note={`${m.rooms_used} salon kullanımı · oturtulamayan ${m.unseated}`} />
                <Step ok={m.invigilation_unfilled === 0} label="4. Gözetmen" note={`${m.invigilation.assignments} görev · yük ${m.invigilation.min}–${m.invigilation.max}`} />
                <Step ok={sch.current?.status === 'published'} label="5. Yayın" note={sch.current?.status === 'published' ? 'öğrencilere açık' : 'taslak, yayınlanmadı'} />
              </div>
            ) : (
              <Empty title="Henüz çizelge yok" hint="İlk takvimi oluşturmak için Sınav takvimi ekranını kullanın." action={<a className="btn primary" href="#/takvim">Takvim oluştur</a>} />
            )
          }
        </Async>
      </Card>

      <div className="grid g2" style={{ alignItems: 'start' }}>
        <Card title="Günlük oturum yoğunluğu" aside={busiest && <span className="small muted">en yoğun: {busiest.label}. gün ({busiest.values.reduce((a, b) => a + b, 0)} sınav)</span>}>
          <Async s={exams} empty={() => bars.length === 0}>{() => <StackedBars data={bars} labels={['Sabah', 'Öğle', 'İkindi']} colors={['#3b4cd0', '#8f9bf0', '#cfd4fb']} />}</Async>
        </Card>

        <Card title="Son olaylar" aside={<LiveBadge updatedAt={feed.updatedAt} error={feed.error} />}>
          {feed.loading && !feed.data ? <div className="skel" /> : feed.data && feed.data.length > 0 ? (
            <ul className="feed">
              {feed.data.map((a) => {
                const [label, tone] = actInfo(a)
                return (
                  <li key={a.id} className={fresh.has(a.id) ? 'new' : ''}>
                    <Dot tone={tone} />
                    <span><b>{label}</b> <span className="muted small">#{a.entity_id}</span>{a.actor && <div className="small muted">{a.actor}</div>}</span>
                    <span className="small muted">{fmtDateTime(a.created_at)}</span>
                  </li>
                )
              })}
            </ul>
          ) : <Empty title="Henüz olay yok" />}
        </Card>
      </div>
    </>
  )
}

const Step = ({ ok, label, note }: { ok: boolean; label: string; note: string }) => (
  <div className={`step ${ok ? 'ok' : 'warn'}`}>
    <div className="row"><Pill kind={ok ? 'ok' : 'warn'}><Dot tone={ok ? 'ok' : 'warn'} />{ok ? 'Tamam' : 'Eksik'}</Pill></div>
    <h3 style={{ marginTop: 8 }}>{label}</h3>
    <div className="small muted">{note}</div>
  </div>
)
