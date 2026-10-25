package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Period, sınav dönemi.
type Period struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	Kind           string    `json:"kind"`
	StartDate      time.Time `json:"start_date"`
	Days           int16     `json:"days"`
	SessionsPerDay int16     `json:"sessions_per_day"`
	Exams          int       `json:"exams"`
}

// ListPeriods, sınav dönemlerini döner.
func ListPeriods(ctx context.Context, pool *pgxpool.Pool) ([]Period, error) {
	rows, err := pool.Query(ctx, `SELECT p.id, p.name, p.kind, p.start_date, p.days, p.sessions_per_day,
		(SELECT count(*) FROM exams e WHERE e.period_id = p.id)::int FROM exam_periods p ORDER BY p.id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Period, error) {
		var p Period
		err := r.Scan(&p.ID, &p.Name, &p.Kind, &p.StartDate, &p.Days, &p.SessionsPerDay, &p.Exams)
		return p, err
	})
}

// Schedule, çizelge sürümü.
type Schedule struct {
	ID          int64           `json:"id"`
	PeriodID    int64           `json:"period_id"`
	Version     int             `json:"version"`
	Status      string          `json:"status"` // draft | published
	Seed        int64           `json:"seed"`
	Metrics     json.RawMessage `json:"metrics"`
	CreatedAt   time.Time       `json:"created_at"`
	PublishedAt *time.Time      `json:"published_at,omitempty"`
}

const scheduleCols = `id, period_id, version, status, seed, metrics, created_at, published_at`

func scanSchedule(r pgx.Row) (Schedule, error) {
	var s Schedule
	err := r.Scan(&s.ID, &s.PeriodID, &s.Version, &s.Status, &s.Seed, &s.Metrics, &s.CreatedAt, &s.PublishedAt)
	return s, err
}

// GetSchedule, çizelge sürümünü döner.
func GetSchedule(ctx context.Context, pool *pgxpool.Pool, id int64) (Schedule, error) {
	s, err := scanSchedule(pool.QueryRow(ctx, `SELECT `+scheduleCols+` FROM schedules WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Schedule{}, ErrNotFound
	}
	return s, err
}

// ListSchedules, sürümleri yeniden eskiye döner (id azalan keyset: before).
// onlyPublished true ise yalnız yayınlanmış sürümler.
func ListSchedules(ctx context.Context, pool *pgxpool.Pool, periodID int64, onlyPublished bool, before int64, limit int) ([]Schedule, error) {
	rows, err := pool.Query(ctx, `SELECT `+scheduleCols+` FROM schedules
		WHERE ($1 = 0 OR period_id = $1) AND (NOT $2 OR status = 'published') AND ($3 = 0 OR id < $3)
		ORDER BY id DESC LIMIT $4`, periodID, onlyPublished, before, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Schedule, error) { return scanSchedule(r) })
}

// CalendarExam, takvimdeki bir sınav.
type CalendarExam struct {
	ExamID          int64     `json:"exam_id"`
	CourseCode      string    `json:"course_code"`
	CourseName      string    `json:"course_name"`
	SlotID          int64     `json:"slot_id"`
	Day             int16     `json:"day"`
	Session         int16     `json:"session"`
	StartsAt        time.Time `json:"starts_at"`
	EndsAt          time.Time `json:"ends_at"`
	DurationMinutes int16     `json:"duration_minutes"`
	Students        int       `json:"students"`
	Rooms           int       `json:"rooms"`
	RequiresLab     bool      `json:"requires_lab"`
	// Fakülte → bölüm → sınıf: takvimi anlaşılır kılar (süzme/gruplama).
	DepartmentID   int64  `json:"department_id"`
	DepartmentCode string `json:"department_code"`
	DepartmentName string `json:"department_name"`
	FacultyName    string `json:"faculty_name"`
	Year           int16  `json:"year"`
	// Dersi veren öğretim üyesi (şubeler birden çoksa virgülle).
	LecturerName string `json:"lecturer_name"`
}

