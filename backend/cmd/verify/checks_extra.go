package main

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (e *env) raw(ctx context.Context, path, tok string) resp {
	r, err := e.a.do(ctx, http.MethodGet, path, tok, "", nil)
	if err != nil {
		return resp{code: 0}
	}
	return r
}

// X-2: Excel/CSV dışa aktarma — geçerli zip, iki sayfa, CSV satır sayısı = takvimdeki sınav.
func checkX2(ctx context.Context, e *env, f *failures) string {
	if !needSnap(e, f) {
		return ""
	}
	sec := e.tok["secretary"]
	x := e.raw(ctx, fmt.Sprintf("/schedules/%d/export", e.sched2), sec)
	f.check(x.code == 200 && strings.HasPrefix(x.header.Get("Content-Type"), "application/vnd.openxmlformats"), "xlsx: HTTP %d %s", x.code, x.header.Get("Content-Type"))
	zr, err := zip.NewReader(bytes.NewReader(x.raw), int64(len(x.raw)))
	if f.check(err == nil, "xlsx geçerli zip değil: %v", err); err == nil {
		have := map[string]bool{}
		for _, zf := range zr.File {
			have[zf.Name] = true
		}
		f.check(have["xl/worksheets/sheet1.xml"] && have["xl/worksheets/sheet2.xml"] && have["xl/workbook.xml"], "xlsx beklenen parçaları içermiyor: %v", have)
	}
	c := e.raw(ctx, fmt.Sprintf("/schedules/%d/export?format=csv", e.sched2), sec)
	lines := strings.Count(string(c.raw), "\n") - 1
	f.check(c.code == 200 && bytes.HasPrefix(c.raw, []byte{0xEF, 0xBB, 0xBF}), "csv: HTTP %d, BOM var mı: %v", c.code, bytes.HasPrefix(c.raw, []byte{0xEF, 0xBB, 0xBF}))
	f.check(lines == len(e.sn.exams), "csv %d satır, takvimde %d sınav", lines, len(e.sn.exams))
	bad := e.raw(ctx, fmt.Sprintf("/schedules/%d/export?format=pdf", e.sched2), sec)
	f.check(bad.code == 422, "geçersiz biçim: HTTP %d (422)", bad.code)
	st := e.raw(ctx, fmt.Sprintf("/schedules/%d/export", e.sched2), e.tok["student"])
	f.check(st.code == 403, "öğrenci export: HTTP %d (403)", st.code)
	return fmt.Sprintf("xlsx (2 sayfa) + csv (%d satır = %d sınav), geçersiz biçim 422, öğrenci 403", lines, len(e.sn.exams))
}

// X-3: iCal — olay sayıları verilerle birebir.
func checkX3(ctx context.Context, e *env, f *failures) string {
	if !needSnap(e, f) {
		return ""
	}
	all := e.raw(ctx, fmt.Sprintf("/schedules/%d/calendar.ics", e.sched2), e.tok["lecturer"])
	n := strings.Count(string(all.raw), "BEGIN:VEVENT")
	f.check(all.code == 200 && n == len(e.sn.exams), "takvim ics: HTTP %d, %d olay, takvimde %d sınav", all.code, n, len(e.sn.exams))
	mine := e.raw(ctx, "/me/exams.ics", e.tok["student"])
	want := len(e.sn.studentExams()[e.studentID])
	got := strings.Count(string(mine.raw), "BEGIN:VEVENT")
	f.check(mine.code == 200 && got == want, "öğrenci ics: HTTP %d, %d olay, programda %d sınav", mine.code, got, want)
	f.check(strings.Contains(string(mine.raw), "Koltuk "), "öğrenci ics koltuk bilgisi içermiyor")
	f.check(strings.Contains(string(mine.raw), "\r\n"), "ics CRLF kullanmıyor")
	d := e.raw(ctx, "/me/duties.ics", e.tok["invigilator"])
	f.check(d.code == 200 && strings.Contains(string(d.raw), "BEGIN:VCALENDAR"), "gözetmen ics: HTTP %d", d.code)
	x := e.raw(ctx, "/me/exams.ics", e.tok["invigilator"])
	f.check(x.code == 403, "gözetmen öğrenci ics: HTTP %d (403)", x.code)
	return fmt.Sprintf("takvim %d olay, öğrenci %d olay (salon+koltuk), gözetmen görevleri, rol denetimi", n, got)
}

