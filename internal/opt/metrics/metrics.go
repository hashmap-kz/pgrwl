package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var PgrwlMetricsCollector pgrwlMetrics = &pgrwlMetricsNoop{}

type pgrwlMetrics interface {
	MetricsEnabled() bool
	AddWALBytesReceived(float64)
	IncWALFilesReceived()
	IncWALFilesUploaded(storageName string)
	IncWALFilesDeleted(storageName string)
}

// noop

type pgrwlMetricsNoop struct{}

var _ pgrwlMetrics = &pgrwlMetricsNoop{}

func (p pgrwlMetricsNoop) MetricsEnabled() bool { return false }

func (p pgrwlMetricsNoop) AddWALBytesReceived(_ float64) {}

func (p pgrwlMetricsNoop) IncWALFilesReceived() {}

func (p pgrwlMetricsNoop) IncWALFilesUploaded(_ string) {}

func (p pgrwlMetricsNoop) IncWALFilesDeleted(_ string) {}

// prom

type pgrwlMetricsProm struct {
	WALBytesReceived prometheus.Counter
	WALFilesReceived prometheus.Counter
	WALFilesUploaded *prometheus.CounterVec
	WALFilesDeleted  *prometheus.CounterVec
}

var _ pgrwlMetrics = &pgrwlMetricsProm{}

func InitPromMetrics() {
	// Unregister default prometheus collectors so we don't collect a bunch of pointless metrics
	prometheus.Unregister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	prometheus.Unregister(collectors.NewGoCollector())

	PgrwlMetricsCollector = &pgrwlMetricsProm{
		WALBytesReceived: promauto.NewCounter(prometheus.CounterOpts{
			Name: "pgrwl_wal_bytes_received_total",
			Help: "Total number of WAL bytes received from PostgreSQL.",
		}),
		WALFilesReceived: promauto.NewCounter(prometheus.CounterOpts{
			Name: "pgrwl_wal_files_received_total",
			Help: "Total number of WAL segments received.",
		}),
		WALFilesUploaded: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "pgrwl_wal_files_uploaded_total",
			Help: "Number of WAL files uploaded, partitioned by storage backend.",
		}, []string{"backend"}),
		WALFilesDeleted: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "pgrwl_wal_files_deleted_total",
			Help: "Number of WAL segments deleted by retention logic.",
		}, []string{"backend"}),
	}
}

func (p *pgrwlMetricsProm) MetricsEnabled() bool {
	return true
}

func (p *pgrwlMetricsProm) AddWALBytesReceived(f float64) {
	p.WALBytesReceived.Add(f)
}

func (p *pgrwlMetricsProm) IncWALFilesReceived() {
	p.WALFilesReceived.Inc()
}

func (p *pgrwlMetricsProm) IncWALFilesUploaded(storageName string) {
	p.WALFilesUploaded.WithLabelValues(storageName).Inc()
}

func (p *pgrwlMetricsProm) IncWALFilesDeleted(storageName string) {
	p.WALFilesDeleted.WithLabelValues(storageName).Inc()
}
