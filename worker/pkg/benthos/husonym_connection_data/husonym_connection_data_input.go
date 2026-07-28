package husonym_benthos_connectiondata

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/gob"
	"fmt"
	"log/slog"
	"sync"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	benthosbuilder_shared "github.com/fishtre-compagnie/husonym/internal/benthos/benthos-builder/shared"
	"github.com/fishtre-compagnie/husonym/internal/gotypeutil"
	husonym_types "github.com/fishtre-compagnie/husonym/internal/types"
	husonym_metadata "github.com/fishtre-compagnie/husonym/worker/pkg/benthos/metadata"

	husonymgob "github.com/fishtre-compagnie/husonym/internal/gob"
	husonymtypes "github.com/fishtre-compagnie/husonym/internal/husonym-types"
	"github.com/redpanda-data/benthos/v4/public/service"
)

func init() {
	husonymgob.RegisterGobTypes()
}

var husonymConnectionDataConfigSpec = service.NewConfigSpec().
	Summary("Streams Husonym connection data").
	Field(service.NewStringField("connection_id")).
	Field(service.NewStringField("connection_type")).
	Field(service.NewStringField("schema")).
	Field(service.NewStringField("table")).
	Field(service.NewStringField("job_id").Optional()).
	Field(service.NewStringField("job_run_id").Optional())

func newHusonymConnectionDataInput(
	conf *service.ParsedConfig,
	husonymConnectApi mgmtv1alpha1connect.ConnectionDataServiceClient,
	logger *slog.Logger,
) (service.Input, error) {
	connectionId, err := conf.FieldString("connection_id")
	if err != nil {
		return nil, err
	}

	connectionType, err := conf.FieldString("connection_type")
	if err != nil {
		return nil, err
	}

	schema, err := conf.FieldString("schema")
	if err != nil {
		return nil, err
	}
	table, err := conf.FieldString("table")
	if err != nil {
		return nil, err
	}

	var jobId *string
	if conf.Contains("job_id") {
		jobIdStr, err := conf.FieldString("job_id")
		if err != nil {
			return nil, err
		}
		jobId = &jobIdStr
	}
	var jobRunId *string
	if conf.Contains("job_run_id") {
		jobRunIdStr, err := conf.FieldString("job_run_id")
		if err != nil {
			return nil, err
		}
		jobRunId = &jobRunIdStr
	}

	registry := husonymtypes.NewTypeRegistry(logger)

	return service.AutoRetryNacks(&husonymInput{
		connectionId:   connectionId,
		connectionType: connectionType,
		schema:         schema,
		table:          table,
		connectionOpts: &connOpts{
			jobId:    jobId,
			jobRunId: jobRunId,
		},
		husonymConnectApi:   husonymConnectApi,
		husonymTypeRegistry: registry,
		logger:              logger,
	}), nil
}

func RegisterHusonymConnectionDataInput(
	env *service.Environment,
	husonymConnectApi mgmtv1alpha1connect.ConnectionDataServiceClient,
	logger *slog.Logger,
) error {
	return env.RegisterInput(
		"husonym_connection_data", husonymConnectionDataConfigSpec,
		func(conf *service.ParsedConfig, mgr *service.Resources) (service.Input, error) {
			return newHusonymConnectionDataInput(conf, husonymConnectApi, logger)
		},
	)
}

//------------------------------------------------------------------------------

type connOpts struct {
	jobId    *string
	jobRunId *string
}

type husonymInput struct {
	connectionId   string
	connectionType string
	connectionOpts *connOpts
	schema         string
	table          string

	logger              *slog.Logger
	husonymConnectApi   mgmtv1alpha1connect.ConnectionDataServiceClient
	husonymTypeRegistry *husonymtypes.TypeRegistry

	recvMut sync.Mutex

	resp *connect.ServerStreamForClient[mgmtv1alpha1.GetConnectionDataStreamResponse]
}

