package console_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/console"
	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/controlplane/cptest"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

const (
	operatorEmail = "operator@example.com"
	instanceOne   = "123e4567-e89b-12d3-a456-426614174000"
	instanceTwo   = "223e4567-e89b-12d3-a456-426614174000"
	fingerprint   = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

var (
	today      = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	customerID = uuid.MustParse("9f8b1c1e-5d0a-4a3b-8d53-0c6f1a2b3c4d")
)

// fakeStore answers what it was given and remembers what it was asked. A nil answer is ErrNotFound.
type fakeStore struct {
	customers []cpstore.CustomerSummary
	customer  *cpstore.CustomerDetail
	license   *cpstore.LicenseDetail
	instance  *cpstore.InstanceDetail
	report    *cpstore.StoredReport
	pending   []cpstore.PendingGroup
	attention *cpstore.Attention
	journal   []cpstore.OperatorAction
	err       error

	calls       int
	askedAt     time.Time
	askedID     uuid.UUID
	askedFor    []string
	askedForDay time.Time
	askedLimit  int
}

func (f *fakeStore) Customers(_ context.Context, now time.Time) ([]cpstore.CustomerSummary, error) {
	f.calls++
	f.askedAt = now
	return f.customers, f.err
}

func (f *fakeStore) Customer(_ context.Context, id uuid.UUID, now time.Time) (*cpstore.CustomerDetail, error) {
	f.calls++
	f.askedAt, f.askedID = now, id
	return answer(f.customer, f.err)
}

func (f *fakeStore) LicenseDetail(_ context.Context, id string, now time.Time) (*cpstore.LicenseDetail, error) {
	f.calls++
	f.askedAt, f.askedFor = now, []string{id}
	return answer(f.license, f.err)
}

func (f *fakeStore) Instance(_ context.Context, licenseID, instanceID string) (*cpstore.InstanceDetail, error) {
	f.calls++
	f.askedFor = []string{licenseID, instanceID}
	return answer(f.instance, f.err)
}

func (f *fakeStore) Report(_ context.Context, licenseID, instanceID string, day time.Time) (*cpstore.StoredReport, error) {
	f.calls++
	f.askedFor, f.askedForDay = []string{licenseID, instanceID}, day
	return answer(f.report, f.err)
}

func (f *fakeStore) PendingByFingerprint(_ context.Context, now time.Time) ([]cpstore.PendingGroup, error) {
	f.calls++
	f.askedAt = now
	return f.pending, f.err
}

func (f *fakeStore) Attention(_ context.Context, now time.Time) (*cpstore.Attention, error) {
	f.calls++
	f.askedAt = now
	return answer(f.attention, f.err)
}

func (f *fakeStore) Journal(_ context.Context, limit int) ([]cpstore.OperatorAction, error) {
	f.calls++
	f.askedLimit = limit
	return f.journal, f.err
}

func answer[T any](value *T, err error) (*T, error) {
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, cpstore.ErrNotFound
	}
	return value, nil
}

var (
	accessOnce   sync.Once
	sharedAccess *cptest.Access
)

// access is the one Access application the tests of this package share.
func access(t *testing.T) *cptest.Access {
	t.Helper()
	accessOnce.Do(func() { sharedAccess = cptest.NewAccess(t) })
	return sharedAccess
}

// bench is a console over a fake store, a fake signer and a fake promoter, behind the Access gate,
// and what it logged.
type bench struct {
	t        *testing.T
	store    *fakeStore
	writer   *fakeWriter
	signer   *fakeSigner
	promoter *fakePromoter
	handler  http.Handler
	logs     *bytes.Buffer
}

// newBench is a console that issues licenses.
func newBench(t *testing.T) *bench {
	t.Helper()
	return newBenchWith(t, true)
}

// newBenchWith is a console that issues licenses, or one without a signer.
func newBenchWith(t *testing.T, issuing bool) *bench {
	t.Helper()
	b := &bench{
		t: t, store: &fakeStore{attention: &cpstore.Attention{}}, writer: &fakeWriter{}, signer: &fakeSigner{},
		promoter: &fakePromoter{}, logs: &bytes.Buffer{},
	}
	logger := slog.New(slog.NewTextHandler(b.logs, nil))
	cfg := &console.Config{Reader: b.store, Writer: b.writer, Now: func() time.Time { return today }, Logger: logger}
	if issuing {
		cfg.Signer, cfg.Promoter = b.signer, b.promoter
	}
	pages, err := console.New(cfg)
	require.NoError(t, err)
	b.handler = access(t).Gate(t, func() time.Time { return today }, logger).Wrap(pages)
	return b
}

type page struct {
	status int
	header http.Header
	body   string
}

func (b *bench) do(method, path string) page {
	b.t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set(cptest.AccessHeader, access(b.t).Token(b.t, operatorEmail, today))
	return serve(b.handler, req)
}

func (b *bench) get(path string) page {
	b.t.Helper()
	return b.do(http.MethodGet, path)
}

func serve(handler http.Handler, req *http.Request) page {
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return page{status: rec.Code, header: rec.Header(), body: rec.Body.String()}
}

// between is the part of text from the first start to the first end after it.
func between(t *testing.T, text, start, end string) string {
	t.Helper()
	from := strings.Index(text, start)
	require.GreaterOrEqual(t, from, 0, "%q is in the page", start)
	length := strings.Index(text[from:], end)
	require.GreaterOrEqual(t, length, 0, "%q follows %q", end, start)
	return text[from : from+length]
}

