package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/AllenVB/sinavplan/backend/internal/domain/attendance"
	"github.com/AllenVB/sinavplan/backend/internal/simdata"
	"github.com/google/uuid"
)

// device, bir gözetmen tableti. Çevrimdışıyken olayları biriktirir.
type device struct {
	id      string
	clock   attendance.Clock
	offline bool
	buffer  []attendance.Event
}

type liveStats struct {
	sent, accepted, duplicates, rejected, conflicts, batches, failedBatches int
}

// runLive, sınav günü yoklamasını API üzerinden üretir:
//   - 6 tablet, ikisi oturumun ilk yarısında çevrimdışı: olayları biriktirip ağ gelince
//     toplu gönderir (offline-first);
//   - bazı paketler bilerek tekrar gönderilir (event_id idempotansı);
//   - bazı öğrenciler iki farklı cihazdan farklı durumla işaretlenir (çakışma).
//
// Veri, aynı tohumla yerelde yeniden üretilir; simülatörün veritabanına erişimi yoktur.
func runLive(log *slog.Logger, seed int64, duration time.Duration) error {
	base := os.Getenv("API_BASE_URL")
	pw := os.Getenv("DEMO_PASSWORD")
	if base == "" || pw == "" {
		return fmt.Errorf("API_BASE_URL ve DEMO_PASSWORD zorunlu")
	}
	ds, err := simdata.Generate(simdata.DefaultConfig(seed))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), duration+30*time.Second)
	defer cancel()

	token, err := login(ctx, base, simdata.EmailInvigilator, pw)
	if err != nil {
		return fmt.Errorf("gözetmen girişi: %w", err)
	}

	// İlk 6 sınavın öğrencileri "bugünün" yoklamasıdır.
	secCourse := map[int64]int64{}
	for _, s := range ds.Sections {
		secCourse[s.ID] = s.CourseID
	}
	examOf := map[int64]int64{}
	for _, e := range ds.Exams {
		examOf[e.CourseID] = e.ID
	}
	type target struct{ exam, student int64 }
	var targets []target
	for _, en := range ds.Enrollments {
		if ex := examOf[secCourse[en.SectionID]]; ex <= 6 {
			targets = append(targets, target{ex, en.StudentID})
		}
	}
	sort.Slice(targets, func(a, b int) bool {
		if targets[a].exam != targets[b].exam {
			return targets[a].exam < targets[b].exam
		}
		return targets[a].student < targets[b].student
	})

	rng := rand.New(rand.NewPCG(uint64(seed), 77))
	devs := make([]*device, 6)
	for i := range devs {
		d := &device{id: fmt.Sprintf("tablet-%02d", i+1), offline: i < 2}
		skew := int64(rng.IntN(4000)) - 2000 // cihaz saatleri ±2 sn kaymış
		d.clock = attendance.Clock{Physical: func() int64 { return time.Now().UnixMilli() + skew }}
		devs[i] = d
	}

	var st liveStats
	send := func(d *device, evs []attendance.Event) {
		res, err := postSync(ctx, base, token, evs)
		st.batches++
		if err != nil {
			st.failedBatches++
			log.Warn("paket gönderilemedi, cihaz yeniden deneyecek", slog.String("device", d.id), slog.String("err", err.Error()))
			d.buffer = append(d.buffer, evs...)
			return
		}
		st.sent += len(evs)
		st.accepted += res.Accepted
		st.duplicates += res.Duplicates
		st.rejected += len(res.Rejected)
		st.conflicts += res.NewConflicts
	}

	ticks := int(duration / time.Second)
	if ticks < 4 {
		ticks = 4
	}
	perTick := max(1, len(targets)/ticks)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	next := 0
	var lastBatch []attendance.Event
	for tick := 0; tick < ticks && next < len(targets); tick++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
		if tick == ticks/2 { // ağ geri geldi: çevrimdışı cihazlar biriktirdiklerini toplu gönderir
			for _, d := range devs {
				if d.offline {
					d.offline = false
					log.Info("cihaz çevrimiçi oldu, birikmiş olaylar gönderiliyor", slog.String("device", d.id), slog.Int("olay", len(d.buffer)))
					for len(d.buffer) > 0 {
						n := min(len(d.buffer), 500)
						batch := d.buffer[:n]
						d.buffer = d.buffer[n:]
						send(d, batch)
					}
				}
			}
		}
		batches := map[*device][]attendance.Event{}
		for k := 0; k < perTick && next < len(targets); k++ {
			tg := targets[next]
			next++
			d := devs[rng.IntN(len(devs))]
			status, source := attendance.Present, attendance.Manual
			if rng.IntN(10) == 0 {
				status = attendance.Absent
			}
			if rng.IntN(3) == 0 {
				source = attendance.QR
			}
			batches[d] = append(batches[d], newEvent(d, tg.exam, tg.student, status, source))
			// %5: başka bir cihaz aynı öğrenciyi farklı durumla işaretler (çakışma).
			if rng.IntN(20) == 0 {
				o := devs[(indexOf(devs, d)+1+rng.IntN(len(devs)-1))%len(devs)]
				flip := attendance.Absent
				if status == attendance.Absent {
					flip = attendance.Present
				}
				batches[o] = append(batches[o], newEvent(o, tg.exam, tg.student, flip, attendance.Manual))
			}
		}
		for _, d := range devs {
			evs := batches[d]
			if len(evs) == 0 {
				continue
			}
			if d.offline {
				d.buffer = append(d.buffer, evs...)
				continue
			}
			send(d, evs)
			lastBatch = evs
		}
		// %25: son paket bilerek tekrar gönderilir (ağ zaman aşımı sonrası yeniden deneme).
		if len(lastBatch) > 0 && rng.IntN(4) == 0 {
			send(devs[2], lastBatch)
		}
	}
	for _, d := range devs { // kalan tamponları boşalt
		if len(d.buffer) > 0 {
			send(d, d.buffer)
			d.buffer = nil
		}
	}
	log.Info("live tamamlandı",
		slog.Int("gönderilen", st.sent), slog.Int("kabul", st.accepted), slog.Int("tekrar_yok_sayıldı", st.duplicates),
		slog.Int("reddedilen", st.rejected), slog.Int("yeni_çakışma", st.conflicts),
		slog.Int("paket", st.batches), slog.Int("başarısız_paket", st.failedBatches))
	if st.rejected > 0 {
		return fmt.Errorf("%d olay reddedildi", st.rejected)
	}
	return nil
}

