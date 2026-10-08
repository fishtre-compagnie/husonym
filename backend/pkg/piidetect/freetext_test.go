package piidetect

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsFreeText(t *testing.T) {
	cases := []struct {
		name   string
		values []string
		want   bool
	}{
		{"names alone are not free text", []string{"Camille Durand", "Jean Petit", "Awa Diallo"}, false},
		{"a mean of exactly the minimum is free text", []string{"un deux trois", "un deux trois"}, true},
		{"a mean just under the minimum is not", []string{"un deux trois", "un deux"}, false},
		{
			"sentences are free text",
			[]string{"Rappeler Marie Martin avant midi", "Client satisfait du service rendu"},
			true,
		},
		{"empty values are ignored", []string{"", "  ", "Rappeler Marie Martin avant midi"}, true},
		{"the mean decides, not each value", []string{"Merci", "Rappeler Marie Martin avant midi svp"}, true},
		{"no value", nil, false},
		{"only empty values", []string{"", "   "}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, IsFreeText(tc.values))
		})
	}
}
