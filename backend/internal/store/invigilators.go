package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

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
	Untouched  int                     `json:"untouched"` // dokunulmayan (kilitli kalan) atama sayısı
	Total      int                     `json:"total"`     // önceki toplam atama
	DryRun     bool                    `json:"dry_run"`   // true: önizleme; işlem geri alındı, hiçbir şey değişmedi
	// Görev ayrıntıları (önizleme/sonuç ekranı için): görev → oturum başlangıcı ve salon kodu.
	Duties map[int64]DutyInfo `json:"duties"`
}

// DutyInfo, bir görevin (oturum × salon) okunur karşılığı.
type DutyInfo struct {
	StartsAt time.Time `json:"starts_at"`
	RoomCode string    `json:"room_code"`
}

// RemoveInvigilator, hastalanan gözetmeni çizelgeden çıkarır; diğer atamalar KİLİTLİ,
// boşalan görevler minimum değişiklikle doldurulur (invigilation.Reassign). Her şey tek
// transaction'da: çizelge satırı kilitlenir, değişen görevlerin atamaları silinip yeniden
// yazılır, denetim kaydı eklenir.
// dryRun=true: aynı işlem eksiksiz koşar (kilitler, yeniden atama, denetim kaydı) ama sonunda GERİ ALINIR —
// önizleme, gerçek uygulamanın birebir aynısıdır, ayrı bir tahmin kodu yoktur.
func RemoveInvigilator(ctx context.Context, pool *pgxpool.Pool, scheduleID, userID, actor int64, dryRun bool) (RemoveInvigilatorResult, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return RemoveInvigilatorResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	st, err := loadInvigilation(ctx, tx, pool, scheduleID)
	if err != nil {
		return RemoveInvigilatorResult{}, err
	}
	if !st.isInvigilator(userID) {
		return RemoveInvigilatorResult{}, ErrNotFound
	}
	in, plan, refs := st.in, st.plan, st.refs

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
		if _, err := tx.Exec(ctx, `DELETE FROM "wm-invigilator_assignments" WHERE schedule_id=$1 AND slot_id=$2 AND room_id=$3`,
			scheduleID, r.slotID, r.roomID); err != nil {
			return RemoveInvigilatorResult{}, err
		}
	}
	for _, a := range res.Plan.Assignments {
		if !affected[a.DutyID] {
			continue
		}
		r := refs[a.DutyID]
		if _, err := tx.Exec(ctx, `INSERT INTO "wm-invigilator_assignments" (schedule_id, exam_id, slot_id, room_id, user_id) VALUES ($1,$2,$3,$4,$5)`,
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
	untouched := 0
	for _, a := range plan.Assignments {
		if !affected[a.DutyID] {
			untouched++
		}
	}
	out := RemoveInvigilatorResult{Vacated: res.Vacated, Changes: res.Changes, Swaps: res.Swaps,
		Unfilled: res.Plan.Unfilled, Violations: viol, Untouched: untouched, Total: len(plan.Assignments), DryRun: dryRun,
		Duties: map[int64]DutyInfo{}}
	for d := range affected {
		r := refs[d]
		var di DutyInfo
		if err := tx.QueryRow(ctx, `SELECT sl.starts_at, ro.code FROM "wm-slots" sl, "wm-rooms" ro WHERE sl.id = $1 AND ro.id = $2`, r.slotID, r.roomID).Scan(&di.StartsAt, &di.RoomCode); err != nil {
			return RemoveInvigilatorResult{}, err
		}
		out.Duties[d] = di
	}
	// Bildirimler: yeni görev alan gözetmenler ve görevleri devredilen gözetmen (önizlemede işlem geri alındığı için kalmaz).
	for _, c := range res.Changes {
		di := out.Duties[c.DutyID]
		if err := Notify(ctx, tx, c.To, "duty_assigned", "Yeni gözetmenlik göreviniz var",
			fmt.Sprintf("%s · %s salonu. Bir gözetmenin çizelgeden çıkarılması nedeniyle size atandı.", clock(di.StartsAt), di.RoomCode),
			"#/yoklama", fmt.Sprintf("duty:%d:%d:%d", scheduleID, c.DutyID, c.To)); err != nil {
			return RemoveInvigilatorResult{}, err
		}
	}
	if res.Vacated > 0 {
		if err := Notify(ctx, tx, userID, "duty_removed", "Gözetmenlik görevleriniz güncellendi",
			fmt.Sprintf("%d göreviniz başka gözetmenlere devredildi.", res.Vacated), "#/yoklama", fmt.Sprintf("duty-removed:%d:%d", scheduleID, userID)); err != nil {
			return RemoveInvigilatorResult{}, err
		}
	}
	if out.Changes == nil {
		out.Changes = []invigilation.Change{}
	}
	if out.Unfilled == nil {
		out.Unfilled = []invigilation.Unfilled{}
	}
	if dryRun {
		return out, nil // ertelenmiş Rollback: hiçbir şey kalıcı olmaz
	}
	return out, tx.Commit(ctx)
}

// dutyRef, bir görevin (slot, salon) veritabanı karşılığı; anchor = salonun ilk sınavı (atama satırının exam_id'si).
type dutyRef struct {
	slotID, roomID, anchor int64
}

// invState, bir çizelgenin gözetmen problemi: görevler, gözetmenler (kendi dersleri, mazeretleri) ve mevcut plan.
type invState struct {
	in      invigilation.Input
	plan    invigilation.Plan
	refs    map[int64]dutyRef
	slotIdx map[int64]int
}

func (st *invState) isInvigilator(id int64) bool {
	for _, v := range st.in.Invigilators {
		if v.ID == id {
			return true
		}
	}
	return false
}

// loadInvigilation, çizelge satırını kilitler (FOR UPDATE) ve gözetmen problemini işlem içinden kurar.
func loadInvigilation(ctx context.Context, tx pgx.Tx, pool *pgxpool.Pool, scheduleID int64) (*invState, error) {
	var period int64
	if err := tx.QueryRow(ctx, `SELECT period_id FROM "wm-schedules" WHERE id = $1 FOR UPDATE`, scheduleID).Scan(&period); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	ds, err := LoadDataset(ctx, pool, period, 0)
	if err != nil {
		return nil, err
	}
	excused, err := ExcusedSlots(ctx, pool, ds)
	if err != nil {
		return nil, err
	}
	slotIdx := map[int64]int{}
	for i, s := range ds.Slots {
		slotIdx[s.ID] = i
	}

	// Görevler: (slot, salon) -> öğrenci sayısı ve sınavlar.
	in := invigilation.Input{SessionsPerDay: ds.Period.SessionsPerDay}
	rows, err := tx.Query(ctx, `SELECT slot_id, room_id, sum(seat_count)::int, array_agg(exam_id ORDER BY exam_id)
		FROM "wm-room_assignments" WHERE schedule_id = $1 GROUP BY slot_id, room_id ORDER BY slot_id, room_id`, scheduleID)
	if err != nil {
		return nil, err
	}
	refs := map[int64]dutyRef{}
	for rows.Next() {
		var slot, room int64
		var n int
		var exams []int64
		if err := rows.Scan(&slot, &room, &n, &exams); err != nil {
			rows.Close()
			return nil, err
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
	for _, u := range ds.Users {
		for _, r := range u.Roles {
			if r == "invigilator" {
				iv := invigilation.Invigilator{ID: u.ID, OwnExams: own[u.ID], Unavailable: excused[u.ID]}
				sort.Slice(iv.OwnExams, func(a, b int) bool { return iv.OwnExams[a] < iv.OwnExams[b] })
				in.Invigilators = append(in.Invigilators, iv)
			}
		}
	}
	var plan invigilation.Plan
	rows, err = tx.Query(ctx, `SELECT slot_id, room_id, user_id FROM "wm-invigilator_assignments" WHERE schedule_id = $1`, scheduleID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var slot, room, u int64
		if err := rows.Scan(&slot, &room, &u); err != nil {
			rows.Close()
			return nil, err
		}
		plan.Assignments = append(plan.Assignments, invigilation.Assignment{DutyID: dutyKey(slotIdx[slot], room), InvigilatorID: u})
	}
	rows.Close()

	return &invState{in: in, plan: plan, refs: refs, slotIdx: slotIdx}, nil
}
