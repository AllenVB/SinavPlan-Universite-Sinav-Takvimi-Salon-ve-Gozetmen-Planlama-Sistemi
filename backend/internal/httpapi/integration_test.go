package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AllenVB/sinavplan/backend/internal/auth"
	"github.com/AllenVB/sinavplan/backend/internal/simdata"
	"github.com/AllenVB/sinavplan/backend/internal/testdb"
)

// Bu dosyadaki testler gerçek PostgreSQL ister (bkz. internal/testdb).

func apiServer(t *testing.T) *Server {
	t.Helper()
	env, _ := testdb.Seeded(t)
	svc, err := auth.NewService(env.App.Pool, auth.NewIssuer(rbacSecret, 15*time.Minute), 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return NewServer(Options{DB: env.App, Pool: env.App.Pool, Auth: svc,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
}

func call(t *testing.T, h http.Handler, method, path, token string, body any) (int, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rd)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func login(t *testing.T, h http.Handler, email string) (access, refresh string) {
	t.Helper()
	code, out := call(t, h, http.MethodPost, "/auth/login", "", map[string]string{"email": email, "password": testdb.DemoPassword})
	if code != http.StatusOK {
		t.Fatalf("%s girişi: %d %v", email, code, out)
	}
	return out["access_token"].(string), out["refresh_token"].(string)
}

func TestLoginRejectsBadCredentialsUniformly(t *testing.T) {
	s := apiServer(t)
	c1, b1 := call(t, s, http.MethodPost, "/auth/login", "", map[string]string{"email": simdata.EmailSecretary, "password": "yanlis"})
	c2, b2 := call(t, s, http.MethodPost, "/auth/login", "", map[string]string{"email": "yok@sinavplan.test", "password": "yanlis"})
	if c1 != http.StatusUnauthorized || c2 != http.StatusUnauthorized {
		t.Fatalf("yanlış parola ve olmayan kullanıcı 401 olmalı: %d %d", c1, c2)
	}
	// Kullanıcı adı numaralandırması olmasın: iki yanıt ayırt edilemez olmalı.
	if b1["error"].(map[string]any)["message"] != b2["error"].(map[string]any)["message"] {
		t.Fatalf("hata mesajları farklı: %v / %v", b1, b2)
	}
	if c, _ := call(t, s, http.MethodPost, "/auth/login", "", map[string]string{"email": ""}); c != http.StatusUnprocessableEntity {
		t.Fatalf("boş alanlar 422 olmalı: %d", c)
	}
}

func TestLoginEveryDemoRoleAndMe(t *testing.T) {
	s := apiServer(t)
	for email, role := range map[string]string{simdata.EmailSecretary: auth.RoleSecretary,
		simdata.EmailLecturer: auth.RoleLecturer, simdata.EmailInvigilator: auth.RoleInvigilator,
		simdata.EmailStudentAffairs: auth.RoleStudentAffairs, simdata.EmailStudent: auth.RoleStudent} {
		tok, _ := login(t, s, email)
		code, me := call(t, s, http.MethodGet, "/me", tok, nil)
		if code != http.StatusOK || me["roles"].([]any)[0] != role {
			t.Errorf("%s: /me %d %v", email, code, me)
		}
	}
}

func TestStudentOwnershipEndToEnd(t *testing.T) {
	s := apiServer(t)
	stu, _ := login(t, s, simdata.EmailStudent) // öğrenci #1'e bağlı
	sec, _ := login(t, s, simdata.EmailSecretary)
	lec, _ := login(t, s, simdata.EmailLecturer)

	checks := []struct {
		name, path, tok string
		want            int
	}{
		{"öğrenci kendi programı", "/students/1/exams", stu, http.StatusOK},
		{"öğrenci /me/exams", "/me/exams", stu, http.StatusOK},
		{"öğrenci başkasının programı", "/students/2/exams", stu, http.StatusForbidden},
		{"sekreter herkesinki", "/students/2/exams", sec, http.StatusOK},
		{"öğretim üyesi listede yok", "/students/2/exams", lec, http.StatusForbidden},
		{"sekreter /me/exams (öğrenci değil)", "/me/exams", sec, http.StatusForbidden},
		{"geçersiz id", "/students/abc/exams", sec, http.StatusUnprocessableEntity},
	}
	for _, c := range checks {
		if code, out := call(t, s, http.MethodGet, c.path, c.tok, nil); code != c.want {
			t.Errorf("%s: %d, beklenen %d (%v)", c.name, code, c.want, out)
		}
	}
}

func TestKeysetPaginationVisitsEveryStudentOnce(t *testing.T) {
	s := apiServer(t)
	tok, _ := login(t, s, simdata.EmailSecretary)
	seen := map[float64]bool{}
	last, pages := 0.0, 0
	path := "/students?limit=37"
	for {
		code, out := call(t, s, http.MethodGet, path, tok, nil)
		if code != http.StatusOK {
			t.Fatalf("sayfa %d: %d %v", pages, code, out)
		}
		pages++
		for _, it := range out["items"].([]any) {
			id := it.(map[string]any)["id"].(float64)
			if seen[id] || id <= last {
				t.Fatalf("id %v tekrar etti veya sıra bozuk (son %v)", id, last)
			}
			seen[id], last = true, id
		}
		next, _ := out["next_cursor"].(string)
		if next == "" {
			break
		}
		path = "/students?limit=37&after=" + next
	}
	if len(seen) != 200 || pages != 6 { // 200/37 -> 6 sayfa
		t.Fatalf("%d öğrenci, %d sayfa (200 ve 6 bekleniyordu)", len(seen), pages)
	}
}

func TestRefreshRotationAndReuseDetection(t *testing.T) {
	s := apiServer(t)
	_, r1 := login(t, s, simdata.EmailSecretary)
	refresh := func(tok string) (int, map[string]any) {
		return call(t, s, http.MethodPost, "/auth/refresh", "", map[string]string{"refresh_token": tok})
	}

	code, out := refresh(r1)
	if code != http.StatusOK {
		t.Fatalf("ilk yenileme: %d %v", code, out)
	}
	r2 := out["refresh_token"].(string)
	if r2 == r1 {
		t.Fatal("rotasyon yeni token üretmeli")
	}
	// Eski token tekrar kullanıldı -> çalınma kabul edilir, aile iptal edilir.
	if code, _ := refresh(r1); code != http.StatusUnauthorized {
		t.Fatalf("eski token reddedilmeli: %d", code)
	}
	if code, _ := refresh(r2); code != http.StatusUnauthorized {
		t.Fatalf("yeniden kullanım tespitinden sonra ailenin yeni token'ı da geçersiz olmalı: %d", code)
	}

	// Çıkış: aile iptal edilir.
	_, r3 := login(t, s, simdata.EmailSecretary)
	if code, _ := call(t, s, http.MethodPost, "/auth/logout", "", map[string]string{"refresh_token": r3}); code != http.StatusNoContent {
		t.Fatalf("logout: %d", code)
	}
	if code, _ := refresh(r3); code != http.StatusUnauthorized {
		t.Fatalf("çıkıştan sonra refresh reddedilmeli: %d", code)
	}
}

// Aynı refresh token'la eşzamanlı yenileme: FOR UPDATE kilidi sayesinde tam bir
// istek yeni token alır.
func TestConcurrentRefreshYieldsExactlyOneSuccess(t *testing.T) {
	s := apiServer(t)
	_, r := login(t, s, simdata.EmailSecretary)
	var ok, unauthorized atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, _ := call(t, s, http.MethodPost, "/auth/refresh", "", map[string]string{"refresh_token": r})
			switch code {
			case http.StatusOK:
				ok.Add(1)
			case http.StatusUnauthorized:
				unauthorized.Add(1)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 || unauthorized.Load() != 9 {
		t.Fatalf("1 başarı + 9 ret bekleniyordu: %d başarı, %d ret", ok.Load(), unauthorized.Load())
	}
}

// --- Idempotency --------------------------------------------------------------

type idemHarness struct {
	s       *Server
	calls   atomic.Int32
	gate    chan struct{} // nil değilse handler bunun kapanmasını bekler
	entered chan struct{}
	fail    atomic.Bool // true ise bir kez 500 döner
}

func newIdem(t *testing.T) *idemHarness {
	return &idemHarness{s: apiServer(t)}
}

func (h *idemHarness) do(userID int64, key, body string) *httptest.ResponseRecorder {
	inner := func(w http.ResponseWriter, r *http.Request) error {
		n := h.calls.Add(1)
		if h.entered != nil {
			h.entered <- struct{}{}
		}
		if h.gate != nil {
			<-h.gate
		}
		if h.fail.CompareAndSwap(true, false) {
			return Internal()
		}
		WriteJSON(w, http.StatusCreated, map[string]any{"n": n})
		return nil
	}
	wrapped := h.s.withIdempotency(inner)
	req := httptest.NewRequest(http.MethodPost, "/x/publish", bytes.NewBufferString(body))
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	req = req.WithContext(WithPrincipal(req.Context(), auth.Principal{UserID: userID, Roles: []string{auth.RoleSecretary}}))
	rec := httptest.NewRecorder()
	if err := wrapped(rec, req); err != nil {
		WriteError(rec, req, err)
	}
	return rec
}

func TestIdempotencyReplaysSameRequestWithoutReexecuting(t *testing.T) {
	h := newIdem(t)
	a := h.do(1, "k1", `{"v":1}`)
	b := h.do(1, "k1", `{"v":1}`)
	if a.Code != http.StatusCreated || b.Code != http.StatusCreated {
		t.Fatalf("durumlar: %d %d", a.Code, b.Code)
	}
	if h.calls.Load() != 1 {
		t.Fatalf("handler bir kez çalışmalı, %d kez çalıştı", h.calls.Load())
	}
	if a.Body.String() != b.Body.String() || b.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("tekrar yanıtı aynı olmalı: %q / %q", a.Body, b.Body)
	}
	// Anahtarsız istek her seferinde işlenir.
	h.do(1, "", `{"v":1}`)
	h.do(1, "", `{"v":1}`)
	if h.calls.Load() != 3 {
		t.Fatalf("anahtarsız istekler işlenmeli: %d", h.calls.Load())
	}
}

func TestIdempotencyKeyWithDifferentBodyIs422(t *testing.T) {
	h := newIdem(t)
	h.do(1, "k2", `{"v":1}`)
	r := h.do(1, "k2", `{"v":2}`)
	if r.Code != http.StatusUnprocessableEntity || !bytes.Contains(r.Body.Bytes(), []byte(CodeIdempotency)) {
		t.Fatalf("farklı gövde 422 olmalı: %d %s", r.Code, r.Body)
	}
	if h.calls.Load() != 1 {
		t.Fatal("farklı gövdeli tekrar handler'ı çalıştırmamalı")
	}
}

func TestIdempotencyKeysAreScopedPerUser(t *testing.T) {
	h := newIdem(t)
	h.do(1, "ortak", `{}`)
	h.do(2, "ortak", `{}`)
	if h.calls.Load() != 2 {
		t.Fatalf("farklı kullanıcıların aynı anahtarı birbirini etkilememeli: %d", h.calls.Load())
	}
}

func TestIdempotencyDoesNotCacheServerErrors(t *testing.T) {
	h := newIdem(t)
	h.fail.Store(true)
	if r := h.do(1, "k3", `{}`); r.Code != http.StatusInternalServerError {
		t.Fatalf("ilk istek 500 olmalı: %d", r.Code)
	}
	if r := h.do(1, "k3", `{}`); r.Code != http.StatusCreated {
		t.Fatalf("5xx sonrası yeniden deneme işlenmeli: %d", r.Code)
	}
	if h.calls.Load() != 2 {
		t.Fatalf("handler iki kez çalışmalı: %d", h.calls.Load())
	}
}

func TestIdempotencyConcurrentSameKeyGets409ThenReplay(t *testing.T) {
	h := newIdem(t)
	h.gate, h.entered = make(chan struct{}), make(chan struct{}, 1)
	first := make(chan *httptest.ResponseRecorder)
	go func() { first <- h.do(1, "k4", `{}`) }()
	<-h.entered // ilk istek handler'ın içinde, anahtar "in_progress"

	// İkinci istek handler'a GİRMEMELİ; girerse kapıda bekleyip testi kilitler.
	// Bu yüzden zaman aşımıyla bekliyoruz ki gerileme kilitlenme değil hata olsun.
	second := make(chan *httptest.ResponseRecorder, 1)
	go func() { second <- h.do(1, "k4", `{}`) }()
	select {
	case r := <-second:
		if r.Code != http.StatusConflict {
			t.Fatalf("eşzamanlı aynı anahtar 409 olmalı: %d %s", r.Code, r.Body)
		}
	case <-h.entered:
		close(h.gate)
		t.Fatal("ikinci istek handler'a girdi: aynı anahtar iki kez işlendi")
	case <-time.After(5 * time.Second):
		close(h.gate)
		t.Fatal("ikinci istek 5 sn içinde yanıt vermedi")
	}
	close(h.gate)
	if r := <-first; r.Code != http.StatusCreated {
		t.Fatalf("ilk istek tamamlanmalı: %d", r.Code)
	}
	h.entered = nil
	if r := h.do(1, "k4", `{}`); r.Code != http.StatusCreated || r.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("tamamlandıktan sonra tekrar yanıt dönmeli: %d", r.Code)
	}
	if h.calls.Load() != 1 {
		t.Fatalf("handler tam bir kez çalışmalı: %d", h.calls.Load())
	}
}

// 50 eşzamanlı istek, aynı anahtar: handler TAM BİR kez çalışır, hiçbir istek 5xx almaz.
func TestIdempotencyFiftyConcurrentRequestsExecuteOnce(t *testing.T) {
	h := newIdem(t)
	var wg sync.WaitGroup
	codes := make([]int, 50)
	for i := range codes {
		wg.Add(1)
		go func(i int) { defer wg.Done(); codes[i] = h.do(1, "k5", `{"x":1}`).Code }(i)
	}
	wg.Wait()
	if h.calls.Load() != 1 {
		t.Fatalf("handler %d kez çalıştı, 1 bekleniyordu", h.calls.Load())
	}
	for i, c := range codes {
		if c != http.StatusCreated && c != http.StatusConflict {
			t.Fatalf("istek %d: beklenmeyen durum %d", i, c)
		}
	}
}
