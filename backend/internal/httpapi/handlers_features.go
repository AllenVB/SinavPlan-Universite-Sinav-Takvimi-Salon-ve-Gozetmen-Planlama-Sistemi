package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/AllenVB/sinavplan/backend/internal/export"
	"github.com/AllenVB/sinavplan/backend/internal/planning"
	"github.com/AllenVB/sinavplan/backend/internal/store"
	"github.com/AllenVB/sinavplan/backend/internal/store/sqlc"
)

// --- Dışa aktarma (X-2) ve iCal (X-3) --------------------------------------------------

const (
	mimeXLSX = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	mimeICS  = "text/calendar; charset=utf-8"
	mimeCSV  = "text/csv; charset=utf-8"
)

func attachment(w http.ResponseWriter, mime, name string) {
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
}

var istanbul = func() *time.Location {
	l, err := time.LoadLocation("Europe/Istanbul")
	if err != nil {
		return time.FixedZone("UTC+3", 3*3600)
	}
	return l
}()

func allExams(r *http.Request, s *Server, id int64) ([]store.CalendarExam, error) {
	var out []store.CalendarExam
	var after int64
	for {
		rows, err := store.ScheduleExams(r.Context(), s.pool, id, after, 501)
		if err != nil {
			return nil, err
		}
		n := min(len(rows), 500)
		out = append(out, rows[:n]...)
		if len(rows) <= 500 {
			return out, nil
		}
		after = rows[n-1].ExamID
	}
}

func allDuties(r *http.Request, s *Server, schedule, user int64) ([]store.Duty, error) {
	var out []store.Duty
	var after int64
	for {
		rows, err := store.Duties(r.Context(), s.pool, schedule, user, 0, after, 501)
		if err != nil {
			return nil, err
		}
		n := min(len(rows), 500)
		out = append(out, rows[:n]...)
		if len(rows) <= 500 {
			return out, nil
		}
		after = rows[n-1].ID
	}
}

// exportSchedule: GET /schedules/{id}/export?format=xlsx|csv
func (s *Server) exportSchedule(w http.ResponseWriter, r *http.Request) error {
	id, err := pathInt64(r, "id")
	if err != nil {
		return err
	}
	sc, err := s.visibleSchedule(r, id)
	if err != nil {
		return err
	}
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "xlsx"
	}
	if format != "xlsx" && format != "csv" {
		return Validation("format xlsx veya csv olmalı", map[string]string{"format": "geçersiz"})
	}
	exams, err := allExams(r, s, id)
	if err != nil {
		return err
	}
	cal := [][]any{{"Tarih", "Başlangıç", "Bitiş", "Gün", "Oturum", "Ders kodu", "Ders adı", "Öğrenci", "Salon sayısı", "Tür"}}
	for _, e := range exams {
		tur := "Klasik"
		if e.RequiresLab {
			tur = "Laboratuvar"
		}
		st := e.StartsAt.In(istanbul)
		cal = append(cal, []any{st.Format("02.01.2006"), st.Format("15:04"), e.EndsAt.In(istanbul).Format("15:04"), int(e.Day) + 1, int(e.Session) + 1,
			e.CourseCode, e.CourseName, e.Students, e.Rooms, tur})
	}
	name := fmt.Sprintf("sinav-takvimi-v%d", sc.Version)
	if format == "csv" {
		attachment(w, mimeCSV, name+".csv")
		return export.WriteCSV(w, cal)
	}
	duties, err := allDuties(r, s, id, 0)
	if err != nil {
		return err
	}
	gz := [][]any{{"Gözetmen", "Tarih", "Saat", "Bina", "Salon", "Ders kodu"}}
	for _, d := range duties {
		st := d.StartsAt.In(istanbul)
		gz = append(gz, []any{d.FullName, st.Format("02.01.2006"), st.Format("15:04"), d.Building, d.RoomCode, d.CourseCode})
	}
	attachment(w, mimeXLSX, name+".xlsx")
	return export.WriteXLSX(w, []export.Sheet{{Name: "Takvim", Rows: cal}, {Name: "Gözetmenler", Rows: gz}})
}

