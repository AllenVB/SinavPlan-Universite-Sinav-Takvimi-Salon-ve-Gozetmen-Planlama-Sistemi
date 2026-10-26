package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/AllenVB/sinavplan/backend/internal/store"
)

type assignInvigilatorRequest struct {
	SlotID        int64 `json:"slot_id"`
	RoomID        int64 `json:"room_id"`
	UserID        int64 `json:"user_id"`
	ReplaceUserID int64 `json:"replace_user_id"`
}

// assignInvigilator: POST /schedules/{id}/invigilators/assign — sekreter, bir görevi (oturum × salon) istediği gözetmene
// verir (replace_user_id ile yerine geçer ya da ek gözetmen olur). T-4 kurallarını bozan atama 409 invigilation_rules
// ile reddedilir, ihlaller fields içinde döner. ?dry_run=true: önizleme, hiçbir şey yazılmaz.
func (s *Server) assignInvigilator(w http.ResponseWriter, r *http.Request) error {
	id, err := pathInt64(r, "id")
	if err != nil {
		return err
	}
	var req assignInvigilatorRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	f := map[string]string{}
	for k, v := range map[string]int64{"slot_id": req.SlotID, "room_id": req.RoomID, "user_id": req.UserID} {
		if v <= 0 {
			f[k] = "zorunlu"
		}
	}
	if req.ReplaceUserID < 0 {
		f["replace_user_id"] = "pozitif olmalı"
	}
	if len(f) > 0 {
		return Validation("atama isteği geçersiz", f)
	}
	dry := r.URL.Query().Get("dry_run") == "true"
	res, err := store.AssignInvigilator(r.Context(), s.pool, store.AssignInvigilatorRequest{ScheduleID: id, SlotID: req.SlotID, RoomID: req.RoomID,
		UserID: req.UserID, ReplaceUserID: req.ReplaceUserID, ActorID: mustPrincipal(r).UserID, DryRun: dry})
	switch {
	case errors.Is(err, store.ErrNotFound):
		return NotFound(detail(err, store.ErrNotFound))
	case errors.Is(err, store.ErrConflict):
		return Conflict(detail(err, store.ErrConflict))
	case err != nil:
		return asAPIError(err)
	}
	if !res.OK && !dry {
		fields := map[string]string{}
		var msgs []string
		for i, v := range res.Violations {
			fields[fmt.Sprintf("%d_%s", i+1, v.Kind)] = v.Detail
			msgs = append(msgs, v.Detail)
		}
		return &APIError{Status: http.StatusConflict, Code: "invigilation_rules", Message: "atama gözetmen kurallarını bozuyor: " + strings.Join(msgs, "; "), Fields: fields}
	}
	WriteJSON(w, http.StatusOK, res)
	return nil
}

// detail, sarılmış hatanın açıklamasını döner ("kayıt bulunamadı: X" -> "X").
func detail(err, base error) string {
	if m := strings.TrimPrefix(err.Error(), base.Error()+": "); m != err.Error() {
		return m
	}
	return base.Error()
}
