package console_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/controlplane/cptest"
	"github.com/fishtre-compagnie/husonym/controlplane/issuing"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

const (
	draftLicenseID    = "0123456789abcdef"
	previousLicenseID = "fedcba9876543210"
	signingKeyPrint   = "fingerprint-of-the-signing-key"
)

// recorded is a license the console asked the store to record.
type recorded struct {
	operator    string
	key         *license.Key
	issued      *license.IssuedLicense
	fingerprint string
	succeeds    string
	note        string
	at          time.Time
}

// fakeWriter remembers what it was asked to write and answers what it was given.
type fakeWriter struct {
	newID     uuid.UUID
	createErr error
	updateErr error
	recordErr error
	// notAdded makes RecordIssuedLicense answer that the license was already there.
	notAdded bool
	// onRecord runs when a license is recorded.
	onRecord func()
	showErr  error
	shownKey string

	writes    int
	operators []string
	created   []cpstore.NewCustomer
	updated   []cpstore.NewCustomer
	updatedID uuid.UUID
	records   []recorded
	shown     []string
}

func (f *fakeWriter) CreateCustomer(_ context.Context, operator string, c cpstore.NewCustomer, _ time.Time) (uuid.UUID, error) {
	f.writes++
	f.operators = append(f.operators, operator)
	f.created = append(f.created, c)
	return f.newID, f.createErr
}

func (f *fakeWriter) UpdateCustomer(_ context.Context, operator string, id uuid.UUID, name, note string, _ time.Time) error {
	f.writes++
	f.operators = append(f.operators, operator)
	f.updatedID = id
	f.updated = append(f.updated, cpstore.NewCustomer{Name: name, Note: note})
	return f.updateErr
}

func (f *fakeWriter) RecordIssuedLicense(
	_ context.Context, operator string, key *license.Key, issued *license.IssuedLicense,
	signingKeyFingerprint, succeeds, note string, now time.Time,
) (bool, error) {
	f.writes++
	f.operators = append(f.operators, operator)
	f.records = append(f.records, recorded{
		operator: operator, key: key, issued: issued, fingerprint: signingKeyFingerprint, succeeds: succeeds, note: note, at: now,
	})
	if f.onRecord != nil {
		f.onRecord()
	}
	if f.recordErr != nil {
		return false, f.recordErr
	}
	return !f.notAdded, nil
}

func (f *fakeWriter) ShowLicenseKey(_ context.Context, operator, licenseID string, _ time.Time) (string, error) {
	f.writes++
	f.operators = append(f.operators, operator)
	f.shown = append(f.shown, licenseID)
	return f.shownKey, f.showErr
}

// fakeSigner signs nothing: the key it gives is a word made of the id of the draft.
type fakeSigner struct {
	err    error
	calls  int
	drafts []*issuing.Draft
}

func keyOf(licenseID string) string { return "KEY-VALUE-OF-" + licenseID }

func (f *fakeSigner) Issue(d *issuing.Draft, _ time.Time) (*license.IssuedLicense, *license.Key, error) {
	f.calls++
	f.drafts = append(f.drafts, d)
	if f.err != nil {
		return nil, nil, f.err
	}
	return &license.IssuedLicense{Encoded: keyOf(d.LicenseID) + "\n", Id: d.LicenseID, Kid: "kid-1"},
		&license.Key{Id: d.LicenseID, CustomerId: d.CustomerExternalID, IssuedTo: d.CustomerName, ExpiresAt: d.ExpiresAt, Plan: d.Plan},
		nil
}

func (*fakeSigner) PublicKeyFingerprint() string { return signingKeyPrint }

type fakePromoter struct {
	err          error
	fingerprints []string
}

func (f *fakePromoter) PromotePending(_ context.Context, fingerprint string) (stored, discarded int, err error) {
	f.fingerprints = append(f.fingerprints, fingerprint)
	return 0, 0, f.err
}

// The three shapes of a request another origin makes the browser of the operator send.
var (
	crossSite     = map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://attacker.example"}
	sameSite      = map[string]string{"Sec-Fetch-Site": "same-site", "Origin": "https://other.example.com"}
	foreignOrigin = map[string]string{"Origin": "https://attacker.example"}
	// sameOrigin is what the browser sends with a form of the console itself.
	sameOrigin = map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "http://example.com"}
)

// post sends a form as the browser of the operator would; headers replace the ones of a form of
// the console itself.
func (b *bench) post(path string, form url.Values, headers ...map[string]string) page {
	b.t.Helper()
	return b.postBody(path, form.Encode(), headers...)
}

func (b *bench) postBody(path, body string, headers ...map[string]string) page {
	b.t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set(cptest.AccessHeader, access(b.t).Token(b.t, operatorEmail, today))
	sent := sameOrigin
	if len(headers) > 0 {
		sent = headers[0]
	}
	for name, value := range sent {
		req.Header.Set(name, value)
	}
	return serve(b.handler, req)
}

func acme() *cpstore.CustomerDetail {
	return &cpstore.CustomerDetail{ID: customerID, ExternalID: "cust-1", Name: "Acme", Note: "a note"}
}

// draftForm is what the confirmation page of a license for acme carries.
func draftForm() url.Values {
	return url.Values{
		"customer":                      {customerID.String()},
		issuing.FieldLicenseID:          {draftLicenseID},
		issuing.FieldCustomerExternalID: {"cust-1"},
		issuing.FieldCustomerName:       {"Acme"},
		issuing.FieldPlan:               {"standard"},
		issuing.FieldFeatures:           {string(license.FeatureJobHooks), string(license.FeatureSso)},
		issuing.FieldMaxSources:         {"5"},
		issuing.FieldExpiresAt:          {"2027-10-08"},
		issuing.FieldGraceDays:          {"7"},
		issuing.FieldTelemetry:          {string(license.TelemetryOfflineReport)},
		issuing.FieldNote:               {"NOTE-OF-THE-DRAFT"},
		issuing.FieldSucceeds:           {""},
	}
}

func requireLayout(t *testing.T, got page) {
	t.Helper()
	require.Contains(t, between(t, got.body, "<header", "</header>"), operatorEmail, "the page has the layout")
}

func Test_CustomerForm_New_AsksForTheThreeFields(t *testing.T) {
	b := newBench(t)

	got := b.get("/customers/new")

	require.Equal(t, http.StatusOK, got.status)
	require.Contains(t, got.body, "<h1>New customer</h1>")
	require.Contains(t, got.body, `<form class="form" method="post" action="/customers">`)
	for _, field := range []string{`name="external_id"`, `name="name"`, `name="note"`} {
		require.Contains(t, got.body, field)
	}
	require.Contains(t, b.get("/customers").body, `href="/customers/new"`)
}

