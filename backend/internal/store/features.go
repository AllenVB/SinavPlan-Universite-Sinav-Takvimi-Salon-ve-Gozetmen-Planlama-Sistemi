package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ScheduleAssignment, çizelgedeki sınav -> slot kimliği eşlemesi.
func ScheduleAssignment(ctx context.Context, pool *pgxpool.Pool, scheduleID int64) (map[int64]int64, error) {
	rows, err := pool.Query(ctx, `SELECT exam_id, slot_id FROM schedule_exams WHERE schedule_id = $1`, scheduleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var e, s int64
		if err := rows.Scan(&e, &s); err != nil {
			return nil, err
		}
		out[e] = s
	}
	return out, rows.Err()
}

// SlotInfo, dönemin bir sınav oturumu (slot).
type SlotInfo struct {
	ID       int64     `json:"id"`
	Day      int16     `json:"day"`
	Session  int16     `json:"session"`
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
}

// PeriodSlots, dönemin tüm slotlarını (kullanılmayanlar dahil) sırayla döner.
func PeriodSlots(ctx context.Context, pool *pgxpool.Pool, periodID int64) ([]SlotInfo, error) {
	rows, err := pool.Query(ctx, `SELECT id, day_index, session_index, starts_at, ends_at FROM slots WHERE period_id = $1 ORDER BY day_index, session_index`, periodID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (SlotInfo, error) {
		var x SlotInfo
		err := r.Scan(&x.ID, &x.Day, &x.Session, &x.StartsAt, &x.EndsAt)
		return x, err
	})
}

// Activity, denetim kaydından türetilen bir etkinlik satırı.
type Activity struct {
	ID        int64           `json:"id"`
	Action    string          `json:"action"`
	Entity    string          `json:"entity"`
	EntityID  string          `json:"entity_id"`
	Actor     *string         `json:"actor,omitempty"`
	Detail    json.RawMessage `json:"detail,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// Moved, iki çizelge arasında slotu değişen sınav.
type Moved struct {
	ExamID     int64     `json:"exam_id"`
	CourseCode string    `json:"course_code"`
	CourseName string    `json:"course_name"`
	FromSlot   int64     `json:"from_slot_id"`
	ToSlot     int64     `json:"to_slot_id"`
	FromStart  time.Time `json:"from_starts_at"`
	ToStart    time.Time `json:"to_starts_at"`
}

// MovedExams, a'dan b'ye slotu değişen sınavları döner. Yalnız ikisinde de bulunan sınavlar
// karşılaştırılır (aynı dönem varsayılır; çağıran denetler).
func MovedExams(ctx context.Context, pool *pgxpool.Pool, a, b int64) ([]Moved, error) {
	rows, err := pool.Query(ctx, `
		SELECT e.id, c.code, c.name, sa.slot_id, sb.slot_id, la.starts_at, lb.starts_at
		FROM schedule_exams sa JOIN schedule_exams sb ON sb.exam_id = sa.exam_id AND sb.schedule_id = $2
		JOIN exams e ON e.id = sa.exam_id JOIN courses c ON c.id = e.course_id
		JOIN slots la ON la.id = sa.slot_id JOIN slots lb ON lb.id = sb.slot_id
		WHERE sa.schedule_id = $1 AND sa.slot_id <> sb.slot_id ORDER BY e.id`, a, b)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Moved, error) {
		var x Moved
		err := r.Scan(&x.ExamID, &x.CourseCode, &x.CourseName, &x.FromSlot, &x.ToSlot, &x.FromStart, &x.ToStart)
		return x, err
	})
}
