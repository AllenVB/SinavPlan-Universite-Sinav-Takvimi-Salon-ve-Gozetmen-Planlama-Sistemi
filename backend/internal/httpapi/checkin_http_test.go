package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AllenVB/sinavplan/backend/internal/auth"
	"github.com/AllenVB/sinavplan/backend/internal/simdata"
	"github.com/AllenVB/sinavplan/backend/internal/store"
	"github.com/AllenVB/sinavplan/backend/internal/testdb"
)

type checkinFx struct {
	s                      *Server
	env                    *testdb.Env
	stu, inv               string
	slotID, roomID, examID int64
}

// checkinFixture: öğrenci 1'in sınavını "şimdi" yapar ve o salon/slot görevini gözetmen hesabına verir.
func checkinFixture(t *testing.T) checkinFx {
	t.Helper()
	srv, e, sp, _ := scheduledServer(t)
	ctx := context.Background()
	f := checkinFx{s: srv, env: e}
	for _, st := range sp.Seats {
		if st.StudentID == 1 {
			f.examID, f.roomID = st.ExamID, st.RoomID
			break
		}
	}
	if f.examID == 0 {
		t.Fatal("öğrenci 1 için koltuk bulunamadı")
	}
	if err := e.Owner.Pool.QueryRow(ctx, `SELECT slot_id FROM "wm-schedule_exams" WHERE exam_id = $1 AND schedule_id IN (SELECT id FROM "wm-schedules" WHERE status='published')`, f.examID).Scan(&f.slotID); err != nil {
		t.Fatal(err)
	}
	f.setSlot(t, "- interval '10 minutes'", "+ interval '110 minutes'")
	if _, err := e.Owner.Pool.Exec(ctx, `UPDATE "wm-invigilator_assignments" SET user_id = (SELECT id FROM "wm-uo-users" WHERE email = $3)
		WHERE slot_id = $1 AND room_id = $2 AND id = (SELECT min(id) FROM "wm-invigilator_assignments" WHERE slot_id = $1 AND room_id = $2)`, f.slotID, f.roomID, simdata.EmailInvigilator); err != nil {
		t.Fatal(err)
	}
	f.stu, _ = login(t, srv, simdata.EmailStudent)
	f.inv, _ = login(t, srv, simdata.EmailInvigilator)
	return f
}

func (f checkinFx) setSlot(t *testing.T, start, end string) {
	t.Helper()
	if _, err := f.env.Owner.Pool.Exec(context.Background(), `UPDATE "wm-slots" SET starts_at = now() `+start+`, ends_at = now() `+end+` WHERE id = $1`, f.slotID); err != nil {
		t.Fatal(err)
	}
}

