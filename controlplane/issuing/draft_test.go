package issuing

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

var draftNow = time.Date(2026, 10, 8, 15, 4, 5, 0, time.UTC)

func validForm() url.Values {
	return url.Values{
		FieldLicenseID:          {"0123456789abcdef"},
		FieldCustomerExternalID: {"acme"},
		FieldCustomerName:       {"Acme Co."},
		FieldPlan:               {"a plan"},
		FieldFeatures:           {"sso", "rbac"},
		FieldMaxSources:         {"5"},
		FieldExpiresAt:          {"2027-10-08"},
		FieldGraceDays:          {"7"},
		FieldTelemetry:          {"offline_report"},
		FieldNote:               {"a note"},
		FieldSucceeds:           {"fedcba9876543210"},
	}
}

func Test_FieldNames(t *testing.T) {
	require.Equal(t, []string{
		"license_id", "customer_external_id", "customer_name", "plan", "features", "all_features",
		"max_sources", "expires_at", "grace_days", "telemetry", "note", "succeeds",
	}, []string{
		FieldLicenseID, FieldCustomerExternalID, FieldCustomerName, FieldPlan, FieldFeatures, FieldAllFeatures,
		FieldMaxSources, FieldExpiresAt, FieldGraceDays, FieldTelemetry, FieldNote, FieldSucceeds,
	})
}

func Test_ParseDraft_ReadsEveryField(t *testing.T) {
	draft, problems := ParseDraft(validForm(), draftNow)

	require.Empty(t, problems)
	sources, grace := 5, 7
	require.Equal(t, &Draft{
		LicenseID:          "0123456789abcdef",
		CustomerExternalID: "acme",
		CustomerName:       "Acme Co.",
		Plan:               "a plan",
		Features:           []string{"sso", "rbac"},
		MaxSources:         &sources,
		ExpiresAt:          time.Date(2027, 10, 8, 23, 59, 59, 0, time.UTC),
		GraceDays:          &grace,
		Telemetry:          "offline_report",
		Note:               "a note",
		Succeeds:           "fedcba9876543210",
	}, draft)
}

func Test_ParseDraft_EmptyFieldsMeanNotWritten(t *testing.T) {
	draft, problems := ParseDraft(url.Values{
		FieldCustomerExternalID: {" acme "},
		FieldCustomerName:       {" Acme Co. "},
		FieldAllFeatures:        {"1"},
		FieldExpiresAt:          {"2026-10-08"},
		FieldMaxSources:         {""},
		FieldGraceDays:          {" "},
		FieldTelemetry:          {""},
	}, draftNow)

	require.Empty(t, problems)
	require.True(t, validLicenseID(draft.LicenseID), "an id is drawn")
	require.Equal(t, "acme", draft.CustomerExternalID)
	require.Equal(t, "Acme Co.", draft.CustomerName)
	require.True(t, draft.AllFeatures)
	require.Nil(t, draft.Features)
	require.Nil(t, draft.MaxSources)
	require.Nil(t, draft.GraceDays)
	require.Empty(t, draft.Telemetry)
	require.Equal(t, time.Date(2026, 10, 8, 23, 59, 59, 0, time.UTC), draft.ExpiresAt, "today still holds until its end")

	other, problems := ParseDraft(url.Values{
		FieldCustomerExternalID: {"acme"}, FieldCustomerName: {"Acme Co."}, FieldExpiresAt: {"2027-01-01"},
	}, draftNow)
	require.Empty(t, problems)
	require.NotEqual(t, draft.LicenseID, other.LicenseID, "each draft has its own id")
}

