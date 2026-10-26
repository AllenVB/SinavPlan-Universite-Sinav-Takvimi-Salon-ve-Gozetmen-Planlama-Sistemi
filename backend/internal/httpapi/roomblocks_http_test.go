package httpapi

import (
	"net/http"
	"testing"

	"github.com/AllenVB/sinavplan/backend/internal/simdata"
)

func TestRoomBlocksLifecycleAndRoles(t *testing.T) {
	f := checkinFixture(t)
	sec, _ := login(t, f.s, simdata.EmailSecretary)
	body := j(map[string]any{"room_id": f.roomID, "slot_id": f.slotID, "reason": "Başka etkinlik"})

	if code, _ := do(t, f.s, http.MethodPost, "/room-blocks", f.inv, "application/json", body); code != http.StatusForbidden {
		t.Fatalf("gözetmen salon kapatamaz: %d", code)
	}
	code, b := do(t, f.s, http.MethodPost, "/room-blocks", sec, "application/json", body)
	if code != http.StatusCreated || b["room_id"].(float64) != float64(f.roomID) || b["affected_exams"].(float64) < 1 {
		t.Fatalf("kapatma: %d %v (yayındaki çizelgede etkilenen sınav bekleniyordu)", code, b)
	}
	if code, _ := do(t, f.s, http.MethodPost, "/room-blocks", sec, "application/json", body); code != http.StatusConflict {
		t.Fatalf("aynı salon+oturum ikinci kez kapatılamaz: %d", code)
	}
	if code, _ := do(t, f.s, http.MethodPost, "/room-blocks", sec, "application/json", j(map[string]any{"room_id": f.roomID, "slot_id": 999999})); code != http.StatusNotFound {
		t.Fatalf("olmayan oturum 404: %d", code)
	}
	if code, _ := do(t, f.s, http.MethodPost, "/room-blocks", sec, "application/json", j(map[string]any{"room_id": 0, "slot_id": 0})); code != http.StatusUnprocessableEntity {
		t.Fatalf("eksik alan 422: %d", code)
	}
	if code, out := do(t, f.s, http.MethodGet, "/room-blocks", sec, "", ""); code != http.StatusOK || len(out["items"].([]any)) != 1 {
		t.Fatalf("liste: %d %v", code, out)
	}
	if code, _ := do(t, f.s, http.MethodGet, "/room-blocks", f.inv, "", ""); code != http.StatusForbidden {
		t.Fatalf("gözetmen listeyi göremez: %d", code)
	}
	id := itoa(int64(b["id"].(float64)))
	if code, _ := do(t, f.s, http.MethodDelete, "/room-blocks/"+id, f.inv, "", ""); code != http.StatusForbidden {
		t.Fatalf("gözetmen silemez: %d", code)
	}
	if code, _ := do(t, f.s, http.MethodDelete, "/room-blocks/"+id, sec, "", ""); code != http.StatusNoContent {
		t.Fatalf("silme: %d", code)
	}
	if code, _ := do(t, f.s, http.MethodDelete, "/room-blocks/"+id, sec, "", ""); code != http.StatusNotFound {
		t.Fatalf("ikinci silme 404: %d", code)
	}
}

func TestFrontLoadWeightIsValidated(t *testing.T) {
	f := checkinFixture(t)
	sec, _ := login(t, f.s, simdata.EmailSecretary)
	for _, bad := range []int{-1, 101} {
		if code, out := do(t, f.s, http.MethodPost, "/schedules", sec, "application/json", j(map[string]any{"period_id": 1, "front_load": bad})); code != http.StatusUnprocessableEntity {
			t.Fatalf("front_load=%d 422 olmalı: %d %v", bad, code, out)
		}
	}
}
