package attendance

import (
	"math/rand/v2"
	"reflect"
	"testing"
)

func ev(id, dev string, student int64, status, source string, wall int64, ctr int32) Event {
	return Event{ID: id, DeviceID: dev, ExamID: 1, StudentID: student, Status: status, Source: source, HLC: HLC{wall, ctr}}
}

func TestQRPresentWinsOverLaterManualAbsent(t *testing.T) {
	qr := ev("a", "tablet-1", 7, Present, QR, 1000, 0)
	man := ev("b", "tablet-2", 7, Absent, Manual, 9000, 0) // daha sonra ama elle
	r := Resolve([]Event{man, qr})
	if r[0].Winner.ID != "a" || len(r[0].Conflicts) != 1 || r[0].Conflicts[0].Rule != RuleQRPresent {
		t.Fatalf("QR present kazanmalı, kural qr_present: %+v", r)
	}
}

func TestLargerHLCWinsOtherwise(t *testing.T) {
	r := Resolve([]Event{ev("a", "t1", 7, Present, Manual, 1000, 5), ev("b", "t2", 7, Absent, Manual, 1000, 6)})
	if r[0].Winner.ID != "b" || r[0].Conflicts[0].Rule != RuleHLC {
		t.Fatalf("büyük HLC (sayaç) kazanmalı: %+v", r)
	}
}

func TestDeviceIDBreaksTies(t *testing.T) {
	r := Resolve([]Event{ev("a", "tablet-A", 7, Present, Manual, 1000, 0), ev("b", "tablet-B", 7, Absent, Manual, 1000, 0)})
	if r[0].Winner.DeviceID != "tablet-B" || r[0].Conflicts[0].Rule != RuleDeviceID {
		t.Fatalf("eşitlikte büyük device_id kazanmalı: %+v", r)
	}
}

func TestSameStatusOrSameDeviceIsNotAConflict(t *testing.T) {
	r := Resolve([]Event{
		ev("a", "t1", 7, Absent, Manual, 1000, 0),
		ev("b", "t1", 7, Present, Manual, 2000, 0), // aynı cihaz fikir değiştirdi
		ev("c", "t2", 7, Present, Manual, 1500, 0), // farklı cihaz, aynı durum
	})
	if r[0].Winner.ID != "b" || len(r[0].Conflicts) != 0 {
		t.Fatalf("çakışma olmamalı: %+v", r)
	}
}

func TestDuplicateEventIDIgnored(t *testing.T) {
	a := ev("a", "t1", 7, Present, Manual, 1000, 0)
	if r := Resolve([]Event{a, a, a}); len(r) != 1 || len(r[0].Conflicts) != 0 {
		t.Fatalf("tekrar eden olay tek sayılmalı: %+v", r)
	}
}

// PERMÜTASYON TESTİ: olayların gelme sırası sonucu değiştirmez.
func TestResolutionIsIndependentOfOrder(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 5))
	var events []Event
	devices := []string{"t-a", "t-b", "t-c", "t-d"}
	for i := 0; i < 400; i++ {
		st := Present
		if rng.IntN(3) == 0 {
			st = Absent
		}
		src := Manual
		if rng.IntN(4) == 0 {
			src = QR
		}
		events = append(events, Event{
			ID: string(rune('A'+i%26)) + string(rune('a'+i/26)), DeviceID: devices[rng.IntN(4)],
			ExamID: int64(1 + rng.IntN(3)), StudentID: int64(1 + rng.IntN(25)), Status: st, Source: src,
			HLC: HLC{Wall: int64(1000 + rng.IntN(20)), Counter: int32(rng.IntN(3))}, // bilerek çok eşitlik
		})
	}
	// Tekrarlar da ekle.
	events = append(events, events[:40]...)
	want := Resolve(events)
	for p := 0; p < 50; p++ {
		perm := append([]Event(nil), events...)
		rng.Shuffle(len(perm), func(i, j int) { perm[i], perm[j] = perm[j], perm[i] })
		if got := Resolve(perm); !reflect.DeepEqual(got, want) {
			t.Fatalf("permütasyon %d farklı sonuç verdi", p)
		}
	}
}

