package console_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/console"
	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/controlplane/cptest"
	"github.com/fishtre-compagnie/husonym/controlplane/intake"
	"github.com/fishtre-compagnie/husonym/controlplane/issuing"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
)

var (
	inputTag    = regexp.MustCompile(`<input\b[^>]*>`)
	selectTag   = regexp.MustCompile(`(?s)<select\b[^>]*\bname="([^"]*)"[^>]*>(.*?)</select>`)
	chosenValue = regexp.MustCompile(`<option value="([^"]*)" selected>`)
	textareaTag = regexp.MustCompile(`(?s)<textarea\b[^>]*\bname="([^"]*)"[^>]*>(.*?)</textarea>`)
	nameAttr    = regexp.MustCompile(`\bname="([^"]*)"`)
	valueAttr   = regexp.MustCompile(`\bvalue="([^"]*)"`)
	shownKey    = regexp.MustCompile(`(?s)<textarea id="license-key"[^>]*>(.*?)</textarea>`)
)

// formOf reads, from a page, what a browser would send for its form to action, untouched: the
// inputs that have a name, the checked boxes, the chosen options and the texts.
func formOf(t *testing.T, body, action string) url.Values {
	t.Helper()
	markup := between(t, body, `action="`+action+`"`, "</form>")
	form := url.Values{}
	for _, tag := range inputTag.FindAllString(markup, -1) {
		name := nameAttr.FindStringSubmatch(tag)
		if name == nil || (strings.Contains(tag, `type="checkbox"`) && !strings.Contains(tag, " checked")) {
			continue
		}
		value := ""
		if found := valueAttr.FindStringSubmatch(tag); found != nil {
			value = html.UnescapeString(found[1])
		}
		form.Add(name[1], value)
	}
	for _, chosen := range selectTag.FindAllStringSubmatch(markup, -1) {
		if value := chosenValue.FindStringSubmatch(chosen[2]); value != nil {
			form.Add(chosen[1], html.UnescapeString(value[1]))
		}
	}
	for _, text := range textareaTag.FindAllStringSubmatch(markup, -1) {
		form.Add(text[1], html.UnescapeString(text[2]))
	}
	return form
}

// reportingSigner signs as the signer it holds does and, before the license it signed is recorded,
// has an instance report under that key: the report cannot but be kept pending.
type reportingSigner struct {
	*issuing.Signer
	t        *testing.T
	receiver *intake.Intake
	reported []string
}

func (s *reportingSigner) Issue(d *issuing.Draft, now time.Time) (*license.IssuedLicense, *license.Key, error) {
	issued, key, err := s.Signer.Issue(d, now)
	if err != nil {
		return nil, nil, err
	}
	entry := license.RegistryEntry{Id: issued.Id, Encoded: issued.Encoded}
	sealed := cptest.ReportFor(s.t, &entry, instanceOne, time.Now().UTC())
	outcome, err := s.receiver.Receive(s.t.Context(), sealed.Document, sealed.Seal, sealed.Fingerprint)
	require.NoError(s.t, err)
	require.Equal(s.t, intake.Pending, outcome, "the license is not recorded yet")
	s.reported = append(s.reported, sealed.Fingerprint)
	return issued, key, nil
}

// answered is an answer of the walk, and whether it is one of the two that may show a key.
type answered struct {
	what    string
	body    string
	keyPage bool
}

// walk is a console over a real database and a real signer, and every answer it gave.
type walk struct {
	t       *testing.T
	handler http.Handler
	ring    license.Keyring
	answers []answered
}

func (w *walk) request(method, path string, form url.Values) page {
	w.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "http://example.com")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
	}
	req.Header.Set(cptest.AccessHeader, access(w.t).Token(w.t, operatorEmail, time.Now()))
	return serve(w.handler, req)
}

func (w *walk) get(path string) page {
	w.t.Helper()
	got := w.request(http.MethodGet, path, nil)
	require.Equal(w.t, http.StatusOK, got.status, path)
	w.answers = append(w.answers, answered{what: "GET " + path, body: got.body})
	return got
}

// post sends a form and wants the status; keyPage tells the answer is one that may show a key.
func (w *walk) post(path string, form url.Values, status int, keyPage bool) page {
	w.t.Helper()
	got := w.request(http.MethodPost, path, form)
	require.Equal(w.t, status, got.status, "POST %s: %s", path, got.body)
	w.answers = append(w.answers, answered{what: "POST " + path, body: got.body, keyPage: keyPage})
	return got
}

