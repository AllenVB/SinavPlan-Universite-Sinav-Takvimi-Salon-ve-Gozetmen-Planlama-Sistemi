// Package planning, saf domain paketlerini (scheduling, rooms, seating) simülatör
// verisine bağlar. Veritabanına dokunmaz; Faz 8'deki servis katmanı aynı dönüşümleri
// veritabanı satırlarından yapacaktır.
package planning

import (
	"fmt"
	"sort"

	"github.com/AllenVB/sinavplan/backend/internal/domain/invigilation"
	"github.com/AllenVB/sinavplan/backend/internal/domain/rooms"
	"github.com/AllenVB/sinavplan/backend/internal/domain/scheduling"
	"github.com/AllenVB/sinavplan/backend/internal/domain/seating"
	"github.com/AllenVB/sinavplan/backend/internal/simdata"
	"github.com/AllenVB/sinavplan/backend/internal/store"
)

// examStudents: sınav kimliği -> öğrenci kimlikleri.
func examStudents(ds *simdata.Dataset) map[int64][]int64 {
	secCourse := map[int64]int64{}
	for _, s := range ds.Sections {
		secCourse[s.ID] = s.CourseID
	}
	byCourse := map[int64][]int64{}
	for _, e := range ds.Enrollments {
		c := secCourse[e.SectionID]
		byCourse[c] = append(byCourse[c], e.StudentID)
	}
	out := map[int64][]int64{}
	for _, ex := range ds.Exams {
		out[ex.ID] = byCourse[ex.CourseID]
	}
	return out
}

// SchedulingInput, T-1 girdisini kurar. Slot kapasitesi, salonların toplam aktif
// koltuğunun %80'idir (lab için ayrıca): T-2'nin etkin kapasite kuralı karışık
// salonlarda tüm koltukları kullanabilir ama bir sınavın payı yarıyla sınırlıdır;
// %80 payı bu sınırlamalara ve bölünme kayıplarına bırakılır.
func SchedulingInput(ds *simdata.Dataset, seed int64) scheduling.Input {
	st := examStudents(ds)
	total, lab := 0, 0
	for _, r := range ds.Rooms {
		total += r.Capacity
		if r.Type == "lab" {
			lab += r.Capacity
		}
	}
	// Kapalı salonlar (ADR-022): kapasite, en çok salon kapatılan oturuma göre düşülür; çözücü hiçbir oturuma
	// o oturumun salonlarının taşıyamayacağı yükü koymaz. Kapalı salon yoksa değerler değişmez.
	blockedTotal, blockedLab := worstBlockedCapacity(ds)
	total -= blockedTotal
	lab -= blockedLab
	in := scheduling.Input{Days: ds.Config.Days, SessionsPerDay: ds.Config.SessionsPerDay, Seed: seed,
		SlotCapacity: total * 8 / 10, LabSlotCapacity: lab * 8 / 10}
	for _, ex := range ds.Exams {
		in.Exams = append(in.Exams, scheduling.Exam{ID: ex.ID, Students: st[ex.ID], Lab: ex.RequiresLab})
	}
	return in
}

// roomBlockViolations, planın kapalı bir salona pay verip vermediğini (T-2'den bağımsız) denetler.
func roomBlockViolations(blocked map[int64]bool, plan rooms.Plan) []rooms.Violation {
	var v []rooms.Violation
	for _, pl := range plan.Placements {
		if blocked[pl.RoomID] {
			v = append(v, rooms.Violation{Kind: "room_blocked", ExamID: pl.ExamID, RoomID: pl.RoomID, Detail: "salon bu oturumda kapalı"})
		}
	}
	return v
}

// blockedBySlot, kapalı salonları slot indeksine göre döner (indeks = ds.Slots sırası).
func blockedBySlot(ds *simdata.Dataset) map[int]map[int64]bool {
	if len(ds.RoomBlocks) == 0 {
		return nil
	}
	idx := map[int64]int{}
	for i, s := range ds.Slots {
		idx[s.ID] = i
	}
	out := map[int]map[int64]bool{}
	for _, b := range ds.RoomBlocks {
		i, ok := idx[b.SlotID]
		if !ok {
			continue
		}
		if out[i] == nil {
			out[i] = map[int64]bool{}
		}
		out[i][b.RoomID] = true
	}
	return out
}

