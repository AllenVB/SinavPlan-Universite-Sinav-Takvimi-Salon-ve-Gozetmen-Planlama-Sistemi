// Package seating, bir salondaki koltuk yerleşimini (T-3) yapar: aynı sınavın
// öğrencileri yan yana oturmaz, engelli öğrenciler erişilebilir koltuklara oturur,
// her öğrenciye en fazla bir koltuk verilir. Saf Go, deterministiktir.
//
// KOMŞULUK TANIMI (DECISIONS ADR-008): iki koltuk, aynı sırada ve sütun numaraları
// 1 farklıysa komşudur (sol/sağ; zorunlu). Strict=true ise aynı sütunda sıra numarası
// 1 farklı olanlar da (ön/arka) komşudur. Çapraz koltuklar komşu DEĞİLDİR. Kullanılmayan
// (is_active=false) bir koltuk fiziksel olarak yerinde olduğundan komşuluğu keser
// değil, yalnızca kimse oturmaz: sütun 2 ve 4'teki öğrenciler arasındaki bozuk koltuk
// 3'ü yok sayıp komşu saymayız (aralarında 1 sütun boşluk var).
package seating

import "sort"

// Seat, bir salon koltuğu.
type Seat struct {
	ID         int64
	Row, Col   int
	Active     bool
	Accessible bool
}

// Room, koltuk düzeniyle salon.
type Room struct {
	ID         int64
	Rows, Cols int
	Seats      []Seat
}

// Student, yerleştirilecek öğrenci.
type Student struct {
	ID       int64
	Disabled bool
}

// Group, salondaki bir sınavın öğrencileri.
type Group struct {
	ExamID   int64
	Students []Student
}

// Assignment, bir öğrencinin koltuğu.
type Assignment struct {
	ExamID    int64
	StudentID int64
	Disabled  bool
	SeatID    int64
	Row, Col  int
}

// Unseated, yerleştirilemeyen öğrenci.
type Unseated struct {
	ExamID    int64
	StudentID int64
	Reason    string // no_accessible_seat | no_free_seat
}

// Plan, bir salonun oturma planı.
type Plan struct {
	RoomID      int64
	Assignments []Assignment
	Unseated    []Unseated
}

type cell [2]int

type layout struct {
	strict bool
	seats  map[cell]Seat
	order  []Seat // satır-sütun sırası, yalnız aktif koltuklar
}

func newLayout(r Room, strict bool) *layout {
	l := &layout{strict: strict, seats: make(map[cell]Seat, len(r.Seats))}
	for _, s := range r.Seats {
		l.seats[cell{s.Row, s.Col}] = s
		if s.Active {
			l.order = append(l.order, s)
		}
	}
	sort.Slice(l.order, func(a, b int) bool {
		if l.order[a].Row != l.order[b].Row {
			return l.order[a].Row < l.order[b].Row
		}
		return l.order[a].Col < l.order[b].Col
	})
	return l
}

func (l *layout) neighbors(c cell) []cell {
	n := []cell{{c[0], c[1] - 1}, {c[0], c[1] + 1}}
	if l.strict {
		n = append(n, cell{c[0] - 1, c[1]}, cell{c[0] + 1, c[1]})
	}
	return n
}

// Arrange, grupları salona yerleştirir.
//
//  1. Engelli öğrenciler önce, erişilebilir koltuklara (ön sıra, kapıya yakın):
//     sırayla ilk uygun erişilebilir koltuğa; aynı sınavdan komşu koltuk yoksa.
//  2. Kalanlar satır satır, soldan sağa: her koltuğa, komşusuyla aynı sınavdan OLMAYAN
//     ve en çok öğrencisi kalan grup. Birden çok sınav varsa sınavlar sütun sütun
//     dönüşümlü oturur (A B A B …); tek sınav varsa uygun grup bulunamadığı için her
//     ikinci koltuk boş kalır (koltuk atlama). İkisi aynı kuraldan çıkar.
//
// Yerleşemeyen öğrenciler Plan.Unseated'da nedeniyle listelenir; yanlış koltuğa
// konmaz.
func Arrange(room Room, groups []Group, strict bool) Plan {
	return ArrangeInto(room, nil, groups, strict)
}

