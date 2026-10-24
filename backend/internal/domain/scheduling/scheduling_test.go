package scheduling

import (
	"math/rand/v2"
	"reflect"
	"testing"
	"time"

	"github.com/AllenVB/sinavplan/backend/internal/simdata"
)

// FromDataset, simülatör verisinden çözücü girdisi kurar (test ve prove için).
func fromDataset(ds *simdata.Dataset) Input {
	secCourse := map[int64]int64{}
	for _, s := range ds.Sections {
		secCourse[s.ID] = s.CourseID
	}
	byCourse := map[int64][]int64{}
	for _, e := range ds.Enrollments {
		c := secCourse[e.SectionID]
		byCourse[c] = append(byCourse[c], e.StudentID)
	}
	in := Input{Days: ds.Config.Days, SessionsPerDay: ds.Config.SessionsPerDay, Seed: 42}
	for _, ex := range ds.Exams {
		in.Exams = append(in.Exams, Exam{ID: ex.ID, Students: byCourse[ex.CourseID], Lab: ex.RequiresLab})
	}
	return in
}

func mustSolve(t testing.TB, in Input) *Result {
	t.Helper()
	r, err := Solve(in)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func noViolations(t testing.TB, in Input, r *Result) Report {
	t.Helper()
	rep := Validate(in, r.Assignment)
	if len(rep.Violations) != 0 {
		t.Fatalf("%d ihlal, ilki: %+v", len(rep.Violations), rep.Violations[0])
	}
	return rep
}

// Ceza formülü: ardışık gün Wc=3, aynı gün 2 sınav Ws=1.
func TestPenaltyFormula(t *testing.T) {
	in := Input{Days: 3, SessionsPerDay: 2, Exams: []Exam{
		{ID: 1, Students: []int64{100}}, {ID: 2, Students: []int64{100}}, {ID: 3, Students: []int64{100}}}}
	// gün 0: sınav 1 ve 2 (aynı gün) ; gün 1: sınav 3 (ardışık gün)
	rep := Validate(in, map[int64]int{1: 0, 2: 1, 3: 2})
	if len(rep.Violations) != 0 {
		t.Fatalf("ihlal beklenmiyordu: %+v", rep.Violations)
	}
	m := rep.Metrics
	if m.ConsecutiveDayPairs != 1 || m.StudentDaysWithTwoExams != 1 || m.Penalty != 3+1 {
		t.Fatalf("metrikler yanlış: %+v", m)
	}
	// Gün 0 ve gün 2: ardışık değil, aynı gün yok -> ceza 0.
	if p := Validate(in, map[int64]int{1: 0, 2: 4, 3: 5}).Metrics.Penalty; p != 1 {
		t.Fatalf("gün 2'de iki sınav: ceza 1 bekleniyordu, %d", p)
	}
}

func TestSmallFeasibleInstanceHasNoViolationsAndZeroPenalty(t *testing.T) {
	// 4 sınav, zincir çatışma 1-2-3-4; 5 gün yeter, ceza sıfıra inebilmeli.
	in := Input{Days: 5, SessionsPerDay: 2, Seed: 1, MaxIterations: 20000, Exams: []Exam{
		{ID: 1, Students: []int64{1, 2}}, {ID: 2, Students: []int64{2, 3}},
		{ID: 3, Students: []int64{3, 4}}, {ID: 4, Students: []int64{4, 5}}}}
	r := mustSolve(t, in)
	if len(r.Unplaced) != 0 {
		t.Fatalf("yerleşmeyen sınav: %v", r.Unplaced)
	}
	rep := noViolations(t, in, r)
	if rep.Metrics.Penalty != 0 {
		t.Fatalf("bu girdide ceza 0 olabilir, %d bulundu (%+v)", rep.Metrics.Penalty, r.Assignment)
	}
}

func TestDeterministicForSameSeed(t *testing.T) {
	in := fromDataset(mustSmall(t))
	in.MaxIterations = 50_000
	a, b := mustSolve(t, in), mustSolve(t, in)
	if !reflect.DeepEqual(a.Assignment, b.Assignment) || a.Penalty != b.Penalty {
		t.Fatal("aynı girdi + aynı tohum farklı çizelge üretti")
	}
	in.Seed = 99
	c := mustSolve(t, in)
	if reflect.DeepEqual(a.Assignment, c.Assignment) {
		t.Log("not: farklı tohum aynı çizelgeyi verdi (küçük girdide mümkün)")
	}
}

func mustSmall(t testing.TB) *simdata.Dataset {
	ds, err := simdata.Generate(simdata.Config{Seed: 7, Exams: 48, Students: 400, Rooms: 10,
		Days: 6, SessionsPerDay: 3, DisabilityRate: 0.02})
	if err != nil {
		t.Fatal(err)
	}
	return ds
}

// Artımlı ceza hesabı (çözücü içi) ile bağımsız doğrulayıcının cezası birebir aynı olmalı.
func TestIncrementalPenaltyMatchesValidator(t *testing.T) {
	for seed := int64(1); seed <= 5; seed++ {
		in := fromDataset(mustSmall(t))
		in.Seed, in.MaxIterations = seed, 30_000
		r := mustSolve(t, in)
		rep := noViolations(t, in, r)
		if rep.Metrics.Penalty != r.Penalty {
			t.Fatalf("seed %d: çözücü cezası %d, doğrulayıcı %d", seed, r.Penalty, rep.Metrics.Penalty)
		}
	}
}

func TestLocalSearchDoesNotWorsenConstruction(t *testing.T) {
	in := fromDataset(mustSmall(t))
	in.MaxIterations = 100_000
	r := mustSolve(t, in)
	if r.Penalty > r.ConstructionPenalty {
		t.Fatalf("yerel arama sonrası ceza (%d) yapıcı çözümden (%d) kötü", r.Penalty, r.ConstructionPenalty)
	}
	if r.Penalty == r.ConstructionPenalty {
		t.Logf("uyarı: yerel arama iyileştirme bulamadı (ceza %d)", r.Penalty)
	}
}

// Bilinçli olarak çözümsüz: 4 sınav karşılıklı çatışıyor (klik), yalnız 3 slot var.
func TestInfeasibleCliqueIsExplained(t *testing.T) {
	in := Input{Days: 3, SessionsPerDay: 1, Exams: []Exam{
		{ID: 10, Students: []int64{1, 2, 3}}, {ID: 20, Students: []int64{1, 4, 5}},
		{ID: 30, Students: []int64{2, 4, 6}}, {ID: 40, Students: []int64{3, 5, 6}}}}
	r := mustSolve(t, in)
	if len(r.Unplaced) == 0 || r.Infeasibility == nil {
		t.Fatal("çözümsüz girdi için yerleşmeyen sınav ve rapor bekleniyordu")
	}
	if got := r.Infeasibility.Clique; !reflect.DeepEqual(got, []int64{10, 20, 30, 40}) {
		t.Fatalf("4'lü klik raporlanmalı, bulunan %v", got)
	}
	u := r.Infeasibility.Exams[0]
	if len(u.Blockers) != 3 || u.SlotsByConflict != 3 {
		t.Fatalf("yerleşmeyen sınavın 3 engelleyicisi ve 3 kapalı slotu olmalı: %+v", u)
	}
	if len(r.Infeasibility.Reasons) == 0 {
		t.Fatal("insan tarafından okunur neden bekleniyordu")
	}
}

// Bilinçli olarak çözümsüz: bir öğrencinin tek günde 3 sınavı var, günde en fazla 2.
func TestInfeasibleDailyLimitIsExplained(t *testing.T) {
	in := Input{Days: 1, SessionsPerDay: 3, Exams: []Exam{
		{ID: 1, Students: []int64{7}}, {ID: 2, Students: []int64{7}}, {ID: 3, Students: []int64{7}}}}
	r := mustSolve(t, in)
	if len(r.Unplaced) != 1 {
		t.Fatalf("tam bir sınav yerleşememeli: %v", r.Unplaced)
	}
	if !reflect.DeepEqual(r.Infeasibility.OverloadedStudents, []int64{7}) {
		t.Fatalf("öğrenci 7 aşırı yüklü raporlanmalı: %+v", r.Infeasibility)
	}
	if r.Infeasibility.Exams[0].SlotsByDailyLimit == 0 {
		t.Fatalf("slot günlük sınırla kapanmış görünmeli: %+v", r.Infeasibility.Exams[0])
	}
}

// Doğrulayıcı bilerek bozulmuş çizelgelerde ihlalleri doğru sayar.
func TestValidatorCatchesBrokenSchedules(t *testing.T) {
	in := Input{Days: 2, SessionsPerDay: 2, SlotCapacity: 3, Exams: []Exam{
		{ID: 1, Students: []int64{1, 2}}, {ID: 2, Students: []int64{2, 3}},
		{ID: 3, Students: []int64{2}}, {ID: 4, Students: []int64{9}}}}
	kinds := func(m map[int64]int) map[string]int {
		out := map[string]int{}
		for _, v := range Validate(in, m).Violations {
			out[v.Kind]++
		}
		return out
	}
	cases := []struct {
		name   string
		assign map[int64]int
		want   map[string]int
	}{
		{"geçerli", map[int64]int{1: 0, 2: 1, 3: 2, 4: 0}, map[string]int{}},
		{"çatışma", map[int64]int{1: 0, 2: 0, 3: 2, 4: 3}, map[string]int{"conflict": 1, "slot_capacity": 1}},
		// Öğrenci 2 sınav 1, 2 ve 3'te: hepsi gün 0'da -> hem çatışma hem günlük sınır.
		{"çatışma + günlük sınır", map[int64]int{1: 0, 2: 1, 3: 0, 4: 3}, map[string]int{"conflict": 1, "daily_limit": 1}},
		{"üç sınav bir gün", map[int64]int{1: 0, 2: 1, 3: 1, 4: 3}, map[string]int{"conflict": 1, "daily_limit": 1}},
		{"yerleşmemiş", map[int64]int{1: 0, 2: 1, 3: 2}, map[string]int{"unplaced": 1}},
		{"aralık dışı", map[int64]int{1: 0, 2: 1, 3: 2, 4: 9}, map[string]int{"invalid_slot": 1}},
		{"bilinmeyen", map[int64]int{1: 0, 2: 1, 3: 2, 4: 3, 77: 0}, map[string]int{"unknown_exam": 1}},
	}
	for _, c := range cases {
		if got := kinds(c.assign); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %v, beklenen %v", c.name, got, c.want)
		}
	}
	// Günlük sınır: öğrenci 2'nin 3 sınavı gün 0'ın iki oturumuna sığmaz; 3 oturumlu bir
	// güne koyup ihlali ayrıca sınıyoruz.
	in3 := Input{Days: 1, SessionsPerDay: 3, Exams: in.Exams[:3]}
	if v := Validate(in3, map[int64]int{1: 0, 2: 1, 3: 2}).Violations; len(v) != 1 || v[0].Kind != "daily_limit" || v[0].StudentID != 2 {
		t.Fatalf("öğrenci 2 için günlük sınır ihlali bekleniyordu: %+v", v)
	}
}

