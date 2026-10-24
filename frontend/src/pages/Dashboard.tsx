import { all, get } from '../api'
import { Async, Bar, Card, Empty, Pill, Stat, useAsync } from '../ui'
import { useExams, useRooms, useSchedules, ScheduleSelect } from './common'

export function Dashboard() {
  const sch = useSchedules()
  const exams = useExams(sch.sel)
  const rooms = useRooms()
  const periods = useAsync(async () => (await get('/periods')).items as { id: number; name: string; exams: number; days: number; sessions_per_day: number }[])
  const pending = useAsync(async () => (await all('/excuses?status=pending')).length)
  const m = sch.current?.metrics
  const cap = rooms.data?.reduce((a, r) => a + r.capacity, 0)

  // gün × oturum yoğunluğu (takvimden hesaplanır)
  const days = new Map<number, number[]>()
  exams.data?.forEach((e) => {
    const a = days.get(e.day) ?? [0, 0, 0]
    a[e.session] = (a[e.session] ?? 0) + 1
    days.set(e.day, a)
  })
  const rows = [...days.entries()].sort((a, b) => a[0] - b[0])
  const max = Math.max(1, ...rows.map(([, a]) => a.reduce((x, y) => x + y, 0)))

  return (
    <>
      <div className="head">
        <div>
          <div className="caps">Bölüm koordinasyonu</div>
          <h1>Bölüm sekreteri ana paneli</h1>
          <p className="muted">{periods.data?.[0]?.name ?? 'Dönem'} · sınav takvimi ve operasyon özeti</p>
        </div>
        <div className="actions">
          <ScheduleSelect s={sch} />
          <a className="btn primary" href="#/takvim">Takvim oluştur / yönet</a>
        </div>
      </div>

      <div className="grid g4">
        <Stat label="Sınav sayısı" value={periods.data?.[0]?.exams ?? '—'} note={periods.data?.[0] && `${periods.data[0].days} gün × ${periods.data[0].sessions_per_day} oturum`} />
        <Stat label="Salon" value={rooms.data?.length ?? '—'} note={cap ? `${cap.toLocaleString('tr-TR')} koltuk kapasitesi` : undefined} />
        <Stat label="Aynı gün 2 sınavı olan öğrenci-gün" value={m ? m.scheduling.student_days_with_two_exams.toLocaleString('tr-TR') : '—'} note={m && `sert ihlal: ${m.room_violations + m.seat_violations}`} />
        <Stat label="Bekleyen mazeret" value={pending.data ?? '—'} note={<a href="#/mazeret">Karara bağla →</a>} />
      </div>

      <Card title="Takvim durumu" aside={sch.current && <Pill kind={sch.current.status === 'published' ? 'ok' : 'warn'}>{sch.current.status === 'published' ? 'Yayında' : 'Taslak'}</Pill>}>
        <Async s={sch.list} empty={(d) => d.length === 0}>
          {() =>
            m ? (
              <div className="grid g4">
                <Step ok label="1. Veri ve salonlar" note={`${rooms.data?.length ?? 0} salon`} />
                <Step ok label="2. Takvim" note={`ceza ${m.penalty.toLocaleString('tr-TR')} · ${m.scheduling.used_slots} slot`} />
                <Step ok={m.unseated === 0} label="3. Salon ve oturma" note={`${m.rooms_used} salon kullanımı · oturtulamayan ${m.unseated}`} />
                <Step ok={m.invigilation_unfilled === 0} label="4. Gözetmen" note={`${m.invigilation.assignments} görev · yük ${m.invigilation.min}–${m.invigilation.max}`} />
              </div>
            ) : (
              <Empty title="Henüz çizelge yok" hint="İlk takvimi oluşturmak için Sınav takvimi ekranını kullanın." action={<a className="btn primary" href="#/takvim">Takvim oluştur</a>} />
            )
          }
        </Async>
      </Card>

      <Card title="Günlük oturum yoğunluğu" aside={<span className="small muted">sınav sayısı / gün</span>}>
        <Async s={exams} empty={() => rows.length === 0}>
          {() => (
            <div className="tablewrap">
              <table>
                <thead><tr><th>Gün</th><th>Sabah</th><th>Öğle</th><th>İkindi</th><th style={{ width: '40%' }}>Toplam</th></tr></thead>
                <tbody>
                  {rows.map(([d, a]) => {
                    const t = a.reduce((x, y) => x + y, 0)
                    return (
                      <tr key={d}>
                        <td>{d + 1}. gün</td><td>{a[0] ?? 0}</td><td>{a[1] ?? 0}</td><td>{a[2] ?? 0}</td>
                        <td><div className="row" style={{ flexWrap: 'nowrap' }}><div style={{ flex: 1 }}><Bar pct={(t / max) * 100} /></div><b>{t}</b></div></td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          )}
        </Async>
      </Card>
    </>
  )
}

const Step = ({ ok, label, note }: { ok: boolean; label: string; note: string }) => (
  <div className="card" style={{ boxShadow: 'none' }}>
    <div className="row"><Pill kind={ok ? 'ok' : 'warn'}>{ok ? 'Tamam' : 'Eksik'}</Pill></div>
    <h3 style={{ marginTop: 6 }}>{label}</h3>
    <div className="small muted">{note}</div>
  </div>
)