// X-4: hızlı simülasyon kaydetmez, deterministiktir, doğrulanır.
func checkX4(ctx context.Context, e *env, f *failures) string {
	sec := e.tok["secretary"]
	count := func() int {
		l, _ := e.a.all(ctx, "/schedules", sec)
		return len(l)
	}
	before := count()
	body := M{"period_id": e.periodID, "seed": 11, "iterations": 30000}
	r1, err := e.a.post(ctx, "/schedules/simulate", sec, body)
	if !f.expectCode("simülasyon", r1, err, 200) {
		return ""
	}
	res := obj(r1.body, "result")
	f.check(boolean(r1.body, "saved") == false, "saved=true döndü")
	f.check(num(res, "violations") == 0 && num(res, "unplaced") == 0, "sert ihlal=%d yerleşemeyen=%d", num(res, "violations"), num(res, "unplaced"))
	r2, _ := e.a.post(ctx, "/schedules/simulate", sec, body)
	f.check(num(obj(r2.body, "result"), "penalty") == num(res, "penalty"), "aynı girdi farklı ceza: %d / %d", num(res, "penalty"), num(obj(r2.body, "result"), "penalty"))
	f.check(count() == before, "simülasyon sürüm oluşturdu (%d → %d)", before, count())
	for name, b := range map[string]M{"iterasyon 0": {"period_id": e.periodID, "iterations": 0}, "çok büyük": {"period_id": e.periodID, "iterations": 9999999}} {
		r, _ := e.a.post(ctx, "/schedules/simulate", sec, b)
		f.check(r.code == 422, "%s: HTTP %d (422)", name, r.code)
	}
	r, _ := e.a.post(ctx, "/schedules/simulate", e.tok["student"], body)
	f.check(r.code == 403, "öğrenci simülasyon: HTTP %d (403)", r.code)
	return fmt.Sprintf("kaydedilmedi (%d sürüm değişmedi), aynı girdi → ceza %d, ihlal 0, 422/403 denetimleri", before, num(res, "penalty"))
}