func (g *husonymInput) Connect(ctx context.Context) error {
	var streamCfg *mgmtv1alpha1.ConnectionStreamConfig

	if g.connectionType == string(benthosbuilder_shared.ConnectionTypeAwsS3) {
		awsS3Cfg := &mgmtv1alpha1.AwsS3StreamConfig{}
		if g.connectionOpts != nil {
			if g.connectionOpts.jobRunId != nil && *g.connectionOpts.jobRunId != "" {
				awsS3Cfg.Id = &mgmtv1alpha1.AwsS3StreamConfig_JobRunId{
					JobRunId: *g.connectionOpts.jobRunId,
				}
			} else if g.connectionOpts.jobId != nil && *g.connectionOpts.jobId != "" {
				awsS3Cfg.Id = &mgmtv1alpha1.AwsS3StreamConfig_JobId{JobId: *g.connectionOpts.jobId}
			}
		}
		streamCfg = &mgmtv1alpha1.ConnectionStreamConfig{
			Config: &mgmtv1alpha1.ConnectionStreamConfig_AwsS3Config{
				AwsS3Config: awsS3Cfg,
			},
		}
	} else if g.connectionType == string(benthosbuilder_shared.ConnectionTypeGCP) {
		if g.connectionOpts != nil {
			gcpCfg := &mgmtv1alpha1.GcpCloudStorageStreamConfig{}
			if g.connectionOpts != nil {
				if g.connectionOpts.jobRunId != nil && *g.connectionOpts.jobRunId != "" {
					gcpCfg.Id = &mgmtv1alpha1.GcpCloudStorageStreamConfig_JobRunId{JobRunId: *g.connectionOpts.jobRunId}
				} else if g.connectionOpts.jobId != nil && *g.connectionOpts.jobId != "" {
					gcpCfg.Id = &mgmtv1alpha1.GcpCloudStorageStreamConfig_JobId{JobId: *g.connectionOpts.jobId}
				}
			}
			streamCfg = &mgmtv1alpha1.ConnectionStreamConfig{
				Config: &mgmtv1alpha1.ConnectionStreamConfig_GcpCloudstorageConfig{
					GcpCloudstorageConfig: gcpCfg,
				},
			}
		}
	}

	resp, err := g.husonymConnectApi.GetConnectionDataStream(
		ctx,
		connect.NewRequest(&mgmtv1alpha1.GetConnectionDataStreamRequest{
			ConnectionId: g.connectionId,
			Schema:       g.schema,
			Table:        g.table,
			StreamConfig: streamCfg,
		}),
	)
	if err != nil {
		return err
	}
	g.resp = resp
	return nil
}

func (g *husonymInput) Read(ctx context.Context) (*service.Message, service.AckFunc, error) {
	g.recvMut.Lock()
	defer g.recvMut.Unlock()

	if g.husonymConnectApi == nil && g.resp == nil {
		return nil, nil, service.ErrNotConnected
	}
	if g.resp == nil {
		return nil, nil, service.ErrEndOfInput
	}

	ok := g.resp.Receive()
	if !ok {
		err := g.resp.Err()
		if err != nil {
			return nil, nil, err
		}
		return nil, nil, service.ErrEndOfInput
	}
	rowBytes := g.resp.Msg().RowBytes

	if g.connectionType == string(benthosbuilder_shared.ConnectionTypeDynamodb) {
		var dynamoDBItem map[string]any
		decoder := gob.NewDecoder(bytes.NewReader(rowBytes))
		err := decoder.Decode(&dynamoDBItem)
		if err != nil {
			return nil, nil, fmt.Errorf(
				"error decoding data connection stream response with gob decoder: %w",
				err,
			)
		}

		resMap, keyTypeMap := unmarshalDynamoDBItem(dynamoDBItem)
		msg := service.NewMessage(nil)
		msg.MetaSetMut(husonym_metadata.MetaTypeMapStr, keyTypeMap)
		msg.SetStructuredMut(resMap)
		return msg, func(ctx context.Context, err error) error {
			// Nacks are retried automatically when we use service.AutoRetryNacks
			return nil
		}, nil
	}

	valuesMap := map[string]any{}
	decoder := gob.NewDecoder(bytes.NewReader(rowBytes))
	err := decoder.Decode(&valuesMap)
	if err != nil {
		return nil, nil, fmt.Errorf(
			"error decoding data connection stream response with gob decoder: %w",
			err,
		)
	}
	msg := service.NewMessage(nil)
	msg.SetStructuredMut(valuesMap)
	return msg, func(ctx context.Context, err error) error {
		// Nacks are retried automatically when we use service.AutoRetryNacks
		return nil
	}, nil
}