func Test_ParseDraft_AllFeaturesAndNoFeatureAreNotTheSame(t *testing.T) {
	form := validForm()
	form.Del(FieldFeatures)
	none, problems := ParseDraft(form, draftNow)
	require.Empty(t, problems)
	require.False(t, none.AllFeatures)
	require.NotNil(t, none.Features)
	require.Empty(t, none.Features)

	form.Set(FieldAllFeatures, "1")
	all, problems := ParseDraft(form, draftNow)
	require.Empty(t, problems)
	require.True(t, all.AllFeatures)
	require.Nil(t, all.Features)

	require.Nil(t, all.request(draftNow).Features)
	require.NotNil(t, none.request(draftNow).Features)
}

func Test_ParseDraft_Problems(t *testing.T) {
	cases := map[string]struct {
		alter func(form url.Values)
		want  string
	}{
		"unknown feature": {
			func(f url.Values) { f.Add(FieldFeatures, "not_a_feature") },
			`The feature "not_a_feature" is not a declared feature.`,
		},
		"the wildcard as a feature": {
			func(f url.Values) { f.Add(FieldFeatures, license.FeatureWildcard) },
			`The feature "*" is not a declared feature.`,
		},
		"a feature twice": {
			func(f url.Values) { f.Add(FieldFeatures, "sso") },
			`The feature "sso" is listed twice.`,
		},
		"all features and a list": {
			func(f url.Values) { f.Set(FieldAllFeatures, "1") },
			"Choose either all the features or a list of features, not both.",
		},
		"all features unreadable": {
			func(f url.Values) { f.Del(FieldFeatures); f.Set(FieldAllFeatures, "yes") },
			"The choice of all the features is unreadable.",
		},
		"date in the past": {
			func(f url.Values) { f.Set(FieldExpiresAt, "2026-10-07") },
			"The expiry date is in the past.",
		},
		"date unreadable": {
			func(f url.Values) { f.Set(FieldExpiresAt, "08/10/2027") },
			"The expiry date must be written YYYY-MM-DD.",
		},
		"no date": {
			func(f url.Values) { f.Del(FieldExpiresAt) },
			"The expiry date is required.",
		},
		"negative cap": {
			func(f url.Values) { f.Set(FieldMaxSources, "-1") },
			"The maximum number of sources cannot be negative.",
		},
		"cap not a number": {
			func(f url.Values) { f.Set(FieldMaxSources, "five") },
			"The maximum number of sources must be a whole number.",
		},
		"negative grace": {
			func(f url.Values) { f.Set(FieldGraceDays, "-3") },
			"The grace period cannot be negative.",
		},
		"grace not a number": {
			func(f url.Values) { f.Set(FieldGraceDays, "1.5") },
			"The grace period must be a whole number of days.",
		},
		"unknown telemetry mode": {
			func(f url.Values) { f.Set(FieldTelemetry, "sometimes") },
			`The telemetry mode "sometimes" is not one of online, offline_report, none.`,
		},
		"id not 16 hex": {
			func(f url.Values) { f.Set(FieldLicenseID, "0123456789ABCDEF") },
			"The license id must be 16 lowercase hexadecimal characters.",
		},
		"id too short": {
			func(f url.Values) { f.Set(FieldLicenseID, "0123") },
			"The license id must be 16 lowercase hexadecimal characters.",
		},
		"no customer": {
			func(f url.Values) { f.Set(FieldCustomerExternalID, " ") },
			"The customer is required.",
		},
		"no customer name": {
			func(f url.Values) { f.Del(FieldCustomerName) },
			"The name of the customer is required.",
		},
		"plan too long": {
			func(f url.Values) { f.Set(FieldPlan, strings.Repeat("é", 65)) },
			"The plan must be at most 64 characters.",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			form := validForm()
			tc.alter(form)

			draft, problems := ParseDraft(form, draftNow)

			require.Equal(t, []string{tc.want}, problems)
			require.Nil(t, draft, "a form with a problem gives no draft to sign")
		})
	}
}

func Test_ParseDraft_ReportsEveryProblemAtOnce(t *testing.T) {
	form := validForm()
	form.Set(FieldExpiresAt, "soon")
	form.Set(FieldGraceDays, "-1")
	form.Set(FieldTelemetry, "x")

	_, problems := ParseDraft(form, draftNow)

	require.Len(t, problems, 3)
}

