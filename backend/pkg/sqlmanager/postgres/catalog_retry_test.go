package sqlmanager_postgres

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	pg_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/postgresql"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// catalogChanged is what PostgreSQL answers when what a query resolves was dropped meanwhile.
func catalogChanged() error {
	return fmt.Errorf("unable to read: %w", &pgconn.PgError{
		Severity: "ERROR", Code: "XX000", Message: "cache lookup failed for type 19421",
	})
}

func Test_isCatalogChange(t *testing.T) {
	require.True(t, isCatalogChange(catalogChanged()))
	require.True(t, isCatalogChange(&pgconn.PgError{Code: "XX000", Message: "cache lookup failed for relation 42"}))

	require.False(t, isCatalogChange(nil))
	require.False(t, isCatalogChange(errors.New("cache lookup failed for type 19421")), "not an error of the server")
	require.False(t, isCatalogChange(&pgconn.PgError{Code: "XX000", Message: "could not read block 0"}),
		"another internal error is not passing")
	require.False(t, isCatalogChange(&pgconn.PgError{Code: "42P01", Message: `relation "t" does not exist`}))
	require.False(t, isCatalogChange(&pgconn.PgError{Code: "P0001", Message: "cache lookup failed for type 1"}),
		"raised by a function of the database, not by the server")
}

func Test_retryOnCatalogChange(t *testing.T) {
	t.Run("reads again until the catalog holds still", func(t *testing.T) {
		reads := 0
		got, err := retryOnCatalogChange(context.Background(), func() (string, error) {
			reads++
			if reads < 3 {
				return "", catalogChanged()
			}
			return "schema", nil
		})
		require.NoError(t, err)
		require.Equal(t, "schema", got)
		require.Equal(t, 3, reads)
	})

	t.Run("gives up after its attempts", func(t *testing.T) {
		reads := 0
		_, err := retryOnCatalogChange(context.Background(), func() (string, error) {
			reads++
			return "", catalogChanged()
		})
		require.True(t, isCatalogChange(err))
		require.Equal(t, catalogReadAttempts, reads)
	})

	t.Run("does not read again on another error", func(t *testing.T) {
		reads := 0
		broken := errors.New("connection refused")
		_, err := retryOnCatalogChange(context.Background(), func() (string, error) {
			reads++
			return "", broken
		})
		require.ErrorIs(t, err, broken)
		require.Equal(t, 1, reads)
	})

	t.Run("does not wait once the caller gave up", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		reads := 0
		start := time.Now()
		_, err := retryOnCatalogChange(ctx, func() (string, error) {
			reads++
			return "", catalogChanged()
		})
		require.True(t, isCatalogChange(err))
		require.Equal(t, 1, reads)
		require.Less(t, time.Since(start), catalogReadPause)
	})
}

// Every query of the catalog is read again: the manager is given none that is not.
func Test_catalogRetryQuerier_ReadsEveryQueryAgain(t *testing.T) {
	querierType := reflect.TypeOf((*pg_queries.Querier)(nil)).Elem()
	require.NotZero(t, querierType.NumMethod())

	for i := range querierType.NumMethod() {
		method := querierType.Method(i)
		t.Run(method.Name, func(t *testing.T) {
			inner := pg_queries.NewMockQuerier(t)
			// The arguments of the query, and what it answers: nothing, of the right types.
			args := make([]reflect.Value, method.Type.NumIn())
			anything := make([]any, method.Type.NumIn())
			for j := range args {
				args[j] = reflect.Zero(method.Type.In(j))
				anything[j] = mock.Anything
			}
			args[0] = reflect.ValueOf(context.Background())
			nothing := reflect.Zero(method.Type.Out(0)).Interface()
			inner.On(method.Name, anything...).Return(nothing, catalogChanged()).Once()
			inner.On(method.Name, anything...).Return(nothing, nil).Once()

			wrapped := NewManager(inner, nil, func() {}).querier
			out := reflect.ValueOf(wrapped).MethodByName(method.Name).Call(args)

			require.Nil(t, out[1].Interface(), "the query was not read again")
		})
	}
}
