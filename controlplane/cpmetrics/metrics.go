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

// New returns the metrics, with the counter of the reports received. The series of each of
// outcomes starts at zero: a rate over a word that was never counted is then 0, where it would
// be absent, and an alert on it can tell "none" from "not measured".
func New(outcomes ...string) *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),
		reports: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: prefix + "usage_reports_total",
			Help: "Report requests received, by outcome.",
		}, []string{"outcome"}),
	}
	m.registry.MustRegister(m.reports)
	for _, outcome := range outcomes {
		m.reports.WithLabelValues(outcome)
	}
	return m
}

// ReportReceived counts one report request. The outcome is one of the fixed words of the
// handler of the public API, never something a caller sent. On a nil *Metrics it counts nothing:
// handed to the handler as its observer, a nil pointer is not a nil interface, and is called.
func (m *Metrics) ReportReceived(outcome string) {
	if m == nil {
		return
	}
	m.reports.WithLabelValues(outcome).Inc()
}

// Handler serves the metrics in the Prometheus text format.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}
