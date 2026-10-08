package cpmetrics

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	gaugeCacheTTL = 60 * time.Second
	sourceTimeout = 5 * time.Second
)

// WatchAttention registers the gauges of what needs the operator's attention. They are read
// from source at most once every 60 seconds, whatever the rate of the scrapes; scrapes that come
// during a read wait for it. When source fails, the gauges are left out of that scrape, the
// failure is counted in husonym_controlplane_attention_read_failures_total and logged with the
// text of the error of source, and the next scrape tries again.
func (m *Metrics) WatchAttention(
	source func(ctx context.Context) (cpstore.AttentionCounts, error), now func() time.Time, logger *slog.Logger,
) {
	m.registry.MustRegister(&attentionCollector{source: source, now: now, logger: logger})
}

var (
	silentDesc   = prometheus.NewDesc(prefix+"silent_instances", "Instances in force that stopped reporting.", nil, nil)
	expiringDesc = prometheus.NewDesc(prefix+"expiring_licenses", "Licenses expiring soon that no other license succeeds.", nil, nil)
	pendingDesc  = prometheus.NewDesc(prefix+"old_pending_reports",
		fmt.Sprintf("Pending reports received more than %d hours ago.", int(cpstore.OldPendingAfter.Hours())), nil, nil)
	rejectedDesc = prometheus.NewDesc(prefix+"seal_rejections_today", "Reports refused for their seal today (UTC).", nil, nil)
	sharedDesc   = prometheus.NewDesc(prefix+"shared_licenses", "Licenses seen lately on more than one instance.", nil, nil)
	failuresDesc = prometheus.NewDesc(prefix+"attention_read_failures_total", "Reads of the gauges that failed.", nil, nil)
)

type attentionCollector struct {
	source func(ctx context.Context) (cpstore.AttentionCounts, error)
	now    func() time.Time
	logger *slog.Logger

	mu       sync.Mutex // held during a read, so that the scrapes that come meanwhile wait for it
	counts   cpstore.AttentionCounts
	readAt   time.Time
	cached   bool
	failures float64
}

func (c *attentionCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{silentDesc, expiringDesc, pendingDesc, rejectedDesc, sharedDesc, failuresDesc} {
		ch <- d
	}
}

func (c *attentionCollector) Collect(ch chan<- prometheus.Metric) {
	counts, ok, failures := c.read()
	ch <- prometheus.MustNewConstMetric(failuresDesc, prometheus.CounterValue, failures)
	if !ok {
		return
	}
	for _, g := range []struct {
		desc  *prometheus.Desc
		value int
	}{
		{silentDesc, counts.SilentInstances},
		{expiringDesc, counts.ExpiringLicenses},
		{pendingDesc, counts.OldPending},
		{rejectedDesc, counts.SealRejections},
		{sharedDesc, counts.SharedLicenses},
	} {
		ch <- prometheus.MustNewConstMetric(g.desc, prometheus.GaugeValue, float64(g.value))
	}
}

// read gives the counts of the cache, or of a fresh read when the cache is older than the TTL.
// A failed read is not cached.
func (c *attentionCollector) read() (counts cpstore.AttentionCounts, ok bool, failures float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if c.cached && now.Sub(c.readAt) < gaugeCacheTTL {
		return c.counts, true, c.failures
	}
	ctx, cancel := context.WithTimeout(context.Background(), sourceTimeout)
	defer cancel()
	fresh, err := c.source(ctx)
	if err != nil {
		// Our own error: the source reads the database and is given nothing of a caller.
		c.logger.Error("unable to read what needs attention for the gauges", "error", err.Error())
		c.failures++
		c.cached = false
		return cpstore.AttentionCounts{}, false, c.failures
	}
	c.counts, c.readAt, c.cached = fresh, now, true
	return fresh, true, c.failures
}
