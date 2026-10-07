// Package v1alpha1_usageservice receives what the worker tells about the runs it executes, and
// keeps it in the usage counters of the instance. It also tells the interface and the CLI how the
// usage report of the instance is sent, gives the reports it keeps and makes the one for a period.
//
// Only the worker calls the first two procedures. A failure to count never fails a run: the worker logs it and goes on.
package v1alpha1_usageservice

import (
	"context"
	"time"

	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/backend/internal/usagereport"
	"github.com/fishtre-compagnie/husonym/backend/internal/usagestore"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
)

// runStore is what the service keeps a run in: usagestore.Store.
type runStore interface {
	RunStarted(ctx context.Context, run usagestore.RunStart) error
	RunEnded(ctx context.Context, run usagestore.RunEnd) error
	CloseRun(
		ctx context.Context, runId string, status usagestore.Status, endedAt time.Time,
		rowsRead, rowsDiscarded, retries, tablesUncounted int64, sourceVersionMajor string,
	) error
}

type Service struct {
	cfg            *Config
	db             *husonymdb.HusonymDb
	userdataclient userdata.Interface
	store          runStore
	reports        reportStore
	key            usagereport.KeyModeSource
	periods        periodBuilder
	now            func() time.Time
}

type Config struct {
	// WorkerOnly guards what only the worker calls: the start and the end of a run.
	WorkerOnly userdata.WorkerOnly
	// ModeSetting is HUSONYM_TELEMETRY as it is written: it may only lower the mode the key provides.
	ModeSetting string
	// Diagnostics is whether the part of the usage report that describes the instance is on.
	Diagnostics bool
}

var _ mgmtv1alpha1connect.UsageServiceHandler = (*Service)(nil)

func New(
	cfg *Config,
	db *husonymdb.HusonymDb,
	userdataclient userdata.Interface,
	store *usagestore.Store,
	key usagereport.KeyModeSource,
	builder *usagereport.Builder,
) *Service {
	return newService(cfg, db, userdataclient, store, store, key, builder, time.Now)
}

func newService(
	cfg *Config,
	db *husonymdb.HusonymDb,
	userdataclient userdata.Interface,
	store runStore,
	reports reportStore,
	key usagereport.KeyModeSource,
	periods periodBuilder,
	now func() time.Time,
) *Service {
	return &Service{
		cfg: cfg, db: db, userdataclient: userdataclient, store: store, reports: reports, key: key, periods: periods, now: now,
	}
}