func Test_ParseDraft_APlanOfTheMaximumLength(t *testing.T) {
	form := validForm()
	form.Set(FieldPlan, "  "+strings.Repeat("é", 64)+"  ")

	draft, problems := ParseDraft(form, draftNow)

	require.Empty(t, problems)
	require.Equal(t, strings.Repeat("é", 64), draft.Plan)
}

func Test_ParseDraft_AProblemQuotesAShortPartOfWhatWasTyped(t *testing.T) {
	form := validForm()
	form.Set(FieldTelemetry, strings.Repeat("x", 5000))

	_, problems := ParseDraft(form, draftNow)

	require.Len(t, problems, 1)
	require.Less(t, len(problems[0]), 200)
}

func Test_Draft_Form_ThenParseDraft_GivesTheSameDraft(t *testing.T) {
	sources, grace, zero := 5, 7, 0
	expiry := time.Date(2027, 10, 8, 23, 59, 59, 0, time.UTC)
	drafts := map[string]*Draft{
		"every field": {
			LicenseID: "0123456789abcdef", CustomerExternalID: "acme", CustomerName: "Acme & Co. <b>",
			Plan: "a plan", Features: []string{"sso", "rbac"}, MaxSources: &sources, ExpiresAt: expiry,
			GraceDays: &grace, Telemetry: "none", Note: "a note\nof two lines", Succeeds: "fedcba9876543210",
		},
		"all features": {
			LicenseID: "0123456789abcdef", CustomerExternalID: "acme", CustomerName: "Acme Co.",
			AllFeatures: true, ExpiresAt: expiry,
		},
		"no feature, zero cap, no grace": {
			LicenseID: "0123456789abcdef", CustomerExternalID: "acme", CustomerName: "Acme Co.",
			Features: []string{}, MaxSources: &zero, GraceDays: &zero, ExpiresAt: expiry,
		},
	}
	for name, draft := range drafts {
		t.Run(name, func(t *testing.T) {
			again, problems := ParseDraft(draft.Form(), draftNow)

			require.Empty(t, problems)
			require.Equal(t, draft, again)
		})
	}
}

// requireSameButTheID holds a draft read back from the form of one that has no id to that one, but
// for the id ParseDraft drew.
func requireSameButTheID(t *testing.T, draft *Draft) {
	t.Helper()
	require.Empty(t, draft.LicenseID, "a draft made for a form to be filled has no id")
	again, problems := ParseDraft(draft.Form(), draftNow)
	require.Empty(t, problems)
	require.True(t, validLicenseID(again.LicenseID), "the id is drawn when the form is read")
	again.LicenseID = ""
	require.Equal(t, draft, again)
}

func Test_TrialDraft(t *testing.T) {
	customer := &cpstore.CustomerDetail{ID: uuid.New(), ExternalID: "acme", Name: "Acme Co."}

	draft := TrialDraft(customer, draftNow)

	requireSameButTheID(t, draft)
	require.Equal(t, "acme", draft.CustomerExternalID)
	require.Equal(t, "Acme Co.", draft.CustomerName)
	require.Equal(t, time.Date(2026, 11, 7, 23, 59, 59, 0, time.UTC), draft.ExpiresAt, "30 days after now, to the end of the day")
	require.True(t, draft.AllFeatures)
	require.Nil(t, draft.Features)
	require.Nil(t, draft.MaxSources)
	require.Nil(t, draft.GraceDays)
	require.Empty(t, draft.Plan)
	require.Empty(t, draft.Telemetry)
	require.Empty(t, draft.Succeeds)
}

