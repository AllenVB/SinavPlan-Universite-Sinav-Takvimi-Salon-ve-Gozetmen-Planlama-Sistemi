// Package rooms, bir slottaki sınavları salonlara yerleştirir (T-2). Saf Go ve
// deterministiktir: aynı girdi (sıra dahil) her zaman aynı planı verir.
//
// ETKİN KAPASİTE (T-3 ile etkileşim, DECISIONS ADR-008): "aynı sınavın öğrencileri
// yan yana oturmaz" kuralı yüzünden bir salonun kullanılabilir koltuk sayısı, içindeki
// sınav bileşimine bağlıdır:
//
//	tek sınavlı salon : her satırdaki ardışık aktif koltuk dizisinde ceil(len/2)
//	                    (bir koltuk atlayarak oturma)  -> Single
//	karışık salon     : aktif koltuk eksi salondaki engelli öğrenci sayısı (sütun
//	                    dönüşümlü oturma); hiçbir sınavın payı Single'ı aşamaz
//
// Yani oda kapasitesi sabit değildir: aynı salona iki sınavı karıştırmak, tek sınavı
// koymaktan neredeyse iki kat öğrenci sığdırır. Çözücü bu yüzden paylaşımı tercih eder.
package rooms

import (
	"fmt"
	"sort"
)

// Seat, salon koltuğu.
type Seat struct {
	Row, Col   int
	Active     bool
	Accessible bool
}

// Room, salon.
type Room struct {
	ID         int64
	Lab        bool
	Rows, Cols int
	Seats      []Seat
}

// Exam, slottaki bir sınav.
type Exam struct {
	ID       int64
	Students int
	Disabled int  // erişilebilir koltuk isteyen öğrenci sayısı (Students'ın alt kümesi)
	Lab      bool // laboratuvar salonu gerektirir
}

// Config, kapasite modelini seçer.
type Config struct {
	// Strict, T-3'te ön/arka koltukların da komşu sayılması (varsayılan kapalı).
	Strict bool
}

// Placement, bir sınavın bir salondaki payı.
type Placement struct {
	ExamID   int64
	RoomID   int64
	Count    int
	Disabled int // bu payın erişilebilir koltuğa oturacak öğrenci sayısı
}

// Unplaced, hiçbir şekilde yerleştirilemeyen sınav (kısmi yerleştirme yapılmaz).
type Unplaced struct {
	ExamID   int64
	Students int
	Disabled int
	Reason   string
}

// Plan, bir slotun salon planı.
type Plan struct {
	Placements []Placement // (sınav, salon) sıralı
	Unplaced   []Unplaced
	RoomsUsed  int
}

// Capacity, salonun kullanılabilir koltuk sayıları.
type Capacity struct {
	Active     int // aktif koltuk (karışık salonun üst sınırı)
	Single     int // tek bir sınavın en fazla oturabileceği sayı
	Accessible int // birbirine komşu olmayan erişilebilir koltuk sayısı
}

type cell [2]int

// EffectiveCapacity, salonun etkin kapasitesini hesaplar.
func EffectiveCapacity(r Room, cfg Config) Capacity {
	active := map[cell]bool{}
	var accessible []Seat
	for _, s := range r.Seats {
		if s.Active {
			active[cell{s.Row, s.Col}] = true
			if s.Accessible {
				accessible = append(accessible, s)
			}
		}
	}
	c := Capacity{Active: len(active)}

	if cfg.Strict {
		// Sıkı mod: dama tahtası bağımsız küme; iki rengin büyüğü.
		var black, white int
		for k := range active {
			if (k[0]+k[1])%2 == 0 {
				black++
			} else {
				white++
			}
		}
		c.Single = max(black, white)
	} else {
		// Her satırda ardışık aktif koltuk dizileri; dizi başına ceil(len/2).
		rows := map[int][]int{}
		for k := range active {
			rows[k[0]] = append(rows[k[0]], k[1])
		}
		for _, cols := range rows {
			sort.Ints(cols)
			run := 1
			for i := 1; i <= len(cols); i++ {
				if i < len(cols) && cols[i] == cols[i-1]+1 {
					run++
					continue
				}
				c.Single += (run + 1) / 2
				run = 1
			}
		}
	}

	// Aynı sınavın iki öğrencisi komşu erişilebilir koltuklara oturamaz: komşu olmayan
	// en büyük alt küme (satır-sütun sırasıyla açgözlü).
	sort.Slice(accessible, func(a, b int) bool {
		if accessible[a].Row != accessible[b].Row {
			return accessible[a].Row < accessible[b].Row
		}
		return accessible[a].Col < accessible[b].Col
	})
	taken := map[cell]bool{}
	for _, s := range accessible {
		k := cell{s.Row, s.Col}
		if taken[cell{k[0], k[1] - 1}] || (cfg.Strict && taken[cell{k[0] - 1, k[1]}]) {
			continue
		}
		taken[k] = true
		c.Accessible++
	}
	return c
}

