package httpapi

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

func (f *featureFixture) publish(t *testing.T) {
	t.Helper()
	if c, _ := do(t, f.s, http.MethodPost, "/schedules/"+f.sid+"/publish", f.sec, "", ""); c != 200 {
		t.Fatalf("yayın: %d", c)
	}
}

// seatingOf: bir sınavın oturma satırları.
func (f *featureFixture) seatingOf(t *testing.T, exam int64) []map[string]any {
	t.Helper()
	_, out := do(t, f.s, http.MethodGet, "/schedules/"+f.sid+"/seating?exam_id="+itoa(exam)+"&limit=200", f.sec, "", "")
	var rows []map[string]any
	for _, it := range out["items"].([]any) {
		rows = append(rows, it.(map[string]any))
	}
	return rows
}

func (f *featureFixture) mark(t *testing.T, exam int64, students []int64, status string) {
	t.Helper()
	var evs []any
	for i, st := range students {
		id, _ := uuid.NewV7()
		evs = append(evs, map[string]any{"event_id": id.String(), "device_id": "tablet-live", "exam_id": exam, "student_id": st,
			"status": status, "source": "manual", "hlc": map[string]any{"wall": time.Now().UnixMilli(), "counter": i}})
	}
	if c, out := do(t, f.s, http.MethodPost, "/attendance/sync", f.inv, "application/json", j(map[string]any{"events": evs})); c != 200 {
		t.Fatalf("sync: %d %v", c, out)
	}
}

func TestLiveOverviewMatchesSeatingAndAttendance(t *testing.T) {
	f := newFeatureFixture(t)
	f.publish(t)
	slotOf := f.examSlots(t, f.sid)

	// slotu en kalabalık sınavın slotu seç; o slottaki tüm sınavların oturan toplamını bağımsız hesapla
	var exam int64
	for e := range slotOf {
		if exam == 0 || e < exam {
			exam = e
		}
	}
	slot := slotOf[exam]
	wantSeated := 0
	for e, s := range slotOf {
		if s == slot {
			wantSeated += len(f.seatingOf(t, e))
		}
	}
	rows := f.seatingOf(t, exam)
	if len(rows) < 4 {
		t.Skipf("sınav çok küçük (%d öğrenci)", len(rows))
	}
	var present, absent []int64
	for i, r := range rows[:4] {
		st := int64(r["student_id"].(float64))
		if i < 3 {
			present = append(present, st)
		} else {
			absent = append(absent, st)
		}
	}
	f.mark(t, exam, present, "present")
	f.mark(t, exam, absent, "absent")

	code, out := do(t, f.s, http.MethodGet, "/schedules/"+f.sid+"/live?slot_id="+itoa(slot), f.sec, "", "")
	if code != 200 {
		t.Fatalf("live: %d %v", code, out)
	}
	tot := out["totals"].(map[string]any)
	if int(tot["seated"].(float64)) != wantSeated || tot["present"].(float64) != 3 || tot["absent"].(float64) != 1 ||
		int(tot["unmarked"].(float64)) != wantSeated-4 {
		t.Fatalf("toplamlar oturma/yoklamayla uyuşmuyor (beklenen oturan %d, 3 geldi, 1 gelmedi): %v", wantSeated, tot)
	}
	rooms := out["rooms"].([]any)
	if int(tot["rooms"].(float64)) != len(rooms) || len(rooms) == 0 {
		t.Fatalf("salon sayısı: %v", tot)
	}
	var sumSeated, sumPresent int
	marked := false
	for _, it := range rooms {
		r := it.(map[string]any)
		seated := int(r["seated"].(float64))
		sumSeated += seated
		sumPresent += int(r["present"].(float64))
		if int(r["present"].(float64))+int(r["absent"].(float64))+int(r["unmarked"].(float64)) != seated {
			t.Fatalf("salon %v: gelen+gelmedi+işaretsiz ≠ oturan", r["room_code"])
		}
		examSum := 0
		for _, e := range r["exams"].([]any) {
			examSum += int(e.(map[string]any)["seated"].(float64))
		}
		if examSum != seated {
			t.Fatalf("salon %v: sınav payları toplamı %d ≠ oturan %d", r["room_code"], examSum, seated)
		}
		if len(r["invigilators"].([]any)) == 0 {
			t.Fatalf("salon %v gözetmensiz", r["room_code"])
		}
		if r["present"].(float64) > 0 {
			marked = true
			if r["last_event_at"] == nil || r["devices"].(float64) != 1 {
				t.Fatalf("işaretli salonda son olay/cihaz yok: %v", r)
			}
		} else if r["absent"].(float64) == 0 && r["last_event_at"] != nil {
			t.Fatalf("hiç işaretlenmemiş salonda son olay olmamalı: %v", r)
		}
	}
	if sumSeated != wantSeated || sumPresent != 3 || !marked {
		t.Fatalf("salon toplamları: oturan %d, gelen %d", sumSeated, sumPresent)
	}
	if mode := out["slot"].(map[string]any)["mode"]; mode != "upcoming" {
		t.Fatalf("2027 tarihli oturum 'upcoming' olmalı: %v", mode)
	}

	// slot verilmezse sıradaki (en erken) oturum seçilir
	_, def := do(t, f.s, http.MethodGet, "/schedules/"+f.sid+"/live", f.aff, "", "")
	dslot := def["slot"].(map[string]any)
	if dslot["mode"] != "upcoming" || dslot["id"].(float64) != 1 && dslot["id"] == nil {
		t.Fatalf("varsayılan slot: %v", dslot)
	}
}

