package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/AllenVB/sinavplan/backend/internal/domain/attendance"
	"github.com/AllenVB/sinavplan/backend/internal/store"
)

// storeError, store hatalarını ortak HTTP hata biçimine çevirir.
func storeError(err error) error {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return NotFound("kayıt bulunamadı")
	case errors.Is(err, store.ErrQRUsed):
		return &APIError{Status: http.StatusConflict, Code: "qr_already_used", Message: "bu sınav kartı daha önce okutuldu"}
	case errors.Is(err, store.ErrQRExpired):
		return &APIError{Status: http.StatusConflict, Code: "qr_expired", Message: "sınav kartının süresi dolmuş veya kart yenilenmiş"}
	case errors.Is(err, store.ErrQRInvalid):
		return Validation("sınav kartı geçersiz", map[string]string{"token": "imza doğrulanamadı"})
	case errors.Is(err, store.ErrCapacity):
		return &APIError{Status: http.StatusConflict, Code: "insufficient_capacity", Message: err.Error()}
	case errors.Is(err, store.ErrConflict):
		return Conflict("kayıt bu işlem için uygun durumda değil")
	}
	return err
}

type syncRequest struct {
	Events []attendance.Event `json:"events"`
}

// attendanceSync: POST /attendance/sync — çevrimdışı cihazların toplu olayları.
func (s *Server) attendanceSync(w http.ResponseWriter, r *http.Request) error {
	var req syncRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	if len(req.Events) == 0 || len(req.Events) > store.MaxSyncBatch {
		return Validation("events 1 ile 1000 olay arasında olmalı", map[string]string{"events": "geçersiz sayı"})
	}
	res, err := store.SyncAttendance(r.Context(), s.pool, req.Events, mustPrincipal(r).UserID)
	if err != nil {
		return err
	}
	WriteJSON(w, http.StatusOK, res)
	return nil
}

type qrUseRequest struct {
	Token    string `json:"token"`
	DeviceID string `json:"device_id"`
}

// attendanceQR: POST /attendance/qr — sınav kartını okut (tek kullanımlık).
func (s *Server) attendanceQR(w http.ResponseWriter, r *http.Request) error {
	var req qrUseRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	if req.Token == "" || req.DeviceID == "" {
		return Validation("token ve device_id zorunlu", map[string]string{"token": "zorunlu", "device_id": "zorunlu"})
	}
	if s.qr == nil {
		return Unavailable("QR yapılandırılmadı")
	}
	u, err := s.qr.Use(r.Context(), s.pool, req.Token, req.DeviceID, mustPrincipal(r).UserID)
	if err != nil {
		return storeError(err)
	}
	WriteJSON(w, http.StatusOK, u)
	return nil
}

// examCard: POST /me/exams/{examId}/card — öğrenci kendi sınav kartını (QR) alır.
// POST, çünkü her çağrı yeni token üretip eskisini iptal eder (yan etkili).
func (s *Server) examCard(w http.ResponseWriter, r *http.Request) error {
	p := mustPrincipal(r)
	if p.StudentID == 0 {
		return Forbidden("bu hesap bir öğrenci kaydına bağlı değil")
	}
	examID, err := pathInt64(r, "examId")
	if err != nil {
		return err
	}
	card, err := store.StudentExamCard(r.Context(), s.pool, p.StudentID, examID)
	if err != nil {
		return storeError(err)
	}
	if s.qr == nil {
		return Unavailable("QR yapılandırılmadı")
	}
	// Kart, sınav bitiminden 1 saat sonrasına kadar geçerli.
	expires := card.EndsAt.Add(time.Hour)
	tok, err := s.qr.Issue(r.Context(), s.pool, examID, p.StudentID, expires)
	if err != nil {
		return err
	}
	WriteJSON(w, http.StatusCreated, map[string]any{"card": card, "student_id": p.StudentID,
		"qr_token": tok, "expires_at": expires})
	return nil
}

// examAttendance: GET /exams/{id}/attendance?limit=&after= — sınav yoklaması (keyset).
func (s *Server) examAttendance(w http.ResponseWriter, r *http.Request) error {
	examID, err := pathInt64(r, "id")
	if err != nil {
		return err
	}
	page, err := parsePage(r)
	if err != nil {
		return err
	}
	rows, err := store.ExamAttendance(r.Context(), s.pool, examID, page.After, page.Limit+1)
	if err != nil {
		return err
	}
	sum, err := store.ExamAttendanceSummary(r.Context(), s.pool, examID)
	if err != nil {
		return err
	}
	resp := paginate(rows, page.Limit, func(a store.AttendanceRow) int64 { return a.StudentID })
	WriteJSON(w, http.StatusOK, map[string]any{"summary": sum, "items": resp.Items, "next_cursor": resp.NextCursor})
	return nil
}

// attendanceConflicts: GET /attendance/conflicts?exam_id=&limit=&after=
func (s *Server) attendanceConflicts(w http.ResponseWriter, r *http.Request) error {
	page, err := parsePage(r)
	if err != nil {
		return err
	}
	var examID int64
	if v := r.URL.Query().Get("exam_id"); v != "" {
		if examID, err = strconv.ParseInt(v, 10, 64); err != nil || examID <= 0 {
			return Validation("exam_id pozitif tam sayı olmalı", map[string]string{"exam_id": "geçersiz"})
		}
	}
	rows, err := store.AttendanceConflicts(r.Context(), s.pool, examID, page.After, page.Limit+1)
	if err != nil {
		return err
	}
	WriteJSON(w, http.StatusOK, paginate(rows, page.Limit, func(c store.ConflictRow) int64 { return c.ID }))
	return nil
}

type roomChangeRequest struct {
	SlotID     int64  `json:"slot_id"`
	FromRoomID int64  `json:"from_room_id"`
	ToRoomID   int64  `json:"to_room_id"`
	Reason     string `json:"reason"`
}

// roomChange: POST /schedules/{id}/room-change — toplu salon değişikliği (tahliye).
func (s *Server) roomChange(w http.ResponseWriter, r *http.Request) error {
	sid, err := pathInt64(r, "id")
	if err != nil {
		return err
	}
	var req roomChangeRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	f := map[string]string{}
	if req.SlotID <= 0 {
		f["slot_id"] = "zorunlu"
	}
	if req.FromRoomID <= 0 {
		f["from_room_id"] = "zorunlu"
	}
	if req.ToRoomID <= 0 {
		f["to_room_id"] = "zorunlu"
	}
	if req.FromRoomID == req.ToRoomID && req.FromRoomID > 0 {
		f["to_room_id"] = "kaynak salondan farklı olmalı"
	}
	if req.Reason == "" {
		f["reason"] = "zorunlu"
	}
	if len(f) > 0 {
		return Validation("salon değişikliği isteği geçersiz", f)
	}
	res, err := store.RoomChange(r.Context(), s.pool, store.RoomChangeRequest{ScheduleID: sid, SlotID: req.SlotID,
		FromRoomID: req.FromRoomID, ToRoomID: req.ToRoomID, Reason: req.Reason, ActorID: mustPrincipal(r).UserID,
		Strict: s.seatingStrict})
	if err != nil {
		return storeError(err)
	}
	WriteJSON(w, http.StatusOK, res)
	return nil
}
