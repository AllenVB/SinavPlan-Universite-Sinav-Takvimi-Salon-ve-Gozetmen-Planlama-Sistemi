import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiError } from './api'

/** Her saniye güncellenen "şimdi" (geri sayım ve "N sn önce" göstergeleri için). */
export function useNow(intervalMs = 1000) {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), intervalMs)
    return () => clearInterval(t)
  }, [intervalMs])
  return now
}

const visible = () => typeof document === 'undefined' || document.visibilityState === 'visible'

/**
 * Canlı veri: ilk yüklemeden sonra her `intervalMs`'de yeniler; sekme gizliyken durur, tekrar
 * görününce hemen yeniler; hata olursa aralığı katlayarak (en çok 30 sn) geri çekilir ve veriyi korur.
 * `updatedAt` son BAŞARILI yenileme zamanıdır — "Son güncelleme" göstergesi buna dayanır.
 */
export function useLive<T>(fn: () => Promise<T>, deps: unknown[], intervalMs: number, enabled = true) {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<ApiError | null>(null)
  const [loading, setLoading] = useState(true)
  const [updatedAt, setUpdatedAt] = useState<number | null>(null)
  const seq = useRef(0)
  const fails = useRef(0)
  const timer = useRef<ReturnType<typeof setTimeout>>()
  const fnRef = useRef(fn)
  fnRef.current = fn

  const load = useCallback(async () => {
    const id = ++seq.current
    try {
      const d = await fnRef.current()
      if (id !== seq.current) return
      fails.current = 0
      setData(d)
      setError(null)
      setUpdatedAt(Date.now())
    } catch (e) {
      if (id !== seq.current) return
      fails.current++
      setError(e instanceof ApiError ? e : new ApiError(0, 'error', String(e)))
    } finally {
      if (id === seq.current) setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (!enabled) return
    let stopped = false
    setLoading(true)
    const tick = async () => {
      if (stopped) return
      if (visible()) await load()
      if (stopped) return
      const wait = Math.min(30_000, intervalMs * 2 ** Math.min(fails.current, 5))
      timer.current = setTimeout(tick, wait)
    }
    void tick()
    const onVis = () => {
      if (visible() && !stopped) {
        clearTimeout(timer.current)
        void tick()
      }
    }
    document.addEventListener('visibilitychange', onVis)
    return () => {
      stopped = true
      seq.current++
      clearTimeout(timer.current)
      document.removeEventListener('visibilitychange', onVis)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, intervalMs, enabled])

  return { data, error, loading, updatedAt, reload: load }
}

/** "Canlı • Son güncelleme: N sn önce" — hata varsa kırmızı "Bağlantı yok", sekme gizliyse "Duraklatıldı". */
export function LiveBadge({ updatedAt, error }: { updatedAt: number | null; error?: ApiError | null }) {
  const now = useNow()
  const hidden = !visible()
  const sec = updatedAt ? Math.max(0, Math.round((now - updatedAt) / 1000)) : null
  const tone = error ? 'err' : hidden ? 'warn' : 'ok'
  const text = error ? 'Bağlantı yok' : hidden ? 'Duraklatıldı' : 'Canlı'
  return (
    <span className={`livebadge ${tone}`} role="status" aria-live="off">
      <i className={`dot ${tone} ${tone === 'ok' ? 'pulse' : ''}`} />
      <b>{text}</b>
      <span className="muted small">{sec === null ? 'yükleniyor…' : `Son güncelleme: ${sec} sn önce`}</span>
    </span>
  )
}
