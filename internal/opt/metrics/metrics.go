package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

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

	WALCurrentLSN = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "pgrwl_wal_stream_lsn_current",
		Help: "Current LSN position received from the WAL stream.",
	})

	WALLagBytes = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "pgrwl_wal_stream_lsn_lag_bytes",
		Help: "Lag in bytes between received LSN and reported server LSN.",
	})

	WALTimeline = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "pgrwl_wal_stream_timeline",
		Help: "Current timeline ID being streamed.",
	})

	// Supervisor (upload, retain)
	WALFilesUploaded = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "pgrwl_wal_files_uploaded_total",
		Help: "Number of WAL files uploaded, partitioned by storage backend.",
	}, []string{"backend"})

	// Performance histograms
	WALSegmentDownloadDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "pgrwl_wal_segment_download_duration_seconds",
		Help:    "Duration of downloading and writing each WAL segment.",
		Buckets: prometheus.ExponentialBuckets(0.1, 2, 10),
	})

	WALWriteLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "pgrwl_wal_stream_write_latency_seconds",
		Help:    "Time taken to write WAL data to disk.",
		Buckets: prometheus.ExponentialBuckets(0.001, 2, 10),
	})

	WALFlushLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "pgrwl_wal_stream_flush_latency_seconds",
		Help:    "Time spent flushing WAL data to disk.",
		Buckets: prometheus.ExponentialBuckets(0.001, 2, 10),
	})

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

	// Optional encryption/compression (integration with storecrypt)
	WALCompressionErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "pgrwl_wal_compression_errors_total",
		Help: "Errors during compression of WAL segments.",
	})

	WALEncryptionErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "pgrwl_wal_encryption_errors_total",
		Help: "Errors during encryption of WAL segments.",
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

	// Application health
	AppUptime = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "pgrwl_app_uptime_seconds",
		Help: "Seconds since the application started.",
	})

	Goroutines = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "pgrwl_goroutines",
		Help: "Number of current goroutines.",
	})
)
