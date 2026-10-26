package httpapi

import (
	"net/http"
	"time"

	"github.com/AllenVB/sinavplan/backend/internal/auth"
	"github.com/AllenVB/sinavplan/backend/internal/store"
)

// listNotifications: GET /me/notifications — önce zamana bağlı bildirimler (sınav/görev hatırlatması,
// "yoklamanız alınmadı") bu kullanıcı için üretilir, sonra kutu ve okunmamış sayısı döner.
func (s *Server) listNotifications(w http.ResponseWriter, r *http.Request) error {
	p := mustPrincipal(r)
	if err := store.SyncDueNotifications(r.Context(), s.pool, p.UserID, p.StudentID, p.HasAnyRole(auth.RoleInvigilator), time.Now()); err != nil {
		return err
	}
	items, unread, err := store.ListNotifications(r.Context(), s.pool, p.UserID, 30)
	if err != nil {
		return err
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items, "unread": unread})
	return nil
}

// readNotification: POST /me/notifications/{id}/read — yalnız kendi bildirimi; başkasınınki 404.
func (s *Server) readNotification(w http.ResponseWriter, r *http.Request) error {
	id, err := pathInt64(r, "id")
	if err != nil {
		return err
	}
	n, err := store.MarkNotificationRead(r.Context(), s.pool, mustPrincipal(r).UserID, id)
	if err != nil {
		return err
	}
	if n == 0 {
		return NotFound("bildirim bulunamadı ya da zaten okundu")
	}
	WriteJSON(w, http.StatusOK, map[string]int64{"marked": n})
	return nil
}

// readAllNotifications: POST /me/notifications/read-all
func (s *Server) readAllNotifications(w http.ResponseWriter, r *http.Request) error {
	n, err := store.MarkNotificationRead(r.Context(), s.pool, mustPrincipal(r).UserID, 0)
	if err != nil {
		return err
	}
	WriteJSON(w, http.StatusOK, map[string]int64{"marked": n})
	return nil
}