func indexOf(ds []*device, d *device) int {
	for i, x := range ds {
		if x == d {
			return i
		}
	}
	return 0
}

func newEvent(d *device, exam, student int64, status, source string) attendance.Event {
	id, _ := uuid.NewV7()
	return attendance.Event{ID: id.String(), DeviceID: d.id, ExamID: exam, StudentID: student,
		Status: status, Source: source, HLC: d.clock.Now()}
}

func login(ctx context.Context, base, email, pw string) (string, error) {
	b, _ := json.Marshal(map[string]string{"email": email, "password": pw})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, base+"/auth/login", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("durum %d", resp.StatusCode)
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.AccessToken, nil
}

type syncResult struct {
	Accepted     int                       `json:"accepted"`
	Duplicates   int                       `json:"duplicates"`
	Rejected     []struct{ Reason string } `json:"rejected"`
	NewConflicts int                       `json:"new_conflicts"`
}

func postSync(ctx context.Context, base, token string, evs []attendance.Event) (syncResult, error) {
	b, _ := json.Marshal(map[string]any{"events": evs})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, base+"/attendance/sync", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return syncResult{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return syncResult{}, fmt.Errorf("durum %d: %s", resp.StatusCode, body)
	}
	var r syncResult
	return r, json.Unmarshal(body, &r)
}
