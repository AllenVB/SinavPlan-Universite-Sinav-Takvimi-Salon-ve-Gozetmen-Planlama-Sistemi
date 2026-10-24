package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/AllenVB/sinavplan/backend/internal/simdata"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// LoadDataset, bir sınav dönemi için planlama girdisini veritabanından yükler ve
// simülatör verisiyle AYNI yapıda (simdata.Dataset) döner; böylece planlama zinciri
// (planning) hem seed verisinde hem gerçek veritabanında aynı kodla çalışır.
func LoadDataset(ctx context.Context, pool *pgxpool.Pool, periodID, seed int64) (*simdata.Dataset, error) {
	ds := &simdata.Dataset{}
	err := pool.QueryRow(ctx, `SELECT id, name, kind, start_date, days, sessions_per_day FROM exam_periods WHERE id = $1`, periodID).
		Scan(&ds.Period.ID, &ds.Period.Name, &ds.Period.Kind, &ds.Period.StartDate, &ds.Period.Days, &ds.Period.SessionsPerDay)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	ds.Config = simdata.Config{Seed: seed, Days: ds.Period.Days, SessionsPerDay: ds.Period.SessionsPerDay}

	each := func(q string, args []any, scan func(pgx.Rows) error) error {
		rows, err := pool.Query(ctx, q, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if err := scan(rows); err != nil {
				return err
			}
		}
		return rows.Err()
	}

	if err := each(`SELECT id, code, name FROM departments ORDER BY id`, nil, func(r pgx.Rows) error {
		var d simdata.Department
		err := r.Scan(&d.ID, &d.Code, &d.Name)
		ds.Departments = append(ds.Departments, d)
		return err
	}); err != nil {
		return nil, err
	}
	if err := each(`SELECT u.id, u.email, u.full_name, coalesce(array_agg(r.role ORDER BY r.role) FILTER (WHERE r.role IS NOT NULL), '{}')
		FROM users u LEFT JOIN user_roles r ON r.user_id = u.id GROUP BY u.id ORDER BY u.id`, nil, func(r pgx.Rows) error {
		var u simdata.User
		err := r.Scan(&u.ID, &u.Email, &u.FullName, &u.Roles)
		ds.Users = append(ds.Users, u)
		return err
	}); err != nil {
		return nil, err
	}
	if err := each(`SELECT id, user_id, department_id FROM lecturers ORDER BY id`, nil, func(r pgx.Rows) error {
		var l simdata.Lecturer
		err := r.Scan(&l.ID, &l.UserID, &l.DepartmentID)
		ds.Lecturers = append(ds.Lecturers, l)
		return err
	}); err != nil {
		return nil, err
	}
	// Yalnızca bu dönemin sınavlarının dersleri.
	if err := each(`SELECT c.id, c.department_id, c.code, c.name, c.year, c.requires_lab FROM courses c
		WHERE c.id IN (SELECT course_id FROM exams WHERE period_id = $1) ORDER BY c.id`, []any{periodID}, func(r pgx.Rows) error {
		var c simdata.Course
		var y int16
		err := r.Scan(&c.ID, &c.DepartmentID, &c.Code, &c.Name, &y, &c.RequiresLab)
		c.Year = int(y)
		ds.Courses = append(ds.Courses, c)
		return err
	}); err != nil {
		return nil, err
	}
	if err := each(`SELECT s.id, s.course_id, s.code, s.lecturer_id FROM sections s
		WHERE s.course_id IN (SELECT course_id FROM exams WHERE period_id = $1) ORDER BY s.id`, []any{periodID}, func(r pgx.Rows) error {
		var s simdata.Section
		err := r.Scan(&s.ID, &s.CourseID, &s.Code, &s.LecturerID)
		ds.Sections = append(ds.Sections, s)
		return err
	}); err != nil {
		return nil, err
	}
	if err := each(`SELECT id, coalesce(user_id, 0), student_no, full_name, department_id, disability_flag FROM students ORDER BY id`, nil, func(r pgx.Rows) error {
		var s simdata.Student
		err := r.Scan(&s.ID, &s.UserID, &s.No, &s.FullName, &s.DepartmentID, &s.Disabled)
		ds.Students = append(ds.Students, s)
		return err
	}); err != nil {
		return nil, err
	}
	if err := each(`SELECT en.student_id, en.section_id FROM enrollments en JOIN sections s ON s.id = en.section_id
		WHERE s.course_id IN (SELECT course_id FROM exams WHERE period_id = $1) ORDER BY 1, 2`, []any{periodID}, func(r pgx.Rows) error {
		var e simdata.Enrollment
		err := r.Scan(&e.StudentID, &e.SectionID)
		ds.Enrollments = append(ds.Enrollments, e)
		return err
	}); err != nil {
		return nil, err
	}
	if err := each(`SELECT id, code, building, floor, type, seat_rows, seat_cols, capacity FROM rooms ORDER BY id`, nil, func(r pgx.Rows) error {
		var x simdata.Room
		var f, rr, cc int16
		err := r.Scan(&x.ID, &x.Code, &x.Building, &f, &x.Type, &rr, &cc, &x.Capacity)
		x.Floor, x.Rows, x.Cols = int(f), int(rr), int(cc)
		ds.Rooms = append(ds.Rooms, x)
		return err
	}); err != nil {
		return nil, err
	}
	if err := each(`SELECT id, room_id, row_no, col_no, is_active, is_accessible FROM room_seats ORDER BY id`, nil, func(r pgx.Rows) error {
		var s simdata.Seat
		var rr, cc int16
		err := r.Scan(&s.ID, &s.RoomID, &rr, &cc, &s.Active, &s.Accessible)
		s.Row, s.Col = int(rr), int(cc)
		ds.Seats = append(ds.Seats, s)
		return err
	}); err != nil {
		return nil, err
	}
	// Slot sırası: gün, oturum -> indeks = gün*oturum + oturum (planning bu sıraya güvenir).
	if err := each(`SELECT id, period_id, day_index, session_index, starts_at, ends_at FROM slots WHERE period_id = $1
		ORDER BY day_index, session_index`, []any{periodID}, func(r pgx.Rows) error {
		var s simdata.Slot
		var d, se int16
		err := r.Scan(&s.ID, &s.PeriodID, &d, &se, &s.StartsAt, &s.EndsAt)
		s.Day, s.Session = int(d), int(se)
		ds.Slots = append(ds.Slots, s)
		return err
	}); err != nil {
		return nil, err
	}
	if err := each(`SELECT id, period_id, course_id, duration_minutes, requires_lab FROM exams WHERE period_id = $1 ORDER BY id`,
		[]any{periodID}, func(r pgx.Rows) error {
			var e simdata.Exam
			var d int16
			err := r.Scan(&e.ID, &e.PeriodID, &e.CourseID, &d, &e.RequiresLab)
			e.DurationMinutes = int(d)
			ds.Exams = append(ds.Exams, e)
			return err
		}); err != nil {
		return nil, err
	}
	if len(ds.Exams) == 0 || len(ds.Rooms) == 0 || len(ds.Slots) != ds.Period.Days*ds.Period.SessionsPerDay {
		return nil, fmt.Errorf("%w: dönemde sınav yok, salon yok veya slotlar eksik (%d sınav, %d salon, %d slot)",
			ErrConflict, len(ds.Exams), len(ds.Rooms), len(ds.Slots))
	}
	ds.Config.Exams, ds.Config.Students, ds.Config.Rooms = len(ds.Exams), len(ds.Students), len(ds.Rooms)
	return ds, nil
}

// ExcusedSlots, ONAYLANMIŞ gözetmen mazeretlerini slot indeksine çevirir
// (kullanıcı kimliği -> mazeretli slot indeksleri; indeks = ds.Slots konumu).
func ExcusedSlots(ctx context.Context, pool *pgxpool.Pool, ds *simdata.Dataset) (map[int64][]int, error) {
	idx := map[int64]int{}
	for i, s := range ds.Slots {
		idx[s.ID] = i
	}
	rows, err := pool.Query(ctx, `SELECT user_id, slot_id FROM excuses WHERE kind = 'invigilator' AND status = 'approved' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]int{}
	for rows.Next() {
		var u, s int64
		if err := rows.Scan(&u, &s); err != nil {
			return nil, err
		}
		if i, ok := idx[s]; ok {
			out[u] = append(out[u], i)
		}
	}
	return out, rows.Err()
}