// section is the section of a page that has the id.
func section(t *testing.T, body, id string) string {
	t.Helper()
	return between(t, body, `<section id="`+id+`"`, "</section>")
}

// row is the table row of a page that holds marker.
func row(t *testing.T, body, marker string) string {
	t.Helper()
	at := strings.Index(body, marker)
	require.GreaterOrEqual(t, at, 0, "%q is in the page", marker)
	from := strings.LastIndex(body[:at], "<tr")
	require.GreaterOrEqual(t, from, 0, "%q is in a row", marker)
	return between(t, body[from:], "<tr", "</tr>")
}

func licenseSummary(id string, state license.State, expiresAt time.Time) cpstore.LicenseSummary {
	return cpstore.LicenseSummary{
		ID: id, CustomerID: customerID, CustomerName: "Acme", Plan: "standard",
		Telemetry: license.TelemetryOnline, ExpiresAt: expiresAt, State: state,
	}
}

func instanceSummary(licenseID, instanceID string) cpstore.InstanceSummary {
	return cpstore.InstanceSummary{
		LicenseID: licenseID, InstanceID: instanceID,
		FirstSeenAt:    time.Date(2026, 9, 1, 7, 5, 0, 0, time.UTC),
		LastSeenAt:     time.Date(2026, 10, 3, 2, 30, 0, 0, time.UTC),
		LastReportDay:  time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC),
		HusonymVersion: "v0.3.0", InstallKind: "helm",
	}
}

func Test_Attention_Empty_FiveSectionsInOrderEachSayingNothing(t *testing.T) {
	b := newBench(t)

	got := b.get("/")

	require.Equal(t, http.StatusOK, got.status)
	require.Equal(t, "text/html; charset=utf-8", got.header.Get("Content-Type"))
	require.Contains(t, got.body, "<h1>Needs attention</h1>")
	require.Equal(t, today, b.store.askedAt)
	last := -1
	for id, heading := range map[string]string{
		"silent-instances":  "Silent instances",
		"expiring-licenses": "Expiring licenses",
		"old-pending":       "Old pending reports",
		"seal-rejections":   "Seal rejections",
		"shared-licenses":   "Shared licenses",
	} {
		part := section(t, got.body, id)
		require.Contains(t, part, heading+` <span class="count">0</span>`)
		require.Contains(t, part, "Nothing.")
		require.NotContains(t, part, "<table")
	}
	for _, id := range []string{"silent-instances", "expiring-licenses", "old-pending", "seal-rejections", "shared-licenses"} {
		at := strings.Index(got.body, `<section id="`+id+`"`)
		require.Greater(t, at, last, "%s comes in the order of the list", id)
		last = at
	}
}

func Test_Attention_ListsEachThingWithItsCountAndItsLinks(t *testing.T) {
	b := newBench(t)
	b.store.attention = &cpstore.Attention{
		SilentInstances: []cpstore.SilentInstance{
			{InstanceSummary: instanceSummary("lic-silent", instanceOne), CustomerID: customerID, CustomerName: "Acme"},
		},
		ExpiringLicenses: []cpstore.LicenseSummary{
			licenseSummary("lic-expiring", license.StateExpiring, time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)),
			licenseSummary("lic-grace", license.StateGrace, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)),
		},
		OldPending: []cpstore.PendingGroup{{
			KeyFingerprint: fingerprint, Reports: 5, OldReports: 3,
			Oldest: time.Date(2026, 10, 5, 4, 0, 0, 0, time.UTC), Newest: time.Date(2026, 10, 8, 4, 0, 0, 0, time.UTC),
			InstanceIDs: []string{instanceOne},
		}},
		SealRejections: []cpstore.SealRejection{
			{
				LicenseID: "lic-refused", CustomerID: customerID, CustomerName: "Acme",
				Day: time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC), Count: 4, LastAt: time.Date(2026, 10, 7, 9, 41, 0, 0, time.UTC),
			},
			{
				LicenseID: "lic-refused", CustomerID: customerID, CustomerName: "Acme",
				Day: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC), Count: 2, LastAt: time.Date(2026, 10, 6, 9, 41, 0, 0, time.UTC),
			},
		},
		SharedLicenses: []cpstore.SharedLicense{
			{LicenseSummary: licenseSummary("lic-shared", license.StateValid, today.AddDate(1, 0, 0)), RecentInstances: 3},
		},
	}

	got := b.get("/")

	require.Equal(t, http.StatusOK, got.status)

	silent := section(t, got.body, "silent-instances")
	require.Contains(t, silent, `Silent instances <span class="count">1</span>`)
	silentRow := row(t, silent, instanceOne)
	require.Contains(t, silentRow, `href="/licenses/lic-silent/instances/`+instanceOne+`"`)
	require.Contains(t, silentRow, `href="/licenses/lic-silent"`)
	require.Contains(t, silentRow, `href="/customers/`+customerID.String()+`"`)
	require.Contains(t, silentRow, "2026-10-02", "the day of its last report")

	expiring := section(t, got.body, "expiring-licenses")
	require.Contains(t, expiring, `Expiring licenses <span class="count">2</span>`)
	require.Contains(t, row(t, expiring, "lic-expiring"), `href="/licenses/lic-expiring"`)
	require.Contains(t, row(t, expiring, "lic-expiring"), "2026-10-20 00:00")
	require.Contains(t, row(t, expiring, "lic-expiring"), ">expiring<")
	require.Contains(t, row(t, expiring, "lic-grace"), ">grace<")

	pending := section(t, got.body, "old-pending")
	require.Contains(t, pending, `Old pending reports <span class="count">3</span>`, "the reports are counted, not the fingerprints")
	require.Contains(t, row(t, pending, fingerprint), `href="/pending#fp-`+fingerprint+`"`)

	refused := section(t, got.body, "seal-rejections")
	require.Contains(t, refused, `Seal rejections <span class="count">6</span>`, "the refused reports are counted")
	require.Contains(t, row(t, refused, "2026-10-07"), `href="/licenses/lic-refused"`)
	require.Contains(t, row(t, refused, "2026-10-07"), "<td class=\"number\">4</td>")

	shared := section(t, got.body, "shared-licenses")
	require.Contains(t, shared, `Shared licenses <span class="count">1</span>`)
	require.Contains(t, row(t, shared, "lic-shared"), `href="/licenses/lic-shared"`)
	require.Contains(t, row(t, shared, "lic-shared"), "<td class=\"number\">3</td>")
}

