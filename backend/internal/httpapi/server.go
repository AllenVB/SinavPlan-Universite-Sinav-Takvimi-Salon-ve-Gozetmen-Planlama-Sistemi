package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/AllenVB/sinavplan/backend/internal/auth"
	"github.com/AllenVB/sinavplan/backend/internal/docs"
	"github.com/AllenVB/sinavplan/backend/internal/idempotency"
	"github.com/AllenVB/sinavplan/backend/internal/jobs"
	"github.com/AllenVB/sinavplan/backend/internal/store"
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

	pool          *pgxpool.Pool
	auth          *auth.Service
	issuer        *auth.Issuer
	idem          *idempotency.Store
	qr            *store.QR
	qrTTL         time.Duration
	runner        *jobs.Runner
	seatingStrict bool
	routeInfos    []RouteInfo
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
	// QR, sınav kartı token'larını üretir/doğrular (QR_HMAC_SECRET).
	QR *store.QR
	// QRTTL, kart jetonunun ömrü (varsayılan 2 dk).
	QRTTL time.Duration
	// Runner, asenkron takvim işlerini çalıştırır.
	Runner *jobs.Runner
	// SeatingStrict, T-3'te ön/arka komşuluğu da uygular (SEATING_STRICT_MODE).
	SeatingStrict bool
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
		qr:             opts.QR,
		qrTTL:          opts.QRTTL,
		runner:         opts.Runner,
		seatingStrict:  opts.SeatingStrict,
	}
	if s.qrTTL <= 0 {
		s.qrTTL = 2 * time.Minute
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

	// Dokümantasyon (kamuya açık): Swagger UI ve OpenAPI belgesi gömülü.
	s.handle(r, http.MethodGet, "/docs", Public(), func(w http.ResponseWriter, r *http.Request) error {
		docs.Index(w, r)
		return nil
	})
	s.handle(r, http.MethodGet, "/docs/assets/{file}", Public(), func(w http.ResponseWriter, r *http.Request) error {
		if !docs.Asset(w, r, chiURLParam(r, "file")) {
			return NotFound("dosya bulunamadı")
		}
		return nil
	})
	s.handle(r, http.MethodGet, "/openapi.yaml", Public(), func(w http.ResponseWriter, r *http.Request) error {
		docs.Spec(w, r)
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
	s.handle(r, http.MethodGet, "/students/{id}/overview",
		Roles(auth.RoleSecretary, auth.RoleStudentAffairs, auth.RoleStudent), s.studentOverview)
	s.handle(r, http.MethodGet, "/search", Roles(auth.RoleSecretary, auth.RoleStudentAffairs), s.search)
	s.handle(r, http.MethodGet, "/me/exams", Roles(auth.RoleStudent), s.myExams)

	// Yoklama, sınav kartı, tahliye (F-7, T-5)
	s.handle(r, http.MethodPost, "/me/exams/{examId}/card", Roles(auth.RoleStudent), s.examCard)
	s.handle(r, http.MethodPost, "/attendance/sync", Roles(auth.RoleInvigilator, auth.RoleSecretary), s.attendanceSync, idempotent())
	s.handle(r, http.MethodPost, "/checkin-sessions", Roles(auth.RoleInvigilator), s.startCheckin)
	s.handle(r, http.MethodGet, "/checkin-sessions/{id}", Roles(auth.RoleInvigilator), s.checkinState)
	s.handle(r, http.MethodPost, "/checkin-sessions/{id}/code", Roles(auth.RoleInvigilator), s.rotateCheckinCode)
	s.handle(r, http.MethodPost, "/checkin-sessions/{id}/close", Roles(auth.RoleInvigilator), s.closeCheckin)
	s.handle(r, http.MethodPost, "/me/checkin", Roles(auth.RoleStudent), s.selfCheckIn)
	s.handle(r, http.MethodPost, "/attendance/qr", Roles(auth.RoleInvigilator), s.attendanceQR, idempotent())
	s.handle(r, http.MethodGet, "/exams/{id}/attendance",
		Roles(auth.RoleInvigilator, auth.RoleSecretary, auth.RoleStudentAffairs, auth.RoleLecturer), s.examAttendance)
	s.handle(r, http.MethodGet, "/attendance/conflicts",
		Roles(auth.RoleInvigilator, auth.RoleSecretary, auth.RoleStudentAffairs), s.attendanceConflicts)
	s.handle(r, http.MethodPost, "/schedules/{id}/room-change",
		Roles(auth.RoleSecretary, auth.RoleInvigilator), s.roomChange, idempotent())

	// Dönemler, içe aktarma, salonlar, takvim, planlar, gözetmenler, mazeret (F-1..F-6)
	s.handle(r, http.MethodGet, "/periods", Roles(auth.RoleSecretary, auth.RoleLecturer, auth.RoleInvigilator, auth.RoleStudentAffairs), s.listPeriods)
	s.handle(r, http.MethodPost, "/imports/{kind}", Roles(auth.RoleSecretary), s.importCSV)
	s.handle(r, http.MethodGet, "/rooms", Roles(auth.RoleSecretary, auth.RoleLecturer, auth.RoleInvigilator, auth.RoleStudentAffairs), s.listRooms)
	s.handle(r, http.MethodGet, "/rooms/{id}", Roles(auth.RoleSecretary, auth.RoleLecturer, auth.RoleInvigilator, auth.RoleStudentAffairs), s.getRoom)
	s.handle(r, http.MethodPost, "/rooms", Roles(auth.RoleSecretary), s.createRoom)
	s.handle(r, http.MethodPut, "/rooms/{id}", Roles(auth.RoleSecretary), s.updateRoom)
	s.handle(r, http.MethodDelete, "/rooms/{id}", Roles(auth.RoleSecretary), s.deleteRoom)

	// Fark yaratan özellikler (Faz 11). Statik yollar ({id} yollarından önce kaydedilir).
	s.handle(r, http.MethodPost, "/schedules/simulate", Roles(auth.RoleSecretary), s.simulate, slow(45*time.Second))
	s.handle(r, http.MethodGet, "/schedules/compare", Roles(auth.RoleSecretary, auth.RoleStudentAffairs), s.compareSchedules)
	s.handle(r, http.MethodGet, "/activity", Roles(auth.RoleSecretary, auth.RoleStudentAffairs), s.activity)
	s.handle(r, http.MethodGet, "/faculties", AllRoles(), s.faculties)
	s.handle(r, http.MethodGet, "/schedules/{id}/live", Roles(auth.RoleSecretary, auth.RoleStudentAffairs), s.live)
	s.handle(r, http.MethodGet, "/schedules/{id}/live/events", Roles(auth.RoleSecretary, auth.RoleStudentAffairs), s.liveEvents)
	s.handle(r, http.MethodGet, "/me/exams/{examId}/seatmap", Roles(auth.RoleStudent), s.mySeatMap)
	s.handle(r, http.MethodGet, "/periods/{id}/slots", Roles(auth.RoleSecretary, auth.RoleLecturer, auth.RoleInvigilator, auth.RoleStudentAffairs), s.periodSlots)
	s.handle(r, http.MethodGet, "/schedules/{id}/export", Roles(auth.RoleSecretary, auth.RoleLecturer, auth.RoleInvigilator, auth.RoleStudentAffairs), s.exportSchedule)
	s.handle(r, http.MethodGet, "/schedules/{id}/calendar.ics", Roles(auth.RoleSecretary, auth.RoleLecturer, auth.RoleInvigilator, auth.RoleStudentAffairs), s.scheduleICS)
	s.handle(r, http.MethodPost, "/schedules/{id}/exams/{examId}/move", Roles(auth.RoleSecretary), s.moveExam, slow(60*time.Second))
	s.handle(r, http.MethodGet, "/me/exams.ics", Roles(auth.RoleStudent), s.myExamsICS)
	s.handle(r, http.MethodGet, "/me/duties.ics", Roles(auth.RoleInvigilator), s.myDutiesICS)
	s.handle(r, http.MethodPost, "/schedules", Roles(auth.RoleSecretary), s.createSchedule)
	s.handle(r, http.MethodGet, "/schedule-jobs/{id}", Roles(auth.RoleSecretary), s.getJob)
	s.handle(r, http.MethodGet, "/schedules", Roles(auth.RoleSecretary, auth.RoleLecturer, auth.RoleInvigilator, auth.RoleStudentAffairs), s.listSchedules)
	s.handle(r, http.MethodGet, "/schedules/{id}", Roles(auth.RoleSecretary, auth.RoleLecturer, auth.RoleInvigilator, auth.RoleStudentAffairs), s.getSchedule)
	s.handle(r, http.MethodPost, "/schedules/{id}/publish", Roles(auth.RoleSecretary), s.publishSchedule, idempotent())
	s.handle(r, http.MethodGet, "/schedules/{id}/exams", Roles(auth.RoleSecretary, auth.RoleLecturer, auth.RoleInvigilator, auth.RoleStudentAffairs), s.scheduleExams)
	s.handle(r, http.MethodGet, "/schedules/{id}/rooms", Roles(auth.RoleSecretary, auth.RoleLecturer, auth.RoleInvigilator, auth.RoleStudentAffairs), s.slotRooms)
	s.handle(r, http.MethodGet, "/schedules/{id}/seating", Roles(auth.RoleSecretary, auth.RoleLecturer, auth.RoleInvigilator, auth.RoleStudentAffairs), s.seating)
	s.handle(r, http.MethodGet, "/schedules/{id}/conflicts", Roles(auth.RoleSecretary, auth.RoleStudentAffairs), s.scheduleConflicts)
	s.handle(r, http.MethodGet, "/schedules/{id}/load", Roles(auth.RoleSecretary, auth.RoleStudentAffairs, auth.RoleLecturer), s.scheduleLoad)
	s.handle(r, http.MethodGet, "/schedules/{id}/exams/{examId}/why", Roles(auth.RoleSecretary, auth.RoleStudentAffairs, auth.RoleLecturer), s.examWhy, slow(30*time.Second))
	s.handle(r, http.MethodGet, "/schedules/{id}/proofs", Roles(auth.RoleSecretary, auth.RoleStudentAffairs), s.proofs, slow(60*time.Second))
	s.handle(r, http.MethodGet, "/schedules/{id}/fix-suggestions", Roles(auth.RoleSecretary, auth.RoleStudentAffairs), s.fixSuggestions, slow(60*time.Second))
	s.handle(r, http.MethodGet, "/schedules/{id}/invigilators", Roles(auth.RoleSecretary, auth.RoleLecturer, auth.RoleInvigilator, auth.RoleStudentAffairs), s.scheduleInvigilators)
	s.handle(r, http.MethodPost, "/schedules/{id}/invigilators/{userId}/remove", Roles(auth.RoleSecretary), s.removeInvigilator, idempotent())
	s.handle(r, http.MethodGet, "/me/duties", Roles(auth.RoleInvigilator), s.myDuties)

	s.handle(r, http.MethodPost, "/excuses", Roles(auth.RoleStudent, auth.RoleInvigilator), s.createExcuse)
	s.handle(r, http.MethodGet, "/excuses", Roles(auth.RoleSecretary, auth.RoleStudentAffairs), s.listExcuses)
	s.handle(r, http.MethodGet, "/me/excuses", Roles(auth.RoleStudent, auth.RoleInvigilator), s.myExcuses)
	s.handle(r, http.MethodPost, "/excuses/{id}/decision", Roles(auth.RoleSecretary, auth.RoleStudentAffairs), s.decideExcuse, idempotent())

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