// issue sends the form of a license, confirms it, and gives the confirmation it sent, the id of the
// license and the key the console showed, verified as the product verifies it.
func (w *walk) issue(form url.Values) (confirmation url.Values, id, encoded string, key *license.Key) {
	w.t.Helper()
	confirm := w.post("/licenses/confirm", form, http.StatusOK, false)
	require.Contains(w.t, confirm.body, "A license cannot be deleted or changed once it is issued.")
	confirmation = formOf(w.t, confirm.body, "/licenses")
	id = confirmation.Get(issuing.FieldLicenseID)
	require.Regexp(w.t, `^[0-9a-f]{16}$`, id)
	encoded, key = w.keyOn(w.post("/licenses", confirmation, http.StatusOK, true))
	require.Equal(w.t, id, key.Id)
	return confirmation, id, encoded, key
}

// keyOn reads the key a key page shows and verifies it against the ring.
func (w *walk) keyOn(got page) (encoded string, key *license.Key) {
	w.t.Helper()
	shown := shownKey.FindStringSubmatch(got.body)
	require.Len(w.t, shown, 2, "the page shows a key")
	encoded = html.UnescapeString(shown[1])
	key, err := license.ParseWith(encoded, w.ring)
	require.NoError(w.t, err, "the product's verifier takes the key")
	return encoded, key
}

func journalRows(t *testing.T, body string) []string {
	t.Helper()
	rows := strings.Split(between(t, body, "<tbody>", "</tbody>"), "</tr>")
	return rows[:len(rows)-1]
}

