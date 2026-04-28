package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	HTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests made.",
		},
		[]string{"method", "path", "status"},
	)

	HTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "http_request_durations_seconds",
			Help: "Duration of HTTP requests.",
		},
		[]string{"method", "path"},
	)

	BuildDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "build_duration_seconds",
			Help:    "Time to build and upload the program.",
			Buckets: []float64{0.5, 1, 2, 5, 10, 15},
		},
	)

	BuildErrorsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "build_errors_total",
			Help: "Number of failed builds.",
		},
	)

	ActiveSessions = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "active_sessions",
			Help: "Current number of active debug sessions.",
		},
	)

	DebugCommandDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "debug_command_duration_seconds",
			Help:    "Time to execute a debug command.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"command"},
	)

	DebugCommandErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "debug_command_errors_total",
			Help: "Total number of failed debug commands.",
		},
		[]string{"command"},
	)
)
