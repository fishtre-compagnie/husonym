package issuing

import (
	"strings"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// A draft built by hand is held to the rules of a draft read from a form: the signing checks them
// itself.
func Test_Signer_Issue_HoldsAHandBuiltDraftToTheRules(t *testing.T) {
	signer, _ := newTestSigner(t)
	now := testNow()
	number := func(n int) *int { return &n }
	const notText = " holds a control or formatting character, or text that is not valid UTF-8."

	cases := map[string]struct {
		alter func(d *Draft)
		want  string
	}{
		"grace at the bound":         {func(d *Draft) { d.GraceDays = number(MaxGraceDays) }, ""},
		"grace over the bound":       {func(d *Draft) { d.GraceDays = number(MaxGraceDays + 1) }, "The grace period cannot be more than 3650 days."},
		"grace that overflows":       {func(d *Draft) { d.GraceDays = number(1 << 40) }, "The grace period cannot be more than 3650 days."},
		"negative grace":             {func(d *Draft) { d.GraceDays = number(-1) }, "The grace period cannot be negative."},
		"sources at the bound":       {func(d *Draft) { d.MaxSources = number(MaxSourcesCap) }, ""},
		"sources over the bound":     {func(d *Draft) { d.MaxSources = number(MaxSourcesCap + 1) }, "The maximum number of sources cannot be more than 100000."},
		"negative sources":           {func(d *Draft) { d.MaxSources = number(-1) }, "The maximum number of sources cannot be negative."},
		"expiry at the horizon":      {func(d *Draft) { d.ExpiresAt = endOfDay(now.AddDate(MaxExpiryYears, 0, 0)) }, ""},
		"expiry over the horizon":    {func(d *Draft) { d.ExpiresAt = endOfDay(now.AddDate(MaxExpiryYears, 0, 1)) }, "The expiry date cannot be more than 10 years from now."},
		"no expiry":                  {func(d *Draft) { d.ExpiresAt = time.Time{} }, "The expiry date is required."},
		"name of the maximum":        {func(d *Draft) { d.CustomerName = strings.Repeat("é", MaxCustomerNameLength) }, ""},
		"name over the maximum":      {func(d *Draft) { d.CustomerName = strings.Repeat("é", MaxCustomerNameLength+1) }, "The name of the customer must be at most 200 characters."},
		"plan over the maximum":      {func(d *Draft) { d.Plan = strings.Repeat("p", MaxPlanLength+1) }, "The plan must be at most 64 characters."},
		"note over the maximum":      {func(d *Draft) { d.Note = strings.Repeat("n", MaxNoteLength+1) }, "The note must be at most 1000 characters."},
		"name not UTF-8":             {func(d *Draft) { d.CustomerName = "Acme \xff" }, "The name of the customer" + notText},
		"name with a NUL":            {func(d *Draft) { d.CustomerName = "Ac\x00me" }, "The name of the customer" + notText},
		"plan with a control":        {func(d *Draft) { d.Plan = "a\x1bplan" }, "The plan" + notText},
		"customer id with a control": {func(d *Draft) { d.CustomerExternalID = "ac\nme" }, "The customer id" + notText},
		"note with a NUL":            {func(d *Draft) { d.Note = "a\x00note" }, "The note" + notText},
		"note of several lines":      {func(d *Draft) { d.Note = "a note\r\nof two lines" }, ""},
		"name with a space around":   {func(d *Draft) { d.CustomerName = " Acme Co." }, "The name of the customer cannot begin or end with a space."},
		"customer id with a space":   {func(d *Draft) { d.CustomerExternalID = "acme " }, "The customer id cannot begin or end with a space."},
		"plan with a space around":   {func(d *Draft) { d.Plan = "a plan " }, "The plan cannot begin or end with a space."},
		"a name of spaces":           {func(d *Draft) { d.CustomerName = "  " }, "The name of the customer cannot begin or end with a space."},
		"license id of another form": {func(d *Draft) { d.LicenseID = "0123456789abcdeg" }, "The license id must be 16 lowercase hexadecimal characters."},
		"no license id":              {func(d *Draft) { d.LicenseID = "" }, "The license id must be 16 lowercase hexadecimal characters."},
		"succeeds none":              {func(d *Draft) { d.Succeeds = "" }, ""},
		"succeeds of another form":   {func(d *Draft) { d.Succeeds = "the-previous-one" }, "The license to renew must be named by an id of 16 lowercase hexadecimal characters."},
		"succeeds in capitals":       {func(d *Draft) { d.Succeeds = "FEDCBA9876543210" }, "The license to renew must be named by an id of 16 lowercase hexadecimal characters."},
		"an unknown feature":         {func(d *Draft) { d.Features = []string{"not_a_feature"} }, `The feature "not_a_feature" is not a declared feature.`},
		"a feature twice":            {func(d *Draft) { d.Features = []string{"sso", "sso"} }, `The feature "sso" is listed twice.`},
		"all features and a list":    {func(d *Draft) { d.AllFeatures = true }, "Choose either all the features or a list of features, not both."},
		"an unknown telemetry mode":  {func(d *Draft) { d.Telemetry = "sometimes" }, `The telemetry mode "sometimes" is not one of online, offline_report, none.`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			draft := validDraft(now)
			tc.alter(draft)

			issued, key, err := signer.Issue(draft, now)

			if tc.want == "" {
				require.NoError(t, err)
				require.NotNil(t, key)
				return
			}
			require.ErrorContains(t, err, tc.want)
			require.Nil(t, issued)
			require.Nil(t, key)
		})
	}
}

