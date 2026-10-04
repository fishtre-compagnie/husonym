package sqlmanager_postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/cenkalti/backoff/v7"
	pg_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/postgresql"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
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

// fastRetryOptions tries as often as the manager does, without its waits.
func fastRetryOptions() []backoff.RetryOption {
	return []backoff.RetryOption{
		backoff.WithBackOff(&backoff.ConstantBackOff{Interval: time.Millisecond}),
		backoff.WithMaxTries(sqlmanager_shared.CatalogReadAttempts),
	}
}

func Test_isCatalogChange(t *testing.T) {
	require.True(t, isCatalogChange(catalogChanged()))
	require.True(t, isCatalogChange(&pgconn.PgError{Code: "XX000", Message: "cache lookup failed for relation 42"}))
	require.True(t, isCatalogChange(&pgconn.PgError{Code: "XX000", Message: "could not open relation with OID 42"}))

	require.False(t, isCatalogChange(nil))
	require.False(t, isCatalogChange(errors.New("cache lookup failed for type 19421")), "not an error of the server")
	require.False(t, isCatalogChange(&pgconn.PgError{Code: "XX000", Message: "could not read block 0"}),
		"another internal error is not passing")
	require.False(t, isCatalogChange(&pgconn.PgError{Code: "42P01", Message: `relation "t" does not exist`}))
	require.False(t, isCatalogChange(&pgconn.PgError{Code: "P0001", Message: "cache lookup failed for type 1"}),
		"raised by a function of the database, not by the server")
}

// A definition gone is a NULL where the row holds a text: database/sql refuses it with words
// of its own, which the read is told by. Scanned for real, lest the words change.
func Test_isCatalogChange_DefinitionGone(t *testing.T) {
	db, sqlMock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	columns := []string{"schema_name", "table_name", "index_name", "index_definition"}
	sqlMock.ExpectQuery("pg_get_indexdef").WillReturnRows(
		sqlmock.NewRows(columns).AddRow("app", "t", "t_v_idx", nil))
	sqlMock.ExpectQuery("pg_get_indexdef").WillReturnRows(
		sqlmock.NewRows(columns).AddRow("app", "t", "t_v_idx", "CREATE INDEX t_v_idx ON app.t (v)").RowError(0, errors.New("broken")))

	_, err = pg_queries.New().GetIndicesBySchemasAndTables(context.Background(), db, []string{"app.t"})
	require.Error(t, err)
	require.True(t, isCatalogChange(err), "a definition gone is not told a change of the catalog: %v", err)
	require.True(t, isCatalogChange(fmt.Errorf("unable to read: %w", errDefinitionGone)))

	_, err = pg_queries.New().GetIndicesBySchemasAndTables(context.Background(), db, []string{"app.t"})
	require.Error(t, err)
	require.False(t, isCatalogChange(err), "another failure of the read is not passing: %v", err)
	require.False(t, isCatalogChange(errors.New("converting NULL to string is unsupported")), "not a scan")
	require.False(t, isCatalogChange(
		errors.New(`sql: Scan error on column index 2, name "n": converting driver.Value type string ("a") to a int64`),
	), "a scan that fails on a value is not passing")
}

func Test_domainConstraintsHeld(t *testing.T) {
	domain := func(constraints string) []*pg_queries.GetDomainsByTablesRow {
		return []*pg_queries.GetDomainsByTablesRow{
			{Schema: "app", Name: "plain", Constraints: json.RawMessage("[]")},
			{Schema: "app", Name: "amount", Constraints: json.RawMessage(constraints)},
		}
	}
	require.NoError(t, domainConstraintsHeld(nil))
	require.NoError(t, domainConstraintsHeld(domain(`[{"name": "positive", "definition": "CHECK ((VALUE > 0))"}]`)))
	require.NoError(t, domainConstraintsHeld(domain(`not a list`)), "left to the one who reads the list")
	require.NoError(t, domainConstraintsHeld(domain(``)))

	require.ErrorIs(t, domainConstraintsHeld(domain(`[{"name": "positive", "definition": null}]`)), errDefinitionGone)
	require.ErrorIs(t, domainConstraintsHeld(domain(
		`[{"name": "positive", "definition": "CHECK ((VALUE > 0))"}, {"name": "small"}]`,
	)), errDefinitionGone)
}