// ScheduleExams, çizelgedeki sınavları (sınav kimliğiyle keyset) döner.
func ScheduleExams(ctx context.Context, pool *pgxpool.Pool, scheduleID, after int64, limit int) ([]CalendarExam, error) {
	rows, err := pool.Query(ctx, `
		SELECT e.id, c.code, c.name, sl.id, sl.day_index, sl.session_index, sl.starts_at, sl.ends_at, e.duration_minutes,
		       coalesce((SELECT sum(seat_count) FROM room_assignments ra WHERE ra.schedule_id = se.schedule_id AND ra.exam_id = e.id), 0)::int,
		       (SELECT count(*) FROM room_assignments ra WHERE ra.schedule_id = se.schedule_id AND ra.exam_id = e.id)::int,
		       e.requires_lab, d.id, d.code, d.name, coalesce(f.name, ''), c.year,
		       coalesce((SELECT string_agg(DISTINCT u.full_name, ', ') FROM sections sc JOIN lecturers l ON l.id = sc.lecturer_id JOIN users u ON u.id = l.user_id WHERE sc.course_id = c.id), '')
		FROM schedule_exams se JOIN exams e ON e.id = se.exam_id JOIN courses c ON c.id = e.course_id JOIN slots sl ON sl.id = se.slot_id
		JOIN departments d ON d.id = c.department_id LEFT JOIN faculties f ON f.id = d.faculty_id
		WHERE se.schedule_id = $1 AND e.id > $2 ORDER BY e.id LIMIT $3`, scheduleID, after, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (CalendarExam, error) {
		var x CalendarExam
		err := r.Scan(&x.ExamID, &x.CourseCode, &x.CourseName, &x.SlotID, &x.Day, &x.Session, &x.StartsAt, &x.EndsAt, &x.DurationMinutes, &x.Students, &x.Rooms, &x.RequiresLab, &x.DepartmentID, &x.DepartmentCode, &x.DepartmentName, &x.FacultyName, &x.Year, &x.LecturerName)
		return x, err
	})
}

// SlotRoom, bir slottaki salon payı.
type SlotRoom struct {
	RoomID     int64  `json:"room_id"`
	RoomCode   string `json:"room_code"`
	RoomType   string `json:"room_type"`
	Building   string `json:"building"`
	ExamID     int64  `json:"exam_id"`
	CourseCode string `json:"course_code"`
	Students   int    `json:"students"`
}

// SlotRooms, bir slotun salon planını döner (en fazla ~100 satır; sayfalama gerekmez).
func SlotRooms(ctx context.Context, pool *pgxpool.Pool, scheduleID, slotID int64) ([]SlotRoom, error) {
	rows, err := pool.Query(ctx, `SELECT ra.room_id, r.code, r.type, r.building, ra.exam_id, c.code, ra.seat_count
		FROM room_assignments ra JOIN rooms r ON r.id = ra.room_id JOIN exams e ON e.id = ra.exam_id JOIN courses c ON c.id = e.course_id
		WHERE ra.schedule_id = $1 AND ra.slot_id = $2 ORDER BY ra.room_id, ra.exam_id`, scheduleID, slotID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (SlotRoom, error) {
		var x SlotRoom
		err := r.Scan(&x.RoomID, &x.RoomCode, &x.RoomType, &x.Building, &x.ExamID, &x.CourseCode, &x.Students)
		return x, err
	})
}

// SeatRow, oturma planı satırı.
type SeatRow struct {
	ID         int64  `json:"id"`
	StudentID  int64  `json:"student_id"`
	StudentNo  string `json:"student_no"`
	FullName   string `json:"full_name"`
	ExamID     int64  `json:"exam_id"`
	RoomID     int64  `json:"room_id"`
	SeatRow    int16  `json:"seat_row"`
	SeatCol    int16  `json:"seat_col"`
	Accessible bool   `json:"accessible"`       // koltuk erişilebilir mi
	Disabled   bool   `json:"disabled_student"` // öğrenci erişilebilir koltuk gerektiriyor mu
}

