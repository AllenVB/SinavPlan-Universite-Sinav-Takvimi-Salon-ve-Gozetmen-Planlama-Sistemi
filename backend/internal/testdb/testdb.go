// Package testdb, gerçek PostgreSQL gerektiren testler için geçici veritabanı kurar.
//
// TEST_DATABASE_URL, veritabanı oluşturma yetkisi olan bir kullanıcıya işaret etmeli
// (CI'da service container). Her çağrı kendi veritabanını yaratır, migrasyonları ve
// uygulama rolü yetkilerini uygular, test bitince siler. Değişken yoksa test
// BAŞARISIZ olur; yalnızca `go test -short` ile atlanır (sessiz atlama yok).
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net/url"
	"os"
	"testing"

	"github.com/AllenVB/sinavplan/backend/internal/simdata"
	"github.com/AllenVB/sinavplan/backend/internal/store"
	"github.com/jackc/pgx/v5"
)

// AppRole, testlerde kullanılan kısıtlı uygulama rolü.
const AppRole, appPassword = "sinavplan_test_app", "test_app_pw_0123456789"

// DemoPassword, küçük test veri kümesindeki tüm hesapların parolası.
const DemoPassword = "Demo!1234"

// Env, bir test veritabanı.
type Env struct {
	OwnerDSN, AppDSN string
	Owner, App       *store.DB
}

// New, boş (migrasyonları uygulanmış) bir veritabanı kurar.
func New(t testing.TB) *Env {
	t.Helper()
	admin := os.Getenv("TEST_DATABASE_URL")
	if admin == "" {
		if testing.Short() {
			t.Skip("TEST_DATABASE_URL yok; -short ile atlandı")
		}
		t.Fatal("TEST_DATABASE_URL gerekli (ör. postgres://u:p@host:5432/postgres?sslmode=disable); atlamak için -short")
	}
	ctx := context.Background()
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	name := "t_" + hex.EncodeToString(suffix)

	ac, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatalf("admin bağlantısı: %v", err)
	}
	if _, err := ac.Exec(ctx, `CREATE DATABASE `+name); err != nil {
		t.Fatalf("veritabanı oluşturulamadı: %v", err)
	}
	env := &Env{OwnerDSN: withDB(admin, name)}
	t.Cleanup(func() {
		env.App.Close()
		env.Owner.Close()
		_, _ = ac.Exec(ctx, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`)
		_ = ac.Close(ctx)
	})

	if env.Owner, err = store.Open(ctx, env.OwnerDSN); err != nil {
		t.Fatal(err)
	}
	// Roller küme geneline aittir; paralel çalışan test paketleri aynı rolü aynı anda
	// ALTER ederse Postgres "tuple concurrently updated" verir. Kurulumu, herkesin
	// bağlandığı admin veritabanındaki bir advisory lock ile sıraya koyuyoruz.
	if _, err := ac.Exec(ctx, `SELECT pg_advisory_lock(727274)`); err != nil {
		t.Fatalf("advisory lock: %v", err)
	}
	err = store.EnsureAppRole(ctx, env.Owner, AppRole, appPassword)
	_, _ = ac.Exec(ctx, `SELECT pg_advisory_unlock(727274)`)
	if err != nil {
		t.Fatalf("rol: %v", err)
	}
	if _, err := store.Migrate(ctx, env.OwnerDSN); err != nil {
		t.Fatalf("migrasyon: %v", err)
	}
	if err := store.ApplyGrants(ctx, env.Owner, AppRole); err != nil {
		t.Fatalf("yetkiler: %v", err)
	}
	u, _ := url.Parse(env.OwnerDSN)
	u.User = url.UserPassword(AppRole, appPassword)
	env.AppDSN = u.String()
	if env.App, err = store.Open(ctx, env.AppDSN); err != nil {
		t.Fatal(err)
	}
	return env
}

// SmallDataset, hızlı testler için küçük ama yapısı aynı veri kümesi.
func SmallDataset(t testing.TB, seed int64) *simdata.Dataset {
	t.Helper()
	ds, err := simdata.Generate(simdata.Config{Seed: seed, Exams: 48, Students: 200, Rooms: 10,
		Days: 5, SessionsPerDay: 3, DisabilityRate: 0.02})
	if err != nil {
		t.Fatal(err)
	}
	return ds
}

// Seeded, küçük veri kümesi yüklenmiş bir veritabanı kurar.
func Seeded(t testing.TB) (*Env, *simdata.Dataset) {
	t.Helper()
	return SeededWith(t, SmallDataset(t, 7))
}

// SeededWith, verilen veri kümesini yükler.
func SeededWith(t testing.TB, ds *simdata.Dataset) (*Env, *simdata.Dataset) {
	t.Helper()
	env := New(t)
	conn, err := pgx.Connect(context.Background(), env.OwnerDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	if err := store.Seed(context.Background(), conn, ds, DemoPassword, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return env, ds
}

func withDB(dsn, name string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		panic(err)
	}
	u.Path = "/" + name
	return u.String()
}
