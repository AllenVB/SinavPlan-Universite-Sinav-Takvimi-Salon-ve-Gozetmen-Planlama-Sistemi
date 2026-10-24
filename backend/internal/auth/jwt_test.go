package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "test-secret-0123456789-0123456789-abcdef"

func TestIssueAndParseRoundTrip(t *testing.T) {
	iss := NewIssuer(testSecret, 15*time.Minute)
	in := Principal{UserID: 42, Roles: []string{RoleStudent}, StudentID: 7}
	tok, exp, err := iss.Issue(in)
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Until(exp); d < 14*time.Minute || d > 15*time.Minute {
		t.Fatalf("access token süresi 15 dk olmalı, %v", d)
	}
	out, err := iss.Parse(tok)
	if err != nil {
		t.Fatal(err)
	}
	if out.UserID != 42 || out.StudentID != 7 || !out.HasAnyRole(RoleStudent) || out.HasAnyRole(RoleSecretary) {
		t.Fatalf("principal bozuldu: %+v", out)
	}
}

func TestParseRejectsExpiredToken(t *testing.T) {
	iss := NewIssuer(testSecret, time.Minute)
	iss.now = func() time.Time { return time.Now().Add(-2 * time.Minute) }
	tok, _, _ := iss.Issue(Principal{UserID: 1, Roles: []string{RoleSecretary}})
	iss.now = time.Now
	if _, err := iss.Parse(tok); err != ErrInvalidToken {
		t.Fatalf("süresi dolmuş token reddedilmeli: %v", err)
	}
}

func TestParseRejectsWrongSecretAndTampering(t *testing.T) {
	tok, _, _ := NewIssuer(testSecret, time.Minute).Issue(Principal{UserID: 1, Roles: []string{RoleStudent}})
	if _, err := NewIssuer(testSecret+"x", time.Minute).Parse(tok); err != ErrInvalidToken {
		t.Fatalf("başka sırla imzalanmış token reddedilmeli: %v", err)
	}
	// Yük kısmını değiştir (rol yükseltme denemesi): imza tutmaz.
	parts := strings.Split(tok, ".")
	parts[1] = parts[1][:len(parts[1])-2] + "AA"
	if _, err := NewIssuer(testSecret, time.Minute).Parse(strings.Join(parts, ".")); err != ErrInvalidToken {
		t.Fatalf("kurcalanmış token reddedilmeli: %v", err)
	}
}

// "alg": "none" saldırısı: imzasız token kabul edilmemeli.
func TestParseRejectsAlgNone(t *testing.T) {
	c := claims{Roles: []string{RoleSecretary}, RegisteredClaims: jwt.RegisteredClaims{
		Issuer: issuer, Subject: "1", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodNone, c).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewIssuer(testSecret, time.Minute).Parse(tok); err != ErrInvalidToken {
		t.Fatalf("alg=none reddedilmeli: %v", err)
	}
}

func TestParseRejectsMissingRolesOrSubject(t *testing.T) {
	iss := NewIssuer(testSecret, time.Minute)
	for _, c := range []claims{
		{Roles: nil, RegisteredClaims: jwt.RegisteredClaims{Issuer: issuer, Subject: "1",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}},
		{Roles: []string{RoleStudent}, RegisteredClaims: jwt.RegisteredClaims{Issuer: issuer, Subject: "abc",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}},
		{Roles: []string{RoleStudent}, RegisteredClaims: jwt.RegisteredClaims{Issuer: "baska", Subject: "1",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}},
	} {
		tok, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte(testSecret))
		if _, err := iss.Parse(tok); err != ErrInvalidToken {
			t.Errorf("eksik/yanlış iddialı token reddedilmeli (%+v): %v", c, err)
		}
	}
}