// Seating, bir sınavın (ve isteğe bağlı salonun) oturma planını keyset sayfalı döner.
func Seating(ctx context.Context, pool *pgxpool.Pool, scheduleID, examID, roomID, after int64, limit int) ([]SeatRow, error) {
	rows, err := pool.Query(ctx, `SELECT sa.id, sa.student_id, st.student_no, st.full_name, sa.exam_id, sa.room_id, rs.row_no, rs.col_no, rs.is_accessible, st.disability_flag
		FROM seat_assignments sa JOIN students st ON st.id = sa.student_id JOIN room_seats rs ON rs.id = sa.seat_id
		WHERE sa.schedule_id = $1 AND sa.exam_id = $2 AND ($3 = 0 OR sa.room_id = $3) AND sa.id > $4 ORDER BY sa.id LIMIT $5`,
		scheduleID, examID, roomID, after, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (SeatRow, error) {
		var x SeatRow
		err := r.Scan(&x.ID, &x.StudentID, &x.StudentNo, &x.FullName, &x.ExamID, &x.RoomID, &x.SeatRow, &x.SeatCol, &x.Accessible, &x.Disabled)
		return x, err
	})
}

// Duty, gözetmen görevi.
type Duty struct {
	ID         int64     `json:"id"`
	ScheduleID int64     `json:"schedule_id"`
	UserID     int64     `json:"user_id"`
	FullName   string    `json:"full_name"`
	SlotID     int64     `json:"slot_id"`
	Day        int16     `json:"day"`
	Session    int16     `json:"session"`
	StartsAt   time.Time `json:"starts_at"`
	RoomID     int64     `json:"room_id"`
	RoomCode   string    `json:"room_code"`
	Building   string    `json:"building"`
	ExamID     int64     `json:"exam_id"`
	CourseCode string    `json:"course_code"`
}

// Duties, gözetmen görevlerini keyset sayfalı döner; userID/slotID 0 ise süzgeç yok.
func Duties(ctx context.Context, pool *pgxpool.Pool, scheduleID, userID, slotID, after int64, limit int) ([]Duty, error) {
	rows, err := pool.Query(ctx, `SELECT ia.id, ia.schedule_id, ia.user_id, u.full_name, ia.slot_id, sl.day_index, sl.session_index, sl.starts_at,
		ia.room_id, r.code, r.building, ia.exam_id, c.code
		FROM invigilator_assignments ia JOIN users u ON u.id = ia.user_id JOIN slots sl ON sl.id = ia.slot_id
		JOIN rooms r ON r.id = ia.room_id JOIN exams e ON e.id = ia.exam_id JOIN courses c ON c.id = e.course_id
		WHERE ia.schedule_id = $1 AND ($2 = 0 OR ia.user_id = $2) AND ($3 = 0 OR ia.slot_id = $3) AND ia.id > $4
		ORDER BY ia.id LIMIT $5`, scheduleID, userID, slotID, after, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Duty, error) {
		var x Duty
		err := r.Scan(&x.ID, &x.ScheduleID, &x.UserID, &x.FullName, &x.SlotID, &x.Day, &x.Session, &x.StartsAt, &x.RoomID, &x.RoomCode, &x.Building, &x.ExamID, &x.CourseCode)
		return x, err
	})
}

