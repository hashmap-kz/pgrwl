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
	AddWALFilesDeleted(storageName string, f float64)
}

// noop

type pgrwlMetricsNoop struct{}

var _ pgrwlMetrics = &pgrwlMetricsNoop{}

func (p pgrwlMetricsNoop) MetricsEnabled() bool                   { return false }
func (p pgrwlMetricsNoop) AddWALBytesReceived(_ float64)          {}
func (p pgrwlMetricsNoop) IncWALFilesReceived()                   {}
func (p pgrwlMetricsNoop) IncWALFilesUploaded(_ string)           {}
func (p pgrwlMetricsNoop) IncWALFilesDeleted(_ string)            {}
func (p pgrwlMetricsNoop) AddWALFilesDeleted(_ string, _ float64) {}

// prom

type pgrwlMetricsProm struct {
	walBytesReceived prometheus.Counter
	walFilesReceived prometheus.Counter
	walFilesUploaded *prometheus.CounterVec
	walFilesDeleted  *prometheus.CounterVec
}

var _ pgrwlMetrics = &pgrwlMetricsProm{}

func InitPromMetrics() {
	// Unregister default prometheus collectors so we don't collect a bunch of pointless metrics
	prometheus.Unregister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	prometheus.Unregister(collectors.NewGoCollector())

	PgrwlMetricsCollector = &pgrwlMetricsProm{
		walBytesReceived: promauto.NewCounter(prometheus.CounterOpts{
			Name: "pgrwl_wal_bytes_received_total",
			Help: "Total number of WAL bytes received from PostgreSQL.",
		}),
		walFilesReceived: promauto.NewCounter(prometheus.CounterOpts{
			Name: "pgrwl_wal_files_received_total",
			Help: "Total number of WAL segments received.",
		}),
		walFilesUploaded: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "pgrwl_wal_files_uploaded_total",
			Help: "Number of WAL files uploaded, partitioned by storage backend.",
		}, []string{"backend"}),
		walFilesDeleted: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "pgrwl_wal_files_deleted_total",
			Help: "Number of WAL segments deleted by retention logic.",
		}, []string{"backend"}),
	}
}

func (p *pgrwlMetricsProm) MetricsEnabled() bool {
	return true
}

func (p *pgrwlMetricsProm) AddWALBytesReceived(f float64) {
	p.walBytesReceived.Add(f)
}

func (p *pgrwlMetricsProm) IncWALFilesReceived() {
	p.walFilesReceived.Inc()
}

func (p *pgrwlMetricsProm) IncWALFilesUploaded(storageName string) {
	p.walFilesUploaded.WithLabelValues(storageName).Inc()
}

func (p *pgrwlMetricsProm) IncWALFilesDeleted(storageName string) {
	p.walFilesDeleted.WithLabelValues(storageName).Inc()
}

func (p *pgrwlMetricsProm) AddWALFilesDeleted(storageName string, f float64) {
	p.walFilesDeleted.WithLabelValues(storageName).Add(f)
}
