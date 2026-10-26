package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/AllenVB/sinavplan/backend/internal/auth"
	"github.com/AllenVB/sinavplan/backend/internal/jobs"
	"github.com/AllenVB/sinavplan/backend/internal/store"
)

// asAPIError, store doğrulama hatasını 422'ye çevirir (diğer hatalar storeError'a).
func asAPIError(err error) error {
	var ve *store.ValidationError
	if errors.As(err, &ve) {
		return Validation("girdi doğrulaması başarısız", ve.Fields)
	}
	return storeError(err)
}

func queryInt64(r *http.Request, name string) (int64, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return 0, Validation(name+" pozitif tam sayı olmalı", map[string]string{name: "geçersiz"})
	}
	return n, nil
}

// --- F-1: CSV içe aktarma ----------------------------------------------------------

const maxImportBytes = 20 << 20

func (s *Server) importCSV(w http.ResponseWriter, r *http.Request) error {
	kind := chiURLParam(r, "kind")
	ok := false
	for _, k := range store.ImportKinds {
		ok = ok || k == kind
	}
	if !ok {
		return NotFound("bilinmeyen içe aktarma türü; geçerli: " + strings.Join(store.ImportKinds, ", "))
	}
	body := http.MaxBytesReader(w, r.Body, maxImportBytes)
	rep, err := store.Import(r.Context(), s.pool, kind, body, mustPrincipal(r).UserID)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return &APIError{Status: http.StatusRequestEntityTooLarge, Code: "payload_too_large", Message: "dosya 20 MB sınırını aşıyor"}
		}
		if rep.Kind == "" { // başlık/dosya düzeyi hata, veritabanı hatası değil
			return Validation(err.Error(), map[string]string{"file": err.Error()})
		}
		return err
	}
	WriteJSON(w, http.StatusOK, rep)
	return nil
}

// --- F-2: Salonlar --------------------------------------------------------------------

func (s *Server) listRooms(w http.ResponseWriter, r *http.Request) error {
	page, err := parsePage(r)
	if err != nil {
		return err
	}
	typ := r.URL.Query().Get("type")
	if typ != "" && typ != "classic" && typ != "lab" {
		return Validation("type classic veya lab olmalı", map[string]string{"type": "geçersiz"})
	}
	rows, err := store.ListRooms(r.Context(), s.pool, typ, page.After, page.Limit+1)
	if err != nil {
		return err
	}
	WriteJSON(w, http.StatusOK, paginate(rows, page.Limit, func(x store.Room) int64 { return x.ID }))
	return nil
}

func (s *Server) getRoom(w http.ResponseWriter, r *http.Request) error {
	id, err := pathInt64(r, "id")
	if err != nil {
		return err
	}
	rm, err := store.GetRoom(r.Context(), s.pool, id)
	if err != nil {
		return storeError(err)
	}
	WriteJSON(w, http.StatusOK, rm)
	return nil
}

func (s *Server) createRoom(w http.ResponseWriter, r *http.Request) error {
	var in store.RoomInput
	if err := decodeJSON(r, &in); err != nil {
		return err
	}
	rm, err := store.CreateRoom(r.Context(), s.pool, in, mustPrincipal(r).UserID)
	if err != nil {
		return asAPIError(err)
	}
	w.Header().Set("Location", "/rooms/"+strconv.FormatInt(rm.ID, 10))
	WriteJSON(w, http.StatusCreated, rm)
	return nil
}

func (s *Server) updateRoom(w http.ResponseWriter, r *http.Request) error {
	id, err := pathInt64(r, "id")
	if err != nil {
		return err
	}
	var in store.RoomInput
	if err := decodeJSON(r, &in); err != nil {
		return err
	}
	rm, err := store.UpdateRoom(r.Context(), s.pool, id, in, mustPrincipal(r).UserID)
	if err != nil {
		return asAPIError(err)
	}
	WriteJSON(w, http.StatusOK, rm)
	return nil
}

