package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AllenVB/sinavplan/backend/internal/auth"
	"github.com/go-chi/chi/v5"
)

const rbacSecret = "rbac-test-secret-0123456789-0123456789"

func rbacServer(t *testing.T) *Server {
	t.Helper()
	s := newTestServer(t, fakePinger{})
	s.issuer = auth.NewIssuer(rbacSecret, time.Minute)
	return s
}

func bearer(t *testing.T, p auth.Principal) string {
	t.Helper()
	tok, _, err := auth.NewIssuer(rbacSecret, time.Minute).Issue(p)
	if err != nil {
		t.Fatal(err)
	}
	return "Bearer " + tok
}

// Router'daki HER route, s.handle üzerinden açık bir yetki tanımıyla kaydedilmiş
// olmalı. Biri chi'nin r.Get'ini doğrudan kullanırsa route RouteInfo listesinde
// olmaz ve bu test başarısız olur.
func TestEveryRouteDeclaresAccess(t *testing.T) {
	s := rbacServer(t)
	declared := map[string]RouteInfo{}
	for _, ri := range s.Routes() {
		declared[ri.Method+" "+ri.Pattern] = ri
	}
	n := 0
	err := chi.Walk(s.router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		n++
		ri, ok := declared[method+" "+route]
		if !ok {
			t.Errorf("%s %s yetki tanımı olmadan kaydedilmiş (s.handle kullanılmamış)", method, route)
			return nil
		}
		if !ri.Access.public && len(ri.Access.roles) == 0 {
			t.Errorf("%s %s için rol listesi boş", method, route)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 || n != len(declared) {
		t.Fatalf("router'da %d route, tanımlı %d", n, len(declared))
	}
}

// Kamuya açık uçlar bilinçli ve sınırlı bir liste olmalı; yeni bir uç yanlışlıkla
// Public() ile eklenirse bu test uyarır.
func TestPublicRoutesAreAnExplicitAllowList(t *testing.T) {
	allowed := map[string]bool{"GET /health": true, "GET /_hz/1e5a": true, "GET /demo/checkin-code": true, "GET /metrics": true,
		"POST /auth/login": true, "POST /auth/refresh": true, "POST /auth/logout": true,
		"GET /docs": true, "GET /docs/assets/{file}": true, "GET /openapi.yaml": true}
	for _, ri := range rbacServer(t).Routes() {
		k := ri.Method + " " + ri.Pattern
		if ri.Access.public && !allowed[k] {
			t.Errorf("%s beklenmedik biçimde herkese açık", k)
		}
	}
}

func TestRegisteringRouteWithoutAccessPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("yetki tanımı olmayan route paniklemeliydi")
		}
	}()
	s := rbacServer(t)
	s.handle(chi.NewRouter(), http.MethodGet, "/x", Access{}, func(http.ResponseWriter, *http.Request) error { return nil })
}

func TestRolesWithEmptyListPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Roles() boş liste ile paniklemeliydi")
		}
	}()
	_ = Roles()
}

func TestAuthorizationStatusCodes(t *testing.T) {
	s := rbacServer(t)
	student := auth.Principal{UserID: 5, Roles: []string{auth.RoleStudent}, StudentID: 1}
	secretary := auth.Principal{UserID: 1, Roles: []string{auth.RoleSecretary}}

	cases := []struct {
		name, path, authz string
		want              int
	}{
		{"token yok", "/me", "", http.StatusUnauthorized},
		{"Bearer öneki yok", "/me", "Token abc", http.StatusUnauthorized},
		{"bozuk token", "/me", "Bearer bozuk.token.degeri", http.StatusUnauthorized},
		{"başka sırla imzalı", "/me", func() string {
			tok, _, _ := auth.NewIssuer(rbacSecret+"x", time.Minute).Issue(secretary)
			return "Bearer " + tok
		}(), http.StatusUnauthorized},
		{"geçerli token, her rol", "/me", bearer(t, student), http.StatusOK},
		// /students yalnız sekreter + öğrenci işleri: öğrenci 403 almalı (DB'ye ulaşmadan).
		{"yetkisiz rol", "/students", bearer(t, student), http.StatusForbidden},
		// Öğrenci başka öğrencinin programını isteyemez (sahiplik, DB'ye ulaşmadan).
		{"başkasının verisi", "/students/2/exams", bearer(t, student), http.StatusForbidden},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, c.path, nil)
		if c.authz != "" {
			req.Header.Set("Authorization", c.authz)
		}
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s: durum %d, beklenen %d (%s)", c.name, rec.Code, c.want, rec.Body.String())
		}
		if c.want == http.StatusUnauthorized && !strings.HasPrefix(rec.Header().Get("WWW-Authenticate"), "Bearer") {
			t.Errorf("%s: 401 yanıtında WWW-Authenticate başlığı yok", c.name)
		}
	}
}

func TestRequireStudentAccess(t *testing.T) {
	st := auth.Principal{UserID: 5, Roles: []string{auth.RoleStudent}, StudentID: 3}
	if requireStudentAccess(st, 3, auth.RoleSecretary) != nil {
		t.Error("öğrenci kendi kaydına erişebilmeli")
	}
	if requireStudentAccess(st, 4, auth.RoleSecretary) == nil {
		t.Error("öğrenci başkasının kaydına erişememeli")
	}
	unlinked := auth.Principal{UserID: 9, Roles: []string{auth.RoleStudent}}
	if requireStudentAccess(unlinked, 0, auth.RoleSecretary) == nil {
		t.Error("öğrenci kaydına bağlı olmayan hesap 0 kimliğiyle erişememeli")
	}
	sec := auth.Principal{UserID: 1, Roles: []string{auth.RoleSecretary}}
	if requireStudentAccess(sec, 4, auth.RoleSecretary) != nil {
		t.Error("ayrıcalıklı rol herkese erişebilmeli")
	}
	lec := auth.Principal{UserID: 2, Roles: []string{auth.RoleLecturer}}
	if requireStudentAccess(lec, 4, auth.RoleSecretary) == nil {
		t.Error("listede olmayan rol erişememeli")
	}
}
