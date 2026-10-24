package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestIsSensitive(t *testing.T) {
	sensitive := []string{
		"password", "Password", "PASSWORD", "password_hash", "refresh_token",
		"token_hash", "Authorization", "qr_secret", "jwt", "api_key",
		"user_password", "db_secret", "some_token_value", "HMAC",
	}
	for _, k := range sensitive {
		if !IsSensitive(k) {
			t.Errorf("%q hassas sayılmalıydı", k)
		}
	}

	safe := []string{"user_id", "exam_id", "email", "role", "status", "duration_ms", "count"}
	for _, k := range safe {
		if IsSensitive(k) {
			t.Errorf("%q hassas sayılmamalıydı", k)
		}
	}
}

// Hassas bir değerin, çağıran yanlışlıkla loglasa bile çıktıya düşmediğini
// kanıtlar. Bu testin amacı kuralı makineye bağlamak.
func TestLoggerRedactsSecrets(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, "info")

	log.Info("giriş denemesi",
		slog.String("email", "demo@example.com"),
		slog.String("password", "cokGizliParola123"),
		slog.String("refresh_token", "rt_abcdef123456"),
		slog.String("qr_secret", "qr_sirri_xyz"),
	)

	out := buf.String()
	for _, leaked := range []string{"cokGizliParola123", "rt_abcdef123456", "qr_sirri_xyz"} {
		if strings.Contains(out, leaked) {
			t.Errorf("hassas değer log'a sızdı: %q\nçıktı: %s", leaked, out)
		}
	}
	if !strings.Contains(out, Redacted) {
		t.Errorf("redaksiyon işareti bulunamadı, çıktı: %s", out)
	}
	if !strings.Contains(out, "demo@example.com") {
		t.Errorf("hassas olmayan alan kaybolmamalıydı, çıktı: %s", out)
	}
}

// Grup içine gömülmüş hassas alanların da temizlendiğini kanıtlar.
func TestLoggerRedactsNestedGroups(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, "info")

	log.Info("istek",
		slog.Group("request",
			slog.String("path", "/auth/login"),
			slog.Group("headers",
				slog.String("authorization", "Bearer gercek_token_degeri"),
			),
		),
	)

	out := buf.String()
	if strings.Contains(out, "gercek_token_degeri") {
		t.Errorf("gömülü grupta hassas değer sızdı: %s", out)
	}
	if !strings.Contains(out, "/auth/login") {
		t.Errorf("gömülü hassas olmayan alan kaybolmamalıydı: %s", out)
	}
}

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug": slog.LevelDebug, "info": slog.LevelInfo,
		"warn": slog.LevelWarn, "warning": slog.LevelWarn,
		"error": slog.LevelError, "": slog.LevelInfo, "saçma": slog.LevelInfo,
	}
	for in, want := range cases {
		if got := parseLevel(in); got != want {
			t.Errorf("parseLevel(%q) = %v, beklenen %v", in, got, want)
		}
	}
}
