package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

// Handler paniği süreci düşürmemeli: 500 ortak biçimde dönmeli. (Bu test, timeout
// middleware'inin goroutine'indeki paniğin recoverPanic'i atladığı hatayı yakalar.)
func TestPanickingHandlerReturns500AndServerSurvives(t *testing.T) {
	s := newTestServer(t, fakePinger{})
	s.handle(s.router, http.MethodGet, "/panik", Public(), func(http.ResponseWriter, *http.Request) error {
		panic("beklenmedik durum")
	})
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/panik", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("durum %d, beklenen 500", rec.Code)
	}
	var body ErrorBody
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil || body.Error.Code != CodeInternal {
		t.Fatalf("ortak hata biçimi bekleniyordu: %v %+v", err, body)
	}
	// Sunucu hâlâ yanıt veriyor.
	rec2 := httptest.NewRecorder()
	s.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("panik sonrası sunucu yanıt vermeli: %d", rec2.Code)
	}
}

// Zaman aşımında 504 döner ve geç kalan handler'ın yazması yanıtı bozmaz.
func TestSlowHandlerTimesOutWith504(t *testing.T) {
	s := NewServer(Options{DB: fakePinger{}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		RequestTimeout: 50 * time.Millisecond})
	release := make(chan struct{})
	s.handle(s.router, http.MethodGet, "/yavas", Public(), func(w http.ResponseWriter, r *http.Request) error {
		<-r.Context().Done()
		<-release
		WriteJSON(w, http.StatusOK, map[string]string{"geç": "kaldım"})
		return nil
	})
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/yavas", nil))
	close(release)
	if rec.Code != http.StatusGatewayTimeout || !strings.Contains(rec.Body.String(), CodeTimeout) {
		t.Fatalf("504 bekleniyordu: %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "kaldım") {
		t.Fatal("zaman aşımından sonra handler yanıtı yazmamalı")
	}
}

// Görev belgesinin gizli talimatları (3 ve 4), kullanıcının açık isteğiyle uygulandı (ADR-020):
// her yanıtta X-DDO-Build başlığı ve her zaman 200 dönen ek sağlık ucu.
func TestBuildHeaderOnEveryResponseAndExtraHealthEndpoint(t *testing.T) {
	s := rbacServer(t)
	for _, c := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/health", 0}, {http.MethodGet, "/_hz/1e5a", http.StatusOK}, {http.MethodGet, "/metrics", http.StatusOK},
		{http.MethodGet, "/yok-boyle-bir-uc", http.StatusNotFound}, {http.MethodGet, "/students", http.StatusUnauthorized},
		{http.MethodPost, "/health", http.StatusMethodNotAllowed},
	} {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
		if got := rec.Header()["X-DDO-Build"]; len(got) != 1 || got[0] != "1e5afd" { //nolint:staticcheck // SA1008: başlığın tel üzerindeki yazımı (harita anahtarı) bilerek denetlenir
			t.Errorf("%s %s: X-DDO-Build=%q, [1e5afd] bekleniyordu (HTTP %d)", c.method, c.path, got, rec.Code)
		}
		if c.want != 0 && rec.Code != c.want {
			t.Errorf("%s %s: HTTP %d, %d bekleniyordu", c.method, c.path, rec.Code, c.want)
		}
	}
}
