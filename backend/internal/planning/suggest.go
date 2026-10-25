package planning

import (
	"time"

	"github.com/AllenVB/sinavplan/backend/internal/domain/scheduling"
	"github.com/AllenVB/sinavplan/backend/internal/simdata"
)

// FixSuggestion, bir sınavı başka bir slota taşıma önerisidir. Sonrası (MetricsAfter) çözücünün
// artımlı hesabından değil, bağımsız doğrulayıcıdan (scheduling.Validate) gelir.
type FixSuggestion struct {
	ExamID       int64              `json:"exam_id"`
	FromSlotID   int64              `json:"from_slot_id"`
	ToSlotID     int64              `json:"to_slot_id"`
	FromStartsAt time.Time          `json:"from_starts_at"`
	ToStartsAt   time.Time          `json:"to_starts_at"`
	Delta        int64              `json:"delta"`
	Violations   int                `json:"violations"`
	MetricsAfter scheduling.Metrics `json:"metrics_after"`
}

// Fixes, öneri yanıtı. StudentDays: öğrencinin "aynı gün 2 sınav" olduğu günler (öğrenci modunda).
type Fixes struct {
	Mode          string             `json:"mode"` // "student" | "global"
	MetricsBefore scheduling.Metrics `json:"metrics_before"`
	StudentDays   []int              `json:"student_days"`
	Suggestions   []FixSuggestion    `json:"suggestions"`
}

// SuggestFixes, yayındaki/taslak bir çizelge için taşıma önerileri üretir.
//   - studentID > 0: öğrencinin aynı gün iki sınavı olan günlerindeki sınavlarından biri, öğrencinin
//     başka sınavı OLMAYAN bir güne taşınır (toplam ceza biraz artabilir; Delta dürüstçe raporlanır).
//   - studentID = 0: yalnız toplam cezayı düşüren tek-sınav taşımaları (çözücü çıktısı genelde bu anlamda
//     yerel optimumdadır, liste boş olabilir).
//
// Başlamış oturumlar kilitlidir: yapılmış sınav taşınmaz, geçmiş bir oturuma sınav konmaz.
func SuggestFixes(ds *simdata.Dataset, seed int64, slotIDs map[int64]int64, studentID int64, now time.Time, limit int) (Fixes, error) {
	in := SchedulingInput(ds, seed)
	idx := SlotIndex(ds)
	assign := make(map[int64]int, len(slotIDs))
	for e, sl := range slotIDs {
		assign[e] = idx[sl]
	}
	out := Fixes{Mode: "global", MetricsBefore: scheduling.Validate(in, assign).Metrics, StudentDays: []int{}, Suggestions: []FixSuggestion{}}
	locked := make([]bool, len(ds.Slots))
	for i, s := range ds.Slots {
		locked[i] = !s.StartsAt.After(now)
	}
	day := func(s int) int { return s / in.SessionsPerDay }
	opt := scheduling.SuggestOptions{Limit: limit, Locked: locked, OnlyImproving: true}
	if studentID > 0 {
		out.Mode = "student"
		var mine []int64
		for e, studs := range examStudents(ds) {
			for _, s := range studs {
				if s == studentID {
					mine = append(mine, e)
					break
				}
			}
		}
		perDay := map[int][]int64{}
		for _, e := range mine {
			if s, ok := assign[e]; ok {
				perDay[day(s)] = append(perDay[day(s)], e)
			}
		}
		for d := 0; d < in.Days; d++ {
			if len(perDay[d]) >= 2 {
				out.StudentDays = append(out.StudentDays, d)
				opt.Exams = append(opt.Exams, perDay[d]...)
			}
		}
		if len(opt.Exams) == 0 {
			return out, nil // öğrencinin çözülecek günü yok
		}
		opt.OnlyImproving, opt.PerExam = false, 3
		opt.Accept = func(m scheduling.MoveSuggestion) bool { return len(perDay[day(m.To)]) == 0 }
	}
	moves, err := scheduling.SuggestMoves(in, assign, opt)
	if err != nil {
		return out, err
	}
	for _, m := range moves {
		after := make(map[int64]int, len(assign))
		for k, v := range assign {
			after[k] = v
		}
		after[m.ExamID] = m.To
		rep := scheduling.Validate(in, after)
		out.Suggestions = append(out.Suggestions, FixSuggestion{ExamID: m.ExamID,
			FromSlotID: ds.Slots[m.From].ID, ToSlotID: ds.Slots[m.To].ID,
			FromStartsAt: ds.Slots[m.From].StartsAt, ToStartsAt: ds.Slots[m.To].StartsAt,
			Delta: m.Delta, Violations: len(rep.Violations), MetricsAfter: rep.Metrics})
	}
	return out, nil
}
