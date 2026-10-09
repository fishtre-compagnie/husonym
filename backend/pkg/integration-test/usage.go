package integrationtests_test

import (
	"context"
	"time"

	"github.com/fishtre-compagnie/husonym/backend/internal/usagereport"
	"github.com/fishtre-compagnie/husonym/backend/internal/usagestore"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
)

// UsageErrorsOfDay gives the errors of a UTC day as the usage report of that day counts them:
// read by the store of the API from its database, and turned into the rows of the report by
// the report's own function. The tests outside of the API cannot reach either.
func (s *HusonymApiTestClient) UsageErrorsOfDay(ctx context.Context, day time.Time) ([]telemetry.ErrorCount, error) {
	store := usagestore.New(husonymdb.New(s.Pgcontainer.DB, s.HusonymQuerier))
	rows, err := store.ErrorsOfDay(ctx, day)
	if err != nil {
		return nil, err
	}
	return usagereport.ErrorCounts(rows), nil
}