func Test_Layout_ShowsTheOperatorAndTheLinksAndNoBanner(t *testing.T) {
	b := newBench(t)

	got := b.get("/customers")

	head := between(t, got.body, "<header", "</header>")
	require.Contains(t, head, operatorEmail)
	require.Contains(t, head, `<a href="/">Needs attention</a>`)
	require.Contains(t, head, `<a href="/customers" aria-current="page">Customers</a>`)
	require.Contains(t, head, `<a href="/pending">Pending</a>`)
	require.Contains(t, got.body, `<link rel="stylesheet" href="/static/console.css">`)
	require.NotContains(t, got.body, "gates are off")
	require.NotContains(t, got.body, "style=", "the policy forbids inline styles")
	require.NotContains(t, got.body, "<script")
}

func Test_Customers_ListsEachWithItsNumbers(t *testing.T) {
	b := newBench(t)
	b.store.customers = []cpstore.CustomerSummary{
		{
			ID: customerID, ExternalID: "cust-1", Name: "Acme", Licenses: 2, RecentInstances: 7,
			NearestExpiry: time.Date(2027, 3, 1, 9, 30, 0, 0, time.FixedZone("plus two", 2*3600)),
		},
		{ID: uuid.New(), ExternalID: "cust-2", Name: "Without a license"},
	}

	got := b.get("/customers")

	require.Equal(t, http.StatusOK, got.status)
	require.Contains(t, got.body, "<h1>Customers</h1>")
	acme := row(t, got.body, "cust-1")
	require.Contains(t, acme, `<a href="/customers/`+customerID.String()+`">Acme</a>`)
	require.Contains(t, acme, `<td class="number">2</td>`)
	require.Contains(t, acme, `<td class="number">7</td>`)
	require.Contains(t, acme, "2027-03-01 07:30", "an instant is shown in UTC")
	require.Contains(t, row(t, got.body, "cust-2"), "—", "no expiry to show")
}

func Test_Customers_None_SaysSo(t *testing.T) {
	b := newBench(t)

	got := b.get("/customers")

	require.Equal(t, http.StatusOK, got.status)
	require.Contains(t, got.body, "No customer.")
}

func Test_Customer_ShowsItsLicensesAndInstances(t *testing.T) {
	b := newBench(t)
	b.store.customer = &cpstore.CustomerDetail{
		ID: customerID, ExternalID: "cust-1", Name: "Acme", Note: "renewed in spring",
		CreatedAt: time.Date(2026, 1, 2, 3, 4, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 5, 6, 7, 8, 0, 0, time.UTC),
		Licenses: []cpstore.LicenseSummary{
			licenseSummary("lic-1", license.StateValid, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)),
			licenseSummary("lic-0", license.StateFrozen, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
		},
		Instances: []cpstore.InstanceSummary{instanceSummary("lic-1", instanceOne)},
	}
	b.store.customer.Licenses[1].HasSuccessor = true

	got := b.get("/customers/" + customerID.String())

	require.Equal(t, http.StatusOK, got.status)
	require.Equal(t, customerID, b.store.askedID)
	require.Contains(t, got.body, "<h1>Acme</h1>")
	require.Contains(t, got.body, "cust-1")
	require.Contains(t, got.body, "renewed in spring")
	require.Contains(t, got.body, "2026-01-02 03:04")
	current := row(t, got.body, `href="/licenses/lic-1"`)
	require.Contains(t, current, ">valid<")
	require.Contains(t, current, ">online<")
	require.Contains(t, current, "2027-01-01 00:00")
	former := row(t, got.body, `href="/licenses/lic-0"`)
	require.Contains(t, former, ">frozen<")
	require.Contains(t, former, "<td>yes</td>", "another license succeeds it")
	seen := row(t, got.body, instanceOne)
	require.Contains(t, seen, `href="/licenses/lic-1/instances/`+instanceOne+`"`)
	require.Contains(t, seen, "v0.3.0")
	require.Contains(t, seen, "helm")
	require.Contains(t, seen, "2026-10-03 02:30")
}