// X-5 + X-7: elle taşıma (reddet / yeni türev taslak) ve sürüm karşılaştırma.
func checkX5X7(ctx context.Context, e *env, f *failures) string {
	if !needSnap(e, f) {
		return ""
	}
	sec := e.tok["secretary"]
	// ortak öğrencili iki sınav: biri diğerinin slotuna taşınırsa çakışır.
	var a, b int64
	for _, exams := range e.sn.studentExams() {
		if len(exams) >= 2 {
			a, b = exams[0], exams[1]
			break
		}
	}
	slotOf := func(id int64) int64 { return num(e.sn.exams[id], "slot_id") }
	path := func(x int64) string { return fmt.Sprintf("/schedules/%d/exams/%d/move", e.sched2, x) }
	versions := func() int { l, _ := e.a.all(ctx, "/schedules", sec); return len(l) }
	n0 := versions()

	r, _ := e.a.post(ctx, path(a), sec, M{"slot_id": slotOf(b)})
	f.check(r.code == 422 && errCode(r) == "constraint_violation", "çakışan taşıma: HTTP %d %s (422 constraint_violation)", r.code, errCode(r))
	f.check(versions() == n0, "reddedilen taşıma sürüm oluşturdu")

	slots, err := e.a.get(ctx, fmt.Sprintf("/periods/%d/slots", e.periodID), sec)
	if !f.expectCode("slotlar", slots, err, 200) {
		return ""
	}
	var target int64
	for _, it := range arr(slots.body, "items") {
		id := num(it.(M), "id")
		if id == slotOf(a) {
			continue
		}
		d, _ := e.a.post(ctx, path(a), sec, M{"slot_id": id, "dry_run": true})
		if boolean(d.body, "ok") {
			target = id
			break
		}
	}
	if target == 0 {
		f.check(false, "geçerli taşıma hedefi bulunamadı")
		return ""
	}
	mv, err := e.a.post(ctx, path(a), sec, M{"slot_id": target})
	if !f.expectCode("taşıma", mv, err, 201) {
		return ""
	}
	nid := num(mv.body, "schedule_id")
	f.check(num(mv.body, "derived_from") == e.sched2, "derived_from=%d", num(mv.body, "derived_from"))
	f.check(versions() == n0+1, "tam 1 yeni sürüm bekleniyordu")

	// yeni sürüm: ihlalsiz taslak, yalnız o sınav taşınmış (bağımsız karşılaştırma uç noktasıyla da)
	nsn, err := loadSnapshot(ctx, e.a, sec, nid)
	if err != nil {
		f.check(false, "yeni sürüm okunamadı: %v", err)
		return ""
	}
	moved := 0
	for id, ex := range e.sn.exams {
		if num(nsn.exams[id], "slot_id") != num(ex, "slot_id") {
			moved++
		}
	}
	f.check(moved == 1 && num(nsn.exams[a], "slot_id") == target, "taşınan sınav sayısı %d, hedef slotta mı: %v", moved, num(nsn.exams[a], "slot_id") == target)
	nsn.checkSeating(f)
	nsn.checkInvigilation(f)
	cmp, err := e.a.get(ctx, fmt.Sprintf("/schedules/compare?a=%d&b=%d", e.sched2, nid), sec)
	if f.expectCode("karşılaştırma", cmp, err, 200) {
		f.check(num(cmp.body, "moved_count") == 1, "karşılaştırma %d taşınan sınav gösterdi (1 bekleniyor)", num(cmp.body, "moved_count"))
	}
	same, _ := e.a.get(ctx, fmt.Sprintf("/schedules/compare?a=%d&b=%d", e.sched2, e.sched2), sec)
	f.check(same.code == 200 && num(same.body, "moved_count") == 0, "aynı sürüm karşılaştırması 0 olmalı")
	for _, role := range []string{"lecturer", "student", "affairs"} {
		r, _ := e.a.post(ctx, path(a), e.tok[role], M{"slot_id": target})
		f.check(r.code == 403, "%s taşıma: HTTP %d (403)", role, r.code)
	}
	return fmt.Sprintf("çakışan taşıma 422 (sürüm sayısı değişmedi); geçerli taşıma → sürüm #%d (taslak, ihlal 0, yalnız 1 sınav farklı); karşılaştırma 1 taşınan", nid)
}

// X-6: etkinlik akışı gerçek denetim kaydından gelir.
func checkX6(ctx context.Context, e *env, f *failures) string {
	r, err := e.a.get(ctx, "/activity?limit=100", e.tok["affairs"])
	if !f.expectCode("etkinlik akışı", r, err, 200) {
		return ""
	}
	seen := map[string]bool{}
	var prev int64 = 1 << 62
	for _, it := range arr(r.body, "items") {
		m := it.(M)
		seen[str(m, "action")] = true
		f.check(num(m, "id") < prev, "akış yeniden eskiye sıralı değil")
		prev = num(m, "id")
	}
	for _, want := range []string{"schedule.create", "schedule.publish", "excuse.decide"} {
		f.check(seen[want], "akışta %q yok", want)
	}
	for _, role := range []string{"student", "lecturer", "invigilator"} {
		r, _ := e.a.get(ctx, "/activity", e.tok[role])
		f.check(r.code == 403, "%s akış: HTTP %d (403)", role, r.code)
	}
	return fmt.Sprintf("%d olay türü, yeniden eskiye; öğrenci/hoca/gözetmen 403", len(seen))
}

