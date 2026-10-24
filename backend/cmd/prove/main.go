// prove: çekirdek görevlerin kanıt komutları. Veritabanı gerektirmez; simülatörün
// deterministik verisini (seed=42) üretip saf domain paketlerini çalıştırır.
//
//	prove t1 [--seed 42] [--iterations 1000000] [--time-limit 60s] [--days 15]
//	prove t2     salon atama: T-1 çizelgesindeki her slot için, determinizm dahil
//	prove t3     oturma düzeni: ihlal sayacı + bilerek bozulmuş planlar
//	prove t4     gözetmen atama: kısıtlar, adalet, bir gözetmen çıkarılınca minimum değişiklik
//	prove all    t1, t2, t3, t4
//
// Çıkış kodu: herhangi bir sert kısıt ihlali, yerleşmeyen sınav veya süre sınırının
// aşılması durumunda 1.
package main

import (
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"reflect"
	"sort"
	"time"

	"github.com/AllenVB/sinavplan/backend/internal/domain/invigilation"
	"github.com/AllenVB/sinavplan/backend/internal/domain/rooms"
	"github.com/AllenVB/sinavplan/backend/internal/domain/scheduling"
	"github.com/AllenVB/sinavplan/backend/internal/domain/seating"
	"github.com/AllenVB/sinavplan/backend/internal/planning"
	"github.com/AllenVB/sinavplan/backend/internal/simdata"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "kullanım: prove <t1|t2|t3|t4|all> [bayraklar]")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "t1":
		os.Exit(proveT1(os.Args[2:]))
	case "t2":
		os.Exit(proveT2(os.Args[2:]))
	case "t3":
		os.Exit(proveT3(os.Args[2:]))
	case "t4":
		os.Exit(proveT4(os.Args[2:]))
	case "all":
		code := 0
		for _, f := range []func([]string) int{proveT1, proveT2, proveT3, proveT4} {
			if c := f(nil); c != 0 {
				code = c
			}
			fmt.Println()
		}
		os.Exit(code)
	default:
		fmt.Fprintf(os.Stderr, "bilinmeyen kanıt: %s\n", os.Args[1])
		os.Exit(2)
	}
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
	in := planning.SchedulingInput(ds, *seed)
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

// pipeline: T-1'i (hızlı bütçeyle) çalıştırıp T-2 ve T-3'ü uygular.
func pipeline(seed int64, iters int) (*simdata.Dataset, *planning.Result, rooms.Config, error) {
	ds, err := simdata.Generate(simdata.DefaultConfig(seed))
	if err != nil {
		return nil, nil, rooms.Config{}, err
	}
	in := planning.SchedulingInput(ds, seed)
	in.MaxIterations = iters
	r, err := scheduling.Solve(in)
	if err != nil {
		return nil, nil, rooms.Config{}, err
	}
	if len(r.Unplaced) != 0 {
		return nil, nil, rooms.Config{}, fmt.Errorf("T-1 %d sınavı yerleştiremedi", len(r.Unplaced))
	}
	cfg := rooms.Config{}
	res, err := planning.Plan(ds, r.Assignment, cfg)
	return ds, res, cfg, err
}

func proveT2(args []string) int {
	fs := flag.NewFlagSet("t2", flag.ExitOnError)
	seed := fs.Int64("seed", 42, "tohum")
	iters := fs.Int("iterations", 200_000, "T-1 iterasyon bütçesi (girdi çizelgesi için)")
	_ = fs.Parse(args)

	fmt.Println("== T-2 Salon atama — kanıt ==")
	ds, res, cfg, err := pipeline(*seed, *iters)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	rr, _ := planning.Rooms(ds)

	// Etkin kapasite: karışık salon ile tek sınavlı salon farkı (kritik etkileşim).
	var active, single int
	for _, r := range rr {
		c := rooms.EffectiveCapacity(r, cfg)
		active += c.Active
		single += c.Single
	}
	fmt.Printf("salonlar: %d, aktif koltuk %d, tek sınavlı etkin kapasite %d (%.0f%%): karışık salon neredeyse 2 kat öğrenci alır\n",
		len(rr), active, single, 100*float64(single)/float64(active))

	tot := res.Totals()
	var lower, naive int
	for _, s := range res.Slots {
		students := 0
		for _, p := range s.Rooms.Placements {
			students += p.Count
		}
		// Alt sınır: öğrenci sayısını en büyük salon(lar)ın aktif kapasitesine böl.
		caps := make([]int, 0, len(rr))
		for _, r := range rr {
			caps = append(caps, rooms.EffectiveCapacity(r, cfg).Active)
		}
		sortDesc(caps)
		left, k := students, 0
		for left > 0 && k < len(caps) {
			left -= caps[k]
			k++
		}
		lower += k
		naive += len(s.Rooms.Placements)
	}

	// Determinizm: aynı çizelge ikinci kez planlanınca birebir aynı çıktı.
	_, res2, _, err := pipeline(*seed, *iters)
	same := err == nil && reflect.DeepEqual(res, res2)

	row := func(k string, v any) { fmt.Printf("  %-46s %v\n", k, v) }
	row("slot sayısı", tot.Slots)
	row("yerleştirilemeyen sınav", tot.ExamsUnplaced)
	row("DOĞRULAYICI İHLALİ (kapasite/tip/bölme/erişilebilir)", tot.RoomViolations)
	row("kullanılan salon (slot toplamı)", tot.RoomsUsed)
	row("alt sınır (en büyük salonlarla, paylaşım serbest)", lower)
	row("paylar toplamı (bölme/paylaşım parçaları)", naive)
	row("aynı girdi, ikinci çalıştırma birebir aynı mı", same)

	if tot.ExamsUnplaced > 0 || tot.RoomViolations > 0 || !same {
		fmt.Println("\nSONUÇ: BAŞARISIZ")
		return 1
	}
	fmt.Println("\nSONUÇ: BAŞARILI")
	return 0
}