// The acts of the operator over a real database and a real signer, following the pages: a customer,
// a trial, its renewal, the key again, and the journal that tells them.
func Test_Console_RecordsACustomerIssuesRenewsAndJournals(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	pool := cptest.NewDatabase(t)
	store := cpstore.New(pool)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	ring := license.Keyring{"walk-kid": pub}
	inner, err := issuing.NewSigner(priv, ring)
	require.NoError(t, err)
	receiver := intake.New(store, time.Now)
	signer := &reportingSigner{Signer: inner, t: t, receiver: receiver}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pages, err := console.New(&console.Config{
		Reader: store, Writer: store, Signer: signer, Promoter: receiver, Now: time.Now, Logger: logger,
	})
	require.NoError(t, err)
	w := &walk{t: t, handler: access(t).Gate(t, time.Now, logger).Wrap(pages), ring: ring}

	// A customer.
	created := w.post("/customers", url.Values{"external_id": {"cust-walk"}, "name": {"Acme & Sons"}, "note": {"met in spring"}},
		http.StatusSeeOther, false)
	customerPath := created.header.Get("Location")
	customer := w.get(customerPath)
	require.Contains(t, customer.body, "<h1>Acme &amp; Sons</h1>")
	require.Contains(t, customer.body, "No license.")

	// A trial, from the link of the page of the customer.
	trialForm := formOf(t, w.get(html.UnescapeString(hrefTo(t, customer.body, `/customers/[^"/]+/licenses/new\?trial=1`))).body, "/licenses/confirm")
	trialForm.Set(issuing.FieldNote, "a trial")
	trialConfirmation, trialID, trialKey, trial := w.issue(trialForm)
	require.Equal(t, "cust-walk", trial.CustomerId)
	require.Equal(t, "Acme & Sons", trial.IssuedTo)
	require.Nil(t, trial.Features, "a trial lists no feature")
	require.True(t, trial.AllowsEveryFeature())
	require.WithinDuration(t, time.Now().AddDate(0, 0, issuing.TrialDays), trial.ExpiresAt, 25*time.Hour)

	// Review focus: the confirmation sent again issues nothing more and shows that same license.
	again, _ := w.keyOn(w.post("/licenses", trialConfirmation, http.StatusOK, true))
	require.Equal(t, trialKey, again)

	// The page of the license names who issued it; the report that was pending under its key is stored.
	trialPage := w.get("/licenses/" + trialID)
	require.Contains(t, between(t, trialPage.body, "<dt>Issued from the console by</dt>", "</dd>"), operatorEmail)
	require.Contains(t, between(t, trialPage.body, "<dt>Origin</dt>", "</dd>"), "console")
	require.Contains(t, between(t, trialPage.body, "<dt>Kid</dt>", "</dd>"), "walk-kid")
	require.Contains(t, between(t, trialPage.body, "<dt>Signing key fingerprint</dt>", "</dd>"), license.PublicKeyFingerprint(pub))
	require.Contains(t, between(t, trialPage.body, "<dt>Note</dt>", "</dd>"), "a trial")
	require.Contains(t, section(t, trialPage.body, "instances"), instanceOne, "the pending report was promoted")
	require.Len(t, signer.reported, 1)
	stillPending, err := store.PendingReports(t.Context(), signer.reported[0])
	require.NoError(t, err)
	require.Empty(t, stillPending)

	// The customer is renamed in its note only: the keys issued carry its name.
	editForm := formOf(t, w.get(hrefTo(t, w.get(customerPath).body, `/customers/[^"/]+/edit`)).body, customerPath)
	require.Equal(t, url.Values{"name": {"Acme & Sons"}, "note": {"met in spring"}}, editForm)
	editForm.Set("note", "met in spring, trial in autumn")
	w.post(customerPath, editForm, http.StatusSeeOther, false)

	// Its renewal, from the link of the page of the license.
	renewForm := formOf(t, w.get(hrefTo(t, trialPage.body, `/licenses/[^"/]+/renew`)).body, "/licenses/confirm")
	require.Equal(t, trialID, renewForm.Get(issuing.FieldSucceeds))
	_, renewedID, renewedKey, renewed := w.issue(renewForm)
	require.NotEqual(t, trialID, renewedID)
	require.Nil(t, renewed.Features, "the renewal of a key without a list has no list either")
	require.True(t, renewed.ExpiresAt.After(trial.ExpiresAt.AddDate(0, 11, 0)))
	renewedPage := w.get("/licenses/" + renewedID)
	require.Contains(t, between(t, renewedPage.body, "<dt>Succeeds</dt>", "</dd>"), `href="/licenses/`+trialID+`"`)
	require.Contains(t, between(t, w.get("/licenses/"+trialID).body, "<dt>Succeeded by</dt>", "</dd>"), `href="/licenses/`+renewedID+`"`)

	// Review focus: a license has one successor. The form is not offered again, and a form kept
	// from before is refused.
	refusedForm := w.get("/licenses/" + trialID + "/renew")
	require.Contains(t, refusedForm.body, "cannot be renewed here")
	require.NotContains(t, refusedForm.body, "<form")
	renewForm.Set(issuing.FieldLicenseID, "")
	second := formOf(t, w.post("/licenses/confirm", renewForm, http.StatusOK, false).body, "/licenses")
	refused := w.post("/licenses", second, http.StatusConflict, false)
	require.Contains(t, refused.body, "already has a successor")
	_, err = store.LicenseDetail(t.Context(), second.Get(issuing.FieldLicenseID), time.Now())
	require.ErrorIs(t, err, cpstore.ErrNotFound, "nothing was issued")

	// The key again.
	shown, _ := w.keyOn(w.post("/licenses/"+trialID+"/key", nil, http.StatusOK, true))
	require.Equal(t, trialKey, shown)

	// The journal: five acts, the newest first. The confirmation sent twice and the renewal refused
	// left no line.
	journal := w.get("/journal")
	rows := journalRows(t, journal.body)
	require.Len(t, rows, 5)
	for i, phrase := range []string{
		"was shown the key of the license again", "issued the license, as a renewal", "changed the customer",
		"<td>issued the license</td>", "recorded the customer",
	} {
		require.Contains(t, rows[i], phrase)
		require.Contains(t, rows[i], operatorEmail)
		require.Contains(t, rows[i], `href="`+customerPath+`"`)
	}
	require.Contains(t, rows[0], `href="/licenses/`+trialID+`"`)
	require.Contains(t, rows[1], `href="/licenses/`+renewedID+`"`)
	require.Contains(t, rows[1], "succeeds: "+trialID)

	// A license that allows no optional feature, and its renewal: none again, and not all of them.
	noneForm := formOf(t, w.get(hrefTo(t, w.get(customerPath).body, `/customers/[^"/?]+/licenses/new`)).body, "/licenses/confirm")
	require.Empty(t, noneForm[issuing.FieldFeatures])
	require.Empty(t, noneForm.Get(issuing.FieldAllFeatures))
	noneForm.Set(issuing.FieldPlan, "standard")
	noneForm.Set(issuing.FieldMaxSources, "3")
	_, noneID, noneKey, none := w.issue(noneForm)
	require.NotNil(t, none.Features)
	require.Empty(t, none.Features)
	require.False(t, none.AllowsEveryFeature())
	require.WithinDuration(t, time.Now().AddDate(1, 0, 0), none.ExpiresAt, 25*time.Hour)
	noneRenewForm := formOf(t, w.get("/licenses/"+noneID+"/renew").body, "/licenses/confirm")
	_, _, noneRenewedKey, noneRenewed := w.issue(noneRenewForm)
	require.NotNil(t, noneRenewed.Features, "the renewal of a key with an empty list has an empty list")
	require.Empty(t, noneRenewed.Features)
	require.False(t, noneRenewed.AllowsEveryFeature())
	require.Equal(t, "standard", noneRenewed.Plan)
	require.Equal(t, 3, *noneRenewed.Limits.MaxSources)

	// A list of features, kept by the renewal as it is.
	someForm := formOf(t, w.get(customerPath+"/licenses/new").body, "/licenses/confirm")
	someForm[issuing.FieldFeatures] = []string{string(license.FeatureSso), string(license.FeatureRbac)}
	_, someID, someKey, _ := w.issue(someForm)
	_, _, someRenewedKey, someRenewed := w.issue(formOf(t, w.get("/licenses/"+someID+"/renew").body, "/licenses/confirm"))
	require.ElementsMatch(t, []string{"sso", "rbac"}, someRenewed.Features)

	// A key is on the two pages that answer its issue and the asking for it again, and nowhere else.
	for _, path := range []string{"/", "/customers", customerPath, "/pending", "/journal", "/licenses/" + renewedID} {
		w.get(path)
	}
	keys := []string{trialKey, renewedKey, noneKey, noneRenewedKey, someKey, someRenewedKey}
	keyPages := 0
	for _, answer := range w.answers {
		if answer.keyPage {
			keyPages++
			continue
		}
		for _, key := range keys {
			require.NotContains(t, answer.body, key, answer.what)
			require.NotContains(t, answer.body, html.EscapeString(key), answer.what)
		}
		require.NotContains(t, answer.body, `id="license-key"`, answer.what)
	}
	require.Equal(t, 8, keyPages, "six issues, one confirmation sent twice, one key shown again")
}

