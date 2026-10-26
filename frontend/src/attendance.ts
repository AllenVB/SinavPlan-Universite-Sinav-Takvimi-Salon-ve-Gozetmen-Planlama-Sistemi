import { post } from './api'

/**
 * Gözetmen/sekreter için elle yoklama işareti (Tablet ve oturma planı krokisi ortak kullanır).
 * Olay istemcide üretilir (UUIDv7 + HLC), önce sunucuya gönderilir; ağ yoksa Tablet'in kuyruğuna (localStorage)
 * yazılır ve çevrimiçi olunca oradan eşitlenir — çevrimdışı yoklama tek bir akıştan geçer (T-5).
 */
export const QKEY = 'sinavplan.queue'
export const DKEY = 'sinavplan.device'

export type AttEvent = { event_id: string; device_id: string; exam_id: number; student_id: number; status: 'present' | 'absent'; source: 'manual'; hlc: { wall: number; counter: number } }

export const readJSON = <T,>(k: string, d: T): T => { try { return JSON.parse(localStorage.getItem(k) ?? '') as T } catch { return d } }
export const writeJSON = (k: string, v: unknown) => { try { localStorage.setItem(k, JSON.stringify(v)) } catch { /* depolama kapalı */ } }

/** UUIDv7: 48 bit zaman + rastgele; olay kimliği istemcide üretilir (idempotent senkron için). */
export function uuidv7() {
  const b = crypto.getRandomValues(new Uint8Array(16))
  const ms = BigInt(Date.now())
  for (let i = 0; i < 6; i++) b[i] = Number((ms >> BigInt(8 * (5 - i))) & 0xffn)
  b[6] = (b[6] & 0x0f) | 0x70
  b[8] = (b[8] & 0x3f) | 0x80
  const h = [...b].map((x) => x.toString(16).padStart(2, '0')).join('')
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`
}

export const deviceId = () => {
  const d = readJSON<string>(DKEY, '') || `tablet-${Math.random().toString(36).slice(2, 7)}`
  writeJSON(DKEY, d)
  return d
}

let counter = 0

/** Tek bir öğrenciyi geldi/gelmedi işaretler. 'synced': sunucu kabul etti; 'queued': ağ yok, kuyruğa yazıldı. */
export async function markAttendance(examId: number, studentId: number, status: 'present' | 'absent'): Promise<'synced' | 'queued'> {
  const ev: AttEvent = { event_id: uuidv7(), device_id: deviceId(), exam_id: examId, student_id: studentId, status, source: 'manual', hlc: { wall: Date.now(), counter: counter++ } }
  if (navigator.onLine) {
    try {
      await post('/attendance/sync', { events: [ev] })
      return 'synced'
    } catch (e) {
      if ((e as { status?: number }).status) throw e // sunucu yanıtladı ve reddetti: kuyruğa atma
    }
  }
  writeJSON(QKEY, [...readJSON<AttEvent[]>(QKEY, []), ev])
  return 'queued'
}
