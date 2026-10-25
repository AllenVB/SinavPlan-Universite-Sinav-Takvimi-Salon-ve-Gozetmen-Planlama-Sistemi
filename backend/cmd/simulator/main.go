// Veri üretici / simülatör.
//
//	simulator seed --seed=N   deterministik demo verisini yükler (idempotent)
//	simulator live --duration sınav günü yoklama olaylarını API üzerinden üretir
//	simulator demo --duration yayınlanmış çizelgenin ilk oturumu için canlı yoklama gösterisi (make demo)
//
// Veritabanını ilk haline döndürmek için `make reset-demo` (migrate reset + seed).
//
// live, sınav günü yoklama olaylarını API üzerinden üretir (bkz. live.go).
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/AllenVB/sinavplan/backend/internal/logging"
	"github.com/AllenVB/sinavplan/backend/internal/planning"
	"github.com/AllenVB/sinavplan/backend/internal/simdata"
	"github.com/AllenVB/sinavplan/backend/internal/store"
	"github.com/jackc/pgx/v5"
)

func main() {
	log := logging.New(os.Stdout, os.Getenv("LOG_LEVEL"))
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "kullanım: simulator <seed|live|demo> [bayraklar]")
		os.Exit(2)
	}
	mode := os.Args[1]
	fs := flag.NewFlagSet(mode, flag.ExitOnError)
	seed := fs.Int64("seed", envInt[int64]("SEED", 42), "deterministik üretim tohumu")
	duration := fs.String("duration", "2m", "live modunun süresi")
	if err := fs.Parse(os.Args[2:]); err != nil {
		os.Exit(2)
	}

	switch mode {
	case "seed":
		if err := runSeed(log, *seed); err != nil {
			log.Error("seed başarısız", slog.String("err", err.Error()))
			os.Exit(1)
		}
	case "demo":
		d, err := time.ParseDuration(*duration)
		if err != nil || d <= 0 {
			log.Error("geçersiz --duration", slog.String("value", *duration))
			os.Exit(2)
		}
		if err := runDemo(log, d); err != nil {
			log.Error("demo başarısız", slog.String("err", err.Error()))
			os.Exit(1)
		}
	case "live":
		d, err := time.ParseDuration(*duration)
		if err != nil || d <= 0 {
			log.Error("geçersiz --duration", slog.String("value", *duration))
			os.Exit(2)
		}
		if err := runLive(log, *seed, d); err != nil {
			log.Error("live başarısız", slog.String("err", err.Error()))
			os.Exit(1)
		}
	default:
		log.Error("bilinmeyen mod", slog.String("mode", mode))
		os.Exit(2)
	}
}

func runSeed(log *slog.Logger, seed int64) error {
	dsn := os.Getenv("DATABASE_URL")
	pw := os.Getenv("DEMO_PASSWORD")
	if dsn == "" || pw == "" {
		return fmt.Errorf("DATABASE_URL ve DEMO_PASSWORD zorunlu")
	}
	cfg := simdata.DefaultConfig(seed)
	cfg.Exams = envInt("SIM_EXAMS", cfg.Exams)
	cfg.Students = envInt("SIM_STUDENTS", cfg.Students)
	cfg.Rooms = envInt("SIM_ROOMS", cfg.Rooms)
	cfg.Days = envInt("SCHEDULING_DAYS", cfg.Days)
	cfg.SessionsPerDay = envInt("SCHEDULING_SESSIONS_PER_DAY", cfg.SessionsPerDay)

	ds, err := simdata.Generate(cfg)
	if err != nil {
		return err
	}
	log.Info("veri üretildi", slog.Int64("seed", seed), slog.String("hash", ds.Hash()[:12]),
		slog.Int("sınav", len(ds.Exams)), slog.Int("öğrenci", len(ds.Students)),
		slog.Int("salon", len(ds.Rooms)), slog.Int("kayıt", len(ds.Enrollments)))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("veritabanına bağlanılamadı: %w", err)
	}
	defer conn.Close(ctx)
	if err := store.Seed(ctx, conn, ds, pw, log); err != nil {
		return err
	}
	// Ayrı bir demo dönemi: küçük, anlaşılır, hazır bir sınav (Prompt Mühendisliği). Tam dönemi etkilemez.
	if os.Getenv("DEMO_EXAM") != "off" {
		db, err := store.Open(ctx, dsn)
		if err != nil {
			return err
		}
		defer db.Close()
		return planning.SeedDemoExam(ctx, db.Pool, time.Now(), log)
	}
	return nil
}

func envInt[T int | int64](key string, def T) T {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return T(n)
		}
	}
	return def
}
