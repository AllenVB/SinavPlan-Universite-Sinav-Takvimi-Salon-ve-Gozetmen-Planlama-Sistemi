// Migrasyon çalıştırıcısı. Compose'da tek seferlik servis olarak, şema sahibi
// rolle (MIGRATE_DATABASE_URL) çalışır.
//
// Kullanım: migrate [up|reset]   (varsayılan: up)
//
// up sırasıyla: uygulama rolünü kurar (APP_DB_USER), gömülü goose migrasyonlarını
// uygular, rol yetkilerini yeniden verir (audit_log ve attendance_events üzerinde
// UPDATE/DELETE/TRUNCATE geri alınır; PROJECT_PLAN V-8).
//
// reset, public şemasını SİLİP yeniden kurar (make reset-demo). APP_ENV=production
// iken reddedilir. Tüm adımlar idempotent: tekrar çalıştırmak güvenlidir.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/AllenVB/sinavplan/backend/internal/logging"
	"github.com/AllenVB/sinavplan/backend/internal/store"
)

func main() {
	log := logging.New(os.Stdout, os.Getenv("LOG_LEVEL"))
	if err := run(log); err != nil {
		log.Error("migrate başarısız", slog.String("err", err.Error()))
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	dsn := os.Getenv("MIGRATE_DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("MIGRATE_DATABASE_URL zorunlu")
	}
	mode := "up"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	if mode != "up" && mode != "reset" {
		return fmt.Errorf("bilinmeyen mod %q (up|reset)", mode)
	}
	if mode == "reset" && os.Getenv("APP_ENV") == "production" {
		return fmt.Errorf("reset production ortamında çalıştırılamaz")
	}
	appUser, appPassword := os.Getenv("APP_DB_USER"), os.Getenv("APP_DB_PASSWORD")
	if appUser == "" || appPassword == "" {
		return fmt.Errorf("APP_DB_USER ve APP_DB_PASSWORD zorunlu")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	db, err := store.Open(ctx, dsn)
	if err != nil {
		return fmt.Errorf("veritabanına bağlanılamadı: %w", err)
	}
	defer db.Close()
	if err := db.Ping(ctx); err != nil {
		return fmt.Errorf("veritabanı yanıt vermiyor: %w", err)
	}

	if mode == "reset" {
		if _, err := db.Pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
			return fmt.Errorf("şema sıfırlanamadı: %w", err)
		}
		log.Warn("public şeması silindi ve yeniden kuruldu (reset)")
	}

	if err := store.EnsureAppRole(ctx, db, appUser, appPassword); err != nil {
		return err
	}
	log.Info("uygulama rolü hazır", slog.String("role", appUser))

	applied, err := store.Migrate(ctx, dsn)
	if err != nil {
		return err
	}
	if len(applied) == 0 {
		log.Info("şema güncel, uygulanacak migrasyon yok")
	} else {
		log.Info("migrasyonlar uygulandı", slog.Any("sürümler", applied))
	}

	if err := store.ApplyGrants(ctx, db, appUser); err != nil {
		return err
	}
	log.Info("yetkiler uygulandı; audit_log ve attendance_events eklemeli-yalnız")
	return nil
}
