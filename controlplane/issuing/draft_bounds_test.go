package issuing

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func Test_ParseDraft_BoundsItsNumbers(t *testing.T) {
	cases := map[string]struct {
		field, typed string
		want         string
	}{
		"grace at the bound":      {FieldGraceDays, "3650", ""},
		"grace of zero":           {FieldGraceDays, "0", ""},
		"grace over the bound":    {FieldGraceDays, "3651", "The grace period cannot be more than 3650 days."},
		"grace no int holds":      {FieldGraceDays, "99999999999999999999999", "The grace period cannot be more than 3650 days."},
		"grace far below zero":    {FieldGraceDays, "-99999999999999999999999", "The grace period cannot be negative."},
		"sources at the bound":    {FieldMaxSources, "100000", ""},
		"sources of zero":         {FieldMaxSources, "0", ""},
		"sources over the bound":  {FieldMaxSources, "100001", "The maximum number of sources cannot be more than 100000."},
		"sources no int holds":    {FieldMaxSources, "99999999999999999999999", "The maximum number of sources cannot be more than 100000."},
		"expiry at the bound":     {FieldExpiresAt, "2036-10-08", ""},
		"expiry over the bound":   {FieldExpiresAt, "2036-10-09", "The expiry date cannot be more than 10 years from now."},
		"expiry far in the years": {FieldExpiresAt, "9999-12-31", "The expiry date cannot be more than 10 years from now."},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			form := validForm()
			form.Set(tc.field, tc.typed)

			draft, problems := ParseDraft(form, draftNow)

			if tc.want == "" {
				require.Empty(t, problems)
				require.NotNil(t, draft)
				return
			}
			require.Equal(t, []string{tc.want}, problems)
			require.Nil(t, draft)
		})
	}

	draft, problems := ParseDraft(func() url.Values {
		form := validForm()
		form.Set(FieldGraceDays, "3650")
		form.Set(FieldMaxSources, "100000")
		form.Set(FieldExpiresAt, "2036-10-08")
		return form
	}(), draftNow)
	require.Empty(t, problems)
	require.Equal(t, MaxGraceDays, *draft.GraceDays)
	require.Equal(t, MaxSourcesCap, *draft.MaxSources)
	require.Equal(t, time.Date(2036, 10, 8, 23, 59, 59, 0, time.UTC), draft.ExpiresAt)
}

func Test_ParseDraft_RefusesTextAKeyCannotCarry(t *testing.T) {
	const notText = " holds a control or formatting character, or text that is not valid UTF-8."
	cases := map[string]struct {
		field, typed string
		want         string
	}{
		"name not UTF-8":              {FieldCustomerName, "Acme \xff", "The name of the customer" + notText},
		"name with a NUL":             {FieldCustomerName, "Acme\x00Co.", "The name of the customer" + notText},
		"name with a line break":      {FieldCustomerName, "Acme\nCo.", "The name of the customer" + notText},
		"name with a tab":             {FieldCustomerName, "Acme\tCo.", "The name of the customer" + notText},
		"name with a DEL":             {FieldCustomerName, "Acme\x7fCo.", "The name of the customer" + notText},
		"name with a C1 control":      {FieldCustomerName, "Acme\u0085Co.", "The name of the customer" + notText},
		"name of the maximum":         {FieldCustomerName, strings.Repeat("é", 200), ""},
		"name over the maximum":       {FieldCustomerName, strings.Repeat("é", 201), "The name of the customer must be at most 200 characters."},
		"name with accents and signs": {FieldCustomerName, "Société Générale & Cie <b> 株式会社", ""},
		"plan not UTF-8":              {FieldPlan, "\xc3\x28", "The plan" + notText},
		"plan with a NUL":             {FieldPlan, "a\x00plan", "The plan" + notText},
		"plan with an escape":         {FieldPlan, "a\x1bplan", "The plan" + notText},
		"note not UTF-8":              {FieldNote, "a note \xff", "The note" + notText},
		"note with a NUL":             {FieldNote, "a\x00note", "The note" + notText},
		"note with a bell":            {FieldNote, "a\x07note", "The note" + notText},
		"note of several lines":       {FieldNote, "a note\r\nof two lines\n\twith a tab", ""},
		"note of the maximum":         {FieldNote, strings.Repeat("é", 1000), ""},
		"note over the maximum":       {FieldNote, strings.Repeat("é", 1001), "The note must be at most 1000 characters."},
		"customer id not UTF-8":       {FieldCustomerExternalID, "acme\xff", "The customer id" + notText},
		"customer id with a NUL":      {FieldCustomerExternalID, "ac\x00me", "The customer id" + notText},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			form := validForm()
			form.Set(tc.field, tc.typed)

			draft, problems := ParseDraft(form, draftNow)

			if tc.want == "" {
				require.Empty(t, problems)
				require.NotNil(t, draft)
				return
			}
			require.Equal(t, []string{tc.want}, problems)
			require.Nil(t, draft)
		})
	}
}

// What ParseDraft lets through, the signing does not refuse for its text: the two agree.
func Test_ParseDraft_WhatItAcceptsIsSigned(t *testing.T) {
	signer, _ := newTestSigner(t)
	now := time.Now().UTC()
	form := validForm()
	form.Set(FieldCustomerName, strings.Repeat("é", 200))
	form.Set(FieldPlan, strings.Repeat("é", 64))
	form.Set(FieldNote, "a note\r\nof two lines")
	form.Set(FieldGraceDays, "3650")
	form.Set(FieldMaxSources, "100000")
	form.Set(FieldExpiresAt, now.AddDate(MaxExpiryYears, 0, 0).Format(dateLayout))

	draft, problems := ParseDraft(form, now)
	require.Empty(t, problems)

	_, key, err := signer.Issue(draft, now)
	require.NoError(t, err)
	require.Equal(t, draft.CustomerName, key.IssuedTo)
	require.False(t, key.GraceEndsAt().Before(key.ExpiresAt), "the end of the grace is an instant that holds")
}

func Test_Options_CarriesTheBounds(t *testing.T) {
	options := Options()

	require.Equal(t, 3650, options.MaxGraceDays)
	require.Equal(t, 100000, options.MaxSourcesCap)
	require.Equal(t, 10, options.MaxExpiryYears)
	require.Equal(t, 200, options.MaxCustomerNameLength)
	require.Equal(t, 64, options.MaxPlanLength)
	require.Equal(t, 1000, options.MaxNoteLength)
}