// Review focus: what a customer is called, and the note on it, come from a registry file.
func Test_Customer_MarkupInItsNameAndNote_RendersAsText(t *testing.T) {
	b := newBench(t)
	b.store.customer = &cpstore.CustomerDetail{
		ID: customerID, ExternalID: `"><script>alert(3)</script>`,
		Name: `<script>alert(1)</script>`, Note: `<script>alert(2)</script><img src=x onerror=alert(4)>`,
	}

	got := b.get("/customers/" + customerID.String())

	require.Equal(t, http.StatusOK, got.status)
	require.NotContains(t, got.body, "<script")
	require.NotContains(t, got.body, "<img")
	require.Contains(t, got.body, "<h1>&lt;script&gt;alert(1)&lt;/script&gt;</h1>")
	require.Contains(t, got.body, "&lt;script&gt;alert(2)&lt;/script&gt;")
	require.Contains(t, got.body, "<title>&lt;script&gt;alert(1)&lt;/script&gt; · Husonym control plane</title>")
}

func Test_License_ShowsWhatItsKeyCarriesAndWhatWasSeenUnderIt(t *testing.T) {
	b := newBench(t)
	grace, maxSources := 14, 5
	b.store.license = &cpstore.LicenseDetail{
		LicenseSummary:        licenseSummary("lic-1", license.StateExpiring, time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)),
		KeyFingerprint:        fingerprint,
		Kid:                   "2026-01",
		Features:              []string{"feature-a", "feature-b"},
		Limits:                &license.Limits{MaxSources: &maxSources, AllowedConnectionTypes: []string{"postgres", "mysql"}},
		StoredTelemetry:       "online",
		IssuedAt:              time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		GraceDays:             &grace,
		SigningKeyFingerprint: "signing-key-fingerprint",
		Note:                  "a note of the registry",
		Origin:                "registry",
		CreatedAt:             time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC),
		PredecessorID:         "lic-0",
		SuccessorIDs:          []string{"lic-2"},
		Instances:             []cpstore.InstanceSummary{instanceSummary("lic-1", instanceOne)},
		SealRejections: []cpstore.SealRejection{{
			LicenseID: "lic-1", CustomerID: customerID, CustomerName: "Acme",
			Day: time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC), Count: 4, LastAt: time.Date(2026, 10, 7, 9, 41, 0, 0, time.UTC),
		}},
	}

	got := b.get("/licenses/lic-1")

	require.Equal(t, http.StatusOK, got.status)
	require.Equal(t, []string{"lic-1"}, b.store.askedFor)
	require.Equal(t, today, b.store.askedAt)
	require.Contains(t, got.body, `<h1 class="id">lic-1</h1>`)
	facts := between(t, got.body, "<dl", "</dl>")
	require.Contains(t, facts, `<a href="/customers/`+customerID.String()+`">Acme</a>`)
	for _, shown := range []string{
		">expiring<", "online", "standard", "2026-10-20 00:00", "2026-01-01 00:00", "14", fingerprint, "2026-01",
		"feature-a", "feature-b", "signing-key-fingerprint", "a note of the registry", "registry",
		`href="/licenses/lic-0"`, `href="/licenses/lic-2"`,
	} {
		require.Contains(t, facts, shown)
	}
	require.Contains(t, between(t, facts, "max_sources", "</dd>"), "5")
	require.Contains(t, between(t, facts, "max_jobs", "</dd>"), "uncapped")
	require.Contains(t, between(t, facts, "allowed_connection_types", "</dd>"), "postgres, mysql")
	require.Contains(t, row(t, got.body, instanceOne), `href="/licenses/lic-1/instances/`+instanceOne+`"`)
	require.Contains(t, row(t, section(t, got.body, "seal-rejections"), "2026-10-07"), `<td class="number">4</td>`)
}

func Test_License_ThatSaysLittle_ShowsWhatThatMeans(t *testing.T) {
	b := newBench(t)
	b.store.license = &cpstore.LicenseDetail{
		LicenseSummary:  licenseSummary("lic-1", license.StateValid, today.AddDate(1, 0, 0)),
		StoredTelemetry: "",
	}

	got := b.get("/licenses/lic-1")

	require.Equal(t, http.StatusOK, got.status)
	facts := between(t, got.body, "<dl", "</dl>")
	require.Contains(t, between(t, facts, "<dt>Features</dt>", "</dd>"), "all")
	require.Contains(t, between(t, facts, "<dt>Limits</dt>", "</dd>"), "none")
	require.Contains(t, between(t, facts, "<dt>Grace days</dt>", "</dd>"), "not said")
	require.Contains(t, between(t, facts, "<dt>Telemetry</dt>", "</dd>"), "the key does not say")
	require.Contains(t, got.body, "No instance.")
	require.Contains(t, section(t, got.body, "seal-rejections"), "Nothing.")
}

func Test_License_AnIDThatNeedsEscaping_IsAskedAsWrittenAndLinkedEscaped(t *testing.T) {
	b := newBench(t)
	b.store.license = &cpstore.LicenseDetail{
		LicenseSummary: licenseSummary("lic/1 a", license.StateValid, today.AddDate(1, 0, 0)),
		Instances:      []cpstore.InstanceSummary{instanceSummary("lic/1 a", instanceOne)},
	}

	got := b.get("/licenses/lic%2F1%20a")

	require.Equal(t, http.StatusOK, got.status)
	require.Equal(t, []string{"lic/1 a"}, b.store.askedFor)
	require.Contains(t, got.body, `href="/licenses/lic%2F1%20a/instances/`+instanceOne+`"`)
}

