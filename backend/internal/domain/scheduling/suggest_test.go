package scheduling

import (
	"reflect"
	"testing"

	"github.com/AllenVB/sinavplan/backend/internal/simdata"
)

// Yerel aramasız (yalnız yapıcı) bir çizelge; günler bol olduğundan tek-sınav taşımasıyla
// iyileşme fırsatı vardır.
func roughSchedule(t *testing.T) (Input, map[int64]int) {
	t.Helper()
	ds, err := simdata.Generate(simdata.Config{Seed: 11, Exams: 60, Students: 500, Rooms: 10, Days: 10, SessionsPerDay: 3, DisabilityRate: 0.02})
	if err != nil {
		t.Fatal(err)
	}
	in := fromDataset(ds)
	in.MaxIterations = 1
	r := mustSolve(t, in)
	noViolations(t, in, r)
	return in, r.Assignment
}

// Kaba kuvvet: her (sınav, slot) için tam doğrulayıcıyla geçerli tek hamlelerin en küçük ceza farkı.
func bestSingleMoveByValidator(in Input, a map[int64]int) int64 {
	base := Validate(in, a).Metrics.Penalty
	best := int64(1 << 62)
	for id, from := range a {
		for s := 0; s < in.Slots(); s++ {
			if s == from {
				continue
			}
			m := make(map[int64]int, len(a))
			for k, v := range a {
				m[k] = v
			}
			m[id] = s
			if r := Validate(in, m); len(r.Violations) == 0 && r.Metrics.Penalty-base < best {
				best = r.Metrics.Penalty - base
			}
		}
	}
	return best
}

// Artımlı arama, tam doğrulayıcıyla kaba kuvvetin bulduğu en iyi tek hamleyi bulur (çözücü çıktısı
// tek-hamle anlamında yerel optimumda olduğundan bu fark genelde ≥ 0'dır; önemli olan eşitlik).
func TestSuggestMovesFindsTheBruteForceOptimum(t *testing.T) {
	in, a := roughSchedule(t)
	sug, err := SuggestMoves(in, a, SuggestOptions{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	want := bestSingleMoveByValidator(in, a)
	if len(sug) != 1 || sug[0].Delta != want {
		t.Fatalf("öneri %+v, kaba kuvvet en iyi delta %d", sug, want)
	}
}

// Her öneri tek başına uygulandığında bağımsız doğrulayıcı 0 ihlal verir ve ceza tam olarak
// önceki + Delta olur (artımlı hesap ile doğrulayıcı birebir).
func TestSuggestedMovesAreValidAndExact(t *testing.T) {
	in, a := roughSchedule(t)
	before := Validate(in, a).Metrics.Penalty
	sug, err := SuggestMoves(in, a, SuggestOptions{Limit: 40})
	if err != nil {
		t.Fatal(err)
	}
	if len(sug) < 10 {
		t.Fatalf("en az 10 geçerli hamle bekleniyordu: %d", len(sug))
	}
	seen := map[int64]bool{}
	for i, s := range sug {
		if a[s.ExamID] != s.From || s.From == s.To {
			t.Fatalf("öneri %d geçersiz: %+v", i, s)
		}
		if i > 0 && sug[i-1].Delta > s.Delta {
			t.Fatalf("öneriler cezayı en çok düşürenden sıralı değil: %d sonra %d", sug[i-1].Delta, s.Delta)
		}
		if seen[s.ExamID] {
			t.Fatalf("sınav %d için birden çok öneri", s.ExamID)
		}
		seen[s.ExamID] = true
		moved := make(map[int64]int, len(a))
		for k, v := range a {
			moved[k] = v
		}
		moved[s.ExamID] = s.To
		rep := Validate(in, moved)
		if len(rep.Violations) != 0 {
			t.Fatalf("öneri %+v uygulanınca %d ihlal: %+v", s, len(rep.Violations), rep.Violations[0])
		}
		if rep.Metrics.Penalty != before+s.Delta {
			t.Fatalf("öneri %+v: doğrulayıcı cezası %d, beklenen %d+%d", s, rep.Metrics.Penalty, before, s.Delta)
		}
	}
}

func TestSuggestMovesHonoursLockedSlotsAndFilter(t *testing.T) {
	in, a := roughSchedule(t)
	locked := make([]bool, in.Slots())
	for s := 0; s < in.Slots()/2; s++ { // ilk yarı: geçmiş oturumlar
		locked[s] = true
	}
	sug, err := SuggestMoves(in, a, SuggestOptions{Limit: 50, Locked: locked})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sug {
		if locked[s.From] || locked[s.To] {
			t.Fatalf("kilitli slota dokunan öneri: %+v", s)
		}
	}
	if len(sug) > 0 {
		only, _ := SuggestMoves(in, a, SuggestOptions{Exams: []int64{sug[0].ExamID}, Locked: locked})
		if len(only) != 1 || only[0] != sug[0] {
			t.Fatalf("sınav süzgeci: %+v, beklenen [%+v]", only, sug[0])
		}
	}
}

func TestSuggestMovesIsDeterministic(t *testing.T) {
	in, a := roughSchedule(t)
	x, _ := SuggestMoves(in, a, SuggestOptions{Limit: 5})
	y, _ := SuggestMoves(in, a, SuggestOptions{Limit: 5})
	if !reflect.DeepEqual(x, y) {
		t.Fatalf("aynı girdi farklı öneri: %+v / %+v", x, y)
	}
}

func TestSuggestMovesRejectsBrokenAssignment(t *testing.T) {
	in, a := roughSchedule(t)
	for id := range a { // bir sınavı çizelgeden çıkar → "unplaced" ihlali
		delete(a, id)
		break
	}
	if _, err := SuggestMoves(in, a, SuggestOptions{}); err == nil {
		t.Fatal("ihlalli atamaya öneri verilmemeli")
	}
}

// Accept ile "öğrencinin başka sınavı olmayan güne" gibi hedefe yönelik koşul; OnlyImproving yalnız
// cezayı düşürenleri bırakır.
func TestSuggestMovesAcceptAndOnlyImproving(t *testing.T) {
	in, a := roughSchedule(t)
	day := func(s int) int { return s / in.SessionsPerDay }
	sug, _ := SuggestMoves(in, a, SuggestOptions{Limit: 100, PerExam: 5, Accept: func(m MoveSuggestion) bool { return day(m.To) == 0 }})
	if len(sug) == 0 {
		t.Fatal("ilk güne taşınabilen sınav olmalı")
	}
	for _, s := range sug {
		if day(s.To) != 0 {
			t.Fatalf("Accept'e uymayan öneri: %+v", s)
		}
	}
	imp, _ := SuggestMoves(in, a, SuggestOptions{Limit: 100, OnlyImproving: true})
	for _, s := range imp {
		if s.Delta >= 0 {
			t.Fatalf("OnlyImproving'de Delta ≥ 0: %+v", s)
		}
	}
}
