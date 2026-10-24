package invigilation

import (
	"math/rand/v2"
	"reflect"
	"testing"
)

func mustAssign(t testing.TB, in Input) Plan {
	t.Helper()
	p, err := Assign(in)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func kinds(vs []Violation) map[string]int {
	m := map[string]int{}
	for _, v := range vs {
		m[v.Kind]++
	}
	return m
}

func assignedTo(p Plan, duty int64) []int64 {
	var out []int64
	for _, a := range p.Assignments {
		if a.DutyID == duty {
			out = append(out, a.InvigilatorID)
		}
	}
	return out
}

func TestRequiredStaffing(t *testing.T) {
	if (Duty{Students: 50}).Required() != 1 || (Duty{Students: 51}).Required() != 2 {
		t.Fatal("50'yi aşan salona 2, diğerlerine 1 gözetmen")
	}
	in := Input{SessionsPerDay: 3, Duties: []Duty{{ID: 1, Slot: 0, Students: 80}, {ID: 2, Slot: 0, Students: 20}},
		Invigilators: []Invigilator{{ID: 10}, {ID: 11}, {ID: 12}}}
	p := mustAssign(t, in)
	if len(assignedTo(p, 1)) != 2 || len(assignedTo(p, 2)) != 1 {
		t.Fatalf("80 öğrencili salona 2, 20 öğrenciliye 1: %+v", p)
	}
	if v := Validate(in, p); len(v) != 0 {
		t.Fatalf("ihlal: %+v", v)
	}
}

func TestHardConstraintsHonoured(t *testing.T) {
	// 10: sınav 100'ün hocası; 11: slot 0'da mazeretli; 12: slot 1'de görevli olacak;
	// slot 0 ve slot 1 aynı gün ardışık -> 12 slot 0'a alınamaz.
	in := Input{SessionsPerDay: 3,
		Duties: []Duty{
			{ID: 1, Slot: 1, Students: 10, Exams: []int64{200}},
			{ID: 2, Slot: 0, Students: 10, Exams: []int64{100}},
		},
		Invigilators: []Invigilator{{ID: 10, OwnExams: []int64{100}}, {ID: 11, Unavailable: []int{0}}, {ID: 12}, {ID: 13}}}
	p := mustAssign(t, in)
	if v := Validate(in, p); len(v) != 0 {
		t.Fatalf("ihlal: %+v", v)
	}
	for _, x := range assignedTo(p, 2) {
		if x == 10 || x == 11 {
			t.Fatalf("görev 2'ye kısıtlı gözetmen atanmış: %d", x)
		}
	}
}

func TestUnfillableDutyIsReported(t *testing.T) {
	in := Input{SessionsPerDay: 3, Duties: []Duty{{ID: 1, Slot: 0, Students: 99, Exams: []int64{7}}},
		Invigilators: []Invigilator{{ID: 1, OwnExams: []int64{7}}, {ID: 2}}}
	p := mustAssign(t, in)
	if len(p.Unfilled) != 1 || p.Unfilled[0].Missing != 1 {
		t.Fatalf("1 eksik kontenjan raporlanmalı: %+v", p)
	}
	if k := kinds(Validate(in, p)); k["understaffed"] != 1 || len(k) != 1 {
		t.Fatalf("yalnız understaffed bekleniyordu: %v", k)
	}
}

// Kısıtsız durumda adalet: 30 görev, 7 gözetmen -> herkes 4 ya da 5 (fark 1 = ideal).
func TestFairnessReachesIdealWithoutConstraints(t *testing.T) {
	in := Input{SessionsPerDay: 3}
	for i := int64(1); i <= 30; i++ {
		in.Duties = append(in.Duties, Duty{ID: i, Slot: int(i-1) / 2 * 3, Students: 10}) // her gün 2 görev, farklı günler
	}
	for v := int64(1); v <= 7; v++ {
		in.Invigilators = append(in.Invigilators, Invigilator{ID: v})
	}
	p := mustAssign(t, in)
	m := Fairness(in, p)
	if m.Spread != m.IdealSpread || m.Assignments != 30 {
		t.Fatalf("fark ideal olmalı: %+v", m)
	}
}

// Takas tabanlı iyileştirme: dengesiz bir durumdan başlayıp farkı düşürür.
func TestImproveReducesSpread(t *testing.T) {
	in := Input{SessionsPerDay: 1}
	for i := int64(1); i <= 12; i++ {
		in.Duties = append(in.Duties, Duty{ID: i, Slot: int(i) * 2, Students: 5})
	}
	in.Invigilators = []Invigilator{{ID: 1}, {ID: 2}, {ID: 3}}
	st, _ := newState(in)
	for d := range st.duties { // hepsini 1 numaraya yığ
		st.assign(0, d)
	}
	before := Fairness(in, st.plan())
	improve(st)
	after := Fairness(in, st.plan())
	if before.Spread != 12 || after.Spread != 0 {
		t.Fatalf("fark 12 -> 0 bekleniyordu: %d -> %d", before.Spread, after.Spread)
	}
	if v := Validate(in, st.plan()); len(v) != 0 {
		t.Fatalf("iyileştirme ihlal üretmemeli: %+v", v)
	}
}

// Doğrudan devir imkânsızken ara kişi üzerinden takas zinciri farkı düşürür.
func TestImproveUsesSwapChainWhenDirectTransferImpossible(t *testing.T) {
	// Gözetmen 3 yalnızca slot 10'u alabilir (diğer slotlarda mazeretli); slot 10 görevi
	// 2'de. 1 çok yüklü ve slot 10'a sahip değil. Zincir: 1 -> 2 (bir görev), 2 -> 3 (slot 10).
	in := Input{SessionsPerDay: 1, Invigilators: []Invigilator{{ID: 1}, {ID: 2}, {ID: 3, Unavailable: []int{0, 2, 4, 6, 8}}}}
	for i := 0; i < 5; i++ {
		in.Duties = append(in.Duties, Duty{ID: int64(i + 1), Slot: i * 2, Students: 5})
	}
	in.Duties = append(in.Duties, Duty{ID: 6, Slot: 10, Students: 5})
	st, _ := newState(in)
	for d := 0; d < 5; d++ {
		st.assign(0, d) // 1: 5 görev
	}
	st.assign(1, 5) // 2: slot 10
	improve(st)
	m := Fairness(in, st.plan())
	// 3 yalnızca slot 10'u alabilir -> en çok 1 görev; kalan 5 görev 1 ve 2'ye 3+2
	// bölünür, yani fark 2 bu girdinin OPTİMUMUdur (başlangıç: 5/1/0 -> fark 5).
	if m.Spread != 2 || m.Max != 3 {
		t.Fatalf("optimum fark 2 (yükler 3/2/1) bekleniyordu: %+v (plan %+v)", m, st.plan())
	}
	if len(assignedTo(st.plan(), 6)) != 1 || assignedTo(st.plan(), 6)[0] != 3 {
		t.Fatalf("slot 10 görevi 3'e geçmeli: %+v", st.plan())
	}
}

func TestValidatorCatchesBrokenPlans(t *testing.T) {
	in := Input{SessionsPerDay: 3,
		Duties:       []Duty{{ID: 1, Slot: 0, Students: 60, Exams: []int64{5}}, {ID: 2, Slot: 1, Students: 10}, {ID: 3, Slot: 0, Students: 10}},
		Invigilators: []Invigilator{{ID: 1, OwnExams: []int64{5}}, {ID: 2, Unavailable: []int{1}}, {ID: 3}, {ID: 4}}}
	cases := []struct {
		name string
		p    Plan
		want map[string]int
	}{
		{"geçerli", Plan{Assignments: []Assignment{{1, 3}, {1, 4}, {2, 1}, {3, 2}}}, map[string]int{}},
		{"kendi dersi", Plan{Assignments: []Assignment{{1, 1}, {1, 4}, {2, 3}, {3, 2}}}, map[string]int{"own_exam": 1, "consecutive_session": 0}},
		{"mazeretli", Plan{Assignments: []Assignment{{1, 3}, {1, 4}, {2, 2}, {3, 1}}}, map[string]int{"excused": 1}},
		{"aynı slot", Plan{Assignments: []Assignment{{1, 3}, {1, 4}, {2, 1}, {3, 3}}}, map[string]int{"double_slot": 1}},
		{"ardışık oturum", Plan{Assignments: []Assignment{{1, 3}, {1, 4}, {2, 3}, {3, 2}}}, map[string]int{"consecutive_session": 1}},
		{"eksik kadro", Plan{Assignments: []Assignment{{1, 3}, {2, 1}, {3, 2}}}, map[string]int{"understaffed": 1}},
		{"tekrar", Plan{Assignments: []Assignment{{1, 3}, {1, 3}, {1, 4}, {2, 1}, {3, 2}}}, map[string]int{"duplicate": 1}},
		{"bilinmeyen", Plan{Assignments: []Assignment{{1, 3}, {1, 4}, {2, 1}, {3, 2}, {9, 3}, {2, 99}}}, map[string]int{"unknown_duty": 1, "unknown_invigilator": 1}},
	}
	for _, c := range cases {
		got := kinds(Validate(in, c.p))
		for k, n := range c.want {
			if n == 0 {
				delete(c.want, k)
			}
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %v, beklenen %v", c.name, got, c.want)
		}
	}
}

func TestDeterministic(t *testing.T) {
	in := randomInput(rand.New(rand.NewPCG(1, 1)), 40, 12)
	a, b := mustAssign(t, in), mustAssign(t, in)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("aynı girdi farklı plan")
	}
	// Girdi sırası sonucu değiştirmez.
	rev := in
	rev.Duties = append([]Duty(nil), in.Duties...)
	rev.Invigilators = append([]Invigilator(nil), in.Invigilators...)
	for i, j := 0, len(rev.Duties)-1; i < j; i, j = i+1, j-1 {
		rev.Duties[i], rev.Duties[j] = rev.Duties[j], rev.Duties[i]
	}
	for i, j := 0, len(rev.Invigilators)-1; i < j; i, j = i+1, j-1 {
		rev.Invigilators[i], rev.Invigilators[j] = rev.Invigilators[j], rev.Invigilators[i]
	}
	if !reflect.DeepEqual(a, mustAssign(t, rev)) {
		t.Fatal("girdi sırası sonucu değiştirdi")
	}
}

