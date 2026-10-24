// API istemcisi: JWT + tek seferlik refresh yenileme, ortak hata biçimi, keyset sayfalama.
// API taban adresi derleme sırasında enjekte edilir (compose: VITE_API_BASE_URL, varsayılan /api).
export const API_BASE_URL: string = import.meta.env.VITE_API_BASE_URL ?? '/api'

export type Role = 'secretary' | 'lecturer' | 'invigilator' | 'student_affairs' | 'student'
export type User = { id: number; roles: Role[]; student_id?: number; lecturer_id?: number }

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public fields?: Record<string, string>,
  ) {
    super(message)
  }
}

type Tokens = { access: string; refresh: string }
const KEY = 'sinavplan.tokens'
let tokens: Tokens | null = null
try {
  tokens = JSON.parse(sessionStorage.getItem(KEY) ?? 'null')
} catch {
  tokens = null
}
let onLogout: () => void = () => {}
export const setLogoutHandler = (f: () => void) => (onLogout = f)
export const hasSession = () => tokens !== null

function save(t: Tokens | null) {
  tokens = t
  try {
    if (t) sessionStorage.setItem(KEY, JSON.stringify(t))
    else sessionStorage.removeItem(KEY)
  } catch {
    /* depolama kapalı olabilir; bellekte devam */
  }
}

async function raw(method: string, path: string, body?: unknown, extra?: HeadersInit, ctype = 'application/json') {
  const headers = new Headers(extra)
  if (tokens) headers.set('Authorization', `Bearer ${tokens.access}`)
  let payload: BodyInit | undefined
  if (body !== undefined) {
    headers.set('Content-Type', ctype)
    payload = typeof body === 'string' ? body : JSON.stringify(body)
  }
  try {
    return await fetch(`${API_BASE_URL}${path}`, { method, headers, body: payload })
  } catch {
    throw new ApiError(0, 'network', 'Sunucuya ulaşılamadı. Bağlantınızı kontrol edip tekrar deneyin.')
  }
}

let refreshing: Promise<boolean> | null = null
function refresh(): Promise<boolean> {
  if (!tokens) return Promise.resolve(false)
  refreshing ??= (async () => {
    try {
      const r = await fetch(`${API_BASE_URL}/auth/refresh`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ refresh_token: tokens!.refresh }),
      })
      if (!r.ok) return false
      const j = await r.json()
      save({ access: j.access_token, refresh: j.refresh_token })
      return true
    } catch {
      return false
    } finally {
      refreshing = null
    }
  })()
  return refreshing
}

export async function request<T = any>(method: string, path: string, body?: unknown, opts: { headers?: HeadersInit; ctype?: string } = {}): Promise<T> {
  let res = await raw(method, path, body, opts.headers, opts.ctype)
  if (res.status === 401 && tokens && !path.startsWith('/auth/')) {
    if (await refresh()) res = await raw(method, path, body, opts.headers, opts.ctype)
    else {
      save(null)
      onLogout()
    }
  }
  const text = await res.text()
  let json: any = null
  try {
    json = text ? JSON.parse(text) : null
  } catch {
    /* gövde JSON değil */
  }
  if (!res.ok) {
    const e = json?.error
    throw new ApiError(res.status, e?.code ?? 'error', e?.message ?? `İstek başarısız (HTTP ${res.status})`, e?.fields)
  }
  return json as T
}

export const get = <T = any>(p: string) => request<T>('GET', p)
export const post = <T = any>(p: string, b?: unknown, headers?: HeadersInit) => request<T>('POST', p, b ?? undefined, { headers })

/** Keyset sayfalı bir listeyi sonuna kadar (üst sınırlı) okur. */
export async function all<T = any>(path: string, max = 5000): Promise<T[]> {
  const out: T[] = []
  let after = ''
  const sep = path.includes('?') ? '&' : '?'
  while (out.length < max) {
    const r = await get<{ items: T[]; next_cursor?: string | null }>(`${path}${sep}limit=200${after ? `&after=${after}` : ''}`)
    out.push(...(r.items ?? []))
    if (!r.next_cursor) break
    after = r.next_cursor
  }
  return out
}

export async function login(email: string, password: string): Promise<User> {
  const r = await request('POST', '/auth/login', { email, password })
  save({ access: r.access_token, refresh: r.refresh_token })
  return r.user as User
}

export async function logout() {
  const t = tokens
  save(null)
  if (t) {
    try {
      await fetch(`${API_BASE_URL}/auth/logout`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ refresh_token: t.refresh }) })
    } catch {
      /* çıkış en iyi çaba */
    }
  }
}

export const me = () => get<User>('/me')

export const idemKey = (p: string) => `${p}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`

// ---- sağlık (giriş öncesi) ----
export type Health = { status: string; version: string; dependencies: Record<string, { status: string; message?: string }> }
export async function fetchHealth(): Promise<Health> {
  const r = await fetch(`${API_BASE_URL}/health`)
  return (await r.json()) as Health
}

// ---- biçimlendirme: sunucu UTC verir, ekranda UTC+3 (Europe/Istanbul) ----
const TZ = 'Europe/Istanbul'
export const fmtDate = (s: string) => new Intl.DateTimeFormat('tr-TR', { timeZone: TZ, day: '2-digit', month: 'long', weekday: 'long' }).format(new Date(s))
export const fmtShort = (s: string) => new Intl.DateTimeFormat('tr-TR', { timeZone: TZ, day: '2-digit', month: '2-digit', year: 'numeric' }).format(new Date(s))
export const fmtTime = (s: string) => new Intl.DateTimeFormat('tr-TR', { timeZone: TZ, hour: '2-digit', minute: '2-digit' }).format(new Date(s))
export const fmtDateTime = (s: string) => `${fmtShort(s)} ${fmtTime(s)}`

/** Kimlik doğrulamalı dosya indirir (başlık gerektiği için düz <a href> yetmez). */
export async function download(path: string, fallbackName: string) {
  let res = await raw('GET', path)
  if (res.status === 401 && (await refresh())) res = await raw('GET', path)
  if (!res.ok) {
    let msg = `İndirme başarısız (HTTP ${res.status})`
    try {
      msg = (await res.json())?.error?.message ?? msg
    } catch {
      /* gövde JSON değil */
    }
    throw new ApiError(res.status, 'download', msg)
  }
  const cd = res.headers.get('Content-Disposition') ?? ''
  const name = /filename="([^"]+)"/.exec(cd)?.[1] ?? fallbackName
  const url = URL.createObjectURL(await res.blob())
  const a = document.createElement('a')
  a.href = url
  a.download = name
  document.body.appendChild(a)
  a.click()
  a.remove()
  setTimeout(() => URL.revokeObjectURL(url), 10_000)
}
