// Package metrics collects Prometheus metrics for the gateway: request
// latency per route, per-upstream request counts (used for the canary split),
// rate-limited (429) counts and upstream up/status. The handler is mounted on
// the gateway's /metrics endpoint.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics bundles the gateway's collectors.
type Metrics struct {
	Durations        *prometheus.HistogramVec
	UpstreamRequests *prometheus.CounterVec
	RateLimited      *prometheus.CounterVec
	UpstreamUp       *prometheus.GaugeVec
}

// New builds and registers the metric collectors with the default registry.
func New() *Metrics {
	return &Metrics{
		Durations: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "gateway_request_duration_seconds",
			Help:    "Latency of gateway-routed requests by route.",
			Buckets: prometheus.DefBuckets,
		}, []string{"route"}),
		UpstreamRequests: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "gateway_upstream_requests_total",
			Help: "Requests routed to each upstream, by route and upstream.",
		}, []string{"route", "upstream"}),
		RateLimited: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "gateway_rate_limited_total",
			Help: "Requests rejected with 429 by route.",
		}, []string{"route"}),
		UpstreamUp: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "gateway_upstream_up",
			Help: "Whether each upstream is currently healthy (1) or not (0).",
		}, []string{"upstream"}),
	}
}

// ObserveRequest records a routed request's latency and which upstream served
// it.
func (m *Metrics) ObserveRequest(route, upstream string, durSeconds float64) {
	m.UpstreamRequests.WithLabelValues(route, upstream).Inc()
	m.Durations.WithLabelValues(route).Observe(durSeconds)
}

// MarkRateLimited records a 429 rejection for a route.
func (m *Metrics) MarkRateLimited(route string) {
	m.RateLimited.WithLabelValues(route).Inc()
}

// UpdateUpstreams refreshes the upstream up/status gauges from a snapshot of
// the health registry.
func (m *Metrics) UpdateUpstreams(snapshot map[string]bool) {
	for upstream, up := range snapshot {
		var v float64
		if up {
			v = 1
		}
		m.UpstreamUp.WithLabelValues(upstream).Set(v)
	}
}