// worstBlockedCapacity, tek bir oturumda kapalı olan salonların en büyük toplam (ve laboratuvar) kapasitesi.
func worstBlockedCapacity(ds *simdata.Dataset) (total, lab int) {
	capOf := map[int64]simdata.Room{}
	for _, r := range ds.Rooms {
		capOf[r.ID] = r
	}
	perSlot := map[int64][2]int{}
	for _, b := range ds.RoomBlocks {
		r, ok := capOf[b.RoomID]
		if !ok {
			continue
		}
		v := perSlot[b.SlotID]
		v[0] += r.Capacity
		if r.Type == "lab" {
			v[1] += r.Capacity
		}
		perSlot[b.SlotID] = v
	}
	for _, v := range perSlot {
		total, lab = max(total, v[0]), max(lab, v[1])
	}
	return total, lab
}

// Rooms, veri kümesinin salonlarını domain biçimine çevirir.
func Rooms(ds *simdata.Dataset) ([]rooms.Room, map[int64]seating.Room) {
	seatsOf := map[int64][]simdata.Seat{}
	for _, s := range ds.Seats {
		seatsOf[s.RoomID] = append(seatsOf[s.RoomID], s)
	}
	var rr []rooms.Room
	sr := map[int64]seating.Room{}
	for _, r := range ds.Rooms {
		rm := rooms.Room{ID: r.ID, Lab: r.Type == "lab", Rows: r.Rows, Cols: r.Cols}
		sm := seating.Room{ID: r.ID, Rows: r.Rows, Cols: r.Cols}
		for _, s := range seatsOf[r.ID] {
			rm.Seats = append(rm.Seats, rooms.Seat{Row: s.Row, Col: s.Col, Active: s.Active, Accessible: s.Accessible})
			sm.Seats = append(sm.Seats, seating.Seat{ID: s.ID, Row: s.Row, Col: s.Col, Active: s.Active, Accessible: s.Accessible})
		}
		rr = append(rr, rm)
		sr[r.ID] = sm
	}
	return rr, sr
}

// SlotResult, bir slotun salon + oturma planı.
type SlotResult struct {
	Slot          int
	Rooms         rooms.Plan
	RoomViolation []rooms.Violation
	Seating       map[int64]seating.Plan // salon kimliği -> plan
	SeatViolation int                    // CountViolations toplamı
	Unseated      int
}

// Result, tüm slotların sonucu.
type Result struct {
	Slots []SlotResult
}

// Totals, özet sayılar.
type Totals struct {
	Slots, ExamsPlaced, ExamsUnplaced, RoomsUsed int
	RoomViolations, SeatViolations, Unseated     int
	Students                                     int
}

// Totals, tüm slotları toplar.
func (r *Result) Totals() Totals {
	var t Totals
	for _, s := range r.Slots {
		t.Slots++
		t.RoomsUsed += s.Rooms.RoomsUsed
		t.ExamsUnplaced += len(s.Rooms.Unplaced)
		t.RoomViolations += len(s.RoomViolation)
		t.SeatViolations += s.SeatViolation
		t.Unseated += s.Unseated
		for _, p := range s.Seating {
			t.Students += len(p.Assignments)
		}
	}
	return t
}

