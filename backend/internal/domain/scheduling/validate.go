package scheduling

import (
	"fmt"
	"sort"
)

// Violation, sert kısıt ihlali.
type Violation struct {
	Kind      string  `json:"kind"` // unplaced | invalid_slot | conflict | daily_limit | slot_capacity | unknown_exam
	ExamIDs   []int64 `json:"exam_ids,omitempty"`
	StudentID int64   `json:"student_id,omitempty"`
	Slot      int     `json:"slot"`
	Detail    string  `json:"detail"`
}

// Metrics, çizelge kalite ölçütleri.
type Metrics struct {
	Penalty                    int64 `json:"penalty"`
	ConsecutiveDayPairs        int   `json:"consecutive_day_pairs"`         // Σ öğrenci-gün çifti
	StudentsWithConsecutiveDay int   `json:"students_with_consecutive_day"` // en az bir ardışık günü olan öğrenci
	StudentDaysWithTwoExams    int   `json:"student_days_with_two_exams"`
	StudentsWithTwoExamsADay   int   `json:"students_with_two_exams_a_day"` // en az bir gün 2 sınavı olan öğrenci
	MaxStudentsInSlot          int   `json:"max_students_in_slot"`
	UsedSlots                  int   `json:"used_slots"`
}

// Report, Validate çıktısı.
type Report struct {
	Violations []Violation `json:"violations"`
	Metrics    Metrics     `json:"metrics"`
}

// Validate, çizelgeyi çözücüden BAĞIMSIZ olarak, sıfırdan denetler: her sınav geçerli
// bir slotta mı, ortak öğrencili iki sınav aynı slotta mı, bir öğrencinin bir günde
// 2'den fazla sınavı var mı, slot kapasitesi aşılıyor mu. Metrikleri de hesaplar.
func Validate(in Input, assignment map[int64]int) Report {
	w := in.Weights
	if w == (Weights{}) {
		w = DefaultWeights
	}
	slots := in.Slots()
	rep := Report{Violations: []Violation{}}

	known := map[int64]bool{}
	byStudent := map[int64][]int64{} // öğrenci -> sınavları
	load := make([]int, max(slots, 0))
	labLoad := make([]int, max(slots, 0))
	for _, e := range in.Exams {
		known[e.ID] = true
		s, ok := assignment[e.ID]
		if !ok {
			rep.Violations = append(rep.Violations, Violation{Kind: "unplaced", ExamIDs: []int64{e.ID}, Slot: -1,
				Detail: "sınav çizelgede yok"})
			continue
		}
		if s < 0 || s >= slots {
			rep.Violations = append(rep.Violations, Violation{Kind: "invalid_slot", ExamIDs: []int64{e.ID}, Slot: s,
				Detail: fmt.Sprintf("slot 0..%d aralığında olmalı", slots-1)})
			continue
		}
		uniq := map[int64]bool{}
		for _, st := range e.Students {
			if !uniq[st] {
				uniq[st] = true
				byStudent[st] = append(byStudent[st], e.ID)
			}
		}
		load[s] += len(uniq)
		if e.Lab {
			labLoad[s] += len(uniq)
		}
	}
	for id := range assignment {
		if !known[id] {
			rep.Violations = append(rep.Violations, Violation{Kind: "unknown_exam", ExamIDs: []int64{id},
				Slot: assignment[id], Detail: "girdide olmayan sınav"})
		}
	}

	students := make([]int64, 0, len(byStudent))
	for s := range byStudent {
		students = append(students, s)
	}
	sort.Slice(students, func(a, b int) bool { return students[a] < students[b] })

	conflictPairs := map[[2]int64]bool{}
	for _, st := range students {
		perSlot := map[int][]int64{}
		perDay := make([]int, max(in.Days, 0))
		for _, ex := range byStudent[st] {
			s := assignment[ex]
			perSlot[s] = append(perSlot[s], ex)
			perDay[s/in.SessionsPerDay]++
		}
		for s, exams := range perSlot {
			for i := 0; i < len(exams); i++ {
				for j := i + 1; j < len(exams); j++ {
					a, b := min(exams[i], exams[j]), max(exams[i], exams[j])
					if !conflictPairs[[2]int64{a, b}] {
						conflictPairs[[2]int64{a, b}] = true
						rep.Violations = append(rep.Violations, Violation{Kind: "conflict", ExamIDs: []int64{a, b},
							StudentID: st, Slot: s, Detail: "ortak öğrencisi olan sınavlar aynı slotta"})
					}
				}
			}
		}
		consecutive, twoDays := 0, 0
		for d, c := range perDay {
			if c > 2 {
				rep.Violations = append(rep.Violations, Violation{Kind: "daily_limit", StudentID: st,
					Slot: d * in.SessionsPerDay, Detail: fmt.Sprintf("gün %d: %d sınav (en fazla 2)", d, c)})
			}
			if c >= 2 {
				twoDays++
			}
			if d+1 < len(perDay) && c > 0 && perDay[d+1] > 0 {
				consecutive++
			}
		}
		rep.Metrics.ConsecutiveDayPairs += consecutive
		rep.Metrics.StudentDaysWithTwoExams += twoDays
		if consecutive > 0 {
			rep.Metrics.StudentsWithConsecutiveDay++
		}
		if twoDays > 0 {
			rep.Metrics.StudentsWithTwoExamsADay++
		}
	}

	for s := 0; s < slots; s++ {
		if load[s] > 0 {
			rep.Metrics.UsedSlots++
		}
		rep.Metrics.MaxStudentsInSlot = max(rep.Metrics.MaxStudentsInSlot, load[s])
		if in.SlotCapacity > 0 && load[s] > in.SlotCapacity {
			rep.Violations = append(rep.Violations, Violation{Kind: "slot_capacity", Slot: s,
				Detail: fmt.Sprintf("%d öğrenci > kapasite %d", load[s], in.SlotCapacity)})
		}
		if in.LabSlotCapacity > 0 && labLoad[s] > in.LabSlotCapacity {
			rep.Violations = append(rep.Violations, Violation{Kind: "slot_capacity", Slot: s,
				Detail: fmt.Sprintf("lab: %d öğrenci > lab kapasitesi %d", labLoad[s], in.LabSlotCapacity)})
		}
	}
	rep.Metrics.Penalty = w.ConsecutiveDay*int64(rep.Metrics.ConsecutiveDayPairs) +
		w.SameDay*int64(rep.Metrics.StudentDaysWithTwoExams)
	return rep
}

