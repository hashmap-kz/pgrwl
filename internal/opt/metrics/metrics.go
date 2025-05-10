package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	// ReceivedBytes Counter for total bytes received
	ReceivedBytes = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "wal_streaming_bytes_total",
		Help: "Total bytes streamed from PostgreSQL",
	})

	// StreamingActive Gauge to indicate whether streaming is active (1 = running, 0 = stopped)
	StreamingActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "wal_streaming_active",
		Help: "WAL streaming activity status",
	})
)

func Init() {
	// Register metrics with Prometheus
	prometheus.MustRegister(
		ReceivedBytes,
		StreamingActive,
	)
}
