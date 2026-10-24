package auth

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Roller. Sistemdeki tek kaynak burasıdır; RBAC listeleri bu sabitleri kullanır.
const (
	RoleSecretary      = "secretary"
	RoleLecturer       = "lecturer"
	RoleInvigilator    = "invigilator"
	RoleStudentAffairs = "student_affairs"
	RoleStudent        = "student"
)

const issuer = "sinavplan"

// ErrInvalidToken, imzası, süresi veya biçimi geçersiz token için döner. Ayrıntı
// bilerek ayrıştırılmaz: istemciye "neden" geçersiz olduğunu söylemeyiz.
var ErrInvalidToken = errors.New("geçersiz veya süresi dolmuş token")

// Principal, kimliği doğrulanmış isteğin sahibidir.
//
// Öğrenci ve öğretim üyesi kimlikleri giriş anında token'a gömülür; böylece
// sahiplik kontrolü her istekte veritabanı turu gerektirmez. Bedeli: rol veya
// bağ değişikliği en geç access token süresi (15 dk) sonunda etkili olur.
type Principal struct {
	UserID     int64
	Roles      []string
	StudentID  int64 // 0 = öğrenci değil
	LecturerID int64 // 0 = öğretim üyesi değil
}

// HasAnyRole, principal'ın verilen rollerden en az birine sahip olup olmadığını söyler.
func (p Principal) HasAnyRole(roles ...string) bool {
	for _, have := range p.Roles {
		for _, want := range roles {
			if have == want {
				return true
			}
		}
	}
	return false
}

type claims struct {
	Roles      []string `json:"roles"`
	StudentID  int64    `json:"sid,omitempty"`
	LecturerID int64    `json:"lid,omitempty"`
	jwt.RegisteredClaims
}

// Issuer, access token üretir ve doğrular (HS256).
type Issuer struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

// NewIssuer, verilen sır ve süreyle bir Issuer kurar.
func NewIssuer(secret string, ttl time.Duration) *Issuer {
	return &Issuer{secret: []byte(secret), ttl: ttl, now: time.Now}
}

// Issue, principal için imzalı access token ve bitiş zamanını döner.
func (i *Issuer) Issue(p Principal) (string, time.Time, error) {
	now := i.now()
	exp := now.Add(i.ttl)
	c := claims{
		Roles: p.Roles, StudentID: p.StudentID, LecturerID: p.LecturerID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   strconv.FormatInt(p.UserID, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(i.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("token imzalanamadı: %w", err)
	}
	return s, exp, nil
}

// Parse, token'ı doğrular. Algoritma açıkça HS256'ya sabitlenir: "alg": "none"
// veya asimetrik algoritma ile anahtar karıştırma saldırıları reddedilir.
func (i *Issuer) Parse(token string) (Principal, error) {
	var c claims
	_, err := jwt.ParseWithClaims(token, &c, func(*jwt.Token) (any, error) { return i.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(issuer),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(i.now),
	)
	if err != nil {
		return Principal{}, ErrInvalidToken
	}
	uid, err := strconv.ParseInt(c.Subject, 10, 64)
	if err != nil || uid <= 0 || len(c.Roles) == 0 {
		return Principal{}, ErrInvalidToken
	}
	return Principal{UserID: uid, Roles: c.Roles, StudentID: c.StudentID, LecturerID: c.LecturerID}, nil
}
