// Package attendance, sınav günü yoklamasının saf kısmıdır (T-5): hibrit mantıksal
// saat (HLC) ve aynı öğrenci için farklı cihazlardan gelen olayların deterministik
// çakışma çözümü. Kalıcılık internal/store'dadır.
package attendance

import (
	"fmt"
	"sort"
)

// HLC, hibrit mantıksal saat zaman damgası: fiziksel bileşen (ms) + mantıksal sayaç.
// Sıra: önce Wall, sonra Counter. Çevrimdışı cihazların saatleri kaysa da nedensel
// sıra korunur (bir cihaz başka bir olayı gördükten sonra ürettiği olay her zaman
// daha büyük damga alır).
type HLC struct {
	Wall    int64 `json:"wall"`
	Counter int32 `json:"counter"`
}

// Compare: a<b -> -1, a==b -> 0, a>b -> 1.
func (a HLC) Compare(b HLC) int {
	switch {
	case a.Wall < b.Wall:
		return -1
	case a.Wall > b.Wall:
		return 1
	case a.Counter < b.Counter:
		return -1
	case a.Counter > b.Counter:
		return 1
	}
	return 0
}

// Clock, bir cihazın HLC saatidir (Kulkarni vd. 2014). Physical fiziksel saati
// (ms) döner; testlerde enjekte edilir.
type Clock struct {
	Physical func() int64
	last     HLC
}

// Now, yerel olay için damga üretir.
func (c *Clock) Now() HLC {
	pt := c.Physical()
	if pt > c.last.Wall {
		c.last = HLC{Wall: pt}
	} else {
		c.last.Counter++
	}
	return c.last
}

// Observe, başka bir cihazdan gelen damgayı görünce saati ileri alır ve yeni
// damgayı döner (alınan olaydan sonra gelen her yerel olay ondan büyük olur).
func (c *Clock) Observe(remote HLC) HLC {
	pt := c.Physical()
	switch {
	case pt > c.last.Wall && pt > remote.Wall:
		c.last = HLC{Wall: pt}
	case remote.Wall > c.last.Wall:
		c.last = HLC{Wall: remote.Wall, Counter: remote.Counter + 1}
	case c.last.Wall > remote.Wall:
		c.last.Counter++
	default: // eşit duvar saati
		c.last.Counter = max(c.last.Counter, remote.Counter) + 1
	}
	return c.last
}

// Durum ve kaynak değerleri.
const (
	Present = "present"
	Absent  = "absent"
	QR      = "qr"
	Manual  = "manual"
)

// Event, bir cihazın ürettiği yoklama olayı.
type Event struct {
	ID        string `json:"event_id"` // UUIDv7, istemci üretir
	DeviceID  string `json:"device_id"`
	ExamID    int64  `json:"exam_id"`
	StudentID int64  `json:"student_id"`
	Status    string `json:"status"`
	Source    string `json:"source"`
	HLC       HLC    `json:"hlc"`
}

// Validate, olayın biçimini denetler.
func (e Event) Validate() error {
	switch {
	case e.ID == "":
		return fmt.Errorf("event_id zorunlu")
	case e.DeviceID == "":
		return fmt.Errorf("device_id zorunlu")
	case e.ExamID <= 0 || e.StudentID <= 0:
		return fmt.Errorf("exam_id ve student_id pozitif olmalı")
	case e.Status != Present && e.Status != Absent:
		return fmt.Errorf("status present veya absent olmalı")
	case e.Source != QR && e.Source != Manual:
		return fmt.Errorf("source qr veya manual olmalı")
	case e.HLC.Wall < 0 || e.HLC.Counter < 0:
		return fmt.Errorf("hlc negatif olamaz")
	}
	return nil
}

// Kural adları (attendance_conflicts.rule).
const (
	RuleQRPresent = "qr_present"
	RuleHLC       = "hlc"
	RuleDeviceID  = "device_id"
)

func qrPresent(e Event) bool { return e.Source == QR && e.Status == Present }

// Beats, a'nın b'ye karşı kazanıp kazanmadığını ve hangi kuralla kazandığını söyler.
// Kural (ADR-010), tam ve deterministik bir sıradır:
//  1. QR kaynaklı "present" kazanır (öğrenci fiziksel olarak kartını okutmuştur;
//     elle "absent" girişi bunu geçersiz kılamaz);
//  2. aksi halde HLC'si büyük olan (en son bilgi);
//  3. eşitlikte device_id'si sözlük sırasında büyük olan;
//  4. o da eşitse event_id büyük olan (yalnızca sıranın tam olması için).
func Beats(a, b Event) (bool, string) {
	if qa, qb := qrPresent(a), qrPresent(b); qa != qb {
		return qa, RuleQRPresent
	}
	if c := a.HLC.Compare(b.HLC); c != 0 {
		return c > 0, RuleHLC
	}
	if a.DeviceID != b.DeviceID {
		return a.DeviceID > b.DeviceID, RuleDeviceID
	}
	return a.ID > b.ID, RuleDeviceID
}

// Conflict, kazanan ile ondan farklı durum bildiren başka bir cihazın olayı.
type Conflict struct {
	Winner, Loser Event
	Rule          string
}

// Resolution, bir (sınav, öğrenci) için sonuç.
type Resolution struct {
	ExamID, StudentID int64
	Winner            Event
	Conflicts         []Conflict
}

type key struct{ exam, student int64 }

// Resolve, olayları (sınav, öğrenci) bazında çözer. Sonuç olayların SIRASINDAN
// BAĞIMSIZDIR: kazanan, tam bir sıranın en büyüğüdür; çakışmalar kazanana göre
// tanımlanır ve kimliğe göre sıralanır. Aynı event_id ikinci kez gelirse yok sayılır.
//
// Çakışma = kazananla FARKLI durum bildiren ve FARKLI cihazdan gelen olay. Aynı
// cihazın kendi fikrini değiştirmesi (önce absent sonra present) çakışma değildir.
func Resolve(events []Event) []Resolution {
	seen := map[string]bool{}
	groups := map[key][]Event{}
	for _, e := range events {
		if seen[e.ID] {
			continue
		}
		seen[e.ID] = true
		k := key{e.ExamID, e.StudentID}
		groups[k] = append(groups[k], e)
	}
	keys := make([]key, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(a, b int) bool {
		if keys[a].exam != keys[b].exam {
			return keys[a].exam < keys[b].exam
		}
		return keys[a].student < keys[b].student
	})

	out := make([]Resolution, 0, len(keys))
	for _, k := range keys {
		evs := groups[k]
		w := evs[0]
		for _, e := range evs[1:] {
			if win, _ := Beats(e, w); win {
				w = e
			}
		}
		r := Resolution{ExamID: k.exam, StudentID: k.student, Winner: w}
		for _, e := range evs {
			if e.ID == w.ID || e.Status == w.Status || e.DeviceID == w.DeviceID {
				continue
			}
			_, rule := Beats(w, e)
			r.Conflicts = append(r.Conflicts, Conflict{Winner: w, Loser: e, Rule: rule})
		}
		sort.Slice(r.Conflicts, func(a, b int) bool { return r.Conflicts[a].Loser.ID < r.Conflicts[b].Loser.ID })
		out = append(out, r)
	}
	return out
}