// scheduleICS: GET /schedules/{id}/calendar.ics — tüm sınavlar.
func (s *Server) scheduleICS(w http.ResponseWriter, r *http.Request) error {
	id, err := pathInt64(r, "id")
	if err != nil {
		return err
	}
	sc, err := s.visibleSchedule(r, id)
	if err != nil {
		return err
	}
	exams, err := allExams(r, s, id)
	if err != nil {
		return err
	}
	ev := make([]export.Event, 0, len(exams))
	for _, e := range exams {
		ev = append(ev, export.Event{UID: fmt.Sprintf("exam-%d-s%d@sinavplan", e.ExamID, id), Start: e.StartsAt, End: e.EndsAt,
			Summary: e.CourseCode + " " + e.CourseName + " sınavı", Desc: fmt.Sprintf("%d öğrenci, %d salon", e.Students, e.Rooms)})
	}
	attachment(w, mimeICS, fmt.Sprintf("sinav-takvimi-v%d.ics", sc.Version))
	return export.WriteICS(w, "Sınav takvimi", ev, time.Now())
}

// myExamsICS: GET /me/exams.ics — öğrencinin kendi sınavları (salon ve koltukla).
func (s *Server) myExamsICS(w http.ResponseWriter, r *http.Request) error {
	p := mustPrincipal(r)
	if p.StudentID == 0 {
		return Forbidden("bu hesap bir öğrenci kaydına bağlı değil")
	}
	rows, err := sqlc.New(s.pool).ListStudentExamSchedule(r.Context(), p.StudentID)
	if err != nil {
		return err
	}
	ev := make([]export.Event, 0, len(rows))
	for _, x := range rows {
		loc, desc := "", ""
		if x.RoomCode.Valid {
			loc = x.RoomCode.String + " (" + x.Building.String + ")"
			desc = fmt.Sprintf("Koltuk %c-%d", 'A'+int(x.RowNo.Int16)%26, x.ColNo.Int16+1)
		}
		ev = append(ev, export.Event{UID: fmt.Sprintf("exam-%d-st%d@sinavplan", x.ExamID, p.StudentID), Start: x.StartsAt.Time, End: x.EndsAt.Time,
			Summary: x.CourseCode + " " + x.CourseName + " sınavı", Location: loc, Desc: desc})
	}
	attachment(w, mimeICS, "sinavlarim.ics")
	return export.WriteICS(w, "Sınavlarım", ev, time.Now())
}

// myDutiesICS: GET /me/duties.ics — gözetmenin görevleri (her dönemin yayınlanmış çizelgesinden).
func (s *Server) myDutiesICS(w http.ResponseWriter, r *http.Request) error {
	var duties []store.Duty
	var after int64
	for {
		rows, err := store.PublishedDuties(r.Context(), s.pool, mustPrincipal(r).UserID, after, 501)
		if err != nil {
			return err
		}
		n := min(len(rows), 500)
		duties = append(duties, rows[:n]...)
		if len(rows) <= 500 {
			break
		}
		after = rows[n-1].ID
	}
	// görev bitişi için sınav süresini çizelge takviminden alırız
	end := map[[2]int64]time.Time{}
	seen := map[int64]bool{}
	for _, d := range duties {
		if seen[d.ScheduleID] {
			continue
		}
		seen[d.ScheduleID] = true
		exams, err := allExams(r, s, d.ScheduleID)
		if err != nil {
			return err
		}
		for _, e := range exams {
			end[[2]int64{d.ScheduleID, e.ExamID}] = e.EndsAt
		}
	}
	ev := make([]export.Event, 0, len(duties))
	for _, d := range duties {
		e := end[[2]int64{d.ScheduleID, d.ExamID}]
		if e.IsZero() {
			e = d.StartsAt.Add(2 * time.Hour)
		}
		ev = append(ev, export.Event{UID: fmt.Sprintf("duty-%d@sinavplan", d.ID), Start: d.StartsAt, End: e,
			Summary: "Gözetmenlik: " + d.CourseCode, Location: d.RoomCode + " (" + d.Building + ")"})
	}
	attachment(w, mimeICS, "gorevlerim.ics")
	return export.WriteICS(w, "Gözetmenlik görevlerim", ev, time.Now())
}

