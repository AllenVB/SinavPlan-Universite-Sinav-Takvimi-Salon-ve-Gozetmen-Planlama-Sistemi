package main

import (
	"context"
	"fmt"
	"sort"
)

func needSnap(e *env, f *failures) bool {
	if e.sn == nil {
		f.check(false, "çizelge yok (F-3 başarısız) — atlandı")
		return false
	}
	return true
}

// T-1: hiçbir öğrencinin aynı slotta iki sınavı yok, günde ≤2 sınav; çakışma uç noktası
// bağımsız hesapla aynı sayıyı veriyor.
func checkT1(ctx context.Context, e *env, f *failures) string {
	if !needSnap(e, f) {
		return ""
	}
	sn := e.sn
	for id, ex := range sn.exams {
		f.check(num(ex, "slot_id") > 0, "sınav %d slotsuz", id)
	}
	multi := 0
	for sid, exams := range sn.studentExams() {
		slots := map[int64]bool{}
		perDay := map[int64]int{}
		for _, x := range exams {
			slot := num(sn.exams[x], "slot_id")
			f.check(!slots[slot], "öğrenci %d slot %d'de birden çok sınavda", sid, slot)
			slots[slot] = true
			perDay[sn.slotDay[slot]]++
		}
		for d, n := range perDay {
			f.check(n <= 2, "öğrenci %d gün %d'de %d sınav (en çok 2)", sid, d, n)
			if n >= 2 {
				multi++
			}
		}
	}
	all, err := e.a.all(ctx, fmt.Sprintf("/schedules/%d/conflicts", e.sched2), e.tok["secretary"])
	if err != nil {
		f.check(false, "çakışma listesi: %v", err)
	} else {
		f.check(len(all) == multi, "çakışma uç noktası %d satır, bağımsız hesap %d", len(all), multi)
	}
	return fmt.Sprintf("%d sınav, %d öğrenci; aynı slot çakışması 0, günde 2 sınav olan öğrenci-gün=%d (uç nokta ile eşleşti)", len(sn.exams), len(sn.studentExams()), multi)
}

// T-2: salon kapasitesi, lab/klasik uyumu, salon payları ile oturma planının tutarlılığı.
func checkT2(_ context.Context, e *env, f *failures) string {
	if !needSnap(e, f) {
		return ""
	}
	sn := e.sn
	perRoom := sn.roomStudents()
	for k, n := range perRoom {
		rm := sn.rooms[k.room]
		f.check(rm != nil && int64(n) <= num(rm, "capacity"), "slot %d salon %d: %d öğrenci > kapasite %d", k.slot, k.room, n, num(rm, "capacity"))
	}
	type trip struct{ slot, room, exam int64 }
	share := map[trip]int{}
	for exam, rows := range sn.byExam {
		ex := sn.exams[exam]
		f.check(len(rows) == int(num(ex, "students")), "sınav %d: takvimde %d öğrenci, oturmada %d", exam, num(ex, "students"), len(rows))
		rooms := map[int64]bool{}
		for _, r := range rows {
			rooms[num(r, "room_id")] = true
			share[trip{num(ex, "slot_id"), num(r, "room_id"), exam}]++
			f.check(boolean(ex, "requires_lab") == (str(sn.rooms[num(r, "room_id")], "type") == "lab"),
				"sınav %d (lab=%v) salon %d tipi %s", exam, boolean(ex, "requires_lab"), num(r, "room_id"), str(sn.rooms[num(r, "room_id")], "type"))
		}
		f.check(len(rooms) == int(num(ex, "rooms")), "sınav %d: takvimde %d salon, oturmada %d", exam, num(ex, "rooms"), len(rooms))
	}
	got := 0
	for slot, list := range sn.slotRooms {
		for _, sr := range list {
			t := trip{slot, num(sr, "room_id"), num(sr, "exam_id")}
			f.check(share[t] == int(num(sr, "students")), "slot %d salon %d sınav %d: pay %d, oturma %d", slot, t.room, t.exam, num(sr, "students"), share[t])
			got++
		}
	}
	f.check(got == len(share), "salon payı satırı %d, oturmadan türeyen %d", got, len(share))
	return fmt.Sprintf("%d (slot,salon) kapasite içinde; %d pay satırı oturma planıyla birebir; lab/klasik uyumlu", len(perRoom), got)
}

