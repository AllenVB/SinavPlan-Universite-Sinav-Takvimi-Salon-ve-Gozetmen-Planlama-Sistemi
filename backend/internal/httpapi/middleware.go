package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type ctxKey string

const ctxKeyRequestID ctxKey = "request_id"

// RequestID, her isteğe bir kimlik verir ve yanıt başlığına koyar. İstemci
// kendi kimliğini gönderdiyse onu korur; böylece log'lar uçtan uca izlenebilir.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeyRequestID, id)))
	})
}

// RequestIDFrom, context'teki istek kimliğini döner.
func RequestIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyRequestID).(string); ok {
		return v
	}
	return ""
}

// statusRecorder, yazılan HTTP durumunu yakalar. Metrikler ve log için
// gerekli; http.ResponseWriter durumu geri okumaya izin vermiyor.
type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusRecorder) WriteHeader(code int) {
	if !w.wrote {
		w.status = code
		w.wrote = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusRecorder) Write(b []byte) (int, error) {
	if !w.wrote {
		w.status = http.StatusOK
		w.wrote = true
	}
	return w.ResponseWriter.Write(b)
}

// observe, isteği loglar ve metriklere işler.
//
// route etiketi olarak chi'nin kalıbını (örn. /students/{id}) kullanıyoruz,
// ham yolu değil: aksi halde her öğrenci kimliği ayrı bir zaman serisi
// yaratıp Prometheus'u şişirirdi (kardinalite patlaması).
func (s *Server) observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		s.metrics.InFlight.Inc()
		defer s.metrics.InFlight.Dec()

		next.ServeHTTP(rec, r)

		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			route = "unmatched"
		}
		elapsed := time.Since(start)

		s.metrics.RequestsTotal.WithLabelValues(r.Method, route, statusLabel(rec.status)).Inc()
		s.metrics.RequestDuration.WithLabelValues(r.Method, route).Observe(elapsed.Seconds())

		// /health ve /metrics her birkaç saniyede yoklanıyor; bunları info
		// seviyesinde loglamak çıktıyı işe yaramaz biçimde doldurur.
		level := slog.LevelInfo
		if route == "/health" || route == "/metrics" {
			level = slog.LevelDebug
		}
		if rec.status >= 500 {
			level = slog.LevelError
		}

		s.log.Log(r.Context(), level, "http isteği",
			slog.String("method", r.Method),
			slog.String("route", route),
			slog.Int("status", rec.status),
			slog.Int64("duration_ms", elapsed.Milliseconds()),
			slog.String("request_id", RequestIDFrom(r.Context())),
		)
	})
}

// timeout, her isteğe context zaman aşımı uygular. Süre aşılırsa ortak hata
// biçiminde 504 döner.
func (s *Server) timeout(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), s.requestTimeout)
		defer cancel()

		done := make(chan struct{})
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		go func() {
			defer close(done)
			next.ServeHTTP(rec, r.WithContext(ctx))
		}()

		select {
		case <-done:
			return
		case <-ctx.Done():
			// Handler hâlâ çalışıyor olabilir; yanıtı yalnızca henüz bir şey
			// yazılmadıysa biz üretiriz, aksi halde gövdeyi bozarız.
			if !rec.wrote {
				WriteError(w, r, &APIError{
					Status:  http.StatusGatewayTimeout,
					Code:    CodeTimeout,
					Message: "istek zaman aşımına uğradı",
				})
			}
			<-done
		}
	})
}

// recoverPanic, bir handler panikleyince süreci düşürmek yerine 500 döner.
func (s *Server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.ErrorContext(r.Context(), "handler panikledi",
					slog.Any("panic", rec),
					slog.String("path", r.URL.Path),
					slog.String("request_id", RequestIDFrom(r.Context())),
				)
				WriteError(w, r, Internal())
			}
		}()
		next.ServeHTTP(w, r)
	})
}
