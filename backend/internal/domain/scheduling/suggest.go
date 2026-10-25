package scheduling

import (
	"fmt"
	"sort"
)

// MoveSuggestion, tek bir sınavı başka bir slota taşımanın sert kısıtları bozmadan cezayı ne kadar
// düşürdüğüdür (Delta < 0 iyileşmedir). Slotlar Input'taki 0 tabanlı indekslerdir.
type MoveSuggestion struct {
	ExamID int64 `json:"exam_id"`
	From   int   `json:"from"`
	To     int   `json:"to"`
	Delta  int64 `json:"delta"`
}

// SuggestOptions, öneri aramasının sınırlarıdır.
type SuggestOptions struct {
	Limit   int     // en çok kaç öneri; 0 = 3
	PerExam int     // sınav başına en çok kaç hedef slot; 0 = 1 (çeşitlilik için)
	Exams   []int64 // yalnız bu sınavlar için ara; boş = tümü
	// Locked[s] true ise s slotu ne kaynak ne hedef olabilir (ör. başlamış/bitmiş oturumlar:
	// yapılmış bir sınav taşınmaz, geçmişe sınav konmaz).
	Locked []bool
	// OnlyImproving: yalnız toplam cezayı düşüren (Delta < 0) hamleler. false ise belirli bir sorunu
	// çözen ama toplam cezayı biraz artırabilen hamleler de döner (Delta dürüstçe raporlanır).
	OnlyImproving bool
	// Accept, çağıranın ek koşuludur (ör. "öğrencinin başka sınavı olmayan bir güne"); nil = hepsi.
	Accept func(m MoveSuggestion) bool
}

// SuggestMoves, geçerli bir atamadan başlayıp aday sınavların her biri için her slotu yerel aramanın
// artımlı değerlendiricisiyle (evalMove) dener: çatışma, slot kapasitesi ve "günde en çok 2 sınav"
// korunur. Sınav başına en iyi PerExam hamle tutulur, toplam cezayı en az artıran / en çok düşürenler döner. Sonuç deterministiktir
// (eşitlikte sınav kimliği, sonra hedef slot). Atama sert kısıt ihlali içeriyorsa hata döner:
// bozuk bir plana "iyileştirme" önermek yanıltıcı olurdu.
func SuggestMoves(in Input, assignment map[int64]int, opt SuggestOptions) ([]MoveSuggestion, error) {
	if rep := Validate(in, assignment); len(rep.Violations) > 0 {
		return nil, fmt.Errorf("atama %d sert kısıtı ihlal ediyor; önce düzeltilmeli", len(rep.Violations))
	}
	p, err := newProblem(in)
	if err != nil {
		return nil, err
	}
	limit := opt.Limit
	if limit <= 0 {
		limit = 3
	}
	perExam := opt.PerExam
	if perExam <= 0 {
		perExam = 1
	}
	locked := func(s int) bool { return s >= 0 && s < len(opt.Locked) && opt.Locked[s] }
	idx := make(map[int64]int, p.n)
	st := newState(p)
	for e, id := range p.examIDs {
		idx[id] = e
		ch := []change{{e, -1, assignment[id]}}
		d, _, _ := st.evaluate(ch)
		st.commit(ch, d)
	}
	cands := make([]int, 0, p.n)
	if len(opt.Exams) == 0 {
		for e := range p.examIDs {
			cands = append(cands, e)
		}
	} else {
		for _, id := range opt.Exams {
			if e, ok := idx[id]; ok {
				cands = append(cands, e)
			}
		}
	}

	var out []MoveSuggestion
	for _, e := range cands {
		a := st.slot[e]
		if locked(a) {
			continue
		}
		var mine []MoveSuggestion
		for b := 0; b < p.slots; b++ {
			if b == a || locked(b) || !st.conflictFree(e, b) {
				continue
			}
			ch := []change{{e, a, b}}
			if !st.capacityOK(ch) {
				continue
			}
			d, ok := st.evalMove(e, a, b)
			st.revert(ch)
			m := MoveSuggestion{ExamID: p.examIDs[e], From: a, To: b, Delta: d}
			if !ok || (opt.OnlyImproving && d >= 0) || (opt.Accept != nil && !opt.Accept(m)) {
				continue
			}
			mine = append(mine, m)
		}
		sort.SliceStable(mine, func(i, j int) bool { return mine[i].Delta < mine[j].Delta })
		out = append(out, mine[:min(len(mine), perExam)]...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Delta != out[j].Delta {
			return out[i].Delta < out[j].Delta
		}
		if out[i].ExamID != out[j].ExamID {
			return out[i].ExamID < out[j].ExamID
		}
		return out[i].To < out[j].To
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
