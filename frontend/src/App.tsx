import { useCallback, useEffect, useState } from 'react'
import { API_BASE_URL, fetchHealth, type Health } from './api'

/**
 * FAZ 1 yer tutucusu.
 *
 * Gerçek ekranlar (giriş, takvim, salon/oturma planı, gözetmen listesi, canlı
 * yoklama, öğrenci programı + QR kartı) Faz 8.5'te yazılacak. Bu sayfa şu an
 * tek bir gerçek işi yapıyor: backend'in /health ucunu okuyup gösteriyor —
 * böylece compose zincirinin uçtan uca çalıştığı arayüzden de görülebiliyor.
 *
 * Yükleniyor / hata / sağlıksız durumları bilinçli olarak baştan ele alındı
 * (proje.md §5), çünkü bu desen sonraki ekranlara taşınacak.
 */
export default function App() {
  const [health, setHealth] = useState<Health | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  const load = useCallback(async (signal?: AbortSignal) => {
    setLoading(true)
    setError(null)
    try {
      setHealth(await fetchHealth(signal))
    } catch (e) {
      if ((e as Error).name === 'AbortError') return
      setHealth(null)
      setError(e instanceof Error ? e.message : 'bilinmeyen hata')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    const ac = new AbortController()
    void load(ac.signal)
    return () => ac.abort()
  }, [load])

  return (
    <div className="wrap">
      <h1>SınavPlan</h1>
      <p className="sub">
        Üniversite sınav takvimi, salon ve gözetmen planlama sistemi
      </p>

      <div className="card">
        <div className="row">
          <strong>Backend durumu</strong>
          <button onClick={() => void load()} disabled={loading}>
            {loading ? 'Yükleniyor…' : 'Yenile'}
          </button>
        </div>

        {loading && !health && !error && (
          <p className="sub" style={{ margin: '12px 0 0' }}>
            Sağlık durumu okunuyor…
          </p>
        )}

        {error && (
          <>
            <p style={{ margin: '12px 0 0' }}>
              <span className="pill err">ERİŞİLEMİYOR</span>
            </p>
            <p className="sub" style={{ margin: '8px 0 0' }}>
              {API_BASE_URL} adresine ulaşılamadı: {error}
            </p>
          </>
        )}

        {health && (
          <>
            <p style={{ margin: '12px 0 0' }}>
              <span className={health.status === 'healthy' ? 'pill ok' : 'pill err'}>
                {health.status === 'healthy' ? 'SAĞLIKLI' : 'SAĞLIKSIZ'}
              </span>
            </p>
            <dl>
              <dt>Sürüm</dt>
              <dd>{health.version}</dd>
              {Object.entries(health.dependencies).map(([name, dep]) => (
                <Dependency key={name} name={name} status={dep.status} message={dep.message} />
              ))}
            </dl>
          </>
        )}
      </div>

      <div className="card">
        <strong>Bu sayfa bir yer tutucudur</strong>
        <p className="sub" style={{ margin: '6px 0 0' }}>
          Gerçek arayüz Faz 8.5'te yazılacak. Planlanan ekranlar:
        </p>
        <ul className="todo">
          <li>Giriş (rol bazlı yönlendirme)</li>
          <li>Sınav takvimi görünümü ve yayınlama</li>
          <li>Salon ve oturma planı</li>
          <li>Gözetmen atama listesi</li>
          <li>Canlı yoklama</li>
          <li>Öğrencinin kendi programı ve QR sınav kartı</li>
        </ul>
        <p className="sub" style={{ margin: '12px 0 0' }}>
          API tabanı: <code>{API_BASE_URL}</code>
        </p>
      </div>
    </div>
  )
}

function Dependency({
  name,
  status,
  message,
}: {
  name: string
  status: string
  message?: string
}) {
  return (
    <>
      <dt>{name}</dt>
      <dd>
        <span className={status === 'healthy' ? 'pill ok' : 'pill err'}>{status}</span>
        {message ? <span className="sub"> — {message}</span> : null}
      </dd>
    </>
  )
}
