package cpstore_test

import (
	"sync"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/controlplane/cptest"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

const operator = "ops@example.com"

// customer creates a customer from the console.
func (s *seeded) customer(externalID, name string) uuid.UUID {
	s.t.Helper()
	id, err := s.store.CreateCustomer(s.t.Context(), operator,
		cpstore.NewCustomer{ExternalID: externalID, Name: name}, today)
	require.NoError(s.t, err)
	return id
}

// issue mints a license for a customer, as the console does, and gives it with its verified key.
func (s *seeded) issue(id, customerID string) (*license.Key, *license.IssuedLicense) {
	s.t.Helper()
	issued := s.issuer.Issue(&license.IssueRequest{
		Id:         id,
		IssuedTo:   "Acme",
		CustomerId: customerID,
		ExpiresAt:  time.Now().UTC().Add(365 * 24 * time.Hour),
		Plan:       "team",
		Telemetry:  string(license.TelemetryOfflineReport),
	})
	key, err := license.ParseWith(issued.Encoded, s.issuer.Keyring())
	require.NoError(s.t, err)
	return key, issued
}

// record records a license issued from the console.
func (s *seeded) record(key *license.Key, issued *license.IssuedLicense, succeeds string) (bool, error) {
	s.t.Helper()
	return s.store.RecordIssuedLicense(s.t.Context(), operator, key, issued,
		s.issuer.SigningKeyFingerprint(), succeeds, "", today)
}

func (s *seeded) journal() []cpstore.OperatorAction {
	s.t.Helper()
	actions, err := s.store.Journal(s.t.Context(), cpstore.JournalCap)
	require.NoError(s.t, err)
	return actions
}

func (s *seeded) count(table string) int {
	s.t.Helper()
	var n int
	require.NoError(s.t, s.pool.QueryRow(s.t.Context(), `SELECT count(*) FROM controlplane.`+table).Scan(&n))
	return n
}

func Test_CreateCustomer_RecordsTheCustomer_AndJournalsIt(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)

	id, err := s.store.CreateCustomer(t.Context(), operator,
		cpstore.NewCustomer{ExternalID: " cust-1 ", Name: " Acme ", Note: "met at the fair"}, today)
	require.NoError(t, err)

	customer, err := s.store.Customer(t.Context(), id, today)
	require.NoError(t, err)
	require.Equal(t, "cust-1", customer.ExternalID)
	require.Equal(t, "Acme", customer.Name)
	require.Equal(t, "met at the fair", customer.Note)
	require.True(t, today.Equal(customer.CreatedAt), "the instant comes from the caller")
	require.True(t, today.Equal(customer.UpdatedAt))

	lines := s.journal()
	require.Len(t, lines, 1)
	require.Equal(t, cpstore.ActionCustomerCreated, lines[0].Action)
	require.Equal(t, operator, lines[0].Operator)
	require.True(t, today.Equal(lines[0].At))
	require.Equal(t, id, lines[0].CustomerID)
	require.Equal(t, "Acme", lines[0].CustomerName)
	require.Empty(t, lines[0].LicenseID)
	require.Equal(t, map[string]string{"external_id": "cust-1", "name": "Acme"}, lines[0].Detail)
}

func Test_CreateCustomer_RefusesAnExternalIdAlreadyTaken(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	s.customer("cust-1", "Acme")

	_, err := s.store.CreateCustomer(t.Context(), operator,
		cpstore.NewCustomer{ExternalID: "cust-1", Name: "Another"}, today)

	require.ErrorIs(t, err, cpstore.ErrCustomerExists)
	require.Equal(t, 1, s.count("customers"))
	require.Len(t, s.journal(), 1)
}

func Test_CreateCustomer_NeedsAnExternalIdAndAName(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)

	for _, incomplete := range []cpstore.NewCustomer{
		{ExternalID: "", Name: "Acme"},
		{ExternalID: "cust-1", Name: " "},
	} {
		_, err := s.store.CreateCustomer(t.Context(), operator, incomplete, today)
		require.ErrorIs(t, err, cpstore.ErrCustomerIncomplete)
	}
	require.Zero(t, s.count("customers"))
	require.Empty(t, s.journal())
}

