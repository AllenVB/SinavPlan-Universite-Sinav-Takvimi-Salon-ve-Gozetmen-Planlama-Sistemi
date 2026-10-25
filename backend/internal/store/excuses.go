package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/AllenVB/sinavplan/backend/internal/audit"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Excuse, mazeret kaydı. Öğrenci mazereti bir sınava, gözetmen mazereti bir slota bağlıdır.
type Excuse struct {
	ID           int64      `json:"id"`
	Kind         string     `json:"kind"` // student | invigilator
	StudentID    *int64     `json:"student_id,omitempty"`
	UserID       *int64     `json:"user_id,omitempty"`
	ExamID       *int64     `json:"exam_id,omitempty"`
	SlotID       *int64     `json:"slot_id,omitempty"`
	Reason       string     `json:"reason"`
	Status       string     `json:"status"` // pending | approved | rejected
	DecidedBy    *int64     `json:"decided_by,omitempty"`
	DecidedAt    *time.Time `json:"decided_at,omitempty"`
	DecisionNote *string    `json:"decision_note,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	// Okunabilir etiketler (yalnız liste uçlarında dolar): kim ve neyle ilgili.
	Person string `json:"person,omitempty"`
	Target string `json:"target,omitempty"`
}

const excuseCols = `id, kind, student_id, user_id, exam_id, slot_id, reason, status, decided_by, decided_at, decision_note, created_at`

func scanExcuse(r pgx.Row) (Excuse, error) {
	var e Excuse
	err := r.Scan(&e.ID, &e.Kind, &e.StudentID, &e.UserID, &e.ExamID, &e.SlotID, &e.Reason, &e.Status, &e.DecidedBy, &e.DecidedAt, &e.DecisionNote, &e.CreatedAt)
	return e, err
}

func validReason(reason string) error {
	if l := len([]rune(reason)); l < 5 || l > 1000 {
		return &ValidationError{map[string]string{"reason": "5 ile 1000 karakter arasında olmalı"}}
	}
	return nil
}

// CreateStudentExcuse: öğrenci, KAYITLI olduğu sınav için mazeret girer. Aynı sınav için
// bekleyen veya onaylanmış mazeret varsa ErrConflict; kayıtlı değilse ErrNotFound.
func CreateStudentExcuse(ctx context.Context, pool *pgxpool.Pool, studentID, examID int64, reason string) (Excuse, error) {
	if err := validReason(reason); err != nil {
		return Excuse{}, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return Excuse{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var ok bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM exams e JOIN sections s ON s.course_id = e.course_id
		JOIN enrollments en ON en.section_id = s.id WHERE e.id = $1 AND en.student_id = $2)`, examID, studentID).Scan(&ok); err != nil {
		return Excuse{}, err
	}
	if !ok {
		return Excuse{}, ErrNotFound
	}
	// Aynı (öğrenci, sınav) için eşzamanlı iki başvuruyu sıraya koy.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7001, $1::int)`, int32(studentID)); err != nil {
		return Excuse{}, err
	}
	var dup bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM excuses WHERE kind='student' AND student_id=$1 AND exam_id=$2 AND status IN ('pending','approved'))`,
		studentID, examID).Scan(&dup); err != nil {
		return Excuse{}, err
	}
	if dup {
		return Excuse{}, ErrConflict
	}
	e, err := scanExcuse(tx.QueryRow(ctx, `INSERT INTO excuses (kind, student_id, exam_id, reason) VALUES ('student',$1,$2,$3) RETURNING `+excuseCols, studentID, examID, reason))
	if err != nil {
		return Excuse{}, err
	}
	if err := audit.Write(ctx, tx, audit.Entry{Action: "excuse.create", Entity: "excuse", EntityID: fmt.Sprint(e.ID), New: map[string]any{"kind": "student", "exam_id": examID}}); err != nil {
		return Excuse{}, err
	}
	return e, tx.Commit(ctx)
}

// CreateInvigilatorExcuse: gözetmen bir slot için mazeret girer.
func CreateInvigilatorExcuse(ctx context.Context, pool *pgxpool.Pool, userID, slotID int64, reason string) (Excuse, error) {
	if err := validReason(reason); err != nil {
		return Excuse{}, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return Excuse{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var ok bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM slots WHERE id = $1)`, slotID).Scan(&ok); err != nil {
		return Excuse{}, err
	}
	if !ok {
		return Excuse{}, ErrNotFound
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7002, $1::int)`, int32(userID)); err != nil {
		return Excuse{}, err
	}
	var dup bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM excuses WHERE kind='invigilator' AND user_id=$1 AND slot_id=$2 AND status IN ('pending','approved'))`,
		userID, slotID).Scan(&dup); err != nil {
		return Excuse{}, err
	}
	if dup {
		return Excuse{}, ErrConflict
	}
	e, err := scanExcuse(tx.QueryRow(ctx, `INSERT INTO excuses (kind, user_id, slot_id, reason) VALUES ('invigilator',$1,$2,$3) RETURNING `+excuseCols, userID, slotID, reason))
	if err != nil {
		return Excuse{}, err
	}
	if err := audit.Write(ctx, tx, audit.Entry{ActorID: userID, Action: "excuse.create", Entity: "excuse", EntityID: fmt.Sprint(e.ID), New: map[string]any{"kind": "invigilator", "slot_id": slotID}}); err != nil {
		return Excuse{}, err
	}
	return e, tx.Commit(ctx)
}

