package invigilation

import (
	"fmt"
	"sort"
)

// Violation, gözetmen planı ihlali.
type Violation struct {
	Kind          string `json:"kind"` // unknown_duty | unknown_invigilator | duplicate | own_exam | excused | double_slot | consecutive_session | understaffed
	DutyID        int64  `json:"duty_id,omitempty"`
	InvigilatorID int64  `json:"invigilator_id,omitempty"`
	Detail        string `json:"detail"`
}

// Validate, planı Assign/Reassign'dan BAĞIMSIZ olarak denetler.
func Validate(in Input, p Plan) []Violation {
	var out []Violation
	add := func(k string, d, v int64, f string, a ...any) {
		out = append(out, Violation{k, d, v, fmt.Sprintf(f, a...)})
	}
	duty := map[int64]Duty{}
	for _, d := range in.Duties {
		duty[d.ID] = d
	}
	inv := map[int64]Invigilator{}
	for _, v := range in.Invigilators {
		inv[v.ID] = v
	}
	staffed := map[int64]int{}
	seen := map[[2]int64]bool{}
	bySlot := map[int64]map[int][]int64{} // gözetmen -> slot -> görevler
	for _, a := range p.Assignments {
		d, dok := duty[a.DutyID]
		v, vok := inv[a.InvigilatorID]
		if !dok {
			add("unknown_duty", a.DutyID, a.InvigilatorID, "girdide olmayan görev")
			continue
		}
		if !vok {
			add("unknown_invigilator", a.DutyID, a.InvigilatorID, "girdide olmayan gözetmen")
			continue
		}
		k := [2]int64{a.DutyID, a.InvigilatorID}
		if seen[k] {
			add("duplicate", a.DutyID, a.InvigilatorID, "aynı gözetmen aynı göreve iki kez")
			continue
		}
		seen[k] = true
		staffed[a.DutyID]++
		for _, e := range d.Exams {
			for _, own := range v.OwnExams {
				if e == own {
					add("own_exam", a.DutyID, a.InvigilatorID, "kendi dersinin sınavı (%d) bu salonda", e)
				}
			}
		}
		for _, s := range v.Unavailable {
			if s == d.Slot {
				add("excused", a.DutyID, a.InvigilatorID, "mazeretli olduğu slot %d", s)
			}
		}
		if bySlot[v.ID] == nil {
			bySlot[v.ID] = map[int][]int64{}
		}
		bySlot[v.ID][d.Slot] = append(bySlot[v.ID][d.Slot], d.ID)
	}
	ids := make([]int64, 0, len(bySlot))
	for id := range bySlot {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(a, b int) bool { return ids[a] < ids[b] })
	for _, id := range ids {
		slots := bySlot[id]
		for s, ds := range slots {
			if len(ds) > 1 {
				add("double_slot", ds[0], id, "slot %d'de %d görev", s, len(ds))
			}
			if in.SessionsPerDay > 0 && s%in.SessionsPerDay < in.SessionsPerDay-1 {
				if next, ok := slots[s+1]; ok {
					add("consecutive_session", next[0], id, "aynı gün ardışık oturumlar %d ve %d", s, s+1)
				}
			}
		}
	}
	for _, d := range in.Duties {
		if staffed[d.ID] < d.Required() {
			add("understaffed", d.ID, 0, "%d gözetmen, en az %d gerekli (%d öğrenci)", staffed[d.ID], d.Required(), d.Students)
		}
	}
	return out
}

// Metrics, adalet ölçütleri.
type Metrics struct {
	Invigilators int     `json:"invigilators"`
	Assignments  int     `json:"assignments"`
	Min          int     `json:"min"`
	Max          int     `json:"max"`
	Spread       int     `json:"spread"` // max - min (amaç: en aza)
	Mean         float64 `json:"mean"`
	// IdealSpread: kısıtlar yok sayılırsa ulaşılabilecek en iyi fark (0 veya 1).
	IdealSpread int `json:"ideal_spread"`
}

// Fairness, planın yük dağılımını hesaplar; hiç görev almayan gözetmenler de sayılır.
func Fairness(in Input, p Plan) Metrics {
	load := map[int64]int{}
	for _, v := range in.Invigilators {
		load[v.ID] = 0
	}
	for _, a := range p.Assignments {
		if _, ok := load[a.InvigilatorID]; ok {
			load[a.InvigilatorID]++
		}
	}
	m := Metrics{Invigilators: len(load)}
	first := true
	for _, n := range load {
		m.Assignments += n
		if first || n < m.Min {
			m.Min = n
		}
		if first || n > m.Max {
			m.Max = n
		}
		first = false
	}
	m.Spread = m.Max - m.Min
	if m.Invigilators > 0 {
		m.Mean = float64(m.Assignments) / float64(m.Invigilators)
		if m.Assignments%m.Invigilators != 0 {
			m.IdealSpread = 1
		}
	}
	return m
}
