package seating

import (
	"reflect"
	"testing"
)

// grid kurar: rows x cols, tümü aktif; accessible verilen (satır,sütun) çiftleri erişilebilir.
func grid(rows, cols int, accessible ...[2]int) Room {
	acc := map[[2]int]bool{}
	for _, a := range accessible {
		acc[a] = true
	}
	r := Room{ID: 1, Rows: rows, Cols: cols}
	id := int64(0)
	for i := 0; i < rows; i++ {
		for j := 0; j < cols; j++ {
			id++
			r.Seats = append(r.Seats, Seat{ID: id, Row: i, Col: j, Active: true, Accessible: acc[[2]int{i, j}]})
		}
	}
	return r
}

func students(from, n int64, disabled ...int64) []Student {
	d := map[int64]bool{}
	for _, x := range disabled {
		d[x] = true
	}
	var out []Student
	for i := int64(0); i < n; i++ {
		out = append(out, Student{ID: from + i, Disabled: d[from+i]})
	}
	return out
}

func seated(p Plan) int { return len(p.Assignments) }

// Tek sınavlı salon: bir koltuk atlayarak oturur; 3x6 salonda satır başına 3 -> 9 koltuk.
func TestSingleExamRoomSeatsEveryOtherSeat(t *testing.T) {
	room := grid(3, 6)
	p := Arrange(room, []Group{{ExamID: 1, Students: students(100, 20)}}, false)
	if seated(p) != 9 || len(p.Unseated) != 11 {
		t.Fatalf("9 oturan, 11 yerleşemeyen bekleniyordu: %d / %d", seated(p), len(p.Unseated))
	}
	if v := CountViolations(room, p, false); v.Total() != 0 {
		t.Fatalf("ihlal: %+v", v)
	}
	for _, u := range p.Unseated {
		if u.Reason != "no_free_seat" {
			t.Fatalf("neden no_free_seat olmalı: %+v", u)
		}
	}
}

// Karışık sınavlı salon: sütun dönüşümlü, tüm koltuklar kullanılır.
func TestMixedRoomAlternatesAndUsesAllSeats(t *testing.T) {
	room := grid(3, 6)
	p := Arrange(room, []Group{
		{ExamID: 1, Students: students(100, 9)}, {ExamID: 2, Students: students(200, 9)}}, false)
	if seated(p) != 18 || len(p.Unseated) != 0 {
		t.Fatalf("18 koltuk dolu olmalı: %d oturan, %d yerleşemeyen", seated(p), len(p.Unseated))
	}
	if v := CountViolations(room, p, false); v.Total() != 0 {
		t.Fatalf("ihlal: %+v", v)
	}
	// Her satırda sınavlar dönüşümlü.
	byCell := map[[2]int]int64{}
	for _, a := range p.Assignments {
		byCell[[2]int{a.Row, a.Col}] = a.ExamID
	}
	for r := 0; r < 3; r++ {
		for c := 0; c+1 < 6; c++ {
			if byCell[[2]int{r, c}] == byCell[[2]int{r, c + 1}] {
				t.Fatalf("satır %d sütun %d-%d aynı sınav", r, c, c+1)
			}
		}
	}
}

func TestDisabledStudentsGetAccessibleSeatsFirst(t *testing.T) {
	room := grid(3, 6, [2]int{0, 0}, [2]int{0, 2}, [2]int{0, 4})
	p := Arrange(room, []Group{
		{ExamID: 1, Students: students(100, 8, 103, 107)}, {ExamID: 2, Students: students(200, 8, 205)}}, false)
	for _, a := range p.Assignments {
		if a.Disabled && !(a.Row == 0 && a.Col%2 == 0) {
			t.Fatalf("engelli öğrenci %d erişilebilir olmayan koltukta (%d,%d)", a.StudentID, a.Row, a.Col)
		}
	}
	nd := 0
	for _, a := range p.Assignments {
		if a.Disabled {
			nd++
		}
	}
	if nd != 3 || len(p.Unseated) != 0 {
		t.Fatalf("3 engelli öğrenci oturmalı, %d oturdu; yerleşemeyen %v", nd, p.Unseated)
	}
	if v := CountViolations(room, p, false); v.Total() != 0 {
		t.Fatalf("ihlal: %+v", v)
	}
}

