package httpapi

import (
	"context"
	"net/http"
	"time"
)

// Pinger, sağlık kontrolünün bağımlılıklardan beklediği tek davranış.
// Arayüz üzerinden aldığımız için /health testleri gerçek veritabanı
// gerektirmiyor.
type Pinger interface {
	Ping(ctx context.Context) error
}

// healthResponse, /health gövdesi. Hangi bağımlılığın düştüğü ayrı ayrı
// bildirilir; "unhealthy" demek yetmez, operatörün nereye bakacağını
// bilmesi gerekir.
type healthResponse struct {
	Status       string                     `json:"status"`
	Version      string                     `json:"version"`
	Dependencies map[string]dependencyState `json:"dependencies"`
}

type dependencyState struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

const (
	statusHealthy   = "healthy"
	statusUnhealthy = "unhealthy"
)

// healthHandler, veritabanı dahil bağımlılıkları yoklar. Hepsi sağlıklıysa
// 200, biri düştüyse 503 döner.
//
// Sağlık kontrolünün kendi zaman aşımı var: veritabanı yanıt vermiyorsa
// isteğin tamamını bekletmek yerine hızlıca 503 vermeyi tercih ediyoruz,
// çünkü bu ucu compose healthcheck'i ve kaos senaryosu yoklayacak.
func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	resp := healthResponse{
		Status:       statusHealthy,
		Version:      s.version,
		Dependencies: make(map[string]dependencyState, 1),
	}

	if err := s.db.Ping(ctx); err != nil {
		resp.Status = statusUnhealthy
		resp.Dependencies["postgres"] = dependencyState{
			Status:  statusUnhealthy,
			Message: "veritabanına ulaşılamıyor",
		}
	} else {
		resp.Dependencies["postgres"] = dependencyState{Status: statusHealthy}
	}

	status := http.StatusOK
	if resp.Status == statusUnhealthy {
		status = http.StatusServiceUnavailable
	}
	WriteJSON(w, status, resp)
}