func Test_UpdateCustomer_ChangesNameAndNote_NeverTheExternalId(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	id := s.customer("cust-1", "Acme")
	later := today.Add(time.Hour)

	require.NoError(t, s.store.UpdateCustomer(t.Context(), "local", id, "Acme Corp", "renamed", later))

	customer, err := s.store.Customer(t.Context(), id, later)
	require.NoError(t, err)
	require.Equal(t, "cust-1", customer.ExternalID)
	require.Equal(t, "Acme Corp", customer.Name)
	require.Equal(t, "renamed", customer.Note)
	require.True(t, today.Equal(customer.CreatedAt))
	require.True(t, later.Equal(customer.UpdatedAt))

	lines := s.journal()
	require.Len(t, lines, 2)
	require.Equal(t, cpstore.ActionCustomerUpdated, lines[0].Action, "the newest line comes first")
	require.Equal(t, "local", lines[0].Operator)
	require.Equal(t, id, lines[0].CustomerID)
	require.Equal(t, map[string]string{"old_name": "Acme", "new_name": "Acme Corp"}, lines[0].Detail)
	require.Equal(t, "Acme Corp", lines[1].CustomerName, "a line bears the name the customer has now")
}

func Test_UpdateCustomer_UnknownOrNameless(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	id := s.customer("cust-1", "Acme")

	require.ErrorIs(t, s.store.UpdateCustomer(t.Context(), operator, uuid.New(), "Acme", "", today), cpstore.ErrNotFound)
	require.ErrorIs(t, s.store.UpdateCustomer(t.Context(), operator, id, " ", "", today), cpstore.ErrCustomerIncomplete)

	require.Len(t, s.journal(), 1)
}

func Test_RecordIssuedLicense_StoresTheLicense_AndJournalsIt(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	customerID := s.customer("cust-1", "Acme")
	key, issued := s.issue("lic-1", "cust-1")
	encoded := issued.Encoded
	issued.Encoded = "  " + encoded + "\n"

	added, err := s.store.RecordIssuedLicense(t.Context(), operator, key, issued,
		s.issuer.SigningKeyFingerprint(), "", "contract 12", today)
	require.NoError(t, err)
	require.True(t, added)

	stored, err := s.store.LicenseByFingerprint(t.Context(), telemetry.KeyFingerprint(encoded))
	require.NoError(t, err)
	require.Equal(t, encoded, stored.Encoded, "the key is stored without the space around it")

	detail, err := s.store.LicenseDetail(t.Context(), "lic-1", today)
	require.NoError(t, err)
	require.Equal(t, customerID, detail.CustomerID)
	require.Equal(t, "console", detail.Origin)
	require.Equal(t, "contract 12", detail.Note)
	require.Equal(t, issued.Kid, detail.Kid)
	require.Equal(t, s.issuer.SigningKeyFingerprint(), detail.SigningKeyFingerprint)
	require.Equal(t, "team", detail.Plan)
	require.Empty(t, detail.PredecessorID)
	require.Equal(t, operator, detail.IssuedBy)
	require.True(t, today.Equal(detail.JournaledAt))

	lines := s.journal()
	require.Len(t, lines, 2)
	require.Equal(t, cpstore.ActionLicenseIssued, lines[0].Action)
	require.Equal(t, operator, lines[0].Operator)
	require.Equal(t, customerID, lines[0].CustomerID)
	require.Equal(t, "Acme", lines[0].CustomerName)
	require.Equal(t, "lic-1", lines[0].LicenseID)
	require.Equal(t, map[string]string{
		"plan":       "team",
		"expires_at": key.ExpiresAt.UTC().Format(time.RFC3339),
		"telemetry":  "offline_report",
	}, lines[0].Detail)
}

// What the console records of a key is what the import of the registry records of the same key,
// but for where it came from.
func Test_RecordIssuedLicense_StoresWhatTheRegistryImportWould(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	fromConsole := newSeeded(t)
	fromRegistry := newSeeded(t)
	fromRegistry.issuer = fromConsole.issuer
	grace := 14
	maxJobs := 10
	request := &license.IssueRequest{
		Id: "lic-1", IssuedTo: "Acme", CustomerId: "cust-1",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
		GraceDays: &grace, Limits: &license.Limits{MaxJobs: &maxJobs},
		Plan: "team", Features: []string{}, Telemetry: string(license.TelemetryNone),
	}
	issued := fromConsole.issuer.Issue(request)
	key, err := license.ParseWith(issued.Encoded, fromConsole.issuer.Keyring())
	require.NoError(t, err)

	fromConsole.customer("cust-1", "Acme")
	added, err := fromConsole.record(key, issued, "")
	require.NoError(t, err)
	require.True(t, added)

	entry := license.RegistryEntry{
		Id: issued.Id, Encoded: issued.Encoded, Kid: issued.Kid,
		KeyFingerprint: fromRegistry.issuer.SigningKeyFingerprint(),
	}
	cptest.AddLicense(t, fromRegistry.store, fromRegistry.issuer, &entry)

	row := func(s *seeded) string {
		var columns string
		require.NoError(t, s.pool.QueryRow(t.Context(), `
			SELECT (to_jsonb(l) - 'origin' - 'created_at' - 'customer_id')::text
			FROM controlplane.licenses l WHERE id = 'lic-1'`).Scan(&columns))
		return columns
	}
	require.JSONEq(t, row(fromRegistry), row(fromConsole))
}