// L-1: canlı özet, bağımsız kaynaklarla (oturma planı + sınav bazlı yoklama uçları) birebir tutarlı.
func checkL1(ctx context.Context, e *env, f *failures) string {
	if !needSnap(e, f) {
		return ""
	}
	sec := e.tok["secretary"]
	// en erken oturum
	var slot int64
	var first string
	for _, ex := range e.sn.exams {
		if st := str(ex, "starts_at"); first == "" || st < first {
			first, slot = st, num(ex, "slot_id")
		}
	}
	// bu oturumda kontrollü yoklama üret: 3 geldi + 1 gelmedi; canlı özet bunu yansıtmalı
	for id, ex := range e.sn.exams {
		if num(ex, "slot_id") != slot || len(e.sn.byExam[id]) < 4 {
			continue
		}
		now := time.Now()
		var evs []M
		for i, row := range e.sn.byExam[id][:4] {
			st := "present"
			if i == 3 {
				st = "absent"
			}
			evs = append(evs, M{"event_id": uuidv7(now), "device_id": "verify-l1", "exam_id": id, "student_id": num(row, "student_id"), "status": st, "source": "manual", "hlc": M{"wall": now.UnixMilli(), "counter": i}})
		}
		if sy, err := e.a.post(ctx, "/attendance/sync", e.tok["invigilator"], M{"events": evs}); !f.expectCode("yoklama üret", sy, err, 200) {
			return ""
		}
		break
	}
	r, err := e.a.get(ctx, fmt.Sprintf("/schedules/%d/live?slot_id=%d", e.sched2, slot), sec)
	if !f.expectCode("canlı özet", r, err, 200) {
		return ""
	}
	wantSeated, wantPresent, wantAbsent := 0, 0, 0
	for id, ex := range e.sn.exams {
		if num(ex, "slot_id") != slot {
			continue
		}
		wantSeated += len(e.sn.byExam[id])
		rows, err := e.a.all(ctx, fmt.Sprintf("/exams/%d/attendance", id), sec)
		if err != nil {
			f.check(false, "yoklama okunamadı: %v", err)
			return ""
		}
		for _, row := range rows {
			switch str(row, "status") {
			case "present":
				wantPresent++
			case "absent":
				wantAbsent++
			}
		}
	}
	tot := obj(r.body, "totals")
	f.check(int(num(tot, "seated")) == wantSeated, "oturan %d, oturma planında %d", num(tot, "seated"), wantSeated)
	f.check(wantPresent >= 3 && wantAbsent >= 1, "kontrollü yoklama okunamadı (gelen %d, gelmedi %d)", wantPresent, wantAbsent)
	f.check(int(num(tot, "present")) == wantPresent, "gelen %d, yoklama uçlarında %d", num(tot, "present"), wantPresent)
	f.check(int(num(tot, "absent")) == wantAbsent, "gelmedi %d, yoklama uçlarında %d", num(tot, "absent"), wantAbsent)
	f.check(int(num(tot, "unmarked")) == wantSeated-wantPresent-wantAbsent, "işaretsiz tutarsız")
	rooms := arr(r.body, "rooms")
	sum := 0
	for _, it := range rooms {
		m := it.(M)
		sum += int(num(m, "seated"))
		f.check(len(arr(m, "invigilators")) >= 1, "salon %s gözetmensiz", str(m, "room_code"))
	}
	f.check(sum == wantSeated && int(num(tot, "rooms")) == len(rooms), "salon toplamı %d ≠ %d", sum, wantSeated)
	ev, err := e.a.get(ctx, fmt.Sprintf("/schedules/%d/live/events?slot_id=%d&limit=5", e.sched2, slot), sec)
	f.expectCode("olay akışı", ev, err, 200)
	// artımlı etkinlik akışı
	a1, _ := e.a.get(ctx, "/activity?limit=3", sec)
	latest := num(a1.body, "latest_id")
	a2, _ := e.a.get(ctx, fmt.Sprintf("/activity?after_id=%d", latest), sec)
	f.check(a2.code == 200 && len(arr(a2.body, "items")) == 0, "after_id=latest boş olmalı")
	for _, role := range []string{"lecturer", "invigilator", "student"} {
		x, _ := e.a.get(ctx, fmt.Sprintf("/schedules/%d/live", e.sched2), e.tok[role])
		f.check(x.code == 403, "%s canlı özet: HTTP %d (403)", role, x.code)
	}
	return fmt.Sprintf("oturum %d: oturan %d = oturma planı, gelen %d / gelmedi %d = yoklama uçları; %d salon, hepsinde gözetmen", slot, wantSeated, wantPresent, wantAbsent, len(rooms))
}

