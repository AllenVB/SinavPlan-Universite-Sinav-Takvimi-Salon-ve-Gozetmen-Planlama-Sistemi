package httpapi

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AllenVB/sinavplan/backend/internal/simdata"
)

func raw(t *testing.T, h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// featureFixture: küçük veriyle bir çizelge üretir (taslak) ve kimlikleri döner.
type featureFixture struct {
	s                       *Server
	sec, lec, inv, aff, stu string
	period, sched           int64
	sid                     string
}

func newFeatureFixture(t *testing.T) *featureFixture {
	t.Helper()
	s, _ := adminServer(t)
	f := &featureFixture{s: s}
	f.sec, _ = login(t, s, simdata.EmailSecretary)
	f.lec, _ = login(t, s, simdata.EmailLecturer)
	f.inv, _ = login(t, s, simdata.EmailInvigilator)
	f.aff, _ = login(t, s, simdata.EmailStudentAffairs)
	f.stu, _ = login(t, s, simdata.EmailStudent)
	_, per := do(t, s, http.MethodGet, "/periods", f.sec, "", "")
	f.period = int64(per["items"].([]any)[0].(map[string]any)["id"].(float64))
	f.sched = f.newSchedule(t, 1)
	f.sid = itoa(f.sched)
	return f
}

func (f *featureFixture) newSchedule(t *testing.T, seed int) int64 {
	t.Helper()
	code, acc := do(t, f.s, http.MethodPost, "/schedules", f.sec, "application/json", j(map[string]any{"period_id": f.period, "seed": seed, "iterations": 20000}))
	if code != http.StatusAccepted {
		t.Fatalf("iş başlatılamadı: %d %v", code, acc)
	}
	job := waitJob(t, f.s, f.sec, int64(acc["job_id"].(float64)))
	if job["status"] != "succeeded" {
		t.Fatalf("iş başarısız: %v", job)
	}
	return int64(job["schedule_id"].(float64))
}

func TestExportXLSXAndCSV(t *testing.T) {
	f := newFeatureFixture(t)
	rec := raw(t, f.s, http.MethodGet, "/schedules/"+f.sid+"/export", f.sec, "")
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Type"), "spreadsheetml") ||
		!strings.Contains(rec.Header().Get("Content-Disposition"), ".xlsx") {
		t.Fatalf("xlsx: %d %v", rec.Code, rec.Header())
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("geçerli zip değil: %v", err)
	}
	names := map[string]bool{}
	for _, zf := range zr.File {
		names[zf.Name] = true
	}
	if !names["xl/worksheets/sheet1.xml"] || !names["xl/worksheets/sheet2.xml"] {
		t.Fatalf("iki sayfa bekleniyordu: %v", names)
	}

	csv := raw(t, f.s, http.MethodGet, "/schedules/"+f.sid+"/export?format=csv", f.sec, "")
	body := csv.Body.String()
	if csv.Code != 200 || !strings.HasPrefix(body, "\xEF\xBB\xBFTarih;") {
		t.Fatalf("csv BOM/başlık: %d %.40q", csv.Code, body)
	}
	_, cal := do(t, f.s, http.MethodGet, "/schedules/"+f.sid+"/exams?limit=200", f.sec, "", "")
	if got, want := strings.Count(body, "\n")-1, len(cal["items"].([]any)); got != want {
		t.Fatalf("csv satır sayısı %d, takvimde %d sınav", got, want)
	}

	if code, _ := do(t, f.s, http.MethodGet, "/schedules/"+f.sid+"/export?format=pdf", f.sec, "", ""); code != 422 {
		t.Fatalf("geçersiz biçim 422 olmalı: %d", code)
	}
	if rec := raw(t, f.s, http.MethodGet, "/schedules/"+f.sid+"/export", f.lec, ""); rec.Code != 404 {
		t.Fatalf("taslak öğretim üyesine görünmemeli (export): %d", rec.Code)
	}
	if rec := raw(t, f.s, http.MethodGet, "/schedules/"+f.sid+"/export", f.stu, ""); rec.Code != 403 {
		t.Fatalf("öğrenci export yapamaz: %d", rec.Code)
	}
	if rec := raw(t, f.s, http.MethodGet, "/schedules/"+f.sid+"/export", "", ""); rec.Code != 401 {
		t.Fatalf("kimliksiz 401: %d", rec.Code)
	}
}

func TestICSExports(t *testing.T) {
	f := newFeatureFixture(t)
	if c, _ := do(t, f.s, http.MethodPost, "/schedules/"+f.sid+"/publish", f.sec, "", "", "Idempotency-Key", "ics-pub"); c != 200 {
		t.Fatalf("yayın: %d", c)
	}
	_, cal := do(t, f.s, http.MethodGet, "/schedules/"+f.sid+"/exams?limit=200", f.sec, "", "")
	rec := raw(t, f.s, http.MethodGet, "/schedules/"+f.sid+"/calendar.ics", f.lec, "")
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/calendar") {
		t.Fatalf("takvim ics: %d %v", rec.Code, rec.Header())
	}
	if n, want := strings.Count(rec.Body.String(), "BEGIN:VEVENT"), len(cal["items"].([]any)); n != want {
		t.Fatalf("VEVENT %d, sınav %d", n, want)
	}

	me := raw(t, f.s, http.MethodGet, "/me/exams.ics", f.stu, "")
	if me.Code != 200 || !strings.Contains(me.Body.String(), "BEGIN:VEVENT") || !strings.Contains(me.Body.String(), "Koltuk ") {
		t.Fatalf("öğrenci ics salon/koltuk içermeli: %d\n%s", me.Code, me.Body.String())
	}
	if strings.Contains(me.Body.String(), "\n") && !strings.Contains(me.Body.String(), "\r\n") {
		t.Fatal("ics CRLF kullanmalı")
	}
	duty := raw(t, f.s, http.MethodGet, "/me/duties.ics", f.inv, "")
	if duty.Code != 200 || !strings.Contains(duty.Body.String(), "BEGIN:VCALENDAR") {
		t.Fatalf("gözetmen ics: %d", duty.Code)
	}
	if rec := raw(t, f.s, http.MethodGet, "/me/exams.ics", f.inv, ""); rec.Code != 403 {
		t.Fatalf("gözetmen öğrenci ics alamaz: %d", rec.Code)
	}
	if rec := raw(t, f.s, http.MethodGet, "/me/duties.ics", f.stu, ""); rec.Code != 403 {
		t.Fatalf("öğrenci gözetmen ics alamaz: %d", rec.Code)
	}
}

