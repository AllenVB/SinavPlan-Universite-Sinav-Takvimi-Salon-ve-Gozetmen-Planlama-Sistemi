package scheduling

import (
	"math"
	"math/rand/v2"
	"time"
)

// Result, çözücü çıktısı.
type Result struct {
	// Assignment: sınav kimliği -> slot (0..Slots-1). Yerleştirilemeyenler yoktur.
	Assignment map[int64]int
	// Unplaced boş değilse çizelge eksiktir; Infeasibility açıklamayı taşır.
	Unplaced      []int64
	Infeasibility *InfeasibilityReport

	ConstructionPenalty int64 // DSatur sonrası ceza
	Penalty             int64 // yerel arama sonrası (en iyi) ceza
	Iterations          int
	AcceptedMoves       int
	StoppedByTime       bool
	Duration            time.Duration

	Graph GraphStats
}

// GraphStats, çatışma grafının boyutu (rapor için).
type GraphStats struct {
	Exams, Students, Edges, MaxDegree int
}

// SlotDay ve SlotSession, slot numarasını gün/oturuma çevirir.
func SlotDay(slot, sessionsPerDay int) int     { return slot / sessionsPerDay }
func SlotSession(slot, sessionsPerDay int) int { return slot % sessionsPerDay }

// Solve, çizelgeyi üretir: DSatur benzeri yapıcı yerleştirme, tek-engelleyici
// onarımı, ardından tohumlu simulated annealing (taşıma + Kempe zinciri hamleleri).
func Solve(in Input) (*Result, error) {
	start := time.Now()
	p, err := newProblem(in)
	if err != nil {
		return nil, err
	}
	st := newState(p)
	rng := rand.New(rand.NewPCG(uint64(in.Seed), 0x2545f4914f6cdd1d))

	unplaced := construct(st)
	if len(unplaced) > 0 {
		unplaced = repair(st, unplaced)
	}

	res := &Result{ConstructionPenalty: st.pen,
		Graph: GraphStats{Exams: p.n, Students: p.m, Edges: p.edges()}}
	for _, a := range p.adj {
		res.Graph.MaxDegree = max(res.Graph.MaxDegree, len(a))
	}

	if len(unplaced) == 0 {
		anneal(st, rng, in, start, res)
	}

	res.Assignment = make(map[int64]int, p.n)
	for e, s := range st.slot {
		if s >= 0 {
			res.Assignment[p.examIDs[e]] = s
		}
	}
	for _, e := range unplaced {
		res.Unplaced = append(res.Unplaced, p.examIDs[e])
	}
	if len(unplaced) > 0 {
		res.Infeasibility = explain(st, unplaced)
	}
	res.Penalty = st.pen
	res.Duration = time.Since(start)
	return res, nil
}

// construct: her adımda en kısıtlı sınav (çatışma nedeniyle kapanmış slot sayısı en
// yüksek; eşitlikte ağırlıklı derece, sonra küçük kimlik) en az ceza artışı veren
// uygun slota konur. Eşitlikte slot yükü az olan, sonra küçük slot numarası seçilir
// (yük dengesi T-2'ye pay bırakır).
func construct(st *state) []int {
	p := st.p
	saturation := make([]int, p.n)
	placed := make([]bool, p.n)
	var unplaced []int
	for step := 0; step < p.n; step++ {
		e := -1
		for i := 0; i < p.n; i++ {
			if placed[i] {
				continue
			}
			if e < 0 || saturation[i] > saturation[e] ||
				(saturation[i] == saturation[e] && (p.wDegree[i] > p.wDegree[e] ||
					(p.wDegree[i] == p.wDegree[e] && p.examIDs[i] < p.examIDs[e]))) {
				e = i
			}
		}
		placed[e] = true

		best, bestDelta, bestLoad := -1, int64(0), 0
		for s := 0; s < p.slots; s++ {
			ch := []change{{e, -1, s}}
			if !st.conflictFree(e, s) || !st.capacityOK(ch) {
				continue
			}
			delta, ok, _ := st.evaluate(ch)
			st.revert(ch)
			if !ok {
				continue
			}
			if best < 0 || delta < bestDelta || (delta == bestDelta && st.load[s] < bestLoad) {
				best, bestDelta, bestLoad = s, delta, st.load[s]
			}
		}
		if best < 0 {
			unplaced = append(unplaced, e)
			continue
		}
		ch := []change{{e, -1, best}}
		d, _, _ := st.evaluate(ch)
		st.commit(ch, d)
		for _, j := range p.adj[e] {
			if !placed[j] && st.conf[int(j)*p.slots+best] == 1 {
				saturation[j]++ // bu slot j için ilk kez kapandı
			}
		}
	}
	return unplaced
}

// repair: yerleşemeyen sınav için, slotunda tek bir engelleyici komşu olan bir slot
// bulunursa o komşuyu başka uygun slota taşıyıp sınavı yerleştirmeyi dener.
func repair(st *state, unplaced []int) []int {
	p := st.p
	for round := 0; round < 3 && len(unplaced) > 0; round++ {
		var still []int
		for _, e := range unplaced {
			if !tryPlace(st, e) {
				still = append(still, e)
			}
		}
		if len(still) == len(unplaced) {
			unplaced = still
			break
		}
		unplaced = still
	}
	_ = p
	return unplaced
}