func Test_CreateCustomer_RecordsItAsTheOperatorAndRedirectsToItsPage(t *testing.T) {
	b := newBench(t)
	b.writer.newID = customerID

	got := b.post("/customers", url.Values{"external_id": {" cust-1 "}, "name": {" Acme "}, "note": {"first\nsecond"}})

	require.Equal(t, http.StatusSeeOther, got.status)
	require.Equal(t, "/customers/"+customerID.String(), got.header.Get("Location"))
	require.Equal(t, []cpstore.NewCustomer{{ExternalID: "cust-1", Name: "Acme", Note: "first\nsecond"}}, b.writer.created)
	require.Equal(t, []string{operatorEmail}, b.writer.operators)
	line := strings.TrimSpace(b.logs.String())
	require.Equal(t, 1, strings.Count(line, "\n")+1, "one line: %s", line)
	require.Contains(t, line, `route="POST /customers"`)
	require.Contains(t, line, "status=303")
	require.NotContains(t, line, "Acme", "no value of the form is logged")
	require.NotContains(t, line, "cust-1")
}

func Test_CreateCustomer_Refused_ShowsTheFormAgainWithItsValues(t *testing.T) {
	cases := map[string]struct {
		form    url.Values
		err     error
		status  int
		message string
		written int
	}{
		"an external id already taken": {
			form: url.Values{"external_id": {"cust-1"}, "name": {"Acme <b>"}}, err: cpstore.ErrCustomerExists,
			status: http.StatusConflict, message: "A customer already has this external id.", written: 1,
		},
		"the store finds it incomplete": {
			form: url.Values{"external_id": {"cust-1"}, "name": {"Acme <b>"}}, err: cpstore.ErrCustomerIncomplete,
			status: http.StatusBadRequest, message: "A customer needs an external id and a name.", written: 1,
		},
		"no name": {
			form:   url.Values{"external_id": {"cust-1"}, "name": {"  "}, "note": {"Acme <b>"}},
			status: http.StatusBadRequest, message: "The name is required.",
		},
		"a control character in the external id": {
			form:   url.Values{"external_id": {"cust\x00-1"}, "name": {"Acme <b>"}},
			status: http.StatusBadRequest, message: "The external id holds a control or formatting character",
		},
		"a name that is too long": {
			form:   url.Values{"external_id": {"cust-1"}, "name": {strings.Repeat("a", 201)}, "note": {"Acme <b>"}},
			status: http.StatusBadRequest, message: "The name is too long.",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			b := newBench(t)
			b.writer.createErr = tc.err

			got := b.post("/customers", tc.form)

			require.Equal(t, tc.status, got.status)
			require.Contains(t, between(t, got.body, `<ul class="problems"`, "</ul>"), tc.message)
			require.Contains(t, got.body, `action="/customers"`)
			require.Contains(t, got.body, "Acme &lt;b&gt;", "what was typed is kept, as text")
			require.NotContains(t, got.body, "<b>")
			require.Equal(t, tc.written, b.writer.writes)
			requireLayout(t, got)
		})
	}
}

func Test_EditCustomer_ShowsTheExternalIDReadOnlyAndSavesNameAndNote(t *testing.T) {
	b := newBench(t)
	b.store.customer = acme()
	path := "/customers/" + customerID.String()

	form := b.get(path + "/edit")

	require.Equal(t, http.StatusOK, form.status)
	require.Contains(t, form.body, `<form class="form" method="post" action="`+path+`">`)
	require.Contains(t, form.body, `<input id="external_id" class="id" value="cust-1" readonly>`)
	require.NotContains(t, form.body, `name="external_id"`, "the external id is not sent")
	require.Contains(t, form.body, `name="name" value="Acme"`)
	require.Contains(t, form.body, ">a note</textarea>")

	saved := b.post(path, url.Values{"name": {"Acme Corp"}, "note": {"renamed"}, "external_id": {"cust-other"}})

	require.Equal(t, http.StatusSeeOther, saved.status)
	require.Equal(t, path, saved.header.Get("Location"))
	require.Equal(t, customerID, b.writer.updatedID)
	require.Equal(t, []cpstore.NewCustomer{{Name: "Acme Corp", Note: "renamed"}}, b.writer.updated)
	require.Equal(t, []string{operatorEmail}, b.writer.operators)
}

func Test_EditCustomer_Refused(t *testing.T) {
	path := "/customers/" + customerID.String()
	t.Run("without a name, the form again", func(t *testing.T) {
		b := newBench(t)
		b.store.customer = acme()

		got := b.post(path, url.Values{"name": {""}, "note": {"kept note"}})

		require.Equal(t, http.StatusBadRequest, got.status)
		require.Contains(t, got.body, "The name is required.")
		require.Contains(t, got.body, ">kept note</textarea>")
		require.Contains(t, got.body, `value="cust-1" readonly`)
		require.Zero(t, b.writer.writes)
	})
	t.Run("a customer that is not there", func(t *testing.T) {
		b := newBench(t)
		b.writer.updateErr = cpstore.ErrNotFound

		got := b.post(path, url.Values{"name": {"Acme"}})

		require.Equal(t, http.StatusNotFound, got.status)
		require.Contains(t, got.body, "<h1>Not found</h1>")
	})
	t.Run("an id that is not one", func(t *testing.T) {
		b := newBench(t)

		got := b.post("/customers/not-a-uuid", url.Values{"name": {"Acme"}})

		require.Equal(t, http.StatusNotFound, got.status)
		require.Zero(t, b.writer.writes)
	})
}

func Test_CustomerPage_LeadsToTheFormsOfIssuing(t *testing.T) {
	b := newBench(t)
	b.store.customer = acme()
	path := "/customers/" + customerID.String()

	got := b.get(path)

	require.Contains(t, got.body, `href="`+path+`/edit"`)
	require.Contains(t, got.body, `href="`+path+`/licenses/new"`)
	require.Contains(t, got.body, `href="`+path+`/licenses/new?trial=1"`)
	require.NotContains(t, got.body, "not configured")
}

