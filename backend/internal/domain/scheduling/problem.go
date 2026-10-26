// Package scheduling, sınav takvimi çizelgelemesini (T-1) çözer. Saf Go'dur:
// veritabanı, saat veya global rastgelelik kullanmaz; aynı girdi + aynı tohum +
// aynı iterasyon bütçesi her makinede aynı çizelgeyi verir.
//
// Model: slot = gün × oturum. Sert kısıtlar: (a) ortak öğrencisi olan iki sınav aynı
// slotta olamaz, (b) bir öğrencinin bir günde en fazla 2 sınavı olur, (c) isteğe bağlı
// slot koltuk kapasitesi. Yumuşak hedef (en aza indirilen ceza, tam sayı):
//
//	ceza = Wc · Σ_öğrenci Σ_d [d ve d+1 günlerinin ikisinde de sınavı var]
//	     + Ws · Σ_öğrenci Σ_d [d gününde 2 sınavı var]
//
// Varsayılan Wc=3, Ws=1: ardışık gün yoğunluğu, aynı gün iki sınavdan daha ağırdır
// (DECISIONS ADR-007).
package scheduling

import (
	"fmt"
	"math/bits"
	"sort"
	"time"
)

// Exam, çizelgelenecek bir sınav.
type Exam struct {
	ID       int64
	Students []int64
	Lab      bool // laboratuvar gerektirir (lab koltuk kapasitesine sayılır)
}

// Weights, yumuşak ceza ağırlıkları.
//
// FrontLoad (varsayılan 0 = kapalı): büyük sınavı (≥ LargeExamStudents öğrenci) erken güne alma cezası; büyük bir sınav
// d. güne konursa ceza FrontLoad·d olur. UniTime/ITC-2007'deki "büyük sınavlar öne" yumuşak kısıtının karşılığıdır;
// sonuç değerlendirmesi ve not girişi için büyük sınavın erken bitmesi istenir (ADR-022).
type Weights struct {
	ConsecutiveDay int64
	SameDay        int64
	FrontLoad      int64
}

// withDefaults: ardışık/aynı gün ağırlıklarının ikisi de 0 ise varsayılanları (3, 1) kullanır; FrontLoad korunur.
func (w Weights) withDefaults() Weights {
	if w.ConsecutiveDay == 0 && w.SameDay == 0 {
		w.ConsecutiveDay, w.SameDay = DefaultWeights.ConsecutiveDay, DefaultWeights.SameDay
	}
	return w
}

// DefaultLargeExam: FrontLoad açıkken "büyük sınav" eşiği (öğrenci sayısı), Input.LargeExamStudents 0 ise.
const DefaultLargeExam = 100

// DefaultWeights: Wc=3, Ws=1.
var DefaultWeights = Weights{ConsecutiveDay: 3, SameDay: 1}

// Input, çözücü girdisi.
type Input struct {
	Exams          []Exam
	Days           int
	SessionsPerDay int
	Seed           int64
	Weights        Weights
	// MaxIterations, yerel arama bütçesi; determinizmin kaynağı budur.
	MaxIterations int
	// TimeLimit yalnızca güvenlik sınırıdır (0 = sınırsız). Süre dolarsa sonuç
	// determinizmini kaybeder ve Result.StoppedByTime=true olur.
	TimeLimit time.Duration
	// SlotCapacity / LabSlotCapacity: bir slottaki toplam öğrenci (koltuk) sınırı;
	// 0 = sınırsız. T-2'nin salon kapasitesi aşılmadan çözülebilmesi için.
	SlotCapacity    int
	LabSlotCapacity int
	// LargeExamStudents: FrontLoad cezasında "büyük sınav" eşiği; 0 = DefaultLargeExam.
	LargeExamStudents int
}

// Slots, toplam slot sayısı.
func (in Input) Slots() int { return in.Days * in.SessionsPerDay }

// problem, girdinin yoğun (indeksli) temsilidir.
type problem struct {
	n, m, days, sessions, slots int
	examIDs                     []int64
	examStudents                [][]int32 // sınav -> öğrenci indeksleri
	lab                         []bool
	size                        []int
	studentIDs                  []int64

	// Öğrenci başına sınav kümesi bitset olarak (m × words). Çatışma grafı bu
	// bitset'lerin OR'lanmasıyla kurulur.
	words        int
	studentBits  []uint64
	conflictBits []uint64 // n × words: sınav -> çatıştığı sınavlar

	adj     [][]int32 // çatışma komşuları
	adjW    [][]int32 // kenar ağırlığı = ortak öğrenci sayısı
	wDegree []int64   // ağırlıklı derece (DSatur eşitlik bozma)

	wC, wS, wF  int64
	large       []bool // büyük sınav (FrontLoad cezası bunlara uygulanır)
	cap, labCap int
}

