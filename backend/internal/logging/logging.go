// Package logging, log/slog tabanlı JSON logger'ı kurar ve hassas alanların
// log'a sızmasını engeller.
//
// Gereksinim: parola, token ve QR sırrı asla loglanmaz. Bunu "dikkatli ol"
// kuralına bırakmak yerine makineye bağladık: ReplaceAttr kancası, anahtar adı
// hassas kabul edilen her niteliğin değerini değiştirir. Böylece çağıran yerde
// yanlışlıkla slog.String("password", ...) yazılsa bile değer log'a düşmez.
package logging

import (
	"io"
	"log/slog"
	"strings"
)

// Redacted, hassas bir değerin yerine yazılan sabit.
const Redacted = "[REDACTED]"

// sensitiveKeys, tam eşleşme ile hassas sayılan nitelik adları.
var sensitiveKeys = map[string]struct{}{
	"password":        {},
	"password_hash":   {},
	"passwordhash":    {},
	"pass":            {},
	"secret":          {},
	"token":           {},
	"access_token":    {},
	"refresh_token":   {},
	"token_hash":      {},
	"authorization":   {},
	"jwt":             {},
	"qr_secret":       {},
	"qr_token":        {},
	"hmac":            {},
	"api_key":         {},
	"apikey":          {},
	"private_key":     {},
	"set-cookie":      {},
	"cookie":          {},
	"database_url":    {},
	"idempotency_key": {},
}

// sensitiveSubstrings, ad içinde geçmesi yeterli olan kalıplar. Tam listeyi
// saymak mümkün olmadığı için bu ikinci savunma hattını ekledik.
var sensitiveSubstrings = []string{
	"password", "secret", "token", "credential", "hmac", "apikey", "api_key",
	"private_key", "passwd",
}

// IsSensitive, verilen nitelik adının hassas olup olmadığını söyler.
func IsSensitive(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	if _, ok := sensitiveKeys[k]; ok {
		return true
	}
	for _, frag := range sensitiveSubstrings {
		if strings.Contains(k, frag) {
			return true
		}
	}
	return false
}

// redact, slog'un her nitelik için çağırdığı kanca. Hassas anahtarların
// değerini sabitle değiştirir; gruplara da özyinelemeli iner.
func redact(_ []string, a slog.Attr) slog.Attr {
	if IsSensitive(a.Key) {
		return slog.String(a.Key, Redacted)
	}
	if a.Value.Kind() == slog.KindGroup {
		attrs := a.Value.Group()
		out := make([]slog.Attr, 0, len(attrs))
		for _, inner := range attrs {
			out = append(out, redact(nil, inner))
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(out...)}
	}
	return a
}

// New, JSON biçiminde yazan ve redaksiyon uygulayan bir logger döner.
func New(w io.Writer, level string) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       parseLevel(level),
		ReplaceAttr: redact,
	}))
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