// PublishedDuties, kullanıcının TÜM yayınlanmış çizelgelerdeki (her dönemin yayınlananı) görevleri.
func PublishedDuties(ctx context.Context, pool *pgxpool.Pool, userID, after int64, limit int) ([]Duty, error) {
	rows, err := pool.Query(ctx, `SELECT ia.id, ia.schedule_id, ia.user_id, u.full_name, ia.slot_id, sl.day_index, sl.session_index, sl.starts_at,
		ia.room_id, r.code, r.building, ia.exam_id, c.code
		FROM invigilator_assignments ia JOIN schedules sch ON sch.id = ia.schedule_id AND sch.status = 'published'
		JOIN users u ON u.id = ia.user_id JOIN slots sl ON sl.id = ia.slot_id
		JOIN rooms r ON r.id = ia.room_id JOIN exams e ON e.id = ia.exam_id JOIN courses c ON c.id = e.course_id
		WHERE ia.user_id = $1 AND ia.id > $2 ORDER BY ia.id LIMIT $3`, userID, after, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Duty, error) {
		var x Duty
		err := r.Scan(&x.ID, &x.ScheduleID, &x.UserID, &x.FullName, &x.SlotID, &x.Day, &x.Session, &x.StartsAt, &x.RoomID, &x.RoomCode, &x.Building, &x.ExamID, &x.CourseCode)
		return x, err
	})
}

// PublishedScheduleOfPeriod, belirli dönemin yayınlanmış çizelgesi (yoksa ErrNotFound).
func PublishedScheduleOfPeriod(ctx context.Context, pool *pgxpool.Pool, periodID int64) (int64, error) {
	var id int64
	err := pool.QueryRow(ctx, `SELECT id FROM schedules WHERE status = 'published' AND period_id = $1 ORDER BY id DESC LIMIT 1`, periodID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}

// PublishedScheduleID, dönemin yayınlanmış çizelgesinin kimliği (yoksa ErrNotFound).
func PublishedScheduleID(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var id int64
	err := pool.QueryRow(ctx, `SELECT id FROM schedules WHERE status = 'published' ORDER BY id DESC LIMIT 1`).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}

// StudentConflict, bir öğrencinin aynı gündeki birden çok sınavı.
type StudentConflict struct {
	StudentID int64   `json:"student_id"`
	StudentNo string  `json:"student_no"`
	Day       int16   `json:"day"`
	Exams     []int64 `json:"exam_ids"`
}

// ScheduleConflicts, aynı gün 2 (veya daha çok) sınavı olan öğrencileri keyset sayfalı döner
// (T-1 yumuşak hedefinin ihlalleri; sert kısıt ihlali olmadığı için "uyarı" niteliğinde).
func ScheduleConflicts(ctx context.Context, pool *pgxpool.Pool, scheduleID, after int64, limit int) ([]StudentConflict, error) {
	// İmleç (öğrenci, gün) çiftini tek sayıya kodlar: yalnız öğrenci kimliği imleç olsaydı
	// aynı öğrencinin sayfa sınırına denk gelen ikinci günü atlanırdı.
	rows, err := pool.Query(ctx, `
		SELECT student_id, student_no, day_index, exam_ids FROM (
			SELECT en.student_id, st.student_no, sl.day_index, array_agg(DISTINCT e.id ORDER BY e.id) AS exam_ids
			FROM schedule_exams se JOIN exams e ON e.id = se.exam_id JOIN slots sl ON sl.id = se.slot_id
			JOIN sections s ON s.course_id = e.course_id JOIN enrollments en ON en.section_id = s.id JOIN students st ON st.id = en.student_id
			WHERE se.schedule_id = $1 AND en.student_id >= $2::bigint / $4::bigint
			GROUP BY en.student_id, st.student_no, sl.day_index HAVING count(DISTINCT e.id) >= 2) c
		WHERE student_id * $4::bigint + day_index > $2::bigint
		ORDER BY student_id, day_index LIMIT $3`, scheduleID, after, limit, ConflictCursorBase)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (StudentConflict, error) {
		var x StudentConflict
		err := r.Scan(&x.StudentID, &x.StudentNo, &x.Day, &x.Exams)
		return x, err
	})
}

// ConflictCursorBase: ScheduleConflicts imlecinin gün çarpanı (gün sayısı bunun altındadır).
const ConflictCursorBase int64 = 1000

// ConflictCursor, bir çakışma satırının keyset imleci.
func ConflictCursor(c StudentConflict) int64 { return c.StudentID*ConflictCursorBase + int64(c.Day) }