// --- Etkinlik akışı (X-6) -------------------------------------------------------------

func (s *Server) activity(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	f := store.ActivityFilter{Limit: 20, ActionPrefix: q.Get("action")}
	if len(f.ActionPrefix) > 40 {
		return Validation("action en çok 40 karakter", map[string]string{"action": "çok uzun"})
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			return Validation("limit 1..200 olmalı", map[string]string{"limit": "geçersiz"})
		}
		f.Limit = n
	}
	for name, dst := range map[string]*int64{"actor": &f.ActorID, "after_id": &f.After, "before_id": &f.Before} {
		if v := q.Get(name); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 0 {
				return Validation(name+" pozitif tam sayı olmalı", map[string]string{name: "geçersiz"})
			}
			*dst = n
		}
	}
	rows, latest, err := store.ActivityPage(r.Context(), s.pool, f)
	if errors.Is(err, store.ErrBadFilter) {
		return Validation(err.Error(), map[string]string{"after_id": "before_id ile birlikte verilemez"})
	}
	if err != nil {
		return err
	}
	var next any
	if len(rows) == f.Limit && f.After == 0 {
		next = rows[len(rows)-1].ID // before_id olarak gönderilir
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": rows, "latest_id": latest, "next_before": next})
	return nil
}

// live: GET /schedules/{id}/live?slot_id= — salon bazlı canlı yoklama özeti (sınav günü izleme).
func (s *Server) live(w http.ResponseWriter, r *http.Request) error {
	id, err := pathInt64(r, "id")
	if err != nil {
		return err
	}
	if _, err := s.visibleSchedule(r, id); err != nil {
		return err
	}
	slotID, err := queryInt64(r, "slot_id")
	if err != nil {
		return err
	}
	slot, err := store.PickLiveSlot(r.Context(), s.pool, id, slotID, time.Now())
	if err != nil {
		return storeError(err)
	}
	out, err := store.Live(r.Context(), s.pool, id, slot)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	WriteJSON(w, http.StatusOK, out)
	return nil
}

// liveEvents: GET /schedules/{id}/live/events?slot_id=&since=&limit= — canlı yoklama olay akışı (artımlı).
func (s *Server) liveEvents(w http.ResponseWriter, r *http.Request) error {
	id, err := pathInt64(r, "id")
	if err != nil {
		return err
	}
	if _, err := s.visibleSchedule(r, id); err != nil {
		return err
	}
	slotID, err := queryInt64(r, "slot_id")
	if err != nil {
		return err
	}
	slot, err := store.PickLiveSlot(r.Context(), s.pool, id, slotID, time.Now())
	if err != nil {
		return storeError(err)
	}
	limit := 30
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			return Validation("limit 1..200 olmalı", map[string]string{"limit": "geçersiz"})
		}
		limit = n
	}
	var since *time.Time
	if v := r.URL.Query().Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			return Validation("since RFC 3339 zamanı olmalı", map[string]string{"since": "geçersiz"})
		}
		since = &t
	}
	rows, err := store.LiveEvents(r.Context(), s.pool, id, slot.ID, since, limit)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	WriteJSON(w, http.StatusOK, map[string]any{"slot": slot, "items": rows})
	return nil
}

// mySeatMap: GET /me/exams/{examId}/seatmap — öğrencinin salon krokisi ve kendi koltuğu.
func (s *Server) mySeatMap(w http.ResponseWriter, r *http.Request) error {
	p := mustPrincipal(r)
	if p.StudentID == 0 {
		return Forbidden("bu hesap bir öğrenci kaydına bağlı değil")
	}
	examID, err := pathInt64(r, "examId")
	if err != nil {
		return err
	}
	m, err := store.StudentSeatMap(r.Context(), s.pool, p.StudentID, examID)
	if err != nil {
		return storeError(err)
	}
	WriteJSON(w, http.StatusOK, m)
	return nil
}

// --- Dönem slotları, simülasyon (X-4), karşılaştırma (X-5) ----------------------------