func previousLicense(customer *cpstore.CustomerDetail) *cpstore.LicenseDetail {
	sources, grace := 5, 7
	return &cpstore.LicenseDetail{
		LicenseSummary: cpstore.LicenseSummary{
			ID: "fedcba9876543210", CustomerID: customer.ID, CustomerName: "Acme Co.", Plan: "a plan",
			Telemetry: license.TelemetryNone, ExpiresAt: time.Date(2027, 3, 4, 10, 22, 11, 0, time.UTC),
		},
		Features:        []string{"sso", "rbac"},
		Limits:          &license.Limits{MaxSources: &sources},
		StoredTelemetry: "none",
		GraceDays:       &grace,
		Note:            "the note of the previous one",
	}
}

func Test_RenewalDraft_SameContentOneYearLater(t *testing.T) {
	customer := &cpstore.CustomerDetail{ID: uuid.New(), ExternalID: "acme", Name: "Acme Corporation"}
	previous := previousLicense(customer)

	draft, err := RenewalDraft(previous, customer, draftNow)

	require.NoError(t, err)
	requireSameButTheID(t, draft)
	require.Equal(t, "fedcba9876543210", draft.Succeeds)
	require.Equal(t, "acme", draft.CustomerExternalID)
	require.Equal(t, "Acme Corporation", draft.CustomerName, "the name is the customer's of today")
	require.Equal(t, "a plan", draft.Plan)
	require.Equal(t, []string{"sso", "rbac"}, draft.Features)
	require.False(t, draft.AllFeatures)
	require.Equal(t, 5, *draft.MaxSources)
	require.Equal(t, 7, *draft.GraceDays)
	require.Equal(t, "none", draft.Telemetry)
	require.Empty(t, draft.Note)
	require.Equal(t, time.Date(2028, 3, 4, 23, 59, 59, 0, time.UTC), draft.ExpiresAt,
		"one year after the previous expiry, which is later than now")

	draft.Features[0] = "altered"
	*draft.MaxSources = 99
	*draft.GraceDays = 99
	require.Equal(t, []string{"sso", "rbac"}, previous.Features, "the draft shares nothing with the license it reads")
	require.Equal(t, 5, *previous.Limits.MaxSources)
	require.Equal(t, 7, *previous.GraceDays)
}

func Test_RenewalDraft_OfAnExpiredLicenseCountsFromNow(t *testing.T) {
	customer := &cpstore.CustomerDetail{ID: uuid.New(), ExternalID: "acme", Name: "Acme Co."}
	previous := previousLicense(customer)
	previous.ExpiresAt = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	draft, err := RenewalDraft(previous, customer, draftNow)

	require.NoError(t, err)
	require.Equal(t, time.Date(2027, 10, 8, 23, 59, 59, 0, time.UTC), draft.ExpiresAt)
}

func Test_RenewalDraft_Features(t *testing.T) {
	customer := &cpstore.CustomerDetail{ID: uuid.New(), ExternalID: "acme", Name: "Acme Co."}
	cases := map[string]struct {
		stored  []string
		all     bool
		wantNil bool
	}{
		"no list is every feature":      {stored: nil, all: true, wantNil: true},
		"the wildcard is every feature": {stored: []string{license.FeatureWildcard}, all: true, wantNil: true},
		"an empty list is no feature":   {stored: []string{}, all: false, wantNil: false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			previous := previousLicense(customer)
			previous.Features = tc.stored
			previous.Limits = nil
			previous.GraceDays = nil

			draft, err := RenewalDraft(previous, customer, draftNow)

			require.NoError(t, err)
			require.Equal(t, tc.all, draft.AllFeatures)
			require.Equal(t, tc.wantNil, draft.Features == nil)
			require.Empty(t, draft.Features)
			require.Nil(t, draft.MaxSources)
			require.Nil(t, draft.GraceDays)
		})
	}
}