// --- Çözümsüzlük raporu --------------------------------------------------------

// UnplacedExam, yerleştirilemeyen bir sınav ve onu bloke edenler.
type UnplacedExam struct {
	ExamID            int64   `json:"exam_id"`
	Blockers          []int64 `json:"blockers"` // slotları kapatan, yerleştirilmiş çatışma komşuları
	SlotsByConflict   int     `json:"slots_blocked_by_conflict"`
	SlotsByDailyLimit int     `json:"slots_blocked_by_daily_limit"`
	SlotsByCapacity   int     `json:"slots_blocked_by_capacity"`
}

// InfeasibilityReport, çözüm bulunamadığında açıklama.
type InfeasibilityReport struct {
	Exams []UnplacedExam `json:"exams"`
	// Clique boş değilse: karşılıklı çatışan bu sınavların sayısı slot sayısını aşıyor,
	// yani hiçbir algoritma hepsini yerleştiremez (kanıtlanabilir neden).
	Clique []int64 `json:"clique,omitempty"`
	// OverloadedStudents: sınav sayısı gün×2 sınırını aşan öğrenciler (yine kesin neden).
	OverloadedStudents []int64  `json:"overloaded_students,omitempty"`
	Reasons            []string `json:"reasons"`
}

func explain(st *state, unplaced []int) *InfeasibilityReport {
	p := st.p
	rep := &InfeasibilityReport{}
	for _, e := range unplaced {
		u := UnplacedExam{ExamID: p.examIDs[e]}
		blockers := map[int64]bool{}
		for s := 0; s < p.slots; s++ {
			ch := []change{{e, -1, s}}
			switch {
			case !st.conflictFree(e, s):
				u.SlotsByConflict++
				for _, j := range p.adj[e] {
					if st.slot[j] == s {
						blockers[p.examIDs[j]] = true
					}
				}
			case !st.capacityOK(ch):
				u.SlotsByCapacity++
			default:
				_, ok, _ := st.evaluate(ch)
				st.revert(ch)
				if !ok {
					u.SlotsByDailyLimit++
				}
			}
		}
		for id := range blockers {
			u.Blockers = append(u.Blockers, id)
		}
		sort.Slice(u.Blockers, func(a, b int) bool { return u.Blockers[a] < u.Blockers[b] })
		rep.Exams = append(rep.Exams, u)
	}

	if c := largestCliqueFrom(p, unplaced); len(c) > p.slots {
		for _, e := range c {
			rep.Clique = append(rep.Clique, p.examIDs[e])
		}
		sort.Slice(rep.Clique, func(a, b int) bool { return rep.Clique[a] < rep.Clique[b] })
		rep.Reasons = append(rep.Reasons, fmt.Sprintf(
			"%d sınav karşılıklı olarak çatışıyor (klik) ama yalnızca %d slot var: çözüm yok",
			len(c), p.slots))
	}
	for s := 0; s < p.m; s++ {
		if n := len(p.studentExamList(s)); n > 2*p.days {
			rep.OverloadedStudents = append(rep.OverloadedStudents, p.studentIDs[s])
		}
	}
	if len(rep.OverloadedStudents) > 0 {
		rep.Reasons = append(rep.Reasons, fmt.Sprintf(
			"%d öğrencinin sınav sayısı gün başına 2 sınırıyla sığmıyor (en fazla %d)",
			len(rep.OverloadedStudents), 2*p.days))
	}
	if len(rep.Reasons) == 0 {
		rep.Reasons = append(rep.Reasons,
			"kesin bir alt sınır bulunamadı; sezgisel yerleştirme bu sınavlar için uygun slot bulamadı "+
				"(engelleyen komşular listelendi) — daha fazla slot veya kısıt gevşetme gerekebilir")
	}
	return rep
}

// largestCliqueFrom: verilen sınavlardan başlayan açgözlü klik araması. Bulunan klik
// slot sayısından büyükse bu, çözümsüzlüğün kanıtıdır (klik üyeleri ikişer ikişer
// farklı slot ister).
func largestCliqueFrom(p *problem, seeds []int) []int {
	adjacent := func(a, b int) bool { return p.conflictBits[a*p.words+b/64]&(1<<(b%64)) != 0 }
	var best []int
	for _, s := range seeds {
		cands := append([]int32(nil), p.adj[s]...)
		sort.Slice(cands, func(a, b int) bool { return len(p.adj[cands[a]]) > len(p.adj[cands[b]]) })
		clique := []int{s}
		for _, c := range cands {
			ok := true
			for _, x := range clique {
				if !adjacent(int(c), x) {
					ok = false
					break
				}
			}
			if ok {
				clique = append(clique, int(c))
			}
		}
		if len(clique) > len(best) {
			best = clique
		}
	}
	return best
}
