// Package invigilation, gözetmen atamasını (T-4) yapar. Saf Go, deterministiktir.
//
// Görev (Duty) bir (slot, salon) çiftidir; salonda 50'den fazla öğrenci varsa en az
// 2 gözetmen gerekir. Kısıtlar:
//   - kendi dersinin sınavında görev yok (salonda gözetmenin verdiği bir dersin sınavı varsa);
//   - aynı slotta iki görev yok;
//   - aynı gün ardışık oturumlarda görev yok;
//   - gözetmenin mazeretli olduğu slotlarda görev yok.
//
// Amaç: max(görev) - min(görev) en aza (adalet). DECISIONS ADR-009.
package invigilation

import (
	"fmt"
	"sort"
)

// LargeRoomThreshold: bu sayıyı AŞAN öğrencisi olan salona en az 2 gözetmen.
const LargeRoomThreshold = 50

// Duty, bir salonun bir slottaki gözetim görevi.
type Duty struct {
	ID       int64
	Slot     int
	RoomID   int64
	Students int
	Exams    []int64 // salondaki sınavlar (kendi dersi kısıtı için)
}

// Required, görevin gerektirdiği gözetmen sayısı.
func (d Duty) Required() int {
	if d.Students > LargeRoomThreshold {
		return 2
	}
	return 1
}

// Invigilator, gözetmen.
type Invigilator struct {
	ID          int64
	OwnExams    []int64 // verdiği derslerin sınavları
	Unavailable []int   // mazeretli slotlar
}

// Input, problem girdisi.
type Input struct {
	SessionsPerDay int
	Duties         []Duty
	Invigilators   []Invigilator
}

// Assignment, bir gözetmenin bir göreve atanması.
type Assignment struct {
	DutyID        int64 `json:"duty_id"`
	InvigilatorID int64 `json:"invigilator_id"`
}

// Unfilled, doldurulamayan görev kontenjanı.
type Unfilled struct {
	DutyID  int64 `json:"duty_id"`
	Missing int   `json:"missing"`
}

// Plan, atama sonucu. Assignments (görev, gözetmen) sıralıdır.
type Plan struct {
	Assignments []Assignment `json:"assignments"`
	Unfilled    []Unfilled   `json:"unfilled"`
}

// --- iç durum ---------------------------------------------------------------------

type state struct {
	in     Input
	duties []Duty
	dIdx   map[int64]int
	invs   []Invigilator
	iIdx   map[int64]int

	own   []map[int64]bool // gözetmen -> kendi sınavları
	unav  []map[int]bool   // gözetmen -> mazeretli slotlar
	slots []map[int]int    // gözetmen -> slot -> görev indeksi
	load  []int
	staff [][]int // görev -> gözetmen indeksleri
}

func newState(in Input) (*state, error) {
	if in.SessionsPerDay <= 0 {
		return nil, fmt.Errorf("SessionsPerDay pozitif olmalı")
	}
	st := &state{in: in, dIdx: map[int64]int{}, iIdx: map[int64]int{}}
	st.duties = append([]Duty(nil), in.Duties...)
	sort.Slice(st.duties, func(a, b int) bool { return st.duties[a].ID < st.duties[b].ID })
	for i, d := range st.duties {
		if _, dup := st.dIdx[d.ID]; dup {
			return nil, fmt.Errorf("görev %d iki kez verilmiş", d.ID)
		}
		st.dIdx[d.ID] = i
	}
	st.invs = append([]Invigilator(nil), in.Invigilators...)
	sort.Slice(st.invs, func(a, b int) bool { return st.invs[a].ID < st.invs[b].ID })
	for i, v := range st.invs {
		if _, dup := st.iIdx[v.ID]; dup {
			return nil, fmt.Errorf("gözetmen %d iki kez verilmiş", v.ID)
		}
		st.iIdx[v.ID] = i
		own := map[int64]bool{}
		for _, e := range v.OwnExams {
			own[e] = true
		}
		un := map[int]bool{}
		for _, s := range v.Unavailable {
			un[s] = true
		}
		st.own, st.unav = append(st.own, own), append(st.unav, un)
		st.slots = append(st.slots, map[int]int{})
	}
	st.load = make([]int, len(st.invs))
	st.staff = make([][]int, len(st.duties))
	return st, nil
}

