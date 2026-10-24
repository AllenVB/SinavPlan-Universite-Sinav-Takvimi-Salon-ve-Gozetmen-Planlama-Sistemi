package rooms

import (
	"fmt"
	"sort"
)

// Violation, salon planı ihlali.
type Violation struct {
	Kind string // unknown_exam | unknown_room | room_type | duplicate | bad_count | not_fully_seated |
	// split_inconsistent | capacity | accessible
	ExamID int64
	RoomID int64
	Detail string
}

// Validate, planı Assign'dan BAĞIMSIZ olarak denetler:
//   - sınav ve salon girdide var mı, lab sınavı lab salonda / diğerleri klasikte mi,
//   - aynı (sınav, salon) çifti bir kez mi, paylar pozitif mi,
//   - her öğrenci tam bir salona düşüyor mu (payların toplamı = öğrenci sayısı),
//   - bölme tutarlılığı: engelli paylarının toplamı = sınavın engelli sayısı ve her payın
//     engelli sayısı payın büyüklüğünü aşmıyor,
//   - kapasite: etkin kapasite modeli (hiçbir sınavın payı Single'ı aşamaz; karışık salonda toplam
//     Active - engelli sayısını, tek sınavlı salonda Single'ı aşamaz; bir sınavın payı ayrıca
//     Single - (başka sınavların engelli sayısı)'nı aşamaz) ve erişilebilir koltuk sayısı.
//
// Plan.Unplaced'da bildirilen sınavlar tam yerleşmemiş sayılmaz (kasıtlı, raporlanmış).
func Validate(rooms []Room, exams []Exam, cfg Config, plan Plan) []Violation {
	var out []Violation
	add := func(kind string, exam, room int64, f string, a ...any) {
		out = append(out, Violation{kind, exam, room, fmt.Sprintf(f, a...)})
	}
	roomByID := map[int64]Room{}
	capOf := map[int64]Capacity{}
	for _, r := range rooms {
		roomByID[r.ID] = r
		capOf[r.ID] = EffectiveCapacity(r, cfg)
	}
	examByID := map[int64]Exam{}
	for _, e := range exams {
		examByID[e.ID] = e
	}
	declared := map[int64]bool{}
	for _, u := range plan.Unplaced {
		declared[u.ExamID] = true
	}

	type key struct{ e, r int64 }
	seen := map[key]bool{}
	sumByExam := map[int64]int{}
	disByExam := map[int64]int{}
	type load struct {
		sum, dis int
		shares   map[int64]int
		disBy    map[int64]int
	}
	byRoom := map[int64]*load{}

	for _, p := range plan.Placements {
		e, eok := examByID[p.ExamID]
		r, rok := roomByID[p.RoomID]
		if !eok {
			add("unknown_exam", p.ExamID, p.RoomID, "girdide olmayan sınav")
		}
		if !rok {
			add("unknown_room", p.ExamID, p.RoomID, "girdide olmayan salon")
		}
		if seen[key{p.ExamID, p.RoomID}] {
			add("duplicate", p.ExamID, p.RoomID, "aynı sınav-salon çifti birden çok kez")
		}
		seen[key{p.ExamID, p.RoomID}] = true
		if p.Count <= 0 || p.Disabled < 0 || p.Disabled > p.Count {
			add("bad_count", p.ExamID, p.RoomID, "geçersiz pay: %d öğrenci, %d engelli", p.Count, p.Disabled)
			continue
		}
		if eok && rok && e.Lab != r.Lab {
			add("room_type", p.ExamID, p.RoomID, "lab=%v sınav, lab=%v salonda", e.Lab, r.Lab)
		}
		sumByExam[p.ExamID] += p.Count
		disByExam[p.ExamID] += p.Disabled
		l := byRoom[p.RoomID]
		if l == nil {
			l = &load{shares: map[int64]int{}, disBy: map[int64]int{}}
			byRoom[p.RoomID] = l
		}
		l.sum += p.Count
		l.dis += p.Disabled
		l.shares[p.ExamID] += p.Count
		l.disBy[p.ExamID] += p.Disabled
	}

	for _, e := range exams {
		if e.Students == 0 || declared[e.ID] {
			continue
		}
		if sumByExam[e.ID] != e.Students {
			add("not_fully_seated", e.ID, 0, "payların toplamı %d, öğrenci sayısı %d", sumByExam[e.ID], e.Students)
		}
		if disByExam[e.ID] != e.Disabled {
			add("split_inconsistent", e.ID, 0, "engelli payları toplamı %d, sınavın engelli sayısı %d", disByExam[e.ID], e.Disabled)
		}
	}
	for _, e := range exams {
		if declared[e.ID] && sumByExam[e.ID] != 0 {
			add("split_inconsistent", e.ID, 0, "yerleşemedi olarak bildirilen sınavın payları var")
		}
	}

	ids := make([]int64, 0, len(byRoom))
	for id := range byRoom {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(a, b int) bool { return ids[a] < ids[b] })
	for _, id := range ids {
		l, c := byRoom[id], capOf[id]
		if _, ok := roomByID[id]; !ok {
			continue
		}
		if l.dis > c.Accessible {
			add("accessible", 0, id, "%d engelli öğrenci > %d erişilebilir koltuk", l.dis, c.Accessible)
		}
		mixed := len(l.shares) > 1
		if !mixed && l.sum > c.Single {
			add("capacity", 0, id, "tek sınavlı salon: %d öğrenci > etkin kapasite %d (yan yana oturma kuralı)", l.sum, c.Single)
		}
		if mixed && l.sum > c.Active-l.dis {
			add("capacity", 0, id, "karışık salon: toplam %d > aktif koltuk %d - engelli %d", l.sum, c.Active, l.dis)
		}
		exs := make([]int64, 0, len(l.shares))
		for ex := range l.shares {
			exs = append(exs, ex)
		}
		sort.Slice(exs, func(a, b int) bool { return exs[a] < exs[b] })
		for _, ex := range exs {
			limit := c.Single
			if mixed {
				limit -= l.dis - l.disBy[ex] // başka sınavların engellileri bu payın tavanını düşürür
			}
			if l.shares[ex] > limit {
				add("capacity", ex, id, "sınav payı %d > izin verilen %d (tek-sınav kapasitesi %d)", l.shares[ex], limit, c.Single)
			}
		}
	}
	return out
}
