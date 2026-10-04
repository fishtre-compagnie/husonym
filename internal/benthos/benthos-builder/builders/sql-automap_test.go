package benthosbuilder_builders

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A column is named by an expression when its name stands there as a whole identifier, in
// any case: the catalogs fold the case of a name that was not quoted.
func Test_namesColumn(t *testing.T) {
	for _, tc := range []struct {
		expression, column string
		want               bool
	}{
		{`CHECK ((email ~ '^.+@.+$'))`, "email", true},
		{`email ~ ''`, "email", true},
		{`char_length(email) > 3`, "email", true},
		{`length(x) > 3 AND email`, "email", true},
		{`CHECK (("Email" IS NOT NULL))`, "email", true},
		{`CHECK ((EMAIL IS NOT NULL))`, "Email", true},
		{"regexp_like(`dob`,_utf8mb4'')", "DOB", true},
		// The name is the end of a longer identifier.
		{`CHECK ((work_email IS NOT NULL))`, "email", false},
		{`CHECK ((workemail IS NOT NULL))`, "email", false},
		{`CHECK ((x2email IS NOT NULL))`, "email", false},
		{`CHECK ((my$email IS NOT NULL))`, "email", false},
		// The name is the start of a longer identifier.
		{`CHECK ((email_verified IS NOT NULL))`, "email", false},
		{`CHECK ((emails IS NOT NULL))`, "email", false},
		{`CHECK ((email2 IS NOT NULL))`, "email", false},
		// A name is compared as it is written, not as an expression.
		{`CHECK ((axb > 0))`, "a.b", false},
		{`CHECK ((a.b > 0))`, "a.b", true},
	} {
		require.Equalf(t, tc.want, namesColumn(tc.expression, tc.column), "%s in %s", tc.column, tc.expression)
	}
}
