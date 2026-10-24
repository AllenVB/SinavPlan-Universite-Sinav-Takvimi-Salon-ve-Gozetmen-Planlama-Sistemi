// Package config, tüm çalışma zamanı yapılandırmasını ortam değişkenlerinden
// okur. Zorunlu bir değer eksikse uygulama açılışta hata verir; sessiz
// varsayılanlarla yanlış ortamda çalışmaktansa açıkça durmayı tercih ediyoruz.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config, bir sürecin ihtiyaç duyduğu tüm ayarları taşır.
type Config struct {
	Env             string
	HTTPAddr        string
	DatabaseURL     string
	LogLevel        string
	RequestTimeout  time.Duration
	ShutdownTimeout time.Duration

	JWTSecret       string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	QRHMACSecret    string
	// QRTTL: sınav kartı QR jetonunun ömrü; arayüz bundan önce yeniler (dinamik QR).
	QRTTL time.Duration

	SeedValue int64

	// SeatingStrict: T-3'te ön/arka koltuklar da komşu sayılır (varsayılan kapalı).
	SeatingStrict bool
}

// Load, ortam değişkenlerini okuyup doğrular. Eksik veya geçersiz tüm alanları
// tek seferde toplayıp bildirir; böylece kullanıcı her seferinde bir hatayı
// düzeltmek yerine listenin tamamını görür.
func Load() (Config, error) {
	var problems []string

	cfg := Config{
		Env:      envOr("APP_ENV", "development"),
		HTTPAddr: envOr("HTTP_ADDR", ":8080"),
		LogLevel: envOr("LOG_LEVEL", "info"),
	}

	cfg.DatabaseURL = os.Getenv("DATABASE_URL")
	if cfg.DatabaseURL == "" {
		problems = append(problems, "DATABASE_URL zorunlu")
	}

	cfg.JWTSecret = os.Getenv("JWT_SECRET")
	if len(cfg.JWTSecret) < 32 {
		problems = append(problems, "JWT_SECRET zorunlu ve en az 32 karakter olmalı")
	}

	cfg.QRHMACSecret = os.Getenv("QR_HMAC_SECRET")
	if len(cfg.QRHMACSecret) < 32 {
		problems = append(problems, "QR_HMAC_SECRET zorunlu ve en az 32 karakter olmalı")
	}

	var err error
	if cfg.RequestTimeout, err = durationOr("REQUEST_TIMEOUT", 15*time.Second); err != nil {
		problems = append(problems, err.Error())
	}
	if cfg.ShutdownTimeout, err = durationOr("SHUTDOWN_TIMEOUT", 15*time.Second); err != nil {
		problems = append(problems, err.Error())
	}
	if cfg.AccessTokenTTL, err = durationOr("ACCESS_TOKEN_TTL", 15*time.Minute); err != nil {
		problems = append(problems, err.Error())
	}
	if cfg.QRTTL, err = durationOr("QR_TTL", 2*time.Minute); err != nil {
		problems = append(problems, err.Error())
	} else if cfg.QRTTL < 15*time.Second || cfg.QRTTL > time.Hour {
		problems = append(problems, "QR_TTL 15sn ile 1sa arasında olmalı")
	}
	if cfg.RefreshTokenTTL, err = durationOr("REFRESH_TOKEN_TTL", 7*24*time.Hour); err != nil {
		problems = append(problems, err.Error())
	}

	if raw := envOr("SEED", "42"); raw != "" {
		cfg.SeedValue, err = strconv.ParseInt(raw, 10, 64)
		if err != nil {
			problems = append(problems, "SEED tam sayı olmalı")
		}
	}

	switch v := strings.ToLower(os.Getenv("SEATING_STRICT_MODE")); v {
	case "", "false", "0", "no":
	case "true", "1", "yes":
		cfg.SeatingStrict = true
	default:
		problems = append(problems, "SEATING_STRICT_MODE true veya false olmalı")
	}

	if len(problems) > 0 {
		return Config{}, fmt.Errorf("yapılandırma geçersiz: %s", strings.Join(problems, "; "))
	}
	return cfg, nil
}

// ErrMissing, zorunlu bir ortam değişkeni bulunamadığında döner.
var ErrMissing = errors.New("zorunlu ortam değişkeni eksik")

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func durationOr(key string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s geçerli bir süre değil (örn. 15s, 7h)", key)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s pozitif olmalı", key)
	}
	return d, nil
}