// Plan, T-1 çizelgesindeki her slot için T-2 ve T-3'ü çalıştırır.
func Plan(ds *simdata.Dataset, assignment map[int64]int, cfg rooms.Config) (*Result, error) {
	st := examStudents(ds)
	disabled := map[int64]bool{}
	for _, s := range ds.Students {
		disabled[s.ID] = s.Disabled
	}
	examByID := map[int64]simdata.Exam{}
	for _, e := range ds.Exams {
		examByID[e.ID] = e
	}
	rr, sr := Rooms(ds)
	blocked := blockedBySlot(ds)

	bySlot := map[int][]int64{}
	for ex, slot := range assignment {
		bySlot[slot] = append(bySlot[slot], ex)
	}
	slots := make([]int, 0, len(bySlot))
	for s := range bySlot {
		slots = append(slots, s)
	}
	sort.Ints(slots)

	res := &Result{}
	for _, slot := range slots {
		exIDs := bySlot[slot]
		sort.Slice(exIDs, func(a, b int) bool { return exIDs[a] < exIDs[b] })

		var exams []rooms.Exam
		dis := map[int64][]int64{}  // sınav -> engelli öğrenciler (sıralı)
		rest := map[int64][]int64{} // sınav -> diğer öğrenciler (sıralı)
		for _, id := range exIDs {
			ids := append([]int64(nil), st[id]...)
			sort.Slice(ids, func(a, b int) bool { return ids[a] < ids[b] })
			for _, s := range ids {
				if disabled[s] {
					dis[id] = append(dis[id], s)
				} else {
					rest[id] = append(rest[id], s)
				}
			}
			exams = append(exams, rooms.Exam{ID: id, Students: len(ids), Disabled: len(dis[id]), Lab: examByID[id].RequiresLab})
		}

		// Bu oturumda kapalı salonlar T-2'ye hiç verilmez; yerleşim sonrası bağımsız denetim bunu yeniden doğrular.
		open := rr
		if len(blocked[slot]) > 0 {
			open = make([]rooms.Room, 0, len(rr))
			for _, r := range rr {
				if !blocked[slot][r.ID] {
					open = append(open, r)
				}
			}
		}
		plan, err := rooms.Assign(open, exams, cfg)
		if err != nil {
			return nil, fmt.Errorf("slot %d: %w", slot, err)
		}
		sres := SlotResult{Slot: slot, Rooms: plan, RoomViolation: rooms.Validate(open, exams, cfg, plan),
			Seating: map[int64]seating.Plan{}}
		sres.RoomViolation = append(sres.RoomViolation, roomBlockViolations(blocked[slot], plan)...)

		// Payları öğrencilere böl: önce engelli öğrenciler (payın engelli sayısı kadar),
		// kalanı diğer öğrenciler. Sıra (sınav, salon) kimliğine göre sabit.
		groups := map[int64][]seating.Group{}
		for _, pl := range plan.Placements {
			d := dis[pl.ExamID][:pl.Disabled]
			dis[pl.ExamID] = dis[pl.ExamID][pl.Disabled:]
			n := pl.Count - pl.Disabled
			o := rest[pl.ExamID][:n]
			rest[pl.ExamID] = rest[pl.ExamID][n:]
			g := seating.Group{ExamID: pl.ExamID}
			for _, s := range d {
				g.Students = append(g.Students, seating.Student{ID: s, Disabled: true})
			}
			for _, s := range o {
				g.Students = append(g.Students, seating.Student{ID: s})
			}
			groups[pl.RoomID] = append(groups[pl.RoomID], g)
		}
		roomIDs := make([]int64, 0, len(groups))
		for id := range groups {
			roomIDs = append(roomIDs, id)
		}
		sort.Slice(roomIDs, func(a, b int) bool { return roomIDs[a] < roomIDs[b] })
		for _, id := range roomIDs {
			sp := seating.Arrange(sr[id], groups[id], cfg.Strict)
			sres.Seating[id] = sp
			sres.SeatViolation += seating.CountViolations(sr[id], sp, cfg.Strict).Total()
			sres.Unseated += len(sp.Unseated)
		}
		res.Slots = append(res.Slots, sres)
	}
	return res, nil
}

// InvigilationInput, salon planından gözetmen görevlerini ve veri kümesinden
// gözetmenleri kurar. Görev kimliği: slot*100000 + salon kimliği (deterministik).
// excused: gözetmen kullanıcı kimliği -> mazeretli slotlar.
func InvigilationInput(ds *simdata.Dataset, res *Result, excused map[int64][]int) invigilation.Input {
	in := invigilation.Input{SessionsPerDay: ds.Config.SessionsPerDay}
	for _, s := range res.Slots {
		type acc struct {
			students int
			exams    []int64
		}
		byRoom := map[int64]*acc{}
		for _, p := range s.Rooms.Placements {
			a := byRoom[p.RoomID]
			if a == nil {
				a = &acc{}
				byRoom[p.RoomID] = a
			}
			a.students += p.Count
			a.exams = append(a.exams, p.ExamID)
		}
		ids := make([]int64, 0, len(byRoom))
		for id := range byRoom {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(a, b int) bool { return ids[a] < ids[b] })
		for _, id := range ids {
			a := byRoom[id]
			in.Duties = append(in.Duties, invigilation.Duty{ID: int64(s.Slot)*100000 + id, Slot: s.Slot,
				RoomID: id, Students: a.students, Exams: a.exams})
		}
	}

	// Kendi dersi: öğretim üyesinin şube verdiği derslerin sınavları.
	examOfCourse := map[int64]int64{}
	for _, e := range ds.Exams {
		examOfCourse[e.CourseID] = e.ID
	}
	lecturerUser := map[int64]int64{}
	for _, l := range ds.Lecturers {
		lecturerUser[l.ID] = l.UserID
	}
	own := map[int64]map[int64]bool{}
	for _, sec := range ds.Sections {
		u := lecturerUser[sec.LecturerID]
		if own[u] == nil {
			own[u] = map[int64]bool{}
		}
		own[u][examOfCourse[sec.CourseID]] = true
	}
	for _, u := range ds.Users {
		isInv := false
		for _, r := range u.Roles {
			isInv = isInv || r == "invigilator"
		}
		if !isInv {
			continue
		}
		iv := invigilation.Invigilator{ID: u.ID, Unavailable: excused[u.ID]}
		for e := range own[u.ID] {
			iv.OwnExams = append(iv.OwnExams, e)
		}
		sort.Slice(iv.OwnExams, func(a, b int) bool { return iv.OwnExams[a] < iv.OwnExams[b] })
		in.Invigilators = append(in.Invigilators, iv)
	}
	return in
}

