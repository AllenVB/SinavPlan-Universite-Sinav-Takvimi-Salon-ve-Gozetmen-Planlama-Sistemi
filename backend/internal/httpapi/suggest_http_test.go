package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/AllenVB/sinavplan/backend/internal/simdata"
)

// Faz 22 B1: aynı gün iki sınavı olan öğrenci için öneri → 0 ihlal; ilk öneri taşıma ucuyla uygulanınca
// yeni taslakta öğrencinin o günü çözülmüş olur. Salt-okur: öneri isteği sürüm oluşturmaz.
func TestFixSuggestionsResolveStudentsSameDay(t *testing.T) {
	s, env, _, schedID := scheduledServer(t)
	ctx := context.Background()
	// Oturumların hepsi gelecekte olsun (başlamış oturumlar kilitlidir).
	if _, err := env.Owner.Pool.Exec(ctx, `UPDATE slots SET starts_at = starts_at - (SELECT min(starts_at) FROM slots) + now() + interval '1 day',
		ends_at = ends_at - (SELECT min(starts_at) FROM slots) + now() + interval '1 day'`); err != nil {
		t.Fatal(err)
	}
	sec, _ := login(t, s, simdata.EmailSecretary)
	aff, _ := login(t, s, simdata.EmailStudentAffairs)
	stu, _ := login(t, s, simdata.EmailStudent)
	sid := fmt.Sprint(schedID)

	_, cf := do(t, s, http.MethodGet, "/schedules/"+sid+"/conflicts?limit=1", sec, "", "")
	items, _ := cf["items"].([]any)
	if len(items) == 0 {
		t.Skip("bu fikstürde aynı gün iki sınavı olan öğrenci yok")
	}
	row := items[0].(map[string]any)
	student := int64(row["student_id"].(float64))
	day := int(row["day"].(float64))

	var versions int
	_ = env.Owner.Pool.QueryRow(ctx, `SELECT count(*) FROM schedules`).Scan(&versions)
	code, out := do(t, s, http.MethodGet, fmt.Sprintf("/schedules/%s/fix-suggestions?student_id=%d", sid, student), aff, "", "")
	if code != http.StatusOK || out["mode"] != "student" {
		t.Fatalf("öneri: %d %v", code, out)
	}
	sug, _ := out["items"].([]any)
	if len(sug) == 0 {
		t.Fatalf("öneri bekleniyordu: %v", out)
	}
	for _, it := range sug {
		g := it.(map[string]any)["suggestion"].(map[string]any)
		if g["violations"].(float64) != 0 {
			t.Fatalf("ihlalli öneri: %v", g)
		}
	}
	var after int
	_ = env.Owner.Pool.QueryRow(ctx, `SELECT count(*) FROM schedules`).Scan(&after)
	if after != versions {
		t.Fatalf("öneri isteği sürüm oluşturmamalı: %d → %d", versions, after)
	}

	first := sug[0].(map[string]any)["suggestion"].(map[string]any)
	code, mv := post(t, s, fmt.Sprintf("/schedules/%s/exams/%d/move", sid, int64(first["exam_id"].(float64))), sec, "", map[string]any{"slot_id": int64(first["to_slot_id"].(float64))})
	if code != http.StatusCreated {
		t.Fatalf("öneri uygulanamadı: %d %v", code, mv)
	}
	newID := fmt.Sprint(int64(mv["schedule_id"].(float64)))
	_, again := do(t, s, http.MethodGet, fmt.Sprintf("/schedules/%s/fix-suggestions?student_id=%d", newID, student), sec, "", "")
	days, ok := again["student_days"].([]any)
	if !ok {
		t.Fatalf("student_days dizi olmalı: %v", again["student_days"])
	}
	for _, d := range days {
		if int(d.(float64)) == day {
			t.Fatalf("yeni taslakta öğrencinin %d. günü hâlâ iki sınavlı", day)
		}
	}

	// yetki ve doğrulama
	if code, _ := do(t, s, http.MethodGet, "/schedules/"+sid+"/fix-suggestions", stu, "", ""); code != http.StatusForbidden {
		t.Fatalf("öğrenci 403 almalı: %d", code)
	}
	if code, _ := do(t, s, http.MethodGet, "/schedules/"+sid+"/fix-suggestions?student_id=999999", sec, "", ""); code != http.StatusNotFound {
		t.Fatalf("bilinmeyen öğrenci 404: %d", code)
	}
	if code, _ := do(t, s, http.MethodGet, "/schedules/"+sid+"/fix-suggestions?limit=11", sec, "", ""); code != http.StatusUnprocessableEntity {
		t.Fatalf("limit 11 → 422: %d", code)
	}
	if code, g := do(t, s, http.MethodGet, "/schedules/"+sid+"/fix-suggestions", sec, "", ""); code != http.StatusOK || g["mode"] != "global" {
		t.Fatalf("genel mod: %d %v", code, g)
	}
}
