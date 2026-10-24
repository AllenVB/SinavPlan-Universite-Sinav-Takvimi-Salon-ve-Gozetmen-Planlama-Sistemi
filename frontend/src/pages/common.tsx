import { useEffect, useState } from 'react'
import { all, get, type Role } from '../api'
import { useAuth } from '../auth'
import { useAsync } from '../ui'

export type Schedule = { id: number; period_id: number; version: number; status: 'draft' | 'published'; seed: number; metrics: any; created_at: string; published_at?: string }
export type Exam = { exam_id: number; course_code: string; course_name: string; slot_id: number; day: number; session: number; starts_at: string; ends_at: string; duration_minutes: number; students: number; rooms: number; requires_lab: boolean }
export type Room = { id: number; code: string; building: string; floor: number; type: string; rows: number; cols: number; capacity: number; accessible_seats: number }

export const hasRole = (roles: Role[], ...r: Role[]) => r.some((x) => roles.includes(x))

/** Sürüm listesi + seçili sürüm. Sekreter taslakları da görür; diğer roller yalnız yayınlananı. */
export function useSchedules() {
  const { user } = useAuth()
  const list = useAsync(async () => (await get<{ items: Schedule[] }>('/schedules?limit=100')).items)
  const [sel, setSel] = useState<number | null>(null)
  useEffect(() => {
    if (!list.data || sel !== null) return
    const pub = list.data.find((s) => s.status === 'published')
    setSel((pub ?? list.data[0])?.id ?? null)
  }, [list.data, sel])
  return { list, sel, setSel, current: list.data?.find((s) => s.id === sel) ?? null, isSecretary: hasRole(user!.roles, 'secretary') }
}

export const useExams = (schedule: number | null) =>
  useAsync(() => (schedule ? all<Exam>(`/schedules/${schedule}/exams`) : Promise.resolve([] as Exam[])), [schedule])

export const useRooms = () => useAsync(() => all<Room>('/rooms'))

export function ScheduleSelect({ s }: { s: ReturnType<typeof useSchedules> }) {
  if (!s.list.data?.length) return null
  return (
    <select aria-label="Çizelge sürümü" value={s.sel ?? ''} onChange={(e) => s.setSel(Number(e.target.value))} style={{ width: 'auto' }}>
      {s.list.data.map((x) => (
        <option key={x.id} value={x.id}>
          Sürüm {x.version} — {x.status === 'published' ? 'yayında' : 'taslak'}
        </option>
      ))}
    </select>
  )
}