func Test_LicenseForm_OffersWhatTheProductDeclaresAndAYearAhead(t *testing.T) {
	b := newBench(t)
	b.store.customer = acme()

	got := b.get("/customers/" + customerID.String() + "/licenses/new")

	require.Equal(t, http.StatusOK, got.status)
	require.Contains(t, got.body, "<h1>New license</h1>")
	require.Contains(t, got.body, `<form class="form" method="post" action="/licenses/confirm">`)
	require.Contains(t, got.body, `<input type="hidden" name="customer" value="`+customerID.String()+`">`)
	require.Contains(t, got.body, `<input type="hidden" name="customer_external_id" value="cust-1">`)
	require.Contains(t, got.body, `<input type="hidden" name="customer_name" value="Acme">`)
	require.Len(t, license.AllFeatures(), strings.Count(got.body, `name="features"`))
	for _, feature := range license.AllFeatures() {
		require.Contains(t, got.body, `<input type="checkbox" name="features" value="`+string(feature)+`">`)
	}
	require.Contains(t, got.body, `<input type="checkbox" name="all_features" value="1">`)
	for _, mode := range []license.TelemetryMode{license.TelemetryOnline, license.TelemetryOfflineReport, license.TelemetryNone} {
		require.Contains(t, got.body, `<option value="`+string(mode)+`">`)
	}
	require.Contains(t, got.body, `<option value="" selected>`)
	require.Contains(t, got.body, `name="expires_at" type="date" value="2027-10-08"`)
	for _, field := range []string{`name="plan"`, `name="max_sources"`, `name="grace_days"`, `name="note"`} {
		require.Contains(t, got.body, field)
	}
	require.Zero(t, b.signer.calls)
}

func Test_LicenseForm_Trial_IsPrefilledWithEveryFeatureAndThirtyDays(t *testing.T) {
	b := newBench(t)
	b.store.customer = acme()

	got := b.get("/customers/" + customerID.String() + "/licenses/new?trial=1")

	require.Equal(t, http.StatusOK, got.status)
	require.Contains(t, got.body, `<input type="checkbox" name="all_features" value="1" checked>`)
	require.Contains(t, got.body, `name="expires_at" type="date" value="2026-11-07"`)
}

func Test_ConfirmLicense_ShowsEveryLineOfTheKeyAndCarriesTheDraft(t *testing.T) {
	b := newBench(t)
	b.store.customer = acme()

	got := b.post("/licenses/confirm", draftForm())

	require.Equal(t, http.StatusOK, got.status)
	require.Contains(t, got.body, "A license cannot be deleted or changed once it is issued.")
	facts := between(t, got.body, "<dl", "</dl>")
	for _, shown := range []string{
		draftLicenseID, "Acme", "cust-1", "standard", "job_hooks, sso", ">5<", "2027-10-08 23:59:59 UTC", "7 days", "offline_report",
	} {
		require.Contains(t, facts, shown)
	}
	require.Contains(t, section(t, got.body, "beside-the-key"), "NOTE-OF-THE-DRAFT")
	form := between(t, got.body, `<form class="form" method="post" action="/licenses">`, "</form>")
	for name, values := range draftForm() {
		for _, value := range values {
			require.Contains(t, form, `<input type="hidden" name="`+name+`" value="`+value+`">`)
		}
	}
	require.Equal(t, 1, strings.Count(got.body, "<button"), "one button")
	require.Zero(t, b.signer.calls, "nothing is signed yet")
	require.Zero(t, b.writer.writes, "nothing is written yet")
}

func Test_ConfirmLicense_WithoutALicenseID_DrawsTheOneTheConfirmationCarries(t *testing.T) {
	b := newBench(t)
	b.store.customer, b.store.license = acme(), renewable()
	form := draftForm()
	form.Set(issuing.FieldLicenseID, "")
	form.Set(issuing.FieldSucceeds, previousLicenseID)

	got := b.post("/licenses/confirm", form)

	require.Equal(t, http.StatusOK, got.status)
	require.Regexp(t, `<input type="hidden" name="license_id" value="[0-9a-f]{16}">`, got.body)
	require.Contains(t, section(t, got.body, "beside-the-key"), `href="/licenses/`+previousLicenseID+`"`)
}

func Test_ConfirmLicense_WithProblems_ShowsThemAndKeepsTheValues(t *testing.T) {
	b := newBench(t)
	b.store.customer = acme()
	form := draftForm()
	form.Set(issuing.FieldExpiresAt, "2020-01-01")
	form.Set(issuing.FieldGraceDays, "-3")
	form.Set(issuing.FieldPlan, "a plan <b>")

	got := b.post("/licenses/confirm", form)

	require.Equal(t, http.StatusBadRequest, got.status)
	problems := between(t, got.body, `<ul class="problems"`, "</ul>")
	require.Contains(t, problems, "The expiry date is in the past.")
	require.Contains(t, problems, "The grace period cannot be negative.")
	require.Contains(t, got.body, `action="/licenses/confirm"`)
	require.Contains(t, got.body, `name="plan" value="a plan &lt;b&gt;"`)
	require.Contains(t, got.body, `name="expires_at" type="date" value="2020-01-01"`)
	require.Contains(t, got.body, `value="-3"`)
	require.Contains(t, got.body, `name="features" value="job_hooks" checked>`)
	require.Contains(t, got.body, `name="features" value="rbac">`)
	require.Contains(t, got.body, `<option value="offline_report" selected>`)
	require.Contains(t, got.body, ">NOTE-OF-THE-DRAFT</textarea>")
	require.Contains(t, got.body, `<input type="hidden" name="license_id" value="`+draftLicenseID+`">`)
	require.NotContains(t, got.body, "Issue the license")
}

// The hidden fields of the customer are never trusted: they are read again from the store.
func Test_ADraftWhoseCustomerIsNotTheOneOfTheStore_IsRefused(t *testing.T) {
	cases := map[string]func(b *bench, form url.Values){
		"another name":           func(_ *bench, form url.Values) { form.Set(issuing.FieldCustomerName, "Another") },
		"another external id":    func(_ *bench, form url.Values) { form.Set(issuing.FieldCustomerExternalID, "cust-2") },
		"a customer that is not": func(b *bench, _ url.Values) { b.store.customer = nil },
		"no customer":            func(_ *bench, form url.Values) { form.Del("customer") },
		"an id that is not one":  func(_ *bench, form url.Values) { form.Set("customer", "cust-1") },
	}
	for _, path := range []string{"/licenses/confirm", "/licenses"} {
		for name, change := range cases {
			t.Run(path+" "+name, func(t *testing.T) {
				b := newBench(t)
				b.store.customer = acme()
				form := draftForm()
				change(b, form)

				got := b.post(path, form)

				require.Contains(t, []int{http.StatusBadRequest, http.StatusConflict}, got.status)
				require.Contains(t, got.body, "customer")
				require.NotContains(t, got.body, "Issue the license")
				require.NotContains(t, got.body, "<textarea")
				require.Zero(t, b.signer.calls)
				require.Zero(t, b.writer.writes)
				requireLayout(t, got)
			})
		}
	}
}

