package store

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/AllenVB/sinavplan/backend/internal/domain/attendance"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// --- Yoklama senkronu -----------------------------------------------------------

// MaxSyncBatch, tek senkron isteğindeki en fazla olay.
const MaxSyncBatch = 1000

// Rejected, kabul edilmeyen olay ve nedeni.
type Rejected struct {
	EventID string `json:"event_id"`
	Reason  string `json:"reason"`
}

// SyncResult, senkron sonucu.
type SyncResult struct {
	Accepted     int        `json:"accepted"`
	Duplicates   int        `json:"duplicates"` // event_id daha önce alınmış (idempotent tekrar)
	Rejected     []Rejected `json:"rejected"`
	NewConflicts int        `json:"new_conflicts"`
}

type pair struct{ exam, student int64 }

// SyncAttendance, cihazdan gelen toplu olayları yazar.
//
//   - event_id UNIQUE: tekrar gelen olay `ON CONFLICT DO NOTHING` ile yok sayılır (idempotent).
//   - olaylar attendance_events'e eklemeli-yalnız yazılır (UPDATE/DELETE yetkisi yok, ADR-005).
//   - etkilenen her (sınav, öğrenci) için o çiftin TÜM olaylarından durum yeniden hesaplanır
//     (attendance.Resolve, sıradan bağımsız) ve attendance_state'e yazılır; yeni çakışmalar
//     attendance_conflicts'e eklenir.
//   - eşzamanlı iki senkron aynı çifte dokunursa çift başına transaction advisory kilidi
//     onları sıraya koyar; kilit, olaylar OKUNMADAN önce alınır, böylece ikinci işlem
//     birincinin işlediği olayları da görür. Kilitler sabit sırayla alınır (kilitlenme yok).
func SyncAttendance(ctx context.Context, pool *pgxpool.Pool, events []attendance.Event, recordedBy int64) (SyncResult, error) {
	res := SyncResult{Rejected: []Rejected{}}
	if len(events) > MaxSyncBatch {
		return res, fmt.Errorf("en fazla %d olay gönderilebilir", MaxSyncBatch)
	}
	var valid []attendance.Event
	seenInBatch := map[string]bool{}
	for _, e := range events {
		if err := e.Validate(); err != nil {
			res.Rejected = append(res.Rejected, Rejected{e.ID, err.Error()})
			continue
		}
		u, err := uuid.Parse(e.ID)
		if err != nil || u.Version() != 7 {
			res.Rejected = append(res.Rejected, Rejected{e.ID, "event_id UUIDv7 olmalı"})
			continue
		}
		if seenInBatch[e.ID] {
			res.Duplicates++
			continue
		}
		seenInBatch[e.ID] = true
		valid = append(valid, e)
	}
	if len(valid) == 0 {
		return res, nil
	}

	// Öğrenci gerçekten o sınava kayıtlı mı?
	exams := make([]int64, len(valid))
	studs := make([]int64, len(valid))
	for i, e := range valid {
		exams[i], studs[i] = e.ExamID, e.StudentID
	}
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT e.id, en.student_id
		FROM unnest($1::bigint[], $2::bigint[]) AS q(exam_id, student_id)
		JOIN "wm-exams" e        ON e.id = q.exam_id
		JOIN "wm-sections" s     ON s.course_id = e.course_id
		JOIN "wm-enrollments" en ON en.section_id = s.id AND en.student_id = q.student_id`, exams, studs)
	if err != nil {
		return res, err
	}
	enrolled := map[pair]bool{}
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.exam, &p.student); err != nil {
			rows.Close()
			return res, err
		}
		enrolled[p] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return res, err
	}
	var ok []attendance.Event
	for _, e := range valid {
		if !enrolled[pair{e.ExamID, e.StudentID}] {
			res.Rejected = append(res.Rejected, Rejected{e.ID, "öğrenci bu sınava kayıtlı değil"})
			continue
		}
		ok = append(ok, e)
	}
	if len(ok) == 0 {
		return res, nil
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return res, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	affected := map[pair]bool{}
	for _, e := range ok {
		var rec any
		if recordedBy != 0 {
			rec = recordedBy
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO "wm-attendance_events" (event_id, device_id, exam_id, student_id, status, source, hlc_wall, hlc_counter, recorded_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT (event_id) DO NOTHING`,
			e.ID, e.DeviceID, e.ExamID, e.StudentID, e.Status, e.Source, e.HLC.Wall, e.HLC.Counter, rec)
		if err != nil {
			return res, fmt.Errorf("olay yazılamadı: %w", err)
		}
		if tag.RowsAffected() == 1 {
			res.Accepted++
			affected[pair{e.ExamID, e.StudentID}] = true
		} else {
			res.Duplicates++
		}
	}
	n, err := resolvePairs(ctx, tx, affected)
	if err != nil {
		return res, err
	}
	res.NewConflicts = n
	return res, tx.Commit(ctx)
}

