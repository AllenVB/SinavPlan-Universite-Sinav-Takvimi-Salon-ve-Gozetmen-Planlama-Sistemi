package scheduling

import (
	"fmt"
	"sort"
)

// SlotOption, bir sınavın tek bir slottaki durumudur: "bu sınav neden burada?" sorusunun cevabı, tüm
// alternatif slotların neden olmadığıyla birlikte verilir (Faz 22 B3).
type SlotOption struct {
	Slot         int        `json:"slot"`
	Current      bool       `json:"current"`
	Conflicts    []Conflict `json:"conflicts"`     // bu slotta ortak öğrencisi olan sınavlar (sert kısıt)
	DailyLimit   int        `json:"daily_limit"`   // bu slota gelirse günde 3. sınava girecek öğrenci sayısı
	OverCapacity bool       `json:"over_capacity"` // slotun koltuk kapasitesi yetmez
	Feasible     bool       `json:"feasible"`      // sert kısıtlar sağlanıyor
	Delta        int64      `json:"delta"`         // uygunsa toplam cezadaki değişim (taşınırsa)
}

// Conflict, ortak öğrencili bir sınav ve ortak öğrenci sayısı.
type Conflict struct {
	ExamID int64 `json:"exam_id"`
	Shared int   `json:"shared"`
}

// ExplainSlots, verilen geçerli atamada examID'nin her slot için durumunu döner. Çatışmalar çatışma
// grafından (kenar ağırlığı = ortak öğrenci), günlük sınır öğrenci gün sayaçlarından, ceza farkı yerel
// aramanın artımlı değerlendiricisinden gelir — çözücünün kullandığı yapıların aynısı.
func ExplainSlots(in Input, assignment map[int64]int, examID int64) ([]SlotOption, error) {
	p, err := newProblem(in)
	if err != nil {
		return nil, err
	}
	e := -1
	st := newState(p)
	for i, id := range p.examIDs {
		s, ok := assignment[id]
		if !ok || s < 0 || s >= p.slots {
			return nil, fmt.Errorf("sınav %d atamada yok ya da slotu geçersiz", id)
		}
		ch := []change{{i, -1, s}}
		d, _, _ := st.evaluate(ch)
		st.commit(ch, d)
		if id == examID {
			e = i
		}
	}
	if e < 0 {
		return nil, fmt.Errorf("sınav %d bu girdide yok", examID)
	}
	a := st.slot[e]
	out := make([]SlotOption, p.slots)
	for b := 0; b < p.slots; b++ {
		o := SlotOption{Slot: b, Current: b == a, Conflicts: []Conflict{}}
		for k, j := range p.adj[e] {
			if st.slot[j] == b {
				o.Conflicts = append(o.Conflicts, Conflict{ExamID: p.examIDs[j], Shared: int(p.adjW[e][k])})
			}
		}
		sort.Slice(o.Conflicts, func(x, y int) bool {
			if o.Conflicts[x].Shared != o.Conflicts[y].Shared {
				return o.Conflicts[x].Shared > o.Conflicts[y].Shared
			}
			return o.Conflicts[x].ExamID < o.Conflicts[y].ExamID
		})
		if b == a {
			o.Feasible = true
			out[b] = o
			continue
		}
		// günlük sınır: öğrencinin hedef gündeki sınav sayısı (aynı günden taşınıyorsa kendisi hariç) + 1 > 2
		db := dayOf(p, b)
		for _, s := range p.examStudents[e] {
			n := int(st.cnt[int(s)*p.days+db])
			if dayOf(p, a) == db {
				n--
			}
			if n+1 > 2 {
				o.DailyLimit++
			}
		}
		o.OverCapacity = !st.capacityOK([]change{{e, a, b}})
		if len(o.Conflicts) == 0 && o.DailyLimit == 0 && !o.OverCapacity {
			ch := []change{{e, a, b}}
			d, ok := st.evalMove(e, a, b)
			st.revert(ch)
			o.Feasible, o.Delta = ok, d
		}
		out[b] = o
	}
	return out, nil
}