func newProblem(in Input) (*problem, error) {
	if in.Days <= 0 || in.SessionsPerDay <= 0 {
		return nil, fmt.Errorf("gün ve oturum sayısı pozitif olmalı")
	}
	w := in.Weights.withDefaults()
	p := &problem{n: len(in.Exams), days: in.Days, sessions: in.SessionsPerDay,
		slots: in.Days * in.SessionsPerDay, wC: w.ConsecutiveDay, wS: w.SameDay, wF: w.FrontLoad,
		cap: in.SlotCapacity, labCap: in.LabSlotCapacity}

	// Öğrenci kimliklerini sıralı yoğun indekslere çevir (determinizm için sıralı).
	seenExam := map[int64]bool{}
	idx := map[int64]int32{}
	var ids []int64
	for _, e := range in.Exams {
		if seenExam[e.ID] {
			return nil, fmt.Errorf("sınav %d iki kez verilmiş", e.ID)
		}
		seenExam[e.ID] = true
		for _, s := range e.Students {
			if _, ok := idx[s]; !ok {
				idx[s] = 0
				ids = append(ids, s)
			}
		}
	}
	sort.Slice(ids, func(a, b int) bool { return ids[a] < ids[b] })
	for i, s := range ids {
		idx[s] = int32(i)
	}
	p.m, p.studentIDs = len(ids), ids

	p.words = (p.n + 63) / 64
	p.studentBits = make([]uint64, p.m*p.words)
	p.examIDs = make([]int64, p.n)
	p.examStudents = make([][]int32, p.n)
	p.lab = make([]bool, p.n)
	p.size = make([]int, p.n)
	for e, ex := range in.Exams {
		p.examIDs[e], p.lab[e] = ex.ID, ex.Lab
		seen := map[int32]bool{}
		for _, s := range ex.Students {
			si := idx[s]
			if seen[si] {
				continue // aynı öğrenci iki kez yazılmışsa bir kez say
			}
			seen[si] = true
			p.examStudents[e] = append(p.examStudents[e], si)
			p.studentBits[int(si)*p.words+e/64] |= 1 << (e % 64)
		}
		sort.Slice(p.examStudents[e], func(a, b int) bool { return p.examStudents[e][a] < p.examStudents[e][b] })
		p.size[e] = len(p.examStudents[e])
	}
	p.large = make([]bool, p.n)
	if p.wF > 0 {
		thr := in.LargeExamStudents
		if thr <= 0 {
			thr = DefaultLargeExam
		}
		for e := range p.large {
			p.large[e] = p.size[e] >= thr
		}
	}

	// Çatışma bitset'leri: sınavın her öğrencisinin sınav kümesini OR'la.
	p.conflictBits = make([]uint64, p.n*p.words)
	for e := 0; e < p.n; e++ {
		row := p.conflictBits[e*p.words : (e+1)*p.words]
		for _, s := range p.examStudents[e] {
			sb := p.studentBits[int(s)*p.words : (int(s)+1)*p.words]
			for k := range row {
				row[k] |= sb[k]
			}
		}
		row[e/64] &^= 1 << (e % 64) // kendisiyle çatışmaz
	}

	// Kenar ağırlıkları: ortak öğrenci sayısı = iki sınavın öğrenci bitset'lerinin
	// kesişimi; burada öğrenci başına sınav listesinden çift sayarak hesaplıyoruz.
	weight := make(map[[2]int32]int32)
	for s := 0; s < p.m; s++ {
		exams := p.studentExamList(s)
		for i := 0; i < len(exams); i++ {
			for j := i + 1; j < len(exams); j++ {
				weight[[2]int32{exams[i], exams[j]}]++
			}
		}
	}
	p.adj = make([][]int32, p.n)
	p.adjW = make([][]int32, p.n)
	p.wDegree = make([]int64, p.n)
	for e := 0; e < p.n; e++ {
		row := p.conflictBits[e*p.words : (e+1)*p.words]
		for k, word := range row {
			for word != 0 {
				b := bits.TrailingZeros64(word)
				word &= word - 1
				j := int32(k*64 + b)
				key := [2]int32{int32(e), j}
				if j < int32(e) {
					key = [2]int32{j, int32(e)}
				}
				w := weight[key]
				p.adj[e] = append(p.adj[e], j)
				p.adjW[e] = append(p.adjW[e], w)
				p.wDegree[e] += int64(w)
			}
		}
	}
	return p, nil
}

// studentExamList, öğrencinin sınavlarını (artan indeks) bitset'ten çıkarır.
func (p *problem) studentExamList(s int) []int32 {
	var out []int32
	for k, word := range p.studentBits[s*p.words : (s+1)*p.words] {
		for word != 0 {
			b := bits.TrailingZeros64(word)
			word &= word - 1
			out = append(out, int32(k*64+b))
		}
	}
	return out
}

// edges, çatışma grafındaki kenar sayısı.
func (p *problem) edges() int {
	t := 0
	for _, a := range p.adj {
		t += len(a)
	}
	return t / 2
}
