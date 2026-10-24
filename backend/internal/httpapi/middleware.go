package httpapi

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"sync"
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
//
// Handler ayrı bir goroutine'de çalışır ve yanıtı doğrudan istemciye değil bir
// arabelleğe (timeoutWriter) yazar. Bu iki hatayı önler:
//  1. Zaman aşımında 504 yazılırken handler'ın aynı ResponseWriter'a yazması
//     (veri yarışı ve bozuk gövde). Zaman aşımından sonraki yazmalar atılır.
//  2. Goroutine içindeki paniğin, dış goroutine'deki recoverPanic'i atlayıp süreci
//     düşürmesi. Panik yakalanır ve dış goroutine'de yeniden fırlatılır.
func (s *Server) timeout(next http.Handler) http.Handler { return s.timeoutFor(s.requestTimeout, next) }

// timeoutFor, bu uç için özel süre sınırı uygular (uzun hesap yapan uçlar için).
func (s *Server) timeoutFor(d time.Duration, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), d)
		defer cancel()

		tw := &timeoutWriter{header: make(http.Header)}
		done := make(chan struct{})
		var panicVal any

		go func() {
			defer func() {
				if p := recover(); p != nil {
					panicVal = p
				}
				close(done)
			}()
			next.ServeHTTP(tw, r.WithContext(ctx))
		}()

		select {
		case <-done:
			if panicVal != nil {
				panic(panicVal) // recoverPanic dış goroutine'de yakalar
			}
			tw.flushTo(w)
		case <-ctx.Done():
			tw.markTimedOut()
			WriteError(w, r, &APIError{
				Status:  http.StatusGatewayTimeout,
				Code:    CodeTimeout,
				Message: "istek zaman aşımına uğradı",
			})
		}
	})
}

// timeoutWriter, handler yanıtını arabelleğe alır.
type timeoutWriter struct {
	mu       sync.Mutex
	header   http.Header
	buf      bytes.Buffer
	status   int
	timedOut bool
}

func (tw *timeoutWriter) Header() http.Header { return tw.header }

func (tw *timeoutWriter) WriteHeader(code int) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.status == 0 && !tw.timedOut {
		tw.status = code
	}
}

func (tw *timeoutWriter) Write(b []byte) (int, error) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut {
		return 0, http.ErrHandlerTimeout
	}
	if tw.status == 0 {
		tw.status = http.StatusOK
	}
	return tw.buf.Write(b)
}

func (tw *timeoutWriter) markTimedOut() {
	tw.mu.Lock()
	tw.timedOut = true
	tw.mu.Unlock()
}

func (tw *timeoutWriter) flushTo(w http.ResponseWriter) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	dst := w.Header()
	for k, v := range tw.header {
		dst[k] = v
	}
	if tw.status == 0 {
		tw.status = http.StatusOK
	}
	w.WriteHeader(tw.status)
	_, _ = w.Write(tw.buf.Bytes())
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
