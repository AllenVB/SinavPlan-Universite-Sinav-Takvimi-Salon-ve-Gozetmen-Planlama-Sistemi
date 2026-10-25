package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// studentPicks: öğrencinin mazeret/QR için kullanılacak sınavları. Önceki koşularda
// onaylanmış mazeret alan sınavlar elenir; böylece koşu tekrarlanabilir.
func (e *env) studentPicks(ctx context.Context, f *failures) (excuseExam, cardExam int64) {
	me, err := e.a.get(ctx, "/me/exams", e.tok["student"])
	ex, err2 := e.a.get(ctx, "/me/excuses", e.tok["student"])
	if err != nil || err2 != nil || me.code != 200 || ex.code != 200 {
		f.check(false, "öğrenci programı/mazeretleri okunamadı")
		return
	}
	done := map[int64]bool{}
	for _, x := range arr(ex.body, "items") {
		if m := x.(M); str(m, "status") != "rejected" {
			done[num(m, "exam_id")] = true
		}
	}
	var free []int64
	for _, x := range arr(me.body, "items") {
		if id := num(x.(M), "exam_id"); !done[id] {
			free = append(free, id)
		}
	}
	sort.Slice(free, func(i, j int) bool { return free[i] < free[j] })
	if len(free) < 2 {
		f.check(false, "mazeretsiz sınavı 2'den az (%d); `make reset-demo` ile sıfırlayın", len(free))
		return
	}
	return free[0], free[len(free)-1]
}

func checkF6(ctx context.Context, e *env, f *failures) string {
	if !needSnap(e, f) {
		return ""
	}
	exam, cardExam := e.studentPicks(ctx, f)
	if exam == 0 {
		return ""
	}
	st, af := e.tok["student"], e.tok["affairs"]
	// geçersiz: hem exam hem slot
	bad, _ := e.a.post(ctx, "/excuses", st, M{"exam_id": exam, "slot_id": 1, "reason": "x"})
	f.check(bad.code == 422, "exam_id+slot_id birlikte: HTTP %d (422)", bad.code)
	r, err := e.a.post(ctx, "/excuses", st, M{"exam_id": exam, "reason": "Doğrulama: hastane raporu"})
	if !f.expectCode("öğrenci mazereti", r, err, 201) {
		return ""
	}
	id := num(r.body, "id")
	f.check(str(r.body, "status") == "pending", "yeni mazeret durumu %s", str(r.body, "status"))
	dup, _ := e.a.post(ctx, "/excuses", st, M{"exam_id": exam, "reason": "yinelenen"})
	f.check(dup.code == 409, "aynı sınav için ikinci mazeret: HTTP %d (409)", dup.code)
	pend, err := e.a.all(ctx, "/excuses?status=pending", af)
	found := false
	for _, x := range pend {
		found = found || num(x, "id") == id
	}
	f.check(err == nil && found, "bekleyen mazeret öğrenci işleri listesinde yok")

	path := fmt.Sprintf("/excuses/%d/decision", id)
	key := fmt.Sprintf("vrf-decide-%d-%d", id, e.runID)
	d1, err := e.a.post(ctx, path, af, M{"decision": "approved", "note": "Rapor doğrulandı"}, "Idempotency-Key", key)
	if f.expectCode("karar", d1, err, 200) {
		f.check(str(d1.body, "status") == "approved", "karar sonrası durum %s", str(d1.body, "status"))
	}
	d2, _ := e.a.post(ctx, path, af, M{"decision": "approved", "note": "Rapor doğrulandı"}, "Idempotency-Key", key)
	f.check(d2.code == 200 && string(d1.raw) == string(d2.raw), "karar tekrarı (aynı anahtar): HTTP %d / farklı yanıt", d2.code)
	d3, _ := e.a.post(ctx, path, af, M{"decision": "rejected"}, "Idempotency-Key", key+"-b")
	f.check(d3.code == 409, "karara bağlanmış mazereti yeniden kararlama: HTTP %d (409)", d3.code)
	card, _ := e.a.post(ctx, fmt.Sprintf("/me/exams/%d/card", exam), st, nil)
	f.check(card.code == 409 && errCode(card) == "excused", "onaylı mazeretli sınav kartı: HTTP %d %s (409 excused)", card.code, errCode(card))
	okCard, _ := e.a.post(ctx, fmt.Sprintf("/me/exams/%d/card", cardExam), st, nil)
	f.check(okCard.code == 201, "mazeretsiz sınav kartı: HTTP %d (201)", okCard.code)

	// gözetmen mazereti: reddedilir ki sonraki çizelgeleri etkilemesin.
	duties, err := e.a.get(ctx, "/me/duties", e.tok["invigilator"])
	if f.expectCode("/me/duties", duties, err, 200) && len(arr(duties.body, "items")) > 0 {
		slot := num(arr(duties.body, "items")[0].(M), "slot_id")
		ir, err := e.a.post(ctx, "/excuses", e.tok["invigilator"], M{"slot_id": slot, "reason": "Doğrulama: başka görev"})
		if f.expectCode("gözetmen mazereti", ir, err, 201) {
			dd, err := e.a.post(ctx, fmt.Sprintf("/excuses/%d/decision", num(ir.body, "id")), af, M{"decision": "rejected", "note": "doğrulama koşusu"})
			f.expectCode("gözetmen mazereti reddi", dd, err, 200)
		}
	} else {
		f.check(false, "demo gözetmenin görevi yok")
	}
	cf, err := e.a.get(ctx, fmt.Sprintf("/schedules/%d/conflicts?limit=5", e.sched2), af)
	f.expectCode("çakışma listesi (öğrenci işleri)", cf, err, 200)
	return fmt.Sprintf("öğrenci mazereti: 422/201/409(yinelenen)/onay(idempotent)/ikinci karar 409/kart 409 excused; gözetmen mazereti reddi; sınav %d", exam)
}