// A browser takes "." and ".." out of a path before it asks for it: a link to such an id would
// lead to another page.
func Test_AnIDThatIsADotSegment_IsShownAsTextNotAsALink(t *testing.T) {
	b := newBench(t)
	b.store.customer = &cpstore.CustomerDetail{
		ID: customerID, ExternalID: "cust-1", Name: "Acme",
		Licenses: []cpstore.LicenseSummary{
			licenseSummary(".", license.StateValid, today.AddDate(1, 0, 0)),
			licenseSummary("..", license.StateValid, today.AddDate(1, 0, 0)),
			licenseSummary("...", license.StateValid, today.AddDate(1, 0, 0)),
		},
		Instances: []cpstore.InstanceSummary{
			instanceSummary("lic-1", ".."), instanceSummary(".", instanceOne), instanceSummary("lic-2", instanceTwo),
		},
	}

	got := b.get("/customers/" + customerID.String())

	require.Equal(t, http.StatusOK, got.status)
	licenses := section(t, got.body, "licenses")
	require.Contains(t, licenses, `<td><span class="id">.</span></td>`)
	require.Contains(t, licenses, `<td><span class="id">..</span></td>`)
	require.Contains(t, licenses, `<a class="id" href="/licenses/...">...</a>`, "three dots are a name like another")
	instances := section(t, got.body, "instances")
	require.Contains(t, instances, `<td><span class="id">..</span></td><td><a class="id" href="/licenses/lic-1">lic-1</a></td>`)
	require.Contains(t, instances, `<td><span class="id">`+instanceOne+`</span></td><td><span class="id">.</span></td>`)
	require.Contains(t, instances, `href="/licenses/lic-2/instances/`+instanceTwo+`"`)
	for _, link := range []string{`href="/licenses/."`, `href="/licenses/.."`, `href="/licenses/./`, `/instances/.."`, `href=""`} {
		require.NotContains(t, got.body, link)
	}
}

func Test_Instance_WhoseIDIsADotSegment_LinksNoneOfItsReports(t *testing.T) {
	b := newBench(t)
	b.store.instance = instanceWithReports(nil)
	b.store.instance.InstanceID = ".."

	got := b.get("/licenses/lic-1/instances/x")

	require.Equal(t, http.StatusOK, got.status)
	require.Contains(t, row(t, section(t, got.body, "reports"), "2026-10-02"), "<td>2026-10-02</td>")
	require.NotContains(t, got.body, "/reports/")
	require.NotContains(t, got.body, `href=""`)
}

func instanceWithReports(limits *license.Limits) *cpstore.InstanceDetail {
	three, seven := 3, 7
	return &cpstore.InstanceDetail{
		InstanceSummary: instanceSummary("lic-1", instanceOne),
		CustomerID:      customerID,
		CustomerName:    "Acme",
		Limits:          limits,
		Reports: []cpstore.ReportSummary{
			{Day: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC), ReceivedAt: time.Date(2026, 10, 3, 2, 30, 0, 0, time.UTC), Sources: &seven},
			{
				Day: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), ReceivedAt: time.Date(2026, 10, 2, 2, 31, 0, 0, time.UTC),
				Sources: &three, Conflicts: 2,
			},
			{Day: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), ReceivedAt: time.Date(2026, 10, 1, 2, 32, 0, 0, time.UTC)},
		},
	}
}

func Test_Instance_WithASourceCap_CountsTheSourcesAgainstItAndSaysWhichAreOver(t *testing.T) {
	b := newBench(t)
	maxSources := 5
	b.store.instance = instanceWithReports(&license.Limits{MaxSources: &maxSources})

	got := b.get("/licenses/lic-1/instances/" + instanceOne)

	require.Equal(t, http.StatusOK, got.status)
	require.Equal(t, []string{"lic-1", instanceOne}, b.store.askedFor)
	require.Contains(t, got.body, `<h1 class="id">`+instanceOne+`</h1>`)
	facts := between(t, got.body, "<dl", "</dl>")
	require.Contains(t, facts, `<a class="id" href="/licenses/lic-1">lic-1</a>`)
	require.Contains(t, facts, `<a href="/customers/`+customerID.String()+`">Acme</a>`)
	require.Contains(t, between(t, facts, "<dt>Source cap</dt>", "</dd>"), "5")
	require.Contains(t, facts, "v0.3.0")
	require.Contains(t, facts, "2026-09-01 07:05")

	over := row(t, got.body, `/reports/2026-10-02"`)
	require.Contains(t, over, `href="/licenses/lic-1/instances/`+instanceOne+`/reports/2026-10-02"`)
	require.Contains(t, over, "7 / 5")
	require.Contains(t, over, "over the cap", "said in a word, not only by a colour")
	require.Contains(t, over, "2026-10-03 02:30")
	under := row(t, got.body, `/reports/2026-10-01"`)
	require.Contains(t, under, "3 / 5")
	require.NotContains(t, under, "over the cap")
	require.Contains(t, under, `<td class="number">2</td>`, "its conflicting re-sends")
	unread := row(t, got.body, `/reports/2026-09-30"`)
	require.NotContains(t, unread, "/ 5")
	require.NotContains(t, unread, "over the cap")
}

func Test_Instance_WithoutASourceCap_ShowsTheSourcesAlone(t *testing.T) {
	maxJobs := 2
	for name, limits := range map[string]*license.Limits{"no limits": nil, "another cap": {MaxJobs: &maxJobs}} {
		t.Run(name, func(t *testing.T) {
			b := newBench(t)
			b.store.instance = instanceWithReports(limits)

			got := b.get("/licenses/lic-1/instances/" + instanceOne)

			require.Equal(t, http.StatusOK, got.status)
			require.Contains(t, between(t, got.body, "<dt>Source cap</dt>", "</dd>"), "none")
			require.Contains(t, row(t, got.body, `/reports/2026-10-02"`), `<td class="number">7</td>`)
			require.NotContains(t, got.body, "over the cap")
		})
	}
}

