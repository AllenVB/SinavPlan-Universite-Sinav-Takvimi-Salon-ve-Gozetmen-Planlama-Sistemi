package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/AllenVB/sinavplan/backend/internal/store/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrBadCredentials, e-posta veya parola yanlışsa döner. Hangisinin yanlış olduğu
// bilerek söylenmez (kullanıcı adı numaralandırmasını önler).
var ErrBadCredentials = errors.New("e-posta veya parola hatalı")

// Tokens, giriş ve yenileme yanıtıdır.
type Tokens struct {
	AccessToken      string    `json:"access_token"`
	AccessExpiresAt  time.Time `json:"access_expires_at"`
	RefreshToken     string    `json:"refresh_token"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
	Principal        Principal `json:"-"`
}

// Service, giriş, refresh token rotasyonu ve çıkışı yönetir.
type Service struct {
	pool       *pgxpool.Pool
	issuer     *Issuer
	refreshTTL time.Duration
	dummyHash  string
}

// NewService, Service kurar. dummyHash, olmayan kullanıcı için de argon2 hesabı
// yapıp yanıt süresini eşitlemek içindir (zamanlama ile kullanıcı tespiti olmasın).
func NewService(pool *pgxpool.Pool, issuer *Issuer, refreshTTL time.Duration) (*Service, error) {
	h, err := HashPassword("zamanlama-eşitleme-için-sabit-olmayan-parola")
	if err != nil {
		return nil, err
	}
	return &Service{pool: pool, issuer: issuer, refreshTTL: refreshTTL, dummyHash: h}, nil
}

// Issuer, access token doğrulayıcısını döner (middleware için).
func (s *Service) Issuer() *Issuer { return s.issuer }

// hashRefresh: refresh token DB'de yalnızca SHA-256 özetiyle tutulur. Token 256 bit
// rastgele olduğu için tuzsuz hızlı özet yeterli (parola değil, tahmin edilemez).
func hashRefresh(raw string) []byte {
	h := sha256.Sum256([]byte(raw))
	return h[:]
}

func newRefreshToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Login, kimlik bilgilerini doğrular ve yeni bir token ailesi başlatır.
func (s *Service) Login(ctx context.Context, email, password string) (Tokens, error) {
	q := sqlc.New(s.pool)
	u, err := q.GetUserByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		_, _ = VerifyPassword(password, s.dummyHash)
		return Tokens{}, ErrBadCredentials
	}
	if err != nil {
		return Tokens{}, fmt.Errorf("kullanıcı okunamadı: %w", err)
	}
	ok, err := VerifyPassword(password, u.PasswordHash)
	if err != nil || !ok || !u.Active {
		return Tokens{}, ErrBadCredentials
	}
	p, err := s.loadPrincipal(ctx, q, u.ID)
	if err != nil {
		return Tokens{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Tokens{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	t, err := s.issue(ctx, sqlc.New(tx), p, uuid.New())
	if err != nil {
		return Tokens{}, err
	}
	return t, tx.Commit(ctx)
}

// Refresh, refresh token'ı döndürür (rotasyon): eskisi iptal edilir, aynı aileden
// yenisi verilir. İPTAL EDİLMİŞ bir token tekrar gelirse token çalınmış kabul edilir
// ve TÜM aile iptal edilir; meşru sahibi de yeniden giriş yapmak zorunda kalır.
func (s *Service) Refresh(ctx context.Context, raw string) (Tokens, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Tokens{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := sqlc.New(tx)

	// FOR UPDATE: aynı token'la eşzamanlı iki yenileme isteğinden yalnızca biri
	// yeni token alır; ikincisi kilidi bekler, sonra iptal edilmiş görür.
	rt, err := q.GetRefreshTokenForUpdate(ctx, hashRefresh(raw))
	if errors.Is(err, pgx.ErrNoRows) {
		return Tokens{}, ErrInvalidToken
	}
	if err != nil {
		return Tokens{}, err
	}
	if rt.RevokedAt.Valid {
		if err := q.RevokeRefreshTokenFamily(ctx, rt.FamilyID); err != nil {
			return Tokens{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Tokens{}, err
		}
		return Tokens{}, ErrInvalidToken
	}
	if !rt.ExpiresAt.Time.After(time.Now()) {
		return Tokens{}, ErrInvalidToken
	}

	p, err := s.loadPrincipal(ctx, q, rt.UserID)
	if err != nil {
		return Tokens{}, err
	}
	t, newID, err := s.issueWithID(ctx, q, p, uuid.UUID(rt.FamilyID.Bytes))
	if err != nil {
		return Tokens{}, err
	}
	if err := q.RevokeRefreshToken(ctx, sqlc.RevokeRefreshTokenParams{ID: rt.ID, ReplacedBy: pgtype.Int8{Int64: newID, Valid: true}}); err != nil {
		return Tokens{}, err
	}
	return t, tx.Commit(ctx)
}

// Logout, refresh token'ın ailesini iptal eder. Bilinmeyen token sessizce kabul
// edilir (çıkış her zaman başarılı görünür).
func (s *Service) Logout(ctx context.Context, raw string) error {
	q := sqlc.New(s.pool)
	rt, err := q.GetRefreshTokenForUpdate(ctx, hashRefresh(raw))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return q.RevokeRefreshTokenFamily(ctx, rt.FamilyID)
}

func (s *Service) issue(ctx context.Context, q *sqlc.Queries, p Principal, family uuid.UUID) (Tokens, error) {
	t, _, err := s.issueWithID(ctx, q, p, family)
	return t, err
}

func (s *Service) issueWithID(ctx context.Context, q *sqlc.Queries, p Principal, family uuid.UUID) (Tokens, int64, error) {
	access, accessExp, err := s.issuer.Issue(p)
	if err != nil {
		return Tokens{}, 0, err
	}
	raw, err := newRefreshToken()
	if err != nil {
		return Tokens{}, 0, err
	}
	exp := time.Now().Add(s.refreshTTL)
	id, err := q.CreateRefreshToken(ctx, sqlc.CreateRefreshTokenParams{
		UserID: p.UserID, TokenHash: hashRefresh(raw),
		FamilyID:  pgtype.UUID{Bytes: family, Valid: true},
		ExpiresAt: pgtype.Timestamptz{Time: exp, Valid: true},
	})
	if err != nil {
		return Tokens{}, 0, fmt.Errorf("refresh token yazılamadı: %w", err)
	}
	return Tokens{AccessToken: access, AccessExpiresAt: accessExp, RefreshToken: raw,
		RefreshExpiresAt: exp, Principal: p}, id, nil
}

func (s *Service) loadPrincipal(ctx context.Context, q *sqlc.Queries, userID int64) (Principal, error) {
	roles, err := q.ListUserRoles(ctx, userID)
	if err != nil {
		return Principal{}, err
	}
	if len(roles) == 0 {
		return Principal{}, ErrBadCredentials // rolü olmayan kullanıcı giriş yapamaz
	}
	p := Principal{UserID: userID, Roles: roles}
	if st, err := q.GetStudentByUserID(ctx, pgtype.Int8{Int64: userID, Valid: true}); err == nil {
		p.StudentID = st.ID
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, err
	}
	if l, err := q.GetLecturerByUserID(ctx, userID); err == nil {
		p.LecturerID = l.ID
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, err
	}
	return p, nil
}
