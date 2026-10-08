// Package cpmetrics is what the control plane tells Prometheus: how many reports it received, by
// outcome, and what needs the attention of the operator.
package cpmetrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const prefix = "husonym_controlplane_"

// Metrics holds the registry of the service. It is its own: nothing is registered globally.
type Metrics struct {
	registry *prometheus.Registry
	reports  *prometheus.CounterVec
}

// New returns the metrics, with the counter of the reports received.
func New() *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),
		reports: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: prefix + "usage_reports_total",
			Help: "Report requests received, by outcome.",
		}, []string{"outcome"}),
	}
	m.registry.MustRegister(m.reports)
	return m
}

// ReportReceived counts one report request. The outcome is one of the fixed words of the
// handler of the public API, never something a caller sent.
func (m *Metrics) ReportReceived(outcome string) {
	m.reports.WithLabelValues(outcome).Inc()
}

// Handler serves the metrics in the Prometheus text format.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}
