package store

import (
	"fmt"
	"sort"
	"time"
)

// LiveAlert, sınav günü izleme ekranındaki "Dikkat gerektirenler" maddesidir (Faz 22 C2).
type LiveAlert struct {
	Level    string `json:"level"` // err | warn | info
	Kind     string `json:"kind"`
	RoomID   int64  `json:"room_id"`
	RoomCode string `json:"room_code"`
	Message  string `json:"message"`
}

// Eşikler: sınav başladıktan sonra yoklamanın oturması için tanınan süre, düşük katılım oranı,
// cihazın "sessiz" sayılacağı süre ve doluluk uyarısı.
const (
	alertGrace       = 15 * time.Minute
	alertSilentAfter = 10 * time.Minute
)

var alertRank = map[string]int{"err": 0, "warn": 1, "info": 2}

// LiveAlerts, canlı özetten kural tabanlı uyarılar üretir. Saf fonksiyondur (veritabanı yok); kurallar:
//   - no_invigilator (err): öğrencisi olan salonda gözetmen yok
//   - understaffed (warn): 50'den fazla öğrencili salonda 2'den az gözetmen
//   - attendance_not_started (warn): sınav başlayalı 15 dk oldu, salonda hiç yoklama kaydı yok
//   - low_turnout (warn): 15 dk sonra gelenler oturanların yarısından az
//   - device_silent (info): sınav sürerken son yoklama olayı 10 dk'dan eski ve işaretsiz öğrenci var
//     (tablet çevrimdışı kalmış olabilir; olaylar bağlantı gelince senkronlanır)
//   - unmarked_after_end (warn): oturum bitti, işaretsiz öğrenci kaldı — oturum başına TEK özet uyarı
//     (salon başına uyarı, her salonda birkaç işaretsiz kalan bitmiş oturumlarda listeyi boğuyordu)
//   - over_capacity (err): salonda aktif koltuk sayısından fazla öğrenci (T-2 bunu engeller; görünürse veri
//     bütünlüğü sorunu vardır). "%95+ dolu" uyarısı denendi ve kaldırıldı: T-2 salon sayısını azaltmak için
//     salonları bilerek sıkı doldurur, salonların çoğu uyarı verip eyleme dönüşmeyen gürültü üretiyordu.
//
// Sıra: önem (err → warn → info), sonra salon kodu.
func LiveAlerts(ov LiveOverview, now time.Time) []LiveAlert {
	out := []LiveAlert{}
	add := func(level, kind string, r LiveRoom, msg string) {
		out = append(out, LiveAlert{Level: level, Kind: kind, RoomID: r.RoomID, RoomCode: r.RoomCode, Message: msg})
	}
	started := ov.Slot.Mode == "active" && now.Sub(ov.Slot.StartsAt) >= alertGrace
	unmarkedRooms, unmarked := 0, 0
	for _, r := range ov.Rooms {
		if r.Seated == 0 {
			continue
		}
		switch n := len(r.Invigilators); {
		case n == 0:
			add("err", "no_invigilator", r, fmt.Sprintf("%d öğrencili salonda gözetmen yok", r.Seated))
		case r.Seated > 50 && n < 2:
			add("warn", "understaffed", r, fmt.Sprintf("%d öğrenci, %d gözetmen (en az 2 gerekli)", r.Seated, n))
		}
		marked := r.Present + r.Absent
		switch {
		case started && marked == 0:
			add("warn", "attendance_not_started", r, fmt.Sprintf("sınav başlayalı %d dk oldu, yoklama başlamadı", int(now.Sub(ov.Slot.StartsAt).Minutes())))
		case started && r.Present*2 < r.Seated:
			add("warn", "low_turnout", r, fmt.Sprintf("katılım düşük: %d / %d geldi", r.Present, r.Seated))
		}
		if ov.Slot.Mode == "active" && marked > 0 && r.Unmarked > 0 && r.LastEventAt != nil && now.Sub(*r.LastEventAt) >= alertSilentAfter {
			add("info", "device_silent", r, fmt.Sprintf("son yoklama olayı %d dk önce, %d öğrenci işaretsiz — tablet çevrimdışı olabilir", int(now.Sub(*r.LastEventAt).Minutes()), r.Unmarked))
		}
		if ov.Slot.Mode == "past" && r.Unmarked > 0 {
			unmarkedRooms++
			unmarked += r.Unmarked
		}
		if r.Capacity > 0 && r.Seated > r.Capacity {
			add("err", "over_capacity", r, fmt.Sprintf("kapasite aşıldı: %d öğrenci, %d koltuk", r.Seated, r.Capacity))
		}
	}
	if unmarkedRooms > 0 {
		out = append(out, LiveAlert{Level: "warn", Kind: "unmarked_after_end",
			Message: fmt.Sprintf("oturum bitti: %d salonda toplam %d öğrenci işaretsiz kaldı (yoklama tamamlanmalı)", unmarkedRooms, unmarked)})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if alertRank[out[i].Level] != alertRank[out[j].Level] {
			return alertRank[out[i].Level] < alertRank[out[j].Level]
		}
		return out[i].RoomCode < out[j].RoomCode
	})
	return out
}
