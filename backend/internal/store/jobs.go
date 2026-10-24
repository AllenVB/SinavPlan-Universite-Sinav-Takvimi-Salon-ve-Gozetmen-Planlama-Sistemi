package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Job, asenkron takvim işi.
type Job struct {
	ID         int64           `json:"id"`
	PeriodID   int64           `json:"period_id"`
	Status     string          `json:"status"` // queued | running | succeeded | failed
	Params     json.RawMessage `json:"params"`
	Result     json.RawMessage `json:"result,omitempty"`
	Error      *string         `json:"error,omitempty"`
	ScheduleID *int64          `json:"schedule_id,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
	StartedAt  *time.Time      `json:"started_at,omitempty"`
	FinishedAt *time.Time      `json:"finished_at,omitempty"`
}

// EnqueueJob, dönem için bir iş kuyruğa alır. Aynı dönemde bekleyen/çalışan iş varsa
// ErrConflict (dönem satırı kilitlenir; eşzamanlı iki istek ikisi de iş açamaz).
func EnqueueJob(ctx context.Context, pool *pgxpool.Pool, periodID int64, params any, actor int64) (int64, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var one int
	if err := tx.QueryRow(ctx, `SELECT 1 FROM exam_periods WHERE id = $1 FOR UPDATE`, periodID).Scan(&one); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	var busy int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM schedule_jobs WHERE period_id = $1 AND status IN ('queued','running')`, periodID).Scan(&busy); err != nil {
		return 0, err
	}
	if busy > 0 {
		return 0, ErrConflict
	}
	pj, _ := json.Marshal(params)
	var by any
	if actor != 0 {
		by = actor
	}
	var id int64
	if err := tx.QueryRow(ctx, `INSERT INTO schedule_jobs (period_id, params, created_by) VALUES ($1,$2,$3) RETURNING id`, periodID, pj, by).Scan(&id); err != nil {
		return 0, err
	}
	return id, tx.Commit(ctx)
}

// GetJob, işi döner.
func GetJob(ctx context.Context, pool *pgxpool.Pool, id int64) (Job, error) {
	var j Job
	err := pool.QueryRow(ctx, `SELECT id, period_id, status, params, result, error, schedule_id, created_at, started_at, finished_at
		FROM schedule_jobs WHERE id = $1`, id).
		Scan(&j.ID, &j.PeriodID, &j.Status, &j.Params, &j.Result, &j.Error, &j.ScheduleID, &j.CreatedAt, &j.StartedAt, &j.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	return j, err
}

// StartJob, işi çalışıyor işaretler.
func StartJob(ctx context.Context, pool *pgxpool.Pool, id int64) error {
	_, err := pool.Exec(ctx, `UPDATE schedule_jobs SET status='running', started_at=now() WHERE id=$1 AND status='queued'`, id)
	return err
}

// FinishJob, işi sonuçlandırır.
func FinishJob(ctx context.Context, pool *pgxpool.Pool, id int64, scheduleID int64, result any, jobErr error) error {
	rj, _ := json.Marshal(result)
	if jobErr != nil {
		_, err := pool.Exec(ctx, `UPDATE schedule_jobs SET status='failed', error=$2, result=$3, finished_at=now() WHERE id=$1`,
			id, jobErr.Error(), rj)
		return err
	}
	_, err := pool.Exec(ctx, `UPDATE schedule_jobs SET status='succeeded', schedule_id=$2, result=$3, finished_at=now() WHERE id=$1`,
		id, scheduleID, rj)
	return err
}

// FailInterruptedJobs, süreç yeniden başlarken yarıda kalmış işleri başarısız işaretler
// (goroutine'ler süreçle birlikte ölür; iş sonsuza dek "running" kalmasın).
func FailInterruptedJobs(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	tag, err := pool.Exec(ctx, `UPDATE schedule_jobs SET status='failed', finished_at=now(),
		error='sunucu yeniden başlatıldı; iş yarıda kaldı' WHERE status IN ('queued','running')`)
	return tag.RowsAffected(), err
}