func (s *Server) periodSlots(w http.ResponseWriter, r *http.Request) error {
	id, err := pathInt64(r, "id")
	if err != nil {
		return err
	}
	slots, err := store.PeriodSlots(r.Context(), s.pool, id)
	if err != nil {
		return err
	}
	if len(slots) == 0 {
		return NotFound("dönem bulunamadı")
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": slots})
	return nil
}

type simulateRequest struct {
	PeriodID   int64          `json:"period_id"`
	Seed       int64          `json:"seed"`
	Iterations int            `json:"iterations"`
	Scope      planning.Scope `json:"scope"`
}

// MaxSimulateIterations: eşzamanlı (istek içinde) koşu için üst sınır; tam üretim asenkron işle yapılır.
const MaxSimulateIterations = 300_000

// simulate: POST /schedules/simulate — T-1'i kaydetmeden çalıştırır, yayındaki sürümle kıyaslar.
func (s *Server) simulate(w http.ResponseWriter, r *http.Request) error {
	var req simulateRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	f := map[string]string{}
	if req.PeriodID <= 0 {
		f["period_id"] = "zorunlu"
	}
	if req.Iterations < 1 || req.Iterations > MaxSimulateIterations {
		f["iterations"] = "1 ile " + strconv.Itoa(MaxSimulateIterations) + " arasında olmalı"
	}
	if len(f) > 0 {
		return Validation("simülasyon isteği geçersiz", f)
	}
	ds, err := store.LoadDataset(r.Context(), s.pool, req.PeriodID, req.Seed)
	if err != nil {
		return storeError(err)
	}
	scoped, info, err := planning.ApplyScope(ds, req.Scope)
	var se *planning.ScopeError
	if errors.As(err, &se) {
		return Validation(se.Msg, map[string]string{se.Field: se.Msg})
	}
	if err != nil {
		return err
	}
	res, err := planning.Simulate(scoped, req.Seed, req.Iterations)
	if err != nil {
		return err
	}
	out := map[string]any{"result": res, "saved": false, "scope": info}
	full := req.Scope.FacultyID == 0 && len(req.Scope.DepartmentIDs) == 0 && len(req.Scope.Years) == 0
	// Yayındaki sürümle kıyas yalnızca tam kapsamda anlamlıdır (kısmi kapsamın cezası daha düşüktür).
	if sid, err := store.PublishedScheduleOfPeriod(r.Context(), s.pool, req.PeriodID); full && err == nil {
		if sc, err := store.GetSchedule(r.Context(), s.pool, sid); err == nil && sc.PeriodID == req.PeriodID {
			out["published"] = map[string]any{"schedule_id": sc.ID, "version": sc.Version, "metrics": sc.Metrics}
		}
	}
	WriteJSON(w, http.StatusOK, out)
	return nil
}

// compareSchedules: GET /schedules/compare?a=&b= — iki sürümün metrikleri ve taşınan sınavlar.
func (s *Server) compareSchedules(w http.ResponseWriter, r *http.Request) error {
	a, err := queryInt64(r, "a")
	if err != nil {
		return err
	}
	b, err := queryInt64(r, "b")
	if err != nil {
		return err
	}
	if a <= 0 || b <= 0 {
		return Validation("a ve b zorunlu", map[string]string{"a": "zorunlu", "b": "zorunlu"})
	}
	sa, err := store.GetSchedule(r.Context(), s.pool, a)
	if err != nil {
		return storeError(err)
	}
	sb, err := store.GetSchedule(r.Context(), s.pool, b)
	if err != nil {
		return storeError(err)
	}
	if sa.PeriodID != sb.PeriodID {
		return Validation("sürümler aynı döneme ait olmalı", map[string]string{"b": "farklı dönem"})
	}
	moved, err := store.MovedExams(r.Context(), s.pool, a, b)
	if err != nil {
		return err
	}
	WriteJSON(w, http.StatusOK, map[string]any{"a": sa, "b": sb, "moved_count": len(moved), "moved": moved[:min(len(moved), 200)]})
	return nil
}

// --- Elle düzenleme / sürükle-bırak (X-7) ---------------------------------------------

type moveRequest struct {
	SlotID int64 `json:"slot_id"`
	DryRun bool  `json:"dry_run"`
}

// moveExam: POST /schedules/{id}/exams/{examId}/move — sınavı başka slota taşır.
// Sert kısıtlar (çakışma, günlük sınır, slot kapasitesi) ihlal edilirse 422 ve neden listesi.
// Başarılıysa T-2..T-4 yeniden kurulur ve kaynak sürüm DEĞİŞMEDEN yeni bir taslak sürüm oluşur.
func (s *Server) moveExam(w http.ResponseWriter, r *http.Request) error {
	sid, err := pathInt64(r, "id")
	if err != nil {
		return err
	}
	examID, err := pathInt64(r, "examId")
	if err != nil {
		return err
	}
	var req moveRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	if req.SlotID <= 0 {
		return Validation("slot_id zorunlu", map[string]string{"slot_id": "zorunlu"})
	}
	sc, err := store.GetSchedule(r.Context(), s.pool, sid)
	if err != nil {
		return storeError(err)
	}
	base, err := store.ScheduleAssignment(r.Context(), s.pool, sid)
	if err != nil {
		return err
	}
	from, ok := base[examID]
	if !ok {
		return NotFound("sınav bu çizelgede yok")
	}
	if from == req.SlotID {
		return Validation("sınav zaten bu slotta", map[string]string{"slot_id": "kaynakla aynı"})
	}
	ds, err := store.LoadDataset(r.Context(), s.pool, sc.PeriodID, sc.Seed)
	if err != nil {
		return storeError(err)
	}
	idx := planning.SlotIndex(ds)
	to, ok := idx[req.SlotID]
	if !ok {
		return Validation("slot bu döneme ait değil", map[string]string{"slot_id": "geçersiz"})
	}
	assign := make(map[int64]int, len(base))
	for e, sl := range base {
		assign[e] = idx[sl]
	}
	before := planning.CheckAssignment(ds, sc.Seed, assign).Metrics.Penalty
	assign[examID] = to
	after := planning.CheckAssignment(ds, sc.Seed, assign)
	if len(after.Violations) > 0 {
		fields := map[string]string{}
		for i, v := range after.Violations {
			if i == 5 {
				break
			}
			fields[fmt.Sprintf("ihlal_%d", i+1)] = v.Kind + ": " + v.Detail
		}
		if req.DryRun {
			WriteJSON(w, http.StatusOK, map[string]any{"ok": false, "violations": after.Violations[:min(len(after.Violations), 20)],
				"violation_count": len(after.Violations), "penalty_before": before, "penalty_after": after.Metrics.Penalty})
			return nil
		}
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "constraint_violation",
			Message: fmt.Sprintf("taşıma %d sert kısıtı ihlal ediyor; çizelge değişmedi", len(after.Violations)), Fields: fields}
	}
	if req.DryRun {
		WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "violations": []any{}, "violation_count": 0, "penalty_before": before, "penalty_after": after.Metrics.Penalty})
		return nil
	}
	excused, err := store.ExcusedSlots(r.Context(), s.pool, ds)
	if err != nil {
		return err
	}
	sp, rep, err := planning.BuildFromAssignment(ds, sc.Seed, assign, excused)
	var inf *planning.InfeasibleError
	if errors.As(err, &inf) {
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "constraint_violation", Message: inf.Message + "; çizelge değişmedi"}
	}
	if err != nil {
		return err
	}
	rep.DerivedFrom = sid
	rep.Edit = &planning.EditInfo{ExamID: examID, FromSlot: from, ToSlot: req.SlotID}
	sp.CreatedBy = mustPrincipal(r).UserID
	newID, version, err := store.CreateSchedule(r.Context(), s.pool, sp)
	if err != nil {
		return storeError(err)
	}
	WriteJSON(w, http.StatusCreated, map[string]any{"schedule_id": newID, "version": version, "derived_from": sid,
		"penalty_before": before, "penalty_after": after.Metrics.Penalty})
	return nil
}

// faculties: GET /faculties — fakülte → bölümler (sınıf, öğrenci, ders sayısı) → öğretim üyeleri.
func (s *Server) faculties(w http.ResponseWriter, r *http.Request) error {
	rows, err := store.Faculties(r.Context(), s.pool)
	if err != nil {
		return err
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": rows})
	return nil
}