func Test_Signer_Issue_NamesEveryProblemOfTheDraft(t *testing.T) {
	signer, _ := newTestSigner(t)
	now := testNow()
	draft := validDraft(now)
	draft.Telemetry = "sometimes"
	draft.Succeeds = "x"

	_, _, err := signer.Issue(draft, now)

	require.ErrorContains(t, err, "The telemetry mode")
	require.ErrorContains(t, err, "The license to renew")
}

// The expiry is judged against the instant given, in the words of this package: the product's
// issuing code would let it through, its own clock being earlier.
func Test_Signer_Issue_RefusesAnExpiryNotAfterTheInstantGiven(t *testing.T) {
	signer, _ := newTestSigner(t)
	now := testNow().AddDate(2, 0, 0)
	draft := validDraft(testNow())
	require.True(t, draft.ExpiresAt.After(time.Now()), "the expiry is ahead of the wall clock")
	require.True(t, draft.ExpiresAt.Before(now))

	_, _, err := signer.Issue(draft, now)
	require.ErrorContains(t, err, "The expiry date is in the past.")

	draft.ExpiresAt = now
	_, _, err = signer.Issue(draft, now)
	require.ErrorContains(t, err, "The expiry date is in the past.", "an expiry at the instant itself is not after it")

	draft.ExpiresAt = now.Add(time.Second)
	_, _, err = signer.Issue(draft, now)
	require.NoError(t, err)
}

func Test_ParseDraft_RefusesFormatCharactersInWhatAKeyShows(t *testing.T) {
	const notText = " holds a control or formatting character, or text that is not valid UTF-8."
	subjects := map[string]string{
		FieldCustomerName:       "The name of the customer",
		FieldCustomerExternalID: "The customer id",
		FieldPlan:               "The plan",
	}
	characters := map[string]string{
		"right-to-left override":  "\u202e",
		"left-to-right override":  "\u202d",
		"right-to-left embedding": "\u202b",
		"right-to-left isolate":   "\u2067",
		"pop directional":         "\u202c",
		"right-to-left mark":      "\u200f",
		"zero width space":        "\u200b",
		"zero width joiner":       "\u200d",
		"byte order mark":         "\ufeff",
		"soft hyphen":             "\u00ad",
		"line separator":          "\u2028",
		"paragraph separator":     "\u2029",
	}
	for field, subject := range subjects {
		for name, character := range characters {
			t.Run(field+"/"+name, func(t *testing.T) {
				form := validForm()
				form.Set(field, "ac"+character+"me")

				draft, problems := ParseDraft(form, draftNow)

				require.Equal(t, []string{subject + notText}, problems)
				require.Nil(t, draft)
			})
		}
	}

	t.Run("the note may hold them", func(t *testing.T) {
		form := validForm()
		form.Set(FieldNote, "see \u200fthe contract")
		_, problems := ParseDraft(form, draftNow)
		require.Empty(t, problems)
	})
}

func Test_ParseDraft_Succeeds(t *testing.T) {
	const wrong = "The license to renew must be named by an id of 16 lowercase hexadecimal characters."
	cases := map[string]string{
		"":                  "",
		"fedcba9876543210":  "",
		"fedcba987654321":   wrong,
		"fedcba98765432100": wrong,
		"FEDCBA9876543210":  wrong,
		"fedcba987654321g":  wrong,
		"../../licenses/x1": wrong,
	}
	for typed, want := range cases {
		t.Run(typed, func(t *testing.T) {
			form := validForm()
			form.Set(FieldSucceeds, typed)

			draft, problems := ParseDraft(form, draftNow)

			if want == "" {
				require.Empty(t, problems)
				require.Equal(t, typed, draft.Succeeds)
				return
			}
			require.Equal(t, []string{want}, problems)
		})
	}
}

