// Migrasyon çalıştırıcısı. Compose'da tek seferlik servis olarak, şema sahibi
// rolle (MIGRATE_DATABASE_URL) çalışır.
//
// Yaptıkları:
//  1. Kısıtlı uygulama rolünü (APP_DB_USER) oluşturur veya parolasını günceller.
//     API bu rolle bağlanır; audit_log üzerinde UPDATE/DELETE yetkisi verilmez
//     (bkz. PROJECT_PLAN V-8).
//  2. Şema migrasyonları — HENÜZ UYGULANMADI, migrations/ dizini Faz 2'de
//     yazılacak.
//
// Tüm adımlar idempotent: tekrar çalıştırmak güvenlidir.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"time"

	"github.com/AllenVB/sinavplan/backend/internal/logging"
	"github.com/AllenVB/sinavplan/backend/internal/store"
	"github.com/jackc/pgx/v5"
)

// roleNamePattern, kabul ettiğimiz rol adlarını sade tanımlayıcılarla sınırlar.
// Alıntılama zaten format(%I) ile güvenli; bu ikinci savunma hattı yalnızca
// yanlış yapılandırmayı erkenden ve okunur bir hatayla yakalamak için.
var roleNamePattern = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

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
	appUser := os.Getenv("APP_DB_USER")
	appPassword := os.Getenv("APP_DB_PASSWORD")
	if appUser == "" || appPassword == "" {
		return fmt.Errorf("APP_DB_USER ve APP_DB_PASSWORD zorunlu")
	}
	if !roleNamePattern.MatchString(appUser) {
		return fmt.Errorf("APP_DB_USER geçersiz: küçük harf, rakam ve _ içermeli")
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

	if err := ensureAppRole(ctx, db, appUser, appPassword); err != nil {
		return err
	}
	log.Info("uygulama rolü hazır", slog.String("role", appUser))

	log.Warn("şema migrasyonları HENÜZ UYGULANMADI — migrations/ dizini Faz 2'de yazılacak",
		slog.String("durum", "yer tutucu"))
	return nil
}

// ensureAppRole, kısıtlı uygulama rolünü idempotent biçimde kurar.
//
// CREATE ROLE ve ALTER ROLE ... PASSWORD bağlama parametresi ($1) kabul
// etmez; bu yüzden ifadeyi Postgres'in kendi format() fonksiyonuyla, %I
// (tanımlayıcı) ve %L (değişmez) alıntılamasıyla sunucu tarafında üretiyoruz.
// Rol adını veya parolayı Go'da dizeye eklemek SQL enjeksiyonuna açık olurdu.
func ensureAppRole(ctx context.Context, db *store.DB, user, password string) error {
	tx, err := db.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("transaction başlatılamadı: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var exists bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)`, user,
	).Scan(&exists); err != nil {
		return fmt.Errorf("rol sorgulanamadı: %w", err)
	}

	// Rol varsa parolayı yine de güncelliyoruz: .env'de parola değiştiyse
	// yeniden çalıştırma onu uygulasın.
	tmpl := `CREATE ROLE %I LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE`
	if exists {
		tmpl = `ALTER ROLE %I WITH LOGIN PASSWORD %L`
	}

	var stmt string
	if err := tx.QueryRow(ctx, `SELECT format($1, $2::text, $3::text)`,
		tmpl, user, password,
	).Scan(&stmt); err != nil {
		return fmt.Errorf("rol ifadesi hazırlanamadı: %w", err)
	}
	if _, err := tx.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("rol oluşturulamadı: %w", err)
	}

	// Asgari yetkiler: bağlanma ve public şemasını kullanma. Tablo bazlı
	// yetkiler (audit_log hariç UPDATE/DELETE) şema migrasyonlarında verilecek.
	// pgx hazırlanmış ifade kullandığı için her komut ayrı Exec ile gider;
	// noktalı virgülle birleştirilmiş çoklu komut reddedilirdi.
	var grantConnect, grantUsage string
	if err := tx.QueryRow(ctx,
		`SELECT format('GRANT CONNECT ON DATABASE %I TO %I', current_database(), $1::text),
		        format('GRANT USAGE ON SCHEMA public TO %I', $1::text)`, user,
	).Scan(&grantConnect, &grantUsage); err != nil {
		return fmt.Errorf("yetki ifadesi hazırlanamadı: %w", err)
	}
	for _, g := range []string{grantConnect, grantUsage} {
		if _, err := tx.Exec(ctx, g); err != nil {
			return fmt.Errorf("yetki verilemedi: %w", err)
		}
	}

	return tx.Commit(ctx)
}
