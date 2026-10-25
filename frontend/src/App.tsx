import { useEffect, useState, type ComponentType } from 'react'
import { fetchHealth, get } from './api'
import { Avatar, Dot, useAsync } from './ui'
import { Icon, type IconName } from './icons'
import { LiveBadge, useLive } from './live'
import { AuthProvider, useAuth } from './auth'
import { Login } from './pages/Login'
import { Dashboard } from './pages/Dashboard'
import { Calendar } from './pages/Calendar'
import { Editor } from './pages/Editor'
import { Rooms } from './pages/Rooms'
import { Invigilators } from './pages/Invigilators'
import { Conflicts } from './pages/Conflicts'
import { Excuses } from './pages/Excuses'
import { Imports } from './pages/Imports'
import { Live } from './pages/Live'
import { Tablet } from './pages/Tablet'
import { MyExams } from './pages/MyExams'
import type { Role } from './api'

type Route = { path: string; label: string; roles: Role[]; page: ComponentType; icon: IconName }
const ALL: Role[] = ['secretary', 'lecturer', 'invigilator', 'student_affairs', 'student']

const routes: Route[] = [
  { path: 'programim', label: 'Sınav programım', roles: ['student'], page: MyExams, icon: 'calendar' },
  { path: 'yoklama', label: 'Yoklama ve görevlerim', roles: ['invigilator'], page: Tablet, icon: 'qr' },
  { path: 'panel', label: 'Ana panel', roles: ['secretary'], page: Dashboard, icon: 'building' },
  { path: 'takvim', label: 'Sınav takvimi', roles: ['secretary', 'student_affairs', 'lecturer'], page: Calendar, icon: 'calendar' },
  { path: 'duzenle', label: 'Takvim editörü', roles: ['secretary'], page: Editor, icon: 'edit' },
  { path: 'salonlar', label: 'Salon ve oturma planı', roles: ['secretary', 'lecturer', 'student_affairs', 'invigilator'], page: Rooms, icon: 'seat' },
  { path: 'gozetmenler', label: 'Gözetmen atama', roles: ['secretary', 'student_affairs'], page: Invigilators, icon: 'users' },
  { path: 'cakisma', label: 'Çakışma masası', roles: ['secretary', 'student_affairs'], page: Conflicts, icon: 'alert' },
  { path: 'mazeret', label: 'Mazeretler', roles: ['secretary', 'student_affairs', 'student', 'invigilator'], page: Excuses, icon: 'mail' },
  { path: 'ice-aktar', label: 'Veri içe aktarma', roles: ['secretary'], page: Imports, icon: 'upload' },
  { path: 'canli', label: 'Sınav günü izleme', roles: ['secretary', 'student_affairs'], page: Live, icon: 'zap' },
]

const roleName: Record<Role, string> = { secretary: 'Bölüm sekreteri', lecturer: 'Öğretim üyesi', invigilator: 'Gözetmen', student_affairs: 'Öğrenci işleri', student: 'Öğrenci' }

function useHash() {
  const [h, setH] = useState(() => location.hash.replace(/^#\/?/, ''))
  useEffect(() => {
    const f = () => setH(location.hash.replace(/^#\/?/, ''))
    addEventListener('hashchange', f)
    return () => removeEventListener('hashchange', f)
  }, [])
  return h
}

function Shell() {
  const { user, logout } = useAuth()
  const hash = useHash()
  const canPending = user!.roles.includes('secretary') || user!.roles.includes('student_affairs')
  const health = useLive(fetchHealth, [], 15_000)
  const pending = useLive(async () => (await get<{ items: unknown[] }>('/excuses?status=pending&limit=100')).items.length, [canPending], 30_000, canPending)
  const period = useAsync(async () => (user!.roles.includes('student') ? null : ((await get('/periods')).items?.[0]?.name as string | undefined) ?? null))
  const roles = user!.roles.length ? user!.roles : ALL.slice(0, 0)
  const mine = routes.filter((r) => r.roles.some((x) => roles.includes(x)))
  const cur = mine.find((r) => r.path === hash) ?? mine[0]
  useEffect(() => {
    if (cur && cur.path !== hash) location.hash = `/${cur.path}`
  }, [cur, hash])
  if (!cur) return <main className="page"><div className="alert err">Bu hesaba tanımlı bir ekran yok.</div></main>
  const Page = cur.page
  const nav = mine.map((r) => (
    <a key={r.path} href={`#/${r.path}`} aria-current={r.path === cur.path ? 'page' : undefined}>
      <Icon name={r.icon} size={17} />
      <span>{r.label}</span>
    </a>
  ))
  return (
    <div className="shell">
      <aside className="side">
        <div className="brand">
          <div className="logo" aria-hidden>S</div>
          <div>
            <b>SınavPlan</b>
            <span className="caps">Yönetim portalı</span>
          </div>
        </div>
        <nav className="nav" aria-label="Ana menü">{nav}</nav>
        <div className="foot small muted"><Dot tone={health.error ? 'err' : 'ok'} pulse={!health.error} /> {health.error ? 'Sunucuya ulaşılamıyor' : 'Sistem çalışıyor'}</div>
      </aside>
      <div className="main">
        <header className="top">
          <span className="caps grow">{period.data ?? ''}</span>
          <div className="topbar-right">
            <LiveBadge updatedAt={health.updatedAt} error={health.error ?? (health.data && health.data.status !== 'healthy' ? ({ status: 503 } as never) : null)} />
            {canPending && (
              <a className="bellbtn" href="#/mazeret" aria-label={`Bekleyen mazeret: ${pending.data ?? 0}`} title="Bekleyen mazeretler">
                <Icon name="bell" size={18} />
                {!!pending.data && <span className="count">{pending.data > 99 ? '99+' : pending.data}</span>}
              </a>
            )}
            <div className="userbox">
              <div className="who"><b className="small">{user!.full_name ?? `Kullanıcı #${user!.id}`}</b><div className="small muted">{roles.map((r) => roleName[r]).join(', ')}</div></div>
              <Avatar name={user!.full_name ?? roles.map((r) => roleName[r]).join(' ')} size={36} />
            </div>
            <button className="btn sm" onClick={() => void logout()}>Çıkış</button>
          </div>
        </header>
        <main className="page">
          <Page key={cur.path} />
        </main>
      </div>
      <nav className="bottomnav" aria-label="Mobil menü">{nav}</nav>
    </div>
  )
}

function Gate() {
  const { user, ready } = useAuth()
  if (!ready) return <div className="login"><div className="skel" style={{ width: 240 }} /></div>
  return user ? <Shell /> : <Login />
}

export default function App() {
  return (
    <AuthProvider>
      <Gate />
    </AuthProvider>
  )
}
