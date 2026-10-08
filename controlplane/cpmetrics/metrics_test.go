package cpmetrics_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/cpmetrics"
	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/stretchr/testify/require"
)

func scrape(t *testing.T, m *cpmetrics.Metrics) (status int, body string) {
	t.Helper()
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	return rec.Code, rec.Body.String()
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func Test_ReportReceived_CountsPerOutcome(t *testing.T) {
	m := cpmetrics.New()
	m.ReportReceived("stored")
	m.ReportReceived("stored")
	m.ReportReceived("refused")

	status, body := scrape(t, m)

	require.Equal(t, http.StatusOK, status)
	require.Contains(t, body, `husonym_controlplane_usage_reports_total{outcome="stored"} 2`)
	require.Contains(t, body, `husonym_controlplane_usage_reports_total{outcome="refused"} 1`)
}

func Test_WatchAttention_ExposesTheFiveGauges(t *testing.T) {
	m := cpmetrics.New()
	m.WatchAttention(func(context.Context) (cpstore.AttentionCounts, error) {
		return cpstore.AttentionCounts{SilentInstances: 1, ExpiringLicenses: 2, OldPending: 3, SealRejections: 4, SharedLicenses: 5}, nil
	}, time.Now)

	_, body := scrape(t, m)

	for _, line := range []string{
		"husonym_controlplane_silent_instances 1",
		"husonym_controlplane_expiring_licenses 2",
		"husonym_controlplane_old_pending_reports 3",
		"husonym_controlplane_seal_rejections_today 4",
		"husonym_controlplane_shared_licenses 5",
		"husonym_controlplane_attention_read_failures_total 0",
	} {
		require.Contains(t, body, line)
	}
}

func Test_WatchAttention_ReadsAtMostOncePer60Seconds(t *testing.T) {
	clk := &clock{t: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)}
	var calls atomic.Int32
	m := cpmetrics.New()
	m.WatchAttention(func(context.Context) (cpstore.AttentionCounts, error) {
		return cpstore.AttentionCounts{SilentInstances: int(calls.Add(1))}, nil
	}, clk.now)

	scrape(t, m)
	clk.advance(59 * time.Second)
	_, body := scrape(t, m)
	require.Equal(t, int32(1), calls.Load())
	require.Contains(t, body, "husonym_controlplane_silent_instances 1")

	clk.advance(2 * time.Second)
	_, body = scrape(t, m)
	require.Equal(t, int32(2), calls.Load())
	require.Contains(t, body, "husonym_controlplane_silent_instances 2")
}

func Test_WatchAttention_ConcurrentScrapesShareOneRead(t *testing.T) {
	var calls atomic.Int32
	m := cpmetrics.New()
	m.WatchAttention(func(context.Context) (cpstore.AttentionCounts, error) {
		calls.Add(1)
		time.Sleep(50 * time.Millisecond)
		return cpstore.AttentionCounts{}, nil
	}, time.Now)

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { scrape(t, m) })
	}
	wg.Wait()

	require.Equal(t, int32(1), calls.Load())
}

func Test_WatchAttention_FailingSource_KeepsAnsweringAndIsNotCached(t *testing.T) {
	var calls atomic.Int32
	m := cpmetrics.New()
	m.ReportReceived("stored")
	m.WatchAttention(func(context.Context) (cpstore.AttentionCounts, error) {
		if calls.Add(1) <= 2 {
			return cpstore.AttentionCounts{}, errors.New("database down")
		}
		return cpstore.AttentionCounts{SilentInstances: 7}, nil
	}, time.Now)

	status, body := scrape(t, m)
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, body, `husonym_controlplane_usage_reports_total{outcome="stored"} 1`)
	require.Contains(t, body, "husonym_controlplane_attention_read_failures_total 1")
	require.NotContains(t, body, "husonym_controlplane_silent_instances")
	require.NotContains(t, body, "database down")

	_, body = scrape(t, m)
	require.Contains(t, body, "husonym_controlplane_attention_read_failures_total 2")

	_, body = scrape(t, m)
	require.Equal(t, int32(3), calls.Load())
	require.Contains(t, body, "husonym_controlplane_silent_instances 7")
	require.Contains(t, body, "husonym_controlplane_attention_read_failures_total 2")
}

func Test_WatchAttention_SourceGetsAContextWithADeadline(t *testing.T) {
	m := cpmetrics.New()
	var remaining time.Duration
	m.WatchAttention(func(ctx context.Context) (cpstore.AttentionCounts, error) {
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		remaining = time.Until(deadline)
		return cpstore.AttentionCounts{}, nil
	}, time.Now)

	scrape(t, m)

	require.Positive(t, remaining)
	require.LessOrEqual(t, remaining, 5*time.Second)
}

func Test_Metrics_UseTheirOwnRegistry(t *testing.T) {
	_, body := scrape(t, cpmetrics.New())

	require.False(t, strings.Contains(body, "go_goroutines"), "no Go collector of the default registry")
}