func TestActivityFeedComesFromAuditLog(t *testing.T) {
	f := newFeatureFixture(t)
	if c, _ := do(t, f.s, http.MethodPost, "/schedules/"+f.sid+"/publish", f.sec, "", ""); c != 200 {
		t.Fatalf("yayın: %d", c)
	}
	code, out := do(t, f.s, http.MethodGet, "/activity?limit=50", f.aff, "", "")
	items, _ := out["items"].([]any)
	if code != 200 || len(items) == 0 {
		t.Fatalf("akış: %d %v", code, out)
	}
	seen := map[string]bool{}
	var lastID float64 = 1e18
	for _, it := range items {
		m := it.(map[string]any)
		seen[m["action"].(string)] = true
		if id := m["id"].(float64); id >= lastID {
			t.Fatal("akış yeniden eskiye sıralı olmalı")
		} else {
			lastID = id
		}
	}
	if len(seen) < 2 {
		t.Fatalf("birden çok olay türü bekleniyordu: %v", seen)
	}
	for _, bad := range []string{"limit=0", "limit=1000", "limit=x"} {
		if c, _ := do(t, f.s, http.MethodGet, "/activity?"+bad, f.sec, "", ""); c != 422 {
			t.Fatalf("%s: %d (422 bekleniyordu)", bad, c)
		}
	}
	for name, tok := range map[string]string{"öğrenci": f.stu, "hoca": f.lec, "gözetmen": f.inv} {
		if c, _ := do(t, f.s, http.MethodGet, "/activity", tok, "", ""); c != 403 {
			t.Fatalf("%s etkinlik akışını görmemeli: %d", name, c)
		}
	}
}

