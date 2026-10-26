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

export const Bar = ({ pct, label = 'Doluluk' }: { pct: number; label?: string }) => (
  <div className={`bar ${pct >= 95 ? 'err' : pct >= 80 ? 'warn' : ''}`} role="progressbar" aria-label={label} aria-valuenow={Math.round(pct)} aria-valuemin={0} aria-valuemax={100}>
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

// ---- Tasarım sistemi v2: canlı bileşenler -----------------------------------------------
import { Icon, type IconName } from './icons'

export type Tone = 'ok' | 'warn' | 'err' | 'info' | 'mute'

/** Durum ışığı (yeşil/sarı/kırmızı nokta). */
export const Dot = ({ tone = 'ok', pulse }: { tone?: Tone; pulse?: boolean }) => <i className={`dot ${tone} ${pulse ? 'pulse' : ''}`} aria-hidden="true" />

/** Noktalı durum çipi: "● Geldi". */
export const Status = ({ tone = 'mute', children }: { tone?: Tone; children: ReactNode }) => (
  <span className={`pill ${tone === 'mute' ? '' : tone}`}>
    <Dot tone={tone} />
    {children}
  </span>
)

/** İkonlu istatistik kartı; isteğe bağlı ilerleme çubuğu. */
export function StatCard({ icon, tone = 'info', label, value, note, progress }: { icon: IconName; tone?: Tone; label: string; value: ReactNode; note?: ReactNode; progress?: number }) {
  return (
    <div className="card stat2">
      <div className="row between">
        <span className="caps">{label}</span>
        <span className={`ibadge ${tone}`}><Icon name={icon} size={18} /></span>
      </div>
      <div className="v">{value}</div>
      {progress !== undefined && <div className={`bar ${tone === 'err' ? 'err' : tone === 'warn' ? 'warn' : tone === 'ok' ? 'ok' : ''}`} role="progressbar" aria-label={label} aria-valuenow={Math.round(progress)} aria-valuemin={0} aria-valuemax={100}><i style={{ width: `${Math.max(0, Math.min(100, progress))}%` }} /></div>}
      {note && <div className="small muted" style={{ marginTop: 6 }}>{note}</div>}
    </div>
  )
}

/** Kenarlıklı uyarı şeridi (kırmızı alarm, turuncu çevrimdışı, mavi bilgi, yeşil başarı). */
export function Banner({ tone = 'info', icon, title, children, actions }: { tone?: Tone; icon?: IconName; title: ReactNode; children?: ReactNode; actions?: ReactNode }) {
  return (
    <div className={`banner ${tone}`} role={tone === 'err' ? 'alert' : 'status'}>
      {icon && <span className="bicon"><Icon name={icon} size={22} /></span>}
      <div style={{ flex: 1, minWidth: 0 }}>
        <b>{title}</b>
        {children && <div className="small">{children}</div>}
      </div>
      {actions && <div className="actions">{actions}</div>}
    </div>
  )
}

/** Baş harflerden yuvarlak avatar; renk addan türetilir (kararlı). */
export function Avatar({ name, size = 32 }: { name: string; size?: number }) {
  const parts = name.trim().split(/\s+/).filter(Boolean)
  const ini = ((parts[0]?.[0] ?? '?') + (parts.length > 1 ? parts[parts.length - 1][0] : '')).toLocaleUpperCase('tr-TR')
  let h = 0
  for (const c of name) h = (h * 31 + c.charCodeAt(0)) % 360
  return (
    <span className="avatar" style={{ width: size, height: size, fontSize: size * 0.4, background: `hsl(${h} 70% 92%)`, color: `hsl(${h} 55% 30%)` }} aria-hidden="true">
      {ini}
    </span>
  )
}

/** Parçalı çubuk: örn. geldi (yeşil) / gelmedi (kırmızı) / işaretsiz (gri). */
export function SegBar({ parts }: { parts: { value: number; tone: Tone; label: string }[] }) {
  const total = parts.reduce((a, p) => a + p.value, 0) || 1
  return (
    <div className="segbar" role="img" aria-label={parts.map((p) => `${p.label}: ${p.value}`).join(', ')}>
      {parts.map((p) => p.value > 0 && <i key={p.label} className={p.tone} style={{ width: `${(p.value / total) * 100}%` }} title={`${p.label}: ${p.value}`} />)}
    </div>
  )
}

/** Geri sayım halkası (0..1 arası kalan oran). */
export function Ring({ fraction, size = 52, children }: { fraction: number; size?: number; children?: ReactNode }) {
  const r = (size - 8) / 2
  const c = 2 * Math.PI * r
  const f = Math.max(0, Math.min(1, fraction))
  return (
    <span className="ring" style={{ width: size, height: size }}>
      <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} aria-hidden="true">
        <circle cx={size / 2} cy={size / 2} r={r} fill="none" stroke="var(--container)" strokeWidth="5" />
        <circle cx={size / 2} cy={size / 2} r={r} fill="none" stroke={f < 0.2 ? 'var(--error)' : 'var(--primary)'} strokeWidth="5" strokeLinecap="round" strokeDasharray={c} strokeDashoffset={c * (1 - f)} transform={`rotate(-90 ${size / 2} ${size / 2})`} style={{ transition: 'stroke-dashoffset .9s linear' }} />
      </svg>
      <span className="ringlabel">{children}</span>
    </span>
  )
}