func (s *Server) deleteRoom(w http.ResponseWriter, r *http.Request) error {
	id, err := pathInt64(r, "id")
	if err != nil {
		return err
	}
	if err := store.DeleteRoom(r.Context(), s.pool, id, mustPrincipal(r).UserID); err != nil {
		return storeError(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// --- F-3: Takvim ---------------------------------------------------------------------

func (s *Server) listPeriods(w http.ResponseWriter, r *http.Request) error {
	ps, err := store.ListPeriods(r.Context(), s.pool)
	if err != nil {
		return err
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": ps})
	return nil
}

type createScheduleRequest struct {
	PeriodID   int64 `json:"period_id"`
	Seed       int64 `json:"seed"`
	Iterations int   `json:"iterations"`
}

// createSchedule: POST /schedules — asenkron iş başlatır (202).
func (s *Server) createSchedule(w http.ResponseWriter, r *http.Request) error {
	var req createScheduleRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	f := map[string]string{}
	if req.PeriodID <= 0 {
		f["period_id"] = "zorunlu"
	}
	if req.Iterations < 0 || req.Iterations > jobs.MaxIterations {
		f["iterations"] = "0 ile " + strconv.Itoa(jobs.MaxIterations) + " arasında olmalı (0 = varsayılan)"
	}
	if len(f) > 0 {
		return Validation("takvim isteği geçersiz", f)
	}
	if s.runner == nil {
		return Unavailable("iş çalıştırıcı yapılandırılmadı")
	}
	id, err := s.runner.Submit(r.Context(), jobs.Params{PeriodID: req.PeriodID, Seed: req.Seed, Iterations: req.Iterations}, mustPrincipal(r).UserID)
	if errors.Is(err, store.ErrConflict) {
		return Conflict("bu dönem için bekleyen veya çalışan bir takvim işi var")
	}
	if err != nil {
		return storeError(err)
	}
	loc := "/schedule-jobs/" + strconv.FormatInt(id, 10)
	w.Header().Set("Location", loc)
	WriteJSON(w, http.StatusAccepted, map[string]any{"job_id": id, "status": "queued", "status_url": loc})
	return nil
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) error {
	id, err := pathInt64(r, "id")
	if err != nil {
		return err
	}
	j, err := store.GetJob(r.Context(), s.pool, id)
	if err != nil {
		return storeError(err)
	}
	WriteJSON(w, http.StatusOK, j)
	return nil
}

// visibleSchedule: sekreter her sürümü, diğer roller yalnız YAYINLANMIŞ sürümü görür
// (taslak başkasına 404: varlığı sızdırılmaz).
func (s *Server) visibleSchedule(r *http.Request, id int64) (store.Schedule, error) {
	sc, err := store.GetSchedule(r.Context(), s.pool, id)
	if err != nil {
		return store.Schedule{}, storeError(err)
	}
	if sc.Status != "published" && !mustPrincipal(r).HasAnyRole(auth.RoleSecretary) {
		return store.Schedule{}, NotFound("kayıt bulunamadı")
	}
	return sc, nil
}

func (s *Server) listSchedules(w http.ResponseWriter, r *http.Request) error {
	period, err := queryInt64(r, "period_id")
	if err != nil {
		return err
	}
	before, err := queryInt64(r, "before")
	if err != nil {
		return err
	}
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if limit, err = strconv.Atoi(v); err != nil || limit < 1 || limit > maxLimit {
			return Validation("limit geçersiz", map[string]string{"limit": "geçersiz"})
		}
	}
	only := !mustPrincipal(r).HasAnyRole(auth.RoleSecretary)
	rows, err := store.ListSchedules(r.Context(), s.pool, period, only, before, limit+1)
	if err != nil {
		return err
	}
	resp := map[string]any{"items": rows, "next_before": nil}
	if len(rows) > limit {
		resp["items"] = rows[:limit]
		resp["next_before"] = rows[limit-1].ID
	}
	if rows == nil {
		resp["items"] = []store.Schedule{}
	}
	WriteJSON(w, http.StatusOK, resp)
	return nil
}

func (s *Server) getSchedule(w http.ResponseWriter, r *http.Request) error {
	id, err := pathInt64(r, "id")
	if err != nil {
		return err
	}
	sc, err := s.visibleSchedule(r, id)
	if err != nil {
		return err
	}
	WriteJSON(w, http.StatusOK, sc)
	return nil
}

func (s *Server) publishSchedule(w http.ResponseWriter, r *http.Request) error {
	id, err := pathInt64(r, "id")
	if err != nil {
		return err
	}
	if err := store.PublishSchedule(r.Context(), s.pool, id, mustPrincipal(r).UserID); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return Conflict("bu çizelge zaten yayınlanmış")
		}
		return storeError(err)
	}
	sc, err := store.GetSchedule(r.Context(), s.pool, id)
	if err != nil {
		return err
	}
	WriteJSON(w, http.StatusOK, sc)
	return nil
}

func (s *Server) scheduleExams(w http.ResponseWriter, r *http.Request) error {
	id, err := pathInt64(r, "id")
	if err != nil {
		return err
	}
	if _, err := s.visibleSchedule(r, id); err != nil {
		return err
	}
	page, err := parsePage(r)
	if err != nil {
		return err
	}
	rows, err := store.ScheduleExams(r.Context(), s.pool, id, page.After, page.Limit+1)
	if err != nil {
		return err
	}
	WriteJSON(w, http.StatusOK, paginate(rows, page.Limit, func(x store.CalendarExam) int64 { return x.ExamID }))
	return nil
}

// --- F-4: Salon ve oturma planı ------------------------------------------------------

func (s *Server) slotRooms(w http.ResponseWriter, r *http.Request) error {
	id, err := pathInt64(r, "id")
	if err != nil {
		return err
	}
	if _, err := s.visibleSchedule(r, id); err != nil {
		return err
	}
	slot, err := queryInt64(r, "slot_id")
	if err != nil {
		return err
	}
	if slot == 0 {
		return Validation("slot_id zorunlu", map[string]string{"slot_id": "zorunlu"})
	}
	rows, err := store.SlotRooms(r.Context(), s.pool, id, slot)
	if err != nil {
		return err
	}
	if rows == nil {
		rows = []store.SlotRoom{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": rows})
	return nil
}

// seating: öğretim üyesi yalnız KENDİ derslerinin sınavını, gözetmen yalnız KENDİ görev
// salonlarını görür (sahiplik sunucuda); sekreter ve öğrenci işleri hepsini.
func (s *Server) seating(w http.ResponseWriter, r *http.Request) error {
	id, err := pathInt64(r, "id")
	if err != nil {
		return err
	}
	if _, err := s.visibleSchedule(r, id); err != nil {
		return err
	}
	exam, err := queryInt64(r, "exam_id")
	if err != nil {
		return err
	}
	room, err := queryInt64(r, "room_id")
	if err != nil {
		return err
	}
	if exam == 0 {
		return Validation("exam_id zorunlu", map[string]string{"exam_id": "zorunlu"})
	}
	p := mustPrincipal(r)
	if !p.HasAnyRole(auth.RoleSecretary, auth.RoleStudentAffairs) {
		allowed := false
		if p.HasAnyRole(auth.RoleLecturer) && p.LecturerID != 0 {
			var ok bool
			if err := s.pool.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM exams e JOIN sections sc ON sc.course_id = e.course_id
				WHERE e.id = $1 AND sc.lecturer_id = $2)`, exam, p.LecturerID).Scan(&ok); err != nil {
				return err
			}
			allowed = allowed || ok
		}
		if !allowed && p.HasAnyRole(auth.RoleInvigilator) {
			var ok bool
			if err := s.pool.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM invigilator_assignments ia
				JOIN room_assignments ra ON ra.schedule_id = ia.schedule_id AND ra.slot_id = ia.slot_id AND ra.room_id = ia.room_id
				WHERE ia.schedule_id = $1 AND ia.user_id = $2 AND ra.exam_id = $3 AND ($4 = 0 OR ra.room_id = $4))`,
				id, p.UserID, exam, room).Scan(&ok); err != nil {
				return err
			}
			allowed = ok
		}
		if !allowed {
			return Forbidden("bu sınavın oturma planına erişim yetkiniz yok")
		}
	}
	page, err := parsePage(r)
	if err != nil {
		return err
	}
	rows, err := store.Seating(r.Context(), s.pool, id, exam, room, page.After, page.Limit+1)
	if err != nil {
		return err
	}
	WriteJSON(w, http.StatusOK, paginate(rows, page.Limit, func(x store.SeatRow) int64 { return x.ID }))
	return nil
}

func (s *Server) scheduleConflicts(w http.ResponseWriter, r *http.Request) error {
	id, err := pathInt64(r, "id")
	if err != nil {
		return err
	}
	if _, err := s.visibleSchedule(r, id); err != nil {
		return err
	}
	page, err := parsePage(r)
	if err != nil {
		return err
	}
	rows, err := store.ScheduleConflicts(r.Context(), s.pool, id, page.After, page.Limit+1)
	if err != nil {
		return err
	}
	// Sayfalama öğrenci kimliğiyle; aynı öğrencinin birden çok günü sayfa sınırında bölünmesin.
	WriteJSON(w, http.StatusOK, paginate(rows, page.Limit, store.ConflictCursor))
	return nil
}

// --- F-5: Gözetmen ---------------------------------------------------------------------

func (s *Server) scheduleInvigilators(w http.ResponseWriter, r *http.Request) error {
	id, err := pathInt64(r, "id")
	if err != nil {
		return err
	}
	if _, err := s.visibleSchedule(r, id); err != nil {
		return err
	}
	user, err := queryInt64(r, "user_id")
	if err != nil {
		return err
	}
	slot, err := queryInt64(r, "slot_id")
	if err != nil {
		return err
	}
	page, err := parsePage(r)
	if err != nil {
		return err
	}
	rows, err := store.Duties(r.Context(), s.pool, id, user, slot, page.After, page.Limit+1)
	if err != nil {
		return err
	}
	WriteJSON(w, http.StatusOK, paginate(rows, page.Limit, func(x store.Duty) int64 { return x.ID }))
	return nil
}

// myDuties: gözetmenin YAYINLANMIŞ çizelgedeki kendi görevleri.
// myDuties: GET /me/duties — gözetmenin görevleri; HER dönemin yayınlanmış çizelgesinden (schedule_id ile).
func (s *Server) myDuties(w http.ResponseWriter, r *http.Request) error {
	page, err := parsePage(r)
	if err != nil {
		return err
	}
	rows, err := store.PublishedDuties(r.Context(), s.pool, mustPrincipal(r).UserID, page.After, page.Limit+1)
	if err != nil {
		return err
	}
	WriteJSON(w, http.StatusOK, paginate(rows, page.Limit, func(x store.Duty) int64 { return x.ID }))
	return nil
}

func (s *Server) removeInvigilator(w http.ResponseWriter, r *http.Request) error {
	id, err := pathInt64(r, "id")
	if err != nil {
		return err
	}
	uid, err := pathInt64(r, "userId")
	if err != nil {
		return err
	}
	// ?dry_run=true: önizleme — aynı işlem koşar, sonunda geri alınır (Faz 22 B2)
	res, err := store.RemoveInvigilator(r.Context(), s.pool, id, uid, mustPrincipal(r).UserID, r.URL.Query().Get("dry_run") == "true")
	if err != nil {
		return storeError(err)
	}
	WriteJSON(w, http.StatusOK, res)
	return nil
}

// --- F-6: Mazeret ----------------------------------------------------------------------

type excuseRequest struct {
	ExamID int64  `json:"exam_id"`
	SlotID int64  `json:"slot_id"`
	Reason string `json:"reason"`
}

// createExcuse: öğrenci sınav için, gözetmen slot için mazeret girer (kimlik token'dan).
func (s *Server) createExcuse(w http.ResponseWriter, r *http.Request) error {
	var req excuseRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	p := mustPrincipal(r)
	var e store.Excuse
	var err error
	switch {
	case p.HasAnyRole(auth.RoleStudent) && p.StudentID != 0:
		if req.ExamID <= 0 || req.SlotID != 0 {
			return Validation("öğrenci mazereti için yalnızca exam_id verilmeli", map[string]string{"exam_id": "zorunlu", "slot_id": "öğrenci için verilmemeli"})
		}
		e, err = store.CreateStudentExcuse(r.Context(), s.pool, p.StudentID, req.ExamID, req.Reason)
	case p.HasAnyRole(auth.RoleInvigilator):
		if req.SlotID <= 0 || req.ExamID != 0 {
			return Validation("gözetmen mazereti için yalnızca slot_id verilmeli", map[string]string{"slot_id": "zorunlu", "exam_id": "gözetmen için verilmemeli"})
		}
		e, err = store.CreateInvigilatorExcuse(r.Context(), s.pool, p.UserID, req.SlotID, req.Reason)
	default:
		return Forbidden("mazeret başvurusu yalnızca öğrenci veya gözetmen hesabıyla yapılabilir")
	}
	if err != nil {
		return asAPIError(err)
	}
	w.Header().Set("Location", "/excuses/"+strconv.FormatInt(e.ID, 10))
	WriteJSON(w, http.StatusCreated, e)
	return nil
}

func (s *Server) listExcuses(w http.ResponseWriter, r *http.Request) error {
	page, err := parsePage(r)
	if err != nil {
		return err
	}
	status, kind := r.URL.Query().Get("status"), r.URL.Query().Get("kind")
	if status != "" && status != "pending" && status != "approved" && status != "rejected" {
		return Validation("status pending, approved veya rejected olmalı", map[string]string{"status": "geçersiz"})
	}
	if kind != "" && kind != "student" && kind != "invigilator" {
		return Validation("kind student veya invigilator olmalı", map[string]string{"kind": "geçersiz"})
	}
	rows, err := store.ListExcuses(r.Context(), s.pool, status, kind, 0, 0, page.After, page.Limit+1)
	if err != nil {
		return err
	}
	WriteJSON(w, http.StatusOK, paginate(rows, page.Limit, func(x store.Excuse) int64 { return x.ID }))
	return nil
}

// myExcuses: kullanıcı yalnız KENDİ mazeretlerini görür.
func (s *Server) myExcuses(w http.ResponseWriter, r *http.Request) error {
	page, err := parsePage(r)
	if err != nil {
		return err
	}
	p := mustPrincipal(r)
	var stu, usr int64
	if p.HasAnyRole(auth.RoleStudent) && p.StudentID != 0 {
		stu = p.StudentID
	} else {
		usr = p.UserID
	}
	rows, err := store.ListExcuses(r.Context(), s.pool, "", "", stu, usr, page.After, page.Limit+1)
	if err != nil {
		return err
	}
	WriteJSON(w, http.StatusOK, paginate(rows, page.Limit, func(x store.Excuse) int64 { return x.ID }))
	return nil
}

type decisionRequest struct {
	Decision string `json:"decision"`
	Note     string `json:"note"`
}

func (s *Server) decideExcuse(w http.ResponseWriter, r *http.Request) error {
	id, err := pathInt64(r, "id")
	if err != nil {
		return err
	}
	var req decisionRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	if req.Decision != "approved" && req.Decision != "rejected" {
		return Validation("decision approved veya rejected olmalı", map[string]string{"decision": "geçersiz"})
	}
	e, err := store.DecideExcuse(r.Context(), s.pool, id, mustPrincipal(r).UserID, req.Decision == "approved", req.Note)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			return Conflict("bu mazeret için karar zaten verilmiş")
		}
		return asAPIError(err)
	}
	WriteJSON(w, http.StatusOK, e)
	return nil
}
