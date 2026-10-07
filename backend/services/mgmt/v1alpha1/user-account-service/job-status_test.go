package v1alpha1_useraccountservice

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"connectrpc.com/connect"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/backend/internal/licensegate"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/stretchr/testify/require"
)

// answeringGate is a job gate that answers every job the same.
type answeringGate struct{ answer error }

func (g answeringGate) CheckStored(context.Context, string, string) error { return g.answer }

// What the check a run makes when it starts answers for its job. The run is started on a gate
// that answered, and on that alone.
func Test_JobStatus(t *testing.T) {
	logged := func() (context.Context, *bytes.Buffer) {
		var logs bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&logs, nil))
		return logger_interceptor.SetLoggerContext(context.Background(), logger), &logs
	}

	t.Run("a job the license allows is not refused", func(t *testing.T) {
		ctx, logs := logged()
		s := &Service{jobgate: answeringGate{}}

		refused, err := s.jobStatus(ctx, "an-account", "a-job")

		require.NoError(t, err)
		require.Nil(t, refused)
		require.Empty(t, logs.String())
	})

	t.Run("a refusal is recognised by its type, wherever it is wrapped", func(t *testing.T) {
		ctx, _ := logged()
		refusal := license.NewRefusal(
			"an-account",
			husonymerrors.NewForbidden(licensegate.RefusalMessage([]license.Feature{license.FeatureJobHooks, license.FeatureSubsetting})),
			license.FeatureGate(license.FeatureJobHooks), license.FeatureGate(license.FeatureSubsetting),
		)
		s := &Service{jobgate: answeringGate{answer: fmt.Errorf("checking the job: %w", refusal)}}

		refused, err := s.jobStatus(ctx, "an-account", "a-job")

		require.NoError(t, err)
		require.NotNil(t, refused)
		require.False(t, refused.GetIsValid())
		require.Equal(t, "this job uses features the license does not include: job_hooks, subsetting", refused.GetReason())
	})

	t.Run("an error that only carries the code of a refusal is not one", func(t *testing.T) {
		ctx, _ := logged()
		denied := connect.NewError(connect.CodePermissionDenied, errors.New("the database role may not read this table"))
		s := &Service{jobgate: answeringGate{answer: denied}}

		refused, err := s.jobStatus(ctx, "an-account", "a-job")

		require.Nil(t, refused)
		require.Equal(t, connect.CodeUnavailable, connect.CodeOf(err), "%v", err)
	})

	t.Run("a job the API cannot find does not hold the run back, and is logged as a warning", func(t *testing.T) {
		ctx, logs := logged()
		s := &Service{jobgate: answeringGate{answer: licensegate.ErrJobNotFound}}

		refused, err := s.jobStatus(ctx, "an-account", "a-job")

		require.NoError(t, err)
		require.Nil(t, refused)
		require.Contains(t, logs.String(), `"level":"WARN"`)
		require.Contains(t, logs.String(), "a-job")
	})

	t.Run("a gate that could not answer is an unavailable error, not a valid answer", func(t *testing.T) {
		ctx, _ := logged()
		down := errors.New("the database is down")
		s := &Service{jobgate: answeringGate{answer: fmt.Errorf("unable to get the enabled hooks of job a-job: %w", down)}}

		refused, err := s.jobStatus(ctx, "an-account", "a-job")

		require.Nil(t, refused)
		require.Equal(t, connect.CodeUnavailable, connect.CodeOf(err), "%v", err)
		require.ErrorIs(t, err, down)
	})
}
