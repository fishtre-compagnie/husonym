package v1alpha1_jobservice

import (
	"errors"
	"fmt"
	"testing"

	"connectrpc.com/connect"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

// The bound on the wait for the row of a job is set by the guard of the cap on sources. A wait
// that gave up is "the job is being changed" only when that guard was in play.
func Test_sourceWriteError(t *testing.T) {
	gaveUp := fmt.Errorf("unable to update job source: %w", &pgconn.PgError{Code: husonymdb.PqLockNotAvailableCode})

	t.Run("under the guard, a wait that gave up is the job being changed", func(t *testing.T) {
		err := sourceWriteError(gaveUp, true)

		require.Equal(t, connect.CodeAborted, connect.CodeOf(err))
		require.ErrorContains(t, err, "the job is being changed by another request: try again in a moment")
	})

	t.Run("without the guard, the same error is returned as it is", func(t *testing.T) {
		require.Same(t, gaveUp, sourceWriteError(gaveUp, false))
	})

	t.Run("under the guard, another error is returned as it is", func(t *testing.T) {
		other := errors.New("connection reset")

		require.Same(t, other, sourceWriteError(other, true))
	})
}