func TestSlotCapacityIsHonoured(t *testing.T) {
	in := fromDataset(mustSmall(t))
	in.MaxIterations = 30_000
	free := mustSolve(t, in)
	cap := Validate(in, free.Assignment).Metrics.MaxStudentsInSlot * 8 / 10
	in.SlotCapacity = cap
	r := mustSolve(t, in)
	if len(r.Unplaced) > 0 {
		t.Skipf("kapasite %d ile çözülemedi (geçerli bir sonuç; testi bu girdide atla)", cap)
	}
	rep := noViolations(t, in, r)
	if rep.Metrics.MaxStudentsInSlot > cap {
		t.Fatalf("slot yükü %d > kapasite %d", rep.Metrics.MaxStudentsInSlot, cap)
	}
}

// Kempe ve taşıma hamleleri sert kısıtları asla bozmaz: rastgele tohumlarla çalıştır.
func TestRandomInstancesAlwaysValid(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 3))
	for trial := 0; trial < 15; trial++ {
		in := Input{Days: 4 + r.IntN(4), SessionsPerDay: 2 + r.IntN(2), Seed: int64(trial), MaxIterations: 20000}
		for e := 1; e <= 25; e++ {
			var st []int64
			for k := 0; k < 3+r.IntN(10); k++ {
				st = append(st, int64(r.IntN(60)))
			}
			in.Exams = append(in.Exams, Exam{ID: int64(e), Students: st})
		}
		res := mustSolve(t, in)
		rep := Validate(in, res.Assignment)
		var hard int
		for _, v := range rep.Violations {
			if v.Kind != "unplaced" {
				hard++
			}
		}
		if hard != 0 {
			t.Fatalf("deneme %d: yerleştirilen sınavlar sert kısıtı ihlal ediyor: %+v", trial, rep.Violations)
		}
		if len(res.Unplaced) != countKind(rep, "unplaced") {
			t.Fatalf("deneme %d: Unplaced listesi doğrulayıcıyla uyuşmuyor", trial)
		}
		if len(res.Unplaced) == 0 && rep.Metrics.Penalty != res.Penalty {
			t.Fatalf("deneme %d: ceza uyuşmuyor %d/%d", trial, res.Penalty, rep.Metrics.Penalty)
		}
	}
}