// --- Atama ---------------------------------------------------------------------

type group struct {
	exam       int64
	count, dis int
}

type roomState struct {
	room   Room
	cap    Capacity
	groups []group
	sum    int
	accUse int
}

func (rs *roomState) used() bool { return len(rs.groups) > 0 }

// fits, sınavdan k öğrenci (d'si erişilebilir koltuklu) eklendikten sonra salonun
// etkin kapasite kurallarının sağlanıp sağlanmadığını söyler:
//
//	erişilebilir:  Σ engelli ≤ Accessible
//	her sınav payı ≤ Single - (başka sınavların engelli sayısı)   [karışık salonda]
//	toplam        ≤ Active - Σ engelli                           [karışık salonda]
//	tek sınavlı salonda yalnız pay ≤ Single.
//
// Engelli öğrenciler erişilebilir (ön sıra, çift sütun) koltuklara ÖNCE oturur. Bir
// sınavın payı Single'a eşitse salonun her satırında maksimum bağımsız kümeyi
// doldurmak zorundadır; başka bir sınavın engellisi o kümeden bir koltuğu alırsa
// pay en fazla 1 azalabilir (bir köşeyi çıkarmak maksimum bağımsız kümeyi ≤ 1
// küçültür). Ölçüm: bu terimler olmadan 52.033 öğrencinin 110'u (sonra 61'i) açıkta kaldı.
func (rs *roomState) fits(exam int64, k, d int) bool {
	acc := rs.accUse + d
	if acc > rs.cap.Accessible {
		return false
	}
	var shares [][2]int // (pay, engelli)
	seen := false
	for _, g := range rs.groups {
		if g.exam == exam {
			shares = append(shares, [2]int{g.count + k, g.dis + d})
			seen = true
		} else {
			shares = append(shares, [2]int{g.count, g.dis})
		}
	}
	if !seen {
		shares = append(shares, [2]int{k, d})
	}
	if len(shares) == 1 {
		return shares[0][0] <= rs.cap.Single
	}
	for _, sh := range shares {
		if sh[0] > rs.cap.Single-(acc-sh[1]) {
			return false
		}
	}
	return rs.sum+k <= rs.cap.Active-acc
}

// maxAdd, sınavdan bu salona eklenebilecek en çok öğrenci (k) ve bunun kaçının
// erişilebilir koltuğa oturabileceği (d); kuralların tamamı fits'te.
func (rs *roomState) maxAdd(exam int64, rem, remDis int) (k, d int) {
	kmax := min(rem, rs.cap.Active-rs.sum)
	for k = kmax; k > 0; k-- {
		for d = min(remDis, rs.cap.Accessible-rs.accUse, k); d >= 0; d-- {
			if rs.fits(exam, k, d) {
				return k, d
			}
		}
	}
	return 0, 0
}

func (rs *roomState) add(exam int64, k, d int) {
	for i := range rs.groups {
		if rs.groups[i].exam == exam {
			rs.groups[i].count += k
			rs.groups[i].dis += d
			rs.sum += k
			rs.accUse += d
			return
		}
	}
	rs.groups = append(rs.groups, group{exam, k, d})
	rs.sum += k
	rs.accUse += d
}

