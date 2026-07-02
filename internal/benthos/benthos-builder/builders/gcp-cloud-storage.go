package benthosbuilder_builders

import (
	"context"
	"errors"
	"strings"

	bb_internal "github.com/fishtre-compagnie/husonym/internal/benthos/benthos-builder/internal"
	"github.com/fishtre-compagnie/husonym/internal/runconfigs"
	husonym_benthos "github.com/fishtre-compagnie/husonym/worker/pkg/benthos"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/shared"
)

type gcpCloudStorageSyncBuilder struct {
}

func NewGcpCloudStorageSyncBuilder() bb_internal.BenthosBuilder {
	return &gcpCloudStorageSyncBuilder{}
}

func (b *gcpCloudStorageSyncBuilder) BuildSourceConfigs(
	ctx context.Context,
	params *bb_internal.SourceParams,
) ([]*bb_internal.BenthosSourceConfig, error) {
	return nil, errors.ErrUnsupported
}

func (b *gcpCloudStorageSyncBuilder) BuildDestinationConfig(
	ctx context.Context,
	params *bb_internal.DestinationParams,
) (*bb_internal.BenthosDestinationConfig, error) {
	config := &bb_internal.BenthosDestinationConfig{}

	benthosConfig := params.SourceConfig
	if benthosConfig.RunType == runconfigs.RunTypeUpdate {
		return config, nil
	}
	destinationOpts := params.DestinationOpts.GetGcpCloudstorageOptions()
	gcpCloudStorageConfig := params.DestConnection.GetConnectionConfig().GetGcpCloudstorageConfig()

	if destinationOpts == nil {
		return nil, errors.New("destination must have configured GCP Cloud Storage options")
	}
	if gcpCloudStorageConfig == nil {
		return nil, errors.New("destination must have configured GCP Cloud Storage config")
	}

	pathpieces := []string{}
	if gcpCloudStorageConfig.GetPathPrefix() != "" {
		pathpieces = append(pathpieces, strings.Trim(gcpCloudStorageConfig.GetPathPrefix(), "/"))
	}

	pathpieces = append(
		pathpieces,
		"workflows",
		params.JobRunId,
		"activities",
		husonym_benthos.BuildBenthosTable(benthosConfig.TableSchema, benthosConfig.TableName),
		"data",
		`${!count("files")}.txt.gz`,
	)

	config.Outputs = append(config.Outputs, husonym_benthos.Outputs{
		Fallback: []husonym_benthos.Outputs{
			{
				GcpCloudStorage: &husonym_benthos.GcpCloudStorageOutput{
					Bucket:          gcpCloudStorageConfig.GetBucket(),
					MaxInFlight:     10,
					Path:            strings.Join(pathpieces, "/"),
					ContentType:     shared.Ptr("txt/plain"),
					ContentEncoding: shared.Ptr("gzip"),
					Batching: &husonym_benthos.Batching{
						Count:  100,
						Period: "5s",
						Processors: []*husonym_benthos.BatchProcessor{
							{Archive: &husonym_benthos.ArchiveProcessor{Format: "lines"}},
							{Compress: &husonym_benthos.CompressProcessor{Algorithm: "gzip"}},
						},
					},
				},
			},
			// kills activity depending on error
			{Error: &husonym_benthos.ErrorOutputConfig{
				ErrorMsg: `${! meta("fallback_error")}`,
				Batching: &husonym_benthos.Batching{
					Period: "5s",
					Count:  100,
				},
			}},
		},
	})

	return config, nil
}
