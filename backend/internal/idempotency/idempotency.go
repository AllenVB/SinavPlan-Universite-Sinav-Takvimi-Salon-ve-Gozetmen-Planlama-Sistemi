// Package idempotency, durum değiştiren kritik POST isteklerinde Idempotency-Key
// başlığını uygular.
//
// Davranış (anahtar kullanıcı başına kapsamlıdır):
//   - ilk istek: işlenir, yanıt (durum + gövde) saklanır;
//   - aynı anahtar + aynı yöntem/yol + aynı gövde: saklanan yanıt aynen döner,
//     handler TEKRAR ÇALIŞMAZ (Idempotent-Replayed: true);
//   - aynı anahtar + farklı gövde veya farklı uç: 422;
//   - aynı anahtar ilk istek bitmeden tekrar gelirse: 409 (bekletmek yerine; PROJECT_PLAN V-5).
//
// Eşzamanlılığı birincil anahtar çözer: (key, user_id) üzerine yalnızca bir INSERT
// başarılı olur, kazanan isteği işler.
//
// 5xx yanıtlar saklanmaz ve anahtar serbest bırakılır: geçici bir sunucu hatası
// istemcinin yeniden denemesini kalıcı olarak engellememeli.
package idempotency

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Header, istemcinin gönderdiği başlık adı.
const Header = "Idempotency-Key"

// MaxBody, özetlenmek üzere belleğe alınan gövde sınırı.
const MaxBody = 1 << 20

// Outcome, Begin'in sonucu.
type Outcome int

const (
	// Proceed: bu istek anahtarı aldı; işlenmeli ve sonra Complete çağrılmalı.
	Proceed Outcome = iota
	// Replay: tamamlanmış aynı istek; saklı yanıt döndürülmeli.
	Replay
	// Mismatch: anahtar başka bir gövde/uç için kullanılmış -> 422.
	Mismatch
	// InProgress: aynı anahtarlı istek hâlâ işleniyor -> 409.
	InProgress
)

// Stored, saklanmış yanıt.
type Stored struct {
	Status int
	Body   []byte
}

// Store, idempotency_keys tablosu üzerinde çalışır.
type Store struct{ pool *pgxpool.Pool }

// NewStore, Store kurar.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// RequestHash, yöntem, yol ve gövdeden kanonik özet üretir.
func RequestHash(methodPath string, body []byte) []byte {
	h := sha256.New()
	h.Write([]byte(methodPath))
	h.Write([]byte{0})
	h.Write(body)
	return h.Sum(nil)
}

// Begin, anahtarı almaya çalışır.
func (s *Store) Begin(ctx context.Context, key string, userID int64, methodPath string, reqHash []byte) (Outcome, Stored, error) {
	tag, err := s.pool.Exec(ctx,
		`INSERT INTO idempotency_keys (key, user_id, method_path, request_hash)
		 VALUES ($1, $2, $3, $4) ON CONFLICT (key, user_id) DO NOTHING`,
		key, userID, methodPath, reqHash)
	if err != nil {
		return 0, Stored{}, fmt.Errorf("idempotency anahtarı yazılamadı: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return Proceed, Stored{}, nil
	}

	var (
		storedPath, state string
		storedHash        []byte
		status            *int32
		body              []byte
	)
	err = s.pool.QueryRow(ctx,
		`SELECT method_path, request_hash, state, status, body
		 FROM idempotency_keys WHERE key = $1 AND user_id = $2`, key, userID,
	).Scan(&storedPath, &storedHash, &state, &status, &body)
	if errors.Is(err, pgx.ErrNoRows) {
		// Arada ilk istek 5xx alıp anahtarı bıraktı; bir kez daha dene.
		return s.Begin(ctx, key, userID, methodPath, reqHash)
	}
	if err != nil {
		return 0, Stored{}, fmt.Errorf("idempotency anahtarı okunamadı: %w", err)
	}
	if storedPath != methodPath || !bytes.Equal(storedHash, reqHash) {
		return Mismatch, Stored{}, nil
	}
	if state != "done" || status == nil {
		return InProgress, Stored{}, nil
	}
	return Replay, Stored{Status: int(*status), Body: body}, nil
}

// Complete, yanıtı saklar.
func (s *Store) Complete(ctx context.Context, key string, userID int64, status int, body []byte) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE idempotency_keys SET state = 'done', status = $3, body = $4
		 WHERE key = $1 AND user_id = $2`, key, userID, status, body)
	return err
}

// Release, anahtarı siler (5xx sonrası yeniden denemeye izin vermek için).
func (s *Store) Release(ctx context.Context, key string, userID int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM idempotency_keys WHERE key = $1 AND user_id = $2`, key, userID)
	return err
}

// Recorder, handler'ın yazdığı yanıtı hem istemciye iletir hem de yakalar.
type Recorder struct {
	http.ResponseWriter
	Status int
	Buf    bytes.Buffer
}

// WriteHeader durumu yakalar.
func (r *Recorder) WriteHeader(code int) {
	if r.Status == 0 {
		r.Status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

// Write gövdeyi yakalar.
func (r *Recorder) Write(b []byte) (int, error) {
	if r.Status == 0 {
		r.Status = http.StatusOK
	}
	r.Buf.Write(b)
	return r.ResponseWriter.Write(b)
}

// ReadBody, gövdeyi okuyup isteğe geri koyar (handler da okuyabilsin).
func ReadBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, MaxBody+1))
	if err != nil {
		return nil, err
	}
	if len(b) > MaxBody {
		return nil, fmt.Errorf("gövde çok büyük")
	}
	r.Body = io.NopCloser(bytes.NewReader(b))
	return b, nil
}
