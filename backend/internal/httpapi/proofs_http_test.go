package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/AllenVB/sinavplan/backend/internal/simdata"
)

// Faz 22 A2: kanıtlar gerçek planda 0 ihlal bulur, bozulmuş sanal planda yakalar, hiçbir şey yazmaz;
// veritabanında GERÇEK bir bozulmayı da yakalar (ok=false). Öğrenci 403.
func TestProofsAreIndependentAndCatchRealCorruption(t *testing.T) {
	s, env, _, schedID := scheduledServer(t)
	ctx := context.Background()
	sec, _ := login(t, s, simdata.EmailSecretary)
	stu, _ := login(t, s, simdata.EmailStudent)
	path := fmt.Sprintf("/schedules/%d/proofs", schedID)
	counts := func() (n [3]int64) {
		_ = env.Owner.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM "wm-schedules"), (SELECT count(*) FROM "wm-seat_assignments"), (SELECT count(*) FROM "wm-audit_log")`).Scan(&n[0], &n[1], &n[2])
		return
	}
	before := counts()
	code, out := do(t, s, http.MethodGet, path, sec, "", "")
	if code != http.StatusOK {
		t.Fatalf("kanıtlar: %d %v", code, out)
	}
	items := out["items"].([]any)
	if len(items) < 12 {
		t.Fatalf("en az 12 denetim bekleniyordu: %d", len(items))
	}
	for _, it := range items {
		p := it.(map[string]any)
		if p["found"].(float64) != 0 {
			t.Fatalf("%s gerçek planda ihlal buldu: %v", p["id"], p)
		}
		if p["detail"] == nil || p["detail"] == "" || p["task"] == "T-5" {
			if p["caught"].(float64) < 1 || p["ok"] != true {
				t.Fatalf("%s bozulmuş planı yakalamadı: %v", p["id"], p)
			}
		}
	}
	if counts() != before {
		t.Fatalf("kanıtlar veri yazdı: %v → %v", before, counts())
	}

	// Gerçek bozulma: erişilebilir OLMAYAN koltuktaki bir öğrenciyi engelli işaretle → T-3c bulmalı.
	tag, err := env.Owner.Pool.Exec(ctx, `UPDATE "wm-students" SET disability_flag = true WHERE id = (
		SELECT a.student_id FROM "wm-seat_assignments" a JOIN "wm-room_seats" rs ON rs.id = a.seat_id WHERE a.schedule_id = $1 AND NOT rs.is_accessible ORDER BY a.id LIMIT 1)`, schedID)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("bozma: %v %d", err, tag.RowsAffected())
	}
	_, out = do(t, s, http.MethodGet, path, sec, "", "")
	for _, it := range out["items"].([]any) {
		p := it.(map[string]any)
		if p["id"] == "T-3c" && (p["found"].(float64) < 1 || p["ok"] != false) {
			t.Fatalf("T-3c gerçek bozulmayı yakalamadı: %v", p)
		}
	}
	if code, _ := do(t, s, http.MethodGet, path, stu, "", ""); code != http.StatusForbidden {
		t.Fatalf("öğrenci 403 almalı: %d", code)
	}
}