func TestLiveAccessAndValidation(t *testing.T) {
	f := newFeatureFixture(t)
	path := "/schedules/" + f.sid + "/live"
	// taslak: sekreter görür, öğrenci işleri 404
	if c, _ := do(t, f.s, http.MethodGet, path, f.sec, "", ""); c != 200 {
		t.Fatalf("sekreter taslağı izler: %d", c)
	}
	if c, _ := do(t, f.s, http.MethodGet, path, f.aff, "", ""); c != 404 {
		t.Fatalf("taslak öğrenci işlerine görünmemeli: %d", c)
	}
	f.publish(t)
	if c, _ := do(t, f.s, http.MethodGet, path, f.aff, "", ""); c != 200 {
		t.Fatalf("öğrenci işleri izler: %d", c)
	}
	for name, tok := range map[string]string{"hoca": f.lec, "gözetmen": f.inv, "öğrenci": f.stu, "kimliksiz": ""} {
		want := 403
		if tok == "" {
			want = 401
		}
		if c, _ := do(t, f.s, http.MethodGet, path, tok, "", ""); c != want {
			t.Fatalf("%s: %d (%d bekleniyordu)", name, c, want)
		}
	}
	if c, _ := do(t, f.s, http.MethodGet, path+"?slot_id=999999", f.sec, "", ""); c != 404 {
		t.Fatalf("olmayan slot 404: %d", c)
	}
	if c, _ := do(t, f.s, http.MethodGet, path+"?slot_id=abc", f.sec, "", ""); c != 422 && c != 400 {
		t.Fatalf("geçersiz slot_id: %d", c)
	}
	if c, _ := do(t, f.s, http.MethodGet, "/schedules/999999/live", f.sec, "", ""); c != 404 {
		t.Fatalf("olmayan çizelge 404: %d", c)
	}
}

func TestActivityFiltersAndIncrementalRead(t *testing.T) {
	f := newFeatureFixture(t)
	f.publish(t)
	// farklı türde olaylar üret: mazeret + salon
	_, me := do(t, f.s, http.MethodGet, "/me/exams", f.stu, "", "")
	exam := me["items"].([]any)[0].(map[string]any)["exam_id"]
	if c, _ := do(t, f.s, http.MethodPost, "/excuses", f.stu, "application/json", j(map[string]any{"exam_id": exam, "reason": "rapor"})); c != 201 {
		t.Fatalf("mazeret: %d", c)
	}

	_, all := do(t, f.s, http.MethodGet, "/activity?limit=200", f.sec, "", "")
	items := all["items"].([]any)
	latest := all["latest_id"].(float64)
	if len(items) < 3 || items[0].(map[string]any)["id"].(float64) != latest {
		t.Fatalf("latest_id akışın en yeni kaydı olmalı: %v / %d öğe", latest, len(items))
	}

	// action ön eki
	_, ex := do(t, f.s, http.MethodGet, "/activity?action=excuse.&limit=50", f.sec, "", "")
	exItems := ex["items"].([]any)
	if len(exItems) == 0 {
		t.Fatal("excuse. süzgeci boş döndü")
	}
	for _, it := range exItems {
		if a := it.(map[string]any)["action"].(string); len(a) < 7 || a[:7] != "excuse." {
			t.Fatalf("süzgeç dışı kayıt: %s", a)
		}
	}

	// artımlı okuma: after_id=latest → boş; yeni olay → yalnız o
	_, none := do(t, f.s, http.MethodGet, "/activity?after_id="+itoa(int64(latest)), f.sec, "", "")
	if len(none["items"].([]any)) != 0 {
		t.Fatalf("after_id=latest boş olmalı: %v", none["items"])
	}
	if c, _ := do(t, f.s, http.MethodPost, "/rooms", f.sec, "application/json", j(map[string]any{"code": "LIVE-1", "building": "Z", "floor": 1, "type": "classic", "rows": 2, "cols": 2})); c != 201 {
		t.Fatalf("salon: %d", c)
	}
	_, inc := do(t, f.s, http.MethodGet, "/activity?after_id="+itoa(int64(latest)), f.sec, "", "")
	incItems := inc["items"].([]any)
	if len(incItems) != 1 || incItems[0].(map[string]any)["action"] != "room.create" || inc["latest_id"].(float64) <= latest {
		t.Fatalf("artımlı okuma yalnız yeni olayı vermeli: %v", inc)
	}

	// before_id sayfalama: bir sayfa sonrası daha eski, çakışmasız
	_, p1 := do(t, f.s, http.MethodGet, "/activity?limit=2", f.sec, "", "")
	next := p1["next_before"].(float64)
	_, p2 := do(t, f.s, http.MethodGet, "/activity?limit=2&before_id="+itoa(int64(next)), f.sec, "", "")
	seen := map[float64]bool{}
	for _, l := range [][]any{p1["items"].([]any), p2["items"].([]any)} {
		for _, it := range l {
			id := it.(map[string]any)["id"].(float64)
			if seen[id] {
				t.Fatalf("sayfalar çakışıyor: %v", id)
			}
			seen[id] = true
		}
	}
	// aktör süzgeci
	_, byActor := do(t, f.s, http.MethodGet, "/activity?actor=999999", f.sec, "", "")
	if len(byActor["items"].([]any)) != 0 {
		t.Fatal("olmayan aktör için kayıt dönmemeli")
	}

	for _, bad := range []string{"after_id=1&before_id=5", "after_id=-1", "limit=500", "actor=x"} {
		if c, _ := do(t, f.s, http.MethodGet, "/activity?"+bad, f.sec, "", ""); c != 422 {
			t.Fatalf("%s: %d (422)", bad, c)
		}
	}
}

