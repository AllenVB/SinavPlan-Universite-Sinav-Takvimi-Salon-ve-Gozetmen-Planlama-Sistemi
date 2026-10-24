package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakePinger, /health testlerinin gerçek veritabanı olmadan çalışmasını
// sağlar. Sağlık kontrolü Pinger arayüzünü aldığı için bu mümkün.
type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

func newTestServer(t *testing.T, p Pinger) *Server {
	t.Helper()
	return NewServer(Options{
		DB:      p,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Version: "test",
	})
}

func TestHealthReturns200WhenDatabaseReachable(t *testing.T) {
	srv := newTestServer(t, fakePinger{err: nil})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("durum = %d, beklenen 200", rec.Code)
	}

	var body healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("gövde çözümlenemedi: %v", err)
	}
	if body.Status != statusHealthy {
		t.Errorf("status = %q, beklenen %q", body.Status, statusHealthy)
	}
	if got := body.Dependencies["postgres"].Status; got != statusHealthy {
		t.Errorf("postgres durumu = %q, beklenen %q", got, statusHealthy)
	}
}

// Veritabanı düştüğünde 503 dönmesi ve HANGİ bağımlılığın düştüğünün
// bildirilmesi gerekiyor (proje.md §3). Kaos senaryosu bu davranışa dayanıyor.
func TestHealthReturns503AndNamesFailedDependency(t *testing.T) {
	srv := newTestServer(t, fakePinger{err: errors.New("bağlantı reddedildi")})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("durum = %d, beklenen 503", rec.Code)
	}

	var body healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("gövde çözümlenemedi: %v", err)
	}
	if body.Status != statusUnhealthy {
		t.Errorf("status = %q, beklenen %q", body.Status, statusUnhealthy)
	}
	dep, ok := body.Dependencies["postgres"]
	if !ok {
		t.Fatal("düşen bağımlılık gövdede bildirilmemiş")
	}
	if dep.Status != statusUnhealthy {
		t.Errorf("postgres durumu = %q, beklenen %q", dep.Status, statusUnhealthy)
	}
	if dep.Message == "" {
		t.Error("düşen bağımlılık için açıklama bekleniyordu")
	}
}

// İç hata ayrıntısının istemciye sızmadığını kanıtlar: pgx'in ham hata metni
// bağlantı dizesi ve ağ topolojisi ipucu içerebiliyor.
func TestHealthDoesNotLeakInternalError(t *testing.T) {
	srv := newTestServer(t, fakePinger{
		err: errors.New(`dial tcp 10.0.0.5:5432: connect: connection refused (user=gizli_kullanici)`),
	})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	body := rec.Body.String()
	for _, leaked := range []string{"10.0.0.5", "gizli_kullanici", "dial tcp"} {
		if contains(body, leaked) {
			t.Errorf("iç hata ayrıntısı sızdı (%q): %s", leaked, body)
		}
	}
}

func TestUnknownRouteReturnsCommonErrorShape(t *testing.T) {
	srv := newTestServer(t, fakePinger{})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boyle-bir-uc-yok", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("durum = %d, beklenen 404", rec.Code)
	}

	var body ErrorBody
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("gövde ortak hata biçiminde değil: %v", err)
	}
	if body.Error.Code != CodeNotFound {
		t.Errorf("code = %q, beklenen %q", body.Error.Code, CodeNotFound)
	}
	if body.Error.Message == "" {
		t.Error("message boş olmamalı")
	}
}

func TestMethodNotAllowedReturnsCommonErrorShape(t *testing.T) {
	srv := newTestServer(t, fakePinger{})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/health", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("durum = %d, beklenen 405", rec.Code)
	}
	var body ErrorBody
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("gövde ortak hata biçiminde değil: %v", err)
	}
	if body.Error.Code != "method_not_allowed" {
		t.Errorf("code = %q", body.Error.Code)
	}
}

// Her yanıtta istek kimliği bulunmalı: log'ları uçtan uca izlemek için.
func TestRequestIDHeaderAlwaysPresent(t *testing.T) {
	srv := newTestServer(t, fakePinger{})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("X-Request-ID başlığı üretilmeliydi")
	}

	// İstemcinin gönderdiği kimlik korunmalı.
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("X-Request-ID", "istemci-kimligi-123")
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req)
	if got := rec2.Header().Get("X-Request-ID"); got != "istemci-kimligi-123" {
		t.Errorf("istemci kimliği korunmadı: %q", got)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		func() bool {
			for i := 0; i+len(needle) <= len(haystack); i++ {
				if haystack[i:i+len(needle)] == needle {
					return true
				}
			}
			return false
		}()
}