// ToStorePlan, zincir çıktılarını kalıcılaştırılacak biçime çevirir. slotID, slot
// indeksinden veritabanı slot kimliğine eşler.
func ToStorePlan(assignment map[int64]int, res *Result, invIn invigilation.Input, inv invigilation.Plan,
	slotID func(int) int64) store.SchedulePlan {
	var p store.SchedulePlan
	exIDs := make([]int64, 0, len(assignment))
	for id := range assignment {
		exIDs = append(exIDs, id)
	}
	sort.Slice(exIDs, func(a, b int) bool { return exIDs[a] < exIDs[b] })
	for _, id := range exIDs {
		p.Exams = append(p.Exams, store.PlanExam{ExamID: id, SlotID: slotID(assignment[id])})
	}
	anchor := map[[2]int64]int64{} // (slot, salon) -> salondaki en küçük sınav kimliği
	for _, s := range res.Slots {
		sid := slotID(s.Slot)
		for _, pl := range s.Rooms.Placements {
			p.Rooms = append(p.Rooms, store.PlanRoom{ExamID: pl.ExamID, SlotID: sid, RoomID: pl.RoomID, SeatCount: pl.Count})
			k := [2]int64{int64(s.Slot), pl.RoomID}
			if a, ok := anchor[k]; !ok || pl.ExamID < a {
				anchor[k] = pl.ExamID
			}
		}
		rooms := make([]int64, 0, len(s.Seating))
		for id := range s.Seating {
			rooms = append(rooms, id)
		}
		sort.Slice(rooms, func(a, b int) bool { return rooms[a] < rooms[b] })
		for _, rid := range rooms {
			for _, a := range s.Seating[rid].Assignments {
				p.Seats = append(p.Seats, store.PlanSeat{ExamID: a.ExamID, SlotID: sid, StudentID: a.StudentID, RoomID: rid, SeatID: a.SeatID})
			}
		}
	}
	duty := map[int64]invigilation.Duty{}
	for _, d := range invIn.Duties {
		duty[d.ID] = d
	}
	for _, a := range inv.Assignments {
		d := duty[a.DutyID]
		p.Invigilators = append(p.Invigilators, store.PlanInvigilator{
			ExamID: anchor[[2]int64{int64(d.Slot), d.RoomID}], SlotID: slotID(d.Slot), RoomID: d.RoomID, UserID: a.InvigilatorID})
	}
	return p
}

// Report, bir çizelge üretiminin ölçüm özeti (schedules.metrics ve iş sonucu olarak saklanır).
type Report struct {
	Scheduling           scheduling.Metrics   `json:"scheduling"`
	Penalty              int64                `json:"penalty"`
	ConstructionPenalty  int64                `json:"construction_penalty"`
	Iterations           int                  `json:"iterations"`
	GraphEdges           int                  `json:"graph_edges"`
	SchedulingMs         int64                `json:"scheduling_ms"`
	RoomsUsed            int                  `json:"rooms_used"`
	RoomViolations       int                  `json:"room_violations"`
	SeatViolations       int                  `json:"seat_violations"`
	Unseated             int                  `json:"unseated"`
	Invigilation         invigilation.Metrics `json:"invigilation"`
	InvigilationUnfilled int                  `json:"invigilation_unfilled"`
	// Infeasibility doluysa çizelge üretilemedi: hangi sınavların neden yerleşemediği.
	Infeasibility *scheduling.InfeasibilityReport `json:"infeasibility,omitempty"`
	UnplacedRooms []rooms.Unplaced                `json:"unplaced_rooms,omitempty"`
	// Elle düzenlemeyle türetilen sürümlerde: kaynak sürüm ve yapılan taşıma.
	DerivedFrom int64     `json:"derived_from,omitempty"`
	Edit        *EditInfo `json:"edit,omitempty"`
}