func tryPlace(st *state, e int) bool {
	p := st.p
	place := func(s int) bool {
		ch := []change{{e, -1, s}}
		if !st.conflictFree(e, s) || !st.capacityOK(ch) {
			return false
		}
		d, ok, _ := st.evaluate(ch)
		if !ok {
			st.revert(ch)
			return false
		}
		st.commit(ch, d)
		return true
	}
	for s := 0; s < p.slots; s++ {
		if place(s) {
			return true
		}
	}
	for s := 0; s < p.slots; s++ {
		var blockers []int
		for _, j := range p.adj[e] {
			if st.slot[j] == s {
				blockers = append(blockers, int(j))
			}
		}
		if len(blockers) != 1 {
			continue
		}
		b := blockers[0]
		for s2 := 0; s2 < p.slots; s2++ {
			if s2 == s {
				continue
			}
			ch := []change{{b, s, s2}}
			if !st.conflictFree(b, s2) || !st.capacityOK(ch) {
				continue
			}
			d, ok, _ := st.evaluate(ch)
			if !ok {
				st.revert(ch)
				continue
			}
			st.commit(ch, d)
			if place(s) {
				return true
			}
			// geri al
			back := []change{{b, s2, s}}
			d2, _, _ := st.evaluate(back)
			st.commit(back, d2)
		}
	}
	return false
}

// DefaultIterations: 300 sınav / 8.000 öğrenci için ~11 sn (hedef < 15 sn).
const DefaultIterations = 1_000_000

// maxKempe, Kempe zincirinin en fazla sınav sayısı.
const maxKempe = 30

// anneal: simulated annealing. Hamleler: (1) tek sınavı başka slota taşıma,
// (2) iki slot arasında Kempe zinciri takası (çatışma kısıtını kendiliğinden korur).
// En iyi çözüm saklanır ve sonunda geri yüklenir.
func anneal(st *state, rng *rand.Rand, in Input, start time.Time, res *Result) {
	p := st.p
	iters := in.MaxIterations
	if iters <= 0 {
		iters = DefaultIterations
	}
	// Başlangıç sıcaklığı ceza ağırlığına göreli. ~170 öğrencili bir sınavı taşımak
	// cezayı onlarca-yüzlerce değiştirir; T0=1.5·Wc ile SA açgözlü inişe dönüp ~9.230'da
	// takılıyordu. Ölçüm (300/8000/15 gün, 1M iterasyon): T0=4.5 -> 9228, 60 -> 8484,
	// 150 -> 8407, 400 -> 8303 (ama 500k'da kararsız). 50·Wc seçildi (ADR-007).
	t0, tEnd := float64(p.wC)*50, 0.05
	alpha := math.Pow(tEnd/t0, 1/float64(iters))
	temp := t0

	bestPen := st.pen
	best := append([]int(nil), st.slot...)

	var chain []change
	inChain := make([]bool, p.n)
	queue := make([]int, 0, p.n)

	for it := 0; it < iters; it++ {
		if in.TimeLimit > 0 && it%2048 == 0 && time.Since(start) > in.TimeLimit {
			res.StoppedByTime = true
			break
		}
		res.Iterations++
		temp *= alpha
		e := rng.IntN(p.n)
		a := st.slot[e]
		b := rng.IntN(p.slots - 1)
		if b >= a {
			b++
		}

		chain = chain[:0]
		if rng.IntN(2) == 0 {
			if !st.conflictFree(e, b) {
				continue
			}
			chain = append(chain, change{e, a, b})
		} else {
			// Kempe zinciri: e'den başlayıp a ve b slotlarındaki çatışma komşularına yayıl.
			// Zincir maxKempe sınavı aşarsa hamle atlanır: dev zincirler (yüksek dereceli
			// seçmeli dersler iki slotun neredeyse tamamını çeker) binlerce öğrencinin
			// cezasını yeniden hesaplatır ve neredeyse hiç kabul edilmez (profil: süreyi
			// yarıdan fazla yiyordu).
			queue = append(queue[:0], e)
			inChain[e] = true
			tooBig := false
			for qi := 0; qi < len(queue) && !tooBig; qi++ {
				x := queue[qi]
				for _, j := range p.adj[x] {
					sj := st.slot[j]
					if (sj == a || sj == b) && sj != st.slot[x] && !inChain[j] {
						inChain[j] = true
						queue = append(queue, int(j))
						if len(queue) > maxKempe {
							tooBig = true
							break
						}
					}
				}
			}
			if tooBig {
				for _, x := range queue {
					inChain[x] = false
				}
				continue
			}
			for _, x := range queue {
				inChain[x] = false
				to := b
				if st.slot[x] == b {
					to = a
				}
				chain = append(chain, change{x, st.slot[x], to})
			}
		}
		if !st.capacityOK(chain) {
			continue
		}
		var delta int64
		var ok bool
		if len(chain) == 1 {
			delta, ok = st.evalMove(chain[0].exam, chain[0].from, chain[0].to)
		} else {
			delta, ok, _ = st.evaluate(chain)
		}
		if !ok || (delta > 0 && rng.Float64() >= math.Exp(-float64(delta)/temp)) {
			st.revert(chain)
			continue
		}
		st.commit(chain, delta)
		res.AcceptedMoves++
		if st.pen < bestPen {
			bestPen = st.pen
			copy(best, st.slot)
		}
	}

	// En iyi çözümü geri yükle: yapıları sıfırdan kur (artımlı hata birikmesin).
	fresh := newState(p)
	for e, s := range best {
		ch := []change{{e, -1, s}}
		d, _, _ := fresh.evaluate(ch)
		fresh.commit(ch, d)
	}
	*st = *fresh
}
