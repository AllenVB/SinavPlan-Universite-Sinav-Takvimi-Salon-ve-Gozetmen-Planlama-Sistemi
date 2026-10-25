package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AllenVB/sinavplan/backend/internal/auth"
	"github.com/AllenVB/sinavplan/backend/internal/jobs"
	"github.com/AllenVB/sinavplan/backend/internal/simdata"
	"github.com/AllenVB/sinavplan/backend/internal/store"
	"github.com/AllenVB/sinavplan/backend/internal/testdb"
)

// adminServer: tohumlanmış küçük veri + iş çalıştırıcı (henüz çizelge YOK).
func adminServer(t *testing.T) (*Server, *testdb.Env) {
	t.Helper()
	env, _ := testdb.Seeded(t)
	svc, _ := auth.NewService(env.App.Pool, auth.NewIssuer(rbacSecret, 15*time.Minute), 7*24*time.Hour)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := NewServer(Options{DB: env.App, Pool: env.App.Pool, Auth: svc, QR: store.NewQR(httpQRSecret),
		Runner: jobs.NewRunner(env.App.Pool, log), Logger: log})
	return s, env
}

func do(t *testing.T, h http.Handler, method, path, token, ctype, body string, hdr ...string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func j(v any) string { b, _ := json.Marshal(v); return string(b) }

func waitJob(t *testing.T, s *Server, tok string, id int64) map[string]any {
	t.Helper()
	for i := 0; i < 240; i++ {
		_, job := do(t, s, http.MethodGet, "/schedule-jobs/"+itoa(id), tok, "", "")
		if st, _ := job["status"].(string); st == "succeeded" || st == "failed" {
			return job
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatal("iş 60 sn içinde bitmedi")
	return nil
}

// --- F-1 ---------------------------------------------------------------------------

func TestImportCSVRowErrorsUpsertAndIdempotency(t *testing.T) {
	s, env := adminServer(t)
	sec, _ := login(t, s, simdata.EmailSecretary)
	stu, _ := login(t, s, simdata.EmailStudent)
	csv := "student_no,full_name,department_code,disability\n" +
		"900001,Ayşe Test,BLM,false\n" +
		"900002,Mehmet Test,ELK,true\n" +
		"9,Kısa No,BLM,false\n" + // geçersiz numara
		"900003,Bilinmeyen Bölüm,XXX,false\n" + // bilinmeyen bölüm
		"900001,Tekrar,BLM,false\n" + // dosyada tekrar
		"900004,Bozuk Bayrak,BLM,belki\n"
	code, rep := do(t, s, http.MethodPost, "/imports/students", sec, "text/csv", csv)
	if code != http.StatusOK || rep["rows"].(float64) != 6 || rep["valid"].(float64) != 2 || rep["created"].(float64) != 2 {
		t.Fatalf("ilk yükleme: %d %v", code, rep)
	}
	errs := rep["errors"].([]any)
	if len(errs) != 4 {
		t.Fatalf("4 satır hatası bekleniyordu: %v", errs)
	}
	lines := map[float64]string{}
	for _, e := range errs {
		m := e.(map[string]any)
		f, _ := m["field"].(string)
		lines[m["line"].(float64)] = f
	}
	if lines[4] != "student_no" || lines[5] != "department_code" || lines[7] != "disability" || lines[6] != "" {
		t.Fatalf("satır numaraları ve alanlar yanlış: %v", lines)
	}
	// Idempotent: aynı dosya tekrar -> created=0, güncellenenler; tablo değişmez.
	var before, after int
	_ = env.App.Pool.QueryRow(context.Background(), `SELECT count(*) FROM students`).Scan(&before)
	_, rep2 := do(t, s, http.MethodPost, "/imports/students", sec, "text/csv", csv)
	_ = env.App.Pool.QueryRow(context.Background(), `SELECT count(*) FROM students`).Scan(&after)
	if rep2["created"].(float64) != 0 || rep2["updated"].(float64) != 2 || before != after {
		t.Fatalf("tekrar yükleme idempotent olmalı: %v (%d -> %d)", rep2, before, after)
	}
	// Upsert: alan değişince güncellenir.
	do(t, s, http.MethodPost, "/imports/students", sec, "text/csv", "student_no,full_name,department_code\n900001,Yeni Ad,BLM\n")
	var name string
	_ = env.App.Pool.QueryRow(context.Background(), `SELECT full_name FROM students WHERE student_no='900001'`).Scan(&name)
	if name != "Yeni Ad" {
		t.Fatalf("upsert adı güncellemeli: %q", name)
	}

	// Ders -> şube -> kayıt zinciri.
	code, r := do(t, s, http.MethodPost, "/imports/courses", sec, "text/csv", "code,name,department_code,year,requires_lab\nTST101,Test Dersi,BLM,1,true\nTST102,Hatalı,BLM,9,false\n")
	if code != http.StatusOK || r["created"].(float64) != 1 || len(r["errors"].([]any)) != 1 {
		t.Fatalf("dersler: %v", r)
	}
	code, r = do(t, s, http.MethodPost, "/imports/sections", sec, "text/csv", "course_code,section_code,lecturer_email\nTST101,A,hoca@sinavplan.test\nTST101,B,yok@x.com\n")
	if r["created"].(float64) != 1 || len(r["errors"].([]any)) != 1 {
		t.Fatalf("şubeler: %v", r)
	}
	enr := "student_no,course_code,section_code\n900001,TST101,A\n900002,TST101,A\n900001,TST101,Z\n"
	_, r = do(t, s, http.MethodPost, "/imports/enrollments", sec, "text/csv", enr)
	if r["created"].(float64) != 2 || len(r["errors"].([]any)) != 1 {
		t.Fatalf("kayıtlar: %v", r)
	}
	_, r = do(t, s, http.MethodPost, "/imports/enrollments", sec, "text/csv", enr)
	if r["created"].(float64) != 0 || r["unchanged"].(float64) != 2 {
		t.Fatalf("kayıtlar tekrar: %v", r)
	}

	// Başlık hataları 422, bilinmeyen tür 404, yetkisiz rol 403, kimliksiz 401.
	if code, _ := do(t, s, http.MethodPost, "/imports/students", sec, "text/csv", "student_no,full_name\n1,2\n"); code != http.StatusUnprocessableEntity {
		t.Fatalf("eksik sütun 422 olmalı: %d", code)
	}
	if code, _ := do(t, s, http.MethodPost, "/imports/students", sec, "text/csv", ""); code != http.StatusUnprocessableEntity {
		t.Fatalf("boş dosya 422 olmalı: %d", code)
	}
	if code, _ := do(t, s, http.MethodPost, "/imports/gizli", sec, "text/csv", "a\n"); code != http.StatusNotFound {
		t.Fatalf("bilinmeyen tür 404: %d", code)
	}
	if code, _ := do(t, s, http.MethodPost, "/imports/students", stu, "text/csv", csv); code != http.StatusForbidden {
		t.Fatalf("öğrenci içe aktaramaz: %d", code)
	}
	if code, _ := do(t, s, http.MethodPost, "/imports/students", "", "text/csv", csv); code != http.StatusUnauthorized {
		t.Fatalf("kimliksiz 401: %d", code)
	}
}

// BOM'lu (Excel) ve büyük/küçük harfli başlık kabul edilir.
func TestImportCSVAcceptsBOMAndHeaderCase(t *testing.T) {
	s, _ := adminServer(t)
	sec, _ := login(t, s, simdata.EmailSecretary)
	code, r := do(t, s, http.MethodPost, "/imports/students", sec, "text/csv", "\ufeffStudent_No, Full_Name ,Department_Code\n900050,Excel Öğrenci,BLM\n")
	if code != http.StatusOK || r["created"].(float64) != 1 {
		t.Fatalf("BOM/harf duyarsız başlık: %d %v", code, r)
	}
}

// --- F-2 ---------------------------------------------------------------------------

func TestRoomsCRUDAndLayoutRules(t *testing.T) {
	s, _ := adminServer(t)
	sec, _ := login(t, s, simdata.EmailSecretary)
	lec, _ := login(t, s, simdata.EmailLecturer)

	body := map[string]any{"code": "TST-1", "building": "Z", "floor": 2, "type": "classic", "rows": 4, "cols": 6,
		"inactive_seats": [][2]int{{3, 5}}, "accessible_seats": [][2]int{{0, 0}, {0, 2}}}
	code, room := do(t, s, http.MethodPost, "/rooms", sec, "application/json", j(body))
	if code != http.StatusCreated || room["capacity"].(float64) != 23 || room["accessible_seats"].(float64) != 2 || len(room["seats"].([]any)) != 24 {
		t.Fatalf("oluşturma: %d %v", code, room)
	}
	id := itoa(int64(room["id"].(float64)))
	if code, _ := do(t, s, http.MethodPost, "/rooms", sec, "application/json", j(body)); code != http.StatusConflict {
		t.Fatalf("aynı kod 409 olmalı: %d", code)
	}
	bad := map[string]any{"code": "X", "building": "Z", "floor": 1, "type": "amfi", "rows": 0, "cols": 6}
	code, out := do(t, s, http.MethodPost, "/rooms", sec, "application/json", j(bad))
	if code != http.StatusUnprocessableEntity || out["error"].(map[string]any)["fields"] == nil {
		t.Fatalf("geçersiz salon 422 + alan hataları: %d %v", code, out)
	}
	badAcc := map[string]any{"code": "X2", "building": "Z", "floor": 1, "type": "classic", "rows": 2, "cols": 2,
		"inactive_seats": [][2]int{{0, 0}}, "accessible_seats": [][2]int{{0, 0}}}
	if code, _ := do(t, s, http.MethodPost, "/rooms", sec, "application/json", j(badAcc)); code != http.StatusUnprocessableEntity {
		t.Fatalf("pasif koltuk erişilebilir olamaz: %d", code)
	}

	if code, got := do(t, s, http.MethodGet, "/rooms/"+id, lec, "", ""); code != http.StatusOK || got["code"] != "TST-1" {
		t.Fatalf("öğretim üyesi okuyabilmeli: %d", code)
	}
	if code, _ := do(t, s, http.MethodPost, "/rooms", lec, "application/json", j(body)); code != http.StatusForbidden {
		t.Fatalf("öğretim üyesi salon oluşturamaz: %d", code)
	}

	// Güncelleme: düzen değişince koltuklar yeniden kurulur (kullanılmayan salon).
	body["rows"], body["cols"], body["inactive_seats"] = 5, 6, [][2]int{}
	code, upd := do(t, s, http.MethodPut, "/rooms/"+id, sec, "application/json", j(body))
	if code != http.StatusOK || upd["capacity"].(float64) != 30 {
		t.Fatalf("güncelleme: %d %v", code, upd)
	}
	if code, _ := do(t, s, http.MethodPut, "/rooms/999999", sec, "application/json", j(body)); code != http.StatusNotFound {
		t.Fatalf("olmayan salon 404: %d", code)
	}

	// Listeleme + tip süzgeci + keyset.
	code, list := do(t, s, http.MethodGet, "/rooms?limit=3&type=lab", sec, "", "")
	if code != http.StatusOK || len(list["items"].([]any)) == 0 {
		t.Fatalf("liste: %d %v", code, list)
	}
	if code, _ := do(t, s, http.MethodGet, "/rooms?type=amfi", sec, "", ""); code != http.StatusUnprocessableEntity {
		t.Fatalf("geçersiz tip 422: %d", code)
	}

	// Silme: kullanılmayan silinir, yoksa 404.
	if code, _ := do(t, s, http.MethodDelete, "/rooms/"+id, sec, "", ""); code != http.StatusNoContent {
		t.Fatalf("silme: %d", code)
	}
	if code, _ := do(t, s, http.MethodGet, "/rooms/"+id, sec, "", ""); code != http.StatusNotFound {
		t.Fatalf("silinen salon 404: %d", code)
	}
}

// --- F-3 + F-4 + F-5 + F-6 uçtan uca -------------------------------------------------

func TestScheduleLifecycleEndToEnd(t *testing.T) {
	s, env := adminServer(t)
	ctx := context.Background()
	sec, _ := login(t, s, simdata.EmailSecretary)
	stu, _ := login(t, s, simdata.EmailStudent)
	lec, _ := login(t, s, simdata.EmailLecturer)
	inv, _ := login(t, s, simdata.EmailInvigilator)
	aff, _ := login(t, s, simdata.EmailStudentAffairs)

	// Dönem listesi.
	code, per := do(t, s, http.MethodGet, "/periods", sec, "", "")
	periods := per["items"].([]any)
	if code != http.StatusOK || len(periods) != 1 {
		t.Fatalf("dönemler: %d %v", code, per)
	}
	periodID := int64(periods[0].(map[string]any)["id"].(float64))

	// Doğrulama ve yetki.
	if code, _ := do(t, s, http.MethodPost, "/schedules", sec, "application/json", `{"period_id":0}`); code != http.StatusUnprocessableEntity {
		t.Fatalf("period_id zorunlu: %d", code)
	}
	if code, _ := do(t, s, http.MethodPost, "/schedules", sec, "application/json", j(map[string]any{"period_id": periodID, "iterations": 99999999})); code != http.StatusUnprocessableEntity {
		t.Fatalf("aşırı iterasyon 422: %d", code)
	}
	if code, _ := do(t, s, http.MethodPost, "/schedules", lec, "application/json", j(map[string]any{"period_id": periodID})); code != http.StatusForbidden {
		t.Fatalf("öğretim üyesi çizelge üretemez: %d", code)
	}
	if code, _ := do(t, s, http.MethodPost, "/schedules", sec, "application/json", `{"period_id":9999}`); code != http.StatusNotFound {
		t.Fatalf("olmayan dönem 404: %d", code)
	}

	// Asenkron iş: 202 -> durum -> sonuç.
	code, acc := do(t, s, http.MethodPost, "/schedules", sec, "application/json", j(map[string]any{"period_id": periodID, "iterations": 30000}))
	if code != http.StatusAccepted || acc["status"] != "queued" {
		t.Fatalf("202 bekleniyordu: %d %v", code, acc)
	}
	jobID := int64(acc["job_id"].(float64))
	// Çalışırken ikinci iş 409 (aynı dönem).
	if code, _ := do(t, s, http.MethodPost, "/schedules", sec, "application/json", j(map[string]any{"period_id": periodID})); code != http.StatusConflict {
		t.Logf("not: ilk iş zaten bitmiş olabilir (%d)", code)
	}
	job := waitJob(t, s, sec, jobID)
	if job["status"] != "succeeded" {
		t.Fatalf("iş başarısız: %v", job)
	}
	schedID := int64(job["schedule_id"].(float64))
	report := job["result"].(map[string]any)["report"].(map[string]any)
	if report["room_violations"].(float64) != 0 || report["seat_violations"].(float64) != 0 || report["unseated"].(float64) != 0 {
		t.Fatalf("üretilen plan ihlalsiz olmalı: %v", report)
	}
	sid := itoa(schedID)

	// Taslak: yalnız sekreter görür, diğer roller 404.
	if code, _ := do(t, s, http.MethodGet, "/schedules/"+sid, sec, "", ""); code != http.StatusOK {
		t.Fatalf("sekreter taslağı görür: %d", code)
	}
	for name, tok := range map[string]string{"öğretim üyesi": lec, "gözetmen": inv, "öğrenci işleri": aff} {
		if code, _ := do(t, s, http.MethodGet, "/schedules/"+sid, tok, "", ""); code != http.StatusNotFound {
			t.Fatalf("%s taslağı görmemeli: %d", name, code)
		}
	}
	if code, _ := do(t, s, http.MethodGet, "/schedules/"+sid, stu, "", ""); code != http.StatusForbidden {
		t.Fatalf("öğrenci takvim yönetim ucuna erişemez: %d", code)
	}
	_, mine := do(t, s, http.MethodGet, "/me/exams", stu, "", "")
	if len(mine["items"].([]any)) != 0 {
		t.Fatalf("yayınlanmamış takvim öğrenciye görünmemeli")
	}

	// Yayınla (idempotent): ikinci çağrı aynı anahtarla aynı 200.
	c1, p1 := do(t, s, http.MethodPost, "/schedules/"+sid+"/publish", sec, "", "", "Idempotency-Key", "pub-1")
	c2, p2 := do(t, s, http.MethodPost, "/schedules/"+sid+"/publish", sec, "", "", "Idempotency-Key", "pub-1")
	if c1 != http.StatusOK || c2 != http.StatusOK || p1["status"] != "published" || p2["status"] != "published" {
		t.Fatalf("yayınlama: %d %v / %d %v", c1, p1, c2, p2)
	}
	if code, _ := do(t, s, http.MethodPost, "/schedules/"+sid+"/publish", sec, "", ""); code != http.StatusConflict {
		t.Fatalf("anahtarsız ikinci yayınlama 409: %d", code)
	}

	// F-4: takvim, salon planı, oturma planı.
	code, cal := do(t, s, http.MethodGet, "/schedules/"+sid+"/exams?limit=200", lec, "", "")
	exams := cal["items"].([]any)
	if code != http.StatusOK || len(exams) != 48 {
		t.Fatalf("takvim: %d, %d sınav", code, len(exams))
	}
	first := exams[0].(map[string]any)
	slot, exam := itoa(int64(first["slot_id"].(float64))), itoa(int64(first["exam_id"].(float64)))
	code, sr := do(t, s, http.MethodGet, "/schedules/"+sid+"/rooms?slot_id="+slot, sec, "", "")
	if code != http.StatusOK || len(sr["items"].([]any)) == 0 {
		t.Fatalf("salon planı: %d %v", code, sr)
	}
	if code, _ := do(t, s, http.MethodGet, "/schedules/"+sid+"/rooms", sec, "", ""); code != http.StatusUnprocessableEntity {
		t.Fatalf("slot_id zorunlu: %d", code)
	}
	code, seat := do(t, s, http.MethodGet, "/schedules/"+sid+"/seating?exam_id="+exam+"&limit=5", sec, "", "")
	if code != http.StatusOK || len(seat["items"].([]any)) == 0 {
		t.Fatalf("oturma planı: %d %v", code, seat)
	}
	// Sahiplik: öğretim üyesi yalnız kendi dersinin sınavını görür.
	var lecturerExam, otherExam int64
	rows, _ := env.App.Pool.Query(ctx, `SELECT e.id, bool_or(sc.lecturer_id = 1) FROM exams e JOIN sections sc ON sc.course_id = e.course_id GROUP BY e.id ORDER BY e.id`)
	for rows.Next() {
		var id int64
		var own bool
		_ = rows.Scan(&id, &own)
		if own && lecturerExam == 0 {
			lecturerExam = id
		}
		if !own && otherExam == 0 {
			otherExam = id
		}
	}
	rows.Close()
	if code, _ := do(t, s, http.MethodGet, "/schedules/"+sid+"/seating?exam_id="+itoa(lecturerExam), lec, "", ""); code != http.StatusOK {
		t.Fatalf("öğretim üyesi kendi sınavını görmeli: %d", code)
	}
	if code, _ := do(t, s, http.MethodGet, "/schedules/"+sid+"/seating?exam_id="+itoa(otherExam), lec, "", ""); code != http.StatusForbidden {
		t.Fatalf("öğretim üyesi başkasının sınavını görmemeli: %d", code)
	}
	if code, _ := do(t, s, http.MethodGet, "/schedules/"+sid+"/seating?exam_id="+exam, stu, "", ""); code != http.StatusForbidden {
		t.Fatalf("öğrenci oturma planına erişemez: %d", code)
	}

	// Öğrencinin programı artık dolu (yayınlandı).
	_, mine = do(t, s, http.MethodGet, "/me/exams", stu, "", "")
	if len(mine["items"].([]any)) == 0 {
		t.Fatal("yayınlanan takvim öğrencinin programında görünmeli")
	}

	// F-5: görevler, gözetmen çıkarma.
	code, duties := do(t, s, http.MethodGet, "/schedules/"+sid+"/invigilators?limit=200", sec, "", "")
	items := duties["items"].([]any)
	if code != http.StatusOK || len(items) == 0 {
		t.Fatalf("görevler: %d", code)
	}
	removed := int64(items[0].(map[string]any)["user_id"].(float64))
	var before int
	_ = env.App.Pool.QueryRow(ctx, `SELECT count(*) FROM invigilator_assignments WHERE schedule_id=$1`, schedID).Scan(&before)
	c1, rm := do(t, s, http.MethodPost, "/schedules/"+sid+"/invigilators/"+itoa(removed)+"/remove", sec, "", "", "Idempotency-Key", "rm-1")
	c2, rm2 := do(t, s, http.MethodPost, "/schedules/"+sid+"/invigilators/"+itoa(removed)+"/remove", sec, "", "", "Idempotency-Key", "rm-1")
	if c1 != http.StatusOK || c2 != http.StatusOK || j(rm) != j(rm2) {
		t.Fatalf("gözetmen çıkarma idempotent olmalı: %d %d", c1, c2)
	}
	vac, changes := rm["vacated"].(float64), len(rm["changes"].([]any))
	if rm["violations"].(float64) != 0 || changes < int(vac) {
		t.Fatalf("çıkarma sonucu: %v", rm)
	}
	var left, after int
	_ = env.App.Pool.QueryRow(ctx, `SELECT count(*) FROM invigilator_assignments WHERE schedule_id=$1 AND user_id=$2`, schedID, removed).Scan(&left)
	_ = env.App.Pool.QueryRow(ctx, `SELECT count(*) FROM invigilator_assignments WHERE schedule_id=$1`, schedID).Scan(&after)
	unfilled := len(rm["unfilled"].([]any))
	if left != 0 || (unfilled == 0 && after != before) {
		t.Fatalf("çıkarılan gözetmenin görevi kalmamalı ve toplam atama korunmalı: kalan %d, %d -> %d", left, before, after)
	}
	if code, _ := do(t, s, http.MethodPost, "/schedules/"+sid+"/invigilators/999999/remove", sec, "", ""); code != http.StatusNotFound {
		t.Fatalf("olmayan gözetmen 404: %d", code)
	}
	if code, _ := do(t, s, http.MethodGet, "/me/duties", inv, "", ""); code != http.StatusOK {
		t.Fatalf("görevlerim: %d", code)
	}

	// F-6: çizelge çakışmaları (aynı gün iki sınav).
	if code, cf := do(t, s, http.MethodGet, "/schedules/"+sid+"/conflicts?limit=5", aff, "", ""); code != http.StatusOK || cf["items"] == nil {
		t.Fatalf("çakışmalar: %d %v", code, cf)
	}

	// Sürüm listesi.
	_, vs := do(t, s, http.MethodGet, "/schedules?period_id="+itoa(periodID), lec, "", "")
	if len(vs["items"].([]any)) != 1 {
		t.Fatalf("öğretim üyesi yalnız yayınlanmışı listede görür: %v", vs)
	}
}

// Çözümsüz girdi: iş "failed" olur, açıklayıcı rapor taşır, kısmi çizelge yazılmaz.
func TestScheduleJobFailsWithExplanationWhenInfeasible(t *testing.T) {
	s, env := adminServer(t)
	sec, _ := login(t, s, simdata.EmailSecretary)
	// Dönemi 1 gün x 1 oturuma indir: ortak öğrencili sınavlar tek slota sığmaz.
	if _, err := env.Owner.Pool.Exec(context.Background(), `DELETE FROM slots WHERE id NOT IN (SELECT id FROM slots ORDER BY day_index, session_index LIMIT 1);
		UPDATE exam_periods SET days = 1, sessions_per_day = 1`); err != nil {
		t.Fatal(err)
	}
	_, per := do(t, s, http.MethodGet, "/periods", sec, "", "")
	pid := int64(per["items"].([]any)[0].(map[string]any)["id"].(float64))
	_, acc := do(t, s, http.MethodPost, "/schedules", sec, "application/json", j(map[string]any{"period_id": pid, "iterations": 1000}))
	job := waitJob(t, s, sec, int64(acc["job_id"].(float64)))
	if job["status"] != "failed" || job["error"] == nil {
		t.Fatalf("çözümsüz iş başarısız olmalı: %v", job)
	}
	res := job["result"].(map[string]any)
	inf := res["infeasibility"].(map[string]any)
	if len(inf["exams"].([]any)) == 0 || len(inf["reasons"].([]any)) == 0 {
		t.Fatalf("hangi sınavların neden yerleşemediği raporlanmalı: %v", inf)
	}
	var n int
	_ = env.App.Pool.QueryRow(context.Background(), `SELECT count(*) FROM schedules`).Scan(&n)
	if n != 0 {
		t.Fatalf("başarısız işte kısmi çizelge yazılmamalı: %d", n)
	}
	if code, _ := do(t, s, http.MethodGet, "/schedule-jobs/999", sec, "", ""); code != http.StatusNotFound {
		t.Fatalf("olmayan iş 404: %d", code)
	}
}

func TestEveryJobOnlyOnePerPeriod(t *testing.T) {
	_, env := adminServer(t)
	ctx := context.Background()
	var ok, busy atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := store.EnqueueJob(ctx, env.App.Pool, 1, map[string]int{"x": 1}, 1); err == nil {
				ok.Add(1)
			} else if err == store.ErrConflict {
				busy.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if ok.Load() != 1 || busy.Load() != 7 {
		t.Fatalf("aynı dönemde tek iş: %d kabul, %d meşgul", ok.Load(), busy.Load())
	}
	if n, err := store.FailInterruptedJobs(ctx, env.App.Pool); err != nil || n != 1 {
		t.Fatalf("yarım iş kapatılmalı: %d %v", n, err)
	}
}

// --- F-6: mazeret ------------------------------------------------------------------

func TestExcuseWorkflow(t *testing.T) {
	s, env := adminServer(t)
	ctx := context.Background()
	stu, _ := login(t, s, simdata.EmailStudent)
	inv, _ := login(t, s, simdata.EmailInvigilator)
	aff, _ := login(t, s, simdata.EmailStudentAffairs)
	sec, _ := login(t, s, simdata.EmailSecretary)
	lec, _ := login(t, s, simdata.EmailLecturer)

	var examID, otherExam int64
	_ = env.App.Pool.QueryRow(ctx, `SELECT e.id FROM exams e JOIN sections s ON s.course_id=e.course_id JOIN enrollments en ON en.section_id=s.id
		WHERE en.student_id=1 ORDER BY e.id LIMIT 1`).Scan(&examID)
	_ = env.App.Pool.QueryRow(ctx, `SELECT e.id FROM exams e WHERE NOT EXISTS (SELECT 1 FROM sections s JOIN enrollments en ON en.section_id=s.id
		WHERE s.course_id=e.course_id AND en.student_id=1) ORDER BY e.id LIMIT 1`).Scan(&otherExam)

	// Öğrenci: kendi sınavı için başvurur; kayıtlı olmadığı sınav 404; kısa gerekçe 422.
	code, ex := do(t, s, http.MethodPost, "/excuses", stu, "application/json", j(map[string]any{"exam_id": examID, "reason": "Hastane raporum var"}))
	if code != http.StatusCreated || ex["status"] != "pending" || ex["kind"] != "student" {
		t.Fatalf("başvuru: %d %v", code, ex)
	}
	id := itoa(int64(ex["id"].(float64)))
	if code, _ := do(t, s, http.MethodPost, "/excuses", stu, "application/json", j(map[string]any{"exam_id": examID, "reason": "Tekrar başvuru"})); code != http.StatusConflict {
		t.Fatalf("aynı sınav için ikinci başvuru 409: %d", code)
	}
	if code, _ := do(t, s, http.MethodPost, "/excuses", stu, "application/json", j(map[string]any{"exam_id": otherExam, "reason": "Kayıtlı olmadığım sınav"})); code != http.StatusNotFound {
		t.Fatalf("kayıtlı olunmayan sınav 404: %d", code)
	}
	if code, _ := do(t, s, http.MethodPost, "/excuses", stu, "application/json", j(map[string]any{"exam_id": examID, "reason": "x"})); code != http.StatusUnprocessableEntity {
		t.Fatalf("kısa gerekçe 422: %d", code)
	}
	if code, _ := do(t, s, http.MethodPost, "/excuses", stu, "application/json", j(map[string]any{"slot_id": 1, "reason": "Yanlış alan"})); code != http.StatusUnprocessableEntity {
		t.Fatalf("öğrenci slot_id veremez: %d", code)
	}
	if code, _ := do(t, s, http.MethodPost, "/excuses", lec, "application/json", j(map[string]any{"exam_id": examID, "reason": "Yetkisiz rol"})); code != http.StatusForbidden {
		t.Fatalf("öğretim üyesi başvuramaz: %d", code)
	}

	// Gözetmen slot mazereti.
	if code, e := do(t, s, http.MethodPost, "/excuses", inv, "application/json", j(map[string]any{"slot_id": 1, "reason": "Başka sınavda görevliyim"})); code != http.StatusCreated || e["kind"] != "invigilator" {
		t.Fatalf("gözetmen mazereti: %d %v", code, e)
	}

	// Listeler: öğrenci yalnız kendisininkini görür; öğrenci işleri hepsini.
	_, mine := do(t, s, http.MethodGet, "/me/excuses", stu, "", "")
	if len(mine["items"].([]any)) != 1 {
		t.Fatalf("öğrenci yalnız kendi mazeretini görmeli: %v", mine)
	}
	if code, _ := do(t, s, http.MethodGet, "/excuses", stu, "", ""); code != http.StatusForbidden {
		t.Fatalf("öğrenci tüm mazeretleri listeleyemez: %d", code)
	}
	_, all := do(t, s, http.MethodGet, "/excuses?status=pending", aff, "", "")
	if len(all["items"].([]any)) != 2 {
		t.Fatalf("2 bekleyen mazeret bekleniyordu: %v", all)
	}
	if code, _ := do(t, s, http.MethodGet, "/excuses?status=bilinmez", aff, "", ""); code != http.StatusUnprocessableEntity {
		t.Fatalf("geçersiz süzgeç 422: %d", code)
	}

	// Karar: öğrenci işleri ve sekreter verebilir; öğrenci/öğretim üyesi veremez.
	if code, _ := do(t, s, http.MethodPost, "/excuses/"+id+"/decision", stu, "application/json", `{"decision":"approved"}`); code != http.StatusForbidden {
		t.Fatalf("öğrenci karar veremez: %d", code)
	}
	if code, _ := do(t, s, http.MethodPost, "/excuses/"+id+"/decision", lec, "application/json", `{"decision":"approved"}`); code != http.StatusForbidden {
		t.Fatalf("öğretim üyesi karar veremez: %d", code)
	}
	if code, _ := do(t, s, http.MethodPost, "/excuses/"+id+"/decision", aff, "application/json", `{"decision":"belki"}`); code != http.StatusUnprocessableEntity {
		t.Fatalf("geçersiz karar 422: %d", code)
	}
	c1, d1 := do(t, s, http.MethodPost, "/excuses/"+id+"/decision", aff, "application/json", `{"decision":"approved","note":"Rapor doğrulandı"}`, "Idempotency-Key", "dec-1")
	c2, d2 := do(t, s, http.MethodPost, "/excuses/"+id+"/decision", aff, "application/json", `{"decision":"approved","note":"Rapor doğrulandı"}`, "Idempotency-Key", "dec-1")
	if c1 != http.StatusOK || c2 != http.StatusOK || d1["status"] != "approved" || j(d1) != j(d2) {
		t.Fatalf("karar idempotent olmalı: %d %d %v", c1, c2, d1)
	}
	if code, _ := do(t, s, http.MethodPost, "/excuses/"+id+"/decision", aff, "application/json", `{"decision":"rejected"}`); code != http.StatusConflict {
		t.Fatalf("karara bağlanmış mazeret 409: %d", code)
	}
	// Sekreter de karar verebilir (rol kabul edilir; karara bağlanmış olduğu için 409).
	if code, _ := do(t, s, http.MethodPost, "/excuses/"+id+"/decision", sec, "application/json", `{"decision":"rejected"}`); code != http.StatusConflict {
		t.Fatalf("sekreter için de rol kabul edilmeli, durum 409: %d", code)
	}
	// Bekleyen diğer mazereti sekreter onaylar.
	if _, pend := do(t, s, http.MethodGet, "/excuses?status=pending", sec, "", ""); len(pend["items"].([]any)) == 1 {
		pid := itoa(int64(pend["items"].([]any)[0].(map[string]any)["id"].(float64)))
		if code, out := do(t, s, http.MethodPost, "/excuses/"+pid+"/decision", sec, "application/json", `{"decision":"rejected","note":"Belge yetersiz"}`); code != http.StatusOK || out["status"] != "rejected" {
			t.Fatalf("sekreter mazereti reddedebilmeli: %d %v", code, out)
		}
	} else {
		t.Fatalf("1 bekleyen mazeret bekleniyordu: %v", pend)
	}
	if code, _ := do(t, s, http.MethodPost, "/excuses/99999/decision", aff, "application/json", `{"decision":"approved"}`); code != http.StatusNotFound {
		t.Fatalf("olmayan mazeret 404: %d", code)
	}

	// Onaylı mazeretli öğrenciye o sınav için kart verilmez; denetim kaydı var.
	if code, out := do(t, s, http.MethodPost, "/me/exams/"+itoa(examID)+"/card", stu, "", ""); code != http.StatusConflict && code != http.StatusNotFound {
		t.Fatalf("mazeretli öğrenciye kart verilmemeli: %d %v", code, out)
	}
	var n int
	_ = env.App.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action IN ('excuse.create','excuse.decide')`).Scan(&n)
	if n != 4 { // 2 başvuru + 2 karar (öğrenci işleri + sekreter)
		t.Fatalf("4 denetim kaydı bekleniyordu: %d", n)
	}
}

// Eşzamanlı iki karar: tam biri geçer.
func TestConcurrentExcuseDecisionsOnlyOneWins(t *testing.T) {
	_, env := adminServer(t)
	ctx := context.Background()
	var examID int64
	if err := env.App.Pool.QueryRow(ctx, `SELECT e.id FROM exams e JOIN sections s ON s.course_id=e.course_id JOIN enrollments en ON en.section_id=s.id
		WHERE en.student_id=1 ORDER BY e.id LIMIT 1`).Scan(&examID); err != nil {
		t.Fatal(err)
	}
	e, err := store.CreateStudentExcuse(ctx, env.App.Pool, 1, examID, "Eşzamanlı karar testi")
	if err != nil {
		t.Fatalf("mazeret oluşturulamadı: %v", err)
	}
	var ok, conflict atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := store.DecideExcuse(ctx, env.App.Pool, e.ID, 4, i%2 == 0, "")
			if err == nil {
				ok.Add(1)
			} else if err == store.ErrConflict {
				conflict.Add(1)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	if ok.Load() != 1 || conflict.Load() != 9 {
		t.Fatalf("1 karar + 9 çakışma bekleniyordu: %d / %d", ok.Load(), conflict.Load())
	}
}
