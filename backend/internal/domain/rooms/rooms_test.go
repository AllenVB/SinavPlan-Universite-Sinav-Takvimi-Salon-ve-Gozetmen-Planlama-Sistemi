package rooms

import (
	"reflect"
	"sort"
	"testing"
)

// mk: rows x cols aktif salon; accessible = sütun 0,2,4 (ön sıra).
func mk(id int64, lab bool, rows, cols int, accessible bool) Room {
	r := Room{ID: id, Lab: lab, Rows: rows, Cols: cols}
	for i := 0; i < rows; i++ {
		for j := 0; j < cols; j++ {
			r.Seats = append(r.Seats, Seat{Row: i, Col: j, Active: true, Accessible: accessible && i == 0 && j%2 == 0 && j <= 4})
		}
	}
	return r
}

func mustAssign(t testing.TB, rooms []Room, exams []Exam) Plan {
	t.Helper()
	p, err := Assign(rooms, exams, Config{})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func clean(t testing.TB, rooms []Room, exams []Exam, p Plan) {
	t.Helper()
	if v := Validate(rooms, exams, Config{}, p); len(v) != 0 {
		t.Fatalf("%d ihlal, ilki: %+v", len(v), v[0])
	}
}

func TestEffectiveCapacity(t *testing.T) {
	c := EffectiveCapacity(mk(1, false, 5, 8, true), Config{})
	if c.Active != 40 || c.Single != 20 || c.Accessible != 3 {
		t.Fatalf("5x8: %+v (beklenen 40/20/3)", c)
	}
	// Tek sütunluk dizi: 3 sütun -> ceil(3/2)=2 ; bozuk koltuk diziyi böler.
	r := mk(2, false, 1, 7, false)
	r.Seats[3].Active = false // sütun 3 kapalı: diziler [0..2] ve [4..6] -> 2 + 2
	c = EffectiveCapacity(r, Config{})
	if c.Active != 6 || c.Single != 4 {
		t.Fatalf("bozuk koltuklu: %+v (beklenen 6/4)", c)
	}
	// Sıkı mod: dama tahtası.
	if c := EffectiveCapacity(mk(3, false, 4, 4, false), Config{Strict: true}); c.Single != 8 {
		t.Fatalf("sıkı mod 4x4: %+v", c)
	}
	// Komşu erişilebilir koltuklar tek sayılır.
	adj := mk(4, false, 2, 4, false)
	adj.Seats[0].Accessible, adj.Seats[1].Accessible = true, true
	if c := EffectiveCapacity(adj, Config{}); c.Accessible != 1 {
		t.Fatalf("komşu erişilebilir koltuklar: %+v", c)
	}
}

// KRİTİK ETKİLEŞİM: tek sınavlı salonun kapasitesi karışıktan düşüktür.
func TestSharingFitsWhereSingleExamCannot(t *testing.T) {
	rooms := []Room{mk(1, false, 5, 8, false)} // Active 40, Single 20
	one := []Exam{{ID: 1, Students: 30}}
	if p := mustAssign(t, rooms, one); len(p.Unplaced) != 1 {
		t.Fatalf("tek sınavlı 30 öğrenci 20 etkin koltuğa sığmamalı: %+v", p)
	}
	two := []Exam{{ID: 1, Students: 20}, {ID: 2, Students: 20}} // karışık: 40 sığar
	p := mustAssign(t, rooms, two)
	if len(p.Unplaced) != 0 || p.RoomsUsed != 1 {
		t.Fatalf("iki sınav tek salonu paylaşmalı: %+v", p)
	}
	clean(t, rooms, two, p)
}

func TestLabExamsGoToLabsOnly(t *testing.T) {
	rooms := []Room{mk(1, false, 6, 10, false), mk(2, true, 5, 8, false), mk(3, true, 4, 8, false)}
	exams := []Exam{{ID: 1, Students: 15, Lab: true}, {ID: 2, Students: 40}}
	p := mustAssign(t, rooms, exams)
	for _, pl := range p.Placements {
		isLabRoom := pl.RoomID != 1
		if (pl.ExamID == 1) != isLabRoom {
			t.Fatalf("yanlış salon tipi: %+v", pl)
		}
	}
	clean(t, rooms, exams, p)
	// Lab salonu yoksa lab sınavı yerleşemez; klasik salona KONMAZ.
	p2 := mustAssign(t, rooms[:1], []Exam{{ID: 1, Students: 5, Lab: true}})
	if len(p2.Unplaced) != 1 || len(p2.Placements) != 0 {
		t.Fatalf("lab salon yokken lab sınavı yerleşmemeli: %+v", p2)
	}
}

func TestLargeExamIsSplitAcrossRoomsConsistently(t *testing.T) {
	rooms := []Room{mk(1, false, 5, 8, false), mk(2, false, 5, 8, false), mk(3, false, 5, 8, false)}
	exams := []Exam{{ID: 1, Students: 55}}
	p := mustAssign(t, rooms, exams)
	if len(p.Unplaced) != 0 || p.RoomsUsed != 3 { // her salonda en çok 20 -> 3 salon (20+20+15)
		t.Fatalf("55 öğrenci 3 salona bölünmeli: %+v", p)
	}
	total := 0
	for _, pl := range p.Placements {
		total += pl.Count
	}
	if total != 55 {
		t.Fatalf("paylar toplamı %d", total)
	}
	clean(t, rooms, exams, p)
}

func TestAccessibleSeatsRequiredAndSplitByAvailability(t *testing.T) {
	rooms := []Room{mk(1, false, 5, 8, false), mk(2, false, 5, 8, true), mk(3, false, 5, 8, true)}
	// 5 engelli: salon başına 3 erişilebilir koltuk -> en az 2 erişilebilir salon.
	exams := []Exam{{ID: 1, Students: 30, Disabled: 5}}
	p := mustAssign(t, rooms, exams)
	if len(p.Unplaced) != 0 {
		t.Fatalf("yerleşmeli: %+v", p)
	}
	for _, pl := range p.Placements {
		if pl.Disabled > 0 && pl.RoomID == 1 {
			t.Fatalf("erişilebilir koltuğu olmayan salona engelli konmuş: %+v", pl)
		}
	}
	clean(t, rooms, exams, p)
	// Erişilebilir koltuk yoksa sınav yerleşmez (kısmi yerleştirme yok).
	p2 := mustAssign(t, rooms[:1], []Exam{{ID: 1, Students: 10, Disabled: 1}})
	if len(p2.Unplaced) != 1 || len(p2.Placements) != 0 {
		t.Fatalf("erişilebilir koltuk yokken yerleşmemeli ve kısmi pay bırakmamalı: %+v", p2)
	}
}

func TestFailedExamLeavesNoPartialShares(t *testing.T) {
	rooms := []Room{mk(1, false, 5, 8, false)} // Single 20
	exams := []Exam{{ID: 1, Students: 18}, {ID: 2, Students: 30}}
	p := mustAssign(t, rooms, exams)
	// 30 öğrenci tek salonda yerleşemez; sınav 2 hiçbir pay bırakmamalı.
	for _, pl := range p.Placements {
		if pl.ExamID == 2 {
			t.Fatalf("yerleşemeyen sınavın kalıntı payı var: %+v", pl)
		}
	}
	if len(p.Unplaced) != 1 || p.Unplaced[0].ExamID != 2 {
		t.Fatalf("sınav 2 yerleşemedi olarak bildirilmeli: %+v", p.Unplaced)
	}
	clean(t, rooms, exams, p)
}

func TestConsolidationReducesRoomCount(t *testing.T) {
	// Üç küçük sınav, her biri tek başına bir salona sığar ama paylaşınca tek salon yeter.
	rooms := []Room{mk(1, false, 5, 8, false), mk(2, false, 5, 8, false), mk(3, false, 5, 8, false)}
	exams := []Exam{{ID: 1, Students: 12}, {ID: 2, Students: 12}, {ID: 3, Students: 12}}
	p := mustAssign(t, rooms, exams)
	if p.RoomsUsed != 1 {
		t.Fatalf("36 öğrenci karışık tek salona (40) sığar, %d salon kullanıldı", p.RoomsUsed)
	}
	clean(t, rooms, exams, p)
}

// --- Determinizm -----------------------------------------------------------------

func scenario() ([]Room, []Exam) {
	var rooms []Room
	for i := int64(1); i <= 12; i++ {
		rooms = append(rooms, mk(i, i > 9, 4+int(i%4), 6+int(i%5), i%3 != 0))
	}
	var exams []Exam
	for i := int64(1); i <= 14; i++ {
		exams = append(exams, Exam{ID: i, Students: int(20 + (i*37)%90), Disabled: int(i % 4), Lab: i%7 == 0})
	}
	return rooms, exams
}

func TestDeterministicSameInputSameOutput(t *testing.T) {
	rooms, exams := scenario()
	a, b := mustAssign(t, rooms, exams), mustAssign(t, rooms, exams)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("aynı girdi farklı plan üretti")
	}
}