func Test_TrialDraft_TrimsWhatTheCustomerSays(t *testing.T) {
	customer := &cpstore.CustomerDetail{ID: uuid.New(), ExternalID: " acme\t", Name: "  Acme Co. \n"}

	draft := TrialDraft(customer, draftNow)

	require.Equal(t, "acme", draft.CustomerExternalID)
	require.Equal(t, "Acme Co.", draft.CustomerName)
	// The first confirmation page and the key signed after it agree.
	requireSameButTheID(t, draft)
}

func Test_TrialDraft_WithoutACustomer(t *testing.T) {
	require.Nil(t, TrialDraft(nil, draftNow))
}

func Test_RenewalDraft_TrimsWhatTheCustomerSays(t *testing.T) {
	customer := &cpstore.CustomerDetail{ID: uuid.New(), ExternalID: " acme ", Name: " Acme Co. "}
	previous := previousLicense(customer)
	previous.Plan = " a plan "

	draft, err := RenewalDraft(previous, customer, draftNow)

	require.NoError(t, err)
	require.Equal(t, "acme", draft.CustomerExternalID)
	require.Equal(t, "Acme Co.", draft.CustomerName)
	require.Equal(t, "a plan", draft.Plan)
	requireSameButTheID(t, draft)
}

func Test_RenewalDraft_WithoutALicenseOrACustomer(t *testing.T) {
	customer := &cpstore.CustomerDetail{ID: uuid.New(), ExternalID: "acme", Name: "Acme Co."}

	draft, err := RenewalDraft(nil, customer, draftNow)
	require.ErrorIs(t, err, ErrNothingToRenew)
	require.Nil(t, draft)

	draft, err = RenewalDraft(previousLicense(customer), nil, draftNow)
	require.ErrorIs(t, err, ErrNothingToRenew)
	require.Nil(t, draft)

	draft, err = RenewalDraft(nil, nil, draftNow)
	require.ErrorIs(t, err, ErrNothingToRenew)
	require.Nil(t, draft)
}

func Test_RenewalDraft_RefusesALicenseWhoseIdADraftCannotName(t *testing.T) {
	customer := &cpstore.CustomerDetail{ID: uuid.New(), ExternalID: "acme", Name: "Acme Co."}
	previous := previousLicense(customer)
	previous.ID = "acme-2025"

	draft, err := RenewalDraft(previous, customer, draftNow)

	require.ErrorIs(t, err, ErrLicenseIDNotRenewable)
	require.Nil(t, draft)
}

// The telemetry of a renewal is what the key of the previous license says, not what the product
// reads from it: a key that does not say stays a key that does not say.
func Test_RenewalDraft_TakesTheTelemetryStored(t *testing.T) {
	customer := &cpstore.CustomerDetail{ID: uuid.New(), ExternalID: "acme", Name: "Acme Co."}
	cases := map[string]struct {
		stored string
		read   license.TelemetryMode
	}{
		"a key that does not say":    {"", license.TelemetryOnline},
		"a key that says online":     {"online", license.TelemetryOnline},
		"a key that says none":       {"none", license.TelemetryNone},
		"a key with an unknown mode": {"sometimes", license.TelemetryOnline},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			previous := previousLicense(customer)
			previous.StoredTelemetry = tc.stored
			previous.Telemetry = tc.read

			draft, err := RenewalDraft(previous, customer, draftNow)

			require.NoError(t, err)
			require.Equal(t, tc.stored, draft.Telemetry)
		})
	}
}

func Test_Draft_Form_NormalisesTheExpiryToTheEndOfItsDay(t *testing.T) {
	draft := validDraft(draftNow)
	draft.ExpiresAt = time.Date(2027, 3, 4, 10, 22, 11, 500, time.UTC)

	again, problems := ParseDraft(draft.Form(), draftNow)

	require.Empty(t, problems)
	require.Equal(t, "2027-03-04", draft.Form().Get(FieldExpiresAt))
	require.Equal(t, time.Date(2027, 3, 4, 23, 59, 59, 0, time.UTC), again.ExpiresAt)

	// The day is the UTC one: 01:00 at +14:00 is still the day before in UTC.
	draft.ExpiresAt = time.Date(2027, 3, 5, 1, 0, 0, 0, time.FixedZone("ahead", 14*3600))
	require.Equal(t, "2027-03-04", draft.Form().Get(FieldExpiresAt))
}

func Test_Draft_Lines_ShowTheExpiryInUTC(t *testing.T) {
	draft := validDraft(draftNow)
	draft.ExpiresAt = time.Date(2027, 3, 5, 9, 59, 59, 0, time.FixedZone("ahead", 14*3600))

	require.Equal(t, Line{"Expires", "2027-03-04 19:59:59 UTC"}, draft.Lines()[6])

	draft.ExpiresAt = time.Date(2027, 3, 4, 12, 0, 0, 0, time.FixedZone("behind", -12*3600))
	require.Equal(t, Line{"Expires", "2027-03-05 00:00:00 UTC"}, draft.Lines()[6])
}