func Test_Report_ShowsTheDocumentIndentedAndLeadsBackToTheInstance(t *testing.T) {
	b := newBench(t)
	b.store.report = &cpstore.StoredReport{
		LicenseID: "lic-1", InstanceID: instanceOne, CustomerID: customerID, CustomerName: "Acme",
		Day:            time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC),
		Document:       []byte(`{"day":"2026-10-02","sources":{"count":7},"note":"<b>bold</b>"}`),
		Seal:           "5ea15ea15ea15ea15ea15ea15ea15ea15ea15ea15ea15ea15ea15ea15ea15ea1",
		ReceivedAt:     time.Date(2026, 10, 3, 2, 30, 0, 0, time.UTC),
		Conflicts:      2,
		LastConflictAt: time.Date(2026, 10, 3, 5, 45, 0, 0, time.UTC),
	}

	got := b.get("/licenses/lic-1/instances/" + instanceOne + "/reports/2026-10-02")

	require.Equal(t, http.StatusOK, got.status)
	require.Equal(t, []string{"lic-1", instanceOne}, b.store.askedFor)
	require.Equal(t, time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC), b.store.askedForDay)
	require.Contains(t, got.body, "<h1>Report of 2026-10-02</h1>")
	require.Equal(t, `<nav class="crumbs" aria-label="Where this is"><a href="/customers">Customers</a>`+
		` <span aria-hidden="true">/</span> <a href="/customers/`+customerID.String()+`">Acme</a>`+
		` <span aria-hidden="true">/</span> <a class="id" href="/licenses/lic-1">lic-1</a>`+
		` <span aria-hidden="true">/</span> <a class="id" href="/licenses/lic-1/instances/`+instanceOne+`">`+instanceOne+`</a>`,
		between(t, got.body, `<nav class="crumbs"`, "</nav>"), "from the customers down, as on the other pages")
	facts := between(t, got.body, "<dl", "</dl>")
	require.Contains(t, facts, `<a class="id" href="/licenses/lic-1/instances/`+instanceOne+`">`+instanceOne+`</a>`)
	require.Contains(t, between(t, facts, "<dt>Received</dt>", "</dd>"), "2026-10-03 02:30")
	require.Contains(t, between(t, facts, "<dt>Conflicting re-sends</dt>", "</dd>"), "2")
	require.Contains(t, between(t, facts, "<dt>Conflicting re-sends</dt>", "</dd>"), "2026-10-03 05:45")
	document := between(t, got.body, "<pre", "</pre>")
	require.Contains(t, document, "{\n  &#34;day&#34;: &#34;2026-10-02&#34;,\n  &#34;sources&#34;: {\n    &#34;count&#34;: 7\n  },")
	require.NotContains(t, got.body, "<b>bold</b>", "the document is text")
}

func Test_Report_ThatIsNotJSON_IsShownAsReceived(t *testing.T) {
	b := newBench(t)
	b.store.report = &cpstore.StoredReport{
		LicenseID: "lic-1", InstanceID: instanceOne, Day: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC),
		Document: []byte("not json </pre><script>alert(1)</script>"),
	}

	got := b.get("/licenses/lic-1/instances/" + instanceOne + "/reports/2026-10-02")

	require.Equal(t, http.StatusOK, got.status)
	require.Contains(t, between(t, got.body, "<pre", "</main>"), "not json &lt;/pre&gt;&lt;script&gt;alert(1)&lt;/script&gt;</pre>")
	require.NotContains(t, got.body, "<script")
	require.Contains(t, between(t, got.body, "<dt>Conflicting re-sends</dt>", "</dd>"), "0")
}

func Test_Pending_ListsTheFingerprintsWhole(t *testing.T) {
	b := newBench(t)
	b.store.pending = []cpstore.PendingGroup{{
		KeyFingerprint: fingerprint, Reports: 5, OldReports: 3,
		Oldest: time.Date(2026, 10, 5, 4, 0, 0, 0, time.UTC), Newest: time.Date(2026, 10, 8, 4, 10, 0, 0, time.UTC),
		InstanceIDs: []string{instanceOne, instanceTwo},
	}}

	got := b.get("/pending")

	require.Equal(t, http.StatusOK, got.status)
	require.Equal(t, today, b.store.askedAt)
	require.Contains(t, got.body, "<h1>Pending</h1>")
	group := row(t, got.body, `id="fp-`+fingerprint+`"`)
	require.Contains(t, group, `<td class="fingerprint">`+fingerprint+`</td>`, "whole, where it can be read and copied")
	require.NotContains(t, group, "title=")
	require.Contains(t, group, `<td class="number">5</td>`)
	require.Contains(t, group, "3 old", "said in a word")
	require.Contains(t, group, "2026-10-05 04:00")
	require.Contains(t, group, "2026-10-08 04:10")
	require.Contains(t, group, instanceOne)
	require.Contains(t, group, instanceTwo)
}

func Test_Pending_None_SaysSo(t *testing.T) {
	b := newBench(t)

	got := b.get("/pending")

	require.Equal(t, http.StatusOK, got.status)
	require.Contains(t, got.body, "Nothing.")
}