func (rs *roomState) remove(exam int64, k, d int) {
	for i := range rs.groups {
		if rs.groups[i].exam == exam {
			rs.groups[i].count -= k
			rs.groups[i].dis -= d
			rs.sum -= k
			rs.accUse -= d
			if rs.groups[i].count == 0 {
				rs.groups = append(rs.groups[:i], rs.groups[i+1:]...)
			}
			return
		}
	}
}

type chunk struct {
	rs   *roomState
	k, d int
}

// place, sınavı verilen salonlara dağıtır; hepsini sığdıramazsa hiçbir şey değiştirmez.
//
// Önce tüm sınavı sığdıran en sıkı (best-fit) salon aranır; yoksa bölünür: erişilebilir
// koltuk gereken öğrenciler varken yalnız erişilebilir koltuğu olan salonlar, sonra
// zaten kullanılan salonlar (paylaşım), sonra büyük salonlar seçilir.
func place(cands []*roomState, e Exam) ([]chunk, bool) {
	rem, remDis := e.Students, e.Disabled

	var best *roomState
	for _, rs := range cands {
		if k, d := rs.maxAdd(e.ID, rem, remDis); k == rem && d == remDis {
			slack := rs.cap.Active - rs.sum - rem
			if best == nil || slack < best.cap.Active-best.sum-rem ||
				(slack == best.cap.Active-best.sum-rem && rs.used() && !best.used()) {
				best = rs
			}
		}
	}
	if best != nil {
		best.add(e.ID, rem, remDis)
		return []chunk{{best, rem, remDis}}, true
	}

	var done []chunk
	undo := func() {
		for _, c := range done {
			c.rs.remove(e.ID, c.k, c.d)
		}
	}
	for rem > 0 {
		var pick *roomState
		var pk, pd int
		for _, rs := range cands {
			k, d := rs.maxAdd(e.ID, rem, remDis)
			if k == 0 || (remDis > 0 && d == 0) {
				continue
			}
			// Payı, kalan engelli öğrencilere hâlâ bir sonraki paylarda yer kalacak
			// şekilde sınırla: aksi halde bu salon tüm kalan öğrencileri alıp yalnızca
			// bir kısım engelli öğrenciye erişilebilir koltuk verebilir ve geri kalan
			// engelli öğrenci sessizce yerleştirilmemiş olurdu.
			k = min(k, rem-(remDis-d))
			if pick == nil || better(rs, k, pick, pk) {
				pick, pk, pd = rs, k, d
			}
		}
		if pick == nil {
			undo()
			return nil, false
		}
		pick.add(e.ID, pk, pd)
		done = append(done, chunk{pick, pk, pd})
		rem -= pk
		remDis -= pd
	}
	return done, true
}

// better: kullanılan salon önce (paylaşım), sonra büyük pay, sonra küçük salon kimliği.
func better(a *roomState, ak int, b *roomState, bk int) bool {
	if a.used() != b.used() {
		return a.used()
	}
	if ak != bk {
		return ak > bk
	}
	return a.room.ID < b.room.ID
}

