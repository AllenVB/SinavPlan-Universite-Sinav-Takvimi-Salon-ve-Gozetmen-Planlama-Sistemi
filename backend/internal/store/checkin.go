package store

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Yoklama oturumu (UBYS tarzı): gözetmen "Yeni QR oluştur" der, ekranda dönen bir QR görünür;
// öğrenci kendi hesabıyla okutunca yoklamaya "geldi" yazılır. QR okunamazsa gözetmenin
// ekranındaki 6 haneli kod girilir.
var (
	ErrCheckinWindow   = errors.New("yoklama oturumu yalnız sınavdan 30 dk önce ile sınav bitiminden 15 dk sonrası arasında açılabilir")
	ErrCheckinClosed   = errors.New("yoklama oturumu kapalı veya süresi dolmuş")
	ErrCheckinBadCode  = errors.New("kod yanlış veya süresi dolmuş")
	ErrCheckinNotHere  = errors.New("bu oturum sizin sınavınız/salonunuz için değil")
	ErrCheckinExcused  = errors.New("bu sınav için onaylanmış mazeretiniz var")
	ErrCheckinTokenOld = errors.New("QR eskimiş; ekrandaki güncel QR'ı okutun")
)

const (
	// CheckinTokenWindow, QR'ın değişme aralığı. Jeton bir önceki pencerede de geçerlidir (okuma gecikmesi payı).
	CheckinTokenWindow = 30 * time.Second
	// CheckinCodeTTL, 6 haneli kodun ömrü.
	CheckinCodeTTL = 10 * time.Minute
)

// CheckinSession, gözetmen ekranına dönen oturum özeti.
type CheckinSession struct {
	ID            int64     `json:"id"`
	SlotID        int64     `json:"slot_id"`
	RoomID        int64     `json:"room_id"`
	RoomCode      string    `json:"room_code"`
	Code          string    `json:"code"`
	CodeExpiresAt time.Time `json:"code_expires_at"`
	EndsAt        time.Time `json:"ends_at"`
	Present       int       `json:"present"`
	Expected      int       `json:"expected"`
	Token         string    `json:"token,omitempty"`
	TokenExpires  time.Time `json:"token_expires_at,omitempty"`
}

func randomCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func (q *QR) checkinMAC(sessionID, window int64) []byte {
	b := make([]byte, 16)
	binary.BigEndian.PutUint64(b[:8], uint64(sessionID))
	binary.BigEndian.PutUint64(b[8:], uint64(window))
	return append(b, q.sign(append([]byte("checkin|"), b...))...)
}

// CheckinToken, oturumun o anki QR jetonunu üretir (veritabanına dokunmaz).
func (q *QR) CheckinToken(sessionID int64, now time.Time) (string, time.Time) {
	w := now.Unix() / int64(CheckinTokenWindow.Seconds())
	exp := time.Unix((w+1)*int64(CheckinTokenWindow.Seconds()), 0)
	return "SP1." + base64.RawURLEncoding.EncodeToString(q.checkinMAC(sessionID, w)), exp
}

// parseCheckinToken, imzayı doğrular ve oturum kimliğini döner; pencere geçerli veya bir önceki olmalı.
func (q *QR) parseCheckinToken(token string, now time.Time) (int64, error) {
	if len(token) < 4 || token[:4] != "SP1." {
		return 0, ErrQRInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(token[4:])
	if err != nil || len(raw) != 16+32 {
		return 0, ErrQRInvalid
	}
	sid, win := int64(binary.BigEndian.Uint64(raw[:8])), int64(binary.BigEndian.Uint64(raw[8:16]))
	if !hmac.Equal(raw, q.checkinMAC(sid, win)) {
		return 0, ErrQRInvalid
	}
	cur := now.Unix() / int64(CheckinTokenWindow.Seconds())
	if win != cur && win != cur-1 {
		return 0, ErrCheckinTokenOld
	}
	return sid, nil
}

const checkinSelect = `SELECT cs.id, cs.slot_id, cs.room_id, r.code, cs.code, cs.code_expires_at, cs.ends_at,
	cs.schedule_id,
	(SELECT count(*) FROM "wm-attendance_state" a JOIN "wm-seat_assignments" sa ON sa.exam_id = a.exam_id AND sa.student_id = a.student_id
	  WHERE sa.schedule_id = cs.schedule_id AND sa.slot_id = cs.slot_id AND sa.room_id = cs.room_id AND a.status = 'present'),
	(SELECT count(*) FROM "wm-seat_assignments" sa WHERE sa.schedule_id = cs.schedule_id AND sa.slot_id = cs.slot_id AND sa.room_id = cs.room_id)
	FROM "wm-checkin_sessions" cs JOIN "wm-rooms" r ON r.id = cs.room_id `

func scanCheckin(row pgx.Row) (CheckinSession, int64, error) {
	var c CheckinSession
	var sched int64
	err := row.Scan(&c.ID, &c.SlotID, &c.RoomID, &c.RoomCode, &c.Code, &c.CodeExpiresAt, &c.EndsAt, &sched, &c.Present, &c.Expected)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, 0, ErrNotFound
	}
	return c, sched, err
}

