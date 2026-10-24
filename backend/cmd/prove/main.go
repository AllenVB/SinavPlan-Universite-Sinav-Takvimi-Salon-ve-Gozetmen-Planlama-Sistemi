// prove: çekirdek görevlerin kanıt komutları. Veritabanı gerektirmez; simülatörün
// deterministik verisini (seed=42) üretip saf domain paketlerini çalıştırır.
//
//	prove t1 [--seed 42] [--iterations 1000000] [--time-limit 60s] [--days 15]
//
// Çıkış kodu: herhangi bir sert kısıt ihlali, yerleşmeyen sınav veya süre sınırının
// aşılması durumunda 1.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/AllenVB/sinavplan/backend/internal/domain/scheduling"
	"github.com/AllenVB/sinavplan/backend/internal/simdata"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "kullanım: prove <t1> [bayraklar]")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "t1":
		os.Exit(proveT1(os.Args[2:]))
	default:
		fmt.Fprintf(os.Stderr, "bilinmeyen kanıt: %s\n", os.Args[1])
		os.Exit(2)
	}
}

// T1Input, simülatör verisinden çizelgeleme girdisi kurar. Slot kapasitesi,
// salonların toplam kapasitesinin %80'i: T-2'de tek sınavlı salonların etkin
// kapasitesi düştüğü için tam kapasiteyi doldurmak salon atamasını imkânsız kılabilir.
func T1Input(ds *simdata.Dataset, seed int64) scheduling.Input {
	secCourse := map[int64]int64{}
	for _, s := range ds.Sections {
		secCourse[s.ID] = s.CourseID
	}
	byCourse := map[int64][]int64{}
	for _, e := range ds.Enrollments {
		byCourse[secCourse[e.SectionID]] = append(byCourse[secCourse[e.SectionID]], e.StudentID)
	}
	total, lab := 0, 0
	for _, r := range ds.Rooms {
		total += r.Capacity
		if r.Type == "lab" {
			lab += r.Capacity
		}
	}
	in := scheduling.Input{Days: ds.Config.Days, SessionsPerDay: ds.Config.SessionsPerDay, Seed: seed,
		SlotCapacity: total * 8 / 10, LabSlotCapacity: lab * 8 / 10}
	for _, ex := range ds.Exams {
		in.Exams = append(in.Exams, scheduling.Exam{ID: ex.ID, Students: byCourse[ex.CourseID], Lab: ex.RequiresLab})
	}
	return in
}

func proveT1(args []string) int {
	fs := flag.NewFlagSet("t1", flag.ExitOnError)
	seed := fs.Int64("seed", 42, "veri ve çözücü tohumu")
	iters := fs.Int("iterations", 1_000_000, "yerel arama iterasyon bütçesi (determinizm bundan gelir)")
	limit := fs.Duration("time-limit", 60*time.Second, "üst süre sınırı")
	days := fs.Int("days", 15, "sınav günü")
	_ = fs.Parse(args)

	cfg := simdata.DefaultConfig(*seed)
	cfg.Days = *days
	ds, err := simdata.Generate(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	in := T1Input(ds, *seed)
	in.MaxIterations, in.TimeLimit = *iters, *limit

	fmt.Println("== T-1 Sınav takvimi çizelgeleme — kanıt ==")
	fmt.Printf("girdi: %d sınav, %d öğrenci, %d kayıt, %d gün x %d oturum = %d slot, seed=%d\n",
		len(ds.Exams), len(ds.Students), len(ds.Enrollments), in.Days, in.SessionsPerDay, in.Slots(), *seed)
	fmt.Printf("slot kapasitesi: %d koltuk (lab: %d)\n", in.SlotCapacity, in.LabSlotCapacity)

	r, err := scheduling.Solve(in)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	rep := scheduling.Validate(in, r.Assignment) // çözücüden bağımsız doğrulayıcı
	m := rep.Metrics

	fmt.Printf("çatışma grafı: %d düğüm, %d kenar, en büyük derece %d\n", r.Graph.Exams, r.Graph.Edges, r.Graph.MaxDegree)
	fmt.Println()
	row := func(k string, v any) { fmt.Printf("  %-44s %v\n", k, v) }
	row("süre", r.Duration.Round(time.Millisecond))
	row("iterasyon (kabul edilen hamle)", fmt.Sprintf("%d (%d)", r.Iterations, r.AcceptedMoves))
	row("süre sınırına takıldı mı", r.StoppedByTime)
	row("yerleşmeyen sınav", len(r.Unplaced))
	row("SERT KISIT İHLALİ (doğrulayıcı)", len(rep.Violations))
	row("ceza: yapıcı (DSatur) -> yerel arama", fmt.Sprintf("%d -> %d", r.ConstructionPenalty, m.Penalty))
	row("ardışık gün çifti (öğrenci-gün)", m.ConsecutiveDayPairs)
	row("ardışık günü olan öğrenci", m.StudentsWithConsecutiveDay)
	row("aynı gün 2 sınav (öğrenci-gün)", m.StudentDaysWithTwoExams)
	row("en az bir gün 2 sınavı olan öğrenci", m.StudentsWithTwoExamsADay)
	row("kullanılan slot / en kalabalık slot", fmt.Sprintf("%d / %d öğrenci", m.UsedSlots, m.MaxStudentsInSlot))

	fail := len(rep.Violations) > 0 || len(r.Unplaced) > 0 || r.Duration > 60*time.Second
	if r.Infeasibility != nil {
		fmt.Println("\nçözümsüzlük raporu:")
		for _, s := range r.Infeasibility.Reasons {
			fmt.Println("  -", s)
		}
	}
	for i, v := range rep.Violations {
		if i == 10 {
			fmt.Printf("  ... ve %d ihlal daha\n", len(rep.Violations)-10)
			break
		}
		fmt.Printf("  ihlal: %+v\n", v)
	}

	fmt.Println("\n== Bilinçli çözümsüz girdi: 4 sınav karşılıklı çatışıyor, 3 slot ==")
	bad := scheduling.Input{Days: 3, SessionsPerDay: 1, Exams: []scheduling.Exam{
		{ID: 10, Students: []int64{1, 2, 3}}, {ID: 20, Students: []int64{1, 4, 5}},
		{ID: 30, Students: []int64{2, 4, 6}}, {ID: 40, Students: []int64{3, 5, 6}}}}
	br, _ := scheduling.Solve(bad)
	if br.Infeasibility == nil || len(br.Infeasibility.Clique) != 4 {
		fmt.Println("  BEKLENMEYEN: çözümsüzlük raporlanmadı")
		fail = true
	} else {
		fmt.Printf("  yerleşmeyen: %v · klik: %v\n", br.Unplaced, br.Infeasibility.Clique)
		for _, u := range br.Infeasibility.Exams {
			fmt.Printf("  sınav %d: engelleyenler %v (%d slot çatışmayla kapalı)\n", u.ExamID, u.Blockers, u.SlotsByConflict)
		}
		for _, s := range br.Infeasibility.Reasons {
			fmt.Println("  neden:", s)
		}
	}

	if fail {
		fmt.Println("\nSONUÇ: BAŞARISIZ")
		return 1
	}
	fmt.Println("\nSONUÇ: BAŞARILI (0 ihlal, 60 sn sınırı içinde)")
	return 0
}
