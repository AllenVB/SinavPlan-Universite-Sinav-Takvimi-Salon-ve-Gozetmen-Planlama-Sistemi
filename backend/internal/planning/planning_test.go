package planning

import (
	"reflect"
	"testing"

	"github.com/AllenVB/sinavplan/backend/internal/domain/rooms"
	"github.com/AllenVB/sinavplan/backend/internal/domain/scheduling"
	"github.com/AllenVB/sinavplan/backend/internal/domain/seating"
	"github.com/AllenVB/sinavplan/backend/internal/simdata"
)

func solve(t testing.TB, ds *simdata.Dataset, iters int) map[int64]int {
	t.Helper()
	in := SchedulingInput(ds, 42)
	in.MaxIterations = iters
	r, err := scheduling.Solve(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Unplaced) != 0 {
		t.Fatalf("T-1 yerleşemeyen sınav: %v", r.Unplaced)
	}
	return r.Assignment
}

// Tam zincir, tam ölçek: T-1 -> T-2 -> T-3. Her sınavın her öğrencisi tam bir koltuğa
// oturur, salon doğrulayıcısı ve koltuk ihlal sayacı sıfır döner.
func TestFullPipelineAtScaleHasZeroViolations(t *testing.T) {
	if testing.Short() {
		t.Skip("-short")
	}
	ds, _ := simdata.Generate(simdata.DefaultConfig(42))
	res, err := Plan(ds, solve(t, ds, 150_000), rooms.Config{})
	if err != nil {
		t.Fatal(err)
	}
	tot := res.Totals()
	t.Logf("%+v", tot)
	if tot.ExamsUnplaced != 0 || tot.RoomViolations != 0 || tot.SeatViolations != 0 || tot.Unseated != 0 {
		t.Fatalf("zincir temiz olmalı: %+v", tot)
	}
	if tot.Students != len(ds.Enrollments) {
		t.Fatalf("oturan %d != kayıt %d: her öğrenci-sınav çifti tam bir koltuğa düşmeli", tot.Students, len(ds.Enrollments))
	}
}

// Her (sınav, öğrenci) tam bir koltuk ve her (slot, salon, koltuk) en çok bir öğrenci.
func TestEveryStudentExamPairHasExactlyOneSeat(t *testing.T) {
	ds, _ := simdata.Generate(simdata.Config{Seed: 5, Exams: 72, Students: 900, Rooms: 24, Days: 6, SessionsPerDay: 3, DisabilityRate: 0.02})
	res, err := Plan(ds, solve(t, ds, 40_000), rooms.Config{})
	if err != nil {
		t.Fatal(err)
	}
	type pair struct{ exam, student int64 }
	seen := map[pair]int{}
	type seatKey struct {
		slot         int
		room, seatID int64
	}
	used := map[seatKey]bool{}
	for _, s := range res.Slots {
		for rid, sp := range s.Seating {
			for _, a := range sp.Assignments {
				seen[pair{a.ExamID, a.StudentID}]++
				k := seatKey{s.Slot, rid, a.SeatID}
				if used[k] {
					t.Fatalf("koltuk çift rezerve: %+v", k)
				}
				used[k] = true
			}
		}
	}
	if len(seen) != len(ds.Enrollments) {
		t.Fatalf("%d çift, %d kayıt bekleniyordu", len(seen), len(ds.Enrollments))
	}
	for p, n := range seen {
		if n != 1 {
			t.Fatalf("%+v %d koltukta", p, n)
		}
	}
}

// T-2'nin determinizm kanıtı tam zincirde: aynı çizelge -> aynı salon ve koltuk planı.
func TestPipelineIsDeterministic(t *testing.T) {
	ds, _ := simdata.Generate(simdata.Config{Seed: 9, Exams: 60, Students: 700, Rooms: 20, Days: 5, SessionsPerDay: 3, DisabilityRate: 0.02})
	asg := solve(t, ds, 30_000)
	a, _ := Plan(ds, asg, rooms.Config{})
	b, _ := Plan(ds, asg, rooms.Config{})
	if !reflect.DeepEqual(a, b) {
		t.Fatal("aynı çizelge farklı salon/koltuk planı üretti")
	}
}

// Engelli öğrencilerin hepsi erişilebilir koltukta oturur.
func TestDisabledStudentsSitOnAccessibleSeats(t *testing.T) {
	ds, _ := simdata.Generate(simdata.Config{Seed: 5, Exams: 72, Students: 900, Rooms: 24, Days: 6, SessionsPerDay: 3, DisabilityRate: 0.03})
	res, err := Plan(ds, solve(t, ds, 40_000), rooms.Config{})
	if err != nil {
		t.Fatal(err)
	}
	disabled := 0
	for _, s := range res.Slots {
		for _, sp := range s.Seating {
			for _, a := range sp.Assignments {
				if a.Disabled {
					disabled++
					if a.Row != 0 || a.Col%2 != 0 {
						t.Fatalf("engelli öğrenci %d erişilebilir olmayan koltukta: (%d,%d)", a.StudentID, a.Row, a.Col)
					}
				}
			}
		}
	}
	if disabled == 0 {
		t.Fatal("testte engelli öğrenci yok")
	}
}

// Neden gerekli: yukarıdaki T-2 kuralı gevşetilseydi T-3 gerçekten yerleştiremezdi.
// A=6 (tam Single) ve B=4 (1 engelli) aynı 2x5 salonda: seating bir öğrenciyi açıkta bırakır.
func TestSeatingCannotFitWhatTheRoomRuleForbids(t *testing.T) {
	var seats []seating.Seat
	id := int64(0)
	for r := 0; r < 2; r++ {
		for c := 0; c < 5; c++ {
			id++
			seats = append(seats, seating.Seat{ID: id, Row: r, Col: c, Active: true, Accessible: r == 0 && c%2 == 0})
		}
	}
	room := seating.Room{ID: 1, Rows: 2, Cols: 5, Seats: seats}
	mkStudents := func(from int64, n int, disabled ...int64) []seating.Student {
		var out []seating.Student
		for i := int64(0); i < int64(n); i++ {
			d := false
			for _, x := range disabled {
				d = d || x == from+i
			}
			out = append(out, seating.Student{ID: from + i, Disabled: d})
		}
		return out
	}
	p := seating.Arrange(room, []seating.Group{
		{ExamID: 1, Students: mkStudents(100, 6)},
		{ExamID: 2, Students: mkStudents(200, 4, 200)}}, false)
	if len(p.Unseated) == 0 {
		t.Fatal("bu kombinasyon fiziksel olarak sığmamalı; T-2 kuralı bu yüzden onu yasaklıyor")
	}
}