func TestCheckinSessionQRAndCode(t *testing.T) {
	f := checkinFixture(t)
	s, stu, inv, slotID, roomID, examID := f.s, f.stu, f.inv, f.slotID, f.roomID, f.examID
	sec, _ := login(t, s, simdata.EmailSecretary)

	// Öğrenci oturum açamaz; gözetmen başka görevin oturumunu açamaz.
	if code, _ := post(t, s, "/checkin-sessions", stu, "", map[string]int64{"slot_id": slotID, "room_id": roomID}); code != http.StatusForbidden {
		t.Fatalf("öğrenci oturum açamaz: %d", code)
	}
	if code, _ := post(t, s, "/checkin-sessions", inv, "", map[string]int64{"slot_id": slotID, "room_id": roomID + 9999}); code != http.StatusNotFound {
		t.Fatalf("olmayan görev 404: %d", code)
	}

	code, sess := post(t, s, "/checkin-sessions", inv, "", map[string]int64{"slot_id": slotID, "room_id": roomID})
	if code != http.StatusCreated || sess["token"] == "" || len(sess["code"].(string)) != 6 {
		t.Fatalf("oturum: %d %v", code, sess)
	}
	id := itoa(int64(sess["id"].(float64)))
	tok := sess["token"].(string)
	if !strings.HasPrefix(tok, "SP1.") {
		t.Fatalf("jeton biçimi: %s", tok)
	}

	// Uydurma jeton reddedilir; sayaç artmaz.
	if code, _ := post(t, s, "/me/checkin", stu, "", map[string]string{"token": "SP1.uydurma"}); code != http.StatusUnprocessableEntity {
		t.Fatalf("uydurma jeton 422: %d", code)
	}
	// Gözetmen öğrenci uçunu kullanamaz.
	if code, _ := post(t, s, "/me/checkin", inv, "", map[string]string{"token": tok}); code != http.StatusForbidden {
		t.Fatalf("gözetmen /me/checkin kullanamaz: %d", code)
	}
	// Öğrenci QR okutur -> yoklamada "geldi".
	code, out := post(t, s, "/me/checkin", stu, "", map[string]string{"token": tok})
	if code != http.StatusOK || out["exam_id"].(float64) != float64(examID) || out["already_present"] != false || out["method"] != "qr" {
		t.Fatalf("QR ile yoklama: %d %v", code, out)
	}
	// Tekrar okutma zararsız: already_present.
	if code, out := post(t, s, "/me/checkin", stu, "", map[string]string{"token": tok}); code != http.StatusOK || out["already_present"] != true {
		t.Fatalf("tekrar okutma: %d %v", code, out)
	}
	_, att := call(t, s, http.MethodGet, "/exams/"+itoa(examID)+"/attendance", sec, nil)
	if att["summary"].(map[string]any)["present"].(float64) != 1 {
		t.Fatalf("yoklama özeti: %v", att)
	}
	// Gözetmen ekranındaki sayaç güncel.
	if code, st := call(t, s, http.MethodGet, "/checkin-sessions/"+id, inv, nil); code != http.StatusOK || st["present"].(float64) != 1 {
		t.Fatalf("durum: %d %v", code, st)
	}

	// Yedek kod: yeni kod üret, eski kod çalışmaz; yanlış kod 422.
	code, rot := post(t, s, "/checkin-sessions/"+id+"/code", inv, "", nil)
	if code != http.StatusOK {
		t.Fatalf("kod yenileme: %d %v", code, rot)
	}
	newCode := rot["code"].(string)
	if code, _ := post(t, s, "/me/checkin", stu, "", map[string]string{"code": "12a456"}); code != http.StatusUnprocessableEntity {
		t.Fatalf("biçimi bozuk kod 422: %d", code)
	}
	wrong := "000000"
	if newCode == wrong {
		wrong = "000001"
	}
	if code, out := post(t, s, "/me/checkin", stu, "", map[string]string{"code": wrong}); code != http.StatusUnprocessableEntity || out["error"].(map[string]any)["code"] != "checkin_bad_code" {
		t.Fatalf("yanlış kod: %d %v", code, out)
	}
	if code, out := post(t, s, "/me/checkin", stu, "", map[string]string{"code": newCode}); code != http.StatusOK || out["method"] != "code" {
		t.Fatalf("doğru kod: %d %v", code, out)
	}

	// Oturum kapanınca QR ve kod çalışmaz.
	if code, _ := post(t, s, "/checkin-sessions/"+id+"/close", inv, "", nil); code != http.StatusNoContent {
		t.Fatalf("kapatma: %d", code)
	}
	if code, out := post(t, s, "/me/checkin", stu, "", map[string]string{"token": tok}); code != http.StatusConflict || out["error"].(map[string]any)["code"] != "checkin_closed" {
		t.Fatalf("kapalı oturum 409: %d %v", code, out)
	}
}