func Test_IssueLicense_SignsRecordsPromotesAndShowsTheKey(t *testing.T) {
	b := newBench(t)
	b.store.customer = acme()
	form := draftForm()
	form.Set(issuing.FieldSucceeds, previousLicenseID)

	got := b.post("/licenses", form)

	require.Equal(t, http.StatusOK, got.status)
	require.Equal(t, "no-store", got.header.Get("Cache-Control"))
	require.Contains(t, between(t, got.body, "<textarea", "</textarea>"), ">"+keyOf(draftLicenseID))
	require.Contains(t, got.body, " readonly")
	require.Contains(t, got.body, `href="/licenses/`+draftLicenseID+`"`)

	require.Equal(t, 1, b.signer.calls)
	draft := b.signer.drafts[0]
	require.Equal(t, draftLicenseID, draft.LicenseID)
	require.Equal(t, []string{"job_hooks", "sso"}, draft.Features)
	require.Len(t, b.writer.records, 1)
	record := b.writer.records[0]
	require.Equal(t, operatorEmail, record.operator)
	require.Equal(t, draftLicenseID, record.key.Id)
	require.Equal(t, signingKeyPrint, record.fingerprint)
	require.Equal(t, previousLicenseID, record.succeeds)
	require.Equal(t, "NOTE-OF-THE-DRAFT", record.note)
	require.Equal(t, today, record.at)
	require.Equal(t, []string{telemetry.KeyFingerprint(keyOf(draftLicenseID))}, b.promoter.fingerprints,
		"the fingerprint of the key as it is stored, without the space around it")

	line := strings.TrimSpace(b.logs.String())
	require.Equal(t, 1, strings.Count(line, "\n")+1, "one line: %s", line)
	require.Contains(t, line, `route="POST /licenses"`)
	for _, value := range []string{keyOf(draftLicenseID), "NOTE-OF-THE-DRAFT", "Acme", draftLicenseID} {
		require.NotContains(t, line, value)
	}
}

// issuedDraft is the license the store holds once draftForm was issued, by whoever and from wherever.
func issuedDraft() *cpstore.LicenseDetail {
	grace, maxSources := 7, 5
	return &cpstore.LicenseDetail{
		LicenseSummary: cpstore.LicenseSummary{
			ID: draftLicenseID, CustomerID: customerID, CustomerName: "Acme", Plan: "standard",
			Telemetry: license.TelemetryOfflineReport, ExpiresAt: time.Date(2027, 10, 8, 23, 59, 59, 0, time.UTC),
		},
		Features: []string{"job_hooks", "sso"}, StoredTelemetry: "offline_report", GraceDays: &grace,
		Limits:         &license.Limits{MaxSources: &maxSources},
		KeyFingerprint: fingerprint, Origin: "registry", IssuedBy: "another@example.com",
	}
}

// requireNoKey holds an answer to showing no key and no page of a key.
func requireNoKey(t *testing.T, got page) {
	t.Helper()
	require.NotContains(t, got.body, "KEY", "no key is in the answer")
	require.NotContains(t, got.body, "<textarea")
	require.NotContains(t, got.body, "license-key")
}

// Review focus: the confirmation submitted twice issues one license. The second answer leads to the
// page of that license and shows no key: a key is shown by the first issue and by asking for it
// again, which is journaled.
func Test_IssueLicense_SubmittedAgain_LeadsToTheLicenseAndShowsNoKey(t *testing.T) {
	t.Run("the license is there already", func(t *testing.T) {
		b := newBench(t)
		b.store.customer, b.store.license = acme(), issuedDraft()
		b.writer.shownKey = "THE-STORED-KEY"

		got := b.post("/licenses", draftForm())

		require.Equal(t, http.StatusSeeOther, got.status)
		require.Equal(t, "/licenses/"+draftLicenseID, got.header.Get("Location"))
		requireNoKey(t, got)
		require.Zero(t, b.signer.calls, "nothing is signed again")
		require.Zero(t, b.writer.writes, "nothing is written: no key is read, and no second line of the journal")
		require.Empty(t, b.promoter.fingerprints)
	})
	t.Run("the other request recorded it first", func(t *testing.T) {
		b := newBench(t)
		b.store.customer = acme()
		b.writer.notAdded = true
		b.writer.onRecord = func() { b.store.license = issuedDraft() }

		got := b.post("/licenses", draftForm())

		require.Equal(t, http.StatusSeeOther, got.status)
		require.Equal(t, "/licenses/"+draftLicenseID, got.header.Get("Location"))
		requireNoKey(t, got)
		require.Len(t, b.writer.records, 1, "the store was asked once, and added nothing")
		require.Empty(t, b.writer.shown)
		require.Empty(t, b.promoter.fingerprints)
	})
}

// Review focus: a confirmation never answers for another draft. Under the id of a license that is
// there, a draft that says anything else is refused: nothing is signed, written or shown.
func Test_IssueLicense_UnderTheIDOfALicenseWithOtherContent_IsRefused(t *testing.T) {
	changes := map[string]func(form url.Values){
		"another plan":             func(form url.Values) { form.Set(issuing.FieldPlan, "another") },
		"other features":           func(form url.Values) { form[issuing.FieldFeatures] = []string{"sso"} },
		"no feature":               func(form url.Values) { form.Del(issuing.FieldFeatures) },
		"every feature":            func(form url.Values) { form.Del(issuing.FieldFeatures); form.Set(issuing.FieldAllFeatures, "1") },
		"another expiry":           func(form url.Values) { form.Set(issuing.FieldExpiresAt, "2027-10-09") },
		"another cap on sources":   func(form url.Values) { form.Set(issuing.FieldMaxSources, "6") },
		"no cap on sources":        func(form url.Values) { form.Set(issuing.FieldMaxSources, "") },
		"another grace period":     func(form url.Values) { form.Set(issuing.FieldGraceDays, "8") },
		"another telemetry":        func(form url.Values) { form.Set(issuing.FieldTelemetry, "none") },
		"a telemetry not written":  func(form url.Values) { form.Set(issuing.FieldTelemetry, "") },
		"a license that it renews": func(form url.Values) { form.Set(issuing.FieldSucceeds, previousLicenseID) },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			b := newBench(t)
			b.store.customer = acme()
			b.store.licenses = map[string]*cpstore.LicenseDetail{draftLicenseID: issuedDraft()}
			b.writer.shownKey = "THE-STORED-KEY"
			form := draftForm()
			change(form)

			got := b.post("/licenses", form)

			require.Equal(t, http.StatusConflict, got.status)
			require.Contains(t, got.body, "A license with this id already exists, with other content.")
			requireNoKey(t, got)
			require.Zero(t, b.signer.calls, "nothing is signed")
			require.Zero(t, b.writer.writes, "nothing is written")
			require.Empty(t, b.promoter.fingerprints)
			requireLayout(t, got)
		})
	}
	t.Run("the license of another customer", func(t *testing.T) {
		b := newBench(t)
		b.store.customer, b.store.license = acme(), issuedDraft()
		b.store.license.CustomerID = uuid.New()

		got := b.post("/licenses", draftForm())

		require.Equal(t, http.StatusConflict, got.status)
		requireNoKey(t, got)
		require.Zero(t, b.signer.calls)
		require.Zero(t, b.writer.writes)
	})
	t.Run("another request recorded another draft first", func(t *testing.T) {
		b := newBench(t)
		b.store.customer = acme()
		b.writer.notAdded = true
		b.writer.onRecord = func() {
			b.store.license = issuedDraft()
			b.store.license.Plan = "another"
		}

		got := b.post("/licenses", draftForm())

		require.Equal(t, http.StatusConflict, got.status)
		require.Contains(t, got.body, "A license with this id already exists, with other content.")
		requireNoKey(t, got)
		require.Empty(t, b.promoter.fingerprints)
	})
	t.Run("the license that took the id cannot be read", func(t *testing.T) {
		b := newBench(t)
		b.store.customer = acme()
		b.writer.notAdded = true

		got := b.post("/licenses", draftForm())

		require.Equal(t, http.StatusInternalServerError, got.status)
		requireNoKey(t, got)
		require.Empty(t, b.promoter.fingerprints)
	})
}

