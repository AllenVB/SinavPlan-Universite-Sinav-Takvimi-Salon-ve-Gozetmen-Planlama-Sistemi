import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { ApiError } from './api'

/** Yükleniyor / hata / veri durumlarını tek yerde yöneten veri çekme kancası. */
export function useAsync<T>(fn: () => Promise<T>, deps: unknown[] = []) {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<ApiError | null>(null)
  const [loading, setLoading] = useState(true)
  const seq = useRef(0)
  const run = useCallback(() => {
    const id = ++seq.current
    setLoading(true)
    setError(null)
    fn().then(
      (d) => id === seq.current && (setData(d), setLoading(false)),
      (e) => id === seq.current && (setError(e instanceof ApiError ? e : new ApiError(0, 'error', String(e))), setLoading(false)),
    )
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)
  useEffect(run, [run])
  return { data, error, loading, reload: run, setData }
}

export const Skeleton = ({ rows = 4 }: { rows?: number }) => (
  <div aria-busy="true" aria-label="Yükleniyor">
    {Array.from({ length: rows }, (_, i) => (
      <div className="skel" key={i} style={{ width: `${95 - (i % 3) * 18}%` }} />
    ))}
  </div>
)

export const Empty = ({ title, hint, action }: { title: string; hint?: string; action?: ReactNode }) => (
  <div className="empty">
    <b>{title}</b>
    {hint && <div className="small">{hint}</div>}
    {action && <div style={{ marginTop: 12 }}>{action}</div>}
  </div>
)

export const ErrorBox = ({ error, retry }: { error: ApiError | string; retry?: () => void }) => {
  const e = typeof error === 'string' ? null : error
  const unavailable = e && (e.status === 503 || e.status === 0)
  return (
    <div className="alert err" role="alert">
      <div style={{ flex: 1 }}>
        <b>{unavailable ? 'Sunucuya şu an ulaşılamıyor' : 'İşlem tamamlanamadı'}</b>
        <div>{typeof error === 'string' ? error : error.message}</div>
        {e?.fields && (
          <ul style={{ margin: '6px 0 0', paddingLeft: 18 }}>
            {Object.entries(e.fields).map(([k, v]) => (
              <li key={k}>
                {k}: {v}
              </li>
            ))}
          </ul>
        )}
      </div>
      {retry && (
        <button className="btn sm" onClick={retry}>
          Tekrar dene
        </button>
      )}
    </div>
  )
}

/** Bir veri durumunu (yükleniyor/hata/boş/dolu) çizer. */
export function Async<T>({ s, empty, children }: { s: ReturnType<typeof useAsync<T>>; empty?: (d: T) => boolean; children: (d: T) => ReactNode }) {
  if (s.loading && !s.data) return <Skeleton />
  if (s.error) return <ErrorBox error={s.error} retry={s.reload} />
  if (!s.data) return null
  if (empty?.(s.data)) return <Empty title="Kayıt yok" hint="Gösterilecek veri bulunamadı." />
  return <>{children(s.data)}</>
}

export const Pill = ({ kind = '', children }: { kind?: 'ok' | 'warn' | 'err' | 'info' | ''; children: ReactNode }) => <span className={`pill ${kind}`}>{children}</span>

export const Card = ({ title, aside, children }: { title?: ReactNode; aside?: ReactNode; children: ReactNode }) => (
  <section className="card">
    {(title || aside) && (
      <div className="ch">
        <h2>{title}</h2>
        {aside}
      </div>
    )}
    {children}
  </section>
)

export const Stat = ({ label, value, note }: { label: string; value: ReactNode; note?: ReactNode }) => (
  <div className="card stat">
    <div className="caps">{label}</div>
    <div className="v">{value}</div>
    {note && <div className="small muted">{note}</div>}
  </div>
)

export const Field = ({ label, error, children, id }: { label: string; error?: string; children: ReactNode; id: string }) => (
  <div className="field">
    <label htmlFor={id}>{label}</label>
    {children}
    {error && <span className="err-text">{error}</span>}
  </div>
)

export function Tabs<T extends string>({ value, onChange, items }: { value: T; onChange: (v: T) => void; items: [T, string][] }) {
  return (
    <div className="tabs" role="group">
      {items.map(([k, l]) => (
        <button key={k} aria-pressed={value === k} onClick={() => onChange(k)}>
          {l}
        </button>
      ))}
    </div>
  )
}

export const Bar = ({ pct }: { pct: number }) => (
  <div className={`bar ${pct >= 95 ? 'err' : pct >= 80 ? 'warn' : ''}`} role="progressbar" aria-valuenow={Math.round(pct)} aria-valuemin={0} aria-valuemax={100}>
    <i style={{ width: `${Math.min(100, pct)}%` }} />
  </div>
)

/** Bir eylem çalıştırır; yükleniyor/hata/başarı durumunu tutar. */
export function useAction<A extends unknown[], R>(fn: (...a: A) => Promise<R>) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<ApiError | null>(null)
  const [result, setResult] = useState<R | null>(null)
  const run = async (...a: A) => {
    setBusy(true)
    setError(null)
    try {
      const r = await fn(...a)
      setResult(r)
      return r
    } catch (e) {
      setError(e instanceof ApiError ? e : new ApiError(0, 'error', String(e)))
      return null
    } finally {
      setBusy(false)
    }
  }
  return { run, busy, error, result, reset: () => (setError(null), setResult(null)) }
}