// eligibleStatic: zamandan bağımsız kısıtlar (kendi dersi, mazeret).
func (st *state) eligibleStatic(v, d int) bool {
	du := st.duties[d]
	if st.unav[v][du.Slot] {
		return false
	}
	for _, e := range du.Exams {
		if st.own[v][e] {
			return false
		}
	}
	return true
}

// canTake: v, d görevini alabilir mi? ignore >= 0 ise v'nin o görevi bırakacağı
// varsayılır (takas değerlendirmesi için).
func (st *state) canTake(v, d, ignore int) bool {
	if !st.eligibleStatic(v, d) {
		return false
	}
	slot := st.duties[d].Slot
	busy := func(s int) bool {
		x, ok := st.slots[v][s]
		return ok && x != ignore
	}
	if busy(slot) {
		return false // aynı slot (bu görev dahil)
	}
	spd := st.in.SessionsPerDay
	if slot%spd > 0 && busy(slot-1) {
		return false // aynı gün önceki oturum
	}
	if slot%spd < spd-1 && busy(slot+1) {
		return false // aynı gün sonraki oturum
	}
	return true
}

func (st *state) assign(v, d int) {
	st.slots[v][st.duties[d].Slot] = d
	st.load[v]++
	st.staff[d] = append(st.staff[d], v)
}

func (st *state) unassign(v, d int) {
	delete(st.slots[v], st.duties[d].Slot)
	st.load[v]--
	s := st.staff[d]
	for i, x := range s {
		if x == v {
			st.staff[d] = append(s[:i:i], s[i+1:]...)
			break
		}
	}
}

func (st *state) plan() Plan {
	var p Plan
	for d, s := range st.staff {
		for _, v := range s {
			p.Assignments = append(p.Assignments, Assignment{st.duties[d].ID, st.invs[v].ID})
		}
		if miss := st.duties[d].Required() - len(s); miss > 0 {
			p.Unfilled = append(p.Unfilled, Unfilled{st.duties[d].ID, miss})
		}
	}
	sort.Slice(p.Assignments, func(a, b int) bool {
		if p.Assignments[a].DutyID != p.Assignments[b].DutyID {
			return p.Assignments[a].DutyID < p.Assignments[b].DutyID
		}
		return p.Assignments[a].InvigilatorID < p.Assignments[b].InvigilatorID
	})
	return p
}

// load, bir planı duruma yükler (Reassign için).
func (st *state) loadPlan(p Plan) error {
	for _, a := range p.Assignments {
		d, ok := st.dIdx[a.DutyID]
		if !ok {
			return fmt.Errorf("planda bilinmeyen görev %d", a.DutyID)
		}
		v, ok := st.iIdx[a.InvigilatorID]
		if !ok {
			return fmt.Errorf("planda bilinmeyen gözetmen %d", a.InvigilatorID)
		}
		st.assign(v, d)
	}
	return nil
}

// --- Atama ----------------------------------------------------------------------