func TestQRCardIsShortLivedAndRotates(t *testing.T) {
	f := newFeatureFixture(t)
	f.publish(t)
	_, me := do(t, f.s, http.MethodGet, "/me/exams", f.stu, "", "")
	exam := itoa(int64(me["items"].([]any)[0].(map[string]any)["exam_id"].(float64)))
	c1, a := do(t, f.s, http.MethodPost, "/me/exams/"+exam+"/card", f.stu, "", "")
	c2, b := do(t, f.s, http.MethodPost, "/me/exams/"+exam+"/card", f.stu, "", "")
	if c1 != 201 || c2 != 201 || a["qr_token"] == b["qr_token"] {
		t.Fatalf("kart: %d %d, jeton her çağrıda yenilenmeli", c1, c2)
	}
	ttl := b["ttl_seconds"].(float64)
	if ttl < 100 || ttl > 120 {
		t.Fatalf("varsayılan ömür 2 dk olmalı: %v sn", ttl)
	}
	exp, err := time.Parse(time.RFC3339Nano, b["expires_at"].(string))
	if err != nil || time.Until(exp) > 3*time.Minute {
		t.Fatalf("expires_at kısa ömürlü olmalı: %v %v", b["expires_at"], err)
	}
	// eski jeton iptal: okutma 409 qr_expired; yenisi çalışır
	n := 0
	scan := func(tok string) (int, map[string]any) {
		n++
		return do(t, f.s, http.MethodPost, "/attendance/qr", f.inv, "application/json", j(map[string]any{"token": tok, "device_id": "t-rot"}), "Idempotency-Key", "rot-"+itoa(int64(n)))
	}
	if c, out := scan(a["qr_token"].(string)); c != 409 || out["error"].(map[string]any)["code"] != "qr_expired" {
		t.Fatalf("yenilenen kartın eski jetonu geçersiz olmalı: %d %v", c, out)
	}
	if c, out := scan(b["qr_token"].(string)); c != 200 {
		t.Fatalf("güncel jeton çalışmalı: %d %v", c, out)
	}
}

