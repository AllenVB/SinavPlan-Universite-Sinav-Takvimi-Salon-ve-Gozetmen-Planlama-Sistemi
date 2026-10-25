package planning

import (
	"testing"
	"time"

	"github.com/AllenVB/sinavplan/backend/internal/simdata"
)

// Öğrenci modunda her öneri: 0 sert ihlal (bağımsız doğrulayıcı), öğrencinin aynı-gün günü çözülür
// (taşınan sınav öğrencinin başka sınavı olmayan bir güne gider), başlamış oturumlara dokunulmaz.
func TestSuggestFixesResolvesAStudentsSameDayExams(t *testing.T) {
	ds, _ := simdata.Generate(simdata.Config{Seed: 5, Exams: 72, Students: 900, Rooms: 24, Days: 6, SessionsPerDay: 3, DisabilityRate: 0.02})
	a := solve(t, ds, 20_000)
	slotIDs := map[int64]int64{}
	for e, s := range a {
		slotIDs[e] = ds.Slots[s].ID
	}
	exams := examStudents(ds)
	byStudent := map[int64]map[int]int{}
	for e, studs := range exams {
		for _, st := range studs {
			if byStudent[st] == nil {
				byStudent[st] = map[int]int{}
			}
			byStudent[st][a[e]/ds.Config.SessionsPerDay]++
		}
	}
	var student int64
	for st, days := range byStudent {
		for d, n := range days {
			if n >= 2 && d > 0 && (student == 0 || st < student) {
				student = st
			}
		}
	}
	if student == 0 {
		t.Skip("bu girdide aynı gün 2 sınavı olan öğrenci yok")
	}
	now := ds.Slots[0].StartsAt.Add(time.Minute) // ilk oturum başlamış: kilitli
	fx, err := SuggestFixes(ds, 42, slotIDs, student, now, 3)
	if err != nil {
		t.Fatal(err)
	}
	if fx.Mode != "student" || len(fx.StudentDays) == 0 || len(fx.Suggestions) == 0 {
		t.Fatalf("öğrenci %d için öneri beklenirdi: %+v", student, fx)
	}
	for _, s := range fx.Suggestions {
		if s.Violations != 0 {
			t.Fatalf("öneri ihlalli: %+v", s)
		}
		if s.FromSlotID == ds.Slots[0].ID || s.ToSlotID == ds.Slots[0].ID {
			t.Fatalf("başlamış oturuma dokunuldu: %+v", s)
		}
		if s.MetricsAfter.Penalty != fx.MetricsBefore.Penalty+s.Delta {
			t.Fatalf("doğrulayıcı cezası %d ≠ önce %d + delta %d", s.MetricsAfter.Penalty, fx.MetricsBefore.Penalty, s.Delta)
		}
		toDay := -1
		for i, sl := range ds.Slots {
			if sl.ID == s.ToSlotID {
				toDay = i / ds.Config.SessionsPerDay
			}
		}
		if byStudent[student][toDay] != 0 {
			t.Fatalf("hedef gün %d'de öğrencinin zaten sınavı var: %+v", toDay, s)
		}
	}
	// Sorunu olmayan öğrenci: öneri yok, hata yok.
	none, err := SuggestFixes(ds, 42, slotIDs, 1<<40, now, 3)
	if err != nil || len(none.Suggestions) != 0 {
		t.Fatalf("bilinmeyen öğrenci: %v %+v", err, none.Suggestions)
	}
}
