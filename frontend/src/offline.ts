import { ApiError } from './api'

/**
 * Faz 22 C3 — çevrimdışı açılış: son BAŞARILI yanıtı cihazda saklar; yalnız ağ hatasında (status 0) onu döner.
 * Sunucu hatası (4xx/5xx) her zaman iletilir — eski veri hatayı gizlemez. Anahtarlar oturumdaki kullanıcıya
 * bağlıdır ve çıkışta silinir (paylaşılan tablette bir sonraki kullanıcı önceki listeyi görmez).
 */
const PREFIX = 'sinavplan-cache:'
let owner = 'anon'
let epoch = 0 // her temizlikte artar: temizlikten önce başlamış bir isteğin yanıtı yazılmaz
export const setCacheOwner = (id: number | string | null) => { owner = id === null ? 'anon' : String(id) }

export async function cached<T>(key: string, fn: () => Promise<T>): Promise<T> {
  const who = owner
  const ep = epoch
  const k = `${PREFIX}${who}:${key}`
  try {
    const v = await fn()
    // İstek sürerken kullanıcı değiştiyse (ör. çıkış) yazma: aksi halde uçuştaki bir yanıt, çıkışta silinen
    // listeyi geri yazıyordu (pwa.cjs bu yarışı yakaladı).
    if (owner === who && epoch === ep) {
      try { localStorage.setItem(k, JSON.stringify({ at: Date.now(), v })) } catch { /* depolama dolu/kapalı: önbelleksiz devam */ }
    }
    return v
  } catch (e) {
    if (e instanceof ApiError && e.status === 0) {
      try {
        const hit = localStorage.getItem(k)
        if (hit) return (JSON.parse(hit) as { v: T }).v
      } catch { /* bozuk kayıt: asıl hatayı ilet */ }
    }
    throw e
  }
}

export function clearCache() {
  epoch++
  try {
    // önce anahtarları topla, sonra sil: silme sırasında tarayıcı anahtar sırasını değiştirebiliyor ve indeksle
    // gezince bir kayıt atlanıyordu (pwa.cjs'te çıkıştan sonra 'cal' listesi kalıyordu)
    const keys = Object.keys(localStorage).filter((k) => k.startsWith(PREFIX))
    keys.forEach((k) => localStorage.removeItem(k))
  } catch { /* depolama kapalı */ }
}