// Assign, açgözlü atama + takas tabanlı iyileştirme yapar.
//
//  1. Görevler en kısıtlıdan başlayarak (uygun gözetmen sayısı / gereken sayı en düşük,
//     eşitlikte büyük salon, sonra küçük kimlik) her kontenjan için en az yüklü uygun
//     gözetmene (eşitlikte küçük kimlik) verilir.
//  2. Onarım: boş kalan kontenjanlar artırma zinciriyle (bkz. fill) doldurulur.
//  3. Yerel iyileştirme: yükü L olan bir gözetmenden yükü ≤ L-2 olana doğrudan görev
//     devri; olmazsa ara bir gözetmen üzerinden iki adımlı takas zinciri. Her hamle
//     Σ yük² değerini kesin azalttığı için süreç sonludur.
func Assign(in Input) (Plan, error) {
	st, err := newState(in)
	if err != nil {
		return Plan{}, err
	}
	nInv := len(st.invs)
	elig := make([]int, len(st.duties))
	for d := range st.duties {
		for v := 0; v < nInv; v++ {
			if st.eligibleStatic(v, d) {
				elig[d]++
			}
		}
	}
	order := make([]int, len(st.duties))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		da, db := st.duties[order[a]], st.duties[order[b]]
		// uygun gözetmen / gereken oranı küçük olan önce (çapraz çarpım: tam sayı)
		ra, rb := elig[order[a]]*db.Required(), elig[order[b]]*da.Required()
		if ra != rb {
			return ra < rb
		}
		if da.Students != db.Students {
			return da.Students > db.Students
		}
		return da.ID < db.ID
	})

	for _, d := range order {
		for need := st.duties[d].Required(); need > 0; need-- {
			best := -1
			for v := 0; v < nInv; v++ {
				if st.canTake(v, d, -1) && (best < 0 || st.load[v] < st.load[best]) {
					best = v
				}
			}
			if best < 0 {
				break
			}
			st.assign(best, d)
		}
	}
	// Onarım: açgözlü aşamanın boş bıraktığı kontenjanlar artırma zinciriyle doldurulur.
	// Örnek (ölçüldü): bir günün talebi 45/50/31 iken akşam oturumu sabahkilerle
	// paylaşılmalıydı (aynı gün ama ardışık değil); en az yüklüyü seçen açgözlü kural
	// farklı kişiler seçip orta oturumda 3 kontenjanı boş bırakıyordu.
	none := make([]bool, len(st.invs))
	for _, d := range order {
		for len(st.staff[d]) < st.duties[d].Required() {
			if st.fill(d, none) == nil {
				break
			}
		}
	}
	improve(st)
	return st.plan(), nil
}

// improve, yük farkını azaltan devir ve takas zincirlerini uygular.
func improve(st *state) {
	n := len(st.invs)
	if n == 0 {
		return
	}
	byLoadDesc := func() []int {
		ix := make([]int, n)
		for i := range ix {
			ix[i] = i
		}
		sort.SliceStable(ix, func(a, b int) bool { return st.load[ix[a]] > st.load[ix[b]] })
		return ix
	}
	dutiesOf := func(v int) []int {
		ds := make([]int, 0, len(st.slots[v]))
		for _, d := range st.slots[v] {
			ds = append(ds, d)
		}
		sort.Ints(ds)
		return ds
	}
	for iter := 0; iter < 100_000; iter++ {
		moved := false
		ix := byLoadDesc()
		maxL, minL := st.load[ix[0]], st.load[ix[n-1]]
	outer:
		for _, a := range ix { // yükü yüksek olandan
			for bi := n - 1; bi >= 0; bi-- { // yükü düşük olana
				b := ix[bi]
				// Farkı yalnızca en yüklüden giden veya en az yüklüye gelen hamle düşürür;
				// diğer çiftleri taramak (101³ üçlü) gereksiz pahalı olurdu.
				if st.load[a]-st.load[b] < 2 || (st.load[a] != maxL && st.load[b] != minL) {
					continue
				}
				// Doğrudan devir a -> b
				for _, d := range dutiesOf(a) {
					if st.canTake(b, d, -1) {
						st.unassign(a, d)
						st.assign(b, d)
						moved = true
						break outer
					}
				}
				// Takas zinciri: a -> m (d1), m -> b (d2); m'nin yükü değişmez.
				for _, m := range ix {
					if m == a || m == b {
						continue
					}
					for _, d1 := range dutiesOf(a) {
						for _, d2 := range dutiesOf(m) {
							if d1 == d2 || !st.canTake(b, d2, -1) {
								continue
							}
							if !st.canTake(m, d1, d2) {
								continue
							}
							st.unassign(m, d2)
							st.assign(b, d2)
							st.unassign(a, d1)
							st.assign(m, d1)
							moved = true
							break outer
						}
					}
				}
			}
		}
		if !moved {
			return
		}
	}
}
