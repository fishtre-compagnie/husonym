package usagereport

import (
	"context"
	"fmt"
	"log/slog"

	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/backend/internal/licensegate"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
)

// DefaultTemporal is what is asked of the orchestrator the deployment runs on by default.
// *clientmanager.ClientManager is one.
type DefaultTemporal interface {
	CountDefaultQueueWorkers(ctx context.Context, logger *slog.Logger) (int, error)
	DefaultServerVersion(ctx context.Context, logger *slog.Logger) (string, error)
}

// InstanceReader reads what the report says of the instance and that no store keeps: the
// sources it counts, and what it runs on.
type InstanceReader struct {
	db       *husonymdb.HusonymDb
	temporal DefaultTemporal
}

func NewInstanceReader(db *husonymdb.HusonymDb, temporal DefaultTemporal) *InstanceReader {
	return &InstanceReader{db: db, temporal: temporal}
}

// SourcesCount is how many sources the instance counts, as the license counts them.
func (r *InstanceReader) SourcesCount(ctx context.Context) (int, error) {
	jobs, err := r.db.Q.ListJobSourcesOfInstance(ctx, r.db.Db)
	if err != nil {
		return 0, fmt.Errorf("unable to list the sources of the instance: %w", err)
	}
	return len(licensegate.SourcesOf(jobs)), nil
}

// PostgresMajor is the major version of the database of the API itself.
func (r *InstanceReader) PostgresMajor(ctx context.Context) (int, error) {
	var number int
	err := r.db.Db.QueryRow(ctx, "SELECT current_setting('server_version_num')::int").Scan(&number)
	if err != nil {
		return 0, fmt.Errorf("unable to read the version of the database: %w", err)
	}
	return number / 10000, nil
}

// TemporalVersion is the version of the default orchestrator, as its server spells it.
func (r *InstanceReader) TemporalVersion(ctx context.Context) (string, error) {
	return r.temporal.DefaultServerVersion(ctx, logger_interceptor.GetLoggerFromContextOrDefault(ctx))
}

// Workers is how many workers serve the default queue.
func (r *InstanceReader) Workers(ctx context.Context) (int, error) {
	return r.temporal.CountDefaultQueueWorkers(ctx, logger_interceptor.GetLoggerFromContextOrDefault(ctx))
}
