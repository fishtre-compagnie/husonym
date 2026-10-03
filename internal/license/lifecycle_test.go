package license

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func ptr[T any](v T) *T { return &v }

func Test_Key_StateAt(t *testing.T) {
	expires := time.Date(2027, time.March, 1, 12, 0, 0, 0, time.UTC)
	key := &Key{ExpiresAt: expires, GraceDays: ptr(14)}
	day := 24 * time.Hour

	cases := map[string]struct {
		now  time.Time
		want State
	}{
		"31 days before expiry":      {expires.Add(-31 * day), StateValid},
		"30 days before expiry":      {expires.Add(-30 * day), StateValid},
		"30 days minus 1s before":    {expires.Add(-30*day + time.Second), StateExpiring},
		"1s before expiry":           {expires.Add(-time.Second), StateExpiring},
		"at expiry":                  {expires, StateGrace},
		"1s before the end of grace": {expires.Add(14*day - time.Second), StateGrace},
		"at the end of grace":        {expires.Add(14 * day), StateFrozen},
		"long after":                 {expires.Add(400 * day), StateFrozen},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, key.StateAt(tc.now))
		})
	}

	t.Run("no grace freezes at expiry", func(t *testing.T) {
		k := &Key{ExpiresAt: expires, GraceDays: ptr(0)}
		require.Equal(t, StateFrozen, k.StateAt(expires))
		require.Equal(t, StateExpiring, k.StateAt(expires.Add(-time.Second)))
	})
}

func Test_Key_GraceDays(t *testing.T) {
	expires := time.Date(2027, time.March, 1, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour

	cases := map[string]struct {
		grace *int
		want  int
	}{
		"absent":   {nil, 14},
		"negative": {ptr(-3), 0},
		"zero":     {ptr(0), 0},
		"set":      {ptr(30), 30},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			key := &Key{ExpiresAt: expires, GraceDays: tc.grace}
			require.Equal(t, expires.Add(time.Duration(tc.want)*day), key.GraceEndsAt())
		})
	}
	require.Equal(t, 14, DefaultGraceDays)
}

func Test_Key_StateAt_IgnoresIssuedAt(t *testing.T) {
	now := time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC)
	key := &Key{
		IssuedAt:  now.AddDate(1, 0, 0),
		ExpiresAt: now.AddDate(2, 0, 0),
	}
	require.Equal(t, StateValid, key.StateAt(now))
}

func Test_State_Values(t *testing.T) {
	require.Equal(t, State("none"), StateNone)
	require.Equal(t, State("valid"), StateValid)
	require.Equal(t, State("expiring"), StateExpiring)
	require.Equal(t, State("grace"), StateGrace)
	require.Equal(t, State("frozen"), StateFrozen)
}

func Test_Limits_Allows(t *testing.T) {
	var nilLimits *Limits
	require.True(t, nilLimits.Allows("mssql"))
	require.True(t, (&Limits{}).Allows("mssql"))
	require.True(t, (&Limits{AllowedConnectionTypes: []string{}}).Allows("mssql"))

	restricted := &Limits{AllowedConnectionTypes: []string{"postgres"}}
	require.True(t, restricted.Allows("postgres"))
	require.False(t, restricted.Allows("mssql"))
}