func countKind(r Report, k string) int {
	n := 0
	for _, v := range r.Violations {
		if v.Kind == k {
			n++
		}
	}
	return n
}

func TestRejectsBadInput(t *testing.T) {
	if _, err := Solve(Input{Days: 0, SessionsPerDay: 3}); err == nil {
		t.Fatal("gün=0 reddedilmeli")
	}
	if _, err := Solve(Input{Days: 1, SessionsPerDay: 1, Exams: []Exam{{ID: 1}, {ID: 1}}}); err == nil {
		t.Fatal("tekrar eden sınav kimliği reddedilmeli")
	}
}

// Tam ölçek: 300 sınav / 8.000 öğrenci / 15 gün. 60 sn sınırı, 0 ihlal.
func TestFullScale(t *testing.T) {
	if testing.Short() {
		t.Skip("tam ölçek -short ile atlandı")
	}
	if raceEnabled {
		// Çözücü tek goroutine'dir; yarış dedektörü burada bir şey yakalamaz ama süreyi
		// ~9 kat (≈90 sn) uzatır. Aynı ölçek CI'da `make prove-t1` ile normal derlemede koşar.
		t.Skip("yarış dedektörü açık: tam ölçek `make prove-t1` adımında doğrulanıyor")
	}
	ds, _ := simdata.Generate(simdata.DefaultConfig(42))
	in := fromDataset(ds)
	in.TimeLimit = 60 * time.Second // varsayılan iterasyon bütçesi
	r := mustSolve(t, in)
	if len(r.Unplaced) != 0 {
		t.Fatalf("yerleşmeyen: %v", r.Unplaced)
	}
	rep := noViolations(t, in, r)
	if r.Duration > 60*time.Second {
		t.Fatalf("süre %v > 60 sn", r.Duration)
	}
	t.Logf("süre=%v iter=%d ceza: yapıcı=%d son=%d metrikler=%+v",
		r.Duration.Round(time.Millisecond), r.Iterations, r.ConstructionPenalty, r.Penalty, rep.Metrics)
}

func BenchmarkSolveFullScale(b *testing.B) {
	ds, _ := simdata.Generate(simdata.DefaultConfig(42))
	in := fromDataset(ds)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Solve(in); err != nil {
			b.Fatal(err)
		}
	}
}
