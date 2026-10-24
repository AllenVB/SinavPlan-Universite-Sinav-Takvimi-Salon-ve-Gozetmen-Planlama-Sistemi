package main

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
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
