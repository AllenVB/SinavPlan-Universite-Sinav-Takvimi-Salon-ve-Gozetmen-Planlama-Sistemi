package store

import (
	"context"
	"fmt"
	"time"

	"github.com/AllenVB/sinavplan/backend/internal/audit"
	"github.com/AllenVB/sinavplan/backend/internal/domain/invigilation"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AssignInvigilatorRequest, elle gözetmen ataması: bir görevi (oturum × salon) belirli bir gözetmene verir.
// ReplaceUserID doluysa o gözetmenin bu görevdeki yeri yeni gözetmene geçer; boşsa görev için ek gözetmen eklenir.
type AssignInvigilatorRequest struct {
	ScheduleID, SlotID, RoomID, UserID, ReplaceUserID, ActorID int64
	DryRun                                                     bool
}

// AssignInvigilatorResult, elle atamanın sonucu. Violations boş değilse hiçbir şey yazılmaz.
type AssignInvigilatorResult struct {
	OK         bool                     `json:"ok"` // kurallara uygun (dry_run değilse yazıldı)
	DryRun     bool                     `json:"dry_run"`
	Violations []invigilation.Violation `json:"violations"`
	Duty       DutyInfo                 `json:"duty"`
	// Adalet bilgisi (engel değildir): gözetmen yük farkı önce/sonra ve atanan gözetmenin yeni görev sayısı.
	SpreadBefore int `json:"spread_before"`
	SpreadAfter  int `json:"spread_after"`
	UserDuties   int `json:"user_duties"`
}

// AssignInvigilator, elle atamayı T-4 kurallarıyla (kendi dersi yok, mazeretli oturum yok, aynı oturumda iki görev yok,
// aynı gün ardışık oturum yok) bağımsız doğrulayıcıdan geçirir; YENİ ihlal doğurursa yazmaz ve ihlalleri döner.
// Uygunsa tek işlemde yazar, denetim kaydı ve bildirimleri ekler. DryRun: aynı yol koşar, sonunda geri alınır.
func AssignInvigilator(ctx context.Context, pool *pgxpool.Pool, req AssignInvigilatorRequest) (AssignInvigilatorResult, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return AssignInvigilatorResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	st, err := loadInvigilation(ctx, tx, pool, req.ScheduleID)
	if err != nil {
		return AssignInvigilatorResult{}, err
	}
	si, ok := st.slotIdx[req.SlotID]
	duty := dutyKey(si, req.RoomID)
	ref, dok := st.refs[duty]
	if !ok || !dok {
		return AssignInvigilatorResult{}, fmt.Errorf("%w: bu oturumda bu salonda sınav yok", ErrNotFound)
	}
	if !st.isInvigilator(req.UserID) || (req.ReplaceUserID != 0 && !st.isInvigilator(req.ReplaceUserID)) {
		return AssignInvigilatorResult{}, fmt.Errorf("%w: gözetmen bulunamadı", ErrNotFound)
	}
	if req.UserID == req.ReplaceUserID {
		return AssignInvigilatorResult{}, &ValidationError{map[string]string{"user_id": "yeni gözetmen eskisiyle aynı"}}
	}

	next := invigilation.Plan{}
	replaced := false
	for _, a := range st.plan.Assignments {
		if a.DutyID == duty && a.InvigilatorID == req.UserID {
			return AssignInvigilatorResult{}, fmt.Errorf("%w: gözetmen bu görevde zaten var", ErrConflict)
		}
		if req.ReplaceUserID != 0 && a.DutyID == duty && a.InvigilatorID == req.ReplaceUserID {
			a.InvigilatorID, replaced = req.UserID, true
		}
		next.Assignments = append(next.Assignments, a)
	}
	if req.ReplaceUserID != 0 && !replaced {
		return AssignInvigilatorResult{}, fmt.Errorf("%w: değiştirilecek gözetmen bu görevde değil", ErrNotFound)
	}
	if req.ReplaceUserID == 0 {
		next.Assignments = append(next.Assignments, invigilation.Assignment{DutyID: duty, InvigilatorID: req.UserID})
	}

	// Yalnız bu atamanın doğurduğu ihlaller sayılır (planın önceden taşıdığı ihlaller elle atamayı engellemez).
	old := map[string]bool{}
	for _, v := range invigilation.Validate(st.in, st.plan) {
		old[fmt.Sprint(v.Kind, v.DutyID, v.InvigilatorID)] = true
	}
	res := AssignInvigilatorResult{DryRun: req.DryRun, Violations: []invigilation.Violation{},
		SpreadBefore: spread(st.in, st.plan), SpreadAfter: spread(st.in, next)}
	for _, v := range invigilation.Validate(st.in, next) {
		if v.Kind != "understaffed" && !old[fmt.Sprint(v.Kind, v.DutyID, v.InvigilatorID)] {
			if d, ok := plainViolation[v.Kind]; ok {
				v.Detail = d
			}
			res.Violations = append(res.Violations, v)
		}
	}
	for _, a := range next.Assignments {
		if a.InvigilatorID == req.UserID {
			res.UserDuties++
		}
	}
	if err := tx.QueryRow(ctx, `SELECT sl.starts_at, ro.code FROM "wm-slots" sl, "wm-rooms" ro WHERE sl.id = $1 AND ro.id = $2`,
		ref.slotID, ref.roomID).Scan(&res.Duty.StartsAt, &res.Duty.RoomCode); err != nil {
		return AssignInvigilatorResult{}, err
	}
	if len(res.Violations) > 0 {
		return res, nil
	}

	if req.ReplaceUserID != 0 {
		if _, err := tx.Exec(ctx, `UPDATE "wm-invigilator_assignments" SET user_id = $5
			WHERE schedule_id = $1 AND slot_id = $2 AND room_id = $3 AND user_id = $4`,
			req.ScheduleID, ref.slotID, ref.roomID, req.ReplaceUserID, req.UserID); err != nil {
			return AssignInvigilatorResult{}, err
		}
	} else if _, err := tx.Exec(ctx, `INSERT INTO "wm-invigilator_assignments" (schedule_id, exam_id, slot_id, room_id, user_id) VALUES ($1,$2,$3,$4,$5)`,
		req.ScheduleID, ref.anchor, ref.slotID, ref.roomID, req.UserID); err != nil {
		return AssignInvigilatorResult{}, err
	}
	if err := audit.Write(ctx, tx, audit.Entry{ActorID: req.ActorID, Action: "invigilator.assign", Entity: "schedule", EntityID: fmt.Sprint(req.ScheduleID),
		Old: map[string]any{"slot_id": ref.slotID, "room_id": ref.roomID, "user_id": req.ReplaceUserID},
		New: map[string]any{"slot_id": ref.slotID, "room_id": ref.roomID, "user_id": req.UserID, "manual": true}}); err != nil {
		return AssignInvigilatorResult{}, err
	}
	stamp := time.Now().UnixNano()
	when := fmt.Sprintf("%s · %s salonu", clock(res.Duty.StartsAt), res.Duty.RoomCode)
	if err := Notify(ctx, tx, req.UserID, "duty_assigned", "Yeni gözetmenlik göreviniz var", when+". Sekreter tarafından atandı.",
		"#/yoklama", fmt.Sprintf("duty-manual:%d:%d:%d", req.ScheduleID, duty, stamp)); err != nil {
		return AssignInvigilatorResult{}, err
	}
	if req.ReplaceUserID != 0 {
		if err := Notify(ctx, tx, req.ReplaceUserID, "duty_removed", "Gözetmenlik göreviniz değişti", when+" görevi başka bir gözetmene verildi.",
			"#/yoklama", fmt.Sprintf("duty-manual-rm:%d:%d:%d", req.ScheduleID, duty, stamp)); err != nil {
			return AssignInvigilatorResult{}, err
		}
	}
	res.OK = true
	if req.DryRun {
		return res, nil // ertelenmiş Rollback: hiçbir şey kalıcı olmaz
	}
	return res, tx.Commit(ctx)
}

// plainViolation: doğrulayıcının teknik açıklamaları (slot indeksi, sınav kimliği) yerine sekreterin okuyacağı metin.
var plainViolation = map[string]string{
	"own_exam":            "bu salonda gözetmenin kendi verdiği dersin sınavı var",
	"excused":             "gözetmenin bu oturum için onaylı mazereti var",
	"double_slot":         "gözetmenin aynı oturumda başka bir salonda görevi var",
	"consecutive_session": "gözetmenin aynı gün bir önceki ya da sonraki oturumda görevi var",
	"duplicate":           "gözetmen bu görevde zaten var",
}

// spread, gözetmenler arasındaki en çok – en az görev farkı (görevsiz gözetmen 0 sayılır).
func spread(in invigilation.Input, p invigilation.Plan) int {
	load := map[int64]int{}
	for _, v := range in.Invigilators {
		load[v.ID] = 0
	}
	for _, a := range p.Assignments {
		load[a.InvigilatorID]++
	}
	lo, hi, first := 0, 0, true
	for _, n := range load {
		if first {
			lo, hi, first = n, n, false
		}
		lo, hi = min(lo, n), max(hi, n)
	}
	return hi - lo
}
