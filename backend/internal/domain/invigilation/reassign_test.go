package invigilation

import (
	"math/rand/v2"
	"testing"
)

// diffOutsideChanges: değişiklik listesi DIŞINDA kalan tüm atamaların aynen kaldığını
// ve değişikliklerin gerçekten planda olduğunu doğrular.
func diffOutsideChanges(t *testing.T, before Plan, res ReassignResult, removed int64) {
	t.Helper()
	type key struct{ d, v int64 }
	b := map[key]bool{}
	for _, a := range before.Assignments {
		b[key{a.DutyID, a.InvigilatorID}] = true
	}
	a := map[key]bool{}
	for _, x := range res.Plan.Assignments {
		a[key{x.DutyID, x.InvigilatorID}] = true
		if x.InvigilatorID == removed {
			t.Fatalf("çıkarılan gözetmen hâlâ görevde: %+v", x)
		}
	}
	expectedRemoved := map[key]bool{}
	expectedAdded := map[key]bool{}
	for _, c := range res.Changes {
		expectedRemoved[key{c.DutyID, c.From}] = true
		expectedAdded[key{c.DutyID, c.To}] = true
	}
	for k := range b {
		if !a[k] && !expectedRemoved[k] && k.v != removed {
			t.Fatalf("kilitli atama değişti: %+v", k)
		}
	}
	for k := range a {
		if !b[k] && !expectedAdded[k] {
			t.Fatalf("raporlanmayan yeni atama: %+v", k)
		}
	}
}

// Doğrudan doldurma mümkünse değişiklik sayısı = boşalan görev sayısı (alt sınır, minimum).
func TestReassignDirectFillIsMinimal(t *testing.T) {
	in := Input{SessionsPerDay: 3, Invigilators: []Invigilator{{ID: 1}, {ID: 2}, {ID: 3}, {ID: 4}}}
	for i := int64(1); i <= 8; i++ {
		in.Duties = append(in.Duties, Duty{ID: i, Slot: int(i-1) * 3, Students: 20})
	}
	before := mustAssign(t, in)
	removed := before.Assignments[0].InvigilatorID
	res, err := Reassign(in, before, removed)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Changes) != res.Vacated || res.Swaps != 0 || res.Vacated == 0 {
		t.Fatalf("doğrudan doldurmada değişiklik = boşalan olmalı: %+v", res)
	}
	if v := Validate(in, res.Plan); len(v) != 0 {
		t.Fatalf("ihlal: %+v", v)
	}
	diffOutsideChanges(t, before, res, removed)
}

// Doğrudan doldurma İMKÂNSIZ, bir takas yeterli: değişiklik = boşalan + 1.
//
//	R (çıkarılacak): görev 1 (slot 0)
//	A: görev 2 (slot 1) -> slot 0 ile aynı gün ardışık, görev 1'i doğrudan alamaz
//	B: slot 0'da mazeretli -> görev 1'i alamaz, ama görev 2'yi alabilir
//	C: görev 1'deki sınavın hocası -> görev 1'i alamaz; görev 3'te (kilitli)
//
// Tek çözüm: B görev 2'yi alır, A görev 1'e geçer (2 değişiklik).
func TestReassignUsesMinimalSwapWhenDirectFillImpossible(t *testing.T) {
	in := Input{SessionsPerDay: 3,
		Duties: []Duty{
			{ID: 1, Slot: 0, Students: 20, Exams: []int64{77}},
			{ID: 2, Slot: 1, Students: 20},
			{ID: 3, Slot: 6, Students: 20},
		},
		Invigilators: []Invigilator{{ID: 100}, {ID: 200}, {ID: 300, Unavailable: []int{0}}, {ID: 400, OwnExams: []int64{77}}}}
	const R, A, B, C = 100, 200, 300, 400
	before := Plan{Assignments: []Assignment{{1, R}, {2, A}, {3, C}}}
	if v := Validate(in, before); len(v) != 0 {
		t.Fatalf("başlangıç planı geçerli olmalı: %+v", v)
	}
	res, err := Reassign(in, before, R)
	if err != nil {
		t.Fatal(err)
	}
	if res.Vacated != 1 || len(res.Changes) != 2 || res.Swaps != 1 {
		t.Fatalf("1 boşalan + 1 takas = 2 değişiklik bekleniyordu: %+v", res)
	}
	want := []Change{{DutyID: 1, From: R, To: A}, {DutyID: 2, From: A, To: B}}
	for i, c := range want {
		if res.Changes[i] != c {
			t.Fatalf("değişiklik %d: %+v, beklenen %+v", i, res.Changes[i], c)
		}
	}
	if v := Validate(in, res.Plan); len(v) != 0 {
		t.Fatalf("ihlal: %+v", v)
	}
	diffOutsideChanges(t, before, res, R)
}

func TestReassignReportsImpossible(t *testing.T) {
	in := Input{SessionsPerDay: 3, Duties: []Duty{{ID: 1, Slot: 0, Students: 20, Exams: []int64{9}}},
		Invigilators: []Invigilator{{ID: 1}, {ID: 2, OwnExams: []int64{9}}}}
	res, err := Reassign(in, Plan{Assignments: []Assignment{{1, 1}}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Changes) != 0 || len(res.Plan.Unfilled) != 1 {
		t.Fatalf("doldurulamayan görev raporlanmalı: %+v", res)
	}
	if _, err := Reassign(in, Plan{}, 999); err == nil {
		t.Fatal("olmayan gözetmen hata vermeli")
	}
}

// Rastgele: Reassign her zaman yalnızca değişiklik listesindeki atamaları oynatır ve
// sonuç planın kısıt ihlali, başlangıç planınınkinden fazla değildir.
func TestReassignRandomLocksEverythingElse(t *testing.T) {
	r := rand.New(rand.NewPCG(11, 11))
	for trial := 0; trial < 25; trial++ {
		in := randomInput(r, 30+r.IntN(30), 10+r.IntN(15))
		before := mustAssign(t, in)
		if len(before.Assignments) == 0 {
			continue
		}
		removed := before.Assignments[r.IntN(len(before.Assignments))].InvigilatorID
		res, err := Reassign(in, before, removed)
		if err != nil {
			t.Fatal(err)
		}
		diffOutsideChanges(t, before, res, removed)
		for _, v := range Validate(in, res.Plan) {
			if v.Kind != "understaffed" {
				t.Fatalf("deneme %d: sert kısıt ihlali %+v", trial, v)
			}
		}
		if len(res.Changes) < res.Vacated-len(res.Plan.Unfilled)+len(before.Unfilled) {
			t.Fatalf("deneme %d: doldurulan görev sayısı tutarsız: %+v", trial, res)
		}
	}
}
