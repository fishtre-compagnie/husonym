package benthosbuilder_builders

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	bb_internal "github.com/fishtre-compagnie/husonym/internal/benthos/benthos-builder/internal"
	"github.com/fishtre-compagnie/husonym/internal/runconfigs"
	husonym_benthos "github.com/fishtre-compagnie/husonym/worker/pkg/benthos"
)

type awsS3SyncBuilder struct {
}

func NewAwsS3SyncBuilder() bb_internal.BenthosBuilder {
	return &awsS3SyncBuilder{}
}

func (b *awsS3SyncBuilder) BuildSourceConfigs(
	ctx context.Context,
	params *bb_internal.SourceParams,
) ([]*bb_internal.BenthosSourceConfig, error) {
	return nil, errors.ErrUnsupported
}

func (b *awsS3SyncBuilder) BuildDestinationConfig(
	ctx context.Context,
	params *bb_internal.DestinationParams,
) (*bb_internal.BenthosDestinationConfig, error) {
	config := &bb_internal.BenthosDestinationConfig{}

	benthosConfig := params.SourceConfig
	if benthosConfig.RunType == runconfigs.RunTypeUpdate {
		return config, nil
	}
	destinationOpts := params.DestinationOpts.GetAwsS3Options()
	connAwsS3Config := params.DestConnection.GetConnectionConfig().GetAwsS3Config()

	if destinationOpts == nil {
		return nil, errors.New("destination must have configured AWS S3 options")
	}
	if connAwsS3Config == nil {
		return nil, errors.New("destination must have configured AWS S3 config")
	}

	s3pathpieces := []string{}
	if connAwsS3Config.PathPrefix != nil && *connAwsS3Config.PathPrefix != "" {
		s3pathpieces = append(s3pathpieces, strings.Trim(*connAwsS3Config.PathPrefix, "/"))
	}

	s3pathpieces = append(
		s3pathpieces,
		"workflows",
		params.JobRunId,
		"activities",
		husonym_benthos.BuildBenthosTable(benthosConfig.TableSchema, benthosConfig.TableName),
		"data",
		`records-${!count("files")}-${!timestamp_unix_nano()}.jsonl.gz`,
	)

	batchingConfig, err := getParsedBatchingConfig(destinationOpts)
	if err != nil {
		return nil, err
	}

	timeout := ""
	if destinationOpts.GetTimeout() != "" {
		_, err := time.ParseDuration(destinationOpts.GetTimeout())
		if err != nil {
			return nil, fmt.Errorf("unable to parse timeout for s3 destination config: %w", err)
		}
		timeout = destinationOpts.GetTimeout()
	}

	storageClass := ""
	if destinationOpts.GetStorageClass() != mgmtv1alpha1.AwsS3DestinationConnectionOptions_STORAGE_CLASS_UNSPECIFIED {
		storageClass = convertToS3StorageClass(destinationOpts.GetStorageClass()).String()
	}

	config.Outputs = append(config.Outputs, husonym_benthos.Outputs{
		Fallback: []husonym_benthos.Outputs{
			{
				AwsS3: &husonym_benthos.AwsS3Insert{
					Bucket:       connAwsS3Config.Bucket,
					MaxInFlight:  int(batchingConfig.MaxInFlight),
					Timeout:      timeout,
					StorageClass: storageClass,
					Path:         strings.Join(s3pathpieces, "/"),
					ContentType:  "application/gzip",
					Batching: &husonym_benthos.Batching{
						Count:  batchingConfig.BatchCount,
						Period: batchingConfig.BatchPeriod,
						Processors: []*husonym_benthos.BatchProcessor{
							{HusonymToJson: &husonym_benthos.HusonymToJsonConfig{}},
							{Archive: &husonym_benthos.ArchiveProcessor{Format: "lines"}},
							{Compress: &husonym_benthos.CompressProcessor{Algorithm: "gzip"}},
						},
					},
					Credentials: buildBenthosS3Credentials(connAwsS3Config.Credentials),
					Region:      connAwsS3Config.GetRegion(),
					Endpoint:    connAwsS3Config.GetEndpoint(),
				},
			},
			// kills activity depending on error
			{Error: &husonym_benthos.ErrorOutputConfig{
				ErrorMsg: `${! meta("fallback_error")}`,
				Batching: &husonym_benthos.Batching{
					Period: batchingConfig.BatchPeriod,
					Count:  batchingConfig.BatchCount,
				},
			}},
		},
	})

	return config, nil
}

type S3StorageClass int

const (
	S3StorageClass_UNSPECIFIED S3StorageClass = iota
	S3StorageClass_STANDARD
	S3StorageClass_REDUCED_REDUNDANCY
	S3StorageClass_GLACIER
	S3StorageClass_STANDARD_IA
	S3StorageClass_ONEZONE_IA
	S3StorageClass_INTELLIGENT_TIERING
	S3StorageClass_DEEP_ARCHIVE
)

func (s S3StorageClass) String() string {
	return [...]string{
		"STORAGE_CLASS_UNSPECIFIED",
		"STANDARD",
		"REDUCED_REDUNDANCY",
		"GLACIER",
		"STANDARD_IA",
		"ONEZONE_IA",
		"INTELLIGENT_TIERING",
		"DEEP_ARCHIVE",
	}[s]
}

func convertToS3StorageClass(
	protoStorageClass mgmtv1alpha1.AwsS3DestinationConnectionOptions_StorageClass,
) S3StorageClass {
	switch protoStorageClass {
	case mgmtv1alpha1.AwsS3DestinationConnectionOptions_STORAGE_CLASS_STANDARD:
		return S3StorageClass_STANDARD
	case mgmtv1alpha1.AwsS3DestinationConnectionOptions_STORAGE_CLASS_REDUCED_REDUNDANCY:
		return S3StorageClass_REDUCED_REDUNDANCY
	case mgmtv1alpha1.AwsS3DestinationConnectionOptions_STORAGE_CLASS_GLACIER:
		return S3StorageClass_GLACIER
	case mgmtv1alpha1.AwsS3DestinationConnectionOptions_STORAGE_CLASS_STANDARD_IA:
		return S3StorageClass_STANDARD_IA
	case mgmtv1alpha1.AwsS3DestinationConnectionOptions_STORAGE_CLASS_ONEZONE_IA:
		return S3StorageClass_ONEZONE_IA
	case mgmtv1alpha1.AwsS3DestinationConnectionOptions_STORAGE_CLASS_INTELLIGENT_TIERING:
		return S3StorageClass_INTELLIGENT_TIERING
	case mgmtv1alpha1.AwsS3DestinationConnectionOptions_STORAGE_CLASS_DEEP_ARCHIVE:
		return S3StorageClass_DEEP_ARCHIVE
	default:
		return S3StorageClass_UNSPECIFIED
	}
}