func checkOwnership(ctx context.Context, e *env, f *failures) string {
	if !needSnap(e, f) {
		return ""
	}
	sn := e.sn
	sp := fmt.Sprintf("/schedules/%d", e.sched2)
	// öğrenci başkasının programına/kartına erişemez
	other := e.studentID + 1
	r, _ := e.a.get(ctx, fmt.Sprintf("/students/%d/exams", other), e.tok["student"])
	f.check(r.code == 403, "öğrenci başkasının programı: HTTP %d (403)", r.code)
	r, _ = e.a.get(ctx, fmt.Sprintf("/students/%d/exams", e.studentID), e.tok["student"])
	f.check(r.code == 200, "öğrenci kendi programı (/students/{id}/exams): HTTP %d (200)", r.code)
	mine := map[int64]bool{}
	for _, x := range sn.studentExams()[e.studentID] {
		mine[x] = true
	}
	var foreign int64
	for id := range sn.exams {
		if !mine[id] && (foreign == 0 || id < foreign) {
			foreign = id
		}
	}
	r, _ = e.a.post(ctx, fmt.Sprintf("/me/exams/%d/card", foreign), e.tok["student"], nil)
	f.check(r.code == 404, "kayıtlı olmadığı sınavın kartı: HTTP %d (404)", r.code)

	// öğretim üyesi yalnız kendi derslerinin oturma planını görür
	ids := make([]int64, 0, len(sn.exams))
	for id := range sn.exams {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if len(ids) > 60 {
		ids = ids[:60]
	}
	ok, denied := 0, 0
	for _, id := range ids {
		r, _ := e.a.get(ctx, fmt.Sprintf("%s/seating?exam_id=%d&limit=1", sp, id), e.tok["lecturer"])
		switch r.code {
		case 200:
			ok++
		case 403:
			denied++
		default:
			f.check(false, "öğretim üyesi sınav %d: HTTP %d", id, r.code)
		}
	}
	f.check(ok > 0 && denied > 0, "öğretim üyesi: %d sınav 200, %d sınav 403 (ikisi de >0 bekleniyor)", ok, denied)

	// gözetmen yalnız görevli olduğu sınavın planını görür
	d, err := e.a.get(ctx, "/me/duties", e.tok["invigilator"])
	// Erişim kuralı salon bazlıdır: görevli olduğu (slot, salon)daki TÜM sınavların planını görür
	// (karma salonda birden çok sınav olabilir; görev satırı yalnız birini gösterir).
	own := map[int64]bool{}
	if err == nil {
		for _, x := range arr(d.body, "items") {
			m := x.(M)
			for _, sr := range sn.slotRooms[num(m, "slot_id")] {
				if num(sr, "room_id") == num(m, "room_id") {
					own[num(sr, "exam_id")] = true
				}
			}
		}
	}
	f.check(len(own) > 0, "demo gözetmenin görevi yok")
	oks, denies := 0, 0
	for _, id := range ids {
		r, _ := e.a.get(ctx, fmt.Sprintf("%s/seating?exam_id=%d&limit=1", sp, id), e.tok["invigilator"])
		if own[id] {
			f.check(r.code == 200, "gözetmen kendi sınavı %d: HTTP %d", id, r.code)
			oks++
		} else if r.code == 200 && strings.Contains(str(sn.exams[id], "lecturer_name"), "Ömer ASLAN") {
			// demo gözetmen aynı zamanda öğretim üyesidir: kendi dersinin planını görmesi doğru
			continue
		} else if r.code == 403 {
			denies++
		} else if len(own) > 0 {
			f.check(false, "gözetmen görevli olmadığı sınav %d: HTTP %d (403)", id, r.code)
		}
	}
	// taslak sürüm başkalarına görünmez (varlığı sızmaz)
	for _, p := range []string{fmt.Sprintf("/schedules/%d", e.sched1), fmt.Sprintf("/schedules/%d/exams", e.sched1)} {
		for _, role := range []string{"lecturer", "student", "invigilator"} {
			r, _ := e.a.get(ctx, p, e.tok[role])
			f.check(r.code == 404 || r.code == 403, "taslak %s (%s): HTTP %d", p, role, r.code)
		}
	}
	return fmt.Sprintf("öğrenci: başkasının programı 403, yabancı kart 404; hoca %d/%d sınav; gözetmen %d kendi, %d yabancı 403; taslak gizli", ok, ok+denied, oks, denies)
}

func (e *env) attendanceOf(ctx context.Context, exam, student int64) (M, error) {
	items, err := e.a.all(ctx, fmt.Sprintf("/exams/%d/attendance", exam), e.tok["secretary"])
	for _, it := range items {
		if num(it, "student_id") == student {
			return it, err
		}
	}
	return nil, err
}

func checkF7(ctx context.Context, e *env, f *failures) string {
	if !needSnap(e, f) {
		return ""
	}
	_, exam := e.studentPicks(ctx, f)
	if exam == 0 {
		return ""
	}
	inv, st, sid := e.tok["invigilator"], e.tok["student"], e.studentID
	cardPath := fmt.Sprintf("/me/exams/%d/card", exam)
	c, err := e.a.post(ctx, cardPath, st, nil)
	if !f.expectCode("sınav kartı", c, err, 201) {
		return ""
	}
	qr := str(c.body, "qr_token")
	key := "vrf-scan-" + qr[:min(len(qr), 24)]
	s1, err := e.a.post(ctx, "/attendance/qr", inv, M{"token": qr, "device_id": "vrf-tablet"}, "Idempotency-Key", key)
	if f.expectCode("QR okutma", s1, err, 200) {
		f.check(num(s1.body, "exam_id") == exam && num(s1.body, "student_id") == sid, "QR yanıtı yanlış öğrenci/sınav")
	}
	s1b, _ := e.a.post(ctx, "/attendance/qr", inv, M{"token": qr, "device_id": "vrf-tablet"}, "Idempotency-Key", key)
	f.check(s1b.code == 200 && string(s1b.raw) == string(s1.raw), "aynı anahtarla QR tekrarı: HTTP %d (aynı yanıt bekleniyor)", s1b.code)
	s2, _ := e.a.post(ctx, "/attendance/qr", inv, M{"token": qr, "device_id": "vrf-tablet"}, "Idempotency-Key", key+"-2")
	f.check(s2.code == 409 && errCode(s2) == "qr_already_used", "ikinci okutma: HTTP %d %s (409 qr_already_used)", s2.code, errCode(s2))
	a, _ := e.attendanceOf(ctx, exam, sid)
	f.check(a != nil && str(a, "status") == "present" && str(a, "source") == "qr", "yoklama durumu: %v", a)

	// çevrimdışı toplu senkron: yeni 2 olay kabul, tekrar gönderim → duplicates
	now := time.Now()
	evs := []M{
		{"event_id": uuidv7(now), "device_id": "vrf-offline-1", "exam_id": exam, "student_id": sid, "status": "present", "source": "manual", "hlc": M{"wall": now.UnixMilli(), "counter": 0}},
		{"event_id": uuidv7(now), "device_id": "vrf-offline-2", "exam_id": exam, "student_id": sid, "status": "present", "source": "manual", "hlc": M{"wall": now.UnixMilli(), "counter": 1}},
	}
	sy, err := e.a.post(ctx, "/attendance/sync", inv, M{"events": evs})
	if f.expectCode("sync", sy, err, 200) {
		f.check(num(sy.body, "accepted") == 2 && len(arr(sy.body, "rejected")) == 0, "sync: accepted=%d rejected=%v", num(sy.body, "accepted"), arr(sy.body, "rejected"))
	}
	sy2, _ := e.a.post(ctx, "/attendance/sync", inv, M{"events": evs})
	f.check(sy2.code == 200 && num(sy2.body, "accepted") == 0 && num(sy2.body, "duplicates") == 2, "sync tekrarı: accepted=%d duplicates=%d (0/2)", num(sy2.body, "accepted"), num(sy2.body, "duplicates"))
	bad, _ := e.a.post(ctx, "/attendance/sync", inv, M{"events": []M{}})
	f.check(bad.code == 422, "boş olay listesi: HTTP %d (422)", bad.code)

	// çakışma: gelecek HLC'li elle "absent" vs QR "present" → QR kazanır (qr_present)
	abs := M{"event_id": uuidv7(now), "device_id": "vrf-offline-3", "exam_id": exam, "student_id": sid, "status": "absent", "source": "manual",
		"hlc": M{"wall": now.Add(time.Hour).UnixMilli(), "counter": 0}}
	if r, err := e.a.post(ctx, "/attendance/sync", inv, M{"events": []M{abs}}); !f.expectCode("absent senkronu", r, err, 200) {
		return ""
	}
	c2, _ := e.a.post(ctx, cardPath, st, nil)
	qr2 := str(c2.body, "qr_token")
	s3, err := e.a.post(ctx, "/attendance/qr", inv, M{"token": qr2, "device_id": "vrf-tablet"}, "Idempotency-Key", "vrf-scan2-"+qr2[:min(len(qr2), 24)])
	f.expectCode("ikinci kart okutma", s3, err, 200)
	a, _ = e.attendanceOf(ctx, exam, sid)
	f.check(a != nil && str(a, "status") == "present", "QR ile elle-absent çakışmasında durum %v (present bekleniyor)", a["status"])
	cf, err := e.a.all(ctx, fmt.Sprintf("/attendance/conflicts?exam_id=%d", exam), e.tok["secretary"])
	rule := false
	for _, x := range cf {
		rule = rule || (num(x, "student_id") == sid && str(x, "rule") == "qr_present")
	}
	f.check(err == nil && rule, "qr_present kuralıyla çakışma kaydı yok (%d kayıt)", len(cf))
	sum, _ := e.a.get(ctx, fmt.Sprintf("/exams/%d/attendance?limit=1", exam), e.tok["secretary"])
	f.check(sum.code == 200 && obj(sum.body, "summary") != nil, "yoklama özeti yok")
	return fmt.Sprintf("kart→QR 200→tekrar 409; sync 2 kabul/tekrar 2 duplicate; QR≻elle-absent çakışması qr_present (sınav %d)", exam)
}

func checkT5(ctx context.Context, e *env, f *failures) string {
	if !needSnap(e, f) {
		return ""
	}
	_, exam := e.studentPicks(ctx, f)
	if exam == 0 {
		return ""
	}
	// 1) 20 eşzamanlı okutma, tek token → tam 1×200, 19×409
	c, err := e.a.post(ctx, fmt.Sprintf("/me/exams/%d/card", exam), e.tok["student"], nil)
	if !f.expectCode("kart", c, err, 201) {
		return ""
	}
	qr := str(c.body, "qr_token")
	const n = 20
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, _ := e.a.post(ctx, "/attendance/qr", e.tok["invigilator"], M{"token": qr, "device_id": fmt.Sprintf("vrf-par-%d", i)},
				"Idempotency-Key", fmt.Sprintf("vrf-par-%d-%d", e.runID, i))
			codes[i] = r.code
		}(i)
	}
	wg.Wait()
	ok, conflict := 0, 0
	for _, c := range codes {
		switch c {
		case 200:
			ok++
		case 409:
			conflict++
		}
	}
	f.check(ok == 1 && conflict == n-1, "%d eşzamanlı okutma: %d×200, %d×409 (1/%d bekleniyor) %v", n, ok, conflict, n-1, codes)

	// 2) Toplu salon değişikliği: önce kapasite yetmeyen hedef (409, hiçbir şey değişmez), sonra başarılı.
	sn, sec := e.sn, e.tok["secretary"]
	before := sn.fingerprint()
	type cand struct {
		slot, room int64
		n          int
		typ        string
	}
	var cands []cand
	for k, cnt := range sn.roomStudents() {
		cands = append(cands, cand{k.slot, k.room, cnt, str(sn.rooms[k.room], "type")})
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].n != cands[j].n {
			return cands[i].n < cands[j].n
		}
		return cands[i].slot*1_000_000+cands[i].room < cands[j].slot*1_000_000+cands[j].room
	})
	var big cand
	for _, c := range cands {
		if c.n >= 5 && c.typ == "classic" {
			big = c
			break
		}
	}
	tiny, err := e.a.post(ctx, "/rooms", sec, M{"code": fmt.Sprintf("VRT-%d", e.runID), "building": "Z", "floor": 1, "type": "classic", "rows": 1, "cols": 2})
	if f.expectCode("küçük hedef salon", tiny, err, 201) && big.n > 0 {
		r, err := e.a.post(ctx, fmt.Sprintf("/schedules/%d/room-change", e.sched2), sec,
			M{"slot_id": big.slot, "from_room_id": big.room, "to_room_id": num(tiny.body, "id"), "reason": "doğrulama: yetersiz kapasite"})
		f.check(err == nil && r.code == 409 && errCode(r) == "insufficient_capacity", "yetersiz kapasite: HTTP %d %s (409 insufficient_capacity)", r.code, errCode(r))
		d, _ := e.a.do(ctx, "DELETE", fmt.Sprintf("/rooms/%d", num(tiny.body, "id")), sec, "", nil)
		f.check(d.code == 204, "geçici salon silinemedi: HTTP %d", d.code)
	}

	var moved cand
	var target int64
	var res resp
	attempts := 0
	for _, c := range cands {
		if c.n < 5 || attempts >= 12 {
			continue
		}
		used := map[int64]bool{}
		for _, sr := range sn.slotRooms[c.slot] {
			used[num(sr, "room_id")] = true
		}
		var free int64
		for id, rm := range sn.rooms {
			if !used[id] && str(rm, "type") == c.typ && num(rm, "capacity") >= int64(c.n)*2 && (free == 0 || num(rm, "capacity") > num(sn.rooms[free], "capacity")) {
				free = id
			}
		}
		if free == 0 {
			continue
		}
		attempts++
		key := fmt.Sprintf("vrf-evac-%d-%d-%d", c.slot, c.room, e.runID)
		r, err := e.a.post(ctx, fmt.Sprintf("/schedules/%d/room-change", e.sched2), sec,
			M{"slot_id": c.slot, "from_room_id": c.room, "to_room_id": free, "reason": "doğrulama: tahliye"}, "Idempotency-Key", key)
		if err == nil && r.code == 200 {
			moved, target, res = c, free, r
			rep, _ := e.a.post(ctx, fmt.Sprintf("/schedules/%d/room-change", e.sched2), sec,
				M{"slot_id": c.slot, "from_room_id": c.room, "to_room_id": free, "reason": "doğrulama: tahliye"}, "Idempotency-Key", key)
			f.check(rep.code == 200 && string(rep.raw) == string(r.raw), "tahliye tekrarı (aynı anahtar): HTTP %d", rep.code)
			break
		}
	}
	if moved.n == 0 {
		f.check(false, "uygun tahliye hedefi bulunamadı/başarılı olmadı (%d deneme)", attempts)
		return ""
	}
	f.check(int(num(res.body, "moved")) == moved.n, "taşınan %d, salondaki öğrenci %d", num(res.body, "moved"), moved.n)

	// 3) tahliye sonrası bütün değişmezler hâlâ geçerli
	after, err := loadSnapshot(ctx, e.a, sec, e.sched2)
	if err != nil {
		f.check(false, "tahliye sonrası okuma: %v", err)
		return ""
	}
	f.check(after.fingerprint() != before, "tahliye hiçbir şeyi değiştirmedi")
	pr := after.roomStudents()
	f.check(pr[dutyKey{moved.slot, moved.room}] == 0, "kaynak salon boşalmadı")
	f.check(pr[dutyKey{moved.slot, target}] == moved.n, "hedef salon %d öğrenci, beklenen %d", pr[dutyKey{moved.slot, target}], moved.n)
	after.checkSeating(f)
	after.checkInvigilation(f)
	e.sn = after
	return fmt.Sprintf("%d eşzamanlı QR → 1×200+%d×409; yetersiz kapasite 409; tahliye %d öğrenci (idempotent), sonrası T-3/T-4 ihlali 0", n, conflict, moved.n)
}
