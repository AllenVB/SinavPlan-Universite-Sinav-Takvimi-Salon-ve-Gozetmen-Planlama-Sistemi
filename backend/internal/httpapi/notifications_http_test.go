package httpapi

import (
	"context"
	"net/http"
	"testing"

	"github.com/AllenVB/sinavplan/backend/internal/simdata"
)

func notifs(t *testing.T, f checkinFx, tok string) (items []map[string]any, unread int) {
	t.Helper()
	code, out := call(t, f.s, http.MethodGet, "/me/notifications", tok, nil)
	if code != http.StatusOK {
		t.Fatalf("/me/notifications: %d %v", code, out)
	}
	for _, x := range out["items"].([]any) {
		items = append(items, x.(map[string]any))
	}
	return items, int(out["unread"].(float64))
}

func ofKind(items []map[string]any, kind string) []map[string]any {
	var r []map[string]any
	for _, i := range items {
		if i["kind"] == kind {
			r = append(r, i)
		}
	}
	return r
}

// Gelmeyene bildirim (plan #4): sınav başlayalı 15 dk geçince ve yoklamada "geldi" yoksa bir kez; geldiyse hiç.
func TestAbsentNotificationOnlyAfterGraceAndOnlyOnce(t *testing.T) {
	f := checkinFixture(t) // sınav 10 dk önce başladı
	if items, _ := notifs(t, f, f.stu); len(ofKind(items, "absent")) != 0 {
		t.Fatalf("15 dk dolmadan 'gelmedi' bildirimi olmamalı: %v", items)
	}
	f.setSlot(t, "- interval '20 minutes'", "+ interval '100 minutes'")
	items, unread := notifs(t, f, f.stu)
	if got := ofKind(items, "absent"); len(got) != 1 || unread < 1 {
		t.Fatalf("20 dk sonra tam 1 'absent' bekleniyordu: %v unread=%d", items, unread)
	}
	if items, _ := notifs(t, f, f.stu); len(ofKind(items, "absent")) != 1 {
		t.Fatalf("ikinci çağrı yeni bildirim üretmemeli (idempotent): %v", items)
	}
	// Başka kullanıcı (gözetmen) öğrencinin bildirimini görmez ve okundu yapamaz.
	if items, _ := notifs(t, f, f.inv); len(ofKind(items, "absent")) != 0 {
		t.Fatal("gözetmen öğrencinin bildirimini görmemeli")
	}
	id := int64(ofKind(items, "absent")[0]["id"].(float64))
	if code, _ := call(t, f.s, http.MethodPost, "/me/notifications/"+itoa(id)+"/read", f.inv, nil); code != http.StatusNotFound {
		t.Fatalf("başkasının bildirimi okundu yapılamaz: %d", code)
	}
	if code, _ := call(t, f.s, http.MethodPost, "/me/notifications/"+itoa(id)+"/read", f.stu, nil); code != http.StatusOK {
		t.Fatalf("kendi bildirimi okundu yapılabilmeli: %d", code)
	}
	if code, _ := call(t, f.s, http.MethodPost, "/me/notifications/"+itoa(id)+"/read", f.stu, nil); code != http.StatusNotFound {
		t.Fatalf("zaten okunan ikinci kez 404: %d", code)
	}
	call(t, f.s, http.MethodPost, "/me/notifications/read-all", f.stu, nil)
	if _, unread := notifs(t, f, f.stu); unread != 0 {
		t.Fatalf("read-all sonrası okunmamış 0 olmalı: %d", unread)
	}
}

// Yoklamada "geldi" yazılan öğrenciye gelmedi bildirimi gitmez.
func TestNoAbsentNotificationWhenPresent(t *testing.T) {
	f := checkinFixture(t)
	f.setSlot(t, "- interval '20 minutes'", "+ interval '100 minutes'")
	f.s.qr.WithDemoCode("111111")
	if code, out := post(t, f.s, "/me/checkin", f.stu, "", map[string]string{"code": "111111"}); code != http.StatusOK {
		t.Fatalf("yoklama: %d %v", code, out)
	}
	if items, _ := notifs(t, f, f.stu); len(ofKind(items, "absent")) != 0 {
		t.Fatalf("yoklamada 'geldi' varsa gelmedi bildirimi olmamalı: %v", items)
	}
}

