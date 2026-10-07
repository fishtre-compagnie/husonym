// Package migrations holds the schema of the control plane database and applies it. The SQL is
// embedded, so the binary carries its own migrations.
package migrations

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log/slog"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed sql/*.sql
var files embed.FS

// Up applies the pending migrations. Nothing to apply is not an error.
func Up(_ context.Context, databaseURL string, logger *slog.Logger) error {
	source, err := iofs.New(files, "sql")
	if err != nil {
		return fmt.Errorf("open the embedded migrations: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", source, databaseURL)
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