// EditInfo, elle düzenlemenin (sürükle-bırak) kaydı.
type EditInfo struct {
	ExamID   int64 `json:"exam_id"`
	FromSlot int64 `json:"from_slot_id"`
	ToSlot   int64 `json:"to_slot_id"`
}

// InfeasibleError, çizelge üretilemediğinde (T-1 yerleşemeyen sınav, T-2 sığmayan sınav)
// açıklayıcı raporu taşır; kısmi çizelge ASLA kalıcılaştırılmaz.
type InfeasibleError struct {
	Message string
	Report  *Report
}

func (e *InfeasibleError) Error() string { return e.Message }

// BuildFrom, veri kümesi için T-1..T-4'ü çalıştırır. excused: gözetmen kullanıcı
// kimliği -> mazeretli slot indeksleri.
func BuildFrom(ds *simdata.Dataset, seed int64, iterations int, excused map[int64][]int) (store.SchedulePlan, *Report, error) {
	return BuildFromFrontLoad(ds, seed, iterations, excused, 0)
}

// BuildFromFrontLoad, BuildFrom'un büyük sınavı öne alma cezası (ağırlık > 0) ile çalışan hâlidir; 0 = kapalı (varsayılan).
func BuildFromFrontLoad(ds *simdata.Dataset, seed int64, iterations int, excused map[int64][]int, frontLoad int64) (store.SchedulePlan, *Report, error) {
	in := SchedulingInput(ds, seed)
	in.MaxIterations = iterations
	if frontLoad > 0 {
		in.Weights = scheduling.Weights{ConsecutiveDay: scheduling.DefaultWeights.ConsecutiveDay, SameDay: scheduling.DefaultWeights.SameDay, FrontLoad: frontLoad}
	}
	r, err := scheduling.Solve(in)
	if err != nil {
		return store.SchedulePlan{}, nil, err
	}
	rep := &Report{Penalty: r.Penalty, ConstructionPenalty: r.ConstructionPenalty, Iterations: r.Iterations,
		GraphEdges: r.Graph.Edges, SchedulingMs: r.Duration.Milliseconds(), Infeasibility: r.Infeasibility}
	if len(r.Unplaced) > 0 {
		return store.SchedulePlan{}, rep, &InfeasibleError{
			Message: fmt.Sprintf("%d sınav hiçbir slota yerleştirilemedi", len(r.Unplaced)), Report: rep}
	}
	return finishPlan(ds, seed, in, r.Assignment, rep, excused)
}

// finishPlan, sınav→slot atamasından T-2..T-4'ü kurar. BuildFrom (çözücü atamasıyla) ve
// BuildFromAssignment (elle düzenlenmiş atamayla) aynı yolu kullanır: tek doğruluk kaynağı.
func finishPlan(ds *simdata.Dataset, seed int64, in scheduling.Input, assignment map[int64]int, rep *Report, excused map[int64][]int) (store.SchedulePlan, *Report, error) {
	rep.Scheduling = scheduling.Validate(in, assignment).Metrics
	rep.Penalty = rep.Scheduling.Penalty

	res, err := Plan(ds, assignment, rooms.Config{})
	if err != nil {
		return store.SchedulePlan{}, rep, err
	}
	tot := res.Totals()
	rep.RoomsUsed, rep.RoomViolations, rep.SeatViolations, rep.Unseated = tot.RoomsUsed, tot.RoomViolations, tot.SeatViolations, tot.Unseated
	if tot.ExamsUnplaced > 0 || tot.RoomViolations > 0 || tot.SeatViolations > 0 || tot.Unseated > 0 {
		for _, s := range res.Slots {
			rep.UnplacedRooms = append(rep.UnplacedRooms, s.Rooms.Unplaced...)
		}
		return store.SchedulePlan{}, rep, &InfeasibleError{
			Message: fmt.Sprintf("salon/oturma planı kurulamadı: %d sınav salonlara sığmadı, %d öğrenci yerleşemedi",
				tot.ExamsUnplaced, tot.Unseated), Report: rep}
	}

	invIn := InvigilationInput(ds, res, excused)
	inv, err := invigilation.Assign(invIn)
	if err != nil {
		return store.SchedulePlan{}, rep, err
	}
	rep.Invigilation = invigilation.Fairness(invIn, inv)
	rep.InvigilationUnfilled = len(inv.Unfilled)

	sp := ToStorePlan(assignment, res, invIn, inv, func(i int) int64 { return ds.Slots[i].ID })
	sp.PeriodID, sp.Seed, sp.Metrics = ds.Period.ID, seed, rep
	return sp, rep, nil
}

