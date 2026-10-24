// Package jobs, takvim oluşturmayı arka planda çalıştırır (F-3): POST işi kuyruğa alıp
// 202 döner, çalıştırıcı goroutine'de planlama zincirini koşturur, GET durum/sonuç verir.
// Ayrı bir worker servisi yerine API süreci içinde çalışır (PROJECT_PLAN K-6); süreç
// yeniden başlarsa yarıda kalan işler açılışta "failed" işaretlenir (store.FailInterruptedJobs).
package jobs

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/AllenVB/sinavplan/backend/internal/planning"
	"github.com/AllenVB/sinavplan/backend/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Params, takvim işinin parametreleri.
type Params struct {
	PeriodID   int64 `json:"period_id"`
	Seed       int64 `json:"seed"`
	Iterations int   `json:"iterations"`
}

// Sınırlar: iterasyon bütçesi süreyi belirler (1M ≈ 10 sn, 300 sınav).
const (
	DefaultIterations = 300_000
	MaxIterations     = 2_000_000
	jobTimeout        = 5 * time.Minute
)

// Runner, işleri çalıştırır; aynı anda en fazla maxParallel iş koşar.
type Runner struct {
	pool *pgxpool.Pool
	log  *slog.Logger
	sem  chan struct{}
	// OnDone, testlerde iş bitişini beklemek için (isteğe bağlı).
	OnDone func(jobID int64)
}

// NewRunner, çalıştırıcı kurar.
func NewRunner(pool *pgxpool.Pool, log *slog.Logger) *Runner {
	return &Runner{pool: pool, log: log, sem: make(chan struct{}, 2)}
}

// Submit, işi kuyruğa alır ve arka planda başlatır; iş kimliğini döner.
func (r *Runner) Submit(ctx context.Context, p Params, actor int64) (int64, error) {
	if p.Seed == 0 {
		p.Seed = 42
	}
	if p.Iterations <= 0 {
		p.Iterations = DefaultIterations
	}
	if p.Iterations > MaxIterations {
		p.Iterations = MaxIterations
	}
	id, err := store.EnqueueJob(ctx, r.pool, p.PeriodID, p, actor)
	if err != nil {
		return 0, err
	}
	// İstek bağlamından bağımsız: istek bitince iş iptal olmamalı.
	go r.run(id, p, actor)
	return id, nil
}

func (r *Runner) run(id int64, p Params, actor int64) {
	r.sem <- struct{}{}
	defer func() { <-r.sem }()
	ctx, cancel := context.WithTimeout(context.Background(), jobTimeout)
	defer cancel()
	defer func() {
		if r.OnDone != nil {
			r.OnDone(id)
		}
	}()
	finish := func(sid int64, result any, err error) {
		if ferr := store.FinishJob(context.Background(), r.pool, id, sid, result, err); ferr != nil {
			r.log.Error("iş sonucu yazılamadı", slog.Int64("job", id), slog.String("err", ferr.Error()))
		}
	}
	defer func() {
		if rec := recover(); rec != nil {
			r.log.Error("iş panikledi", slog.Int64("job", id), slog.Any("panic", rec))
			finish(0, nil, errors.New("beklenmeyen iç hata"))
		}
	}()

	if err := store.StartJob(ctx, r.pool, id); err != nil {
		finish(0, nil, err)
		return
	}
	ds, err := store.LoadDataset(ctx, r.pool, p.PeriodID, p.Seed)
	if err != nil {
		finish(0, nil, err)
		return
	}
	excused, err := store.ExcusedSlots(ctx, r.pool, ds)
	if err != nil {
		finish(0, nil, err)
		return
	}
	sp, rep, err := planning.BuildFrom(ds, p.Seed, p.Iterations, excused)
	if err != nil {
		finish(0, rep, err) // rep, çözümsüzlük raporunu taşır
		return
	}
	sp.CreatedBy = actor
	sid, version, err := store.CreateSchedule(ctx, r.pool, sp)
	if err != nil {
		finish(0, rep, err)
		return
	}
	finish(sid, map[string]any{"schedule_id": sid, "version": version, "report": rep}, nil)
	r.log.Info("takvim üretildi", slog.Int64("job", id), slog.Int64("schedule", sid), slog.Int("version", version))
}
