package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
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
}

// Options, Server kurulumunda gereken dışsal bağımlılıklar.
type Options struct {
	DB             Pinger
	Logger         *slog.Logger
	Version        string
	RequestTimeout time.Duration
	Registry       *prometheus.Registry
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
	}
	s.routes(opts.Registry)
	return s
}

// ServeHTTP, Server'ı http.Handler yapar.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

// routes, tüm uçları kaydeder.
//
// Not: durum değiştiren ve rol gerektiren uçlar Faz 3'te RBAC sarmalayıcısı
// üzerinden eklenecek. Aşağıdaki üç uç bilinçli olarak herkese açıktır:
// /health ve /metrics altyapı tarafından yoklanır, /docs ise dokümantasyondur.
func (s *Server) routes(reg *prometheus.Registry) {
	r := chi.NewRouter()

	r.Use(RequestID)
	r.Use(s.recoverPanic)
	r.Use(s.observe)
	r.Use(s.timeout)

	r.Get("/health", s.healthHandler)
	r.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))

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