func TestDeterministicRegardlessOfInputOrder(t *testing.T) {
	rooms, exams := scenario()
	base := mustAssign(t, rooms, exams)
	for shift := 1; shift < 6; shift++ {
		r2 := append(append([]Room(nil), rooms[shift:]...), rooms[:shift]...)
		e2 := append(append([]Exam(nil), exams[shift:]...), exams[:shift]...)
		sort.SliceStable(e2, func(a, b int) bool { return (e2[a].ID*7+int64(shift))%11 < (e2[b].ID*7+int64(shift))%11 })
		if got := mustAssign(t, r2, e2); !reflect.DeepEqual(base, got) {
			t.Fatalf("girdi sırası (kaydırma %d) sonucu değiştirdi", shift)
		}
	}
}

func TestRandomScenariosAlwaysValid(t *testing.T) {
	for trial := int64(0); trial < 40; trial++ {
		var rooms []Room
		for i := int64(1); i <= 6+trial%5; i++ {
			rooms = append(rooms, mk(i, i%4 == 0, 3+int(i+trial)%5, 5+int(i*trial)%7, (i+trial)%2 == 0))
		}
		var exams []Exam
		for i := int64(1); i <= 5+trial%6; i++ {
			n := int(5 + (i*13+trial*17)%80)
			exams = append(exams, Exam{ID: i, Students: n, Disabled: int((i + trial) % 3), Lab: i%5 == 0})
		}
		p := mustAssign(t, rooms, exams)
		if v := Validate(rooms, exams, Config{}, p); len(v) != 0 {
			t.Fatalf("deneme %d: %+v", trial, v[0])
		}
	}
}