// Hatırlatma (plan #2): 24 saat içindeki sınav öğrenciye, görev gözetmene; uzak tarihli olana yok.
func TestReminderNotificationsForExamAndDuty(t *testing.T) {
	f := checkinFixture(t)
	f.setSlot(t, "+ interval '5 hours'", "+ interval '7 hours'")
	if items, _ := notifs(t, f, f.stu); len(ofKind(items, "exam_reminder")) < 1 {
		t.Fatalf("5 saat sonraki sınav için hatırlatma bekleniyordu: %v", items)
	}
	if items, _ := notifs(t, f, f.inv); len(ofKind(items, "duty_reminder")) < 1 {
		t.Fatalf("5 saat sonraki görev için hatırlatma bekleniyordu: %v", items)
	}
	g := checkinFixture(t)
	g.setSlot(t, "+ interval '60 hours'", "+ interval '62 hours'")
	if items, _ := notifs(t, g, g.stu); len(ofKind(items, "exam_reminder")) != 0 {
		t.Fatalf("60 saat sonrası için hatırlatma olmamalı: %v", items)
	}
}

// Olay bildirimi: mazeret kararı sahibine ulaşır.
func TestExcuseDecisionNotifiesOwner(t *testing.T) {
	f := checkinFixture(t)
	sec, _ := login(t, f.s, simdata.EmailSecretary)
	code, ex := do(t, f.s, http.MethodPost, "/excuses", f.stu, "application/json", j(map[string]any{"exam_id": f.examID, "reason": "Hastane raporum var"}))
	if code != http.StatusCreated {
		t.Fatalf("mazeret: %d %v", code, ex)
	}
	id := int64(ex["id"].(float64))
	if code, out := do(t, f.s, http.MethodPost, "/excuses/"+itoa(id)+"/decision", sec, "application/json", j(map[string]any{"decision": "approved", "note": "Rapor geçerli"}), "Idempotency-Key", "n-1"); code != http.StatusOK {
		t.Fatalf("karar: %d %v", code, out)
	}
	items, _ := notifs(t, f, f.stu)
	got := ofKind(items, "excuse_decision")
	if len(got) != 1 || got[0]["title"] != "Mazeretiniz onaylandı" {
		t.Fatalf("mazeret kararı bildirimi bekleniyordu: %v", items)
	}
}

// Gözetmen çıkarılınca görevleri devralanlara ve çıkarılana bildirim gider; önizlemede (dry_run) hiçbiri kalmaz.
func TestRemoveInvigilatorNotifiesAssignees(t *testing.T) {
	f := checkinFixture(t)
	sec, _ := login(t, f.s, simdata.EmailSecretary)
	ctx := context.Background()
	var sid, uid int64
	if err := f.env.Owner.Pool.QueryRow(ctx, `SELECT id FROM "wm-schedules" WHERE status = 'published'`).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	if err := f.env.Owner.Pool.QueryRow(ctx, `SELECT id FROM "wm-uo-users" WHERE email = $1`, simdata.EmailInvigilator).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	count := func() (n int) {
		_ = f.env.Owner.Pool.QueryRow(ctx, `SELECT count(*) FROM "wm-notifications" WHERE kind IN ('duty_assigned','duty_removed')`).Scan(&n)
		return
	}
	path := "/schedules/" + itoa(sid) + "/invigilators/" + itoa(uid) + "/remove"
	if code, out := call(t, f.s, http.MethodPost, path+"?dry_run=true", sec, nil); code != http.StatusOK || count() != 0 {
		t.Fatalf("önizleme bildirim bırakmamalı: %d %v n=%d", code, out, count())
	}
	if code, out := do(t, f.s, http.MethodPost, path, sec, "application/json", "{}", "Idempotency-Key", "rm-1"); code != http.StatusOK {
		t.Fatalf("çıkarma: %d %v", code, out)
	}
	if count() < 2 {
		t.Fatalf("çıkarılana ve devralanlara bildirim bekleniyordu, n=%d", count())
	}
	if items, _ := notifs(t, f, f.inv); len(ofKind(items, "duty_removed")) != 1 {
		t.Fatalf("çıkarılan gözetmen bilgilendirilmeli: %v", items)
	}
}
