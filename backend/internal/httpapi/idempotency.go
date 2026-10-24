package httpapi

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/AllenVB/sinavplan/backend/internal/idempotency"
)

// withIdempotency, Idempotency-Key başlığını uygular. Başlık yoksa istek normal
// işlenir (destek isteğe bağlı). Ayrıntılar: internal/idempotency paket belgesi.
func (s *Server) withIdempotency(next HandlerFunc) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		key := r.Header.Get(idempotency.Header)
		if key == "" {
			return next(w, r)
		}
		if len(key) > 200 {
			return Validation("Idempotency-Key en fazla 200 karakter olabilir", nil)
		}
		if s.idem == nil {
			return Unavailable("idempotency deposu yapılandırılmadı")
		}
		p := mustPrincipal(r)
		body, err := idempotency.ReadBody(r)
		if err != nil {
			return BadRequest("istek gövdesi okunamadı veya çok büyük")
		}
		methodPath := r.Method + " " + r.URL.Path
		outcome, stored, err := s.idem.Begin(r.Context(), key, p.UserID, methodPath,
			idempotency.RequestHash(methodPath, body))
		if err != nil {
			return err
		}
		switch outcome {
		case idempotency.Mismatch:
			return &APIError{Status: http.StatusUnprocessableEntity, Code: CodeIdempotency,
				Message: "bu Idempotency-Key farklı bir istek için kullanılmış"}
		case idempotency.InProgress:
			return Conflict("aynı Idempotency-Key ile bir istek hâlâ işleniyor")
		case idempotency.Replay:
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Idempotent-Replayed", "true")
			w.WriteHeader(stored.Status)
			_, _ = w.Write(stored.Body)
			return nil
		}

		rec := &idempotency.Recorder{ResponseWriter: w}
		herr := next(rec, r)
		if herr != nil {
			WriteError(rec, r, herr)
		}
		// 5xx saklanmaz: geçici sunucu hatası yeniden denemeyi engellememeli.
		if rec.Status >= 500 || rec.Status == 0 {
			if err := s.idem.Release(r.Context(), key, p.UserID); err != nil {
				s.log.Error("idempotency anahtarı bırakılamadı", slog.String("err", err.Error()))
			}
			return nil
		}
		if err := s.idem.Complete(r.Context(), key, p.UserID, rec.Status, rec.Buf.Bytes()); err != nil {
			s.log.Error("idempotency yanıtı saklanamadı",
				slog.String("err", err.Error()), slog.String("status", strconv.Itoa(rec.Status)))
		}
		return nil
	}
}
