import { useState, type FormEvent } from 'react'
import { fetchHealth } from '../api'
import { useAuth } from '../auth'
import { ErrorBox, Field, Pill, useAction, useAsync } from '../ui'

// Demo hesapları: rol, e-posta ve o rolün sistemde ne yaptığı (giriş ekranında tek tıkla doldurulur)
const DEMO = [
  ['Bölüm sekreteri', 'seed.mercan@ddo-case.invalid', 'Takvim üretir/yayınlar, salon ve gözetmen planını yönetir'],
  ['Öğretim üyesi', 'hoca@sinavplan.test', 'Kendi derslerinin takvimini ve oturma planını görür'],
  ['Gözetmen', 'gozetmen@sinavplan.test', 'Görevlerini görür, QR/6 haneli kodla yoklama alır'],
  ['Öğrenci işleri', 'ogrenciisleri@sinavplan.test', 'Mazeretleri karara bağlar, sınav gününü izler'],
  ['Öğrenci', 'ogrenci@sinavplan.test', 'Sınav programı, sınav kartı ve kodla yoklama (demo kodu 111111)'],
]

export function Login() {
  const { login } = useAuth()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [show, setShow] = useState(false)
  const health = useAsync(fetchHealth)
  const act = useAction(() => login(email.trim(), password))
  const submit = (e: FormEvent) => {
    e.preventDefault()
    void act.run()
  }
  const bad = act.error?.status === 401
  return (
    <div className="login">
      <div className="box">
        <form className="l" onSubmit={submit} noValidate>
          <div className="row">
            <div className="logo" aria-hidden>S</div>
            <div>
              <b style={{ color: 'var(--primary)' }}>SınavPlan</b>
              <div className="small muted">Üniversite sınav, salon ve gözetmen yönetim sistemi</div>
            </div>
          </div>
          <h1>Sisteme giriş yap</h1>
          <p className="muted">Sınav takvimini yönetmek, görevlerinizi veya sınavlarınızı görmek için hesabınızla giriş yapın.</p>
          <Field label="E-posta" id="email">
            <input id="email" className="input" type="email" autoComplete="username" value={email} onChange={(e) => setEmail(e.target.value)} required aria-invalid={bad} />
          </Field>
          <Field label="Parola" id="pw">
            <div className="row" style={{ flexWrap: 'nowrap' }}>
              <input id="pw" className="input" type={show ? 'text' : 'password'} autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} required aria-invalid={bad} />
              <button type="button" className="btn" onClick={() => setShow(!show)} aria-pressed={show}>
                {show ? 'Gizle' : 'Göster'}
              </button>
            </div>
          </Field>
          {act.error && (bad ? <div className="alert err" role="alert"><div><b>Giriş başarısız:</b> e-posta veya parola hatalı.</div></div> : <ErrorBox error={act.error} />)}
          <button className="btn primary" disabled={act.busy || !email || !password} style={{ padding: 12 }}>
            {act.busy ? 'Giriş yapılıyor…' : 'Giriş yap'}
          </button>
          <div>
            <div className="caps" style={{ marginBottom: 4 }}>Demo hesapları — parola: Demo!1234</div>
            <p className="small muted" style={{ margin: '0 0 8px' }}>Bir role tıklayın: <b>e-posta ve parola otomatik dolar</b>, sonra <b>Giriş yap</b>'a basın. Her rol yalnızca kendi ekranlarını görür; içerdeyken üst çubuktaki <b>Tur</b> sistemi adım adım tanıtır.</p>
            <div className="demoaccts">
              {DEMO.map(([n, m, d]) => (
                <button type="button" key={m} className={`btn sm demoacct ${email === m ? 'on' : ''}`} aria-pressed={email === m} onClick={() => (setEmail(m), setPassword('Demo!1234'))} title={`${m} ile doldur`}>
                  <b>{n}</b>
                  <span className="small muted">{d}</span>
                </button>
              ))}
            </div>
          </div>
        </form>
        <div className="r">
          <Pill kind={health.error ? 'err' : health.data?.status === 'healthy' ? 'ok' : 'warn'}>
            {health.error ? 'Sunucuya ulaşılamıyor' : health.data ? (health.data.status === 'healthy' ? 'Sistem çalışıyor' : 'Sistem sağlıksız') : 'Kontrol ediliyor…'}
          </Pill>
          <h2 style={{ fontSize: '1.5rem', lineHeight: '2rem' }}>Çakışmasız sınav dağıtımı</h2>
          <p className="muted">Salon kapasiteleri, ortak öğrenciler ve gözetmen adaleti tek merkezde: takvim, oturma planı ve gözetmen ataması otomatik üretilir ve bağımsız olarak doğrulanır.</p>
          <div className="kpis">
            <div><div className="small muted">Takvim</div><b>Çakışmasız</b></div>
            <div><div className="small muted">Oturma</div><b>Yan yana yok</b></div>
            <div><div className="small muted">Gözetmen</div><b>Adil yük</b></div>
          </div>
        </div>
      </div>
    </div>
  )
}
