package console_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/console"
	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/controlplane/cptest"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
)

// hrefTo finds in a page the one link whose href matches pattern, and gives the href.
func hrefTo(t *testing.T, body, pattern string) string {
	t.Helper()
	found := regexp.MustCompile(`href="(`+pattern+`)"`).FindAllStringSubmatch(body, -1)
	hrefs := map[string]bool{}
	for _, match := range found {
		hrefs[match[1]] = true
	}
	require.Len(t, hrefs, 1, "one link matches %s", pattern)
	return found[0][1]
}

// The console over a real database: from what needs attention to the document of a report,
// following only the links of the pages.
func Test_Console_WalksFromWhatNeedsAttentionToAReport(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	pool := cptest.NewDatabase(t)
	store := cpstore.New(pool)
	issuer := cptest.NewIssuer(t)
	now := time.Now().UTC()
	maxSources := 2
	entry := issuer.EntryFor(&license.IssueRequest{
		Id:         "lic-walk",
		IssuedTo:   `Acme <script>alert(1)</script>`,
		CustomerId: "cust-walk",
		ExpiresAt:  now.Add(365 * 24 * time.Hour),
		Telemetry:  string(license.TelemetryOnline),
		Limits:     &license.Limits{MaxSources: &maxSources},
	})
	cptest.AddLicense(t, store, issuer, &entry)
	// Its last report is ten days old: the instance is silent. The report tells three sources.
	reportDay := now.AddDate(0, 0, -10)
	cptest.StoreReport(t, store, &entry, instanceOne, reportDay, reportDay.Add(26*time.Hour))

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pages, err := console.New(store, time.Now, logger)
	require.NoError(t, err)
	handler := access(t).Gate(t, time.Now, logger).Wrap(pages)
	fetch := func(path string) page {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set(cptest.AccessHeader, access(t).Token(t, operatorEmail, time.Now()))
		got := serve(handler, req)
		require.Equal(t, http.StatusOK, got.status, path)
		require.NotContains(t, got.body, entry.Encoded, "the key is on no page")
		require.NotContains(t, got.body, "<script")
		return got
	}

	attention := fetch("/")
	silent := section(t, attention.body, "silent-instances")
	require.Contains(t, silent, `Silent instances <span class="count">1</span>`)
	require.Contains(t, silent, "Acme &lt;script&gt;alert(1)&lt;/script&gt;")

	instance := fetch(hrefTo(t, silent, `/licenses/lic-walk/instances/[^"/]+`))
	require.Contains(t, instance.body, `<h1 class="id">`+instanceOne+`</h1>`)
	require.Contains(t, between(t, instance.body, "<dt>Source cap</dt>", "</dd>"), "2")
	reportRow := row(t, instance.body, "/reports/")
	require.Contains(t, reportRow, "3 / 2")
	require.Contains(t, reportRow, "over the cap")

	report := fetch(hrefTo(t, instance.body, `/licenses/lic-walk/instances/[^"/]+/reports/[^"/]+`))
	require.Contains(t, report.body, "<h1>Report of "+reportDay.Format(time.DateOnly)+"</h1>")
	document := between(t, report.body, "<pre", "</pre>")
	require.Contains(t, document, "&#34;instance_id&#34;: &#34;"+instanceOne+"&#34;")
	require.Contains(t, document, "\n  &#34;sources&#34;: {\n")

	licensePage := fetch(hrefTo(t, between(t, report.body, "<dl", "</dl>"), `/licenses/[^"/]+`))
	require.Contains(t, licensePage.body, `<h1 class="id">lic-walk</h1>`)
	require.Contains(t, between(t, licensePage.body, "max_sources", "</dd>"), "2")

	customer := fetch(hrefTo(t, between(t, licensePage.body, "<dl", "</dl>"), `/customers/[^"/]+`))
	require.Contains(t, customer.body, "<h1>Acme &lt;script&gt;alert(1)&lt;/script&gt;</h1>")
	require.Contains(t, customer.body, "cust-walk")
	require.Contains(t, row(t, customer.body, `href="/licenses/lic-walk"`), ">valid<")

	customers := fetch("/customers")
	require.Contains(t, row(t, customers.body, "cust-walk"), hrefTo(t, customers.body, `/customers/[^"/]+`))

	require.Contains(t, fetch("/pending").body, "Nothing.")
}