// The id of a license is drawn when its form is sent, never when a form is shown: a form shown
// twice, or reloaded, would otherwise carry an id that is not the one of what the operator filled.
func Test_TheFormsOfALicense_CarryNoLicenseID(t *testing.T) {
	customerPath := "/customers/" + customerID.String()
	for _, path := range []string{
		customerPath + "/licenses/new", customerPath + "/licenses/new?trial=1", "/licenses/" + previousLicenseID + "/renew",
	} {
		t.Run(path, func(t *testing.T) {
			b := newBench(t)
			b.store.customer, b.store.license = acme(), renewable()

			got := b.get(path)

			require.Equal(t, http.StatusOK, got.status)
			require.Contains(t, got.body, `action="/licenses/confirm"`)
			require.NotContains(t, got.body, "license_id")
		})
	}
}

func Test_IssueLicense_Refused(t *testing.T) {
	t.Run("without the license id of a confirmation", func(t *testing.T) {
		b := newBench(t)
		b.store.customer = acme()
		form := draftForm()
		form.Del(issuing.FieldLicenseID)

		got := b.post("/licenses", form)

		require.Equal(t, http.StatusBadRequest, got.status)
		require.Zero(t, b.signer.calls)
		require.Zero(t, b.writer.writes)
	})
	t.Run("a draft with problems", func(t *testing.T) {
		b := newBench(t)
		b.store.customer = acme()
		form := draftForm()
		form.Add(issuing.FieldFeatures, "not_a_feature")

		got := b.post("/licenses", form)

		require.Equal(t, http.StatusBadRequest, got.status)
		require.Contains(t, got.body, "is not a declared feature.")
		require.Zero(t, b.signer.calls)
		require.Zero(t, b.writer.writes)
	})
	for name, err := range map[string]error{
		"the license already has a successor": cpstore.ErrAlreadySucceeded,
		"the license is one of another":       cpstore.ErrOtherCustomer,
		"the license to succeed is not there": cpstore.ErrNotFound,
	} {
		t.Run(name, func(t *testing.T) {
			b := newBench(t)
			b.store.customer = acme()
			b.writer.recordErr = err

			got := b.post("/licenses", draftForm())

			require.Equal(t, http.StatusConflict, got.status)
			require.NotContains(t, got.body, keyOf(draftLicenseID))
			require.NotContains(t, got.body, "<textarea")
			require.Empty(t, b.promoter.fingerprints)
			requireLayout(t, got)
		})
	}
	t.Run("the signer fails", func(t *testing.T) {
		b := newBench(t)
		b.store.customer = acme()
		b.signer.err = errors.New(`invalid license request: [telemetry "NOTE-OF-THE-DRAFT" is not online]`)

		got := b.post("/licenses", draftForm())

		require.Equal(t, http.StatusInternalServerError, got.status)
		require.Contains(t, got.body, "<h1>Something went wrong</h1>")
		require.Zero(t, b.writer.writes)
		require.Contains(t, b.logs.String(), "level=ERROR")
		require.NotContains(t, b.logs.String(), "NOTE-OF-THE-DRAFT", "what the signer says may quote the form")
	})
	t.Run("the store fails", func(t *testing.T) {
		b := newBench(t)
		b.store.customer = acme()
		b.writer.recordErr = errors.New("the database is away")

		got := b.post("/licenses", draftForm())

		require.Equal(t, http.StatusInternalServerError, got.status)
		require.NotContains(t, got.body, keyOf(draftLicenseID))
		require.Contains(t, b.logs.String(), "the database is away")
		require.NotContains(t, b.logs.String(), keyOf(draftLicenseID))
	})
}

// The hourly pass of the public server promotes later: the issue stands.
func Test_IssueLicense_APromotionThatFails_IsLoggedAndTheKeyIsShown(t *testing.T) {
	b := newBench(t)
	b.store.customer = acme()
	b.promoter.err = errors.New("the promotion is away")

	got := b.post("/licenses", draftForm())

	require.Equal(t, http.StatusOK, got.status)
	require.Contains(t, got.body, keyOf(draftLicenseID))
	require.Contains(t, b.logs.String(), "the promotion is away")
	require.NotContains(t, b.logs.String(), keyOf(draftLicenseID))
}

func Test_ShowKeyAgain_IsJournaledByTheStoreAndShowsTheKey(t *testing.T) {
	b := newBench(t)
	b.writer.shownKey = "THE-STORED-KEY"

	got := b.post("/licenses/"+draftLicenseID+"/key", nil)

	require.Equal(t, http.StatusOK, got.status)
	require.Equal(t, "no-store", got.header.Get("Cache-Control"))
	require.Contains(t, between(t, got.body, "<textarea", "</textarea>"), ">THE-STORED-KEY")
	require.Contains(t, got.body, `href="/licenses/`+draftLicenseID+`"`)
	require.Equal(t, []string{draftLicenseID}, b.writer.shown)
	require.Equal(t, []string{operatorEmail}, b.writer.operators)
	require.NotContains(t, b.logs.String(), "THE-STORED-KEY")
	require.Contains(t, b.logs.String(), `route="POST /licenses/{id}/key"`)
}

