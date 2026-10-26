package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/AllenVB/sinavplan/backend/internal/simdata"
)

// Faz 22 B2: "ya olursa?" önizlemesi. ?dry_run=true aynı işlemi koşar ve geri alır: veritabanında hiçbir
// şey değişmez (atamalar, koltuklar, denetim ve olay kayıtları); ardından gerçek uygulama önizlemeyle aynı.
func TestDryRunPreviewsChangeNothingAndMatchTheRealApply(t *testing.T) {
	s, env, _, schedID := scheduledServer(t)
	ctx := context.Background()
	sec, _ := login(t, s, simdata.EmailSecretary)
	sid := itoa(schedID)
	state := func() string {
		var x string
		if err := env.Owner.Pool.QueryRow(ctx, `SELECT md5(coalesce((SELECT string_agg(slot_id||':'||room_id||':'||user_id, ',' ORDER BY slot_id, room_id, user_id) FROM "wm-invigilator_assignments" WHERE schedule_id = $1), ''))
			|| md5(coalesce((SELECT string_agg(student_id||':'||room_id||':'||seat_id, ',' ORDER BY exam_id, student_id) FROM "wm-seat_assignments" WHERE schedule_id = $1), ''))
			|| (SELECT count(*) FROM "wm-audit_log") || '/' || (SELECT count(*) FROM "wm-room_change_events")`, schedID).Scan(&x); err != nil {
			t.Fatal(err)
		}
		return x
	}

	// 1) Gözetmen çıkarma
	_, duties := do(t, s, http.MethodGet, "/schedules/"+sid+"/invigilators?limit=1", sec, "", "")
	uid := int64(duties["items"].([]any)[0].(map[string]any)["user_id"].(float64))
	before := state()
	code, pv := do(t, s, http.MethodPost, fmt.Sprintf("/schedules/%s/invigilators/%d/remove?dry_run=true", sid, uid), sec, "", "")
	if code != http.StatusOK || pv["dry_run"] != true || pv["vacated"].(float64) < 1 {
		t.Fatalf("önizleme: %d %v", code, pv)
	}
	if pv["untouched"].(float64)+float64(len(pv["duties"].(map[string]any))) < pv["total"].(float64)-pv["vacated"].(float64) {
		t.Fatalf("dokunulmayan + etkilenen görev toplamı tutarsız: %v", pv)
	}
	if state() != before {
		t.Fatal("gözetmen önizlemesi veritabanını değiştirdi")
	}
	code, real := do(t, s, http.MethodPost, fmt.Sprintf("/schedules/%s/invigilators/%d/remove", sid, uid), sec, "", "", "Idempotency-Key", "pv-rm")
	if code != http.StatusOK || real["dry_run"] != false {
		t.Fatalf("uygulama: %d %v", code, real)
	}
	a, _ := json.Marshal(pv["changes"])
	b, _ := json.Marshal(real["changes"])
	if string(a) != string(b) || pv["vacated"] != real["vacated"] || pv["untouched"] != real["untouched"] {
		t.Fatalf("uygulama önizlemeden farklı:\n önizleme %s\n gerçek    %s", a, b)
	}
	if state() == before {
		t.Fatal("gerçek uygulama hiçbir şeyi değiştirmedi")
	}

	// 2) Tahliye: en az öğrencili (slot, salon) → o slotta kullanılmayan, yeterince büyük bir salon
	var slot, from, to int64
	if err := env.Owner.Pool.QueryRow(ctx, `WITH u AS (SELECT slot_id, room_id, count(*) n FROM "wm-seat_assignments" WHERE schedule_id = $1 GROUP BY 1, 2 ORDER BY n, 1, 2 LIMIT 1)
		SELECT u.slot_id, u.room_id, r.id FROM u JOIN "wm-rooms" r ON r.capacity >= u.n * 3 AND r.type = (SELECT type FROM "wm-rooms" WHERE id = u.room_id)
		WHERE r.id NOT IN (SELECT room_id FROM "wm-seat_assignments" WHERE schedule_id = $1 AND slot_id = u.slot_id) ORDER BY r.id LIMIT 1`, schedID).Scan(&slot, &from, &to); err != nil {
		t.Skipf("uygun tahliye çifti yok: %v", err)
	}
	body := map[string]any{"slot_id": slot, "from_room_id": from, "to_room_id": to, "reason": "önizleme testi"}
	before = state()
	code, ev := post(t, s, "/schedules/"+sid+"/room-change?dry_run=true", sec, "", body)
	if code != http.StatusOK || ev["dry_run"] != true || ev["moved"].(float64) < 1 || ev["room_change_event_id"].(float64) != 0 {
		t.Fatalf("tahliye önizlemesi: %d %v", code, ev)
	}
	if state() != before {
		t.Fatal("tahliye önizlemesi veritabanını değiştirdi")
	}
	code, done := post(t, s, "/schedules/"+sid+"/room-change", sec, "pv-rc", body)
	if code != http.StatusOK || done["moved"] != ev["moved"] || done["dry_run"] != false {
		t.Fatalf("tahliye uygulaması önizlemeden farklı: %d %v / %v", code, done, ev)
	}
}