func TestLiveEventsFeedIsIncrementalAndScopedToSlot(t *testing.T) {
	f := newFeatureFixture(t)
	f.publish(t)
	slotOf := f.examSlots(t, f.sid)
	var exam, other int64
	for e := range slotOf {
		if exam == 0 || e < exam {
			exam = e
		}
	}
	for e, s := range slotOf { // başka slottaki bir sınav
		if s != slotOf[exam] {
			other = e
			break
		}
	}
	rows := f.seatingOf(t, exam)
	orows := f.seatingOf(t, other)
	if len(rows) < 3 || len(orows) < 1 {
		t.Skip("veri seti çok küçük")
	}
	st := func(r map[string]any) int64 { return int64(r["student_id"].(float64)) }
	f.mark(t, exam, []int64{st(rows[0]), st(rows[1])}, "present")
	f.mark(t, other, []int64{st(orows[0])}, "present") // başka slot: akışta görünmemeli

	path := "/schedules/" + f.sid + "/live/events?slot_id=" + itoa(slotOf[exam])
	code, out := do(t, f.s, http.MethodGet, path, f.sec, "", "")
	items := out["items"].([]any)
	if code != 200 || len(items) != 2 {
		t.Fatalf("akış yalnız bu slotun 2 olayını vermeli: %d %v", code, out)
	}
	first := items[0].(map[string]any)
	if first["full_name"] == "" || first["student_no"] == "" || first["course_code"] == "" || first["room_code"] == nil || first["status"] != "present" {
		t.Fatalf("olay zenginleştirilmemiş: %v", first)
	}
	// yeniden eskiye
	if items[0].(map[string]any)["received_at"].(string) < items[1].(map[string]any)["received_at"].(string) {
		t.Fatal("yeniden eskiye sıralı olmalı")
	}
	// since=en yeni → boş; yeni olay → yalnız o
	since := first["received_at"].(string)
	_, none := do(t, f.s, http.MethodGet, path+"&since="+since, f.sec, "", "")
	if len(none["items"].([]any)) != 0 {
		t.Fatalf("since=en yeni boş olmalı: %v", none["items"])
	}
	f.mark(t, exam, []int64{st(rows[2])}, "absent")
	_, inc := do(t, f.s, http.MethodGet, path+"&since="+since, f.sec, "", "")
	if got := inc["items"].([]any); len(got) != 1 || got[0].(map[string]any)["status"] != "absent" {
		t.Fatalf("artımlı okuma yalnız yeni olayı vermeli: %v", inc)
	}
	for _, bad := range []string{"since=dün", "limit=0", "limit=999"} {
		if c, _ := do(t, f.s, http.MethodGet, path+"&"+bad, f.sec, "", ""); c != 422 {
			t.Fatalf("%s: %d (422)", bad, c)
		}
	}
	for name, tok := range map[string]string{"hoca": f.lec, "gözetmen": f.inv, "öğrenci": f.stu} {
		if c, _ := do(t, f.s, http.MethodGet, path, tok, "", ""); c != 403 {
			t.Fatalf("%s olay akışını görmemeli: %d", name, c)
		}
	}
}

func TestStudentSeatMapAndMeName(t *testing.T) {
	f := newFeatureFixture(t)
	if c, _ := do(t, f.s, http.MethodGet, "/me/exams/1/seatmap", f.stu, "", ""); c != 404 {
		t.Fatalf("yayın yokken kroki 404: %d", c)
	}
	f.publish(t)
	_, mine := do(t, f.s, http.MethodGet, "/me/exams", f.stu, "", "")
	it := mine["items"].([]any)[0].(map[string]any)
	exam := itoa(int64(it["exam_id"].(float64)))
	code, m := do(t, f.s, http.MethodGet, "/me/exams/"+exam+"/seatmap", f.stu, "", "")
	if code != 200 || m["room_code"] != it["room_code"] || m["mine_row"] != it["seat_row"] || m["mine_col"] != it["seat_col"] {
		t.Fatalf("kroki öğrencinin programıyla uyuşmuyor: %d %v / %v", code, m, it)
	}
	seats := m["seats"].([]any)
	if len(seats) != int(m["rows"].(float64))*int(m["cols"].(float64)) {
		t.Fatalf("koltuk sayısı satır×sütun olmalı: %d", len(seats))
	}
	for _, c := range seats { // başka öğrenci verisi sızmamalı
		for k := range c.(map[string]any) {
			if k != "row" && k != "col" && k != "active" && k != "accessible" {
				t.Fatalf("koltukta beklenmeyen alan %q", k)
			}
		}
	}
	// başkasının/kayıtsız sınav 404, rol 403
	if c, _ := do(t, f.s, http.MethodGet, "/me/exams/999999/seatmap", f.stu, "", ""); c != 404 {
		t.Fatalf("kayıtsız sınav 404: %d", c)
	}
	if c, _ := do(t, f.s, http.MethodGet, "/me/exams/"+exam+"/seatmap", f.inv, "", ""); c != 403 {
		t.Fatalf("gözetmen öğrenci krokisi alamaz: %d", c)
	}
	// /me ad döner
	if _, me := do(t, f.s, http.MethodGet, "/me", f.stu, "", ""); me["full_name"] == nil || me["full_name"] == "" {
		t.Fatalf("/me full_name döndürmeli: %v", me)
	}
}