func TestSimulateDoesNotPersistAndValidates(t *testing.T) {
	f := newFeatureFixture(t)
	count := func() int {
		_, l := do(t, f.s, http.MethodGet, "/schedules?limit=100", f.sec, "", "")
		return len(l["items"].([]any))
	}
	before := count()
	code, out := do(t, f.s, http.MethodPost, "/schedules/simulate", f.sec, "application/json", j(map[string]any{"period_id": f.period, "seed": 7, "iterations": 5000}))
	res, _ := out["result"].(map[string]any)
	if code != 200 || out["saved"] != false || res == nil || res["violations"].(float64) != 0 || res["unplaced"].(float64) != 0 || res["penalty"].(float64) <= 0 {
		t.Fatalf("simülasyon: %d %v", code, out)
	}
	if count() != before {
		t.Fatal("simülasyon çizelge kaydetmemeli")
	}
	// aynı girdi → aynı sonuç (deterministik)
	_, out2 := do(t, f.s, http.MethodPost, "/schedules/simulate", f.sec, "application/json", j(map[string]any{"period_id": f.period, "seed": 7, "iterations": 5000}))
	if out2["result"].(map[string]any)["penalty"] != res["penalty"] {
		t.Fatal("aynı tohum+iterasyon aynı cezayı vermeli")
	}
	for name, body := range map[string]map[string]any{"iterasyon 0": {"period_id": f.period, "iterations": 0}, "çok büyük": {"period_id": f.period, "iterations": 10_000_000}, "dönem yok": {"iterations": 10}} {
		if c, _ := do(t, f.s, http.MethodPost, "/schedules/simulate", f.sec, "application/json", j(body)); c != 422 {
			t.Fatalf("%s: %d (422)", name, c)
		}
	}
	if c, _ := do(t, f.s, http.MethodPost, "/schedules/simulate", f.stu, "application/json", j(map[string]any{"period_id": f.period, "iterations": 10})); c != 403 {
		t.Fatalf("öğrenci simüle edemez: %d", c)
	}
}

// examSlots: çizelgedeki sınav -> slot eşlemesi.
func (f *featureFixture) examSlots(t *testing.T, sched string) map[int64]int64 {
	t.Helper()
	_, cal := do(t, f.s, http.MethodGet, "/schedules/"+sched+"/exams?limit=200", f.sec, "", "")
	slot := map[int64]int64{}
	for _, it := range cal["items"].([]any) {
		m := it.(map[string]any)
		slot[int64(m["exam_id"].(float64))] = int64(m["slot_id"].(float64))
	}
	return slot
}

// sharedExamPair: en az bir ortak öğrencisi olan iki sınav (oturma planından bulunur).
func (f *featureFixture) sharedExamPair(t *testing.T) (a, b int64) {
	t.Helper()
	byStudent := map[int64][]int64{}
	for exam := range f.examSlots(t, f.sid) {
		_, out := do(t, f.s, http.MethodGet, "/schedules/"+f.sid+"/seating?exam_id="+itoa(exam)+"&limit=200", f.sec, "", "")
		for _, it := range out["items"].([]any) {
			st := int64(it.(map[string]any)["student_id"].(float64))
			byStudent[st] = append(byStudent[st], exam)
			if len(byStudent[st]) >= 2 {
				return byStudent[st][0], byStudent[st][1]
			}
		}
	}
	t.Fatal("ortak öğrencili iki sınav bulunamadı (veri seti beklenenden küçük)")
	return 0, 0
}

