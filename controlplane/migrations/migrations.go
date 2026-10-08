// Package migrations holds the schema of the control plane database and applies it. The SQL is
// embedded, so the binary carries its own migrations.
package migrations

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed sql/*.sql
var files embed.FS

// bookkeepingTable is where golang-migrate records the version applied, named with its schema.
// Left to itself, the driver looks the table up in the current schema of the connection, and that
// schema changes under it: the first migration creates the schema controlplane, which a role of
// the same name then has first in its search path. The next start would find no table there,
// take the database for an empty one and run the first migration again.
const bookkeepingTable = `"public"."schema_migrations"`

// Up applies the pending migrations. Nothing to apply is not an error.
func Up(_ context.Context, databaseURL string, logger *slog.Logger) error {
	target, err := url.Parse(databaseURL)
	if err != nil {
		// The URL carries the password: its error text is not passed on.
		return errors.New("open the migrations: the address of the database cannot be parsed")
	}
	query := target.Query()
	query.Set("x-migrations-table", bookkeepingTable)
	query.Set("x-migrations-table-quoted", "1")
	target.RawQuery = query.Encode()

	source, err := iofs.New(files, "sql")
	if err != nil {
		return fmt.Errorf("open the embedded migrations: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", source, target.String())
	if err != nil {
		_ = source.Close()
		// The URL carries the password: its error text is not passed on.
		return errors.New("open the migrations: could not connect to the database")
	}
	m.Log = &migrateLogger{logger: logger}

	upErr := m.Up()
	sourceErr, databaseErr := m.Close()
	if upErr != nil && !errors.Is(upErr, migrate.ErrNoChange) {
		return fmt.Errorf("apply the migrations: %w", upErr)
	}
	if err := errors.Join(sourceErr, databaseErr); err != nil {
		return fmt.Errorf("close the migrations: %w", err)
	}
	return nil
}

type migrateLogger struct {
	logger *slog.Logger
}

func (l *migrateLogger) Verbose() bool { return false }

func (l *migrateLogger) Printf(format string, v ...any) {
	l.logger.Info(fmt.Sprintf("migrate: "+format, v...))
}
