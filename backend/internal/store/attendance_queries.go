package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ExamCard, öğrencinin sınav kartı bilgileri (yayınlanmış çizelgeden).
type ExamCard struct {
	ExamID     int64     `json:"exam_id"`
	CourseCode string    `json:"course_code"`
	CourseName string    `json:"course_name"`
	StartsAt   time.Time `json:"starts_at"`
	EndsAt     time.Time `json:"ends_at"`
	RoomCode   *string   `json:"room_code"`
	Building   *string   `json:"building"`
	SeatRow    *int16    `json:"seat_row"`
	SeatCol    *int16    `json:"seat_col"`
}

// StudentExamCard, öğrenci gerçekten o sınava kayıtlıysa ve sınav yayınlanmış
// çizelgedeyse kart bilgisini döner; aksi halde ErrNotFound (başkasının sınavını
// sorgulamak "yok" görünür).
func StudentExamCard(ctx context.Context, pool *pgxpool.Pool, studentID, examID int64) (ExamCard, error) {
	var c ExamCard
	err := pool.QueryRow(ctx, `
		SELECT e.id, co.code, co.name, sl.starts_at, sl.ends_at, r.code, r.building, rs.row_no, rs.col_no
		FROM exams e
		JOIN courses co        ON co.id = e.course_id
		JOIN schedules sch     ON sch.period_id = e.period_id AND sch.status = 'published'
		JOIN schedule_exams se ON se.schedule_id = sch.id AND se.exam_id = e.id
		JOIN slots sl          ON sl.id = se.slot_id
		LEFT JOIN seat_assignments sa ON sa.schedule_id = sch.id AND sa.exam_id = e.id AND sa.student_id = $1
		LEFT JOIN rooms r      ON r.id = sa.room_id
		LEFT JOIN room_seats rs ON rs.id = sa.seat_id
		WHERE e.id = $2 AND EXISTS (
			SELECT 1 FROM sections s JOIN enrollments en ON en.section_id = s.id
			WHERE s.course_id = e.course_id AND en.student_id = $1)`, studentID, examID).
		Scan(&c.ExamID, &c.CourseCode, &c.CourseName, &c.StartsAt, &c.EndsAt, &c.RoomCode, &c.Building, &c.SeatRow, &c.SeatCol)
	if errors.Is(err, pgx.ErrNoRows) {
		return ExamCard{}, ErrNotFound
	}
	return c, err
}

// AttendanceRow, bir öğrencinin yoklama durumu.
type AttendanceRow struct {
	StudentID int64     `json:"student_id"`
	Status    string    `json:"status"`
	Source    string    `json:"source"`
	DeviceID  string    `json:"device_id"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ExamAttendance, sınavın yoklama durumunu keyset sayfalı döner (student_id > after).
func ExamAttendance(ctx context.Context, pool *pgxpool.Pool, examID, after int64, limit int) ([]AttendanceRow, error) {
	rows, err := pool.Query(ctx, `SELECT student_id, status, source, device_id, updated_at FROM attendance_state
		WHERE exam_id = $1 AND student_id > $2 ORDER BY student_id LIMIT $3`, examID, after, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (AttendanceRow, error) {
		var a AttendanceRow
		err := r.Scan(&a.StudentID, &a.Status, &a.Source, &a.DeviceID, &a.UpdatedAt)
		return a, err
	})
}

// AttendanceSummary, sınavın yoklama özeti.
type AttendanceSummary struct {
	Enrolled  int `json:"enrolled"`
	Present   int `json:"present"`
	Absent    int `json:"absent"`
	Conflicts int `json:"conflicts"`
}

// ExamAttendanceSummary, sınav için özet sayılar.
func ExamAttendanceSummary(ctx context.Context, pool *pgxpool.Pool, examID int64) (AttendanceSummary, error) {
	var s AttendanceSummary
	err := pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(DISTINCT en.student_id) FROM exams e JOIN sections sc ON sc.course_id = e.course_id
		     JOIN enrollments en ON en.section_id = sc.id WHERE e.id = $1),
		  (SELECT count(*) FROM attendance_state WHERE exam_id = $1 AND status = 'present'),
		  (SELECT count(*) FROM attendance_state WHERE exam_id = $1 AND status = 'absent'),
		  (SELECT count(*) FROM attendance_conflicts WHERE exam_id = $1)`, examID).
		Scan(&s.Enrolled, &s.Present, &s.Absent, &s.Conflicts)
	return s, err
}

// ConflictRow, yoklama çakışması.
type ConflictRow struct {
	ID          int64     `json:"id"`
	ExamID      int64     `json:"exam_id"`
	StudentID   int64     `json:"student_id"`
	WinnerEvent string    `json:"winner_event_id"`
	LoserEvent  string    `json:"loser_event_id"`
	Rule        string    `json:"rule"`
	DetectedAt  time.Time `json:"detected_at"`
}

// AttendanceConflicts, çakışmaları keyset sayfalı döner; examID 0 ise tümü.
func AttendanceConflicts(ctx context.Context, pool *pgxpool.Pool, examID, after int64, limit int) ([]ConflictRow, error) {
	rows, err := pool.Query(ctx, `SELECT id, exam_id, student_id, winner_event_id::text, loser_event_id::text, rule, detected_at
		FROM attendance_conflicts WHERE ($1 = 0 OR exam_id = $1) AND id > $2 ORDER BY id LIMIT $3`, examID, after, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ConflictRow, error) {
		var c ConflictRow
		err := r.Scan(&c.ID, &c.ExamID, &c.StudentID, &c.WinnerEvent, &c.LoserEvent, &c.Rule, &c.DetectedAt)
		return c, err
	})
}
