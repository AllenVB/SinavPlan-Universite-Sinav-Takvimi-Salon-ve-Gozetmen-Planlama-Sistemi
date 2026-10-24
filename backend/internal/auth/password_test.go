package auth

import (
	"strings"
	"testing"
)

func TestHashAndVerifyRoundTrip(t *testing.T) {
	h, err := HashPassword("Demo!1234")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("beklenmeyen biçim: %s", h)
	}
	ok, err := VerifyPassword("Demo!1234", h)
	if err != nil || !ok {
		t.Fatalf("doğru parola kabul edilmeliydi: ok=%v err=%v", ok, err)
	}
}

func TestVerifyRejectsWrongPassword(t *testing.T) {
	h, _ := HashPassword("dogru")
	ok, err := VerifyPassword("yanlis", h)
	if err != nil {
		t.Fatalf("yanlış parola hata değil false döndürmeli: %v", err)
	}
	if ok {
		t.Fatal("yanlış parola kabul edildi")
	}
}

func TestSameLengthPasswordsProduceDifferentHashes(t *testing.T) {
	a, _ := HashPassword("ayni")
	b, _ := HashPassword("ayni")
	if a == b {
		t.Fatal("tuz rastgele olmalı; iki özet aynı çıktı")
	}
}

func TestVerifyRejectsMalformedHashes(t *testing.T) {
	cases := []string{
		"", "düz-metin", "$bcrypt$x", "$argon2id$v=19$m=1,t=1$salt$key",
		"$argon2id$v=18$m=19456,t=2,p=1$YWJj$YWJj",     // yanlış sürüm
		"$argon2id$v=19$m=999999999,t=2,p=1$YWJj$YWJj", // bellek bombası
		"$argon2id$v=19$m=19456,t=2,p=1$!!!$YWJj",      // bozuk base64
	}
	for _, c := range cases {
		ok, err := VerifyPassword("x", c)
		if ok || err == nil {
			t.Errorf("%q reddedilmeliydi (ok=%v err=%v)", c, ok, err)
		}
	}
}