/** Gün başına yığılmış sütun grafiği (SVG yok; erişilebilir tablo özeti ile). */
export function StackedBars({ data, labels, colors, height = 150 }: { data: { label: string; values: number[] }[]; labels: string[]; colors: string[]; height?: number }) {
  const max = Math.max(1, ...data.map((d) => d.values.reduce((a, b) => a + b, 0)))
  return (
    <div>
      <div className="legend" style={{ marginBottom: 8 }}>{labels.map((l, i) => <span key={l}><i style={{ background: colors[i] }} />{l}</span>)}</div>
      <div className="sbars" style={{ height }} role="img" aria-label="Günlük oturum yoğunluğu">
        {data.map((d) => {
          const total = d.values.reduce((a, b) => a + b, 0)
          return (
            <div key={d.label} className="sbar" title={`${d.label}: ${total} sınav (${d.values.join(' / ')})`}>
              <span className="sval">{total}</span>
              <div className="sstack" style={{ height: `${(total / max) * 100}%` }}>
                {d.values.map((v, i) => v > 0 && <i key={i} style={{ flex: v, background: colors[i] }} />)}
              </div>
              <span className="slab">{d.label}</span>
            </div>
          )
        })}
      </div>
    </div>
  )
}

export type Quality = { label: string; iterations: number }

/**
 * Takvim hesabının iki ayarı, sade adlarla: "Varyasyon no" (aynı numara = aynı takvim; farklı numara farklı çözüm
 * dener) ve "Hesaplama kalitesi" (daha yüksek = daha uzun sürer, daha rahat takvim). API'deki seed/iterations alanlarına eşlenir.
 */
export function TuneFields({ seed, setSeed, quality, setQuality, presets, idPrefix, frontLoad, setFrontLoad }: { seed: string; setSeed: (v: string) => void; quality: number; setQuality: (v: number) => void; presets: Quality[]; idPrefix: string; frontLoad?: number; setFrontLoad?: (v: number) => void }) {
  // Varsayılan akış tek düğmedir; ince ayarlar kapalı başlayan "Gelişmiş ayarlar" altındadır.
  return (
    <details className="adv">
      <summary>Gelişmiş ayarlar</summary>
      <div className="row" style={{ alignItems: 'flex-end' }}>
        <Field label="Varyasyon no" id={`${idPrefix}-seed`}>
          <div className="row" style={{ flexWrap: 'nowrap' }}>
            <input id={`${idPrefix}-seed`} className="input" inputMode="numeric" value={seed} onChange={(e) => setSeed(e.target.value)} style={{ width: 90 }} aria-describedby={`${idPrefix}-help`} />
            <button type="button" className="btn sm" title="Rastgele bir numara seç" onClick={() => setSeed(String(1 + Math.floor(Math.random() * 999)))}>Rastgele</button>
          </div>
        </Field>
        <Field label="Hesaplama kalitesi" id={`${idPrefix}-q`}>
          <select id={`${idPrefix}-q`} value={quality} onChange={(e) => setQuality(Number(e.target.value))} style={{ width: 'auto' }}>
            {presets.map((p) => <option key={p.iterations} value={p.iterations}>{p.label}</option>)}
          </select>
        </Field>
        {setFrontLoad && (
          <Field label="Büyük sınavlar" id={`${idPrefix}-front`}>
            <select id={`${idPrefix}-front`} value={frontLoad ?? 0} onChange={(e) => setFrontLoad(Number(e.target.value))} style={{ width: 'auto' }}>
              <option value={0}>Fark etmez (varsayılan)</option>
              <option value={5}>Hafifçe öne al</option>
              <option value={20}>Güçlü öne al</option>
            </select>
          </Field>
        )}
        <p id={`${idPrefix}-help`} className="small muted" style={{ flexBasis: '100%', margin: 0 }}>
          <b>Varyasyon no:</b> aynı numara her zaman aynı takvimi üretir; farklı numara aynı kurallara uyan başka bir çözüm dener.{' '}
          <b>Hesaplama kalitesi:</b> yükseldikçe hesap uzar, takvim öğrenciler için daha rahat (daha düşük rahatsızlık puanı) olur.{setFrontLoad && <> <b>Büyük sınavlar:</b> 100+ öğrencili sınavları takvimin başına çeker (not girişi erken biter); öğrenci rahatlığı biraz azalabilir.</>}
        </p>
      </div>
    </details>
  )
}