// T-3: komşu koltukta aynı sınav yok, koltuk tekil, engelli öğrenci erişilebilir koltukta.
func checkT3(_ context.Context, e *env, f *failures) string {
	if !needSnap(e, f) {
		return ""
	}
	n := e.sn.checkSeating(f)
	return fmt.Sprintf("%d oturma satırı: yan yana aynı sınav 0, çift koltuk 0, engelli→erişilebilir koltuk ihlali 0", n)
}

func (sn *snapshot) checkSeating(f *failures) int {
	type seat struct{ slot, room, row, col int64 }
	used := map[seat]int64{}
	byRow := map[[3]int64][]M{} // slot, room, row
	disabled := 0
	for exam, rows := range sn.byExam {
		slot := num(sn.exams[exam], "slot_id")
		seen := map[int64]bool{}
		for _, r := range rows {
			s := seat{slot, num(r, "room_id"), num(r, "seat_row"), num(r, "seat_col")}
			if prev, ok := used[s]; ok {
				f.check(false, "slot %d salon %d koltuk (%d,%d) iki öğrenciye (%d,%d)", slot, s.room, s.row, s.col, prev, num(r, "student_id"))
			}
			used[s] = num(r, "student_id")
			f.check(!seen[num(r, "student_id")], "öğrenci %d sınav %d'de iki koltukta", num(r, "student_id"), exam)
			seen[num(r, "student_id")] = true
			rm := sn.rooms[s.room]
			f.check(rm != nil && s.row < num(rm, "rows") && s.col < num(rm, "cols"), "koltuk salon sınırı dışında: salon %d (%d,%d)", s.room, s.row, s.col)
			if boolean(r, "disabled_student") {
				disabled++
				f.check(boolean(r, "accessible"), "engelli öğrenci %d erişilebilir olmayan koltukta (salon %d, %d,%d)", num(r, "student_id"), s.room, s.row, s.col)
			}
			k := [3]int64{slot, s.room, s.row}
			rr := M{"col": float64(s.col), "exam": float64(exam)}
			byRow[k] = append(byRow[k], rr)
		}
	}
	for k, list := range byRow {
		sort.Slice(list, func(i, j int) bool { return num(list[i], "col") < num(list[j], "col") })
		for i := 1; i < len(list); i++ {
			adj := num(list[i], "col") == num(list[i-1], "col")+1
			f.check(!(adj && num(list[i], "exam") == num(list[i-1], "exam")), "slot %d salon %d sıra %d: sınav %d yan yana (sütun %d,%d)", k[0], k[1], k[2], num(list[i], "exam"), num(list[i-1], "col"), num(list[i], "col"))
		}
	}
	f.check(disabled > 0, "veri setinde engelli öğrenci görünmüyor; kontrol anlamsız")
	return len(used)
}

// F-4: salon/oturma planı, öğrencinin kendi programında (salon+koltuk) görünüyor.
func checkF4(ctx context.Context, e *env, f *failures) string {
	if !needSnap(e, f) {
		return ""
	}
	r, err := e.a.get(ctx, "/me/exams", e.tok["student"])
	if !f.expectCode("/me/exams", r, err, 200) {
		return ""
	}
	mine := e.sn.studentExams()[e.studentID]
	items := arr(r.body, "items")
	f.check(len(items) == len(mine) && len(mine) > 0, "öğrenci programı %d sınav, planda %d", len(items), len(mine))
	seat := map[int64]M{}
	for _, row := range e.sn.seats {
		if num(row, "student_id") == e.studentID {
			seat[num(row, "exam_id")] = row
		}
	}
	for _, it := range items {
		m := it.(M)
		row := seat[num(m, "exam_id")]
		f.check(row != nil && str(m, "room_code") == str(e.sn.rooms[num(row, "room_id")], "code") &&
			num(m, "seat_row") == num(row, "seat_row") && num(m, "seat_col") == num(row, "seat_col"),
			"sınav %d: öğrenci ekranı %v/%v/%v, plan %v", num(m, "exam_id"), m["room_code"], m["seat_row"], m["seat_col"], row != nil)
	}
	return fmt.Sprintf("öğrenci %d: %d sınavın tamamında salon+koltuk plan ile aynı", e.studentID, len(items))
}