// ListExcuses: durum ve tür süzgeçli, keyset sayfalı liste. ownerStudent/ownerUser sıfırdan
// farklıysa yalnız o kişinin mazeretleri (öğrenci/gözetmen kendi kayıtlarını görür).
func ListExcuses(ctx context.Context, pool *pgxpool.Pool, status, kind string, ownerStudent, ownerUser, after int64, limit int) ([]Excuse, error) {
	rows, err := pool.Query(ctx, `SELECT x.id, x.kind, x.student_id, x.user_id, x.exam_id, x.slot_id, x.reason, x.status, x.decided_by, x.decided_at, x.decision_note, x.created_at,
		       coalesce(CASE WHEN x.kind = 'student'
		         THEN (SELECT st.full_name || ' (' || st.student_no || ')' FROM students st WHERE st.id = x.student_id)
		         ELSE (SELECT u.full_name FROM users u WHERE u.id = x.user_id) END, ''),
		       coalesce(CASE WHEN x.kind = 'student'
		         THEN (SELECT c.code || ' — ' || c.name FROM exams e JOIN courses c ON c.id = e.course_id WHERE e.id = x.exam_id)
		         ELSE (SELECT to_char(sl.starts_at AT TIME ZONE 'Europe/Istanbul', 'DD.MM.YYYY HH24:MI') || ' oturumu' FROM slots sl WHERE sl.id = x.slot_id) END, '')
		FROM excuses x
		WHERE x.id > $1 AND ($2 = '' OR x.status = $2) AND ($3 = '' OR x.kind = $3)
		  AND ($4 = 0 OR x.student_id = $4) AND ($5 = 0 OR x.user_id = $5)
		ORDER BY x.id LIMIT $6`, after, status, kind, ownerStudent, ownerUser, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Excuse, error) {
		var e Excuse
		err := r.Scan(&e.ID, &e.Kind, &e.StudentID, &e.UserID, &e.ExamID, &e.SlotID, &e.Reason, &e.Status, &e.DecidedBy, &e.DecidedAt, &e.DecisionNote, &e.CreatedAt, &e.Person, &e.Target)
		return e, err
	})
}

// DecideExcuse: bekleyen mazereti onaylar/reddeder. Satır FOR UPDATE ile kilitlenir;
// zaten karara bağlanmışsa ErrConflict (eşzamanlı iki karardan yalnız biri geçer).
// Onaylanmış gözetmen mazereti sonraki çizelge üretimlerinde T-4 kısıtı olur;
// onaylanmış öğrenci mazereti o sınav için sınav kartı verilmesini engeller (V-6).
func DecideExcuse(ctx context.Context, pool *pgxpool.Pool, id, decider int64, approve bool, note string) (Excuse, error) {
	if len([]rune(note)) > 1000 {
		return Excuse{}, &ValidationError{map[string]string{"note": "en fazla 1000 karakter"}}
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return Excuse{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM excuses WHERE id = $1 FOR UPDATE`, id).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Excuse{}, ErrNotFound
		}
		return Excuse{}, err
	}
	if status != "pending" {
		return Excuse{}, ErrConflict
	}
	next := "rejected"
	if approve {
		next = "approved"
	}
	var n any
	if note != "" {
		n = note
	}
	e, err := scanExcuse(tx.QueryRow(ctx, `UPDATE excuses SET status=$2, decided_by=$3, decided_at=now(), decision_note=$4 WHERE id=$1 RETURNING `+excuseCols,
		id, next, decider, n))
	if err != nil {
		return Excuse{}, err
	}
	if err := audit.Write(ctx, tx, audit.Entry{ActorID: decider, Action: "excuse.decide", Entity: "excuse", EntityID: fmt.Sprint(id),
		Old: map[string]any{"status": status}, New: map[string]any{"status": next, "note": note}}); err != nil {
		return Excuse{}, err
	}
	return e, tx.Commit(ctx)
}

// HasApprovedExcuse: öğrencinin bu sınav için onaylanmış mazereti var mı?
func HasApprovedExcuse(ctx context.Context, pool *pgxpool.Pool, studentID, examID int64) (bool, error) {
	var ok bool
	err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM excuses WHERE kind='student' AND student_id=$1 AND exam_id=$2 AND status='approved')`,
		studentID, examID).Scan(&ok)
	return ok, err
}