func Test_ShowKeyAgain_OfALicenseThatIsNotThere_AnswersNotFound(t *testing.T) {
	for name, path := range map[string]string{"unknown": "/licenses/lic-9/key", "not storable": "/licenses/%00/key"} {
		t.Run(name, func(t *testing.T) {
			b := newBench(t)
			b.writer.showErr = cpstore.ErrNotFound

			got := b.post(path, nil)

			require.Equal(t, http.StatusNotFound, got.status)
			require.NotContains(t, got.body, "<textarea")
		})
	}
}

func renewable() *cpstore.LicenseDetail {
	grace, maxSources := 21, 4
	return &cpstore.LicenseDetail{
		LicenseSummary: cpstore.LicenseSummary{
			ID: previousLicenseID, CustomerID: customerID, CustomerName: "Acme", Plan: "standard",
			ExpiresAt: time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC),
		},
		Features: []string{"sso"}, StoredTelemetry: "none", GraceDays: &grace,
		Limits: &license.Limits{MaxSources: &maxSources},
	}
}

func Test_RenewForm_IsPrefilledFromTheLicenseItSucceeds(t *testing.T) {
	b := newBench(t)
	b.store.customer, b.store.license = acme(), renewable()

	got := b.get("/licenses/" + previousLicenseID + "/renew")

	require.Equal(t, http.StatusOK, got.status)
	require.Contains(t, got.body, "<h1>Renew a license</h1>")
	require.Contains(t, got.body, `<input type="hidden" name="succeeds" value="`+previousLicenseID+`">`)
	require.Contains(t, between(t, got.body, "<dl", "</dl>"), `href="/licenses/`+previousLicenseID+`"`)
	require.Contains(t, got.body, `name="plan" value="standard"`)
	require.Contains(t, got.body, `name="features" value="sso" checked>`)
	require.Contains(t, got.body, `name="all_features" value="1">`)
	require.Contains(t, got.body, `name="max_sources" type="number" min="0" max="100000" step="1" value="4"`)
	require.Contains(t, got.body, `name="expires_at" type="date" value="2027-12-31"`)
	require.Contains(t, got.body, `step="1" value="21"`)
	require.Contains(t, got.body, `<option value="none" selected>`)
}

func Test_RenewForm_OfALicenseThatCannotBeRenewedHere_SaysWhyAndHasNoForm(t *testing.T) {
	maxJobs := 3
	cases := map[string]struct {
		change  func(l *cpstore.LicenseDetail)
		message string
	}{
		"it has a successor":          {func(l *cpstore.LicenseDetail) { l.SuccessorIDs = []string{"aaaaaaaaaaaaaaaa"} }, "already has a successor"},
		"a limit a form cannot carry": {func(l *cpstore.LicenseDetail) { l.Limits.MaxJobs = &maxJobs }, "husonym-license"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			b := newBench(t)
			b.store.customer, b.store.license = acme(), renewable()
			tc.change(b.store.license)

			got := b.get("/licenses/" + previousLicenseID + "/renew")

			require.Equal(t, http.StatusOK, got.status)
			require.Contains(t, got.body, "cannot be renewed here")
			require.Contains(t, got.body, tc.message)
			require.NotContains(t, got.body, "<form")
			require.NotContains(t, b.logs.String(), "level=ERROR")
			requireLayout(t, got)
		})
	}
}

// A license issued on the command line, or imported, bears whatever id it was given: it is renewed
// from the console as any other, under a drawn id.
func Test_ALicenseOfAnyID_IsRenewed(t *testing.T) {
	const importedID = "lic-2024-001"
	b := newBench(t)
	imported := renewable()
	imported.ID = importedID
	b.store.customer = acme()
	b.store.licenses = map[string]*cpstore.LicenseDetail{importedID: imported}

	page := b.get("/licenses/" + importedID)
	require.Contains(t, page.body, `<a class="button" href="/licenses/`+importedID+`/renew">Renew</a>`)
	form := b.get("/licenses/" + importedID + "/renew")
	require.Equal(t, http.StatusOK, form.status)
	require.Contains(t, form.body, "<h1>Renew a license</h1>")
	require.Contains(t, form.body, `<input type="hidden" name="succeeds" value="`+importedID+`">`)

	confirm := b.post("/licenses/confirm", formOf(t, form.body, "/licenses/confirm"))
	require.Equal(t, http.StatusOK, confirm.status)
	require.Contains(t, section(t, confirm.body, "beside-the-key"), `href="/licenses/`+importedID+`"`)
	confirmation := formOf(t, confirm.body, "/licenses")
	require.Equal(t, importedID, confirmation.Get(issuing.FieldSucceeds))
	id := confirmation.Get(issuing.FieldLicenseID)
	require.Regexp(t, `^[0-9a-f]{16}$`, id, "the license that succeeds it bears a drawn id")

	issued := b.post("/licenses", confirmation)
	require.Equal(t, http.StatusOK, issued.status)
	require.Contains(t, between(t, issued.body, "<textarea", "</textarea>"), ">"+keyOf(id))
	require.Len(t, b.writer.records, 1)
	require.Equal(t, importedID, b.writer.records[0].succeeds, "the link is given to the store")
	require.Equal(t, id, b.writer.records[0].key.Id)
}

