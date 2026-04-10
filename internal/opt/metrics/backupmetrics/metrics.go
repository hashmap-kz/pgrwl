package backupmetrics

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var M bbMetrics = &bbMetricsNoop{}

type bbMetrics interface {
	AddBasebackupBytesReceived(float64)
	AddBasebackupBytesDeleted(float64)
	BackupStarted(id, cron string, startTime time.Time)
	BackupFinished(id string, duration time.Duration)
	BackupFailed(id string)
	SetCronSchedule(cron string)
}

type ActiveBackup struct {
	ID        string
	Cron      string
	StartTime time.Time
}

// noop

type bbMetricsNoop struct{}

var _ bbMetrics = &bbMetricsNoop{}

func (p bbMetricsNoop) AddBasebackupBytesReceived(_ float64)     {}
func (p bbMetricsNoop) AddBasebackupBytesDeleted(_ float64)      {}
func (p bbMetricsNoop) BackupStarted(_, _ string, _ time.Time)   {}
func (p bbMetricsNoop) BackupFinished(_ string, _ time.Duration) {}
func (p bbMetricsNoop) BackupFailed(_ string)                    {}
func (p bbMetricsNoop) SetCronSchedule(_ string)                 {}

// prom

type pgrwlMetricsProm struct {
	// basebackup
	bbBytesReceived prometheus.Counter
	bbBytesDeleted  prometheus.Counter
	bbDuration      prometheus.Histogram
	bbStarted       prometheus.Counter
	bbFailed        prometheus.Counter
	bbActive        prometheus.Gauge
	bbStuck         prometheus.Gauge
	bbCronSchedule  prometheus.Gauge
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
		bbDuration: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "pgrwl_basebackup_duration_seconds",
			Help:    "Basebackup streaming duration in seconds.",
			Buckets: []float64{60, 300, 600, 1800, 3600, 7200, 14400, 28800, 57600},
		}),
		bbStarted: promauto.NewCounter(prometheus.CounterOpts{
			Name: "pgrwl_basebackup_starts_total",
			Help: "Total number of basebackup operations started.",
		}),
		bbFailed: promauto.NewCounter(prometheus.CounterOpts{
			Name: "pgrwl_basebackup_failures_total",
			Help: "Total number of failed basebackup operations.",
		}),
		bbActive: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "pgrwl_basebackup_in_progress",
			Help: "Number of currently active basebackup operations.",
		}),
		bbStuck: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "pgrwl_basebackup_stuck_count",
			Help: "Number of stuck basebackup operations (exceeded threshold).",
		}),
		bbCronSchedule: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "pgrwl_basebackup_schedule_info",
			Help: "Current backup cron schedule (value is always 1).",
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

func (p *pgrwlMetricsProm) BackupStarted(id, cron string, startTime time.Time) {
	p.bbStarted.Inc()
	p.bbActive.Inc()
	p.bbStuck.Set(float64(Tracker.CountStuck(StuckThresholdDefault)))
	Tracker.Start(id, cron, startTime)
}

func (p *pgrwlMetricsProm) BackupFinished(id string, duration time.Duration) {
	p.bbDuration.Observe(duration.Seconds())
	p.bbActive.Dec()
	p.bbStuck.Set(float64(Tracker.CountStuck(StuckThresholdDefault)))
	Tracker.Finish(id)
}

func (p *pgrwlMetricsProm) BackupFailed(id string) {
	p.bbFailed.Inc()
	p.bbActive.Dec()
	p.bbStuck.Set(float64(Tracker.CountStuck(StuckThresholdDefault)))
	Tracker.Finish(id)
}

func (p *pgrwlMetricsProm) SetCronSchedule(cron string) {
	p.bbCronSchedule.Set(1)
	_ = cron
}