func TestAssignRejectsInvalidExam(t *testing.T) {
	if _, err := Assign(nil, []Exam{{ID: 1, Students: 3, Disabled: 4}}, Config{}); err == nil {
		t.Fatal("engelli > öğrenci reddedilmeli")
	}
	if _, err := Assign(nil, []Exam{{ID: 1, Students: -1}}, Config{}); err == nil {
		t.Fatal("negatif öğrenci reddedilmeli")
	}
	if p, err := Assign(nil, []Exam{{ID: 1, Students: 0}}, Config{}); err != nil || len(p.Unplaced) != 0 {
		t.Fatalf("0 öğrencili sınav sorunsuz atlanmalı: %v %+v", err, p)
	}
}

// --- Doğrulayıcı: bilerek bozulmuş planlarda ihlal yakalar -----------------------

func TestValidatorCatchesBrokenPlans(t *testing.T) {
	rooms := []Room{mk(1, false, 5, 8, true), mk(2, true, 5, 8, false)} // Active 40, Single 20
	exams := []Exam{{ID: 1, Students: 30, Disabled: 2}, {ID: 2, Students: 10, Lab: true}}
	kinds := func(p Plan) map[string]int {
		out := map[string]int{}
		for _, v := range Validate(rooms, exams, Config{}, p) {
			out[v.Kind]++
		}
		return out
	}
	good := Plan{Placements: []Placement{
		{1, 1, 20, 2}, {1, 1, 0, 0}, // geçersiz sıfır pay aşağıda ayrı
	}}
	_ = good
	cases := []struct {
		name string
		plan Plan
		want map[string]int
	}{
		{"geçerli (bölünmüş)", Plan{Placements: []Placement{{1, 1, 20, 2}, {2, 2, 10, 0}, {1, 2, 10, 0}}}, map[string]int{"room_type": 1, "capacity": 0}},
		{"eksik öğrenci", Plan{Placements: []Placement{{1, 1, 20, 2}, {2, 2, 10, 0}}}, map[string]int{"not_fully_seated": 1}},
		{"fazla öğrenci", Plan{Placements: []Placement{{1, 1, 20, 2}, {1, 1, 20, 0}, {2, 2, 10, 0}}}, map[string]int{"duplicate": 1, "not_fully_seated": 1, "capacity": 1}},
		{"lab sınavı klasik salonda", Plan{Placements: []Placement{{1, 1, 20, 2}, {1, 2, 10, 0}, {2, 1, 10, 0}}}, map[string]int{"room_type": 2}},
		{"tek sınav payı kapasiteyi aşıyor", Plan{Placements: []Placement{{1, 1, 30, 2}, {2, 2, 10, 0}}}, map[string]int{"capacity": 2}},
		{"engelli payı tutarsız", Plan{Placements: []Placement{{1, 1, 20, 1}, {1, 2, 10, 0}, {2, 2, 10, 0}}}, map[string]int{"split_inconsistent": 1, "room_type": 1}},
		{"erişilebilir koltuk yetmiyor", Plan{Placements: []Placement{{1, 1, 20, 2}, {2, 2, 10, 0}, {1, 2, 10, 0}}}, map[string]int{"room_type": 1}},
		{"olmayan salon ve sınav", Plan{Placements: []Placement{{9, 9, 5, 0}}}, map[string]int{"unknown_exam": 1, "unknown_room": 1, "not_fully_seated": 2}},
		{"geçersiz pay", Plan{Placements: []Placement{{1, 1, 0, 0}, {1, 1, 5, 9}}}, map[string]int{"bad_count": 2, "duplicate": 1, "not_fully_seated": 2}},
	}
	for _, c := range cases {
		got := kinds(c.plan)
		for k, n := range c.want {
			if n == 0 {
				delete(c.want, k)
			}
		}
		for k, n := range c.want {
			if got[k] < n {
				t.Errorf("%s: %q en az %d bekleniyordu, bulunan %v", c.name, k, n, got)
			}
		}
	}
	// Kesin sayı denetimi: tam geçerli bir plan sıfır ihlal verir.
	rooms2 := []Room{mk(1, false, 5, 8, true)}
	exams2 := []Exam{{ID: 1, Students: 20, Disabled: 3}}
	if v := Validate(rooms2, exams2, Config{}, Plan{Placements: []Placement{{1, 1, 20, 3}}}); len(v) != 0 {
		t.Fatalf("geçerli plan: %+v", v)
	}
	// Erişilebilir koltuk sayısını aşan engelli payı.
	v := Validate(rooms2, []Exam{{ID: 1, Students: 20, Disabled: 4}}, Config{}, Plan{Placements: []Placement{{1, 1, 20, 4}}})
	if len(v) != 1 || v[0].Kind != "accessible" {
		t.Fatalf("4 engelli > 3 erişilebilir koltuk: %+v", v)
	}
	// Bildirilen ama payı olan yerleşemeyen sınav.
	v = Validate(rooms2, exams2, Config{}, Plan{Placements: []Placement{{1, 1, 20, 3}}, Unplaced: []Unplaced{{ExamID: 1}}})
	if len(v) != 1 || v[0].Kind != "split_inconsistent" {
		t.Fatalf("yerleşemedi denip payı olan sınav: %+v", v)
	}
	// Bildirilmiş yerleşemeyen sınav ihlal sayılmaz.
	if v := Validate(rooms2, exams2, Config{}, Plan{Unplaced: []Unplaced{{ExamID: 1, Students: 20}}}); len(v) != 0 {
		t.Fatalf("bildirilmiş yerleşemeyen sınav ihlal olmamalı: %+v", v)
	}
}

