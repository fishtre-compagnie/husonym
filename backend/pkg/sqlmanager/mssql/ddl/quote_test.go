package ddl

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_QuoteIdentifier(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("n", 128)
	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "plain", input: "users", expected: "[users]"},
		{name: "space", input: "Order Lines", expected: "[Order Lines]"},
		{name: "closing bracket", input: "we]ird", expected: "[we]]ird]"},
		{name: "two closing brackets", input: "a]]b", expected: "[a]]]]b]"},
		{name: "opening bracket", input: "we[ird", expected: "[we[ird]"},
		{name: "single quote", input: "it's", expected: "[it's]"},
		{name: "double quote", input: `say "hi"`, expected: `[say "hi"]`},
		{name: "dot", input: "a.b", expected: "[a.b]"},
		{name: "unicode", input: "données_日本", expected: "[données_日本]"},
		{name: "128 characters", input: long, expected: "[" + long + "]"},
		{name: "empty", input: "", expected: "[]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.expected, QuoteIdentifier(tc.input))
		})
	}
}

func Test_QuoteLiteral(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "plain", input: "users", expected: "N'users'"},
		{name: "space", input: "Order Lines", expected: "N'Order Lines'"},
		{name: "single quote", input: "it's", expected: "N'it''s'"},
		{name: "two single quotes", input: "''", expected: "N''''''"},
		{name: "brackets", input: "[we]]ird]", expected: "N'[we]]ird]'"},
		{name: "double quote", input: `say "hi"`, expected: `N'say "hi"'`},
		{name: "dot", input: "a.b", expected: "N'a.b'"},
		{name: "unicode", input: "données_日本", expected: "N'données_日本'"},
		{name: "empty", input: "", expected: "N''"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.expected, QuoteLiteral(tc.input))
		})
	}
}

func Test_QualifiedName(t *testing.T) {
	t.Parallel()
	require.Equal(t, "[sales].[orders]", QualifiedName("sales", "orders"))
	require.Equal(t, "[we]]ird].[Order ]] Lines]", QualifiedName("we]ird", "Order ] Lines"))
	require.Equal(t, "[a.b].[c]", QualifiedName("a.b", "c"))
	require.Equal(t, "[it's].[select]", QualifiedName("it's", "select"))
}

func Test_QuoteLiteral_OfAQualifiedName(t *testing.T) {
	t.Parallel()
	// A name inside a guard takes both escapes, the brackets first.
	require.Equal(
		t,
		"N'[it''s].[Order ]] Lines]'",
		QuoteLiteral(QualifiedName("it's", "Order ] Lines")),
	)
}
