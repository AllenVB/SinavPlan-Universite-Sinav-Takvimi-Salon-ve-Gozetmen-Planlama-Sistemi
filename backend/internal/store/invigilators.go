package store

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/AllenVB/sinavplan/backend/internal/audit"
	"github.com/AllenVB/sinavplan/backend/internal/domain/invigilation"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// dutyKey, görev kimliği: slot indeksi*100000 + salon (planning ile aynı kural).
func dutyKey(slotIdx int, room int64) int64 { return int64(slotIdx)*100000 + room }

// RemoveInvigilatorResult, gözetmen çıkarma sonucu.
type RemoveInvigilatorResult struct {
	Vacated    int                     `json:"vacated"`
	Changes    []invigilation.Change   `json:"changes"`
	Swaps      int                     `json:"swaps"`
	Unfilled   []invigilation.Unfilled `json:"unfilled"`
	Violations int                     `json:"violations"`
}

// RemoveInvigilator, hastalanan gözetmeni çizelgeden çıkarır; diğer atamalar KİLİTLİ,
// boşalan görevler minimum değişiklikle doldurulur (invigilation.Reassign). Her şey tek
// transaction'da: çizelge satırı kilitlenir, değişen görevlerin atamaları silinip yeniden
// yazılır, denetim kaydı eklenir.
func RemoveInvigilator(ctx context.Context, pool *pgxpool.Pool, scheduleID, userID, actor int64) (RemoveInvigilatorResult, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return RemoveInvigilatorResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var period int64
	if err := tx.QueryRow(ctx, `SELECT period_id FROM schedules WHERE id = $1 FOR UPDATE`, scheduleID).Scan(&period); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RemoveInvigilatorResult{}, ErrNotFound
		}
		return RemoveInvigilatorResult{}, err
	}
	ds, err := LoadDataset(ctx, pool, period, 0)
	if err != nil {
		return RemoveInvigilatorResult{}, err
	}
	excused, err := ExcusedSlots(ctx, pool, ds)
	if err != nil {
		return RemoveInvigilatorResult{}, err
	}
	slotIdx := map[int64]int{}
	for i, s := range ds.Slots {
		slotIdx[s.ID] = i
	}

	// Görevler: (slot, salon) -> öğrenci sayısı ve sınavlar.
	in := invigilation.Input{SessionsPerDay: ds.Period.SessionsPerDay}
	rows, err := tx.Query(ctx, `SELECT slot_id, room_id, sum(seat_count)::int, array_agg(exam_id ORDER BY exam_id)
		FROM room_assignments WHERE schedule_id = $1 GROUP BY slot_id, room_id ORDER BY slot_id, room_id`, scheduleID)
	if err != nil {
		return RemoveInvigilatorResult{}, err
	}
	type dutyRef struct {
		slotID, roomID, anchor int64
	}
	refs := map[int64]dutyRef{}
	for rows.Next() {
		var slot, room int64
		var n int
		var exams []int64
		if err := rows.Scan(&slot, &room, &n, &exams); err != nil {
			rows.Close()
			return RemoveInvigilatorResult{}, err
		}
		id := dutyKey(slotIdx[slot], room)
		in.Duties = append(in.Duties, invigilation.Duty{ID: id, Slot: slotIdx[slot], RoomID: room, Students: n, Exams: exams})
		refs[id] = dutyRef{slot, room, exams[0]}
	}
	rows.Close()

	// Gözetmenler: invigilator rolü; kendi dersleri; onaylı mazeretleri.
	examOfCourse := map[int64]int64{}
	for _, e := range ds.Exams {
		examOfCourse[e.CourseID] = e.ID
	}
	lecUser := map[int64]int64{}
	for _, l := range ds.Lecturers {
		lecUser[l.ID] = l.UserID
	}
	own := map[int64][]int64{}
	for _, sec := range ds.Sections {
		if ex, ok := examOfCourse[sec.CourseID]; ok {
			u := lecUser[sec.LecturerID]
			own[u] = append(own[u], ex)
		}
	}
	found := false
	for _, u := range ds.Users {
		for _, r := range u.Roles {
			if r == "invigilator" {
				iv := invigilation.Invigilator{ID: u.ID, OwnExams: own[u.ID], Unavailable: excused[u.ID]}
				sort.Slice(iv.OwnExams, func(a, b int) bool { return iv.OwnExams[a] < iv.OwnExams[b] })
				in.Invigilators = append(in.Invigilators, iv)
				found = found || u.ID == userID
			}
		}
	}
	if !found {
		return RemoveInvigilatorResult{}, ErrNotFound
	}

	var plan invigilation.Plan
	rows, err = tx.Query(ctx, `SELECT slot_id, room_id, user_id FROM invigilator_assignments WHERE schedule_id = $1`, scheduleID)
	if err != nil {
		return RemoveInvigilatorResult{}, err
	}
	for rows.Next() {
		var slot, room, u int64
		if err := rows.Scan(&slot, &room, &u); err != nil {
			rows.Close()
			return RemoveInvigilatorResult{}, err
		}
		plan.Assignments = append(plan.Assignments, invigilation.Assignment{DutyID: dutyKey(slotIdx[slot], room), InvigilatorID: u})
	}
	rows.Close()

	res, err := invigilation.Reassign(in, plan, userID)
	if err != nil {
		return RemoveInvigilatorResult{}, err
	}

	// Etkilenen görevler: değişen + çıkarılanın tüm görevleri. Bu görevlerin atamaları
	// silinip sonuç plandan yeniden yazılır (anahtar çakışması ara adımları olmaz).
	affected := map[int64]bool{}
	for _, a := range plan.Assignments {
		if a.InvigilatorID == userID {
			affected[a.DutyID] = true
		}
	}
	for _, c := range res.Changes {
		affected[c.DutyID] = true
	}
	for d := range affected {
		r := refs[d]
		if _, err := tx.Exec(ctx, `DELETE FROM invigilator_assignments WHERE schedule_id=$1 AND slot_id=$2 AND room_id=$3`,
			scheduleID, r.slotID, r.roomID); err != nil {
			return RemoveInvigilatorResult{}, err
		}
	}
	for _, a := range res.Plan.Assignments {
		if !affected[a.DutyID] {
			continue
		}
		r := refs[a.DutyID]
		if _, err := tx.Exec(ctx, `INSERT INTO invigilator_assignments (schedule_id, exam_id, slot_id, room_id, user_id) VALUES ($1,$2,$3,$4,$5)`,
			scheduleID, r.anchor, r.slotID, r.roomID, a.InvigilatorID); err != nil {
			return RemoveInvigilatorResult{}, fmt.Errorf("atama yazılamadı: %w", err)
		}
	}
	viol := 0
	for _, v := range invigilation.Validate(in, res.Plan) {
		if v.Kind != "understaffed" {
			viol++
		}
	}
	if err := audit.Write(ctx, tx, audit.Entry{ActorID: actor, Action: "invigilator.remove", Entity: "schedule",
		EntityID: fmt.Sprint(scheduleID), Old: map[string]any{"invigilator": userID, "duties": res.Vacated},
		New: map[string]any{"changes": len(res.Changes), "swaps": res.Swaps, "unfilled": len(res.Plan.Unfilled)}}); err != nil {
		return RemoveInvigilatorResult{}, err
	}
	out := RemoveInvigilatorResult{Vacated: res.Vacated, Changes: res.Changes, Swaps: res.Swaps,
		Unfilled: res.Plan.Unfilled, Violations: viol}
	if out.Changes == nil {
		out.Changes = []invigilation.Change{}
	}
	if out.Unfilled == nil {
		out.Unfilled = []invigilation.Unfilled{}
	}
	return out, tx.Commit(ctx)
}