// Regresyon: başka sınavın engelli öğrencisi, payı Single'a eşit bir sınavın zorunlu
// koltuklarından birini alır. 2x5 salon: Single=6, Active=10, erişilebilir sütun 0,2,4.
// A=6 (tam Single) ve B=4 (1 engelli) aynı salona KONMAMALI; ayrı ayrı sığarlar.
func TestOtherExamsDisabledLowersShareCeiling(t *testing.T) {
	room := mk(1, false, 2, 5, true)
	if c := EffectiveCapacity(room, Config{}); c.Single != 6 || c.Active != 10 {
		t.Fatalf("varsayım: %+v", c)
	}
	both := []Exam{{ID: 1, Students: 6}, {ID: 2, Students: 4, Disabled: 1}}
	p := mustAssign(t, []Room{room}, both)
	if len(p.Unplaced) != 1 {
		t.Fatalf("A(6)+B(4, 1 engelli) tek salona sığmamalı: %+v", p)
	}
	clean(t, []Room{room}, both, p)

	// Kural gevşek olsaydı (pay ≤ Single), doğrulayıcı bu planı reddetmeli.
	bad := Plan{Placements: []Placement{{1, 1, 6, 0}, {2, 1, 4, 1}}}
	v := Validate([]Room{room}, both, Config{}, bad)
	if len(v) == 0 {
		t.Fatal("doğrulayıcı A=Single + başkasının engellisi kombinasyonunu reddetmeli")
	}
	// İki salon varsa sorunsuz ayrı ayrı yerleşir.
	two := []Room{room, mk(2, false, 2, 5, true)}
	if p2 := mustAssign(t, two, both); len(p2.Unplaced) != 0 {
		t.Fatalf("iki salonda yerleşmeli: %+v", p2)
	}
}
