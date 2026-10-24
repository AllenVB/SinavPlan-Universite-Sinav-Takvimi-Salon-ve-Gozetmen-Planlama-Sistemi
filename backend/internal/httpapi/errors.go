// Package httpapi, HTTP katmanını barındırır: router, handler'lar, middleware
// ve tüm uçlarda ortak olan hata biçimi.
package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// Hata kodları. Gövdedeki "code" alanı makine tarafından okunur; istemci
// metne değil bu koda göre dallanır.
const (
	CodeUnauthorized   = "unauthorized"
	CodeForbidden      = "forbidden"
	CodeNotFound       = "not_found"
	CodeValidation     = "validation_failed"
	CodeConflict       = "conflict"
	CodeIdempotency    = "idempotency_key_reused"
	CodeInternal       = "internal_error"
	CodeUnavailable    = "service_unavailable"
	CodeBadRequest     = "bad_request"
	CodeTooManyRequest = "too_many_requests"
	CodeTimeout        = "request_timeout"
)

// ErrorBody, tüm hata yanıtlarının zarfı: {"error":{"code":"…","message":"…"}}
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail, hatanın kodu, insan okuyabilir mesajı ve isteğe bağlı alan
// bazlı ayrıntıları.
type ErrorDetail struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

// APIError, handler'ların döndürebileceği tipli hata. HTTP durumunu ve gövde
// içeriğini birlikte taşır.
type APIError struct {
	Status  int
	Code    string
	Message string
	Fields  map[string]string
}

func (e *APIError) Error() string { return e.Code + ": " + e.Message }

// Hazır kurucular — çağrı yerlerinde durum kodu ile kodun eşleşmesini
// garanti eder, böylece 422 yerine yanlışlıkla 400 dönülmesi önlenir.

// Unauthorized: kimlik yok veya geçersiz.
func Unauthorized(msg string) *APIError {
	return &APIError{http.StatusUnauthorized, CodeUnauthorized, msg, nil}
}

// Forbidden: kimlik var ama yetki yok.
func Forbidden(msg string) *APIError {
	return &APIError{http.StatusForbidden, CodeForbidden, msg, nil}
}

// NotFound: kayıt yok (ya da sahiplik kontrolü yokmuş gibi gösteriyor).
func NotFound(msg string) *APIError {
	return &APIError{http.StatusNotFound, CodeNotFound, msg, nil}
}

// Validation: girdi doğrulaması başarısız -> 422.
func Validation(msg string, fields map[string]string) *APIError {
	return &APIError{http.StatusUnprocessableEntity, CodeValidation, msg, fields}
}

// Conflict: eşzamanlılık ya da durum çakışması -> 409.
func Conflict(msg string) *APIError {
	return &APIError{http.StatusConflict, CodeConflict, msg, nil}
}

// BadRequest: gövde çözümlenemedi gibi biçimsel sorunlar -> 400.
func BadRequest(msg string) *APIError {
	return &APIError{http.StatusBadRequest, CodeBadRequest, msg, nil}
}

// Internal: beklenmeyen sunucu hatası -> 500. Mesaj sabittir; ayrıntı log'a
// gider, istemciye iç detay sızdırmayız.
func Internal() *APIError {
	return &APIError{http.StatusInternalServerError, CodeInternal, "beklenmeyen bir sunucu hatası oluştu", nil}
}

// Unavailable: bir bağımlılık (örn. veritabanı) erişilemez -> 503.
func Unavailable(msg string) *APIError {
	return &APIError{http.StatusServiceUnavailable, CodeUnavailable, msg, nil}
}

// WriteJSON, verilen durumu ve gövdeyi JSON olarak yazar.
func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// Başlıklar gönderildiği için durumu değiştiremeyiz; yalnızca kaydet.
		slog.Error("yanıt gövdesi yazılamadı", slog.String("err", err.Error()))
	}
}

// WriteError, bir hatayı ortak biçimde yazar. APIError değilse 500'e düşürür
// ve iç ayrıntıyı istemciye değil log'a verir.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	apiErr, ok := err.(*APIError)
	if !ok {
		slog.ErrorContext(r.Context(), "işlenmemiş handler hatası",
			slog.String("err", err.Error()),
			slog.String("path", r.URL.Path),
		)
		apiErr = Internal()
	}
	if apiErr.Status >= 500 {
		slog.ErrorContext(r.Context(), "sunucu hatası",
			slog.String("code", apiErr.Code),
			slog.String("path", r.URL.Path),
		)
	}
	WriteJSON(w, apiErr.Status, ErrorBody{Error: ErrorDetail{
		Code:    apiErr.Code,
		Message: apiErr.Message,
		Fields:  apiErr.Fields,
	}})
}
