package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/AllenVB/sinavplan/backend/internal/audit"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SchedulePlan, kalıcılaştırılacak tam bir çizelge (T-1..T-4 çıktısı).
type SchedulePlan struct {
	PeriodID  int64
	Seed      int64
	Metrics   any
	CreatedBy int64

	Exams        []PlanExam
	Rooms        []PlanRoom
	Seats        []PlanSeat
	Invigilators []PlanInvigilator
}

type (
	// PlanExam: sınav -> slot.
	PlanExam struct{ ExamID, SlotID int64 }
	// PlanRoom: sınavın bir salondaki payı.
	PlanRoom struct {
		ExamID, SlotID, RoomID int64
		SeatCount              int
	}
	// PlanSeat: öğrencinin koltuğu.
	PlanSeat struct{ ExamID, SlotID, StudentID, RoomID, SeatID int64 }
	// PlanInvigilator: salonun gözetmeni. ExamID, salondaki sınavlardan biridir
	// (bileşik FK için; görev salon düzeyindedir).
	PlanInvigilator struct{ ExamID, SlotID, RoomID, UserID int64 }
)

// ErrNotFound, kayıt bulunamadığında döner.
var ErrNotFound = errors.New("kayıt bulunamadı")

// ErrConflict, durum çakışmasında döner (ör. zaten yayınlanmış).
var ErrConflict = errors.New("durum çakışması")

// CreateSchedule, planı TASLAK olarak yeni bir sürümle yazar ve (id, sürüm) döner.
// Dönem satırı FOR UPDATE ile kilitlenir: eşzamanlı iki oluşturma aynı sürüm
// numarasını alamaz. Toplu satırlar COPY ile yazılır (52 bin koltuk ~ saniyeler).
func CreateSchedule(ctx context.Context, pool *pgxpool.Pool, p SchedulePlan) (int64, int, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var one int
	if err := tx.QueryRow(ctx, `SELECT 1 FROM "wm-exam_periods" WHERE id = $1 FOR UPDATE`, p.PeriodID).Scan(&one); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, 0, ErrNotFound
		}
		return 0, 0, err
	}
	var version int
	if err := tx.QueryRow(ctx, `SELECT coalesce(max(version), 0) + 1 FROM "wm-schedules" WHERE period_id = $1`, p.PeriodID).Scan(&version); err != nil {
		return 0, 0, err
	}
	metrics, err := json.Marshal(p.Metrics)
	if err != nil {
		return 0, 0, err
	}
	var createdBy any
	if p.CreatedBy != 0 {
		createdBy = p.CreatedBy
	}
	var id int64
	if err := tx.QueryRow(ctx,
		`INSERT INTO "wm-schedules" (period_id, version, seed, metrics, created_by) VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		p.PeriodID, version, p.Seed, metrics, createdBy).Scan(&id); err != nil {
		return 0, 0, err
	}

	copyN := func(table string, cols []string, n int, row func(int) []any) error {
		_, err := tx.CopyFrom(ctx, pgx.Identifier{TableName(table)}, cols, pgx.CopyFromSlice(n, func(i int) ([]any, error) { return row(i), nil }))
		if err != nil {
			return fmt.Errorf("%s yazılamadı: %w", table, err)
		}
		return nil
	}
	if err := copyN("schedule_exams", []string{"schedule_id", "exam_id", "slot_id"}, len(p.Exams),
		func(i int) []any { x := p.Exams[i]; return []any{id, x.ExamID, x.SlotID} }); err != nil {
		return 0, 0, err
	}
	if err := copyN("room_assignments", []string{"schedule_id", "exam_id", "slot_id", "room_id", "seat_count"}, len(p.Rooms),
		func(i int) []any { x := p.Rooms[i]; return []any{id, x.ExamID, x.SlotID, x.RoomID, x.SeatCount} }); err != nil {
		return 0, 0, err
	}
	if err := copyN("seat_assignments", []string{"schedule_id", "exam_id", "slot_id", "student_id", "room_id", "seat_id"}, len(p.Seats),
		func(i int) []any {
			x := p.Seats[i]
			return []any{id, x.ExamID, x.SlotID, x.StudentID, x.RoomID, x.SeatID}
		}); err != nil {
		return 0, 0, err
	}
	if err := copyN("invigilator_assignments", []string{"schedule_id", "exam_id", "slot_id", "room_id", "user_id"}, len(p.Invigilators),
		func(i int) []any { x := p.Invigilators[i]; return []any{id, x.ExamID, x.SlotID, x.RoomID, x.UserID} }); err != nil {
		return 0, 0, err
	}
	if err := audit.Write(ctx, tx, audit.Entry{ActorID: p.CreatedBy, Action: "schedule.create", Entity: "schedule",
		EntityID: fmt.Sprint(id), New: map[string]any{"version": version, "status": "draft", "exams": len(p.Exams), "seats": len(p.Seats)}}); err != nil {
		return 0, 0, err
	}
	return id, version, tx.Commit(ctx)
}

// PublishSchedule, taslağı yayınlar. Dönemde başka bir yayınlanmış sürüm varsa o
// taslağa döner (dönem başına tek yayınlanmış sürüm; `archived` durumu yok, V-3).
// Zaten yayınlanmışsa ErrConflict. Denetim kaydı aynı transaction'da.
func PublishSchedule(ctx context.Context, pool *pgxpool.Pool, scheduleID, actor int64) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var period int64
	var status string
	err = tx.QueryRow(ctx, `SELECT period_id, status FROM "wm-schedules" WHERE id = $1 FOR UPDATE`, scheduleID).Scan(&period, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status == "published" {
		return ErrConflict
	}
	// Dönem kilidi: eşzamanlı iki yayınlama sıraya girer.
	if _, err := tx.Exec(ctx, `SELECT 1 FROM "wm-exam_periods" WHERE id = $1 FOR UPDATE`, period); err != nil {
		return err
	}
	var prev *int64
	_ = tx.QueryRow(ctx, `UPDATE "wm-schedules" SET status = 'draft', published_at = NULL
		WHERE period_id = $1 AND status = 'published' RETURNING id`, period).Scan(&prev)
	if _, err := tx.Exec(ctx, `UPDATE "wm-schedules" SET status = 'published', published_at = now() WHERE id = $1`, scheduleID); err != nil {
		return err
	}
	if err := audit.Write(ctx, tx, audit.Entry{ActorID: actor, Action: "schedule.publish", Entity: "schedule",
		EntityID: fmt.Sprint(scheduleID), Old: map[string]any{"status": status, "previous_published": prev},
		New: map[string]any{"status": "published"}}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