// CheckAssignment, verilen sınav→slot atamasının sert kısıtlarını (çakışma, günlük sınır,
// slot kapasitesi, geçersiz slot) ve ceza metriklerini hesaplar. Saf hesap: yazma yok.
func CheckAssignment(ds *simdata.Dataset, seed int64, assignment map[int64]int) scheduling.Report {
	return scheduling.Validate(SchedulingInput(ds, seed), assignment)
}

// BuildFromAssignment, çözücüyü ATLAYIP verilen (elle düzenlenmiş) atamadan salon, oturma ve
// gözetmen planını yeniden kurar. Atama sert kısıtları bozuyorsa *ViolationError döner.
func BuildFromAssignment(ds *simdata.Dataset, seed int64, assignment map[int64]int, excused map[int64][]int) (store.SchedulePlan, *Report, error) {
	in := SchedulingInput(ds, seed)
	if vr := scheduling.Validate(in, assignment); len(vr.Violations) > 0 {
		return store.SchedulePlan{}, nil, &ViolationError{Violations: vr.Violations}
	}
	rep := &Report{}
	return finishPlan(ds, seed, in, assignment, rep, excused)
}

// ViolationError, elle atamanın ihlal ettiği sert kısıtları taşır.
type ViolationError struct{ Violations []scheduling.Violation }

func (e *ViolationError) Error() string {
	return fmt.Sprintf("atama %d sert kısıtı ihlal ediyor", len(e.Violations))
}

// BuildAll, test fikstürleri için BuildFrom'un mazeretsiz ve hata-raporsuz kısayoludur.
func BuildAll(ds *simdata.Dataset, seed int64, iterations int) (store.SchedulePlan, error) {
	sp, _, err := BuildFrom(ds, seed, iterations, nil)
	return sp, err
}

// SimResult, kaydedilmeyen bir "ya şöyle olsaydı" T-1 koşusunun özeti.
type SimResult struct {
	Penalty             int64                           `json:"penalty"`
	ConstructionPenalty int64                           `json:"construction_penalty"`
	Iterations          int                             `json:"iterations"`
	DurationMs          int64                           `json:"duration_ms"`
	Unplaced            int                             `json:"unplaced"`
	Violations          int                             `json:"violations"`
	Metrics             scheduling.Metrics              `json:"metrics"`
	Infeasibility       *scheduling.InfeasibilityReport `json:"infeasibility,omitempty"`
}

// Simulate, yalnızca T-1'i (takvim) çalıştırır ve hiçbir şey yazmaz: hızlı ön izleme.
// Salon/oturma/gözetmen kurulmaz; tam üretim için Runner kullanılır.
func Simulate(ds *simdata.Dataset, seed int64, iterations int) (*SimResult, error) {
	in := SchedulingInput(ds, seed)
	in.MaxIterations = iterations
	r, err := scheduling.Solve(in)
	if err != nil {
		return nil, err
	}
	out := &SimResult{Penalty: r.Penalty, ConstructionPenalty: r.ConstructionPenalty, Iterations: r.Iterations,
		DurationMs: r.Duration.Milliseconds(), Unplaced: len(r.Unplaced), Infeasibility: r.Infeasibility}
	if len(r.Unplaced) == 0 {
		v := scheduling.Validate(in, r.Assignment)
		out.Violations, out.Metrics = len(v.Violations), v.Metrics
	}
	return out, nil
}

// SlotIndex, slot kimliği -> veri kümesindeki slot indeksi.
func SlotIndex(ds *simdata.Dataset) map[int64]int {
	m := make(map[int64]int, len(ds.Slots))
	for i, s := range ds.Slots {
		m[s.ID] = i
	}
	return m
}

