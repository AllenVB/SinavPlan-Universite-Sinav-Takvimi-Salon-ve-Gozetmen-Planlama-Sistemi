package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/AllenVB/sinavplan/backend/internal/auth"
	"github.com/AllenVB/sinavplan/backend/internal/idempotency"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Server, HTTP katmanının bağımlılıklarını taşır ve router'ı kurar.
type Server struct {
	db             Pinger
	log            *slog.Logger
	version        string
	requestTimeout time.Duration
	metrics        *Metrics
	router         chi.Router

	pool       *pgxpool.Pool
	auth       *auth.Service
	issuer     *auth.Issuer
	idem       *idempotency.Store
	routeInfos []RouteInfo
}

// Options, Server kurulumunda gereken dışsal bağımlılıklar.
type Options struct {
	DB             Pinger
	Logger         *slog.Logger
	Version        string
	RequestTimeout time.Duration
	Registry       *prometheus.Registry

	// Veritabanı gerektiren uçlar için. Birim testlerinde nil olabilir.
	Pool *pgxpool.Pool
	Auth *auth.Service
	// Issuer verilmezse Auth'unki kullanılır; testler yalnızca Issuer verebilir.
	Issuer *auth.Issuer
}

// NewServer, verilen bağımlılıklarla router'ı kurar.
func NewServer(opts Options) *Server {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.RequestTimeout <= 0 {
		opts.RequestTimeout = 15 * time.Second
	}
	if opts.Registry == nil {
		opts.Registry = prometheus.NewRegistry()
	}
	if opts.Version == "" {
		opts.Version = "dev"
	}

	s := &Server{
		db:             opts.DB,
		log:            opts.Logger,
		version:        opts.Version,
		requestTimeout: opts.RequestTimeout,
		metrics:        NewMetrics(opts.Registry),
		pool:           opts.Pool,
		auth:           opts.Auth,
		issuer:         opts.Issuer,
	}
	if s.issuer == nil && s.auth != nil {
		s.issuer = s.auth.Issuer()
	}
	if opts.Pool != nil {
		s.idem = idempotency.NewStore(opts.Pool)
	}
	s.routes(opts.Registry)
	return s
}

// ServeHTTP, Server'ı http.Handler yapar.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

// routes, tüm uçları kaydeder. Her route s.handle üzerinden ve açık bir yetki
// tanımıyla (Public veya Roles) kaydedilir; bkz. rbac.go.
func (s *Server) routes(reg *prometheus.Registry) {
	r := chi.NewRouter()

	r.Use(RequestID)
	r.Use(s.recoverPanic)
	r.Use(s.observe)
	// Zaman aşımı global middleware DEĞİL, route başına (s.handle) uygulanır: chi
	// yönlendirmeyi middleware'lerden sonra yapıp RouteContext'i değiştirir; zaman
	// aşımı goroutine'i yönlendirmeden önce başlarsa observe ile yarışır.

	metrics := promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
	s.handle(r, http.MethodGet, "/health", Public(), func(w http.ResponseWriter, r *http.Request) error {
		s.healthHandler(w, r)
		return nil
	})
	s.handle(r, http.MethodGet, "/metrics", Public(), func(w http.ResponseWriter, r *http.Request) error {
		metrics.ServeHTTP(w, r)
		return nil
	})

	// Kimlik
	s.handle(r, http.MethodPost, "/auth/login", Public(), s.login)
	s.handle(r, http.MethodPost, "/auth/refresh", Public(), s.refresh)
	s.handle(r, http.MethodPost, "/auth/logout", Public(), s.logout)
	s.handle(r, http.MethodGet, "/me", AllRoles(), s.me)

	// Öğrenciler
	s.handle(r, http.MethodGet, "/students", Roles(auth.RoleSecretary, auth.RoleStudentAffairs), s.listStudents)
	s.handle(r, http.MethodGet, "/students/{id}/exams",
		Roles(auth.RoleSecretary, auth.RoleStudentAffairs, auth.RoleStudent), s.studentExams)
	s.handle(r, http.MethodGet, "/me/exams", Roles(auth.RoleStudent), s.myExams)

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, r, NotFound("istenen uç bulunamadı"))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, r, &APIError{
			Status:  http.StatusMethodNotAllowed,
			Code:    "method_not_allowed",
			Message: "bu uç bu HTTP metodunu desteklemiyor",
		})
	})

	s.router = r
}
