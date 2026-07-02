package benthosbuilder_builders

import (
	"context"
	"errors"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	bb_internal "github.com/fishtre-compagnie/husonym/internal/benthos/benthos-builder/internal"
	bb_shared "github.com/fishtre-compagnie/husonym/internal/benthos/benthos-builder/shared"
	"github.com/fishtre-compagnie/husonym/internal/runconfigs"
	husonym_benthos "github.com/fishtre-compagnie/husonym/worker/pkg/benthos"
)

type husonymConnectionDataBuilder struct {
	connectiondataclient  mgmtv1alpha1connect.ConnectionDataServiceClient
	sqlmanagerclient      sqlmanager.SqlManagerClient
	sourceJobRunId        *string
	syncConfigs           []*runconfigs.RunConfig
	destinationConnection *mgmtv1alpha1.Connection
	sourceConnectionType  bb_shared.ConnectionType
}

func NewHusonymConnectionDataSyncBuilder(
	connectiondataclient mgmtv1alpha1connect.ConnectionDataServiceClient,
	sqlmanagerclient sqlmanager.SqlManagerClient,
	sourceJobRunId *string,
	syncConfigs []*runconfigs.RunConfig,
	destinationConnection *mgmtv1alpha1.Connection,
	sourceConnectionType bb_shared.ConnectionType,
) bb_internal.BenthosBuilder {
	return &husonymConnectionDataBuilder{
		connectiondataclient:  connectiondataclient,
		sqlmanagerclient:      sqlmanagerclient,
		sourceJobRunId:        sourceJobRunId,
		syncConfigs:           syncConfigs,
		destinationConnection: destinationConnection,
		sourceConnectionType:  sourceConnectionType,
	}
}

func (b *husonymConnectionDataBuilder) BuildSourceConfigs(
	ctx context.Context,
	params *bb_internal.SourceParams,
) ([]*bb_internal.BenthosSourceConfig, error) {
	sourceConnection := params.SourceConnection
	job := params.Job
	configs := []*bb_internal.BenthosSourceConfig{}

	for _, config := range b.syncConfigs {
		schema, table := sqlmanager_shared.SplitTableKey(config.Table())

		bc := &husonym_benthos.BenthosConfig{
			StreamConfig: husonym_benthos.StreamConfig{
				Logger: &husonym_benthos.LoggerConfig{
					Level:        "ERROR",
					AddTimestamp: true,
				},
				Input: &husonym_benthos.InputConfig{
					Inputs: husonym_benthos.Inputs{
						HusonymConnectionData: &husonym_benthos.HusonymConnectionData{
							ConnectionId:   sourceConnection.GetId(),
							ConnectionType: string(b.sourceConnectionType),
							JobId:          &job.Id,
							JobRunId:       b.sourceJobRunId,
							Schema:         schema,
							Table:          table,
						},
					},
				},
				Pipeline: &husonym_benthos.PipelineConfig{},
				Output: &husonym_benthos.OutputConfig{
					Outputs: husonym_benthos.Outputs{
						Broker: &husonym_benthos.OutputBrokerConfig{
							Pattern: "fan_out",
							Outputs: []husonym_benthos.Outputs{},
						},
					},
				},
			},
		}
		configs = append(configs, &bb_internal.BenthosSourceConfig{
			Name:      config.Id(),
			Config:    bc,
			DependsOn: config.DependsOn(),
			RunType:   config.RunType(),

			BenthosDsns: []*bb_shared.BenthosDsn{{ConnectionId: sourceConnection.Id}},

			TableSchema: schema,
			TableName:   table,
			Columns:     config.InsertColumns(),
			PrimaryKeys: config.PrimaryKeys(),
		})
	}

	return configs, nil
}

func (b *husonymConnectionDataBuilder) BuildDestinationConfig(
	ctx context.Context,
	params *bb_internal.DestinationParams,
) (*bb_internal.BenthosDestinationConfig, error) {
	return nil, errors.ErrUnsupported
}
