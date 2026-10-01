package husonym_benthos_sql

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	continuation_token "github.com/fishtre-compagnie/husonym/internal/continuation-token"
	"github.com/redpanda-data/benthos/v4/public/service"
	"github.com/stretchr/testify/require"
)

type fixedDbProvider struct {
	db SqlDbtx
}

func (p fixedDbProvider) GetDb(context.Context, string) (SqlDbtx, error) { return p.db, nil }
func (p fixedDbProvider) GetDriver(string) (string, error) {
	return sqlmanager_shared.PostgresDriver, nil
}

// The stream connects its input again after a failure, and each failing attempt signals the
// stop. The activity acts on the first signal and listens no more: the attempts past the room
// of the channel must not hold the input.
func TestInput_StopSignalDoesNotWaitForAListener(t *testing.T) {
	const stopChannelSize = 3
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	critical := errors.New(`relation "public.gone" does not exist`)

	stop := make(chan error, stopChannelSize)
	input := &pooledInput{
		logger:              service.MockResources().Logger(),
		driver:              sqlmanager_shared.PostgresDriver,
		provider:            fixedDbProvider{db: db},
		queryStatic:         "SELECT id FROM public.gone",
		stopActivityChannel: stop,
	}

	for range stopChannelSize + 5 {
		mock.ExpectQuery("SELECT").WillReturnError(critical)
	}
	for _, err := range connectsInTime(t, input, stopChannelSize+5) {
		require.ErrorIs(t, err, critical)
	}
	require.ErrorIs(t, firstSignal(t, stop), critical)
}

// connectsInTime connects the input several times over, as a stream does after a failure, and
// fails the test when an attempt is still going after a moment: it waits on a channel nobody
// listens to anymore.
func connectsInTime(t *testing.T, input *pooledInput, attempts int) []error {
	t.Helper()
	done := make(chan []error, 1)
	go func() {
		errs := make([]error, 0, attempts)
		for range attempts {
			errs = append(errs, input.Connect(context.Background()))
		}
		done <- errs
	}()
	select {
	case errs := <-done:
		return errs
	case <-time.After(2 * time.Second):
		require.FailNow(t, "the stop signal waits for a listener: the input is held for good")
		return nil
	}
}

// firstSignal returns the signal that waits on the channel, without waiting for one.
func firstSignal(t *testing.T, stop <-chan error) error {
	t.Helper()
	select {
	case err := <-stop:
		return err
	default:
		require.FailNow(t, "no stop signal was sent")
		return nil
	}
}

// A page asked with a token that does not match its order columns stops the activity the same
// way, at each attempt.
func TestInput_TokenMismatchSignalDoesNotWaitForAListener(t *testing.T) {
	const stopChannelSize = 3
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	paged, total := "SELECT id FROM t WHERE id > $1 LIMIT $2", 10
	stop := make(chan error, stopChannelSize)
	input := &pooledInput{
		logger:              service.MockResources().Logger(),
		driver:              sqlmanager_shared.PostgresDriver,
		provider:            fixedDbProvider{db: db},
		queryStatic:         "SELECT id FROM t",
		pagedQueryStatic:    &paged,
		expectedTotalRows:   &total,
		orderByColumns:      []string{"id", "name"},
		continuationToken:   continuation_token.NewFromContents(continuation_token.NewContents([]any{int64(1)})),
		stopActivityChannel: stop,
	}

	for _, err := range connectsInTime(t, input, stopChannelSize+5) {
		require.ErrorContains(t, err, "must be the same length")
	}
	require.ErrorContains(t, firstSignal(t, stop), "must be the same length")
}
