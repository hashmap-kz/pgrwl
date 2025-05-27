package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// TODO: when metrics disabled by config, we should totally ellimitate any inits

var (
	// Replication metrics

	WALBytesReceived = promauto.NewCounter(prometheus.CounterOpts{
		Name: "pgrwl_wal_bytes_received_total",
		Help: "Total number of WAL bytes received from PostgreSQL.",
	})

	WALSegmentsReceived = promauto.NewCounter(prometheus.CounterOpts{
		Name: "pgrwl_wal_segments_received_total",
		Help: "Total number of WAL segments received.",
	})

	// Supervisor (upload, retain)

	WALFilesUploaded = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "pgrwl_wal_files_uploaded_total",
		Help: "Number of WAL files uploaded, partitioned by storage backend.",
	}, []string{"backend"})

	// Storage metrics

	WALDiskUsage = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "pgrwl_wal_disk_usage_bytes",
		Help: "Disk space used by all stored WAL segments.",
	})

	// Retention and cleanup

	WALSegmentsDeleted = promauto.NewCounter(prometheus.CounterOpts{
		Name: "pgrwl_wal_segments_deleted_total",
		Help: "Number of WAL segments deleted by retention logic.",
	})

	WALRetentionDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "pgrwl_wal_retention_run_duration_seconds",
		Help:    "Duration of the WAL retention cleanup run.",
		Buckets: prometheus.ExponentialBuckets(0.5, 2, 10),
	})

	// Connection/errors

	WALStreamErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "pgrwl_wal_stream_errors_total",
		Help: "Errors during WAL stream reading.",
	})

	WALDisconnects = promauto.NewCounter(prometheus.CounterOpts{
		Name: "pgrwl_wal_pg_server_disconnects_total",
		Help: "Unexpected disconnects from PostgreSQL server.",
	})

	WALReconnects = promauto.NewCounter(prometheus.CounterOpts{
		Name: "pgrwl_wal_pg_connection_retries_total",
		Help: "Number of reconnection attempts to PostgreSQL.",
	})
)

func init() {
	// Unregister default prometheus collectors so we don't collect a bunch of pointless metrics
	prometheus.Unregister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	prometheus.Unregister(collectors.NewGoCollector())
}