func TestCheckinCodeGuessingIsRateLimited(t *testing.T) {
	f := checkinFixture(t)
	s, stu, inv, slotID, roomID := f.s, f.stu, f.inv, f.slotID, f.roomID
	_, sess := post(t, s, "/checkin-sessions", inv, "", map[string]int64{"slot_id": slotID, "room_id": roomID})
	good := sess["code"].(string)
	bad := "000000"
	if good == bad {
		bad = "000001"
	}
	var last int
	for i := 0; i < checkinMaxFails+1; i++ {
		last, _ = post(t, s, "/me/checkin", stu, "", map[string]string{"code": bad})
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("8 hatalı denemeden sonra 429 bekleniyordu: %d", last)
	}
	// Engel doğru kodu da durdurur (kaba kuvvetin amacı bu).
	if code, _ := post(t, s, "/me/checkin", stu, "", map[string]string{"code": good}); code != http.StatusTooManyRequests {
		t.Fatalf("engelliyken doğru kod da 429: %d", code)
	}
}

func TestCheckinSessionOnlyOpensAroundTheExam(t *testing.T) {
	f := checkinFixture(t)
	body := map[string]int64{"slot_id": f.slotID, "room_id": f.roomID}
	f.setSlot(t, "+ interval '5 hours'", "+ interval '7 hours'")
	if code, out := post(t, f.s, "/checkin-sessions", f.inv, "", body); code != http.StatusConflict || out["error"].(map[string]any)["code"] != "checkin_window" {
		t.Fatalf("sınavdan çok önce oturum açılamaz: %d %v", code, out)
	}
	f.setSlot(t, "- interval '5 hours'", "- interval '3 hours'")
	if code, _ := post(t, f.s, "/checkin-sessions", f.inv, "", body); code != http.StatusConflict {
		t.Fatalf("sınav bittikten sonra oturum açılamaz: %d", code)
	}
}

// Kod/QR başka salonun oturumuna aitse, o salonda koltuğu olmayan öğrenci yoklamaya yazılamaz.
func TestCheckinRejectsStudentSeatedInAnotherRoom(t *testing.T) {
	f := checkinFixture(t)
	_, sess := post(t, f.s, "/checkin-sessions", f.inv, "", map[string]int64{"slot_id": f.slotID, "room_id": f.roomID})
	if _, err := f.env.Owner.Pool.Exec(context.Background(), `UPDATE "wm-checkin_sessions" SET room_id = (SELECT id FROM "wm-rooms" WHERE id <> $1 ORDER BY id LIMIT 1) WHERE id = $2`,
		f.roomID, int64(sess["id"].(float64))); err != nil {
		t.Fatal(err)
	}
	code, out := post(t, f.s, "/me/checkin", f.stu, "", map[string]string{"token": sess["token"].(string)})
	if code != http.StatusConflict || out["error"].(map[string]any)["code"] != "checkin_not_here" {
		t.Fatalf("başka salonun oturumu 409 checkin_not_here olmalı: %d %v", code, out)
	}
}

