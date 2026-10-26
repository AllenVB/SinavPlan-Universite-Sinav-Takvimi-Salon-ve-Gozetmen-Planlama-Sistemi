package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SearchHit, komut paletinin (Ctrl+K) bir sonucudur.
type SearchHit struct {
	Kind  string `json:"kind"` // student | course | room
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Sub   string `json:"sub"`
}

// likeEscape, kullanıcı girdisindeki LIKE joker karakterlerini (% _ \) düz metne çevirir.
func likeEscape(q string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q)
}

// Search, öğrenci (numara öneki ya da ad), ders (kod/ad) ve salon (kod) arar; tür başına en çok perKind sonuç.
// Sorgular parametrelidir; girdi LIKE jokerlerinden arındırılır.
func Search(ctx context.Context, pool *pgxpool.Pool, q string, perKind int) ([]SearchHit, error) {
	q = strings.TrimSpace(q)
	out := []SearchHit{}
	if len([]rune(q)) < 2 {
		return out, nil
	}
	pre, sub := likeEscape(q)+"%", "%"+likeEscape(q)+"%"
	queries := []struct{ kind, sql string }{
		{"student", `SELECT s.id, s.student_no || ' · ' || s.full_name, d.name FROM "wm-students" s JOIN "wm-departments" d ON d.id = s.department_id
			WHERE s.student_no LIKE $1 ESCAPE '\' OR s.full_name ILIKE $2 ESCAPE '\' ORDER BY s.student_no LIMIT $3`},
		{"course", `SELECT c.id, c.code || ' · ' || c.name, d.name || ' · ' || c.year || '. sınıf' FROM "wm-courses" c JOIN "wm-departments" d ON d.id = c.department_id
			WHERE c.code ILIKE $1 ESCAPE '\' OR c.name ILIKE $2 ESCAPE '\' ORDER BY c.code LIMIT $3`},
		{"room", `SELECT r.id, r.code, 'Bina ' || r.building || ' · ' || CASE r.type WHEN 'lab' THEN 'Laboratuvar' ELSE 'Derslik' END || ' · ' || r.capacity || ' kişi'
			FROM "wm-rooms" r WHERE r.code ILIKE $1 ESCAPE '\' ORDER BY r.code LIMIT $2`},
	}
	for _, x := range queries {
		args := []any{pre, sub, perKind}
		if x.kind == "room" {
			args = []any{sub, perKind}
		}
		rows, err := pool.Query(ctx, x.sql, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			h := SearchHit{Kind: x.kind}
			if err := rows.Scan(&h.ID, &h.Title, &h.Sub); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, h)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// StudentOverview, "Öğrenci 360" kartının sınav dışı kısmıdır: kimlik, bölüm, yoklama durumu, mazeretler.
type StudentOverview struct {
	ID         int64                       `json:"id"`
	StudentNo  string                      `json:"student_no"`
	FullName   string                      `json:"full_name"`
	Department string                      `json:"department"`
	Faculty    string                      `json:"faculty"`
	Disability bool                        `json:"disability_flag"`
	Attendance map[int64]StudentAttendance `json:"attendance"` // exam_id -> durum
	Excuses    []StudentExcuse             `json:"excuses"`
}

type StudentAttendance struct {
	Status    string    `json:"status"`
	Source    string    `json:"source"`
	UpdatedAt time.Time `json:"updated_at"`
}

type StudentExcuse struct {
	ID         int64     `json:"id"`
	ExamID     *int64    `json:"exam_id"`
	CourseCode *string   `json:"course_code"`
	Reason     string    `json:"reason"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

func GetStudentOverview(ctx context.Context, pool *pgxpool.Pool, id int64) (StudentOverview, error) {
	o := StudentOverview{Attendance: map[int64]StudentAttendance{}, Excuses: []StudentExcuse{}}
	err := pool.QueryRow(ctx, `SELECT s.id, s.student_no, s.full_name, d.name, coalesce(f.name, ''), s.disability_flag
		FROM "wm-students" s JOIN "wm-departments" d ON d.id = s.department_id LEFT JOIN "wm-faculties" f ON f.id = d.faculty_id WHERE s.id = $1`, id).
		Scan(&o.ID, &o.StudentNo, &o.FullName, &o.Department, &o.Faculty, &o.Disability)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, ErrNotFound
	}
	if err != nil {
		return o, err
	}
	rows, err := pool.Query(ctx, `SELECT exam_id, status, source, updated_at FROM "wm-attendance_state" WHERE student_id = $1`, id)
	if err != nil {
		return o, err
	}
	for rows.Next() {
		var e int64
		var a StudentAttendance
		if err := rows.Scan(&e, &a.Status, &a.Source, &a.UpdatedAt); err != nil {
			rows.Close()
			return o, err
		}
		o.Attendance[e] = a
	}
	rows.Close()
	rows, err = pool.Query(ctx, `SELECT e.id, e.exam_id, c.code, e.reason, e.status, e.created_at FROM "wm-excuses" e
		LEFT JOIN "wm-exams" x ON x.id = e.exam_id LEFT JOIN "wm-courses" c ON c.id = x.course_id
		WHERE e.student_id = $1 ORDER BY e.id DESC LIMIT 50`, id)
	if err != nil {
		return o, err
	}
	defer rows.Close()
	for rows.Next() {
		var x StudentExcuse
		if err := rows.Scan(&x.ID, &x.ExamID, &x.CourseCode, &x.Reason, &x.Status, &x.CreatedAt); err != nil {
			return o, err
		}
		o.Excuses = append(o.Excuses, x)
	}
	return o, rows.Err()
}
