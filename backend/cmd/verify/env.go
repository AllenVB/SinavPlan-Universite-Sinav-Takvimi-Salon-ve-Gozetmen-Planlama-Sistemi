package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"
)

// env, kimlikler arasında paylaşılan durum.
type env struct {
	a        *api
	password string

	tok       map[string]string // rol -> access token
	userID    map[string]int64
	studentID int64

	periodID    int64
	periodExams int64
	sched1      int64 // taslak kalan ilk sürüm
	sched2      int64 // yayınlanan sürüm
	sn          *snapshot
	runID       int64 // bu koşuya özgü sayı (içe aktarma/oda kodları için)
}

var accounts = map[string]string{
	"secretary":   "seed.mercan@ddo-case.invalid",
	"lecturer":    "hoca@sinavplan.test",
	"invigilator": "gozetmen@sinavplan.test",
	"affairs":     "ogrenciisleri@sinavplan.test",
	"student":     "ogrenci@sinavplan.test",
}

// login, tüm demo rollerini bir kez giriş yaptırır.
func (e *env) login(ctx context.Context) error {
	if e.tok != nil {
		return nil
	}
	e.tok, e.userID = map[string]string{}, map[string]int64{}
	e.runID = time.Now().UnixNano() / 1000 % 900000
	for role, email := range accounts {
		r, err := e.a.post(ctx, "/auth/login", "", M{"email": email, "password": e.password})
		if err != nil {
			return err
		}
		if r.code != 200 {
			return fmt.Errorf("%s girişi: HTTP %d", email, r.code)
		}
		e.tok[role] = str(r.body, "access_token")
		u := obj(r.body, "user")
		e.userID[role] = num(u, "id")
		if role == "student" {
			e.studentID = num(u, "student_id")
		}
	}
	return nil
}

// uuidv7, istemci tarafı olay kimliği (RFC 9562).
func uuidv7(t time.Time) string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	ms := uint64(t.UnixMilli())
	b[0], b[1], b[2], b[3], b[4], b[5] = byte(ms>>40), byte(ms>>32), byte(ms>>24), byte(ms>>16), byte(ms>>8), byte(ms)
	b[6] = b[6]&0x0f | 0x70
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