// Q-1: dinamik QR — kısa ömürlü jeton, yenileme eskisini iptal eder.
func checkQ1(ctx context.Context, e *env, f *failures) string {
	_, exam := e.studentPicks(ctx, f)
	if exam == 0 {
		return ""
	}
	path := fmt.Sprintf("/me/exams/%d/card", exam)
	a, err := e.a.post(ctx, path, e.tok["student"], nil)
	if !f.expectCode("kart 1", a, err, 201) {
		return ""
	}
	b, _ := e.a.post(ctx, path, e.tok["student"], nil)
	ttl := num(b.body, "ttl_seconds")
	f.check(ttl > 0 && ttl <= 3600, "ttl_seconds=%d (1..3600 bekleniyor)", ttl)
	f.check(str(a.body, "qr_token") != str(b.body, "qr_token"), "jeton yenilenmedi")
	scan := func(tok, key string) resp {
		r, _ := e.a.post(ctx, "/attendance/qr", e.tok["invigilator"], M{"token": tok, "device_id": "verify-q1"}, "Idempotency-Key", key)
		return r
	}
	old := scan(str(a.body, "qr_token"), fmt.Sprintf("q1-old-%d", e.runID))
	f.check(old.code == 409 && errCode(old) == "qr_expired", "yenilenen kartın eski jetonu: HTTP %d %s (409 qr_expired)", old.code, errCode(old))
	cur := scan(str(b.body, "qr_token"), fmt.Sprintf("q1-cur-%d", e.runID))
	f.check(cur.code == 200, "güncel jeton: HTTP %d (200)", cur.code)
	return fmt.Sprintf("jeton ömrü %d sn; yenileme eski jetonu iptal eder (409 qr_expired), güncel jeton 200", ttl)
}

