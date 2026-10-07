package v1alpha1_useraccountservice

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	auth_apikey "github.com/fishtre-compagnie/husonym/backend/internal/auth/apikey"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/backend/internal/licensegate"
	"github.com/fishtre-compagnie/husonym/backend/internal/licenserefusal"
	"github.com/fishtre-compagnie/husonym/internal/apikey"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// answeringGate is a job gate that answers every job the same.
type answeringGate struct{ answer error }

func (g answeringGate) CheckStored(context.Context, string, string) error { return g.answer }

// refusalLog is a counter that remembers the gates it was asked to count, with their account.
type refusalLog struct{ counted []string }

func (l *refusalLog) CountRefusal(_ context.Context, accountId string, gates []license.Gate, _ time.Time) error {
	for _, gate := range gates {
		l.counted = append(l.counted, accountId+" "+string(gate))
	}
	return nil
}

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
		counter := &refusalLog{}
		s := &Service{jobgate: answeringGate{answer: fmt.Errorf("checking the job: %w", refusal)}, refusals: counter}

		refused, err := s.jobStatus(ctx, "an-account", "a-job")

		require.NoError(t, err)
		require.NotNil(t, refused)
		require.False(t, refused.GetIsValid())
		require.Equal(t, "this job uses features the license does not include: job_hooks, subsetting", refused.GetReason())
		require.Equal(t, []string{"an-account job_hooks", "an-account subsetting"}, counter.counted)
	})

	t.Run("a job the license allows counts nothing", func(t *testing.T) {
		ctx, _ := logged()
		counter := &refusalLog{}
		s := &Service{jobgate: answeringGate{}, refusals: counter}

		_, err := s.jobStatus(ctx, "an-account", "a-job")

		require.NoError(t, err)
		require.Empty(t, counter.counted)
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

// failingRefusalCounter is a counter that cannot count.
type failingRefusalCounter struct{ asked int }

func (c *failingRefusalCounter) CountRefusal(context.Context, string, []license.Gate, time.Time) error {
	c.asked++
	return errors.New("the usage database is down")
}

// asTheWorker is the context of a call the worker makes: it is let into any account.
func asTheWorker() context.Context {
	return auth_apikey.SetTokenData(context.Background(), &auth_apikey.TokenContextData{ApiKeyType: apikey.WorkerApiKey})
}

// What IsAccountStatusValid answers, and what it counts, when the license of the instance is
// not in force.
func Test_IsAccountStatusValid_CountsAnExpiredLicenseForARun(t *testing.T) {
	accountId := uuid.NewString()
	jobId := uuid.NewString()
	expired := func(counter licenserefusal.Counter) *Service {
		return &Service{
			cfg:           &Config{IsAuthEnabled: true},
			licenseclient: testutil.NewFakeEELicense(),
			refusals:      counter,
		}
	}

	t.Run("a run that names its job is counted once, for its account", func(t *testing.T) {
		counter := &refusalLog{}

		resp, err := expired(counter).IsAccountStatusValid(asTheWorker(), connect.NewRequest(
			&mgmtv1alpha1.IsAccountStatusValidRequest{AccountId: accountId, JobId: &jobId},
		))

		require.NoError(t, err)
		require.False(t, resp.Msg.GetIsValid())
		require.Equal(t, mgmtv1alpha1.AccountStatus_ACCOUNT_STATUS_ACCOUNT_IN_EXPIRED_STATE, resp.Msg.GetAccountStatus())
		require.Equal(t, []string{accountId + " license_not_in_force"}, counter.counted)
	})

	t.Run("a bare status question is not counted, and is answered the same", func(t *testing.T) {
		counter := &refusalLog{}

		resp, err := expired(counter).IsAccountStatusValid(asTheWorker(), connect.NewRequest(
			&mgmtv1alpha1.IsAccountStatusValidRequest{AccountId: accountId},
		))

		require.NoError(t, err)
		require.False(t, resp.Msg.GetIsValid())
		require.Equal(t, mgmtv1alpha1.AccountStatus_ACCOUNT_STATUS_ACCOUNT_IN_EXPIRED_STATE, resp.Msg.GetAccountStatus())
		require.Empty(t, counter.counted)
	})

	t.Run("a counter that fails changes nothing to the answer", func(t *testing.T) {
		counter := &failingRefusalCounter{}

		resp, err := expired(counter).IsAccountStatusValid(asTheWorker(), connect.NewRequest(
			&mgmtv1alpha1.IsAccountStatusValidRequest{AccountId: accountId, JobId: &jobId},
		))

		require.NoError(t, err)
		require.False(t, resp.Msg.GetIsValid())
		require.Equal(t, mgmtv1alpha1.AccountStatus_ACCOUNT_STATUS_ACCOUNT_IN_EXPIRED_STATE, resp.Msg.GetAccountStatus())
		require.Equal(t, 1, counter.asked)
	})
}

func Test_JobStatus_ACounterThatFailsChangesNothing(t *testing.T) {
	refusal := license.NewRefusal(
		"an-account",
		husonymerrors.NewForbidden("this job uses features the license does not include: subsetting"),
		license.FeatureGate(license.FeatureSubsetting),
	)
	counter := &failingRefusalCounter{}
	s := &Service{jobgate: answeringGate{answer: refusal}, refusals: counter}

	refused, err := s.jobStatus(context.Background(), "an-account", "a-job")

	require.NoError(t, err)
	require.False(t, refused.GetIsValid())
	require.Equal(t, "this job uses features the license does not include: subsetting", refused.GetReason())
	require.Equal(t, 1, counter.asked)
}