func Test_WhatIsNotThere_AnswersTheNotFoundPage(t *testing.T) {
	malformed := map[string]string{
		"a customer id that is not one": "/customers/not-a-uuid",
		"a day that is not one":         "/licenses/lic-1/instances/" + instanceOne + "/reports/yesterday",
		"a day that does not exist":     "/licenses/lic-1/instances/" + instanceOne + "/reports/2026-02-30",
		"a path of the public server":   "/v1/usage-reports",
		"a path under a page":           "/customers/" + customerID.String() + "/licenses",
		"a path with a trailing slash":  "/customers/",
		"another static file":           "/static/console.js",
		"the health check":              "/healthz",
		// What a text column cannot hold: the store would answer an error, not "no such row".
		"a license id with a zero byte":       "/licenses/%00",
		"a license id that is not UTF-8":      "/licenses/%ff",
		"an instance id with a zero byte":     "/licenses/lic-1/instances/a%00b",
		"an instance id that is not UTF-8":    "/licenses/lic-1/instances/%ff",
		"the license of an instance, zero":    "/licenses/%00/instances/" + instanceOne,
		"the license of an instance, invalid": "/licenses/%c3%28/instances/" + instanceOne,
		"the instance of a report, zero":      "/licenses/lic-1/instances/%00/reports/2026-10-02",
		"the license of a report, invalid":    "/licenses/%ff/instances/" + instanceOne + "/reports/2026-10-02",
		"a customer id with a zero byte":      "/customers/%00",
		"a customer id that is not UTF-8":     "/customers/%ff",
	}
	for name, path := range malformed {
		t.Run(name, func(t *testing.T) {
			b := newBench(t)

			got := b.get(path)

			require.Equal(t, http.StatusNotFound, got.status)
			require.Contains(t, got.body, "<h1>Not found</h1>")
			require.Contains(t, between(t, got.body, "<header", "</header>"), operatorEmail, "the page has the layout")
			require.Zero(t, b.store.calls, "the store is not asked")
		})
	}

	unknown := map[string]string{
		"an unknown customer": "/customers/" + customerID.String(),
		"an unknown license":  "/licenses/lic-9",
		"an unknown instance": "/licenses/lic-1/instances/" + instanceOne,
		"an unknown report":   "/licenses/lic-1/instances/" + instanceOne + "/reports/2026-10-02",
	}
	for name, path := range unknown {
		t.Run(name, func(t *testing.T) {
			b := newBench(t)

			got := b.get(path)

			require.Equal(t, http.StatusNotFound, got.status)
			require.Contains(t, got.body, "<h1>Not found</h1>")
			require.Equal(t, 1, b.store.calls)
			require.NotContains(t, b.logs.String(), "level=ERROR")
		})
	}
}

func Test_StoreFailure_AnswersTheFailurePageInFixedWordsAndLogsTheError(t *testing.T) {
	paths := []string{
		"/", "/customers", "/customers/" + customerID.String(), "/licenses/lic-1",
		"/licenses/lic-1/instances/" + instanceOne,
		"/licenses/lic-1/instances/" + instanceOne + "/reports/2026-10-02", "/pending",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			b := newBench(t)
			b.store.err = errors.New("the database said: relation is away")

			got := b.get(path)

			require.Equal(t, http.StatusInternalServerError, got.status)
			require.Contains(t, got.body, "<h1>Something went wrong</h1>")
			require.Contains(t, got.body, "The page could not be shown. The error is in the log.")
			require.NotContains(t, got.body, "relation is away")
			require.Contains(t, between(t, got.body, "<header", "</header>"), operatorEmail, "the page has the layout")
			require.Contains(t, b.logs.String(), "relation is away")
			require.Contains(t, b.logs.String(), "status=500")
		})
	}
}

// A form of the console is sent to the console and nowhere else.
const policy = "default-src 'none'; style-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

func Test_EveryAnswer_CarriesTheSecurityHeaders(t *testing.T) {
	b := newBench(t)
	failing := newBench(t)
	failing.store.err = errors.New("away")
	answers := map[string]page{
		"a page":             b.get("/customers"),
		"the stylesheet":     b.get("/static/console.css"),
		"the not found page": b.get("/nothing-here"),
		"a refused method":   b.do(http.MethodPut, "/customers"),
		"a refused origin":   b.post("/customers", nil, crossSite),
		"the failure page":   failing.get("/"),
	}
	for name, got := range answers {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, "no-store", got.header.Get("Cache-Control"))
			require.Equal(t, policy, got.header.Get("Content-Security-Policy"))
			require.Equal(t, "nosniff", got.header.Get("X-Content-Type-Options"))
			require.Equal(t, "no-referrer", got.header.Get("Referrer-Policy"))
		})
	}
}

func Test_Stylesheet_IsServedAsCSSAndNamesNothingOutside(t *testing.T) {
	b := newBench(t)

	got := b.get("/static/console.css")

	require.Equal(t, http.StatusOK, got.status)
	require.Equal(t, "text/css; charset=utf-8", got.header.Get("Content-Type"))
	require.Contains(t, got.body, "prefers-color-scheme: dark")
	require.NotContains(t, got.body, "url(")
	require.NotContains(t, got.body, "@import")
}