// ArrangeInto, salonda zaten oturan öğrencileri (occupied) YERİNDEN OYNATMADAN yeni
// grupları boş koltuklara yerleştirir (toplu salon değişikliği / tahliye için).
// Dönen plan yalnızca yeni yerleşimleri içerir; komşuluk kuralı mevcut öğrencilere
// karşı da uygulanır.
func ArrangeInto(room Room, occupied []Assignment, groups []Group, strict bool) Plan {
	l := newLayout(room, strict)
	plan := Plan{RoomID: room.ID}
	occ := map[cell]int64{}
	taken := map[int64]bool{} // koltuk kimliği
	for _, a := range occupied {
		occ[cell{a.Row, a.Col}] = a.ExamID
		taken[a.SeatID] = true
	}

	type queue struct {
		exam  int64
		rest  []Student
		index int
	}
	gs := make([]*queue, 0, len(groups))
	var disabled []struct {
		exam int64
		st   Student
	}
	sorted := append([]Group(nil), groups...)
	sort.Slice(sorted, func(a, b int) bool { return sorted[a].ExamID < sorted[b].ExamID })
	for _, g := range sorted {
		q := &queue{exam: g.ExamID}
		sts := append([]Student(nil), g.Students...)
		sort.Slice(sts, func(a, b int) bool { return sts[a].ID < sts[b].ID })
		for _, s := range sts {
			if s.Disabled {
				disabled = append(disabled, struct {
					exam int64
					st   Student
				}{g.ExamID, s})
			} else {
				q.rest = append(q.rest, s)
			}
		}
		gs = append(gs, q)
	}

	conflict := func(c cell, exam int64) bool {
		for _, n := range l.neighbors(c) {
			if e, ok := occ[n]; ok && e == exam {
				return true
			}
		}
		return false
	}
	put := func(s Seat, exam int64, st Student) {
		occ[cell{s.Row, s.Col}] = exam
		taken[s.ID] = true
		plan.Assignments = append(plan.Assignments, Assignment{ExamID: exam, StudentID: st.ID,
			Disabled: st.Disabled, SeatID: s.ID, Row: s.Row, Col: s.Col})
	}

	// 1) Engelli öğrenciler.
	for _, d := range disabled {
		placed := false
		for _, s := range l.order {
			if !s.Accessible || taken[s.ID] || conflict(cell{s.Row, s.Col}, d.exam) {
				continue
			}
			put(s, d.exam, d.st)
			placed = true
			break
		}
		if !placed {
			plan.Unseated = append(plan.Unseated, Unseated{d.exam, d.st.ID, "no_accessible_seat"})
		}
	}

	// 2) Diğer öğrenciler.
	remaining := func(q *queue) int { return len(q.rest) - q.index }
	for _, s := range l.order {
		if taken[s.ID] {
			continue
		}
		c := cell{s.Row, s.Col}
		var pick *queue
		for _, q := range gs {
			if remaining(q) == 0 || conflict(c, q.exam) {
				continue
			}
			if pick == nil || remaining(q) > remaining(pick) {
				pick = q // eşitlikte küçük sınav kimliği (gs sıralı)
			}
		}
		if pick == nil {
			continue
		}
		put(s, pick.exam, pick.rest[pick.index])
		pick.index++
	}
	for _, q := range gs {
		for _, st := range q.rest[q.index:] {
			plan.Unseated = append(plan.Unseated, Unseated{q.exam, st.ID, "no_free_seat"})
		}
	}

	sort.Slice(plan.Assignments, func(a, b int) bool { return plan.Assignments[a].StudentID < plan.Assignments[b].StudentID })
	sort.Slice(plan.Unseated, func(a, b int) bool {
		if plan.Unseated[a].ExamID != plan.Unseated[b].ExamID {
			return plan.Unseated[a].ExamID < plan.Unseated[b].ExamID
		}
		return plan.Unseated[a].StudentID < plan.Unseated[b].StudentID
	})
	return plan
}

// Violations, bir planın ihlal sayıları.
type Violations struct {
	AdjacentSameExam      int // komşu koltuklarda aynı sınavın öğrencileri (çift sayısı)
	SeatDoubleBooked      int // aynı koltuğa birden çok öğrenci (fazla öğrenci sayısı)
	StudentSeatedTwice    int // aynı öğrenciye birden çok koltuk (fazla koltuk sayısı)
	InvalidSeat           int // salonda olmayan veya kullanılmayan koltuk, ya da satır/sütun uyuşmazlığı
	DisabledNotAccessible int // engelli öğrenci erişilebilir olmayan koltukta
}

// Total, tüm ihlallerin toplamı.
func (v Violations) Total() int {
	return v.AdjacentSameExam + v.SeatDoubleBooked + v.StudentSeatedTwice + v.InvalidSeat + v.DisabledNotAccessible
}

// CountViolations, planı Arrange'dan BAĞIMSIZ olarak salon geometrisine karşı sayar.
func CountViolations(room Room, plan Plan, strict bool) Violations {
	l := newLayout(room, strict)
	var v Violations
	bySeat := map[int64]int{}
	byStudent := map[int64]int{}
	occ := map[cell]int64{}
	for _, a := range plan.Assignments {
		bySeat[a.SeatID]++
		byStudent[a.StudentID]++
		s, ok := l.seats[cell{a.Row, a.Col}]
		if !ok || !s.Active || s.ID != a.SeatID {
			v.InvalidSeat++
			continue
		}
		if a.Disabled && !s.Accessible {
			v.DisabledNotAccessible++
		}
		occ[cell{a.Row, a.Col}] = a.ExamID
	}
	for _, n := range bySeat {
		v.SeatDoubleBooked += n - 1
	}
	for _, n := range byStudent {
		v.StudentSeatedTwice += n - 1
	}
	// Komşu çiftleri yalnızca bir kez say: yalnız sağ ve alt yönler.
	for c, e := range occ {
		if o, ok := occ[cell{c[0], c[1] + 1}]; ok && o == e {
			v.AdjacentSameExam++
		}
		if strict {
			if o, ok := occ[cell{c[0] + 1, c[1]}]; ok && o == e {
				v.AdjacentSameExam++
			}
		}
	}
	return v
}
