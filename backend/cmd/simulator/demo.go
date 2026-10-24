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
	"time"

	"github.com/AllenVB/sinavplan/backend/internal/domain/attendance"
	"github.com/AllenVB/sinavplan/backend/internal/simdata"
)

// apiDo, JSON istek/yanıt yardımcısı (yalnızca demo komutu için).
func apiDo(ctx context.Context, base, token, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequestWithContext(ctx, method, base+path, rd)
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: HTTP %d %.160s", method, path, resp.StatusCode, raw)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// runDemo, "canlı izleme" için gerçek bir gösteri üretir; yalnızca API'yi kullanır:
//  1. yayınlanmış çizelge yoksa üretir ve yayınlar (sekreter);
//  2. en erken oturumun öğrencilerini gerçek oturma planından okur;
//  3. duration boyunca, 6 tabletten (ikisi başta çevrimdışı) yoklama olaylarını akıtır:
//     çoğu "geldi", bir kısmı "gelmedi"; Canlı izleme ekranındaki sayılar ve akış kendiliğinden ilerler.
func runDemo(log *slog.Logger, duration time.Duration) error {
	base, pw := os.Getenv("API_BASE_URL"), os.Getenv("DEMO_PASSWORD")
	if base == "" || pw == "" {
		return fmt.Errorf("API_BASE_URL ve DEMO_PASSWORD zorunlu")
	}
	ctx, cancel := context.WithTimeout(context.Background(), duration+5*time.Minute)
	defer cancel()
	sec, err := login(ctx, base, simdata.EmailSecretary, pw)
	if err != nil {
		return fmt.Errorf("sekreter girişi: %w", err)
	}
	inv, err := login(ctx, base, simdata.EmailInvigilator, pw)
	if err != nil {
		return fmt.Errorf("gözetmen girişi: %w", err)
	}

	type schedule struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	var list struct{ Items []schedule }
	if err := apiDo(ctx, base, sec, http.MethodGet, "/schedules?limit=100", nil, &list); err != nil {
		return err
	}
	var pub int64
	for _, s := range list.Items {
		if s.Status == "published" {
			pub = s.ID
			break
		}
	}
	if pub == 0 {
		log.Info("yayınlanmış çizelge yok; üretiliyor")
		var per struct{ Items []struct{ ID int64 } }
		if err := apiDo(ctx, base, sec, http.MethodGet, "/periods", nil, &per); err != nil || len(per.Items) == 0 {
			return fmt.Errorf("dönem bulunamadı: %v", err)
		}
		var job struct {
			JobID int64 `json:"job_id"`
		}
		if err := apiDo(ctx, base, sec, http.MethodPost, "/schedules", map[string]any{"period_id": per.Items[0].ID, "seed": 42, "iterations": 100000}, &job); err != nil {
			return err
		}
		for i := 0; ; i++ {
			var j struct {
				Status     string `json:"status"`
				ScheduleID int64  `json:"schedule_id"`
				Error      string `json:"error"`
			}
			if err := apiDo(ctx, base, sec, http.MethodGet, fmt.Sprintf("/schedule-jobs/%d", job.JobID), nil, &j); err != nil {
				return err
			}
			if j.Status == "succeeded" {
				pub = j.ScheduleID
				break
			}
			if j.Status == "failed" || i > 150 {
				return fmt.Errorf("çizelge üretilemedi: %s %s", j.Status, j.Error)
			}
			time.Sleep(2 * time.Second)
		}
		if err := apiDo(ctx, base, sec, http.MethodPost, fmt.Sprintf("/schedules/%d/publish", pub), nil, nil); err != nil {
			return err
		}
		log.Info("çizelge yayınlandı", slog.Int64("schedule", pub))
	}

	// en erken oturumun tüm öğrencileri
	type exam struct {
		ExamID  int64     `json:"exam_id"`
		SlotID  int64     `json:"slot_id"`
		Starts  time.Time `json:"starts_at"`
		Student int       `json:"students"`
	}
	var cal struct{ Items []exam }
	if err := apiDo(ctx, base, sec, http.MethodGet, fmt.Sprintf("/schedules/%d/exams?limit=200", pub), nil, &cal); err != nil || len(cal.Items) == 0 {
		return fmt.Errorf("takvim okunamadı: %v", err)
	}
	slot, start := cal.Items[0].SlotID, cal.Items[0].Starts
	for _, e := range cal.Items {
		if e.Starts.Before(start) {
			slot, start = e.SlotID, e.Starts
		}
	}
	type target struct{ exam, student int64 }
	var targets []target
	for _, e := range cal.Items {
		if e.SlotID != slot {
			continue
		}
		var seats struct {
			Items []struct {
				StudentID int64 `json:"student_id"`
			}
		}
		if err := apiDo(ctx, base, sec, http.MethodGet, fmt.Sprintf("/schedules/%d/seating?exam_id=%d&limit=200", pub, e.ExamID), nil, &seats); err != nil {
			return err
		}
		for _, s := range seats.Items {
			targets = append(targets, target{e.ExamID, s.StudentID})
		}
	}
	rng := rand.New(rand.NewPCG(7, 7))
	rng.Shuffle(len(targets), func(i, j int) { targets[i], targets[j] = targets[j], targets[i] })
	log.Info("canlı demo başlıyor", slog.Int("öğrenci", len(targets)), slog.Int64("slot", slot), slog.Duration("süre", duration))

	devs := make([]*device, 6)
	for i := range devs {
		d := &device{id: fmt.Sprintf("demo-tablet-%02d", i+1), offline: i < 2}
		d.clock = attendance.Clock{Physical: func() int64 { return time.Now().UnixMilli() }}
		devs[i] = d
	}
	ticks := max(4, int(duration/time.Second))
	per := max(1, len(targets)/ticks)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	next, sent := 0, 0
	for tick := 0; next < len(targets); tick++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
		if tick == ticks/3 { // ağ geldi: çevrimdışı cihazlar birikenleri toplu gönderir
			for _, d := range devs {
				if d.offline {
					d.offline = false
					for len(d.buffer) > 0 {
						n := min(len(d.buffer), 500)
						if _, err := postSync(ctx, base, inv, d.buffer[:n]); err != nil {
							return err
						}
						sent += n
						d.buffer = d.buffer[n:]
					}
				}
			}
		}
		batches := map[*device][]attendance.Event{}
		for k := 0; k < per && next < len(targets); k++ {
			tg := targets[next]
			next++
			d := devs[rng.IntN(len(devs))]
			status := "present"
			if rng.IntN(100) < 12 {
				status = "absent"
			}
			batches[d] = append(batches[d], newEvent(d, tg.exam, tg.student, status, "manual"))
		}
		for d, evs := range batches {
			if d.offline {
				d.buffer = append(d.buffer, evs...)
				continue
			}
			if _, err := postSync(ctx, base, inv, evs); err != nil {
				return err
			}
			sent += len(evs)
		}
	}
	for _, d := range devs { // kalan çevrimdışı birikimi boşalt
		for len(d.buffer) > 0 {
			n := min(len(d.buffer), 500)
			if _, err := postSync(ctx, base, inv, d.buffer[:n]); err != nil {
				return err
			}
			sent += n
			d.buffer = d.buffer[n:]
		}
	}
	log.Info("canlı demo bitti", slog.Int("gönderilen_olay", sent))
	return nil
}
