package scheduling

import "testing"

// Açıklama, bağımsız doğrulayıcıyla tutarlıdır: "uygun" denen her slot taşınınca 0 ihlal ve ceza tam
// olarak önceki + Delta; çatışma/günlük sınır/kapasite denen her slot taşınınca doğrulayıcı ihlal bulur.
func TestExplainSlotsAgreesWithValidator(t *testing.T) {
	in, a := roughSchedule(t)
	base := Validate(in, a).Metrics.Penalty
	checked := 0
	for id := range a {
		opts, err := ExplainSlots(in, a, id)
		if err != nil {
			t.Fatal(err)
		}
		if len(opts) != in.Slots() || !opts[a[id]].Current {
			t.Fatalf("sınav %d: %d seçenek, mevcut slot işaretli değil", id, len(opts))
		}
		for _, o := range opts {
			if o.Current {
				continue
			}
			m := make(map[int64]int, len(a))
			for k, v := range a {
				m[k] = v
			}
			m[id] = o.Slot
			rep := Validate(in, m)
			if o.Feasible {
				if len(rep.Violations) != 0 || rep.Metrics.Penalty != base+o.Delta {
					t.Fatalf("sınav %d → %d 'uygun' denmiş ama %d ihlal / ceza %d ≠ %d+%d", id, o.Slot, len(rep.Violations), rep.Metrics.Penalty, base, o.Delta)
				}
			} else if len(rep.Violations) == 0 {
				t.Fatalf("sınav %d → %d 'uygun değil' denmiş ama doğrulayıcı ihlal bulmadı: %+v", id, o.Slot, o)
			}
			if len(o.Conflicts) > 0 && countKind(rep, "conflict") == 0 && countKind(rep, "same_slot_conflict") == 0 && len(rep.Violations) == 0 {
				t.Fatalf("çatışma denmiş ama doğrulayıcı görmedi: %+v", o)
			}
			checked++
		}
		if checked > 600 {
			break
		}
	}
	if _, err := ExplainSlots(in, a, -1); err == nil {
		t.Fatal("bilinmeyen sınav hata vermeli")
	}
}