func Test_RenewalDraft_Refuses(t *testing.T) {
	customer := &cpstore.CustomerDetail{ID: uuid.New(), ExternalID: "acme", Name: "Acme Co."}
	jobs := 3

	t.Run("the license of another customer", func(t *testing.T) {
		previous := previousLicense(customer)
		previous.CustomerID = uuid.New()
		draft, err := RenewalDraft(previous, customer, draftNow)
		require.ErrorIs(t, err, ErrNotTheCustomerOfTheLicense)
		require.Nil(t, draft)
	})
	for name, limits := range map[string]*license.Limits{
		"a cap on jobs":              {MaxJobs: &jobs},
		"a cap on connections":       {MaxConnections: &jobs},
		"a list of connection types": {AllowedConnectionTypes: []string{"postgres"}},
	} {
		t.Run(name, func(t *testing.T) {
			previous := previousLicense(customer)
			previous.Limits = limits
			draft, err := RenewalDraft(previous, customer, draftNow)
			require.ErrorIs(t, err, ErrLimitsNotCarried, "a renewal never drops a limit silently")
			require.Nil(t, draft)
		})
	}
}

func Test_Draft_Lines(t *testing.T) {
	sources, grace, zero := 5, 7, 0
	full := &Draft{
		LicenseID: "0123456789abcdef", CustomerExternalID: "acme", CustomerName: "Acme Co.", Plan: "a plan",
		Features: []string{"sso", "rbac"}, MaxSources: &sources,
		ExpiresAt: time.Date(2027, 10, 8, 23, 59, 59, 0, time.UTC), GraceDays: &grace, Telemetry: "none",
		Note: "a note", Succeeds: "fedcba9876543210",
	}
	require.Equal(t, []Line{
		{"License id", "0123456789abcdef"},
		{"Issued to", "Acme Co."},
		{"Customer id", "acme"},
		{"Plan", "a plan"},
		{"Features", "sso, rbac"},
		{"Maximum sources", "5"},
		{"Expires", "2027-10-08 23:59:59 UTC"},
		{"Grace period", "7 days"},
		{"Telemetry", "none"},
	}, full.Lines())

	bare := &Draft{
		LicenseID: "0123456789abcdef", CustomerExternalID: "acme", CustomerName: "Acme Co.", AllFeatures: true,
		ExpiresAt: time.Date(2027, 10, 8, 23, 59, 59, 0, time.UTC),
	}
	require.Equal(t, []Line{
		{"License id", "0123456789abcdef"},
		{"Issued to", "Acme Co."},
		{"Customer id", "acme"},
		{"Plan", "none"},
		{"Features", "all"},
		{"Maximum sources", "no cap"},
		{"Expires", "2027-10-08 23:59:59 UTC"},
		{"Grace period", "14 days (default)"},
		{"Telemetry", "online (not written)"},
	}, bare.Lines())

	none := &Draft{Features: []string{}, GraceDays: &zero, MaxSources: &zero}
	lines := none.Lines()
	require.Equal(t, Line{"Features", "none"}, lines[4])
	require.Equal(t, Line{"Maximum sources", "0"}, lines[5])
	require.Equal(t, Line{"Grace period", "none"}, lines[7])
}

func Test_Options_ReadsWhatTheProductDeclares(t *testing.T) {
	options := Options()

	declared := make([]string, 0, len(license.AllFeatures()))
	for _, feature := range license.AllFeatures() {
		declared = append(declared, string(feature))
	}
	require.Equal(t, declared, options.Features)
	require.NotContains(t, options.Features, license.FeatureWildcard)
	require.Equal(t, []string{"online", "offline_report", "none"}, options.TelemetryModes)
	require.Empty(t, options.Plans)
	require.True(t, options.PlanIsFreeText)
	require.Equal(t, license.DefaultGraceDays, options.DefaultGraceDays)

	for _, mode := range options.TelemetryModes {
		form := validForm()
		form.Set(FieldTelemetry, mode)
		_, problems := ParseDraft(form, draftNow)
		require.Empty(t, problems)
	}
	form := validForm()
	form[FieldFeatures] = options.Features
	draft, problems := ParseDraft(form, draftNow)
	require.Empty(t, problems)
	require.Equal(t, options.Features, draft.Features)
}