// Sayaç veritabanındadır: ikinci bir API örneği (aynı veritabanı) de engeli görür; pencere dışına düşen
// denemeler sayılmaz ve yeni bir hatalı denemede silinir.
func TestCheckinRateLimitIsSharedAcrossInstances(t *testing.T) {
	f := checkinFixture(t)
	_, sess := post(t, f.s, "/checkin-sessions", f.inv, "", map[string]int64{"slot_id": f.slotID, "room_id": f.roomID})
	bad := "000000"
	if sess["code"].(string) == bad {
		bad = "000001"
	}
	for i := 0; i < checkinMaxFails; i++ {
		post(t, f.s, "/me/checkin", f.stu, "", map[string]string{"code": bad})
	}
	svc, _ := auth.NewService(f.env.App.Pool, auth.NewIssuer(rbacSecret, 15*time.Minute), 7*24*time.Hour)
	other := NewServer(Options{DB: f.env.App, Pool: f.env.App.Pool, Auth: svc, QR: store.NewQR(httpQRSecret),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if code, _ := post(t, other, "/me/checkin", f.stu, "", map[string]string{"code": bad}); code != http.StatusTooManyRequests {
		t.Fatalf("ikinci örnek de 429 vermeli: %d", code)
	}

	// Denemeler pencerenin dışına kayınca engel kalkar; bir sonraki hata eski satırları temizler.
	ctx := context.Background()
	if _, err := f.env.Owner.Pool.Exec(ctx, `UPDATE "wm-checkin_failures" SET failed_at = failed_at - interval '6 minutes'`); err != nil {
		t.Fatal(err)
	}
	if code, _ := post(t, other, "/me/checkin", f.stu, "", map[string]string{"code": bad}); code != http.StatusUnprocessableEntity {
		t.Fatalf("pencere geçince yeniden 422 (hatalı kod) beklenir: %d", code)
	}
	var rows int
	if err := f.env.Owner.Pool.QueryRow(ctx, `SELECT count(*) FROM "wm-checkin_failures"`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("eski denemeler temizlenmeli, kalan %d (1 beklenir)", rows)
	}
}

// Demo yoklama kodu (DEMO_CHECKIN_CODE): açıkken 111111 gözetmen oturumu olmadan öğrenciyi 'geldi' yazar; kapalıyken reddedilir.
func TestDemoCheckinCode(t *testing.T) {
	f := checkinFixture(t)
	rec := httptest.NewRecorder()
	f.s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/demo/checkin-code", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"code":""`) {
		t.Fatalf("kapalıyken ipucu boş olmalı: %d %s", rec.Code, rec.Body.String())
	}
	if code, _ := post(t, f.s, "/me/checkin", f.stu, "", map[string]string{"code": "111111"}); code == http.StatusOK {
		t.Fatal("demo kodu kapalıyken 111111 kabul edilmemeli")
	}

	f.s.qr.WithDemoCode("111111")
	rec = httptest.NewRecorder()
	f.s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/demo/checkin-code", nil))
	if !strings.Contains(rec.Body.String(), `"code":"111111"`) {
		t.Fatalf("açıkken ipucu kodu göstermeli: %s", rec.Body.String())
	}
	if code, _ := post(t, f.s, "/me/checkin", f.stu, "", map[string]string{"code": "222222"}); code == http.StatusOK {
		t.Fatal("yanlış kod kabul edildi")
	}
	code, out := post(t, f.s, "/me/checkin", f.stu, "", map[string]string{"code": "111111"})
	if code != http.StatusOK || out["method"] != "demo" || out["already_present"] == true {
		t.Fatalf("demo kodu yoklamaya yazmalı: %d %v", code, out)
	}
	var status string
	if err := f.env.Owner.Pool.QueryRow(context.Background(), `SELECT status FROM "wm-attendance_state" WHERE exam_id = $1 AND student_id = 1`, f.examID).Scan(&status); err != nil || status != "present" {
		t.Fatalf("durum present olmalı: %q %v", status, err)
	}
	// İkinci giriş: yoklaması zaten alınmış sınavı değil, henüz alınmamış bir sonrakini kaydeder.
	if code, out := post(t, f.s, "/me/checkin", f.stu, "", map[string]string{"code": "111111"}); code != http.StatusOK || out["already_present"] == true || int64(out["exam_id"].(float64)) == f.examID {
		t.Fatalf("ikinci giriş başka (yoklaması alınmamış) sınavı kaydetmeli: %d %v", code, out)
	}
	// Yanlış denemeler demo kodunu kilitlemez.
	for i := 0; i < 9; i++ {
		post(t, f.s, "/me/checkin", f.stu, "", map[string]string{"code": "000000"})
	}
	if code, out := post(t, f.s, "/me/checkin", f.stu, "", map[string]string{"code": "111111"}); code != http.StatusOK {
		t.Fatalf("deneme sınırı demo kodunu engellememeli: %d %v", code, out)
	}
	if code, _ := post(t, f.s, "/me/checkin", f.inv, "", map[string]string{"code": "111111"}); code != http.StatusForbidden {
		t.Fatalf("gözetmen /me/checkin kullanamaz: %d", code)
	}
}
