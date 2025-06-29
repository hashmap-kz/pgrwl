package backupmetrics

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var M bbMetrics = &bbMetricsNoop{}

type bbMetrics interface {
	AddBasebackupBytesReceived(float64)
	AddBasebackupBytesDeleted(float64)
}

// noop

type bbMetricsNoop struct{}

var _ bbMetrics = &bbMetricsNoop{}

func (p bbMetricsNoop) AddBasebackupBytesReceived(_ float64) {}
func (p bbMetricsNoop) AddBasebackupBytesDeleted(_ float64)  {}

// prom

type pgrwlMetricsProm struct {
	// basebackup
	bbBytesReceived prometheus.Counter
	bbBytesDeleted  prometheus.Counter
}

var _ bbMetrics = &pgrwlMetricsProm{}

func InitPromMetrics(_ context.Context) {
	// Unregister default prometheus collectors so we don't collect a bunch of pointless metrics
	prometheus.Unregister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	prometheus.Unregister(collectors.NewGoCollector())

	M = &pgrwlMetricsProm{
		// basebackup
		bbBytesReceived: promauto.NewCounter(prometheus.CounterOpts{
			Name: "pgrwl_basebackup_bytes_received_total",
			Help: "Total number of basebackup bytes received.",
		}),
		bbBytesDeleted: promauto.NewCounter(prometheus.CounterOpts{
			Name: "pgrwl_basebackup_bytes_deleted_total",
			Help: "Total number of basebackup bytes deleted.",
		}),
	}
}

// basebackup

func (p *pgrwlMetricsProm) AddBasebackupBytesReceived(f float64) {
	p.bbBytesReceived.Add(f)
}

func (p *pgrwlMetricsProm) AddBasebackupBytesDeleted(f float64) {
	p.bbBytesDeleted.Add(f)
}