// FAK-1: fakülte → bölüm → sınıf yapısı, hoca adları, takvim satırı alanları ve kapsamlı sınav provası.
func checkFak1(ctx context.Context, e *env, f *failures) string {
	sec := e.tok["secretary"]
	r, err := e.a.get(ctx, "/faculties", sec)
	if !f.expectCode("fakülteler", r, err, 200) {
		return ""
	}
	items := arr(r.body, "items")
	f.check(len(items) == 4, "%d fakülte (4 bekleniyordu)", len(items))
	wantDepts := map[string][]string{
		"Mühendislik Fakültesi":                {"Yazılım Mühendisliği", "Elektrik Mühendisliği", "Bilgisayar Mühendisliği"},
		"İktisadi ve İdari Bilimler Fakültesi": {"Yönetim Bilişim Sistemleri", "Ekonometri", "Maliye", "Tarih", "Uluslararası İlişkiler"},
		"Sağlık Bilimleri Fakültesi":           {"Hemşirelik", "Eczacılık", "Ebelik", "Hasta Bakım"},
	}
	depts, students := 0, 0
	var f1, d1 M
	for i, it := range items {
		fm := it.(M)
		name := str(fm, "name")
		ds := arr(fm, "departments")
		depts += len(ds)
		for _, d := range ds {
			students += int(num(d.(M), "students"))
			// F-1 (içe aktarma) aynı koşuda ders/öğrenci ekleyebildiğinden alt sınır denetlenir
			f.check(len(arr(d.(M), "years")) == 4 && num(d.(M), "courses") >= 20, "%s: %d sınıf, %d ders (4 sınıf × ≥5 ders)", str(d.(M), "name"), len(arr(d.(M), "years")), num(d.(M), "courses"))
		}
		if want, ok := wantDepts[name]; ok {
			for j, w := range want {
				f.check(j < len(ds) && str(ds[j].(M), "name") == w, "%s bölüm %d: beklenen %q", name, j, w)
			}
		} else {
			f.check(name == "Spor Bilimleri Fakültesi" && len(ds) == 3, "spor bilimleri 3 bölümlü olmalı (%q, %d)", name, len(ds))
		}
		lec := arr(fm, "lecturers")
		if i == 0 {
			f1, d1 = fm, ds[0].(M)
			want := []string{"Fahrettin Burak DEMİR", "Ömer ASLAN", "Abdullah Elen", "Buket Toptaş", "Serhat KILIÇARSLAN", "Süleyman Gökhan TAŞKIN", "Mehmet Ali Şimşek", "Murat TOPTAŞ"}
			f.check(len(lec) == 8, "mühendislik %d öğretim üyesi (8)", len(lec))
			for j, w := range want {
				f.check(j < len(lec) && str(lec[j].(M), "full_name") == w, "mühendislik öğretim üyesi %d: beklenen %q", j, w)
			}
		} else {
			f.check(len(lec) >= 4 && len(lec) <= 6, "%s: %d öğretim üyesi (4–6)", name, len(lec))
		}
	}
	f.check(depts == 15 && students >= 8000, "%d bölüm, %d öğrenci (15 / ≥8000)", depts, students)

	for id, ex := range e.sn.exams {
		f.check(str(ex, "faculty_name") != "" && str(ex, "department_name") != "" && num(ex, "year") >= 1 && num(ex, "year") <= 4, "sınav %d takvim satırında fakülte/bölüm/sınıf eksik", id)
	}
	run := func(scope M) (resp, int64) {
		x, _ := e.a.post(ctx, "/schedules/simulate", sec, M{"period_id": e.periodID, "seed": 5, "iterations": 5000, "scope": scope})
		return x, num(obj(x.body, "scope"), "exams")
	}
	_, all := run(M{})
	_, fac := run(M{"faculty_id": num(f1, "id")})
	_, dep := run(M{"faculty_id": num(f1, "id"), "department_ids": []any{num(d1, "id")}})
	one, y1 := run(M{"faculty_id": num(f1, "id"), "department_ids": []any{num(d1, "id")}, "years": []int{1}})
	// ilk kurulumdaki demo dersi (YZM406, Yazılım Müh. 4. sınıf) ilk fakülte/bölümde +1 sınav ekler
	extra := int64(0)
	for _, ex := range e.sn.exams {
		if str(ex, "course_code") == "YZM406" {
			extra = 1
		}
	}
	f.check(all == 300+extra && fac == 60+extra && dep == 20+extra && y1 == 5, "kapsam sınav sayıları %d/%d/%d/%d (%d/%d/%d/5 bekleniyordu)", all, fac, dep, y1, 300+extra, 60+extra, 20+extra)
	f.check(num(obj(one.body, "result"), "violations") == 0, "kapsamlı prova ihlalli")
	bad, _ := run(M{"faculty_id": num(f1, "id"), "department_ids": []any{9999}})
	f.check(bad.code == 422, "olmayan bölüm: HTTP %d (422)", bad.code)
	return fmt.Sprintf("4 fakülte, 15 bölüm (her biri 4 sınıf × 5 ders), mühendislik 8 adlı öğretim üyesi; prova kapsamı 300→60→20→5 sınav, ihlal 0")
}

