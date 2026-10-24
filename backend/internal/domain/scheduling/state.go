package scheduling

// state, bir (kısmi) çizelge ve onu hızla değerlendirmek için artımlı yapılar.
type state struct {
	p    *problem
	slot []int // sınav -> slot, -1 = yerleştirilmedi

	cnt     []uint8 // m × days: öğrencinin o günkü sınav sayısı
	conf    []int16 // n × slots: sınavın o slottaki çatışan komşu sayısı
	load    []int   // slot -> öğrenci sayısı
	labLoad []int
	pen     int64

	stamp    []uint32 // öğrenci tekilleştirme (değerlendirme başına damga)
	stampGen uint32
	touched  []int32
	dayBuf   []int // evaluate için yeniden kullanılan tamponlar (ayırma yok)
	pairBuf  []int
}

func newState(p *problem) *state {
	st := &state{p: p, slot: make([]int, p.n), cnt: make([]uint8, p.m*p.days),
		conf: make([]int16, p.n*p.slots), load: make([]int, p.slots), labLoad: make([]int, p.slots),
		stamp: make([]uint32, p.m)}
	for i := range st.slot {
		st.slot[i] = -1
	}
	return st
}

type change struct{ exam, from, to int }

// capacityOK, slot kapasitesi kısıtını değişiklikler uygulandıktan sonraki
// yüklerle denetler. Hamleler (taşıma, Kempe takası, yerleştirme) en fazla iki
// slotu etkiler; bu yüzden ayırma yapmadan iki girişlik bir tabloda çalışır.
func (st *state) capacityOK(changes []change) bool {
	p := st.p
	if p.cap == 0 && p.labCap == 0 {
		return true
	}
	var slotIdx [2]int
	var all, lab [2]int
	n := 0
	add := func(s, sz, lz int) bool {
		for i := 0; i < n; i++ {
			if slotIdx[i] == s {
				all[i] += sz
				lab[i] += lz
				return true
			}
		}
		if n == len(slotIdx) {
			return false
		}
		slotIdx[n], all[n], lab[n] = s, sz, lz
		n++
		return true
	}
	for _, c := range changes {
		sz := p.size[c.exam]
		lz := 0
		if p.lab[c.exam] {
			lz = sz
		}
		if c.from >= 0 && !add(c.from, -sz, -lz) {
			return false
		}
		if !add(c.to, sz, lz) {
			return false
		}
	}
	for i := 0; i < n; i++ {
		s := slotIdx[i]
		if p.cap > 0 && all[i] > 0 && st.load[s]+all[i] > p.cap {
			return false
		}
		if p.labCap > 0 && lab[i] > 0 && st.labLoad[s]+lab[i] > p.labCap {
			return false
		}
	}
	return true
}

// localPenalty, öğrenci s için yalnızca verilen gün çiftleri ve günlerin cezası.
func (st *state) localPenalty(s int32, pairs, days []int) int64 {
	p := st.p
	base := int(s) * p.days
	var pen int64
	for _, x := range pairs {
		if st.cnt[base+x] > 0 && st.cnt[base+x+1] > 0 {
			pen += p.wC
		}
	}
	for _, d := range days {
		if st.cnt[base+d] >= 2 {
			pen += p.wS
		}
	}
	return pen
}

// evaluate, değişiklikleri öğrenci gün sayaçlarına UYGULAR ve ceza farkını ile
// günlük sınır uygunluğunu döner. Çağıran ya commit ya revert etmelidir.
// Çatışma ve kapasite denetimi çağıranın sorumluluğundadır.
func (st *state) evaluate(changes []change) (delta int64, feasible bool, days []int) {
	p := st.p
	days = st.dayBuf[:0]
	defer func() { st.dayBuf = days[:0] }()
	for _, c := range changes {
		for _, d := range [2]int{dayOf(p, c.from), dayOf(p, c.to)} {
			if d >= 0 && !containsInt(days, d) {
				days = append(days, d)
			}
		}
	}
	dayChanged := false
	for _, c := range changes {
		if dayOf(p, c.from) != dayOf(p, c.to) {
			dayChanged = true
			break
		}
	}
	if !dayChanged {
		return 0, true, nil
	}
	pairs := st.pairBuf[:0]
	defer func() { st.pairBuf = pairs[:0] }()
	for _, d := range days {
		for _, x := range [2]int{d - 1, d} {
			if x >= 0 && x < p.days-1 && !containsInt(pairs, x) {
				pairs = append(pairs, x)
			}
		}
	}

	st.stampGen++
	st.touched = st.touched[:0]
	for _, c := range changes {
		if dayOf(p, c.from) == dayOf(p, c.to) {
			continue
		}
		for _, s := range p.examStudents[c.exam] {
			if st.stamp[s] != st.stampGen {
				st.stamp[s] = st.stampGen
				st.touched = append(st.touched, s)
			}
		}
	}
	var before int64
	for _, s := range st.touched {
		before += st.localPenalty(s, pairs, days)
	}
	st.applyCounts(changes, +1)
	feasible = true
	for _, s := range st.touched {
		base := int(s) * p.days
		for _, d := range days {
			if st.cnt[base+d] > 2 {
				feasible = false
			}
		}
	}
	var after int64
	for _, s := range st.touched {
		after += st.localPenalty(s, pairs, days)
	}
	return after - before, feasible, days
}