// A renewal that cannot succeed is refused when its form is sent, before any page asks to confirm
// it: the license to renew is not there, is the one of another customer, or has its successor.
func Test_ConfirmLicense_OfARenewalThatCannotSucceed_IsRefusedBeforeTheConfirmation(t *testing.T) {
	cases := map[string]struct {
		previous func() *cpstore.LicenseDetail
		message  string
	}{
		"the license to renew is not there": {
			func() *cpstore.LicenseDetail { return nil }, "The license to renew is not recorded.",
		},
		"the license to renew is the one of another customer": {
			func() *cpstore.LicenseDetail {
				previous := renewable()
				previous.CustomerID = uuid.New()
				return previous
			}, "The license to renew is a license of another customer.",
		},
		"the license to renew has a successor": {
			func() *cpstore.LicenseDetail {
				previous := renewable()
				previous.SuccessorIDs = []string{"aaaaaaaaaaaaaaaa"}
				return previous
			}, "The license to renew already has a successor: a license is renewed once.",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			b := newBench(t)
			b.store.customer = acme()
			b.store.licenses = map[string]*cpstore.LicenseDetail{}
			if previous := tc.previous(); previous != nil {
				b.store.licenses[previousLicenseID] = previous
			}
			form := draftForm()
			form.Del(issuing.FieldLicenseID)
			form.Set(issuing.FieldSucceeds, previousLicenseID)

			got := b.post("/licenses/confirm", form)

			require.Equal(t, http.StatusConflict, got.status)
			require.Contains(t, got.body, tc.message)
			require.NotContains(t, got.body, "Issue the license")
			require.NotContains(t, got.body, "<form")
			require.Equal(t, []string{previousLicenseID}, b.store.askedFor, "the store is asked for the license to renew")
			require.Zero(t, b.signer.calls)
			require.Zero(t, b.writer.writes)
			requireLayout(t, got)
		})
	}
	t.Run("the store fails", func(t *testing.T) {
		b := newBench(t)
		b.store.customer = acme()
		b.store.licenses = map[string]*cpstore.LicenseDetail{}
		form := draftForm()
		form.Set(issuing.FieldSucceeds, previousLicenseID)
		// The customer is read, then the license to renew: only that second read fails.
		b.store.licenseErr = errors.New("the database is away")

		got := b.post("/licenses/confirm", form)

		require.Equal(t, http.StatusInternalServerError, got.status)
		require.NotContains(t, got.body, "Issue the license")
		require.Contains(t, b.logs.String(), "the database is away")
	})
	t.Run("a draft with problems is told its problems first", func(t *testing.T) {
		b := newBench(t)
		b.store.customer = acme()
		b.store.licenses = map[string]*cpstore.LicenseDetail{}
		form := draftForm()
		form.Set(issuing.FieldSucceeds, previousLicenseID)
		form.Set(issuing.FieldGraceDays, "-3")

		got := b.post("/licenses/confirm", form)

		require.Equal(t, http.StatusBadRequest, got.status)
		require.Contains(t, got.body, "The grace period cannot be negative.")
	})
}

func Test_LicensePage_OfALicenseThatHasASuccessor_DoesNotOfferToRenew(t *testing.T) {
	b := newBench(t)
	b.store.license = renewable()
	b.store.license.SuccessorIDs = []string{"aaaaaaaaaaaaaaaa"}

	got := b.get("/licenses/" + previousLicenseID)

	require.Equal(t, http.StatusOK, got.status)
	require.NotContains(t, got.body, "/renew")
	require.NotContains(t, got.body, ">Renew<")
	require.Contains(t, between(t, got.body, "<dt>Succeeded by</dt>", "</dd>"), `href="/licenses/aaaaaaaaaaaaaaaa"`)
	require.Contains(t, got.body, "Show the key again", "its key can still be shown")
	require.NotContains(t, got.body, "not configured")
}

func Test_LicensePage_NamesItsIssuerAndOffersToRenewAndToShowTheKey(t *testing.T) {
	b := newBench(t)
	b.store.license = renewable()
	b.store.license.IssuedBy = "issuer@example.com"
	b.store.license.JournaledAt = time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC)

	got := b.get("/licenses/" + previousLicenseID)

	issuer := between(t, got.body, "<dt>Issued from the console by</dt>", "</dd>")
	require.Contains(t, issuer, "issuer@example.com")
	require.Contains(t, issuer, "2026-10-01 09:30")
	require.Contains(t, got.body, `<a class="button" href="/licenses/`+previousLicenseID+`/renew">Renew</a>`)
	require.Contains(t, got.body,
		`<form method="post" action="/licenses/`+previousLicenseID+`/key"><button type="submit">Show the key again</button></form>`)

	b.store.license.IssuedBy, b.store.license.JournaledAt = "", time.Time{}
	require.NotContains(t, b.get("/licenses/"+previousLicenseID).body, "Issued from the console by")
}

// Review focus: a POST another origin makes the browser send issues nothing and writes nothing.
func Test_ACrossOriginPost_IsRefusedOnEveryRouteAndNothingIsRecorded(t *testing.T) {
	routes := map[string]url.Values{
		"/customers":                           {"external_id": {"cust-1"}, "name": {"Acme"}},
		"/customers/" + customerID.String():    {"name": {"Acme"}},
		"/licenses/confirm":                    draftForm(),
		"/licenses":                            draftForm(),
		"/licenses/" + draftLicenseID + "/key": nil,
	}
	shapes := map[string]map[string]string{
		"Sec-Fetch-Site cross-site": crossSite, "Sec-Fetch-Site same-site": sameSite, "a foreign Origin": foreignOrigin,
	}
	for path, form := range routes {
		for shape, headers := range shapes {
			t.Run(path+" "+shape, func(t *testing.T) {
				b := newBench(t)
				b.store.customer = acme()
				b.writer.newID, b.writer.shownKey = customerID, "THE-STORED-KEY"

				got := b.post(path, form, headers)

				require.Equal(t, http.StatusForbidden, got.status)
				require.Contains(t, got.body, "<h1>Refused</h1>")
				requireLayout(t, got)
				require.NotContains(t, got.body, "KEY")
				require.Zero(t, b.writer.writes, "nothing is written")
				require.Zero(t, b.signer.calls, "nothing is signed")
				require.Zero(t, b.store.calls, "nothing is read")
				require.Empty(t, b.promoter.fingerprints)
			})
		}
	}
}

// What is not a browser sends neither header and is let through, as the gates let it through.
func Test_APostWithoutTheHeadersOfABrowser_IsLetThrough(t *testing.T) {
	b := newBench(t)
	b.writer.newID = customerID

	got := b.post("/customers", url.Values{"external_id": {"cust-1"}, "name": {"Acme"}}, map[string]string{})

	require.Equal(t, http.StatusSeeOther, got.status)
}

// Review focus: without a signing key the console manages the customers and issues nothing.
func Test_WithoutASigner_TheCustomersAreManagedAndNothingIsIssued(t *testing.T) {
	b := newBenchWith(t, false)
	b.store.customer, b.store.license = acme(), renewable()
	b.writer.newID, b.writer.shownKey = customerID, "THE-STORED-KEY"
	customerPath := "/customers/" + customerID.String()

	customer := b.get(customerPath)
	require.Contains(t, customer.body, "Issuing licenses is not configured on this server.")
	require.NotContains(t, customer.body, "/licenses/new")
	require.Contains(t, customer.body, `href="`+customerPath+`/edit"`)
	licensePage := b.get("/licenses/" + previousLicenseID)
	require.Contains(t, licensePage.body, "Issuing licenses is not configured on this server.")
	require.NotContains(t, licensePage.body, "/renew")

	for _, path := range []string{customerPath + "/licenses/new", customerPath + "/licenses/new?trial=1", "/licenses/" + previousLicenseID + "/renew"} {
		got := b.get(path)
		require.Equal(t, http.StatusNotFound, got.status, path)
		require.Contains(t, got.body, "<h1>Not found</h1>", path)
		require.NotContains(t, got.body, "<form", path)
	}
	for _, path := range []string{"/licenses/confirm", "/licenses"} {
		got := b.post(path, draftForm())
		require.Equal(t, http.StatusNotFound, got.status, path)
		require.Contains(t, got.body, "<h1>Not found</h1>", path)
	}
	require.Zero(t, b.writer.writes)

	require.Equal(t, http.StatusSeeOther, b.post("/customers", url.Values{"external_id": {"cust-2"}, "name": {"Other"}}).status)
	require.Equal(t, http.StatusSeeOther, b.post(customerPath, url.Values{"name": {"Acme Corp"}}).status)
	// Showing a key again signs nothing.
	shown := b.post("/licenses/"+previousLicenseID+"/key", nil)
	require.Equal(t, http.StatusOK, shown.status)
	require.Contains(t, shown.body, "THE-STORED-KEY")
}

