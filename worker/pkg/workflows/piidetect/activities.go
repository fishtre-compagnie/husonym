package piidetect

import (
	"context"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/connectiondata"
	husonymgob "github.com/fishtre-compagnie/husonym/internal/gob"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/model"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
)

const (
	// sampledRows is how many rows of a table are read when the job samples data.
	sampledRows = 200
	// samplingTimeout bounds the reading of those rows.
	samplingTimeout = 30 * time.Second
	// heartbeatEvery is how often the model activity and the content activity say that
	// they are alive. The workflow gives them minutes between two heartbeats: several
	// fit, so that one that is late does not end the attempt.
	heartbeatEvery = 30 * time.Second
)

// Config is what a deployment sets. It is read once, when the worker starts.
type Config struct {
	// TablesAtOnce is how many tables a run scans at once; defaultTablesAtOnce when it
	// is not positive.
	TablesAtOnce int
	Model        model.Config
}

// JobAPI is what the activities call on the job service of the API.
// mgmtv1alpha1connect.JobServiceClient is one.
type JobAPI interface {
	GetJob(
		context.Context,
		*connect.Request[mgmtv1alpha1.GetJobRequest],
	) (*connect.Response[mgmtv1alpha1.GetJobResponse], error)
	GetRunContext(
		context.Context,
		*connect.Request[mgmtv1alpha1.GetRunContextRequest],
	) (*connect.Response[mgmtv1alpha1.GetRunContextResponse], error)
	SetRunContext(
		context.Context,
		*connect.Request[mgmtv1alpha1.SetRunContextRequest],
	) (*connect.Response[mgmtv1alpha1.SetRunContextResponse], error)
}

// ConnectionAPI is what the activities call on the connection service of the API.
// mgmtv1alpha1connect.ConnectionServiceClient is one.
type ConnectionAPI interface {
	GetConnection(
		context.Context,
		*connect.Request[mgmtv1alpha1.GetConnectionRequest],
	) (*connect.Response[mgmtv1alpha1.GetConnectionResponse], error)
}

// ContentAPI is what the activities call on the connection data service of the API.
// mgmtv1alpha1connect.ConnectionDataServiceClient is one.
type ContentAPI interface {
	DetectPiiInConnectionData(
		context.Context,
		*connect.Request[mgmtv1alpha1.DetectPiiInConnectionDataRequest],
	) (*connect.Response[mgmtv1alpha1.DetectPiiInConnectionDataResponse], error)
}

// Activities are the nine activities of the package: eight that the two workflows run,
// and DetectPiiContent.
type Activities struct {
	jobs        JobAPI
	connections ConnectionAPI
	content     ContentAPI
	data        connectiondata.ConnectionDataBuilder
	schedules   client.ScheduleClient
	// classifier asks the model; nil when no model is configured.
	classifier *model.Classifier

	tablesAtOnce    int
	samplingTimeout time.Duration
	// The model activity and the content activity say that they are alive every
	// heartbeatEvery, through heartbeat.
	heartbeatEvery time.Duration
	heartbeat      func(ctx context.Context, details ...any)
}

func NewActivities(
	jobs JobAPI,
	connections ConnectionAPI,
	content ContentAPI,
	data connectiondata.ConnectionDataBuilder,
	schedules client.ScheduleClient,
	classifier *model.Classifier,
	cfg *Config,
) *Activities {
	// The sampled rows arrive encoded, and decoding them needs the types of their values
	// to be registered. The activities ask for it themselves rather than count on
	// another package of the worker having done so.
	husonymgob.RegisterGobTypes()

	return &Activities{
		jobs:            jobs,
		connections:     connections,
		content:         content,
		data:            data,
		schedules:       schedules,
		classifier:      classifier,
		tablesAtOnce:    tablesAtOnceOr(cfg.TablesAtOnce, defaultTablesAtOnce),
		samplingTimeout: samplingTimeout,
		heartbeatEvery:  heartbeatEvery,
		heartbeat:       activity.RecordHeartbeat,
	}
}

// modelName is the name of the configured model, empty when there is none.
func (a *Activities) modelName() string {
	if a.classifier == nil {
		return ""
	}
	return a.classifier.Model()
}

// connection reads a connection from the API.
func (a *Activities) connection(
	ctx context.Context,
	connectionId string,
) (*mgmtv1alpha1.Connection, error) {
	resp, err := a.connections.GetConnection(
		ctx,
		connect.NewRequest(&mgmtv1alpha1.GetConnectionRequest{Id: connectionId}),
	)
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetConnection(), nil
}