func Test_RecordIssuedLicense_Twice_AddsOnce_AndJournalsOnce(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	s.customer("cust-1", "Acme")
	key, issued := s.issue("lic-1", "cust-1")

	added, err := s.record(key, issued, "")
	require.NoError(t, err)
	require.True(t, added)
	added, err = s.record(key, issued, "")
	require.NoError(t, err)
	require.False(t, added)

	require.Equal(t, 1, countLicenses(t, s.pool))
	require.Len(t, s.journal(), 2, "the customer and the license, once each")
}

func Test_RecordIssuedLicense_DoesNotCreateTheCustomer(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	key, issued := s.issue("lic-1", "cust-unknown")

	added, err := s.record(key, issued, "")

	require.ErrorIs(t, err, cpstore.ErrNotFound)
	require.False(t, added)
	require.Zero(t, s.count("customers"))
	require.Zero(t, countLicenses(t, s.pool))
	require.Empty(t, s.journal())
}

func Test_RecordIssuedLicense_Renewal_LinksThePredecessor_Once(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	s.customer("cust-1", "Acme")
	s.customer("cust-2", "Zeta")
	key, issued := s.issue("lic-1", "cust-1")
	_, err := s.record(key, issued, "")
	require.NoError(t, err)

	key, issued = s.issue("lic-other", "cust-2")
	_, err = s.record(key, issued, "lic-1")
	require.ErrorIs(t, err, cpstore.ErrOtherCustomer)

	key, issued = s.issue("lic-orphan", "cust-1")
	_, err = s.record(key, issued, "lic-none")
	require.ErrorIs(t, err, cpstore.ErrNotFound)

	key, issued = s.issue("lic-2", "cust-1")
	added, err := s.record(key, issued, "lic-1")
	require.NoError(t, err)
	require.True(t, added)

	lines := s.journal()
	require.Len(t, lines, 4, "two customers, a license and its renewal")
	require.Equal(t, cpstore.ActionLicenseRenewed, lines[0].Action)
	require.Equal(t, "lic-2", lines[0].LicenseID)
	require.Equal(t, "lic-1", lines[0].Detail["succeeds"])

	detail, err := s.store.LicenseDetail(t.Context(), "lic-2", today)
	require.NoError(t, err)
	require.Equal(t, "lic-1", detail.PredecessorID)
	require.Equal(t, operator, detail.IssuedBy)

	// The same renewal confirmed again is the license already there, not a second successor.
	added, err = s.record(key, issued, "lic-1")
	require.NoError(t, err)
	require.False(t, added)

	key, issued = s.issue("lic-3", "cust-1")
	added, err = s.record(key, issued, "lic-1")
	require.ErrorIs(t, err, cpstore.ErrAlreadySucceeded)
	require.False(t, added)

	require.Equal(t, 2, countLicenses(t, s.pool))
	require.Len(t, s.journal(), 4)
}

func Test_RecordIssuedLicense_ConcurrentRenewals_OneSucceeds(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	s.customer("cust-1", "Acme")
	key, issued := s.issue("lic-1", "cust-1")
	_, err := s.record(key, issued, "")
	require.NoError(t, err)

	const callers = 2
	keys := make([]*license.Key, callers)
	minted := make([]*license.IssuedLicense, callers)
	for i, id := range []string{"lic-2a", "lic-2b"} {
		keys[i], minted[i] = s.issue(id, "cust-1")
	}
	added := make([]bool, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Go(func() {
			added[i], errs[i] = s.store.RecordIssuedLicense(t.Context(), operator, keys[i], minted[i],
				s.issuer.SigningKeyFingerprint(), "lic-1", "", today)
		})
	}
	wg.Wait()

	succeeded, refused := 0, 0
	for i := range callers {
		switch {
		case errs[i] == nil && added[i]:
			succeeded++
		default:
			require.ErrorIs(t, errs[i], cpstore.ErrAlreadySucceeded)
			require.False(t, added[i])
			refused++
		}
	}
	require.Equal(t, 1, succeeded)
	require.Equal(t, 1, refused)
	require.Equal(t, 2, countLicenses(t, s.pool))

	detail, err := s.store.LicenseDetail(t.Context(), "lic-1", today)
	require.NoError(t, err)
	require.Len(t, detail.SuccessorIDs, 1)
	require.Len(t, s.journal(), 3, "the customer, the license and one renewal")
}

