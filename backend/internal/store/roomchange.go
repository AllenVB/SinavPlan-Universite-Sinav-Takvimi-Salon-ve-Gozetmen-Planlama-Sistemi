package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/AllenVB/sinavplan/backend/internal/audit"
	"github.com/AllenVB/sinavplan/backend/internal/domain/seating"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrCapacity, hedef salonda yeterli uygun koltuk yoksa döner.
var ErrCapacity = errors.New("hedef salonda yeterli uygun koltuk yok")

// RoomChangeRequest, bir slotta bir salonun tüm öğrencilerinin başka salona taşınması.
type RoomChangeRequest struct {
	ScheduleID, SlotID, FromRoomID, ToRoomID int64
	Reason                                   string
	ActorID                                  int64
	Strict                                   bool
}

// RoomChangeResult, taşıma sonucu.
type RoomChangeResult struct {
	EventID int64 `json:"room_change_event_id"`
	Moved   int   `json:"moved"`
}

// RoomChange, tahliyeyi TEK transaction'da yapar:
//
//  1. İki salon satırı kimlik sırasıyla FOR UPDATE kilitlenir: aynı salonlara dokunan
//     eşzamanlı iki tahliye sıraya girer (aynı hedefe iki kaynak taşınırken ikisi de
//     "yer var" görüp hedefi aşamaz). Sabit sıra kilitlenmeyi önler.
//  2. Kaynak ve hedefteki oturma atamaları FOR UPDATE kilitlenir.
//  3. Hedefin kapasitesi, mevcut öğrenciler yerinde kalarak ve komşuluk kuralı (T-3)
//     uygulanarak seating.ArrangeInto ile denetlenir; sığmazsa hiçbir şey değişmez (ErrCapacity).
//  4. Koltuklar, salon payları ve gözetmenler güncellenir; room_change_events ve
//     audit_log aynı transaction'da yazılır.
func RoomChange(ctx context.Context, pool *pgxpool.Pool, req RoomChangeRequest) (RoomChangeResult, error) {
	if req.FromRoomID == req.ToRoomID {
		return RoomChangeResult{}, fmt.Errorf("kaynak ve hedef salon aynı olamaz")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return RoomChangeResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	lo, hi := min(req.FromRoomID, req.ToRoomID), max(req.FromRoomID, req.ToRoomID)
	rows, err := tx.Query(ctx, `SELECT id FROM rooms WHERE id IN ($1, $2) ORDER BY id FOR UPDATE`, lo, hi)
	if err != nil {
		return RoomChangeResult{}, err
	}
	n := 0
	for rows.Next() {
		n++
	}
	rows.Close()
	if n != 2 {
		return RoomChangeResult{}, ErrNotFound
	}

	type moving struct {
		id, exam, student int64
		disabled          bool
	}
	rows, err = tx.Query(ctx, `
		SELECT sa.id, sa.exam_id, sa.student_id, st.disability_flag
		FROM seat_assignments sa JOIN students st ON st.id = sa.student_id
		WHERE sa.schedule_id = $1 AND sa.slot_id = $2 AND sa.room_id = $3
		ORDER BY sa.exam_id, sa.student_id
		FOR UPDATE OF sa`, req.ScheduleID, req.SlotID, req.FromRoomID)
	if err != nil {
		return RoomChangeResult{}, err
	}
	var mv []moving
	for rows.Next() {
		var m moving
		if err := rows.Scan(&m.id, &m.exam, &m.student, &m.disabled); err != nil {
			rows.Close()
			return RoomChangeResult{}, err
		}
		mv = append(mv, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return RoomChangeResult{}, err
	}
	if len(mv) == 0 {
		return RoomChangeResult{}, ErrNotFound
	}

	// Hedef salonun koltukları ve mevcut öğrencileri.
	target := seating.Room{ID: req.ToRoomID}
	rows, err = tx.Query(ctx, `SELECT id, row_no, col_no, is_active, is_accessible FROM room_seats WHERE room_id = $1`, req.ToRoomID)
	if err != nil {
		return RoomChangeResult{}, err
	}
	for rows.Next() {
		var s seating.Seat
		var r, c int16
		if err := rows.Scan(&s.ID, &r, &c, &s.Active, &s.Accessible); err != nil {
			rows.Close()
			return RoomChangeResult{}, err
		}
		s.Row, s.Col = int(r), int(c)
		target.Seats = append(target.Seats, s)
	}
	rows.Close()
	rows, err = tx.Query(ctx, `
		SELECT sa.exam_id, sa.student_id, sa.seat_id, rs.row_no, rs.col_no
		FROM seat_assignments sa JOIN room_seats rs ON rs.id = sa.seat_id
		WHERE sa.schedule_id = $1 AND sa.slot_id = $2 AND sa.room_id = $3
		FOR UPDATE OF sa`, req.ScheduleID, req.SlotID, req.ToRoomID)
	if err != nil {
		return RoomChangeResult{}, err
	}
	var occupied []seating.Assignment
	for rows.Next() {
		var a seating.Assignment
		var r, c int16
		if err := rows.Scan(&a.ExamID, &a.StudentID, &a.SeatID, &r, &c); err != nil {
			rows.Close()
			return RoomChangeResult{}, err
		}
		a.Row, a.Col = int(r), int(c)
		occupied = append(occupied, a)
	}
	rows.Close()

	var groups []seating.Group
	idx := map[int64]int{}
	for _, m := range mv {
		i, ok := idx[m.exam]
		if !ok {
			i = len(groups)
			idx[m.exam] = i
			groups = append(groups, seating.Group{ExamID: m.exam})
		}
		groups[i].Students = append(groups[i].Students, seating.Student{ID: m.student, Disabled: m.disabled})
	}
	plan := seating.ArrangeInto(target, occupied, groups, req.Strict)
	if len(plan.Unseated) > 0 {
		return RoomChangeResult{}, fmt.Errorf("%w: %d öğrenciden %d'i sığmıyor", ErrCapacity, len(mv), len(plan.Unseated))
	}

	newSeat := map[[2]int64]int64{}
	for _, a := range plan.Assignments {
		newSeat[[2]int64{a.ExamID, a.StudentID}] = a.SeatID
	}
	ids := make([]int64, len(mv))
	seats := make([]int64, len(mv))
	perExam := map[int64]int{}
	for i, m := range mv {
		ids[i], seats[i] = m.id, newSeat[[2]int64{m.exam, m.student}]
		perExam[m.exam]++
	}
	if _, err := tx.Exec(ctx, `
		UPDATE seat_assignments sa SET room_id = $1, seat_id = u.seat_id
		FROM unnest($2::bigint[], $3::bigint[]) AS u(id, seat_id) WHERE sa.id = u.id`, req.ToRoomID, ids, seats); err != nil {
		return RoomChangeResult{}, fmt.Errorf("koltuklar taşınamadı: %w", err)
	}
	for exam, c := range perExam {
		if _, err := tx.Exec(ctx, `DELETE FROM room_assignments WHERE schedule_id = $1 AND exam_id = $2 AND room_id = $3`,
			req.ScheduleID, exam, req.FromRoomID); err != nil {
			return RoomChangeResult{}, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO room_assignments (schedule_id, exam_id, slot_id, room_id, seat_count) VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT (schedule_id, exam_id, room_id) DO UPDATE SET seat_count = room_assignments.seat_count + EXCLUDED.seat_count`,
			req.ScheduleID, exam, req.SlotID, req.ToRoomID, c); err != nil {
			return RoomChangeResult{}, err
		}
	}
	// Gözetmenler öğrencileriyle birlikte hedef salona geçer.
	if _, err := tx.Exec(ctx, `UPDATE invigilator_assignments SET room_id = $1
		WHERE schedule_id = $2 AND slot_id = $3 AND room_id = $4`, req.ToRoomID, req.ScheduleID, req.SlotID, req.FromRoomID); err != nil {
		return RoomChangeResult{}, err
	}

	var actor any
	if req.ActorID != 0 {
		actor = req.ActorID
	}
	var res RoomChangeResult
	if err := tx.QueryRow(ctx, `INSERT INTO room_change_events (schedule_id, slot_id, from_room_id, to_room_id, reason, moved_count, actor_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		req.ScheduleID, req.SlotID, req.FromRoomID, req.ToRoomID, req.Reason, len(mv), actor).Scan(&res.EventID); err != nil {
		return RoomChangeResult{}, err
	}
	res.Moved = len(mv)
	if err := audit.Write(ctx, tx, audit.Entry{ActorID: req.ActorID, Action: "room.change", Entity: "room_change_event",
		EntityID: fmt.Sprint(res.EventID),
		Old:      map[string]any{"room_id": req.FromRoomID, "slot_id": req.SlotID, "students": len(mv)},
		New:      map[string]any{"room_id": req.ToRoomID, "reason": req.Reason}}); err != nil {
		return RoomChangeResult{}, err
	}
	return res, tx.Commit(ctx)
}
