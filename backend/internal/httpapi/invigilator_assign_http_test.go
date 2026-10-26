package httpapi

import (
	"context"
	"net/http"
	"testing"

	"github.com/AllenVB/sinavplan/backend/internal/simdata"
)

// Elle gözetmen ataması: kuralı bozan atama reddedilir (409, ihlaller alanlarda); uygun atama yazılır, eski gözetmen
// yerinden çıkar, ikisine bildirim gider; önizleme hiçbir şey değiştirmez; yalnız sekreter yapabilir.
func TestManualInvigilatorAssignment(t *testing.T) {
	f := checkinFixture(t)
	sec, _ := login(t, f.s, simdata.EmailSecretary)
	ctx := context.Background()
	db := f.env.Owner.Pool
	var sid, inv int64
	if err := db.QueryRow(ctx, `SELECT id FROM "wm-schedules" WHERE status = 'published'`).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	_ = db.QueryRow(ctx, `SELECT id FROM "wm-uo-users" WHERE email = $1`, simdata.EmailInvigilator).Scan(&inv)
	path := "/schedules/" + itoa(sid) + "/invigilators/assign"

	// Aday: aynı oturumda görevi olmayan, önceki/sonraki oturumda görevi olmayan ve mazeretsiz bir gözetmen.
	var cand int64
	if err := db.QueryRow(ctx, `
		SELECT u.id FROM "wm-uo-users" u JOIN "wm-user_roles" r ON r.user_id = u.id AND r.role = 'invigilator'
		WHERE u.id <> $3 AND NOT EXISTS (
			SELECT 1 FROM "wm-invigilator_assignments" ia JOIN "wm-slots" s ON s.id = ia.slot_id, "wm-slots" me
			WHERE me.id = $2 AND ia.schedule_id = $1 AND ia.user_id = u.id AND s.day_index = me.day_index
			  AND abs(s.session_index - me.session_index) <= 1)
		  AND NOT EXISTS (SELECT 1 FROM "wm-lecturers" l JOIN "wm-sections" sc ON sc.lecturer_id = l.id JOIN "wm-exams" e ON e.course_id = sc.course_id
			JOIN "wm-room_assignments" ra ON ra.exam_id = e.id AND ra.schedule_id = $1 AND ra.slot_id = $2 AND ra.room_id = $4 WHERE l.user_id = u.id)
		ORDER BY u.id LIMIT 1`, sid, f.slotID, inv, f.roomID).Scan(&cand); err != nil {
		t.Fatalf("aday gözetmen: %v", err)
	}
	// Kuralı bozan aday: önizlemesi ihlal döndüren ilk gözetmen (aynı/ardışık oturumda görevli, kendi dersi ya da mazeretli).
	rows, err := db.Query(ctx, `SELECT u.id FROM "wm-uo-users" u JOIN "wm-user_roles" r ON r.user_id = u.id AND r.role = 'invigilator' WHERE u.id <> $1 ORDER BY u.id`, inv)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	var busy int64
	var why []any
	for _, id := range ids {
		_, pv := do(t, f.s, http.MethodPost, path+"?dry_run=true", sec, "application/json", j(map[string]any{"slot_id": f.slotID, "room_id": f.roomID, "user_id": id, "replace_user_id": inv}))
		if pv["ok"] == false {
			busy, why = id, pv["violations"].([]any)
			break
		}
	}
	if busy == 0 || len(why) == 0 {
		t.Fatal("kuralı bozan aday bulunamadı (fikstür)")
	}
	count := func() (n int) {
		_ = db.QueryRow(ctx, `SELECT count(*) FROM "wm-invigilator_assignments" WHERE schedule_id = $1`, sid).Scan(&n)
		return
	}
	before := count()

	if code, _ := do(t, f.s, http.MethodPost, path, f.inv, "application/json", j(map[string]any{"slot_id": f.slotID, "room_id": f.roomID, "user_id": cand})); code != http.StatusForbidden {
		t.Fatalf("gözetmen elle atama yapamaz: %d", code)
	}
	code, out := do(t, f.s, http.MethodPost, path, sec, "application/json", j(map[string]any{"slot_id": f.slotID, "room_id": f.roomID, "user_id": busy, "replace_user_id": inv}), "Idempotency-Key", "as-1")
	if code != http.StatusConflict || out["error"].(map[string]any)["code"] != "invigilation_rules" {
		t.Fatalf("aynı oturumda iki görev reddedilmeli: %d %v", code, out)
	}
	if code, out := do(t, f.s, http.MethodPost, path+"?dry_run=true", sec, "application/json", j(map[string]any{"slot_id": f.slotID, "room_id": f.roomID, "user_id": cand, "replace_user_id": inv})); code != http.StatusOK || out["ok"] != true || out["dry_run"] != true {
		t.Fatalf("önizleme: %d %v", code, out)
	}
	var who int64
	_ = db.QueryRow(ctx, `SELECT count(*) FROM "wm-invigilator_assignments" WHERE schedule_id=$1 AND slot_id=$2 AND room_id=$3 AND user_id=$4`, sid, f.slotID, f.roomID, inv).Scan(&who)
	if who != 1 || count() != before {
		t.Fatal("önizleme veriyi değiştirmemeli")
	}
	code, out = do(t, f.s, http.MethodPost, path, sec, "application/json", j(map[string]any{"slot_id": f.slotID, "room_id": f.roomID, "user_id": cand, "replace_user_id": inv}), "Idempotency-Key", "as-2")
	if code != http.StatusOK || out["ok"] != true {
		t.Fatalf("yerine atama: %d %v", code, out)
	}
	var newWho, oldWho int
	_ = db.QueryRow(ctx, `SELECT count(*) FILTER (WHERE user_id=$4), count(*) FILTER (WHERE user_id=$5) FROM "wm-invigilator_assignments" WHERE schedule_id=$1 AND slot_id=$2 AND room_id=$3`,
		sid, f.slotID, f.roomID, cand, inv).Scan(&newWho, &oldWho)
	if newWho != 1 || oldWho != 0 || count() != before {
		t.Fatalf("yerine atama sonrası: yeni %d eski %d toplam %d/%d", newWho, oldWho, count(), before)
	}
	if items, _ := notifs(t, f, f.inv); len(ofKind(items, "duty_removed")) != 1 {
		t.Fatalf("eski gözetmene bildirim: %v", items)
	}
	var notified int
	_ = db.QueryRow(ctx, `SELECT count(*) FROM "wm-notifications" WHERE user_id = $1 AND kind = 'duty_assigned'`, cand).Scan(&notified)
	if notified != 1 {
		t.Fatalf("yeni gözetmene bildirim: %d", notified)
	}
	// Aynı gözetmeni aynı göreve ikinci kez eklemek 409; olmayan salon 404; eksik alan 422.
	if code, _ := do(t, f.s, http.MethodPost, path, sec, "application/json", j(map[string]any{"slot_id": f.slotID, "room_id": f.roomID, "user_id": cand}), "Idempotency-Key", "as-3"); code != http.StatusConflict {
		t.Fatalf("aynı göreve ikinci kez: %d", code)
	}
	if code, _ := do(t, f.s, http.MethodPost, path, sec, "application/json", j(map[string]any{"slot_id": f.slotID, "room_id": 999999, "user_id": cand}), "Idempotency-Key", "as-4"); code != http.StatusNotFound {
		t.Fatalf("görev olmayan salon 404: %d", code)
	}
	if code, _ := do(t, f.s, http.MethodPost, path, sec, "application/json", j(map[string]any{"slot_id": f.slotID}), "Idempotency-Key", "as-5"); code != http.StatusUnprocessableEntity {
		t.Fatalf("eksik alan 422: %d", code)
	}
	var au int
	_ = db.QueryRow(ctx, `SELECT count(*) FROM "wm-audit_log" WHERE action = 'invigilator.assign'`).Scan(&au)
	if au != 1 {
		t.Fatalf("denetim kaydı 1 olmalı (önizleme ve reddedilen yazılmaz): %d", au)
	}
}
