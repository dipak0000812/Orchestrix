package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus metrics.
type Metrics struct {
	JobsCreated   prometheus.Counter
	JobsSucceeded prometheus.Counter
	JobsFailed    prometheus.Counter
	JobsCancelled prometheus.Counter
	JobDuration   prometheus.Histogram
	QueueDepth    prometheus.Gauge
	HTTPRequests  *prometheus.CounterVec
}

// NewMetrics creates and registers all metrics.
func NewMetrics() *Metrics {
	return &Metrics{
		JobsCreated: promauto.NewCounter(prometheus.CounterOpts{
			Name: "orchestrix_jobs_created_total",
			Help: "Total number of jobs created",
		}),
		JobsSucceeded: promauto.NewCounter(prometheus.CounterOpts{
			Name: "orchestrix_jobs_succeeded_total",
			Help: "Total number of jobs that succeeded",
		}),
		JobsFailed: promauto.NewCounter(prometheus.CounterOpts{
			Name: "orchestrix_jobs_failed_total",
			Help: "Total number of jobs that failed",
		}),
		JobsCancelled: promauto.NewCounter(prometheus.CounterOpts{
			Name: "orchestrix_jobs_cancelled_total",
			Help: "Total number of jobs cancelled",
		}),
		JobDuration: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "orchestrix_job_duration_seconds",
			Help:    "Job execution duration in seconds",
			Buckets: prometheus.DefBuckets,
		}),
		QueueDepth: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "orchestrix_queue_depth",
			Help: "Current number of jobs in queue",
		}),
		HTTPRequests: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "orchestrix_http_requests_total",
				Help: "Total HTTP requests by endpoint and status",
			},
			[]string{"method", "endpoint", "status"},
		),
	}
}