// Assign, tek bir slotun sınavlarını salonlara yerleştirir.
//
// Best/First-Fit Decreasing: sınavlar büyükten küçüğe (eşitlikte küçük kimlik),
// laboratuvar sınavları yalnız lab salonlara, diğerleri yalnız klasik salonlara.
// Ardından salon sayısını azaltan bir birleştirme geçişi uygulanır.
func Assign(rooms []Room, exams []Exam, cfg Config) (Plan, error) {
	for _, e := range exams {
		if e.Students < 0 || e.Disabled < 0 || e.Disabled > e.Students {
			return Plan{}, fmt.Errorf("sınav %d: geçersiz öğrenci sayıları (%d, engelli %d)", e.ID, e.Students, e.Disabled)
		}
	}
	states := make([]*roomState, 0, len(rooms))
	for _, r := range rooms {
		states = append(states, &roomState{room: r, cap: EffectiveCapacity(r, cfg)})
	}
	// Büyük salon önce (en az salonla doldurmak için), eşitlikte küçük kimlik.
	sort.Slice(states, func(a, b int) bool {
		if states[a].cap.Active != states[b].cap.Active {
			return states[a].cap.Active > states[b].cap.Active
		}
		return states[a].room.ID < states[b].room.ID
	})
	pool := func(lab bool) []*roomState {
		var out []*roomState
		for _, rs := range states {
			if rs.room.Lab == lab {
				out = append(out, rs)
			}
		}
		return out
	}
	classic, labs := pool(false), pool(true)

	order := append([]Exam(nil), exams...)
	sort.Slice(order, func(a, b int) bool {
		if order[a].Students != order[b].Students {
			return order[a].Students > order[b].Students
		}
		return order[a].ID < order[b].ID
	})

	var plan Plan
	for _, e := range order {
		if e.Students == 0 {
			continue
		}
		cands := classic
		if e.Lab {
			cands = labs
		}
		if _, ok := place(cands, e); !ok {
			reason := "yeterli etkin kapasite yok"
			if e.Disabled > 0 {
				reason = "yeterli etkin kapasite veya erişilebilir koltuk yok"
			}
			plan.Unplaced = append(plan.Unplaced, Unplaced{e.ID, e.Students, e.Disabled, reason})
		}
	}
	consolidate(classic)
	consolidate(labs)

	for _, rs := range states {
		if rs.used() {
			plan.RoomsUsed++
		}
		for _, g := range rs.groups {
			plan.Placements = append(plan.Placements, Placement{ExamID: g.exam, RoomID: rs.room.ID, Count: g.count, Disabled: g.dis})
		}
	}
	sort.Slice(plan.Placements, func(a, b int) bool {
		if plan.Placements[a].ExamID != plan.Placements[b].ExamID {
			return plan.Placements[a].ExamID < plan.Placements[b].ExamID
		}
		return plan.Placements[a].RoomID < plan.Placements[b].RoomID
	})
	sort.Slice(plan.Unplaced, func(a, b int) bool { return plan.Unplaced[a].ExamID < plan.Unplaced[b].ExamID })
	return plan, nil
}

// consolidate: en az dolu salondan başlayarak payları diğer KULLANILAN salonlara
// taşımayı dener; hepsi taşınırsa salon boşalır (salon sayısı azalır). Bir salon
// boşaltılamıyorsa hiçbir şey değişmez. Değişiklik olmayana dek tekrarlanır.
func consolidate(pool []*roomState) {
	for changed := true; changed; {
		changed = false
		used := make([]*roomState, 0, len(pool))
		for _, rs := range pool {
			if rs.used() {
				used = append(used, rs)
			}
		}
		sort.Slice(used, func(a, b int) bool {
			if used[a].sum != used[b].sum {
				return used[a].sum < used[b].sum
			}
			return used[a].room.ID < used[b].room.ID
		})
		for _, src := range used {
			var others []*roomState
			for _, rs := range used {
				if rs != src && rs.used() {
					others = append(others, rs)
				}
			}
			if tryEmpty(src, others) {
				changed = true
				break
			}
		}
	}
}

func tryEmpty(src *roomState, others []*roomState) bool {
	return moveAll(src, append([]group(nil), src.groups...), others)
}

// moveAll: src'deki her payı others'a yerleştirir; biri bile sığmazsa hepsini geri alır.
func moveAll(src *roomState, groups []group, others []*roomState) bool {
	type undo struct {
		exam int64
		cs   []chunk
	}
	var done []undo
	for _, g := range groups {
		cs, ok := place(others, Exam{ID: g.exam, Students: g.count, Disabled: g.dis})
		if !ok {
			for _, u := range done {
				for _, c := range u.cs {
					c.rs.remove(u.exam, c.k, c.d)
				}
			}
			return false
		}
		done = append(done, undo{g.exam, cs})
	}
	for _, g := range groups {
		src.remove(g.exam, g.count, g.dis)
	}
	return true
}
