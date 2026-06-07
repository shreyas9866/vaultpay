package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Track total HTTP requests broken down by method, path, and status code
	RequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vaultpay_http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "path", "status"},
	)

	// Track exactly how fast our API is running (for p50 and p99 latency)
	RequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "vaultpay_http_request_duration_seconds",
			Help:    "Histogram of response latency (seconds)",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5},
		},
		[]string{"method", "path"},
	)

	// Business metrics: track money flow success vs. failure
	ChargesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vaultpay_charges_total",
			Help: "Total number of charge attempts",
		},
		[]string{"status"}, // will be "success" or "failed"
	)
)