// Aynı sınavın iki engelli öğrencisi komşu erişilebilir koltuklara oturtulmaz.
func TestTwoDisabledOfSameExamNeverAdjacent(t *testing.T) {
	adjacent := grid(2, 6, [2]int{0, 0}, [2]int{0, 1}) // bilerek komşu erişilebilir koltuklar
	p := Arrange(adjacent, []Group{{ExamID: 1, Students: students(100, 2, 100, 101)}}, false)
	if len(p.Unseated) != 1 || p.Unseated[0].Reason != "no_accessible_seat" {
		t.Fatalf("ikinci engelli öğrenci yerleşememeli (komşu): %+v", p)
	}
	if CountViolations(adjacent, p, false).Total() != 0 {
		t.Fatal("plan ihlalsiz olmalı")
	}
	// Farklı sınavların engelli öğrencileri komşu erişilebilir koltuklara oturabilir.
	p2 := Arrange(adjacent, []Group{{ExamID: 1, Students: students(100, 1, 100)}, {ExamID: 2, Students: students(200, 1, 200)}}, false)
	if len(p2.Unseated) != 0 {
		t.Fatalf("farklı sınavlar komşu olabilir: %+v", p2.Unseated)
	}
}

func TestDisabledWithoutAnyAccessibleSeatIsReportedNotMisplaced(t *testing.T) {
	room := grid(2, 4)
	p := Arrange(room, []Group{{ExamID: 1, Students: students(100, 3, 101)}}, false)
	if len(p.Unseated) != 1 || p.Unseated[0].Reason != "no_accessible_seat" || p.Unseated[0].StudentID != 101 {
		t.Fatalf("beklenen: 101 no_accessible_seat, bulunan %+v", p.Unseated)
	}
	for _, a := range p.Assignments {
		if a.StudentID == 101 {
			t.Fatal("engelli öğrenci erişilemez koltuğa konmamalı")
		}
	}
}

// Sıkı mod: ön/arka da komşu; tek sınavlı salonda dama tahtası deseni.
func TestStrictModeAlsoSeparatesFrontAndBack(t *testing.T) {
	room := grid(4, 4)
	g := []Group{{ExamID: 1, Students: students(100, 16)}}
	loose := Arrange(room, g, false)
	strict := Arrange(room, g, true)
	if seated(loose) != 8 || seated(strict) != 8 {
		t.Fatalf("her ikisi de 8 olmalı: gevşek %d sıkı %d", seated(loose), seated(strict))
	}
	if CountViolations(room, strict, true).Total() != 0 {
		t.Fatal("sıkı modda ihlal olmamalı")
	}
	// Gevşek planın sütunları hizalıdır: sıkı kuralla bakılınca ön/arka ihlali görünür.
	if CountViolations(room, loose, true).AdjacentSameExam == 0 {
		t.Fatal("gevşek plan sıkı kuralla ön/arka ihlali içermeli")
	}
}

func TestInactiveSeatsAreNeverUsed(t *testing.T) {
	room := grid(1, 5)
	room.Seats[1].Active, room.Seats[3].Active = false, false
	p := Arrange(room, []Group{{ExamID: 1, Students: students(100, 3)}}, false)
	// Aktif sütunlar 0, 2, 4 birbirine komşu değil -> üçü de oturur.
	if seated(p) != 3 {
		t.Fatalf("3 öğrenci oturmalı: %d", seated(p))
	}
	for _, a := range p.Assignments {
		if a.Col == 1 || a.Col == 3 {
			t.Fatalf("kullanılmayan koltuğa yerleştirildi: %+v", a)
		}
	}
}

func TestDeterministicRegardlessOfInputOrder(t *testing.T) {
	room := grid(5, 8, [2]int{0, 0}, [2]int{0, 2})
	g1 := Group{ExamID: 1, Students: students(100, 14, 104)}
	g2 := Group{ExamID: 2, Students: students(200, 12, 203)}
	g3 := Group{ExamID: 3, Students: students(300, 6)}
	a := Arrange(room, []Group{g1, g2, g3}, false)
	b := Arrange(room, []Group{g3, g1, g2}, false)
	// Öğrenci sırasını da karıştır.
	g1r := g1
	g1r.Students = append([]Student(nil), g1.Students...)
	for i, j := 0, len(g1r.Students)-1; i < j; i, j = i+1, j-1 {
		g1r.Students[i], g1r.Students[j] = g1r.Students[j], g1r.Students[i]
	}
	c := Arrange(room, []Group{g2, g3, g1r}, false)
	if !reflect.DeepEqual(a, b) || !reflect.DeepEqual(a, c) {
		t.Fatal("girdi sırası sonucu değiştirmemeli")
	}
}

// --- CountViolations: bozuk planlarda DOĞRU SAYI -------------------------------