func TestMoveExamValidatesAndCreatesDerivedDraft(t *testing.T) {
	f := newFeatureFixture(t)
	slotOf := f.examSlots(t, f.sid)
	a, b := f.sharedExamPair(t)
	versions := func() int {
		_, l := do(t, f.s, http.MethodGet, "/schedules?limit=100", f.sec, "", "")
		return len(l["items"].([]any))
	}
	n0 := versions()
	movePath := func(exam int64) string { return "/schedules/" + f.sid + "/exams/" + itoa(exam) + "/move" }

	// 1) Çakışma: aynı öğrencinin başka sınavının slotuna taşı → reddedilmeli.
	code, out := do(t, f.s, http.MethodPost, movePath(a), f.sec, "application/json", j(map[string]any{"slot_id": slotOf[b]}))
	if code != 422 || out["error"].(map[string]any)["code"] != "constraint_violation" {
		t.Fatalf("çakışan taşıma reddedilmeli: %d %v", code, out)
	}
	if out["error"].(map[string]any)["fields"] == nil {
		t.Fatal("ihlal nedenleri alan listesinde olmalı")
	}
	code, dry := do(t, f.s, http.MethodPost, movePath(a), f.sec, "application/json", j(map[string]any{"slot_id": slotOf[b], "dry_run": true}))
	if code != 200 || dry["ok"] != false || dry["violation_count"].(float64) < 1 {
		t.Fatalf("dry_run ihlali göstermeli: %d %v", code, dry)
	}
	if versions() != n0 {
		t.Fatal("reddedilen taşıma sürüm oluşturmamalı")
	}

	// 2) Geçerli bir hedef bul (dry_run ile tara), gerçekten taşı.
	_, sl := do(t, f.s, http.MethodGet, "/periods/"+itoa(f.period)+"/slots", f.sec, "", "")
	var target int64
	for _, it := range sl["items"].([]any) {
		id := int64(it.(map[string]any)["id"].(float64))
		if id == slotOf[a] {
			continue
		}
		_, d := do(t, f.s, http.MethodPost, movePath(a), f.sec, "application/json", j(map[string]any{"slot_id": id, "dry_run": true}))
		if d["ok"] == true {
			target = id
			break
		}
	}
	if target == 0 {
		t.Skip("bu veri setinde geçerli taşıma hedefi yok")
	}
	code, mv := do(t, f.s, http.MethodPost, movePath(a), f.sec, "application/json", j(map[string]any{"slot_id": target}))
	if code != 201 || mv["derived_from"].(float64) != float64(f.sched) {
		t.Fatalf("taşıma: %d %v", code, mv)
	}
	newSid := itoa(int64(mv["schedule_id"].(float64)))
	if versions() != n0+1 {
		t.Fatal("tam bir yeni sürüm oluşmalı")
	}
	// kaynak sürüm değişmedi, yeni sürümde yalnızca o sınav taşındı
	oldSlots := f.examSlots(t, f.sid)
	newSlots := f.examSlots(t, newSid)
	if oldSlots[a] != slotOf[a] {
		t.Fatal("kaynak sürüm değişmemeli")
	}
	if newSlots[a] != target {
		t.Fatalf("sınav hedef slota taşınmadı: %d != %d", newSlots[a], target)
	}
	diff := 0
	for e, s := range newSlots {
		if oldSlots[e] != s {
			diff++
		}
	}
	if diff != 1 {
		t.Fatalf("yalnızca 1 sınav taşınmalıydı, %d farklı", diff)
	}
	// yeni sürümün planı ihlalsiz ve kayıt/metrik tutarlı
	_, sc := do(t, f.s, http.MethodGet, "/schedules/"+newSid, f.sec, "", "")
	m := sc["metrics"].(map[string]any)
	if sc["status"] != "draft" || m["room_violations"].(float64) != 0 || m["seat_violations"].(float64) != 0 || m["unseated"].(float64) != 0 {
		t.Fatalf("yeni sürüm ihlalsiz taslak olmalı: %v", sc)
	}
	edit := m["edit"].(map[string]any)
	if edit["exam_id"].(float64) != float64(a) || edit["to_slot_id"].(float64) != float64(target) || m["derived_from"].(float64) != float64(f.sched) {
		t.Fatalf("düzenleme kaydı eksik: %v", m)
	}

	// 3) Karşılaştırma: tam olarak o sınav taşınmış görünmeli.
	code, cmp := do(t, f.s, http.MethodGet, "/schedules/compare?a="+f.sid+"&b="+newSid, f.sec, "", "")
	if code != 200 || cmp["moved_count"].(float64) != 1 {
		t.Fatalf("karşılaştırma 1 taşınan sınav göstermeli: %d %v", code, cmp)
	}
	mvd := cmp["moved"].([]any)[0].(map[string]any)
	if mvd["exam_id"].(float64) != float64(a) || mvd["to_slot_id"].(float64) != float64(target) {
		t.Fatalf("yanlış taşınan sınav: %v", mvd)
	}
	if code, same := do(t, f.s, http.MethodGet, "/schedules/compare?a="+f.sid+"&b="+f.sid, f.sec, "", ""); code != 200 || same["moved_count"].(float64) != 0 {
		t.Fatalf("aynı sürüm: taşınan 0 olmalı: %v", same)
	}

	// 4) Kenar durumlar ve yetki.
	if c, _ := do(t, f.s, http.MethodPost, movePath(a), f.sec, "application/json", j(map[string]any{"slot_id": slotOf[a]})); c != 422 {
		t.Fatalf("aynı slota taşıma 422: %d", c)
	}
	if c, _ := do(t, f.s, http.MethodPost, movePath(a), f.sec, "application/json", j(map[string]any{"slot_id": 999999})); c != 422 {
		t.Fatalf("olmayan slot 422: %d", c)
	}
	if c, _ := do(t, f.s, http.MethodPost, movePath(999999), f.sec, "application/json", j(map[string]any{"slot_id": target})); c != 404 {
		t.Fatalf("olmayan sınav 404: %d", c)
	}
	for name, tok := range map[string]string{"hoca": f.lec, "öğrenci": f.stu, "öğrenci işleri": f.aff} {
		if c, _ := do(t, f.s, http.MethodPost, movePath(a), tok, "application/json", j(map[string]any{"slot_id": target})); c != 403 {
			t.Fatalf("%s taşıyamaz: %d", name, c)
		}
	}
	if c, _ := do(t, f.s, http.MethodGet, "/schedules/compare?a=1", f.sec, "", ""); c != 422 && c != 400 {
		t.Fatalf("eksik parametre: %d", c)
	}
}

