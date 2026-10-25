package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// snapshot, yayınlanmış çizelgenin API'den okunan tam görüntüsüdür. T-1..T-4
// kontrolleri çözücü kodunu kullanmaz, yalnızca bu veriden yeniden hesaplar.
type snapshot struct {
	scheduleID int64
	exams      map[int64]M // exam_id -> takvim satırı
	slotDay    map[int64]int64
	slotSess   map[int64]int64
	seats      []M           // tüm oturma satırları
	byExam     map[int64][]M // exam_id -> oturma satırları
	rooms      map[int64]M   // room_id -> salon
	duties     []M           // gözetmen görevleri
	slotRooms  map[int64][]M // slot_id -> salon payları
}

func loadSnapshot(ctx context.Context, a *api, tok string, scheduleID int64) (*snapshot, error) {
	sn := &snapshot{scheduleID: scheduleID, exams: map[int64]M{}, slotDay: map[int64]int64{}, slotSess: map[int64]int64{},
		byExam: map[int64][]M{}, rooms: map[int64]M{}, slotRooms: map[int64][]M{}}
	sp := fmt.Sprintf("/schedules/%d", scheduleID)

	cal, err := a.all(ctx, sp+"/exams", tok)
	if err != nil {
		return nil, err
	}
	for _, e := range cal {
		sn.exams[num(e, "exam_id")] = e
		sn.slotDay[num(e, "slot_id")], sn.slotSess[num(e, "slot_id")] = num(e, "day"), num(e, "session")
	}
	rooms, err := a.all(ctx, "/rooms", tok)
	if err != nil {
		return nil, err
	}
	for _, r := range rooms {
		sn.rooms[num(r, "id")] = r
	}
	ids := make([]int64, 0, len(sn.exams))
	for id := range sn.exams {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		rows, err := a.all(ctx, fmt.Sprintf("%s/seating?exam_id=%d", sp, id), tok)
		if err != nil {
			return nil, err
		}
		sn.byExam[id] = rows
		sn.seats = append(sn.seats, rows...)
	}
	slots := make([]int64, 0, len(sn.slotDay))
	for s := range sn.slotDay {
		slots = append(slots, s)
	}
	sort.Slice(slots, func(i, j int) bool { return slots[i] < slots[j] })
	for _, s := range slots {
		r, err := a.get(ctx, fmt.Sprintf("%s/rooms?slot_id=%d", sp, s), tok)
		if err != nil || r.code != 200 {
			return nil, fmt.Errorf("slot %d salon planı: %v (HTTP %d)", s, err, r.code)
		}
		for _, it := range arr(r.body, "items") {
			sn.slotRooms[s] = append(sn.slotRooms[s], it.(M))
		}
	}
	if err := sn.reloadDuties(ctx, a, tok); err != nil {
		return nil, err
	}
	return sn, nil
}

func (sn *snapshot) reloadDuties(ctx context.Context, a *api, tok string) error {
	d, err := a.all(ctx, fmt.Sprintf("/schedules/%d/invigilators", sn.scheduleID), tok)
	sn.duties = d
	return err
}

// studentExams: öğrenci -> sınav kimlikleri.
func (sn *snapshot) studentExams() map[int64][]int64 {
	out := map[int64][]int64{}
	for exam, rows := range sn.byExam {
		for _, r := range rows {
			out[num(r, "student_id")] = append(out[num(r, "student_id")], exam)
		}
	}
	return out
}

// dutyKey, (slot, salon) görev anahtarı.
type dutyKey struct{ slot, room int64 }

// roomStudents: (slot, salon) -> o salondaki toplam öğrenci.
func (sn *snapshot) roomStudents() map[dutyKey]int {
	out := map[dutyKey]int{}
	for exam, rows := range sn.byExam {
		slot := num(sn.exams[exam], "slot_id")
		for _, r := range rows {
			out[dutyKey{slot, num(r, "room_id")}]++
		}
	}
	return out
}

// checkInvigilation, T-4 kısıtlarını görev listesinden bağımsızca doğrular.
func (sn *snapshot) checkInvigilation(f *failures) (lo, hi int) {
	perUserSlot := map[[2]int64]int{}
	type ds struct{ day, sess int64 }
	byUser := map[int64][]ds{}
	staff := map[dutyKey]map[int64]bool{}
	load := map[int64]int{}
	for _, d := range sn.duties {
		u, slot, room := num(d, "user_id"), num(d, "slot_id"), num(d, "room_id")
		perUserSlot[[2]int64{u, slot}]++
		byUser[u] = append(byUser[u], ds{num(d, "day"), num(d, "session")})
		k := dutyKey{slot, room}
		if staff[k] == nil {
			staff[k] = map[int64]bool{}
		}
		staff[k][u] = true
		load[u]++
	}
	for k, n := range perUserSlot {
		f.check(n == 1, "gözetmen %d slot %d'de %d görev (aynı slotta iki görev yasak)", k[0], k[1], n)
	}
	for u, list := range byUser {
		seen := map[ds]bool{}
		for _, x := range list {
			seen[x] = true
		}
		for x := range seen {
			f.check(!seen[ds{x.day, x.sess + 1}], "gözetmen %d gün %d ardışık oturumlarda (%d,%d) görevli", u, x.day, x.sess, x.sess+1)
		}
	}
	for k, n := range sn.roomStudents() {
		need := 1
		if n > 50 {
			need = 2
		}
		f.check(len(staff[k]) >= need, "slot %d salon %d: %d öğrenci, %d gözetmen (en az %d)", k.slot, k.room, n, len(staff[k]), need)
	}
	first := true
	for _, n := range load {
		if first || n < lo {
			lo = n
		}
		if first || n > hi {
			hi = n
		}
		first = false
	}
	return lo, hi
}

// fingerprint, çizelgenin kimlik numaralarından bağımsız içeriğinin özetidir: iki
// üretimin (aynı tohum) özdeşliğini karşılaştırmak için. Sürüm kimliği içermez.
func (sn *snapshot) fingerprint() string {
	h := sha256.New()
	ids := make([]int64, 0, len(sn.exams))
	for id := range sn.exams {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		fmt.Fprintf(h, "e%d:%d;", id, num(sn.exams[id], "slot_id"))
		for _, r := range sn.byExam[id] {
			fmt.Fprintf(h, "s%d:%d:%d:%d;", num(r, "student_id"), num(r, "room_id"), num(r, "seat_row"), num(r, "seat_col"))
		}
	}
	d := make([]string, 0, len(sn.duties))
	for _, x := range sn.duties {
		d = append(d, fmt.Sprintf("d%d:%d:%d", num(x, "slot_id"), num(x, "room_id"), num(x, "user_id")))
	}
	sort.Strings(d)
	fmt.Fprint(h, strings.Join(d, ","))
	return hex.EncodeToString(h.Sum(nil))
}
