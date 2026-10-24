// Package store, veritabanı erişimini barındırır: pgxpool bağlantı havuzu,
// sqlc ile üretilen sorgular ve bunların üzerine kurulu repository'ler.
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DB, uygulamanın paylaştığı bağlantı havuzunu sarar.
type DB struct {
	Pool *pgxpool.Pool
}

// Open, havuzu kurar ve ilk bağlantıyı doğrular. Açılışta veritabanı yoksa
// hata döner; çağıran bunu ölümcül mü sayacağına kendi karar verir.
func Open(ctx context.Context, dsn string) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("DATABASE_URL çözümlenemedi: %w", err)
	}

	// Havuz boyutları, compose'daki tek API örneği ve Postgres'in varsayılan
	// 100 bağlantı sınırı düşünülerek seçildi.
	cfg.MaxConns = 20
	cfg.MinConns = 2
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 30 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("bağlantı havuzu kurulamadı: %w", err)
	}
	return &DB{Pool: pool}, nil
}

// Ping, veritabanının yanıt verdiğini doğrular. /health bu çağrıyı kullanır.
func (db *DB) Ping(ctx context.Context) error {
	if db == nil || db.Pool == nil {
		return fmt.Errorf("bağlantı havuzu kurulmadı")
	}
	return db.Pool.Ping(ctx)
}

// Close, havuzu kapatır.
func (db *DB) Close() {
	if db != nil && db.Pool != nil {
		db.Pool.Close()
	}
}
