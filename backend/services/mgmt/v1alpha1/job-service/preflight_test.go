package v1alpha1_jobservice

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"connectrpc.com/connect"
	"github.com/fishtre-compagnie/husonym/internal/temporal/clientmanager"
	"github.com/stretchr/testify/require"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
)

// The caller learns why a check did not end, never the chain of the workflow that carries
// its identifiers and those of the worker.
func Test_preflightError(t *testing.T) {
	done, cancel := context.WithCancel(context.Background())
	cancel()
	chain := func(inner error) error {
		return fmt.Errorf("workflow execution error (type: JobPreflight, workflowID: preflight-x, runID: r): "+
			"activity error (identity: 97@worker-host): %w", inner)
	}

	for name, test := range map[string]struct {
		ctx     context.Context
		err     error
		code    connect.Code
		message string
	}{
		"no worker": {context.Background(), clientmanager.ErrNoWorker, connect.CodeFailedPrecondition, "no worker serves this account"},
		"caller gave up": {done, errors.New("rpc error: code = DeadlineExceeded"), connect.CodeDeadlineExceeded, "did not end within"},
		"workflow timed out": {context.Background(),
			chain(temporal.NewTimeoutError(enumspb.TIMEOUT_TYPE_START_TO_CLOSE, nil)), connect.CodeDeadlineExceeded, "did not end within"},
		"connection out of reach": {context.Background(),
			chain(temporal.NewApplicationError(`unable to tell whether destination "dest" accepts writes: dial tcp`, "wrapError")),
			connect.CodeUnavailable, `could not end: unable to tell whether destination "dest" accepts writes: dial tcp`},
		"anything else": {context.Background(), errors.New("boom"), connect.CodeInternal, "could not end"},
	} {
		err := preflightError(test.ctx, test.err, slog.Default())
		require.Equal(t, test.code, connect.CodeOf(err), name)
		require.ErrorContains(t, err, test.message, name)
		require.NotContains(t, err.Error(), "worker-host", name)
		require.NotContains(t, err.Error(), "workflowID", name)
	}
}
