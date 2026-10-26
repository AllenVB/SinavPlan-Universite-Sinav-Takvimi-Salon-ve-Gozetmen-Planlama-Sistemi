package httpapi

import (
	"net/http"

	"github.com/AllenVB/sinavplan/backend/internal/store"
)

// --- Salon–oturum uygunluğu (kapalı salonlar) ---------------------------------------------

func (s *Server) listRoomBlocks(w http.ResponseWriter, r *http.Request) error {
	items, err := store.ListRoomBlocks(r.Context(), s.pool)
	if err != nil {
		return err
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

type roomBlockRequest struct {
	RoomID int64  `json:"room_id"`
	SlotID int64  `json:"slot_id"`
	Reason string `json:"reason"`
}

func (s *Server) createRoomBlock(w http.ResponseWriter, r *http.Request) error {
	var in roomBlockRequest
	if err := decodeJSON(r, &in); err != nil {
		return err
	}
	if in.RoomID <= 0 || in.SlotID <= 0 {
		return Validation("room_id ve slot_id zorunlu", map[string]string{"room_id": "zorunlu", "slot_id": "zorunlu"})
	}
	b, err := store.CreateRoomBlock(r.Context(), s.pool, in.RoomID, in.SlotID, in.Reason, mustPrincipal(r).UserID)
	if err != nil {
		return asAPIError(err)
	}
	WriteJSON(w, http.StatusCreated, b)
	return nil
}

func (s *Server) deleteRoomBlock(w http.ResponseWriter, r *http.Request) error {
	id, err := pathInt64(r, "id")
	if err != nil {
		return err
	}
	if err := store.DeleteRoomBlock(r.Context(), s.pool, id, mustPrincipal(r).UserID); err != nil {
		return storeError(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