// Beats tam bir sıra: antisimetrik ve geçişli (permütasyon bağımsızlığının temeli).
func TestBeatsIsATotalOrder(t *testing.T) {
	rng := rand.New(rand.NewPCG(9, 9))
	mk := func(i int) Event {
		src := []string{QR, Manual}[rng.IntN(2)]
		st := []string{Present, Absent}[rng.IntN(2)]
		return Event{ID: string(rune('a' + i)), DeviceID: []string{"x", "y"}[rng.IntN(2)], ExamID: 1, StudentID: 1,
			Status: st, Source: src, HLC: HLC{int64(rng.IntN(3)), int32(rng.IntN(2))}}
	}
	var es []Event
	for i := 0; i < 20; i++ {
		es = append(es, mk(i))
	}
	for _, a := range es {
		for _, b := range es {
			if a.ID == b.ID {
				continue
			}
			ab, _ := Beats(a, b)
			ba, _ := Beats(b, a)
			if ab == ba {
				t.Fatalf("antisimetri bozuk: %+v / %+v", a, b)
			}
			for _, c := range es {
				bc, _ := Beats(b, c)
				ac, _ := Beats(a, c)
				if ab && bc && c.ID != a.ID && !ac {
					t.Fatalf("geçişlilik bozuk: %s>%s>%s ama %s<=%s", a.ID, b.ID, c.ID, a.ID, c.ID)
				}
			}
		}
	}
}

func TestHLCClockMonotonicAndCausal(t *testing.T) {
	now := int64(1000)
	c := &Clock{Physical: func() int64 { return now }}
	a := c.Now()
	b := c.Now() // aynı ms -> sayaç artar
	if b.Compare(a) <= 0 || b.Wall != 1000 || b.Counter != 1 {
		t.Fatalf("aynı ms içinde monoton olmalı: %v %v", a, b)
	}
	now = 900 // saat geri kaydı (çevrimdışı cihaz)
	d := c.Now()
	if d.Compare(b) <= 0 {
		t.Fatalf("saat geri kaysa da damga büyümeli: %v -> %v", b, d)
	}
	// Uzak cihazın ilerideki damgası gözlenince sonraki yerel olay ondan büyük olur.
	remote := HLC{Wall: 5000, Counter: 3}
	o := c.Observe(remote)
	if o.Compare(remote) <= 0 {
		t.Fatalf("gözlem sonrası damga uzak damgadan büyük olmalı: %v", o)
	}
	if n := c.Now(); n.Compare(o) <= 0 {
		t.Fatalf("monotonluk bozuldu: %v -> %v", o, n)
	}
	// Eşit duvar saati dalları.
	c2 := &Clock{Physical: func() int64 { return 10 }}
	c2.Now()
	if x := c2.Observe(HLC{Wall: 10, Counter: 7}); x != (HLC{10, 8}) {
		t.Fatalf("eşit duvarda sayaç max+1 olmalı: %v", x)
	}
	if x := c2.Observe(HLC{Wall: 5, Counter: 0}); x != (HLC{10, 9}) {
		t.Fatalf("geride kalan uzak damga sayacı artırmalı: %v", x)
	}
	c3 := &Clock{Physical: func() int64 { return 100 }}
	if x := c3.Observe(HLC{Wall: 50}); x != (HLC{100, 0}) {
		t.Fatalf("fiziksel saat öndeyse onu almalı: %v", x)
	}
}

func TestEventValidate(t *testing.T) {
	ok := ev("a", "t", 1, Present, QR, 1, 0)
	if ok.Validate() != nil {
		t.Fatal("geçerli olay reddedildi")
	}
	bad := []Event{
		{DeviceID: "t", ExamID: 1, StudentID: 1, Status: Present, Source: QR},
		{ID: "a", ExamID: 1, StudentID: 1, Status: Present, Source: QR},
		{ID: "a", DeviceID: "t", StudentID: 1, Status: Present, Source: QR},
		{ID: "a", DeviceID: "t", ExamID: 1, StudentID: 1, Status: "late", Source: QR},
		{ID: "a", DeviceID: "t", ExamID: 1, StudentID: 1, Status: Present, Source: "nfc"},
		{ID: "a", DeviceID: "t", ExamID: 1, StudentID: 1, Status: Present, Source: QR, HLC: HLC{-1, 0}},
	}
	for i, e := range bad {
		if e.Validate() == nil {
			t.Errorf("geçersiz olay %d kabul edildi", i)
		}
	}
}
