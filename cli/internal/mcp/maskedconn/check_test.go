package maskedconn

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
)

// A driver's error may quote where and how it connects: the API writes its own words under
// some codes only, and a call cut short may carry the error it was cut in.
func Test_checkError(t *testing.T) {
	t.Parallel()
	const driver = "unable to create sql connection: dial tcp db.internal:5432: user=shop: i/o timeout"
	fromAPI := func(code connect.Code, message string) error {
		return connect.NewWireError(code, errors.New(message))
	}

	for name, tc := range map[string]struct {
		err  error
		kept bool
	}{
		"a system table":           {fromAPI(connect.CodeInvalidArgument, "pg_catalog.pg_authid is a table of the server itself"), true},
		"connection not found":     {fromAPI(connect.CodeNotFound, "unable to find connection by id"), true},
		"permission":               {fromAPI(connect.CodePermissionDenied, "missing connection:view_sensitive"), true},
		"an error the API did not": {fromAPI(connect.CodeUnknown, driver), false},
		"cut short at the API":     {fromAPI(connect.CodeDeadlineExceeded, driver), false},
		"cut short here":           {connect.NewError(connect.CodeDeadlineExceeded, context.DeadlineExceeded), false},
		"the API out of reach":     {connect.NewError(connect.CodeUnavailable, errors.New("dial tcp api.internal:8080")), false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := checkError(tc.err)
			if tc.kept {
				require.Equal(t, tc.err, got)
				return
			}
			require.NotContains(t, got.Error(), "db.internal")
			require.NotContains(t, got.Error(), "api.internal")
			require.NotContains(t, got.Error(), "user=")
		})
	}
}
