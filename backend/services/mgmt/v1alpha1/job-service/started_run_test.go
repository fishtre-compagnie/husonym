package v1alpha1_jobservice

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/stretchr/testify/require"
)

func Test_waitForStartedJobRun(t *testing.T) {
	t.Parallel()
	notVisible := husonymerrors.NewNotFound("workflow not found")

	t.Run("returns the run once it is visible", func(t *testing.T) {
		t.Parallel()
		reads := 0
		run, err := waitForStartedJobRun(t.Context(), 5*time.Second, func(context.Context) (*mgmtv1alpha1.JobRun, error) {
			reads++
			if reads < 2 {
				return nil, notVisible
			}
			return &mgmtv1alpha1.JobRun{Id: "run"}, nil
		})
		require.NoError(t, err)
		require.Equal(t, "run", run.GetId())
	})

	t.Run("returns no run when it is still not visible", func(t *testing.T) {
		t.Parallel()
		run, err := waitForStartedJobRun(t.Context(), 50*time.Millisecond, func(context.Context) (*mgmtv1alpha1.JobRun, error) {
			return nil, notVisible
		})
		require.NoError(t, err)
		require.Nil(t, run)
	})

	// The wait may end while a read is going: the read fails with the deadline, not with a
	// not found, and the run is no less started.
	t.Run("returns no run when the wait ends during a read", func(t *testing.T) {
		t.Parallel()
		run, err := waitForStartedJobRun(t.Context(), 50*time.Millisecond, func(ctx context.Context) (*mgmtv1alpha1.JobRun, error) {
			<-ctx.Done()
			return nil, connect.NewError(connect.CodeDeadlineExceeded, ctx.Err())
		})
		require.NoError(t, err)
		require.Nil(t, run)
	})

	t.Run("tells the caller gave up", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		_, err := waitForStartedJobRun(ctx, 5*time.Second, func(ctx context.Context) (*mgmtv1alpha1.JobRun, error) {
			cancel()
			<-ctx.Done()
			return nil, ctx.Err()
		})
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("fails on another error", func(t *testing.T) {
		t.Parallel()
		broken := errors.New("temporal is down")
		_, err := waitForStartedJobRun(t.Context(), 5*time.Second, func(context.Context) (*mgmtv1alpha1.JobRun, error) {
			return nil, broken
		})
		require.ErrorIs(t, err, broken)
	})
}
