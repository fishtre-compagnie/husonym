// Package v1alpha1_usageservice receives what the worker tells about the runs it executes, and
// keeps it in the usage counters of the instance.
//
// Only the worker calls it. A failure to count never fails a run: the worker logs it and goes on.
package v1alpha1_usageservice

import (
	"context"
	"time"

	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
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
}

type Config struct {
	// WorkerOnly guards what only the worker calls: the start and the end of a run.
	WorkerOnly userdata.WorkerOnly
}

var _ mgmtv1alpha1connect.UsageServiceHandler = (*Service)(nil)

func New(
	cfg *Config,
	db *husonymdb.HusonymDb,
	userdataclient userdata.Interface,
	store *usagestore.Store,
) *Service {
	return newService(cfg, db, userdataclient, store)
}

func newService(
	cfg *Config,
	db *husonymdb.HusonymDb,
	userdataclient userdata.Interface,
	store runStore,
) *Service {
	return &Service{cfg: cfg, db: db, userdataclient: userdataclient, store: store}
}
