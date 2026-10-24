package invigilation

import (
	"fmt"
	"sort"
)

// Change, bir atamanın gözetmen değişikliği.
type Change struct {
	DutyID int64 `json:"duty_id"`
	From   int64 `json:"from"` // önceki gözetmen
	To     int64 `json:"to"`   // yeni gözetmen
}

// ReassignResult, yeniden atama sonucu.
type ReassignResult struct {
	Plan    Plan     `json:"plan"`
	Changes []Change `json:"changes"`
	// Vacated: çıkarılan gözetmenden boşalan görev sayısı. Her boşalan görev en az bir
	// değişiklik gerektirir; bu yüzden len(Changes) >= Vacated bir alt sınırdır ve
	// len(Changes) == Vacated ise sonuç kanıtlanabilir biçimde minimumdur.
	Vacated int `json:"vacated"`
	// Swaps: boşalan görevleri doldurmak için yerinden oynatılan ek atama sayısı.
	Swaps int `json:"swaps"`
}

// MaxChain, bir boşalan görevi doldurmak için izin verilen en uzun takas zinciri
// (1 = doğrudan doldurma, 2 = bir kişinin görevini başkasına aktarıp onu kullanma, ...).
const MaxChain = 3

// Reassign, removed gözetmeni plandan çıkarır. Diğer TÜM atamalar kilitlidir;
// yalnızca boşalan görevler doldurulur. Her boşalan görev için önce doğrudan
// doldurma denenir; olmazsa artan uzunlukta takas zincirleri (iteratif derinleştirme),
// böylece görev başına değişiklik sayısı en azdır. Çıkarılan gözetmen hiçbir göreve
// yeniden atanmaz.
func Reassign(in Input, plan Plan, removed int64) (ReassignResult, error) {
	st, err := newState(in)
	if err != nil {
		return ReassignResult{}, err
	}
	if err := st.loadPlan(plan); err != nil {
		return ReassignResult{}, err
	}
	rv, ok := st.iIdx[removed]
	if !ok {
		return ReassignResult{}, fmt.Errorf("gözetmen %d girdide yok", removed)
	}

	var vacated []int
	for _, d := range st.slots[rv] {
		vacated = append(vacated, d)
	}
	sort.Ints(vacated)
	for _, d := range vacated {
		st.unassign(rv, d)
	}
	res := ReassignResult{Vacated: len(vacated)}

	used := make([]bool, len(st.invs))
	used[rv] = true

	for _, d := range vacated {
		chain := st.fill(d, used)
		if chain == nil {
			continue // Plan.Unfilled'da görünür
		}
		from := removed
		for _, m := range chain {
			res.Changes = append(res.Changes, Change{st.duties[m.take].ID, from, st.invs[m.v].ID})
			from = st.invs[m.v].ID
		}
		res.Swaps += len(chain) - 1
	}
	res.Plan = st.plan()
	return res, nil
}

type move struct{ v, take, release int } // v, take görevini alır; release'i bırakır (-1: yok)

// fill, duty görevindeki bir boş kontenjanı en kısa zincirle doldurur ve uygular.
// Önce doğrudan doldurma, olmazsa artan uzunlukta takas zinciri (iteratif
// derinleştirme, en fazla MaxChain hamle): v, elindeki x görevini bırakırsa duty'yi
// alabiliyorsa x'i başka biri (özyinelemeli olarak) üstlenir. used[v] olan gözetmenler
// zincire giremez. Adaylar en az yüklüden (eşitlikte küçük kimlik) denenir.
func (st *state) fill(duty int, used []bool) []move {
	var search func(duty, depth int) []move
	search = func(duty, depth int) []move {
		cands := make([]int, 0, len(st.invs))
		for v := range st.invs {
			if !used[v] {
				cands = append(cands, v)
			}
		}
		sort.SliceStable(cands, func(a, b int) bool { return st.load[cands[a]] < st.load[cands[b]] })
		for _, v := range cands {
			if st.canTake(v, duty, -1) {
				return []move{{v, duty, -1}}
			}
		}
		if depth == 1 {
			return nil
		}
		for _, v := range cands {
			held := make([]int, 0, len(st.slots[v]))
			for _, x := range st.slots[v] {
				held = append(held, x)
			}
			sort.Ints(held)
			for _, x := range held {
				if !st.canTake(v, duty, x) {
					continue
				}
				used[v] = true
				rest := search(x, depth-1)
				used[v] = false
				if rest != nil {
					return append([]move{{v, duty, x}}, rest...)
				}
			}
		}
		return nil
	}
	for depth := 1; depth <= MaxChain; depth++ {
		if chain := search(duty, depth); chain != nil {
			for _, m := range chain {
				if m.release >= 0 {
					st.unassign(m.v, m.release)
				}
			}
			for _, m := range chain {
				st.assign(m.v, m.take)
			}
			return chain
		}
	}
	return nil
}
