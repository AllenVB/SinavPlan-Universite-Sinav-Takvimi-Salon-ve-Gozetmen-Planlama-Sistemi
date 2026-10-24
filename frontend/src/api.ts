// API taban adresi derleme sırasında enjekte edilir (compose: VITE_API_BASE_URL).
export const API_BASE_URL: string =
  import.meta.env.VITE_API_BASE_URL ?? 'http://localhost:8080'

export type DependencyState = { status: string; message?: string }
export type Health = {
  status: string
  version: string
  dependencies: Record<string, DependencyState>
}

/** Backend sağlık durumunu okur. Hata durumunu çağırana bırakır. */
export async function fetchHealth(signal?: AbortSignal): Promise<Health> {
  const res = await fetch(`${API_BASE_URL}/health`, { signal })
  // /health sağlıksızken 503 döner ve gövdesi hâlâ anlamlıdır; bu yüzden
  // durum kodunu hata saymıyoruz, gövdeyi okuyup gösteriyoruz.
  if (res.status !== 200 && res.status !== 503) {
    throw new Error(`beklenmeyen durum kodu: ${res.status}`)
  }
  return (await res.json()) as Health
}
