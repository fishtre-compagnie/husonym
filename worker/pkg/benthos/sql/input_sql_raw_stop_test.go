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

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range stopChannelSize + 5 {
			mock.ExpectQuery("SELECT").WillReturnError(critical)
			require.ErrorIs(t, input.Connect(context.Background()), critical)
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "the stop signal waits for a listener: the input is held for good")
	}
	require.ErrorIs(t, <-stop, critical)
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

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range stopChannelSize + 5 {
			require.Error(t, input.Connect(context.Background()))
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "the stop signal waits for a listener: the input is held for good")
	}
	require.ErrorContains(t, <-stop, "must be the same length")
}