// A page is read with a GET, or the HEAD of one, and nothing else gets it. The two paths of the
// customers take a POST too, which records or changes a customer: they are left out of that method.
func Test_NoPageAcceptsAnotherMethod(t *testing.T) {
	paths := []string{
		"/", "/customers", "/customers/" + customerID.String(), "/licenses/lic-1",
		"/licenses/lic-1/instances/" + instanceOne,
		"/licenses/lic-1/instances/" + instanceOne + "/reports/2026-10-02", "/pending", "/journal",
		"/customers/new", "/customers/" + customerID.String() + "/edit",
		"/customers/" + customerID.String() + "/licenses/new", "/licenses/lic-1/renew", "/static/console.css",
	}
	posted := map[string]bool{"/customers": true, "/customers/" + customerID.String(): true}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		for _, path := range paths {
			if method == http.MethodPost && posted[path] {
				continue
			}
			t.Run(method+" "+path, func(t *testing.T) {
				b := newBench(t)
				b.store.customer, b.store.license = &cpstore.CustomerDetail{}, &cpstore.LicenseDetail{}
				b.store.instance, b.store.report = &cpstore.InstanceDetail{}, &cpstore.StoredReport{}

				got := b.do(method, path)

				require.Contains(t, []int{http.StatusNotFound, http.StatusMethodNotAllowed}, got.status)
				require.Zero(t, b.store.calls, "the store is not asked")
				require.Zero(t, b.writer.writes, "nothing is written")
			})
		}
	}
}

// An act of the operator is a POST: asked for with a GET, it does nothing.
func Test_NoActIsDoneByAGet(t *testing.T) {
	for _, path := range []string{"/licenses", "/licenses/confirm", "/licenses/lic-1/key"} {
		t.Run(path, func(t *testing.T) {
			b := newBench(t)
			b.store.license = &cpstore.LicenseDetail{}

			got := b.get(path + "?license_id=0123456789abcdef")

			require.Zero(t, b.writer.writes, "nothing is written")
			require.Zero(t, b.signer.calls, "nothing is signed")
			require.NotContains(t, got.body, "<textarea")
		})
	}
}

func Test_EachRequest_LogsOneLineWithTheOperatorTheMethodTheRouteAndTheStatus(t *testing.T) {
	routes := map[string]string{
		"/":                     `route="GET /{$}"`,
		"/customers":            `route="GET /customers"`,
		"/customers/not-a-uuid": `route="GET /customers/{id}"`,
		"/licenses/lic-SECRET":  `route="GET /licenses/{id}"`,
		"/licenses/lic-SECRET/instances/inst-SECRET":                    `route="GET /licenses/{license}/instances/{instance}"`,
		"/licenses/lic-SECRET/instances/inst-SECRET/reports/2026-10-02": `route="GET /licenses/{license}/instances/{instance}/reports/{day}"`,
		"/pending":                `route="GET /pending"`,
		"/static/console.css":     `route="GET /static/console.css"`,
		"/a-path-SECRET?q=SECRET": "route=-",
	}
	for path, route := range routes {
		t.Run(path, func(t *testing.T) {
			b := newBench(t)

			got := b.get(path)

			line := strings.TrimSpace(b.logs.String())
			require.Equal(t, 1, strings.Count(line, "\n")+1, "one line: %s", line)
			require.Contains(t, line, "operator="+operatorEmail)
			require.Contains(t, line, "method=GET")
			require.Contains(t, line, route)
			require.Contains(t, line, "status="+strconv.Itoa(got.status))
			require.NotContains(t, line, "SECRET", "the path as written is not logged")
		})
	}
}

// The console trusts the gate before it for who the operator is: without one it shows nothing.
func Test_RequestThatDidNotPassTheGate_GetsNoPage(t *testing.T) {
	store := &fakeStore{attention: &cpstore.Attention{}}
	var logs bytes.Buffer
	pages, err := console.New(&console.Config{
		Reader: store, Writer: &fakeWriter{}, Now: func() time.Time { return today },
		Logger: slog.New(slog.NewTextHandler(&logs, nil)),
	})
	require.NoError(t, err)

	for _, path := range []string{"/", "/customers", "/static/console.css", "/nothing-here"} {
		got := serve(pages, httptest.NewRequest(http.MethodGet, path, nil))

		require.Equal(t, http.StatusInternalServerError, got.status, path)
		require.NotContains(t, got.body, "<h1>Needs attention</h1>")
		require.Equal(t, policy, got.header.Get("Content-Security-Policy"))
	}
	require.Zero(t, store.calls)
	require.Contains(t, logs.String(), "level=ERROR")
}

func Test_Unguarded_ShowsABannerOnEveryPageAndTheOperatorAsLocal(t *testing.T) {
	store := &fakeStore{attention: &cpstore.Attention{}}
	var logs bytes.Buffer
	pages, err := console.NewUnguarded(&console.Config{
		Reader: store, Writer: &fakeWriter{}, Now: func() time.Time { return today },
		Logger: slog.New(slog.NewTextHandler(&logs, nil)),
	})
	require.NoError(t, err)

	for path, status := range map[string]int{
		"/": http.StatusOK, "/customers": http.StatusOK, "/pending": http.StatusOK, "/licenses/lic-9": http.StatusNotFound,
	} {
		got := serve(pages, httptest.NewRequest(http.MethodGet, path, nil))

		require.Equal(t, status, got.status, path)
		require.Contains(t, got.body, "The host and Access gates are off", path)
		require.Contains(t, between(t, got.body, "<header", "</header>"), `<span class="operator">local</span>`)
	}
	require.Contains(t, logs.String(), "operator=local")
}
