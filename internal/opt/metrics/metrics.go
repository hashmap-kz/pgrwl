package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

type PgrwlMetrics interface {
	WalBytesReceivedAdd(float64)
	WalSegmentsReceivedInc()
	WalFilesUploadedInc(storageName string)
	WalFilesRetainedInc(storageName string)
}

// factory

type PgrwlMetricsOpts struct {
	Enable bool
}

func NewPgrwlMetrics(o *PgrwlMetricsOpts) PgrwlMetrics {
	if o.Enable {
		return newPgrwlMetricsImpl()
	}
	return &pgrwlMetricsNoop{}
}

// noop

type pgrwlMetricsNoop struct{}

func (m pgrwlMetricsNoop) WalBytesReceivedAdd(_ float64) {}

func (m pgrwlMetricsNoop) WalSegmentsReceivedInc() {}

func (m pgrwlMetricsNoop) WalFilesUploadedInc(_ string) {}

func (m pgrwlMetricsNoop) WalFilesRetainedInc(_ string) {}

var _ PgrwlMetrics = &pgrwlMetricsNoop{}

// impl

type pgrwlMetricsImpl struct {
	walBytesReceived prometheus.Counter
	walFilesReceived prometheus.Counter
	walFilesUploaded *prometheus.CounterVec
	walFilesDeleted  *prometheus.CounterVec
}

var _ PgrwlMetrics = &pgrwlMetricsImpl{}

func newPgrwlMetricsImpl() PgrwlMetrics {
	m := &pgrwlMetricsImpl{}

	// Unregister default prometheus collectors so we don't collect a bunch of pointless metrics
	prometheus.Unregister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	prometheus.Unregister(collectors.NewGoCollector())

	m.walBytesReceived = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "pgrwl_wal_bytes_received_total",
		Help: "Total number of WAL bytes received from PostgreSQL.",
	})

	m.walFilesReceived = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "pgrwl_wal_segments_received_total",
		Help: "Total number of WAL segments received.",
	})

	m.walFilesUploaded = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "pgrwl_wal_files_uploaded_total",
		Help: "Number of WAL files uploaded, partitioned by storage backend.",
	}, []string{"backend"})

	m.walFilesDeleted = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "pgrwl_wal_segments_deleted_total",
		Help: "Number of WAL segments deleted by retention logic.",
	}, []string{"backend"})

	prometheus.MustRegister(
		m.walBytesReceived,
		m.walFilesReceived,
		m.walFilesUploaded,
		m.walFilesDeleted,
	)
	return m
}

func (m *pgrwlMetricsImpl) WalBytesReceivedAdd(v float64) {
	m.walBytesReceived.Add(v)
}

func (m *pgrwlMetricsImpl) WalSegmentsReceivedInc() {
	m.walFilesReceived.Inc()
}

func (m *pgrwlMetricsImpl) WalFilesUploadedInc(storageName string) {
	m.walFilesUploaded.WithLabelValues(storageName).Inc()
}

func (m *pgrwlMetricsImpl) WalFilesRetainedInc(storageName string) {
	m.walFilesUploaded.WithLabelValues(storageName).Inc()
}