// CHK-1: gözetmen "Yeni QR oluştur" → öğrenci QR/6 haneli kodla kendini yoklamaya yazar.
func checkCheckin1(ctx context.Context, e *env, f *failures) string {
	d, err := e.a.get(ctx, "/me/duties?limit=200", e.tok["invigilator"])
	if !f.expectCode("görevler", d, err, 200) {
		return ""
	}
	var duty M
	for _, x := range arr(d.body, "items") {
		m := x.(M)
		if t, err := time.Parse(time.RFC3339, str(m, "starts_at")); err == nil && !t.After(time.Now()) && time.Since(t) < 90*time.Minute {
			duty = m
			break
		}
	}
	if duty == nil {
		return "atlandı: şu an süren gözetmen görevi yok (DEMO_EXAM=off); go test ./internal/httpapi -run Checkin kapsıyor"
	}
	body := M{"slot_id": num(duty, "slot_id"), "room_id": num(duty, "room_id")}
	r, _ := e.a.post(ctx, "/checkin-sessions", e.tok["student"], body)
	f.check(r.code == 403, "öğrenci oturum açamaz: HTTP %d (403)", r.code)
	s, err := e.a.post(ctx, "/checkin-sessions", e.tok["invigilator"], body)
	if !f.expectCode("oturum aç", s, err, 201) {
		return ""
	}
	id, token, code := num(s.body, "id"), str(s.body, "token"), str(s.body, "code")
	f.check(len(code) == 6 && strings.HasPrefix(token, "SP1."), "kod/jeton biçimi: %q %q", code, token)
	bad := "000000"
	if code == bad {
		bad = "000001"
	}
	r, _ = e.a.post(ctx, "/me/checkin", e.tok["student"], M{"code": bad})
	f.check(r.code == 422 && errCode(r) == "checkin_bad_code", "yanlış kod: HTTP %d %s (422 checkin_bad_code)", r.code, errCode(r))
	r, _ = e.a.post(ctx, "/me/checkin", e.tok["student"], M{"token": token})
	f.check(r.code == 200 && num(r.body, "exam_id") > 0, "QR ile yoklama: HTTP %d (200) %s", r.code, errCode(r))
	exam := num(r.body, "exam_id")
	rot, _ := e.a.post(ctx, fmt.Sprintf("/checkin-sessions/%d/code", id), e.tok["invigilator"], nil)
	newCode := str(rot.body, "code")
	f.check(rot.code == 200 && len(newCode) == 6, "kod yenileme: HTTP %d", rot.code)
	if newCode != code {
		r, _ = e.a.post(ctx, "/me/checkin", e.tok["student"], M{"code": code})
		f.check(r.code == 422, "eski kod geçersiz: HTTP %d (422)", r.code)
	}
	r, _ = e.a.post(ctx, "/me/checkin", e.tok["student"], M{"code": newCode})
	f.check(r.code == 200 && str(r.body, "method") == "code", "kodla yoklama: HTTP %d (200)", r.code)
	att, _ := e.attendanceOf(ctx, exam, e.studentID)
	f.check(str(att, "status") == "present", "yoklamada öğrenci: %q (present bekleniyordu)", str(att, "status"))
	c, _ := e.a.post(ctx, fmt.Sprintf("/checkin-sessions/%d/close", id), e.tok["invigilator"], nil)
	f.check(c.code == 204, "oturum kapatma: HTTP %d (204)", c.code)
	r, _ = e.a.post(ctx, "/me/checkin", e.tok["student"], M{"token": token})
	f.check(r.code == 409 && errCode(r) == "checkin_closed", "kapalı oturum: HTTP %d %s (409 checkin_closed)", r.code, errCode(r))
	return fmt.Sprintf("oturum %d: QR + 6 haneli kod → yoklamada 'geldi'; yanlış kod 422, eski kod 422, kapalı oturum 409, öğrenci oturum açamaz 403", id)
}