func randomInput(r *rand.Rand, duties, invs int) Input {
	in := Input{SessionsPerDay: 3}
	for i := 1; i <= duties; i++ {
		var exams []int64
		for k := 0; k < 1+r.IntN(2); k++ {
			exams = append(exams, int64(r.IntN(20)))
		}
		in.Duties = append(in.Duties, Duty{ID: int64(i), Slot: r.IntN(15), RoomID: int64(r.IntN(8)),
			Students: 10 + r.IntN(90), Exams: exams})
	}
	for v := 1; v <= invs; v++ {
		iv := Invigilator{ID: int64(100 + v)}
		if r.IntN(3) == 0 {
			iv.OwnExams = []int64{int64(r.IntN(20))}
		}
		if r.IntN(3) == 0 {
			iv.Unavailable = []int{r.IntN(15)}
		}
		in.Invigilators = append(in.Invigilators, iv)
	}
	return in
}

// Rastgele girdilerde plan yalnızca (gerçekten doldurulamayan) eksik kadro içerebilir.
func TestRandomPlansRespectHardConstraints(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 7))
	for trial := 0; trial < 30; trial++ {
		in := randomInput(r, 20+r.IntN(40), 6+r.IntN(20))
		p := mustAssign(t, in)
		k := kinds(Validate(in, p))
		missing := 0
		for _, u := range p.Unfilled {
			missing += u.Missing
		}
		if k["understaffed"] != len(p.Unfilled) || len(k) > 1 || (len(k) == 1 && k["understaffed"] == 0) {
			t.Fatalf("deneme %d: beklenmeyen ihlaller %v (eksik %d)", trial, k, missing)
		}
	}
}