func Test_Journal_ListsTheActsInOrderWithTheirLinks(t *testing.T) {
	b := newBench(t)
	at := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	b.store.journal = []cpstore.OperatorAction{
		{ID: 5, At: at.Add(4 * time.Minute), Operator: "b@example.com", Action: cpstore.ActionLicenseKeyShown, CustomerID: customerID, CustomerName: "Acme", LicenseID: draftLicenseID},
		{
			ID: 4, At: at.Add(3 * time.Minute), Operator: "a@example.com", Action: cpstore.ActionLicenseRenewed, CustomerID: customerID,
			CustomerName: "Acme", LicenseID: draftLicenseID,
			Detail: map[string]string{"succeeds": previousLicenseID, "plan": "standard", "expires_at": "2027-10-08T23:59:59Z", "telemetry": "online"},
		},
		{ID: 3, At: at.Add(2 * time.Minute), Operator: "a@example.com", Action: cpstore.ActionLicenseIssued, CustomerID: customerID, CustomerName: "Acme", LicenseID: previousLicenseID},
		{
			ID: 2, At: at.Add(time.Minute), Operator: "a@example.com", Action: cpstore.ActionCustomerUpdated, CustomerID: customerID,
			CustomerName: "Acme", Detail: map[string]string{"old_name": "<b>Acme</b>", "new_name": "Acme"},
		},
		{ID: 1, At: at, Operator: "a@example.com", Action: cpstore.ActionCustomerCreated, CustomerID: customerID, CustomerName: "Acme"},
		{ID: 0, At: at.Add(-time.Minute), Operator: "a@example.com", Action: "something_else"},
	}

	got := b.get("/journal")

	require.Equal(t, http.StatusOK, got.status)
	require.Equal(t, 200, b.store.askedLimit, "the 200 most recent lines")
	require.Contains(t, got.body, "<h1>Journal</h1>")
	require.Contains(t, between(t, got.body, "<header", "</header>"), `<a href="/journal" aria-current="page">Journal</a>`)
	require.Equal(t, []string{"Journal of the operators"}, tablesOf(t, got.body))
	last := -1
	for _, phrase := range []string{
		"was shown the key of the license again", "issued the license, as a renewal", "<td>issued the license</td>",
		"changed the customer", "recorded the customer", "something_else",
	} {
		at := strings.Index(got.body, phrase)
		require.Greater(t, at, last, "%q comes in the order of the journal", phrase)
		last = at
	}
	shown := row(t, got.body, "was shown the key")
	require.Contains(t, shown, "2026-10-08 09:04")
	require.Contains(t, shown, "b@example.com")
	require.Contains(t, shown, `<a href="/customers/`+customerID.String()+`">Acme</a>`)
	require.Contains(t, shown, `<a class="id" href="/licenses/`+draftLicenseID+`">`)
	require.Contains(t, row(t, got.body, "as a renewal"),
		"expires at: 2027-10-08T23:59:59Z; plan: standard; succeeds: "+previousLicenseID+"; telemetry: online")
	require.Contains(t, row(t, got.body, "changed the customer"), "old name: &lt;b&gt;Acme&lt;/b&gt;")
	require.Contains(t, row(t, got.body, "something_else"), "<td>—</td><td>—</td>")
	require.NotContains(t, got.body, "<b>")
}

func Test_Journal_Empty_SaysSo(t *testing.T) {
	b := newBench(t)

	got := b.get("/journal")

	require.Equal(t, http.StatusOK, got.status)
	require.Contains(t, got.body, "Nothing.")
}

func Test_APostWhoseBodyIsOverTheCapOrUnreadable_IsRefusedAndNothingIsWritten(t *testing.T) {
	atTheCap := "external_id=cust-1&name=Acme&note=" + strings.Repeat("a", 64<<10-len("external_id=cust-1&name=Acme&note="))
	cases := map[string]struct {
		body   string
		status int
	}{
		"over 64 KiB":  {atTheCap + "a", http.StatusRequestEntityTooLarge},
		"not a form":   {"name=%zz", http.StatusBadRequest},
		"a bad escape": {"external_id=cust-1&name=Acme%", http.StatusBadRequest},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			b := newBench(t)

			got := b.postBody("/customers", tc.body)

			require.Equal(t, tc.status, got.status)
			require.Zero(t, b.writer.writes)
			requireLayout(t, got)
		})
	}
	t.Run("at the cap, the form is read", func(t *testing.T) {
		b := newBench(t)

		got := b.postBody("/customers", atTheCap)

		// The note is over its own length: the form was read, and says so.
		require.Equal(t, http.StatusBadRequest, got.status)
		require.Contains(t, got.body, "The note is too long.")
	})
}

func Test_EachWriteRoute_LogsItsPatternAndNoPath(t *testing.T) {
	routes := map[string]string{
		"/customers/new": `route="GET /customers/new"`,
		"/customers/" + customerID.String() + "/edit":         `route="GET /customers/{id}/edit"`,
		"/customers/" + customerID.String() + "/licenses/new": `route="GET /customers/{id}/licenses/new"`,
		"/licenses/lic-SECRET/renew":                          `route="GET /licenses/{id}/renew"`,
		"/journal":                                            `route="GET /journal"`,
	}
	for path, route := range routes {
		t.Run(path, func(t *testing.T) {
			b := newBench(t)

			b.get(path)

			line := strings.TrimSpace(b.logs.String())
			require.Equal(t, 1, strings.Count(line, "\n")+1, "one line: %s", line)
			require.Contains(t, line, route)
			require.NotContains(t, line, "SECRET")
		})
	}
}