// Review focus: a POST another origin made the browser send writes nothing in the database.
func Test_Console_ACrossOriginPost_LeavesTheDatabaseAsItWas(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	pool := cptest.NewDatabase(t)
	store := cpstore.New(pool)
	issuer := cptest.NewIssuer(t)
	entry := issuer.Entry("0123456789abcdef", "cust-1", "Acme")
	cptest.AddLicense(t, store, issuer, &entry)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := issuing.NewSigner(priv, license.Keyring{"walk-kid": pub})
	require.NoError(t, err)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pages, err := console.New(&console.Config{
		Reader: store, Writer: store, Signer: signer, Promoter: intake.New(store, time.Now), Now: time.Now, Logger: logger,
	})
	require.NoError(t, err)
	handler := access(t).Gate(t, time.Now, logger).Wrap(pages)
	customers, err := store.Customers(t.Context(), time.Now())
	require.NoError(t, err)
	require.Len(t, customers, 1)
	draft := url.Values{
		"customer": {customers[0].ID.String()}, issuing.FieldCustomerExternalID: {"cust-1"}, issuing.FieldCustomerName: {"Acme"},
		issuing.FieldLicenseID: {"aaaaaaaaaaaaaaaa"}, issuing.FieldAllFeatures: {"1"},
		issuing.FieldExpiresAt: {time.Now().UTC().AddDate(0, 1, 0).Format(time.DateOnly)},
	}
	posts := map[string]url.Values{
		"/customers":                             {"external_id": {"cust-2"}, "name": {"Other"}},
		"/customers/" + customers[0].ID.String(): {"name": {"Renamed"}},
		"/licenses/confirm":                      draft,
		"/licenses":                              draft,
		"/licenses/0123456789abcdef/key":         nil,
	}

	for path, form := range posts {
		for shape, headers := range map[string]map[string]string{"cross-site": crossSite, "same-site": sameSite, "a foreign Origin": foreignOrigin} {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set(cptest.AccessHeader, access(t).Token(t, operatorEmail, time.Now()))
			for name, value := range headers {
				req.Header.Set(name, value)
			}

			got := serve(handler, req)

			require.Equal(t, http.StatusForbidden, got.status, "%s, %s", path, shape)
			require.NotContains(t, got.body, entry.Encoded)
		}
	}

	journal, err := store.Journal(t.Context(), 10)
	require.NoError(t, err)
	require.Empty(t, journal, "nothing was journaled")
	after, err := store.Customers(t.Context(), time.Now())
	require.NoError(t, err)
	require.Len(t, after, 1)
	require.Equal(t, "Acme", after[0].Name)
	require.Equal(t, 1, after[0].Licenses)
}