func TestFacultiesEndpointForEveryRole(t *testing.T) {
	f := newFeatureFixture(t)
	for name, tok := range map[string]string{"sekreter": f.sec, "hoca": f.lec, "gözetmen": f.inv, "öğrenci": f.stu, "öğrenci işleri": f.aff} {
		code, out := do(t, f.s, http.MethodGet, "/faculties", tok, "", "")
		items, _ := out["items"].([]any)
		if code != 200 || len(items) == 0 {
			t.Fatalf("%s: %d %v", name, code, out)
		}
	}
	if c, _ := do(t, f.s, http.MethodGet, "/faculties", "", "", ""); c != 401 {
		t.Fatalf("kimliksiz 401: %d", c)
	}
	_, out := do(t, f.s, http.MethodGet, "/faculties", f.sec, "", "")
	first := out["items"].([]any)[0].(map[string]any)
	if first["name"] != "Mühendislik Fakültesi" || len(first["lecturers"].([]any)) != 8 {
		t.Fatalf("ilk fakülte Mühendislik ve 8 öğretim üyeli olmalı: %v", first)
	}
}

func TestSimulateScopeFacultyDepartmentsYears(t *testing.T) {
	f := newFeatureFixture(t)
	_, fac := do(t, f.s, http.MethodGet, "/faculties", f.sec, "", "")
	items := fac["items"].([]any)
	f1 := items[0].(map[string]any)
	d1 := f1["departments"].([]any)[0].(map[string]any)
	d2 := f1["departments"].([]any)[1].(map[string]any)
	other := items[1].(map[string]any)["departments"].([]any)[0].(map[string]any)
	body := func(sc map[string]any) string {
		return j(map[string]any{"period_id": f.period, "seed": 3, "iterations": 3000, "scope": sc})
	}
	run := func(sc map[string]any) (int, map[string]any) {
		return do(t, f.s, http.MethodPost, "/schedules/simulate", f.sec, "application/json", body(sc))
	}
	count := func(out map[string]any) float64 { return out["scope"].(map[string]any)["exams"].(float64) }

	_, all := run(map[string]any{})
	_, oneFac := run(map[string]any{"faculty_id": f1["id"]})
	_, oneDept := run(map[string]any{"faculty_id": f1["id"], "department_ids": []any{d1["id"]}})
	_, twoYears := run(map[string]any{"faculty_id": f1["id"], "department_ids": []any{d1["id"]}, "years": []int{1, 2}})
	_, oneYear := run(map[string]any{"faculty_id": f1["id"], "department_ids": []any{d1["id"]}, "years": []int{3}})
	if !(count(all) > count(oneFac) && count(oneFac) > count(oneDept) && count(oneDept) > count(twoYears) && count(twoYears) > count(oneYear)) {
		t.Fatalf("kapsam daraldıkça sınav sayısı azalmalı: %v > %v > %v > %v > %v", count(all), count(oneFac), count(oneDept), count(twoYears), count(oneYear))
	}
	// 1 bölüm × 1 sınıf = bölümün o sınıftaki ders sayısı (2 veya 5)
	if n := count(oneYear); n < 1 || n > 5 {
		t.Fatalf("1 bölüm 1 sınıf: %v sınav", n)
	}
	sc := oneYear["scope"].(map[string]any)
	if len(sc["departments"].([]any)) != 1 || sc["departments"].([]any)[0] != d1["name"] || sc["faculty"] != f1["name"] || sc["students"].(float64) <= 0 {
		t.Fatalf("kapsam özeti: %v", sc)
	}
	if oneYear["result"].(map[string]any)["violations"].(float64) != 0 {
		t.Fatal("kapsamlı prova da sert ihlalsiz olmalı")
	}
	// yayındaki sürümle kıyas yalnız tam kapsamda
	if _, ok := oneDept["published"]; ok {
		t.Fatal("kısmi kapsamda yayındakiyle kıyas verilmemeli")
	}
	// geçersiz kapsamlar
	bad := map[string]map[string]any{
		"olmayan fakülte":         {"faculty_id": 9999},
		"başka fakültenin bölümü": {"faculty_id": f1["id"], "department_ids": []any{other["id"]}},
		"olmayan bölüm":           {"department_ids": []any{9999}},
		"sınıf 5":                 {"years": []int{5}},
		"sınıf 0":                 {"years": []int{0}},
	}
	for name, sc := range bad {
		if c, _ := run(sc); c != 422 {
			t.Fatalf("%s: HTTP %d (422 bekleniyordu)", name, c)
		}
	}
	// iki bölüm birlikte
	_, two := run(map[string]any{"faculty_id": f1["id"], "department_ids": []any{d1["id"], d2["id"]}})
	if count(two) <= count(oneDept) || len(two["scope"].(map[string]any)["departments"].([]any)) != 2 {
		t.Fatalf("iki bölüm: %v", two["scope"])
	}
	if c, _ := do(t, f.s, http.MethodPost, "/schedules/simulate", f.stu, "application/json", body(map[string]any{})); c != 403 {
		t.Fatalf("öğrenci simüle edemez: %d", c)
	}
}

func TestCalendarRowsCarryFacultyDepartmentYear(t *testing.T) {
	f := newFeatureFixture(t)
	_, cal := do(t, f.s, http.MethodGet, "/schedules/"+f.sid+"/exams?limit=200", f.sec, "", "")
	for _, it := range cal["items"].([]any) {
		m := it.(map[string]any)
		if m["faculty_name"] == "" || m["department_name"] == "" || m["department_code"] == "" || m["year"].(float64) < 1 || m["year"].(float64) > 4 {
			t.Fatalf("takvim satırında fakülte/bölüm/sınıf eksik: %v", m)
		}
	}
}