func (g *husonymInput) Close(ctx context.Context) error {
	// close client
	// todo: prob need mutex
	if g.resp != nil {
		err := g.resp.Close()
		if err != nil {
			return err
		}
		g.resp = nil
	}

	g.husonymConnectApi = nil // idk if this really matters
	return nil
}

func unmarshalDynamoDBItem(
	item map[string]any,
) (standardMap map[string]any, keyTypeMap map[string]husonym_types.KeyType) {
	result := make(map[string]any)
	ktm := make(map[string]husonym_types.KeyType)
	for key, value := range item {
		result[key] = parseDynamoDBAttributeValue(key, value, ktm)
	}

	return result, ktm
}

func parseDynamoDBAttributeValue(
	key string,
	value any,
	keyTypeMap map[string]husonym_types.KeyType,
) any {
	if m, ok := value.(map[string]any); ok {
		for dynamoType, dynamoValue := range m {
			switch dynamoType {
			case "S":
				return dynamoValue.(string)
			case "B":
				switch v := dynamoValue.(type) {
				case string:
					byteSlice, err := base64.StdEncoding.DecodeString(v)
					if err != nil {
						return dynamoValue
					}
					return byteSlice
				case []byte:
					return v
				default:
					return dynamoValue
				}
			case "N":
				n, err := gotypeutil.ParseStringAsNumber(dynamoValue.(string))
				if err != nil {
					return dynamoValue
				}
				return n
			case "BOOL":
				return dynamoValue.(bool)
			case "NULL":
				return nil
			case "L":
				list := dynamoValue.([]any)
				result := make([]any, len(list))
				for i, item := range list {
					result[i] = parseDynamoDBAttributeValue(
						fmt.Sprintf("%s[%d]", key, i),
						item,
						keyTypeMap,
					)
				}
				return result
			case "M":
				mAny := map[string]any{}
				for k, v := range dynamoValue.(map[string]any) {
					path := k
					if key != "" {
						path = fmt.Sprintf("%s.%s", key, k)
					}
					val := parseDynamoDBAttributeValue(path, v, keyTypeMap)
					mAny[k] = val
				}
				return mAny
			case "BS":
				var result [][]byte
				switch bytes := dynamoValue.(type) {
				case []any:
					result = make([][]byte, len(bytes))
					for i, b := range bytes {
						s := b.(string)
						byteSlice, err := base64.StdEncoding.DecodeString(s)
						if err != nil {
							return dynamoValue
						}
						result[i] = byteSlice
					}
				case [][]byte:
					return bytes
				default:
					return dynamoValue
				}
				return result
			case "SS":
				keyTypeMap[key] = husonym_types.StringSet
				switch ss := dynamoValue.(type) {
				case []any:
					result := make([]string, len(ss))
					for i, s := range ss {
						result[i] = s.(string)
					}
					return result
				case []string:
					return ss
				default:
					return dynamoValue
				}
			case "NS":
				keyTypeMap[key] = husonym_types.NumberSet
				var result []any
				switch ns := dynamoValue.(type) {
				case []any:
					result = make([]any, len(ns))
					for i, num := range ns {
						n, err := gotypeutil.ParseStringAsNumber(num.(string))
						if err != nil {
							result[i] = num
						}
						result[i] = n
					}
				case []string:
					result = make([]any, len(ns))
					for i, num := range ns {
						n, err := gotypeutil.ParseStringAsNumber(num)
						if err != nil {
							result[i] = num
						}
						result[i] = n
					}
				default:
					return dynamoValue
				}
				return result
			}
		}
	}
	return value
}
