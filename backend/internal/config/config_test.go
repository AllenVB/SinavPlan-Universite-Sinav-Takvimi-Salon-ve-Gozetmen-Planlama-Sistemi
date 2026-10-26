package config

import (
	"strings"
	"testing"
	"time"
)

// validEnv, geçerli bir asgari ortam kurar.
func validEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/db?sslmode=disable")
	t.Setenv("JWT_SECRET", strings.Repeat("a", 32))
	t.Setenv("QR_HMAC_SECRET", strings.Repeat("b", 32))
}

func TestLoadAppliesDefaults(t *testing.T) {
	validEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, beklenen :8080", cfg.HTTPAddr)
	}
	if cfg.RequestTimeout != 15*time.Second {
		t.Errorf("RequestTimeout = %v, beklenen 15s", cfg.RequestTimeout)
	}
	if cfg.ShutdownTimeout != 15*time.Second {
		t.Errorf("ShutdownTimeout = %v, beklenen 15s", cfg.ShutdownTimeout)
	}
	if cfg.AccessTokenTTL != 15*time.Minute {
		t.Errorf("AccessTokenTTL = %v, beklenen 15m", cfg.AccessTokenTTL)
	}
	if cfg.RefreshTokenTTL != 7*24*time.Hour {
		t.Errorf("RefreshTokenTTL = %v, beklenen 168h", cfg.RefreshTokenTTL)
	}
	if cfg.SeedValue != 42 {
		t.Errorf("SeedValue = %d, beklenen 42", cfg.SeedValue)
	}
}

// Eksik zorunlu alanlar TEK seferde bildirilmeli: kullanıcı her çalıştırmada
// bir hatayı düzeltmek zorunda kalmasın.
func TestLoadReportsAllMissingRequiredFieldsAtOnce(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("JWT_SECRET", "")
	t.Setenv("QR_HMAC_SECRET", "")

	_, err := Load()
	if err == nil {
		t.Fatal("hata beklenmişti")
	}
	msg := err.Error()
	for _, want := range []string{"DATABASE_URL", "JWT_SECRET", "QR_HMAC_SECRET"} {
		if !strings.Contains(msg, want) {
			t.Errorf("hata mesajı %q içermiyor: %s", want, msg)
		}
	}
}

// Kısa sır kabul edilmemeli: 32 karakter altı JWT/HMAC anahtarı zayıf.
func TestLoadRejectsShortSecrets(t *testing.T) {
	validEnv(t)
	t.Setenv("JWT_SECRET", "kisa")

	_, err := Load()
	if err == nil {
		t.Fatal("kısa JWT_SECRET reddedilmeliydi")
	}
	if !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Errorf("hata JWT_SECRET'a işaret etmeli: %v", err)
	}
}

func TestLoadRejectsInvalidDuration(t *testing.T) {
	validEnv(t)
	t.Setenv("REQUEST_TIMEOUT", "onbes saniye")

	_, err := Load()
	if err == nil {
		t.Fatal("geçersiz süre reddedilmeliydi")
	}
	if !strings.Contains(err.Error(), "REQUEST_TIMEOUT") {
		t.Errorf("hata REQUEST_TIMEOUT'a işaret etmeli: %v", err)
	}
}

func TestLoadRejectsNonPositiveDuration(t *testing.T) {
	validEnv(t)
	t.Setenv("SHUTDOWN_TIMEOUT", "0s")

	if _, err := Load(); err == nil {
		t.Fatal("sıfır süre reddedilmeliydi")
	}
}

func TestLoadRejectsNonNumericSeed(t *testing.T) {
	validEnv(t)
	t.Setenv("SEED", "kirkiki")

	if _, err := Load(); err == nil {
		t.Fatal("sayısal olmayan SEED reddedilmeliydi")
	}
}

func TestLoadHonoursOverrides(t *testing.T) {
	validEnv(t)
	t.Setenv("HTTP_ADDR", ":9999")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("APP_ENV", "production")
	t.Setenv("REQUEST_TIMEOUT", "30s")
	t.Setenv("SEED", "7")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}
	if cfg.HTTPAddr != ":9999" {
		t.Errorf("HTTPAddr = %q", cfg.HTTPAddr)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q", cfg.LogLevel)
	}
	if cfg.Env != "production" {
		t.Errorf("Env = %q", cfg.Env)
	}
	if cfg.RequestTimeout != 30*time.Second {
		t.Errorf("RequestTimeout = %v", cfg.RequestTimeout)
	}
	if cfg.SeedValue != 7 {
		t.Errorf("SeedValue = %d", cfg.SeedValue)
	}
}

func TestLoadValidatesDemoCheckinCode(t *testing.T) {
	validEnv(t)
	t.Setenv("DEMO_CHECKIN_CODE", "111111")
	cfg, err := Load()
	if err != nil || cfg.DemoCheckinCode != "111111" {
		t.Fatalf("geçerli demo kodu: %q %v", cfg.DemoCheckinCode, err)
	}
	for _, bad := range []string{"12345", "1234567", "abcdef"} {
		t.Setenv("DEMO_CHECKIN_CODE", bad)
		if _, err := Load(); err == nil {
			t.Errorf("%q reddedilmeliydi", bad)
		}
	}
}
