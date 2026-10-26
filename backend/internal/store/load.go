package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// LoadCell, ısı haritasının bir hücresi: gün × oturum.
type LoadCell struct {
	SlotID   int64 `json:"slot_id"`
	Day      int   `json:"day"`
	Session  int   `json:"session"`
	Exams    int   `json:"exams"`
	Students int   `json:"students"` // o oturumda sınava giren öğrenci (öğrenci-sınav çifti)
}

// LoadDay, günlük yoğunluk: aynı gün 2 sınavı olan öğrenci ve önceki günle ardışık sınavı olan öğrenci.
type LoadDay struct {
	Day         int `json:"day"`
	TwoExams    int `json:"two_exams"`   // o gün 2 sınava giren öğrenci
	Consecutive int `json:"consecutive"` // o gün ve ertesi gün sınavı olan öğrenci (gün çifti)
}

// ScheduleLoad, çizelgenin yoğunluk haritasını SQL ile (çözücüden bağımsız) çıkarır (Faz 22 B3).
// Toplamlar, çözücünün metrikleriyle (student_days_with_two_exams, consecutive_day_pairs) aynı olmalıdır.
func ScheduleLoad(ctx context.Context, pool *pgxpool.Pool, sid int64) ([]LoadCell, []LoadDay, error) {
	cells := []LoadCell{}
	rows, err := pool.Query(ctx, `
		WITH es AS (SELECT DISTINCT e.id AS exam_id, en.student_id FROM "wm-exams" e JOIN "wm-sections" s ON s.course_id = e.course_id
		            JOIN "wm-enrollments" en ON en.section_id = s.id)
		SELECT sl.id, sl.day_index, sl.session_index, count(DISTINCT se.exam_id), count(es.student_id)
		FROM "wm-slots" sl JOIN "wm-schedules" sc ON sc.period_id = sl.period_id AND sc.id = $1
		LEFT JOIN "wm-schedule_exams" se ON se.schedule_id = $1 AND se.slot_id = sl.id
		LEFT JOIN es ON es.exam_id = se.exam_id
		GROUP BY sl.id, sl.day_index, sl.session_index ORDER BY sl.day_index, sl.session_index`, sid)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var c LoadCell
		if err := rows.Scan(&c.SlotID, &c.Day, &c.Session, &c.Exams, &c.Students); err != nil {
			rows.Close()
			return nil, nil, err
		}
		cells = append(cells, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	days := []LoadDay{}
	rows, err = pool.Query(ctx, `
		WITH es AS (SELECT DISTINCT e.id AS exam_id, en.student_id FROM "wm-exams" e JOIN "wm-sections" s ON s.course_id = e.course_id
		            JOIN "wm-enrollments" en ON en.section_id = s.id),
		     sd AS (SELECT es.student_id, sl.day_index AS d, count(*) AS n FROM es JOIN "wm-schedule_exams" se ON se.schedule_id = $1 AND se.exam_id = es.exam_id
		            JOIN "wm-slots" sl ON sl.id = se.slot_id GROUP BY 1, 2),
		     dd AS (SELECT DISTINCT day_index AS d FROM "wm-slots" WHERE period_id = (SELECT period_id FROM "wm-schedules" WHERE id = $1))
		SELECT dd.d, (SELECT count(*) FROM sd WHERE sd.d = dd.d AND sd.n >= 2),
		       (SELECT count(*) FROM sd a JOIN sd b ON b.student_id = a.student_id AND b.d = a.d + 1 WHERE a.d = dd.d)
		FROM dd ORDER BY dd.d`, sid)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var d LoadDay
		if err := rows.Scan(&d.Day, &d.TwoExams, &d.Consecutive); err != nil {
			return nil, nil, err
		}
		days = append(days, d)
	}
	return cells, days, rows.Err()
}