func Test_ShowLicenseKey_GivesTheKey_AndJournalsIt(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	entry := s.license("lic-1", "cust-1", "Acme", today.AddDate(1, 0, 0))

	encoded, err := s.store.ShowLicenseKey(t.Context(), operator, "lic-1", today)
	require.NoError(t, err)
	require.Equal(t, entry.Encoded, encoded)

	lines := s.journal()
	require.Len(t, lines, 1)
	require.Equal(t, cpstore.ActionLicenseKeyShown, lines[0].Action)
	require.Equal(t, operator, lines[0].Operator)
	require.Equal(t, "lic-1", lines[0].LicenseID)
	require.Equal(t, s.customerID("cust-1"), lines[0].CustomerID)
	require.Empty(t, lines[0].Detail)

	_, err = s.store.ShowLicenseKey(t.Context(), operator, "lic-none", today)
	require.ErrorIs(t, err, cpstore.ErrNotFound)
	require.Len(t, s.journal(), 1)
}

func Test_Journal_NewestFirst_WithinItsLimit(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	id := s.customer("cust-1", "Acme")
	require.NoError(t, s.store.UpdateCustomer(t.Context(), operator, id, "Acme 2", "", today.Add(-time.Hour)))
	require.NoError(t, s.store.UpdateCustomer(t.Context(), operator, id, "Acme 3", "", today.Add(time.Hour)))

	all, err := s.store.Journal(t.Context(), 1_000_000)
	require.NoError(t, err)
	require.Len(t, all, 3)
	require.Equal(t, "Acme 3", all[0].Detail["new_name"])
	require.Equal(t, cpstore.ActionCustomerCreated, all[1].Action)
	require.Equal(t, "Acme 2", all[2].Detail["new_name"], "ordered by the instant of the act, not by when it was written")

	two, err := s.store.Journal(t.Context(), 2)
	require.NoError(t, err)
	require.Len(t, two, 2)

	for _, limit := range []int{0, -5} {
		one, err := s.store.Journal(t.Context(), limit)
		require.NoError(t, err)
		require.Len(t, one, 1, "a limit under one gives one line")
		require.Equal(t, all[0].ID, one[0].ID)
	}
}

// A write whose line of the journal cannot be written is not kept. The journal is made to refuse
// every line by a constraint added to the table of this test's own database.
func Test_OperatorWrites_AreNotKept_WithoutTheirJournalLine(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	id := s.customer("cust-1", "Acme")
	first, firstIssued := s.issue("lic-1", "cust-1")
	_, err := s.record(first, firstIssued, "")
	require.NoError(t, err)
	before := s.journal()
	s.exec(`ALTER TABLE controlplane.operator_actions ADD CONSTRAINT refuse_every_line CHECK (false) NOT VALID`)

	_, err = s.store.CreateCustomer(t.Context(), operator, cpstore.NewCustomer{ExternalID: "cust-2", Name: "Zeta"}, today)
	require.ErrorContains(t, err, "refuse_every_line")
	require.Equal(t, 1, s.count("customers"))

	err = s.store.UpdateCustomer(t.Context(), operator, id, "Renamed", "changed", today.Add(time.Hour))
	require.ErrorContains(t, err, "refuse_every_line")
	customer, err := s.store.Customer(t.Context(), id, today)
	require.NoError(t, err)
	require.Equal(t, "Acme", customer.Name)
	require.Empty(t, customer.Note)
	require.True(t, today.Equal(customer.UpdatedAt))

	key, issued := s.issue("lic-2", "cust-1")
	added, err := s.record(key, issued, "")
	require.ErrorContains(t, err, "refuse_every_line")
	require.False(t, added)
	key, issued = s.issue("lic-3", "cust-1")
	added, err = s.record(key, issued, "lic-1")
	require.ErrorContains(t, err, "refuse_every_line")
	require.False(t, added)
	require.Equal(t, 1, countLicenses(t, s.pool))

	encoded, err := s.store.ShowLicenseKey(t.Context(), operator, "lic-1", today)
	require.ErrorContains(t, err, "refuse_every_line")
	require.Empty(t, encoded, "a key that could not be journaled as shown is not given")

	s.exec(`ALTER TABLE controlplane.operator_actions DROP CONSTRAINT refuse_every_line`)
	require.Equal(t, before, s.journal())

	// The renewal that was not kept left the license free to be succeeded.
	added, err = s.record(key, issued, "lic-1")
	require.NoError(t, err)
	require.True(t, added)
}
