package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var M pgrwlMetrics = &pgrwlMetricsNoop{}

type pgrwlMetrics interface {
	MetricsEnabled() bool
	AddWALBytesReceived(float64)
	IncWALFilesReceived()
	IncWALFilesUploaded()
	IncWALFilesDeleted()
	AddWALFilesDeleted(f float64)
	IncJobsSubmitted(name string)
	IncJobsExecuted(name string)
	IncJobsDropped(name string)
	ObserveJobDuration(name string, f float64)
}

// noop

type pgrwlMetricsNoop struct{}

var _ pgrwlMetrics = &pgrwlMetricsNoop{}

func (p pgrwlMetricsNoop) MetricsEnabled() bool                   { return false }
func (p pgrwlMetricsNoop) AddWALBytesReceived(_ float64)          {}
func (p pgrwlMetricsNoop) IncWALFilesReceived()                   {}
func (p pgrwlMetricsNoop) IncWALFilesUploaded()                   {}
func (p pgrwlMetricsNoop) IncWALFilesDeleted()                    {}
func (p pgrwlMetricsNoop) AddWALFilesDeleted(_ float64)           {}
func (p pgrwlMetricsNoop) IncJobsSubmitted(_ string)              {}
func (p pgrwlMetricsNoop) IncJobsExecuted(_ string)               {}
func (p pgrwlMetricsNoop) IncJobsDropped(_ string)                {}
func (p pgrwlMetricsNoop) ObserveJobDuration(_ string, _ float64) {}

// prom

type pgrwlMetricsProm struct {
	walBytesReceived prometheus.Counter
	walFilesReceived prometheus.Counter
	walFilesUploaded prometheus.Counter
	walFilesDeleted  prometheus.Counter

	// job-queue
	jobsSubmitted *prometheus.CounterVec
	jobsExecuted  *prometheus.CounterVec
	jobsDropped   *prometheus.CounterVec
	jobDuration   *prometheus.HistogramVec
}

var _ pgrwlMetrics = &pgrwlMetricsProm{}

func InitPromMetrics() {
	// Unregister default prometheus collectors so we don't collect a bunch of pointless metrics
	prometheus.Unregister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	prometheus.Unregister(collectors.NewGoCollector())

	M = &pgrwlMetricsProm{
		walBytesReceived: promauto.NewCounter(prometheus.CounterOpts{
			Name: "pgrwl_wal_bytes_received_total",
			Help: "Total number of WAL bytes received from PostgreSQL.",
		}),
		walFilesReceived: promauto.NewCounter(prometheus.CounterOpts{
			Name: "pgrwl_wal_files_received_total",
			Help: "Total number of WAL segments received.",
		}),
		walFilesUploaded: promauto.NewCounter(prometheus.CounterOpts{
			Name: "pgrwl_wal_files_uploaded_total",
			Help: "Number of WAL files uploaded, partitioned by storage backend.",
		}),
		walFilesDeleted: promauto.NewCounter(prometheus.CounterOpts{
			Name: "pgrwl_wal_files_deleted_total",
			Help: "Number of WAL segments deleted by retention logic.",
		}),

		// job-queue
		jobsSubmitted: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "pgrwl_jobq_jobs_submitted_total",
			Help: "Total number of jobs submitted to the queue.",
		}, []string{"name"}),
		jobsExecuted: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "pgrwl_jobq_jobs_executed_total",
			Help: "Total number of jobs executed from the queue.",
		}, []string{"name"}),
		jobsDropped: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "pgrwl_jobq_jobs_dropped_total",
			Help: "Number of jobs dropped (duplicate or queue full).",
		}, []string{"name", "reason"}),
		jobDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "pgrwl_jobq_job_duration_seconds",
			Help:    "Duration of job executions.",
			Buckets: prometheus.DefBuckets,
		}, []string{"name"}),
	}
}

func (p *pgrwlMetricsProm) MetricsEnabled() bool {
	return true
}

// receive, manage, etc...

func (p *pgrwlMetricsProm) AddWALBytesReceived(f float64) {
	p.walBytesReceived.Add(f)
}

func (p *pgrwlMetricsProm) IncWALFilesReceived() {
	p.walFilesReceived.Inc()
}

func (p *pgrwlMetricsProm) IncWALFilesUploaded() {
	p.walFilesUploaded.Inc()
}

func (p *pgrwlMetricsProm) IncWALFilesDeleted() {
	p.walFilesDeleted.Inc()
}

func (p *pgrwlMetricsProm) AddWALFilesDeleted(f float64) {
	p.walFilesDeleted.Add(f)
}

// job-queue

func (p *pgrwlMetricsProm) IncJobsSubmitted(name string) {
	p.jobsSubmitted.WithLabelValues(name).Inc()
}

func (p *pgrwlMetricsProm) IncJobsExecuted(name string) {
	p.jobsExecuted.WithLabelValues(name).Inc()
}

func (p *pgrwlMetricsProm) IncJobsDropped(name string) {
	p.jobsDropped.WithLabelValues(name).Inc()
}

func (p *pgrwlMetricsProm) ObserveJobDuration(name string, f float64) {
	p.jobDuration.WithLabelValues(name).Observe(f)
}