func proveT3(args []string) int {
	fs := flag.NewFlagSet("t3", flag.ExitOnError)
	seed := fs.Int64("seed", 42, "tohum")
	iters := fs.Int("iterations", 200_000, "T-1 iterasyon bütçesi")
	_ = fs.Parse(args)

	fmt.Println("== T-3 Kopya önleyici oturma — kanıt ==")
	ds, res, cfg, err := pipeline(*seed, *iters)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	_, sr := planning.Rooms(ds)

	seated, disabled, onAccessible := 0, 0, 0
	var total seating.Violations
	for _, s := range res.Slots {
		for rid, p := range s.Seating {
			v := seating.CountViolations(sr[rid], p, cfg.Strict)
			total.AdjacentSameExam += v.AdjacentSameExam
			total.SeatDoubleBooked += v.SeatDoubleBooked
			total.StudentSeatedTwice += v.StudentSeatedTwice
			total.InvalidSeat += v.InvalidSeat
			total.DisabledNotAccessible += v.DisabledNotAccessible
			seated += len(p.Assignments)
			for _, a := range p.Assignments {
				if a.Disabled {
					disabled++
					if a.Row == 0 && a.Col%2 == 0 {
						onAccessible++
					}
				}
			}
		}
	}
	tot := res.Totals()
	row := func(k string, v any) { fmt.Printf("  %-46s %v\n", k, v) }
	row("oturan öğrenci-sınav çifti / kayıt", fmt.Sprintf("%d / %d", seated, len(ds.Enrollments)))
	row("yerleşemeyen öğrenci", tot.Unseated)
	row("engelli öğrenci (erişilebilir koltukta)", fmt.Sprintf("%d (%d)", disabled, onAccessible))
	row("İHLAL: yan yana aynı sınav", total.AdjacentSameExam)
	row("İHLAL: koltuk çift rezerve", total.SeatDoubleBooked)
	row("İHLAL: öğrenci birden çok koltukta", total.StudentSeatedTwice)
	row("İHLAL: geçersiz koltuk", total.InvalidSeat)
	row("İHLAL: engelli erişilemez koltukta", total.DisabledNotAccessible)
	row("TOPLAM İHLAL", total.Total())

	// Sayacın kendisinin doğru çalıştığının kanıtı: bilerek bozulmuş plan.
	fmt.Println("\n  bilerek bozulmuş plan (sayaç sınaması):")
	room := seating.Room{ID: 1, Rows: 1, Cols: 4, Seats: []seating.Seat{
		{ID: 1, Row: 0, Col: 0, Active: true, Accessible: true}, {ID: 2, Row: 0, Col: 1, Active: true},
		{ID: 3, Row: 0, Col: 2, Active: true}, {ID: 4, Row: 0, Col: 3, Active: true}}}
	broken := seating.Plan{Assignments: []seating.Assignment{
		{ExamID: 1, StudentID: 1, SeatID: 2, Row: 0, Col: 1},                  // 1-2 komşu
		{ExamID: 1, StudentID: 2, SeatID: 3, Row: 0, Col: 2},                  // 2-3 komşu
		{ExamID: 2, StudentID: 3, SeatID: 3, Row: 0, Col: 2},                  // koltuk 3 çift rezerve
		{ExamID: 2, StudentID: 4, Disabled: true, SeatID: 4, Row: 0, Col: 3}}} // engelli erişilemez koltukta
	bv := seating.CountViolations(room, broken, false)
	fmt.Printf("    bulunan: %+v (toplam %d)\n", bv, bv.Total())
	want := seating.Violations{AdjacentSameExam: 1, SeatDoubleBooked: 1, DisabledNotAccessible: 1}
	fmt.Printf("    beklenen: %+v\n", want)

	if total.Total() > 0 || tot.Unseated > 0 || seated != len(ds.Enrollments) || bv != want {
		fmt.Println("\nSONUÇ: BAŞARISIZ")
		return 1
	}
	fmt.Println("\nSONUÇ: BAŞARILI (0 ihlal; sayaç bozuk planda doğru sayıyı buldu)")
	return 0
}

func sortDesc(a []int) { sort.Sort(sort.Reverse(sort.IntSlice(a))) }