// resolvePairs, verilen çiftlerin durumunu tüm olaylarından yeniden hesaplar.
func resolvePairs(ctx context.Context, tx pgx.Tx, affected map[pair]bool) (int, error) {
	ps := make([]pair, 0, len(affected))
	for p := range affected {
		ps = append(ps, p)
	}
	sort.Slice(ps, func(a, b int) bool {
		if ps[a].exam != ps[b].exam {
			return ps[a].exam < ps[b].exam
		}
		return ps[a].student < ps[b].student
	})
	newConflicts := 0
	for _, p := range ps {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1::int, $2::int)`, int32(p.exam), int32(p.student)); err != nil {
			return 0, err
		}
		rows, err := tx.Query(ctx, `SELECT event_id::text, device_id, status, source, hlc_wall, hlc_counter
			FROM "wm-attendance_events" WHERE exam_id = $1 AND student_id = $2`, p.exam, p.student)
		if err != nil {
			return 0, err
		}
		var evs []attendance.Event
		for rows.Next() {
			e := attendance.Event{ExamID: p.exam, StudentID: p.student}
			if err := rows.Scan(&e.ID, &e.DeviceID, &e.Status, &e.Source, &e.HLC.Wall, &e.HLC.Counter); err != nil {
				rows.Close()
				return 0, err
			}
			evs = append(evs, e)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return 0, err
		}
		r := attendance.Resolve(evs)
		if len(r) == 0 {
			continue
		}
		w := r[0].Winner
		if _, err := tx.Exec(ctx, `
			INSERT INTO "wm-attendance_state" (exam_id, student_id, status, source, device_id, hlc_wall, hlc_counter, event_id, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,now())
			ON CONFLICT (exam_id, student_id) DO UPDATE SET status = EXCLUDED.status, source = EXCLUDED.source,
				device_id = EXCLUDED.device_id, hlc_wall = EXCLUDED.hlc_wall, hlc_counter = EXCLUDED.hlc_counter,
				event_id = EXCLUDED.event_id, updated_at = now()`,
			p.exam, p.student, w.Status, w.Source, w.DeviceID, w.HLC.Wall, w.HLC.Counter, w.ID); err != nil {
			return 0, err
		}
		for _, c := range r[0].Conflicts {
			tag, err := tx.Exec(ctx, `INSERT INTO "wm-attendance_conflicts" (exam_id, student_id, winner_event_id, loser_event_id, rule)
				VALUES ($1,$2,$3,$4,$5) ON CONFLICT (winner_event_id, loser_event_id) DO NOTHING`,
				p.exam, p.student, c.Winner.ID, c.Loser.ID, c.Rule)
			if err != nil {
				return 0, err
			}
			newConflicts += int(tag.RowsAffected())
		}
	}
	return newConflicts, nil
}

// --- Tek kullanımlık QR ---------------------------------------------------------

// QR hataları.
var (
	ErrQRInvalid = errors.New("QR geçersiz (imza tutmuyor)")
	ErrQRUsed    = errors.New("QR daha önce kullanılmış")
	ErrQRExpired = errors.New("QR süresi dolmuş veya iptal edilmiş")
)

// QR, sınav kartı token'larını üretir ve doğrular.
//
// Token = base64url( exam_id[8] | student_id[8] | nonce[16] | HMAC-SHA256(gövde)[32] ).
// HMAC, uydurma token'ı veritabanına gitmeden reddeder; veritabanında token'ın kendisi
// değil yalnızca SHA-256 özeti tutulur (sızıntıda token yeniden kurulamaz).
type QR struct {
	secret []byte
	mu     sync.Mutex
	clock  attendance.Clock
	// demoCode: doluysa bu 6 haneli kod, gözetmen oturumu olmadan öğrenciyi yoklamaya yazar (yalnız demo).
	demoCode string
}

// NewQR, verilen HMAC sırrıyla QR yöneticisi kurar.
func NewQR(secret string) *QR {
	return &QR{secret: []byte(secret), clock: attendance.Clock{Physical: func() int64 { return time.Now().UnixMilli() }}}
}

// WithDemoCode demo yoklama kodunu açar (boş = kapalı) ve QR yöneticisini döndürür.
func (q *QR) WithDemoCode(code string) *QR {
	q.demoCode = code
	return q
}

// IsDemoCode: kod, açık demo koduyla aynı mı (sabit zamanlı karşılaştırma; demo kapalıysa her zaman false).
func (q *QR) IsDemoCode(code string) bool {
	return code != "" && q.demoCode != "" && subtle.ConstantTimeCompare([]byte(code), []byte(q.demoCode)) == 1
}

// DemoCode açık demo kodunu döndürür (kapalıysa boş).
func (q *QR) DemoCode() string { return q.demoCode }

func (q *QR) sign(body []byte) []byte {
	m := hmac.New(sha256.New, q.secret)
	m.Write(body)
	return m.Sum(nil)
}

// Parse, token'ın imzasını doğrular ve kimlikleri döner (veritabanına dokunmaz).
func (q *QR) Parse(token string) (examID, studentID int64, err error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 8+8+16+32 {
		return 0, 0, ErrQRInvalid
	}
	body, mac := raw[:32], raw[32:]
	if !hmac.Equal(mac, q.sign(body)) {
		return 0, 0, ErrQRInvalid
	}
	return int64(binary.BigEndian.Uint64(body[:8])), int64(binary.BigEndian.Uint64(body[8:16])), nil
}

func tokenHash(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// Issue, (sınav, öğrenci) için yeni token üretir; aynı çiftin kullanılmamış eski
// token'larını iptal eder (kart yeniden istenirse eski QR çalışmaz).
func (q *QR) Issue(ctx context.Context, pool *pgxpool.Pool, examID, studentID int64, expires time.Time) (string, error) {
	body := make([]byte, 32)
	binary.BigEndian.PutUint64(body[:8], uint64(examID))
	binary.BigEndian.PutUint64(body[8:16], uint64(studentID))
	if _, err := rand.Read(body[16:]); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(append(body, q.sign(body)...))

	tx, err := pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE "wm-qr_tokens" SET revoked_at = now()
		WHERE exam_id = $1 AND student_id = $2 AND used_at IS NULL AND revoked_at IS NULL`, examID, studentID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO "wm-qr_tokens" (exam_id, student_id, token_hash, expires_at) VALUES ($1,$2,$3,$4)`,
		examID, studentID, tokenHash(token), expires); err != nil {
		return "", err
	}
	return token, tx.Commit(ctx)
}

// QRUse, başarılı kullanımın sonucu.
type QRUse struct {
	ExamID    int64  `json:"exam_id"`
	StudentID int64  `json:"student_id"`
	EventID   string `json:"event_id"`
}

// Use, token'ı TEK KEZ kullanır ve "present / qr" yoklama olayı üretir.
//
// Yarışı koşullu UPDATE çözer: `... WHERE token_hash = $h AND used_at IS NULL ...
// RETURNING`. Aynı token'la eşzamanlı N istekte satır kilidi tek bir UPDATE'in
// koşulu sağlamasına izin verir; diğerleri kilidi bekleyip used_at'ı dolu görür ve
// 0 satır alır -> ErrQRUsed. Kullanım ve yoklama olayı aynı transaction'dadır.
func (q *QR) Use(ctx context.Context, pool *pgxpool.Pool, token, deviceID string, actor int64) (QRUse, error) {
	if _, _, err := q.Parse(token); err != nil {
		return QRUse{}, err
	}
	if deviceID == "" {
		return QRUse{}, fmt.Errorf("device_id zorunlu")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return QRUse{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var u QRUse
	err = tx.QueryRow(ctx, `UPDATE "wm-qr_tokens" SET used_at = now(), used_by_device = $2
		WHERE token_hash = $1 AND used_at IS NULL AND revoked_at IS NULL AND expires_at > now()
		RETURNING exam_id, student_id`, tokenHash(token), deviceID).Scan(&u.ExamID, &u.StudentID)
	if errors.Is(err, pgx.ErrNoRows) {
		var used, revoked bool
		var expired bool
		err2 := tx.QueryRow(ctx, `SELECT used_at IS NOT NULL, revoked_at IS NOT NULL, expires_at <= now()
			FROM "wm-qr_tokens" WHERE token_hash = $1`, tokenHash(token)).Scan(&used, &revoked, &expired)
		switch {
		case errors.Is(err2, pgx.ErrNoRows):
			return QRUse{}, ErrNotFound
		case err2 != nil:
			return QRUse{}, err2
		case used:
			return QRUse{}, ErrQRUsed
		default:
			return QRUse{}, ErrQRExpired
		}
	}
	if err != nil {
		return QRUse{}, err
	}

	q.mu.Lock()
	ts := q.clock.Now()
	q.mu.Unlock()
	id, err := uuid.NewV7()
	if err != nil {
		return QRUse{}, err
	}
	u.EventID = id.String()
	var rec any
	if actor != 0 {
		rec = actor
	}
	if _, err := tx.Exec(ctx, `INSERT INTO "wm-attendance_events" (event_id, device_id, exam_id, student_id, status, source, hlc_wall, hlc_counter, recorded_by)
		VALUES ($1,$2,$3,$4,'present','qr',$5,$6,$7)`, u.EventID, deviceID, u.ExamID, u.StudentID, ts.Wall, ts.Counter, rec); err != nil {
		return QRUse{}, err
	}
	if _, err := resolvePairs(ctx, tx, map[pair]bool{{u.ExamID, u.StudentID}: true}); err != nil {
		return QRUse{}, err
	}
	return u, tx.Commit(ctx)
}