// Scope, "sınav provası" kapsamıdır: fakülte → bölümler → sınıflar. Boş alan = süzme yok.
type Scope struct {
	FacultyID     int64   `json:"faculty_id"`
	DepartmentIDs []int64 `json:"department_ids"`
	Years         []int   `json:"years"`
}

// ScopeInfo, uygulanan kapsamın özeti (yanıtta gösterilir).
type ScopeInfo struct {
	Faculty     string   `json:"faculty,omitempty"`
	Departments []string `json:"departments"`
	Years       []int    `json:"years"`
	Exams       int      `json:"exams"`
	Students    int      `json:"students"`
}

// ScopeError, geçersiz kapsam (bilinmeyen fakülte/bölüm, bölüm başka fakültede, yıl aralık dışı, boş sonuç).
type ScopeError struct{ Field, Msg string }

func (e *ScopeError) Error() string { return e.Msg }

// ApplyScope, kapsama uyan sınavlardan oluşan bir veri kümesi kopyası döner (Exams süzülür; diğer
// tablolar paylaşılır, salt okunur kullanılır). Kapsam boşsa veri kümesi olduğu gibi döner.
func ApplyScope(ds *simdata.Dataset, sc Scope) (*simdata.Dataset, ScopeInfo, error) {
	info := ScopeInfo{Years: []int{}, Departments: []string{}}
	depts := map[int64]simdata.Department{}
	for _, d := range ds.Departments {
		depts[d.ID] = d
	}
	if sc.FacultyID != 0 {
		found := false
		for _, f := range ds.Faculties {
			if f.ID == sc.FacultyID {
				found, info.Faculty = true, f.Name
			}
		}
		if !found {
			return nil, info, &ScopeError{"faculty_id", "fakülte bulunamadı"}
		}
	}
	allowed := map[int64]bool{}
	for _, id := range sc.DepartmentIDs {
		d, ok := depts[id]
		switch {
		case !ok:
			return nil, info, &ScopeError{"department_ids", fmt.Sprintf("bölüm %d bulunamadı", id)}
		case sc.FacultyID != 0 && d.FacultyID != sc.FacultyID:
			return nil, info, &ScopeError{"department_ids", fmt.Sprintf("%s bölümü seçilen fakültede değil", d.Name)}
		}
		allowed[id] = true
	}
	years := map[int]bool{}
	for _, y := range sc.Years {
		if y < 1 || y > 4 {
			return nil, info, &ScopeError{"years", "sınıf 1 ile 4 arasında olmalı"}
		}
		years[y] = true
	}
	course := map[int64]simdata.Course{}
	for _, c := range ds.Courses {
		course[c.ID] = c
	}
	cp := *ds
	cp.Exams = nil
	for _, e := range ds.Exams {
		c := course[e.CourseID]
		d := depts[c.DepartmentID]
		if sc.FacultyID != 0 && d.FacultyID != sc.FacultyID {
			continue
		}
		if len(allowed) > 0 && !allowed[c.DepartmentID] {
			continue
		}
		if len(years) > 0 && !years[c.Year] {
			continue
		}
		cp.Exams = append(cp.Exams, e)
	}
	if len(cp.Exams) == 0 {
		return nil, info, &ScopeError{"scope", "seçilen kapsamda sınav yok"}
	}
	info.Exams = len(cp.Exams)
	inScope := map[int64]bool{}
	for _, e := range cp.Exams {
		inScope[e.CourseID] = true
	}
	secCourse := map[int64]int64{}
	for _, s := range ds.Sections {
		secCourse[s.ID] = s.CourseID
	}
	stu := map[int64]bool{}
	for _, en := range ds.Enrollments {
		if inScope[secCourse[en.SectionID]] {
			stu[en.StudentID] = true
		}
	}
	info.Students = len(stu)
	seenD := map[int64]bool{}
	for _, e := range cp.Exams {
		if d := depts[course[e.CourseID].DepartmentID]; !seenD[d.ID] {
			seenD[d.ID] = true
			info.Departments = append(info.Departments, d.Name)
		}
	}
	for y := 1; y <= 4; y++ {
		if len(years) == 0 || years[y] {
			info.Years = append(info.Years, y)
		}
	}
	return &cp, info, nil
}