// T-4: gözetmen kısıtları + adalet.
func checkT4(_ context.Context, e *env, f *failures) string {
	if !needSnap(e, f) {
		return ""
	}
	if len(e.sn.duties) == 0 {
		f.check(false, "görev listesi boş")
		return ""
	}
	lo, hi := e.sn.checkInvigilation(f)
	f.check(hi-lo <= 2, "yük farkı %d (en çok 2 bekleniyor; min %d, max %d)", hi-lo, lo, hi)
	return fmt.Sprintf("%d görev; aynı slotta tek görev, ardışık oturum yok, >50 öğrenci→2 gözetmen; yük min %d / max %d", len(e.sn.duties), lo, hi)
}

// F-5: gözetmen çıkarma — minimum değişiklik, kilitli diğer atamalar, idempotent.
func checkF5(ctx context.Context, e *env, f *failures) string {
	if !needSnap(e, f) {
		return ""
	}
	sn := e.sn
	load := map[int64]int{}
	for _, d := range sn.duties {
		load[num(d, "user_id")]++
	}
	var victim int64
	for u, n := range load {
		if u == e.userID["invigilator"] {
			continue
		}
		if n > load[victim] || (n == load[victim] && u < victim) {
			victim = u
		}
	}
	before := dutySet(sn.duties)
	path := fmt.Sprintf("/schedules/%d/invigilators/%d/remove", e.sched2, victim)
	key := fmt.Sprintf("vrf-remove-%d-%d-%d", e.sched2, victim, e.runID)
	r, err := e.a.post(ctx, path, e.tok["secretary"], nil, "Idempotency-Key", key)
	if !f.expectCode("gözetmen çıkar", r, err, 200) {
		return ""
	}
	vac, sw, ch := num(r.body, "vacated"), num(r.body, "swaps"), len(arr(r.body, "changes"))
	f.check(int(vac) == load[victim], "boşalan %d, çıkarılanın görevi %d", vac, load[victim])
	f.check(num(r.body, "violations") == 0, "çıkarma sonrası ihlal=%d", num(r.body, "violations"))
	f.check(len(arr(r.body, "unfilled")) == 0, "doldurulamayan görev var")
	f.check(int64(ch) == vac+sw, "değişiklik %d ≠ boşalan %d + takas %d", ch, vac, sw)
	rep, _ := e.a.post(ctx, path, e.tok["secretary"], nil, "Idempotency-Key", key)
	f.check(rep.code == 200 && string(rep.raw) == string(r.raw), "aynı anahtarla tekrar farklı yanıt (HTTP %d)", rep.code)

	if err := sn.reloadDuties(ctx, e.a, e.tok["secretary"]); err != nil {
		f.check(false, "görevler yeniden okunamadı: %v", err)
		return ""
	}
	after := dutySet(sn.duties)
	added := 0
	for k := range after {
		if !before[k] {
			added++
		}
	}
	f.check(added == ch, "plandaki yeni (slot,salon,gözetmen) %d, bildirilen değişiklik %d", added, ch)
	for _, d := range sn.duties {
		f.check(num(d, "user_id") != victim, "çıkarılan gözetmen %d hâlâ görevde", victim)
	}
	f.check(len(after) == len(before), "toplam görev %d → %d", len(before), len(after))
	sn.checkInvigilation(f)
	return fmt.Sprintf("gözetmen %d çıktı: %d boşalan, %d takas, %d değişiklik; ihlal 0, kalan atamalar kilitli, yanıt idempotent", victim, vac, sw, ch)
}

func dutySet(duties []M) map[[3]int64]bool {
	out := map[[3]int64]bool{}
	for _, d := range duties {
		out[[3]int64{num(d, "slot_id"), num(d, "room_id"), num(d, "user_id")}] = true
	}
	return out
}