// A domain that lists a constraint without its definition is read again.
func Test_catalogRetryQuerier_ReadsDomainsAgainWhenADefinitionIsGone(t *testing.T) {
	gone := []*pg_queries.GetDomainsByTablesRow{
		{Schema: "app", Name: "amount", Constraints: json.RawMessage(`[{"name": "positive", "definition": null}]`)},
	}
	held := []*pg_queries.GetDomainsByTablesRow{
		{Schema: "app", Name: "amount", Constraints: json.RawMessage(`[]`)},
	}

	t.Run("until the list holds", func(t *testing.T) {
		inner := pg_queries.NewMockQuerier(t)
		inner.EXPECT().GetDomainsByTables(mock.Anything, mock.Anything, mock.Anything).Return(gone, nil).Twice()
		inner.EXPECT().GetDomainsByTables(mock.Anything, mock.Anything, mock.Anything).Return(held, nil).Once()
		wrapped := &catalogRetryQuerier{inner: inner, retryOpts: fastRetryOptions}

		domains, err := wrapped.GetDomainsByTables(context.Background(), nil, []string{"app.t"})

		require.NoError(t, err)
		require.Equal(t, held, domains)
	})

	t.Run("and fails rather than tell a constraint without definition", func(t *testing.T) {
		inner := pg_queries.NewMockQuerier(t)
		inner.EXPECT().GetDomainsByTables(mock.Anything, mock.Anything, mock.Anything).
			Return(gone, nil).Times(sqlmanager_shared.CatalogReadAttempts)
		wrapped := &catalogRetryQuerier{inner: inner, retryOpts: fastRetryOptions}

		_, err := wrapped.GetDomainsByTables(context.Background(), nil, []string{"app.t"})

		require.ErrorIs(t, err, errDefinitionGone)
	})
}

func Test_retryOnCatalogChange(t *testing.T) {
	t.Run("reads again until the catalog holds still", func(t *testing.T) {
		reads := 0
		got, err := retryOnCatalogChange(context.Background(), fastRetryOptions, func() (string, error) {
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
		_, err := retryOnCatalogChange(context.Background(), fastRetryOptions, func() (string, error) {
			reads++
			return "", catalogChanged()
		})
		require.True(t, isCatalogChange(err))
		require.Equal(t, sqlmanager_shared.CatalogReadAttempts, reads)
	})

	t.Run("does not read again on another error", func(t *testing.T) {
		reads := 0
		broken := errors.New("connection refused")
		_, err := retryOnCatalogChange(context.Background(), fastRetryOptions, func() (string, error) {
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
		// The waits of the manager: a caller who gave up is not made to sit through them.
		_, err := retryOnCatalogChange(ctx, sqlmanager_shared.CatalogRetryOptions, func() (string, error) {
			reads++
			return "", catalogChanged()
		})
		require.Error(t, err)
		require.LessOrEqual(t, reads, 1)
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

			wrapped := &catalogRetryQuerier{inner: inner, retryOpts: fastRetryOptions}
			out := reflect.ValueOf(wrapped).MethodByName(method.Name).Call(args)

			require.Nil(t, out[1].Interface(), "the query was not read again")
		})
	}
}

// The manager reads the catalog through the querier that reads again, with its waits.
func Test_NewManager_ReadsTheCatalogAgain(t *testing.T) {
	inner := pg_queries.NewMockQuerier(t)
	inner.EXPECT().GetAllSchemas(mock.Anything, mock.Anything).Return(nil, catalogChanged()).Once()
	inner.EXPECT().GetAllSchemas(mock.Anything, mock.Anything).Return([]string{"public"}, nil).Once()

	schemas, err := NewManager(inner, nil, func() {}).querier.GetAllSchemas(context.Background(), nil)

	require.NoError(t, err)
	require.Equal(t, []string{"public"}, schemas)
}

// The waits are those of a migration that drops a few objects, not of an outage.
func Test_catalogRetryOptions_GivesUpInUnderASecond(t *testing.T) {
	reads := 0
	start := time.Now()
	_, err := retryOnCatalogChange(context.Background(), sqlmanager_shared.CatalogRetryOptions, func() (string, error) {
		reads++
		return "", catalogChanged()
	})
	require.True(t, isCatalogChange(err), "the error of the last read is the one returned: %v", err)
	require.Equal(t, sqlmanager_shared.CatalogReadAttempts, reads)
	require.Less(t, time.Since(start), time.Second)
}