func TestCountViolationsOnBrokenPlans(t *testing.T) {
	room := grid(2, 4, [2]int{0, 0})
	at := func(exam, student int64, disabled bool, row, col int) Assignment {
		return Assignment{ExamID: exam, StudentID: student, Disabled: disabled,
			SeatID: int64(row*4 + col + 1), Row: row, Col: col}
	}
	cases := []struct {
		name   string
		plan   Plan
		strict bool
		want   Violations
	}{
		{"temiz", Plan{Assignments: []Assignment{at(1, 1, false, 0, 0), at(2, 2, false, 0, 1), at(1, 3, false, 0, 2)}}, false, Violations{}},
		{"bir komşu çift", Plan{Assignments: []Assignment{at(1, 1, false, 0, 0), at(1, 2, false, 0, 1)}}, false, Violations{AdjacentSameExam: 1}},
		{"üçlü zincir = 2 çift", Plan{Assignments: []Assignment{at(1, 1, false, 0, 0), at(1, 2, false, 0, 1), at(1, 3, false, 0, 2)}}, false, Violations{AdjacentSameExam: 2}},
		{"ön/arka yalnız sıkı modda", Plan{Assignments: []Assignment{at(1, 1, false, 0, 1), at(1, 2, false, 1, 1)}}, false, Violations{}},
		{"ön/arka sıkı modda", Plan{Assignments: []Assignment{at(1, 1, false, 0, 1), at(1, 2, false, 1, 1)}}, true, Violations{AdjacentSameExam: 1}},
		{"çapraz komşu değil", Plan{Assignments: []Assignment{at(1, 1, false, 0, 0), at(1, 2, false, 1, 1)}}, true, Violations{}},
		{"aynı koltuk iki öğrenci", Plan{Assignments: []Assignment{at(1, 1, false, 0, 1), at(2, 2, false, 0, 1)}}, false, Violations{SeatDoubleBooked: 1}},
		{"öğrenci iki koltukta", Plan{Assignments: []Assignment{at(1, 1, false, 0, 0), at(1, 1, false, 0, 2)}}, false, Violations{StudentSeatedTwice: 1}},
		{"olmayan koltuk", Plan{Assignments: []Assignment{{ExamID: 1, StudentID: 1, SeatID: 99, Row: 5, Col: 5}}}, false, Violations{InvalidSeat: 1}},
		{"kimlik-konum uyuşmazlığı", Plan{Assignments: []Assignment{{ExamID: 1, StudentID: 1, SeatID: 8, Row: 0, Col: 0}}}, false, Violations{InvalidSeat: 1}},
		{"engelli erişilemez koltukta", Plan{Assignments: []Assignment{at(1, 1, true, 1, 2)}}, false, Violations{DisabledNotAccessible: 1}},
		{"engelli erişilebilir koltukta", Plan{Assignments: []Assignment{at(1, 1, true, 0, 0)}}, false, Violations{}},
		{"karışık: 1 komşu + 1 engelli", Plan{Assignments: []Assignment{at(1, 1, true, 1, 0), at(1, 2, false, 1, 1)}}, false, Violations{AdjacentSameExam: 1, DisabledNotAccessible: 1}},
	}
	for _, c := range cases {
		if got := CountViolations(room, c.plan, c.strict); got != c.want {
			t.Errorf("%s: %+v, beklenen %+v", c.name, got, c.want)
		}
	}
	if (Violations{AdjacentSameExam: 2, InvalidSeat: 1}).Total() != 3 {
		t.Error("Total yanlış")
	}
}

func TestCountViolationsRejectsInactiveSeat(t *testing.T) {
	room := grid(1, 3)
	room.Seats[1].Active = false
	p := Plan{Assignments: []Assignment{{ExamID: 1, StudentID: 1, SeatID: 2, Row: 0, Col: 1}}}
	if v := CountViolations(room, p, false); v.InvalidSeat != 1 {
		t.Fatalf("kullanılmayan koltuk geçersiz sayılmalı: %+v", v)
	}
}

// Rastgele girdilerde Arrange'ın ürettiği plan her zaman 0 ihlal içerir ve hiçbir
// öğrenci iki kez veya kaybolarak (oturan + yerleşemeyen = girdi) sayılmaz.
func TestArrangeAlwaysViolationFreeAndComplete(t *testing.T) {
	for trial := 0; trial < 60; trial++ {
		rows, cols := 2+trial%7, 3+trial%9
		room := grid(rows, cols, [2]int{0, 0}, [2]int{0, 2})
		if trial%4 == 0 {
			room.Seats[trial%len(room.Seats)].Active = false
		}
		var groups []Group
		total := 0
		for g := 0; g < 1+trial%4; g++ {
			n := int64(3 + (trial*7+g*5)%17)
			total += int(n)
			groups = append(groups, Group{ExamID: int64(g + 1), Students: students(int64(1000*(g+1)), n, int64(1000*(g+1)), int64(1000*(g+1))+1)})
		}
		strict := trial%3 == 0
		p := Arrange(room, groups, strict)
		if v := CountViolations(room, p, strict); v.Total() != 0 {
			t.Fatalf("deneme %d: ihlal %+v", trial, v)
		}
		if seated(p)+len(p.Unseated) != total {
			t.Fatalf("deneme %d: %d oturan + %d yerleşemeyen != %d", trial, seated(p), len(p.Unseated), total)
		}
	}
}
