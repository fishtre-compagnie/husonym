package issuing

import (
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// storedFrom is the license the store holds once draft was issued to the customer.
func storedFrom(draft *Draft, customerID uuid.UUID) *cpstore.LicenseDetail {
	stored := &cpstore.LicenseDetail{
		LicenseSummary: cpstore.LicenseSummary{
			ID: draft.LicenseID, CustomerID: customerID, Plan: draft.Plan, ExpiresAt: draft.ExpiresAt,
		},
		StoredTelemetry: draft.Telemetry,
		GraceDays:       copyInt(draft.GraceDays),
		PredecessorID:   draft.Succeeds,
	}
	if !draft.AllFeatures {
		stored.Features = append([]string{}, draft.Features...)
	}
	if draft.MaxSources != nil {
		stored.Limits = &license.Limits{MaxSources: copyInt(draft.MaxSources)}
	}
	return stored
}

func Test_Draft_IsTheContentOf_TheLicenseItWasIssuedAs(t *testing.T) {
	customerID := uuid.New()
	every := validDraft(draftNow)
	bare := &Draft{
		LicenseID: "0123456789abcdef", CustomerExternalID: "acme", CustomerName: "Acme Co.", AllFeatures: true,
		ExpiresAt: endOfDay(draftNow.AddDate(1, 0, 0)),
	}
	none := &Draft{
		LicenseID: "0123456789abcdef", CustomerExternalID: "acme", CustomerName: "Acme Co.", Features: []string{},
		ExpiresAt: endOfDay(draftNow.AddDate(1, 0, 0)),
	}

	for name, draft := range map[string]*Draft{"every field": every, "all features": bare, "no feature": none} {
		t.Run(name, func(t *testing.T) {
			require.True(t, draft.IsTheContentOf(storedFrom(draft, customerID), customerID))
		})
	}

	t.Run("the expiry in another zone is the same instant", func(t *testing.T) {
		stored := storedFrom(every, customerID)
		stored.ExpiresAt = stored.ExpiresAt.In(time.FixedZone("ahead", 2*3600))
		require.True(t, every.IsTheContentOf(stored, customerID))
	})
	t.Run("limits that cap nothing are no cap", func(t *testing.T) {
		stored := storedFrom(bare, customerID)
		stored.Limits = &license.Limits{}
		require.True(t, bare.IsTheContentOf(stored, customerID))
	})
}

func Test_Draft_IsTheContentOf_TellsEveryFieldThatDiffers(t *testing.T) {
	customerID := uuid.New()
	number := func(n int) *int { return &n }
	cases := map[string]func(stored *cpstore.LicenseDetail){
		"another id":                  func(l *cpstore.LicenseDetail) { l.ID = "fedcba9876543210" },
		"another plan":                func(l *cpstore.LicenseDetail) { l.Plan = "another plan" },
		"no plan":                     func(l *cpstore.LicenseDetail) { l.Plan = "" },
		"another feature":             func(l *cpstore.LicenseDetail) { l.Features[1] = "job_hooks" },
		"one feature more":            func(l *cpstore.LicenseDetail) { l.Features = append(l.Features, "job_hooks") },
		"the features in other order": func(l *cpstore.LicenseDetail) { l.Features[0], l.Features[1] = l.Features[1], l.Features[0] },
		"an empty list of features":   func(l *cpstore.LicenseDetail) { l.Features = []string{} },
		"no list of features":         func(l *cpstore.LicenseDetail) { l.Features = nil },
		"another cap on sources":      func(l *cpstore.LicenseDetail) { l.Limits.MaxSources = number(6) },
		"no cap on sources":           func(l *cpstore.LicenseDetail) { l.Limits.MaxSources = nil },
		"no limits":                   func(l *cpstore.LicenseDetail) { l.Limits = nil },
		"a cap on jobs":               func(l *cpstore.LicenseDetail) { l.Limits.MaxJobs = number(3) },
		"a cap on connections":        func(l *cpstore.LicenseDetail) { l.Limits.MaxConnections = number(3) },
		"a list of connection types":  func(l *cpstore.LicenseDetail) { l.Limits.AllowedConnectionTypes = []string{"postgres"} },
		"an expiry a day later":       func(l *cpstore.LicenseDetail) { l.ExpiresAt = l.ExpiresAt.AddDate(0, 0, 1) },
		"an expiry a second earlier":  func(l *cpstore.LicenseDetail) { l.ExpiresAt = l.ExpiresAt.Add(-time.Second) },
		"another grace period":        func(l *cpstore.LicenseDetail) { l.GraceDays = number(8) },
		"no grace period":             func(l *cpstore.LicenseDetail) { l.GraceDays = nil },
		"another telemetry":           func(l *cpstore.LicenseDetail) { l.StoredTelemetry = "none" },
		"a telemetry not written":     func(l *cpstore.LicenseDetail) { l.StoredTelemetry = "" },
		"another predecessor":         func(l *cpstore.LicenseDetail) { l.PredecessorID = "aaaaaaaaaaaaaaaa" },
		"no predecessor":              func(l *cpstore.LicenseDetail) { l.PredecessorID = "" },
		"another customer":            func(l *cpstore.LicenseDetail) { l.CustomerID = uuid.New() },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			draft := validDraft(draftNow)
			stored := storedFrom(draft, customerID)
			change(stored)

			require.False(t, draft.IsTheContentOf(stored, customerID))
		})
	}

	// What the product reads from a key that does not say is not what the key says.
	t.Run("a key that does not say is not a key that says online", func(t *testing.T) {
		draft := validDraft(draftNow)
		draft.Telemetry = string(license.TelemetryOnline)
		stored := storedFrom(draft, customerID)
		stored.StoredTelemetry, stored.Telemetry = "", license.TelemetryOnline

		require.False(t, draft.IsTheContentOf(stored, customerID))
	})
	t.Run("no feature is not all of them", func(t *testing.T) {
		all := validDraft(draftNow)
		all.Features, all.AllFeatures = nil, true
		none := validDraft(draftNow)
		none.Features = []string{}

		require.False(t, all.IsTheContentOf(storedFrom(none, customerID), customerID))
		require.False(t, none.IsTheContentOf(storedFrom(all, customerID), customerID))
	})
	t.Run("no license", func(t *testing.T) {
		require.False(t, validDraft(draftNow).IsTheContentOf(nil, customerID))
	})
}
