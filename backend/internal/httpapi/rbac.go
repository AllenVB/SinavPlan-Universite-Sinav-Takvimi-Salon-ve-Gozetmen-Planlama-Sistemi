package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/AllenVB/sinavplan/backend/internal/auth"
	"github.com/go-chi/chi/v5"
)

// Access, bir route'un kim tarafından çağrılabileceğini söyler. Sıfır değeri
// GEÇERSİZDİR: route kaydı Public() ya da boş olmayan Roles(...) ister, aksi halde
// sunucu açılışta panikler. Böylece "yetki kontrolünü unutmak" mümkün değildir;
// router'ın doğrudan kullanımı da TestEveryRouteDeclaresAccess ile yakalanır.
type Access struct {
	public bool
	roles  []string
}

// Public, kimlik gerektirmeyen uçlar içindir (giriş, sağlık, metrik).
func Public() Access { return Access{public: true} }

// Roles, yalnızca verilen rollerden birine sahip kimliklerin erişebileceği uç.
func Roles(roles ...string) Access {
	if len(roles) == 0 {
		panic("httpapi.Roles: en az bir rol gerekli")
	}
	return Access{roles: roles}
}

// AllRoles, kimliği doğrulanmış her kullanıcı.
func AllRoles() Access {
	return Roles(auth.RoleSecretary, auth.RoleLecturer, auth.RoleInvigilator,
		auth.RoleStudentAffairs, auth.RoleStudent)
}

func (a Access) String() string {
	if a.public {
		return "public"
	}
	return strings.Join(a.roles, ",")
}

// HandlerFunc, hata döndürebilen handler. Dönen hata ortak biçimde yazılır.
type HandlerFunc func(w http.ResponseWriter, r *http.Request) error

// RouteInfo, kayıtlı bir route'un yetki tanımı (test ve dokümantasyon için).
type RouteInfo struct {
	Method, Pattern string
	Access          Access
	Idempotent      bool
}

type routeOpt func(*RouteInfo)

// idempotent, uca Idempotency-Key desteği ekler.
func idempotent() routeOpt { return func(ri *RouteInfo) { ri.Idempotent = true } }

// handle, route'ları kaydetmenin TEK yoludur.
func (s *Server) handle(r chi.Router, method, pattern string, acc Access, h HandlerFunc, opts ...routeOpt) {
	if !acc.public && len(acc.roles) == 0 {
		panic(fmt.Sprintf("route %s %s için yetki tanımı yok", method, pattern))
	}
	ri := RouteInfo{Method: method, Pattern: pattern, Access: acc}
	for _, o := range opts {
		o(&ri)
	}
	s.routeInfos = append(s.routeInfos, ri)

	final := h
	if ri.Idempotent {
		final = s.withIdempotency(final)
	}
	var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := final(w, r); err != nil {
			WriteError(w, r, err)
		}
	})
	if !acc.public {
		handler = s.authorize(acc.roles, handler)
	}
	r.Method(method, pattern, s.timeout(handler))
}

// Routes, kayıtlı tüm route'ları döner.
func (s *Server) Routes() []RouteInfo { return append([]RouteInfo(nil), s.routeInfos...) }

type principalKey struct{}

// PrincipalFrom, istekteki kimliği döner (kimliksiz uçlarda ok=false).
func PrincipalFrom(ctx context.Context) (auth.Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(auth.Principal)
	return p, ok
}

// WithPrincipal, testlerde kimlik enjekte etmek için.
func WithPrincipal(ctx context.Context, p auth.Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// authorize: Bearer token yoksa veya geçersizse 401, rol uymuyorsa 403.
func (s *Server) authorize(roles []string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		token, ok := strings.CutPrefix(h, "Bearer ")
		if !ok || token == "" || s.issuer == nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="sinavplan"`)
			WriteError(w, r, Unauthorized("kimlik doğrulaması gerekli"))
			return
		}
		p, err := s.issuer.Parse(token)
		if err != nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="sinavplan", error="invalid_token"`)
			WriteError(w, r, Unauthorized("geçersiz veya süresi dolmuş token"))
			return
		}
		if !p.HasAnyRole(roles...) {
			WriteError(w, r, Forbidden("bu işlem için yetkiniz yok"))
			return
		}
		next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
	})
}

func mustPrincipal(r *http.Request) auth.Principal {
	p, _ := PrincipalFrom(r.Context())
	return p
}

// requireStudentAccess: öğrenci yalnızca kendi kaydına erişir; ayrıcalıklı roller
// herkesinkine. Kontrol sunucuda, token'daki öğrenci kimliğiyle yapılır.
func requireStudentAccess(p auth.Principal, studentID int64, privileged ...string) error {
	if p.HasAnyRole(privileged...) {
		return nil
	}
	if p.HasAnyRole(auth.RoleStudent) && p.StudentID != 0 && p.StudentID == studentID {
		return nil
	}
	return Forbidden("bu öğrencinin verisine erişim yetkiniz yok")
}