// applyCounts, gün sayaçlarını ileri (+1) veya geri (-1) uygular. Önce tüm
// azaltmalar sonra tüm artırmalar: iki gün arasında takas (Kempe) doğru sayılır.
func (st *state) applyCounts(changes []change, dir int) {
	p := st.p
	from, to := func(c change) int { return c.from }, func(c change) int { return c.to }
	if dir < 0 {
		from, to = to, from
	}
	for _, c := range changes {
		if d := dayOf(p, from(c)); d >= 0 && d != dayOf(p, to(c)) {
			for _, s := range p.examStudents[c.exam] {
				st.cnt[int(s)*p.days+d]--
			}
		}
	}
	for _, c := range changes {
		if d := dayOf(p, to(c)); d >= 0 && d != dayOf(p, from(c)) {
			for _, s := range p.examStudents[c.exam] {
				st.cnt[int(s)*p.days+d]++
			}
		}
	}
}

func (st *state) revert(changes []change) { st.applyCounts(changes, -1) }

// commit, değerlendirilmiş değişiklikleri kalıcı yapar: slot, çatışma sayaçları, yükler.
func (st *state) commit(changes []change, delta int64) {
	p := st.p
	for _, c := range changes {
		e := c.exam
		for _, j := range p.adj[e] {
			if c.from >= 0 {
				st.conf[int(j)*p.slots+c.from]--
			}
			st.conf[int(j)*p.slots+c.to]++
		}
		if c.from >= 0 {
			st.load[c.from] -= p.size[e]
			if p.lab[e] {
				st.labLoad[c.from] -= p.size[e]
			}
		}
		st.load[c.to] += p.size[e]
		if p.lab[e] {
			st.labLoad[c.to] += p.size[e]
		}
		st.slot[e] = c.to
	}
	st.pen += delta
}

// conflictFree, sınavın verilen slota (kendi slotu dışındaki) çatışan komşu olmadan
// gidebileceğini söyler.
func (st *state) conflictFree(e, s int) bool { return st.conf[e*st.p.slots+s] == 0 }

func dayOf(p *problem, slot int) int {
	if slot < 0 {
		return -1
	}
	return slot / p.sessions
}

func containsInt(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

// evalMove, tek bir sınavın taşınması için evaluate'in hızlı yoludur (yerel aramadaki
// en sık hamle). Sınavın öğrencileri tekil olduğundan damgalama gerekmez; her öğrenci
// için önce/sonra cezası tek geçişte hesaplanır. Sözleşme evaluate ile aynıdır:
// sayaçlar UYGULANIR, çağıran commit ya da revert eder.
func (st *state) evalMove(e, from, to int) (delta int64, feasible bool) {
	p := st.p
	d1, d2 := dayOf(p, from), dayOf(p, to)
	if d1 == d2 {
		return 0, true
	}
	var pairs [4]int
	np := 0
	addPair := func(x int) {
		if x < 0 || x >= p.days-1 {
			return
		}
		for i := 0; i < np; i++ {
			if pairs[i] == x {
				return
			}
		}
		pairs[np] = x
		np++
	}
	if d1 >= 0 {
		addPair(d1 - 1)
		addPair(d1)
	}
	addPair(d2 - 1)
	addPair(d2)

	pen := func(base int) int64 {
		var v int64
		for i := 0; i < np; i++ {
			if st.cnt[base+pairs[i]] > 0 && st.cnt[base+pairs[i]+1] > 0 {
				v += p.wC
			}
		}
		if d1 >= 0 && st.cnt[base+d1] >= 2 {
			v += p.wS
		}
		if st.cnt[base+d2] >= 2 {
			v += p.wS
		}
		return v
	}

	feasible = true
	for _, s := range p.examStudents[e] {
		base := int(s) * p.days
		before := pen(base)
		if d1 >= 0 {
			st.cnt[base+d1]--
		}
		st.cnt[base+d2]++
		if st.cnt[base+d2] > 2 {
			feasible = false
		}
		delta += pen(base) - before
	}
	return delta, feasible
}
