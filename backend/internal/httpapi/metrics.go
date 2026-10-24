package httpapi

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Metrics, Prometheus sayaç ve histogramlarını taşır.
type Metrics struct {
	RequestsTotal   *prometheus.CounterVec
	RequestDuration *prometheus.HistogramVec
	InFlight        prometheus.Gauge
}

// NewMetrics, metrikleri verilen registry'ye kaydeder. Go çalışma zamanı ve
// süreç metriklerini de ekliyoruz; kaos senaryosunda bellek/goroutine
// davranışını görmek işe yarıyor.
func NewMetrics(reg *prometheus.Registry) *Metrics {
	m := &Metrics{
		RequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "İşlenen HTTP isteği sayısı.",
		}, []string{"method", "route", "status"}),

		RequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP istek süresi (saniye).",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}, []string{"method", "route"}),

		InFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "http_requests_in_flight",
			Help: "Halen işlenmekte olan HTTP isteği sayısı.",
		}),
	}

	reg.MustRegister(m.RequestsTotal, m.RequestDuration, m.InFlight)
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return m
}

func statusLabel(code int) string { return strconv.Itoa(code) }
