import { useEffect, useMemo, useState } from 'react'
import { all, get, type Role } from '../api'
import { useAuth } from '../auth'
import { useAsync } from '../ui'

export type Schedule = { id: number; period_id: number; version: number; status: 'draft' | 'published'; seed: number; metrics: any; created_at: string; published_at?: string }
export type Exam = { exam_id: number; course_code: string; course_name: string; slot_id: number; day: number; session: number; starts_at: string; ends_at: string; duration_minutes: number; students: number; rooms: number; requires_lab: boolean; department_id: number; department_code: string; department_name: string; faculty_name: string; year: number }
export type Faculty = { id: number; code: string; name: string; departments: { id: number; code: string; name: string; students: number; courses: number; years: number[] }[]; lecturers: { id: number; full_name: string }[] }
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

export const useFaculties = () => useAsync(async () => (await get<{ items: Faculty[] }>('/faculties')).items)

/**
 * Fakülte → bölüm → sınıf süzgeci (kademeli seçim). `element` üç açılır listeyi çizer; `match` bir sınavın
 * seçime uyup uymadığını söyler. Ders listelerini (yüzlerce sınav) anlaşılır kılmak için ortak kullanılır.
 */
export function useExamFilter() {
  const fac = useFaculties()
  const [facId, setFacId] = useState(0)
  const [deptId, setDeptId] = useState(0)
  const [year, setYear] = useState(0)
  const faculty = fac.data?.find((f) => f.id === facId)
  const match = useMemo(() => (e: Exam) => (!faculty || e.faculty_name === faculty.name) && (!deptId || e.department_id === deptId) && (!year || e.year === year), [faculty, deptId, year])
  const active = !!(facId || deptId || year)
  const element = (
    <>
      <select aria-label="Fakülte" value={facId} onChange={(e) => { setFacId(Number(e.target.value)); setDeptId(0) }} style={{ width: 'auto' }}>
        <option value={0}>Tüm fakülteler</option>
        {fac.data?.map((f) => <option key={f.id} value={f.id}>{f.name}</option>)}
      </select>
      <select aria-label="Bölüm" value={deptId} onChange={(e) => setDeptId(Number(e.target.value))} disabled={!faculty} style={{ width: 'auto' }}>
        <option value={0}>{faculty ? 'Tüm bölümler' : 'Önce fakülte seçin'}</option>
        {faculty?.departments.map((d) => <option key={d.id} value={d.id}>{d.name}</option>)}
      </select>
      <select aria-label="Sınıf" value={year} onChange={(e) => setYear(Number(e.target.value))} style={{ width: 'auto' }}>
        <option value={0}>Tüm sınıflar</option>
        {[1, 2, 3, 4].map((y) => <option key={y} value={y}>{y}. sınıf</option>)}
      </select>
      {active && <button className="btn sm" onClick={() => { setFacId(0); setDeptId(0); setYear(0) }}>Süzgeci temizle</button>}
    </>
  )
  return { element, match, active }
}

/** "BLM101 — Veri Yapıları (Bilgisayar Mühendisliği · 1. sınıf)" */
export const examLabel = (e: Exam) => `${e.course_code} — ${e.course_name} (${e.department_name} · ${e.year}. sınıf)`