func proveT4(args []string) int {
	fs := flag.NewFlagSet("t4", flag.ExitOnError)
	seed := fs.Int64("seed", 42, "tohum")
	iters := fs.Int("iterations", 200_000, "T-1 iterasyon bütçesi")
	_ = fs.Parse(args)

	fmt.Println("== T-4 Adil gözetmen atama — kanıt ==")
	ds, res, _, err := pipeline(*seed, *iters)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	// Mazeretleri deterministik simüle et: 10 gözetmen, her biri 3 slotta mazeretli.
	in0 := planning.InvigilationInput(ds, res, nil)
	r := rand.New(rand.NewPCG(uint64(*seed), 4))
	excused := map[int64][]int{}
	for _, k := range r.Perm(len(in0.Invigilators))[:10] {
		id := in0.Invigilators[k].ID
		for j := 0; j < 3; j++ {
			excused[id] = append(excused[id], r.IntN(ds.Config.Days*ds.Config.SessionsPerDay))
		}
	}
	in := planning.InvigilationInput(ds, res, excused)

	start := time.Now()
	p, err := invigilation.Assign(in)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	dur := time.Since(start)
	v := invigilation.Validate(in, p)
	m := invigilation.Fairness(in, p)
	big, own := 0, 0
	for _, d := range in.Duties {
		if d.Required() == 2 {
			big++
		}
	}
	for _, iv := range in.Invigilators {
		if len(iv.OwnExams) > 0 {
			own++
		}
	}

	row := func(k string, x any) { fmt.Printf("  %-46s %v\n", k, x) }
	row("görev (slot x salon) / >50 öğrenci (2 gözetmen)", fmt.Sprintf("%d / %d", len(in.Duties), big))
	row("gözetmen (kendi dersi olan) / mazeretli", fmt.Sprintf("%d (%d) / %d", len(in.Invigilators), own, len(excused)))
	row("süre", dur.Round(time.Millisecond))
	row("atama / doldurulamayan kontenjan", fmt.Sprintf("%d / %d", m.Assignments, len(p.Unfilled)))
	row("DOĞRULAYICI İHLALİ", len(v))
	row("görev yükü min / max / ortalama", fmt.Sprintf("%d / %d / %.2f", m.Min, m.Max, m.Mean))
	row("adalet: max - min (ideal)", fmt.Sprintf("%d (%d)", m.Spread, m.IdealSpread))

	// Bir gözetmen hastalanıp çıkarılıyor: en çok görevi olan.
	load := map[int64]int{}
	for _, a := range p.Assignments {
		load[a.InvigilatorID]++
	}
	var removed int64
	for _, iv := range in.Invigilators {
		if load[iv.ID] > load[removed] || removed == 0 {
			removed = iv.ID
		}
	}
	rr, err := invigilation.Reassign(in, p, removed)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	rv := invigilation.Validate(in, rr.Plan)

	// Kilit kanıtı: değişiklik listesi dışındaki her atama aynen duruyor mu?
	type key struct{ d, v int64 }
	after := map[key]bool{}
	for _, a := range rr.Plan.Assignments {
		after[key{a.DutyID, a.InvigilatorID}] = true
	}
	changedFrom := map[key]bool{}
	for _, c := range rr.Changes {
		changedFrom[key{c.DutyID, c.From}] = true
	}
	untouched, moved := 0, 0
	for _, a := range p.Assignments {
		k := key{a.DutyID, a.InvigilatorID}
		switch {
		case after[k]:
			untouched++
		case changedFrom[k]:
			moved++
		}
	}
	// Adalet: çıkarılan gözetmen hariç.
	var rest invigilation.Input = in
	rest.Invigilators = nil
	for _, iv := range in.Invigilators {
		if iv.ID != removed {
			rest.Invigilators = append(rest.Invigilators, iv)
		}
	}
	m2 := invigilation.Fairness(rest, rr.Plan)

	fmt.Printf("\n  gözetmen %d çıkarıldı (%d görevi vardı):\n", removed, load[removed])
	row("boşalan görev (alt sınır)", rr.Vacated)
	row("değişen atama (= boşalan + takas)", fmt.Sprintf("%d (= %d + %d)", len(rr.Changes), rr.Vacated, rr.Swaps))
	row("dokunulmayan atama / toplam", fmt.Sprintf("%d / %d", untouched, len(p.Assignments)))
	row("yeniden atama sonrası İHLAL", len(rv))
	row("sonrası adalet max - min", m2.Spread)
	for i, c := range rr.Changes {
		if i == 4 {
			fmt.Printf("    ... %d değişiklik daha\n", len(rr.Changes)-4)
			break
		}
		fmt.Printf("    görev %d: %d -> %d\n", c.DutyID, c.From, c.To)
	}

	ok := len(v) == 0 && len(p.Unfilled) == 0 && len(rv) == 0 &&
		untouched+moved == len(p.Assignments) && len(rr.Changes) == rr.Vacated+rr.Swaps
	if !ok {
		fmt.Println("\nSONUÇ: BAŞARISIZ")
		return 1
	}
	fmt.Println("\nSONUÇ: BAŞARILI")
	return 0
}
