package httpapi

import (
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/AllenVB/sinavplan/backend/internal/store"
)

// checkinGuard, 6 haneli kod tahminini sınırlar: kullanıcı başına 5 dakikada en çok 8 başarısız deneme.
type checkinGuard struct {
	mu   sync.Mutex
	fail map[int64][]time.Time
}

const (
	checkinMaxFails = 8
	checkinFailSpan = 5 * time.Minute
)

func (g *checkinGuard) blocked(uid int64, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	kept := g.fail[uid][:0]
	for _, t := range g.fail[uid] {
		if now.Sub(t) < checkinFailSpan {
			kept = append(kept, t)
		}
	}
	if g.fail == nil {
		g.fail = map[int64][]time.Time{}
	}
	g.fail[uid] = kept
	return len(kept) >= checkinMaxFails
}

func (g *checkinGuard) record(uid int64, now time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.fail == nil {
		g.fail = map[int64][]time.Time{}
	}
	g.fail[uid] = append(g.fail[uid], now)
}

func checkinError(err error) error {
	switch {
	case errors.Is(err, store.ErrCheckinWindow):
		return &APIError{Status: http.StatusConflict, Code: "checkin_window", Message: err.Error()}
	case errors.Is(err, store.ErrCheckinClosed):
		return &APIError{Status: http.StatusConflict, Code: "checkin_closed", Message: err.Error()}
	case errors.Is(err, store.ErrCheckinBadCode):
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "checkin_bad_code", Message: err.Error()}
	case errors.Is(err, store.ErrCheckinNotHere):
		return &APIError{Status: http.StatusConflict, Code: "checkin_not_here", Message: err.Error()}
	case errors.Is(err, store.ErrCheckinExcused):
		return &APIError{Status: http.StatusConflict, Code: "excused", Message: err.Error()}
	case errors.Is(err, store.ErrCheckinTokenOld):
		return &APIError{Status: http.StatusConflict, Code: "checkin_token_old", Message: err.Error()}
	}
	return storeError(err)
}

type startCheckinRequest struct {
	SlotID int64 `json:"slot_id"`
	RoomID int64 `json:"room_id"`
}

// startCheckin: POST /checkin-sessions — gözetmen "Yeni QR oluştur".
func (s *Server) startCheckin(w http.ResponseWriter, r *http.Request) error {
	var req startCheckinRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	if req.SlotID <= 0 || req.RoomID <= 0 {
		return Validation("slot_id ve room_id zorunlu", map[string]string{"slot_id": "zorunlu", "room_id": "zorunlu"})
	}
	if s.qr == nil {
		return Unavailable("QR yapılandırılmadı")
	}
	c, err := s.qr.StartCheckin(r.Context(), s.pool, mustPrincipal(r).UserID, req.SlotID, req.RoomID, time.Now())
	if err != nil {
		return checkinError(err)
	}
	WriteJSON(w, http.StatusCreated, c)
	return nil
}

// checkinState: GET /checkin-sessions/{id} — taze QR jetonu + sayaçlar (ekran 5 sn'de bir çağırır).
func (s *Server) checkinState(w http.ResponseWriter, r *http.Request) error {
	id, err := pathInt64(r, "id")
	if err != nil {
		return err
	}
	c, err := s.qr.CheckinState(r.Context(), s.pool, mustPrincipal(r).UserID, id, time.Now())
	if err != nil {
		return checkinError(err)
	}
	WriteJSON(w, http.StatusOK, c)
	return nil
}

// rotateCheckinCode: POST /checkin-sessions/{id}/code — yeni 6 haneli kod.
func (s *Server) rotateCheckinCode(w http.ResponseWriter, r *http.Request) error {
	id, err := pathInt64(r, "id")
	if err != nil {
		return err
	}
	c, err := s.qr.RotateCheckinCode(r.Context(), s.pool, mustPrincipal(r).UserID, id, time.Now())
	if err != nil {
		return checkinError(err)
	}
	WriteJSON(w, http.StatusOK, c)
	return nil
}

// closeCheckin: POST /checkin-sessions/{id}/close
func (s *Server) closeCheckin(w http.ResponseWriter, r *http.Request) error {
	id, err := pathInt64(r, "id")
	if err != nil {
		return err
	}
	if err := store.CloseCheckin(r.Context(), s.pool, mustPrincipal(r).UserID, id); err != nil {
		return checkinError(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

type checkInRequest struct {
	Token string `json:"token"`
	Code  string `json:"code"`
}

// selfCheckIn: POST /me/checkin — öğrenci kendi hesabıyla QR veya 6 haneli kodla yoklamaya yazılır.
func (s *Server) selfCheckIn(w http.ResponseWriter, r *http.Request) error {
	p := mustPrincipal(r)
	if p.StudentID == 0 {
		return Forbidden("bu hesap bir öğrenci kaydına bağlı değil")
	}
	var req checkInRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	if (req.Token == "") == (req.Code == "") {
		return Validation("token veya code'dan yalnız biri gönderilmeli", map[string]string{"token": "token ya da code", "code": "token ya da code"})
	}
	if req.Code != "" {
		if len(req.Code) != 6 || func() bool {
			for _, c := range req.Code {
				if c < '0' || c > '9' {
					return true
				}
			}
			return false
		}() {
			return Validation("kod 6 haneli sayı olmalı", map[string]string{"code": "6 haneli sayı"})
		}
	}
	now := time.Now()
	if s.checkins.blocked(p.UserID, now) {
		return &APIError{Status: http.StatusTooManyRequests, Code: "too_many_attempts", Message: "çok fazla hatalı deneme; birkaç dakika sonra tekrar deneyin"}
	}
	if s.qr == nil {
		return Unavailable("QR yapılandırılmadı")
	}
	res, err := s.qr.CheckIn(r.Context(), s.pool, p.UserID, p.StudentID, req.Token, req.Code, now)
	if err != nil {
		if errors.Is(err, store.ErrCheckinBadCode) || errors.Is(err, store.ErrQRInvalid) {
			s.checkins.record(p.UserID, now)
		}
		return checkinError(err)
	}
	WriteJSON(w, http.StatusOK, res)
	return nil
}