// StartCheckin, gözetmenin (slot, salon) görevi için yeni oturum açar; varsa önceki açık oturumu kapatır.
func (q *QR) StartCheckin(ctx context.Context, pool *pgxpool.Pool, userID, slotID, roomID int64, now time.Time) (CheckinSession, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return CheckinSession{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var sched int64
	var starts, ends time.Time
	err = tx.QueryRow(ctx, `SELECT ia.schedule_id, sl.starts_at, sl.ends_at
		FROM "wm-invigilator_assignments" ia
		JOIN "wm-schedules" sc ON sc.id = ia.schedule_id AND sc.status = 'published'
		JOIN "wm-slots" sl ON sl.id = ia.slot_id
		WHERE ia.user_id = $1 AND ia.slot_id = $2 AND ia.room_id = $3
		LIMIT 1`, userID, slotID, roomID).Scan(&sched, &starts, &ends)
	if errors.Is(err, pgx.ErrNoRows) {
		return CheckinSession{}, ErrNotFound // bu görev sizin değil
	}
	if err != nil {
		return CheckinSession{}, err
	}
	if now.Before(starts.Add(-30*time.Minute)) || now.After(ends.Add(15*time.Minute)) {
		return CheckinSession{}, ErrCheckinWindow
	}
	code, err := randomCode()
	if err != nil {
		return CheckinSession{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE "wm-checkin_sessions" SET closed_at = now()
		WHERE schedule_id = $1 AND slot_id = $2 AND room_id = $3 AND closed_at IS NULL`, sched, slotID, roomID); err != nil {
		return CheckinSession{}, err
	}
	var id int64
	if err := tx.QueryRow(ctx, `INSERT INTO "wm-checkin_sessions" (schedule_id, slot_id, room_id, started_by, code, code_expires_at, ends_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`, sched, slotID, roomID, userID, code, now.Add(CheckinCodeTTL), ends.Add(15*time.Minute)).Scan(&id); err != nil {
		return CheckinSession{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CheckinSession{}, err
	}
	return q.CheckinState(ctx, pool, userID, id, now)
}

// CheckinState, gözetmen için oturumun güncel durumunu ve taze QR jetonunu döner.
func (q *QR) CheckinState(ctx context.Context, pool *pgxpool.Pool, userID, id int64, now time.Time) (CheckinSession, error) {
	c, _, err := scanCheckin(pool.QueryRow(ctx, checkinSelect+`
		WHERE cs.id = $1 AND cs.closed_at IS NULL AND cs.started_by = $2`, id, userID))
	if err != nil {
		return c, err
	}
	if !now.Before(c.EndsAt) {
		return c, ErrCheckinClosed
	}
	c.Token, c.TokenExpires = q.CheckinToken(id, now)
	return c, nil
}

// RotateCheckinCode, 6 haneli yedek kodu yeniler (eski kod anında geçersiz olur).
func (q *QR) RotateCheckinCode(ctx context.Context, pool *pgxpool.Pool, userID, id int64, now time.Time) (CheckinSession, error) {
	code, err := randomCode()
	if err != nil {
		return CheckinSession{}, err
	}
	tag, err := pool.Exec(ctx, `UPDATE "wm-checkin_sessions" SET code = $3, code_expires_at = $4
		WHERE id = $1 AND started_by = $2 AND closed_at IS NULL`, id, userID, code, now.Add(CheckinCodeTTL))
	if err != nil {
		return CheckinSession{}, err
	}
	if tag.RowsAffected() == 0 {
		return CheckinSession{}, ErrNotFound
	}
	return q.CheckinState(ctx, pool, userID, id, now)
}

// CloseCheckin, oturumu kapatır (QR ve kod artık çalışmaz).
func CloseCheckin(ctx context.Context, pool *pgxpool.Pool, userID, id int64) error {
	tag, err := pool.Exec(ctx, `UPDATE "wm-checkin_sessions" SET closed_at = now() WHERE id = $1 AND started_by = $2 AND closed_at IS NULL`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CheckinResult, öğrenciye dönen sonuç.
type CheckinResult struct {
	ExamID        int64  `json:"exam_id"`
	CourseCode    string `json:"course_code"`
	CourseName    string `json:"course_name"`
	RoomCode      string `json:"room_code"`
	AlreadyMarked bool   `json:"already_present"`
	Method        string `json:"method"` // qr | code
}

// CheckIn, öğrencinin QR (token) veya 6 haneli kodla kendini yoklamaya yazmasını sağlar.
//
// Kural: oturumun (slot, salon) kapsamında o öğrenciye atanmış bir koltuk olmalı; böylece başka salondaki
// biri kodu öğrenip yoklamaya yazılamaz. Kod yoluyla `manual`, QR yoluyla `qr` kaynağı yazılır.
func (q *QR) CheckIn(ctx context.Context, pool *pgxpool.Pool, userID, studentID int64, token, code string, now time.Time) (CheckinResult, error) {
	if code != "" && q.demoCode != "" && subtle.ConstantTimeCompare([]byte(code), []byte(q.demoCode)) == 1 {
		return q.demoCheckIn(ctx, pool, userID, studentID, now)
	}
	var sessionID int64
	method := "qr"
	if token != "" {
		id, err := q.parseCheckinToken(token, now)
		if err != nil {
			return CheckinResult{}, err
		}
		sessionID = id
	} else {
		method = "code"
		// Kod, öğrencinin şu an açık oturumu olan salon+slotundaki oturumlarla karşılaştırılır.
		rows, err := pool.Query(ctx, `SELECT cs.id, cs.code, cs.code_expires_at FROM "wm-checkin_sessions" cs
			JOIN "wm-seat_assignments" sa ON sa.schedule_id = cs.schedule_id AND sa.slot_id = cs.slot_id AND sa.room_id = cs.room_id
			WHERE sa.student_id = $1 AND cs.closed_at IS NULL AND cs.ends_at > $2`, studentID, now)
		if err != nil {
			return CheckinResult{}, err
		}
		type cand struct {
			id  int64
			c   string
			exp time.Time
		}
		cands, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (cand, error) {
			var c cand
			return c, r.Scan(&c.id, &c.c, &c.exp)
		})
		if err != nil {
			return CheckinResult{}, err
		}
		for _, c := range cands {
			if subtle.ConstantTimeCompare([]byte(c.c), []byte(code)) == 1 && now.Before(c.exp) {
				sessionID = c.id
			}
		}
		if sessionID == 0 {
			return CheckinResult{}, ErrCheckinBadCode
		}
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return CheckinResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var sched, slotID, roomID int64
	var ends time.Time
	err = tx.QueryRow(ctx, `SELECT schedule_id, slot_id, room_id, ends_at FROM "wm-checkin_sessions" WHERE id = $1 AND closed_at IS NULL`, sessionID).
		Scan(&sched, &slotID, &roomID, &ends)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !now.Before(ends)) {
		return CheckinResult{}, ErrCheckinClosed
	}
	if err != nil {
		return CheckinResult{}, err
	}
	var res CheckinResult
	res.Method = method
	err = tx.QueryRow(ctx, `SELECT sa.exam_id, co.code, co.name, r.code
		FROM "wm-seat_assignments" sa JOIN "wm-exams" e ON e.id = sa.exam_id JOIN "wm-courses" co ON co.id = e.course_id JOIN "wm-rooms" r ON r.id = sa.room_id
		WHERE sa.schedule_id = $1 AND sa.slot_id = $2 AND sa.room_id = $3 AND sa.student_id = $4`, sched, slotID, roomID, studentID).
		Scan(&res.ExamID, &res.CourseCode, &res.CourseName, &res.RoomCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return CheckinResult{}, ErrCheckinNotHere
	}
	if err != nil {
		return CheckinResult{}, err
	}
	source := "qr"
	if method == "code" {
		source = "manual"
	}
	return q.recordPresent(ctx, tx, res, userID, studentID, source, fmt.Sprintf("self-checkin-%d", sessionID))
}

// recordPresent: mazeret denetimi, zaten-geldi denetimi ve 'present' olayı (+ çakışma çözümü); işlemi kapatır.
func (q *QR) recordPresent(ctx context.Context, tx pgx.Tx, res CheckinResult, userID, studentID int64, source, device string) (CheckinResult, error) {
	var excused bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM "wm-excuses" WHERE kind='student' AND student_id=$1 AND exam_id=$2 AND status='approved')`,
		studentID, res.ExamID).Scan(&excused); err != nil {
		return CheckinResult{}, err
	}
	if excused {
		return CheckinResult{}, ErrCheckinExcused
	}
	var status string
	err := tx.QueryRow(ctx, `SELECT status FROM "wm-attendance_state" WHERE exam_id = $1 AND student_id = $2`, res.ExamID, studentID).Scan(&status)
	if err == nil && status == "present" {
		res.AlreadyMarked = true
		return res, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return CheckinResult{}, err
	}

	q.mu.Lock()
	ts := q.clock.Now()
	q.mu.Unlock()
	id, err := uuid.NewV7()
	if err != nil {
		return CheckinResult{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO "wm-attendance_events" (event_id, device_id, exam_id, student_id, status, source, hlc_wall, hlc_counter, recorded_by)
		VALUES ($1,$2,$3,$4,'present',$5,$6,$7,$8)`, id.String(), device, res.ExamID, studentID, source, ts.Wall, ts.Counter, userID); err != nil {
		return CheckinResult{}, err
	}
	if _, err := resolvePairs(ctx, tx, map[pair]bool{{res.ExamID, studentID}: true}); err != nil {
		return CheckinResult{}, err
	}
	return res, tx.Commit(ctx)
}

// demoCheckIn: DEMO_CHECKIN_CODE (ör. 111111) girilince öğrenci, yayındaki çizelgede şu ana en yakın (mazeretsiz) sınavına
// 'geldi' yazılır. Yalnızca demo içindir (değişken boşsa kapalı); gözetmen oturumu gerekmez, salon/koltuk denetimi
// yoktur — bu yüzden gerçek dağıtımlarda ayarlanmamalıdır. Kaynak `manual`, cihaz `demo-code` olarak izlenir.
func (q *QR) demoCheckIn(ctx context.Context, pool *pgxpool.Pool, userID, studentID int64, now time.Time) (CheckinResult, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return CheckinResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	res := CheckinResult{Method: "demo"}
	err = tx.QueryRow(ctx, `SELECT sa.exam_id, co.code, co.name, r.code
		FROM "wm-seat_assignments" sa
		JOIN "wm-schedules" sc ON sc.id = sa.schedule_id AND sc.status = 'published'
		JOIN "wm-slots" sl ON sl.id = sa.slot_id
		JOIN "wm-exams" e ON e.id = sa.exam_id JOIN "wm-courses" co ON co.id = e.course_id JOIN "wm-rooms" r ON r.id = sa.room_id
		WHERE sa.student_id = $1
		  AND NOT EXISTS (SELECT 1 FROM "wm-excuses" x WHERE x.kind = 'student' AND x.student_id = sa.student_id AND x.exam_id = sa.exam_id AND x.status = 'approved')
		ORDER BY abs(extract(epoch FROM (sl.starts_at - $2::timestamptz))), sa.exam_id LIMIT 1`, studentID, now).
		Scan(&res.ExamID, &res.CourseCode, &res.CourseName, &res.RoomCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return CheckinResult{}, ErrCheckinNotHere
	}
	if err != nil {
		return CheckinResult{}, err
	}
	return q.recordPresent(ctx, tx, res, userID, studentID, "manual", "demo-code")
}
