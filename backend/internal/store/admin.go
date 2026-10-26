package store

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"

	"github.com/AllenVB/sinavplan/backend/migrations"
	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib" // "pgx" sürücüsünü database/sql'e kaydeder
	"github.com/pressly/goose/v3"
)

// roleNamePattern, kabul edilen rol adlarını sade tanımlayıcılarla sınırlar.
// Alıntılama zaten format(%I) ile güvenli; bu, yanlış yapılandırmayı erkenden
// okunur bir hatayla yakalamak için ikinci savunma hattıdır.
var roleNamePattern = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

// Migrate, gömülü goose migrasyonlarını uygular ve uygulanan sürümleri döner.
// goose database/sql ister; pgx'in stdlib köprüsüyle aynı DSN kullanılır.
func Migrate(ctx context.Context, dsn string) ([]int64, error) {
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("sql bağlantısı açılamadı: %w", err)
	}
	defer sqlDB.Close()

	opt, err := newGooseProviderOption() // sürüm tablosu da "wm-" önekli (tables.go)
	if err != nil {
		return nil, err
	}
	provider, err := goose.NewProvider("", sqlDB, migrations.FS, opt) // özel store verildiğinde dialect boş olmalı (goose kuralı)
	if err != nil {
		return nil, fmt.Errorf("goose kurulamadı: %w", err)
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return nil, fmt.Errorf("migrasyon başarısız: %w", err)
	}
	applied := make([]int64, 0, len(results))
	for _, r := range results {
		applied = append(applied, r.Source.Version)
	}
	return applied, nil
}

// EnsureAppRole, kısıtlı uygulama rolünü idempotent biçimde kurar.
//
// CREATE ROLE ve ALTER ROLE ... PASSWORD bağlama parametresi ($1) kabul etmez;
// bu yüzden ifadeyi Postgres'in format() fonksiyonuyla, %I (tanımlayıcı) ve %L
// (değişmez) alıntılamasıyla sunucu tarafında üretiyoruz. Rol adını veya parolayı
// Go'da dizeye eklemek SQL enjeksiyonuna açık olurdu.
func EnsureAppRole(ctx context.Context, db *DB, user, password string) error {
	if !roleNamePattern.MatchString(user) {
		return fmt.Errorf("rol adı geçersiz: küçük harf, rakam ve _ içermeli")
	}
	tx, err := db.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("transaction başlatılamadı: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)`, user).Scan(&exists); err != nil {
		return fmt.Errorf("rol sorgulanamadı: %w", err)
	}
	// Rol varsa parolayı yine de güncelliyoruz: .env'de parola değiştiyse yeniden
	// çalıştırma onu uygulasın.
	tmpl := `CREATE ROLE %I LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE`
	if exists {
		tmpl = `ALTER ROLE %I WITH LOGIN PASSWORD %L`
	}
	var stmt string
	if err := tx.QueryRow(ctx, `SELECT format($1, $2::text, $3::text)`, tmpl, user, password).Scan(&stmt); err != nil {
		return fmt.Errorf("rol ifadesi hazırlanamadı: %w", err)
	}
	if _, err := tx.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("rol oluşturulamadı: %w", err)
	}

	// pgx hazırlanmış ifade kullandığı için her komut ayrı Exec ile gider.
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

// ApplyGrants, uygulama rolünün tablo yetkilerini her çalışmada yeniden kurar
// (idempotent): yeni tablolar otomatik kapsanır, append-only tablolar geri alınır.
// Sahip rol zaten trigger'larla durdurulur; bu ikinci katmandır.
func ApplyGrants(ctx context.Context, db *DB, user string) error {
	if !roleNamePattern.MatchString(user) {
		return fmt.Errorf("rol adı geçersiz")
	}
	tmpls := []string{
		`GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO %I`,
		`GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO %I`,
		`REVOKE UPDATE, DELETE, TRUNCATE ON "wm-audit_log", "wm-attendance_events" FROM %I`,
		`REVOKE ALL ON "wm-goose_db_version" FROM %I`,
	}
	for _, t := range tmpls {
		var stmt string
		if err := db.Pool.QueryRow(ctx, `SELECT format($1, $2::text)`, t, user).Scan(&stmt); err != nil {
			return fmt.Errorf("yetki ifadesi hazırlanamadı: %w", err)
		}
		if _, err := db.Pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("yetki uygulanamadı (%s): %w", t, err)
		}
	}
	return nil
}
