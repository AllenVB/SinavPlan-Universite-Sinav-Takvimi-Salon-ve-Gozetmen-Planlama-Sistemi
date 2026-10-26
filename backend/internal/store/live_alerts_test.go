package store

import (
	"testing"
	"time"
)

func TestLiveAlertRules(t *testing.T) {
	t0 := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
	inv := func(n int) []LiveInvigilator { return make([]LiveInvigilator, n) }
	at := func(d time.Duration) *time.Time { x := t0.Add(d); return &x }
	rooms := []LiveRoom{
		{RoomCode: "A", Seated: 30, Capacity: 40, Present: 25, Absent: 1, Unmarked: 4, Invigilators: inv(1), LastEventAt: at(18 * time.Minute)}, // sağlıklı
		{RoomCode: "B", Seated: 20, Capacity: 40, Invigilators: inv(0)},                                                                         // gözetmensiz + yoklama yok
		{RoomCode: "C", Seated: 60, Capacity: 62, Present: 50, Absent: 2, Unmarked: 8, Invigilators: inv(1), LastEventAt: at(19 * time.Minute)}, // eksik gözetmen (%96 dolu: uyarı YOK)
		{RoomCode: "D", Seated: 41, Capacity: 40, Present: 10, Absent: 0, Unmarked: 31, Invigilators: inv(2), LastEventAt: at(2 * time.Minute)}, // düşük katılım + sessiz cihaz + kapasite aşımı
		{RoomCode: "E", Seated: 0, Capacity: 40}, // boş: uyarı yok
	}
	ov := LiveOverview{Slot: LiveSlot{Mode: "active", StartsAt: t0}, Rooms: rooms}
	got := map[string]string{}
	for _, a := range LiveAlerts(ov, t0.Add(20*time.Minute)) {
		got[a.RoomCode+":"+a.Kind] = a.Level
	}
	want := map[string]string{
		"B:no_invigilator": "err", "B:attendance_not_started": "warn",
		"C:understaffed": "warn",
		"D:low_turnout":  "warn", "D:device_silent": "info", "D:over_capacity": "err",
	}
	for k, lv := range want {
		if got[k] != lv {
			t.Errorf("%s: %q bekleniyordu, %q geldi", k, lv, got[k])
		}
	}
	if len(got) != len(want) {
		t.Errorf("fazla/eksik uyarı: %v", got)
	}

	// Sıra: önce err, sonra warn, sonra info
	al := LiveAlerts(ov, t0.Add(20*time.Minute))
	for i := 1; i < len(al); i++ {
		if alertRank[al[i-1].Level] > alertRank[al[i].Level] {
			t.Fatalf("sıralama bozuk: %v sonra %v", al[i-1], al[i])
		}
	}

	// 15 dk dolmadan katılım/yoklama uyarısı yok
	for _, a := range LiveAlerts(ov, t0.Add(10*time.Minute)) {
		if a.Kind == "low_turnout" || a.Kind == "attendance_not_started" {
			t.Fatalf("erken uyarı: %v", a)
		}
	}
	// oturum bitti: işaretsizler uyarılır, süren-oturum kuralları susar
	ov.Slot.Mode = "past"
	past := map[string]bool{}
	for _, a := range LiveAlerts(ov, t0.Add(3*time.Hour)) {
		past[a.RoomCode+":"+a.Kind] = true
		if a.Kind == "low_turnout" || a.Kind == "device_silent" || a.Kind == "attendance_not_started" {
			t.Fatalf("bitmiş oturumda süren-oturum uyarısı: %v", a)
		}
	}
	n := 0
	for k := range past {
		if k == ":unmarked_after_end" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("bitmiş oturumda tek özet işaretsiz uyarısı bekleniyordu: %v", past)
	}
	for _, a := range LiveAlerts(ov, t0.Add(3*time.Hour)) {
		if a.Kind == "unmarked_after_end" && a.Message != "oturum bitti: 3 salonda toplam 43 öğrenci işaretsiz kaldı (yoklama tamamlanmalı)" {
			t.Fatalf("özet mesajı: %q", a.Message)
		}
	}
}
