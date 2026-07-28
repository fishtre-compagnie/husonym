package benthos_environment

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	continuation_token "github.com/fishtre-compagnie/husonym/internal/continuation-token"
	husonym_benthos_defaulttransform "github.com/fishtre-compagnie/husonym/worker/pkg/benthos/default_transform"
	husonym_benthos_dynamodb "github.com/fishtre-compagnie/husonym/worker/pkg/benthos/dynamodb"
	husonym_benthos_error "github.com/fishtre-compagnie/husonym/worker/pkg/benthos/error"
	javascript_processor "github.com/fishtre-compagnie/husonym/worker/pkg/benthos/javascript"
	husonym_benthos_json "github.com/fishtre-compagnie/husonym/worker/pkg/benthos/json"
	benthos_metrics "github.com/fishtre-compagnie/husonym/worker/pkg/benthos/metrics"
	husonym_benthos_mongodb "github.com/fishtre-compagnie/husonym/worker/pkg/benthos/mongodb"
	husonym_benthos_connectiondata "github.com/fishtre-compagnie/husonym/worker/pkg/benthos/husonym_connection_data"
	openaigenerate "github.com/fishtre-compagnie/husonym/worker/pkg/benthos/openai_generate"
	benthos_redis "github.com/fishtre-compagnie/husonym/worker/pkg/benthos/redis"
	husonym_benthos_sql "github.com/fishtre-compagnie/husonym/worker/pkg/benthos/sql"
	"github.com/fishtre-compagnie/husonym/worker/pkg/benthos/transformers"
	"github.com/redis/go-redis/v9"
	"github.com/redpanda-data/benthos/v4/public/bloblang"
	"github.com/redpanda-data/benthos/v4/public/service"
	"go.opentelemetry.io/otel/metric"
)

type RegisterConfig struct {
	meter metric.Meter // nil to disable

	sqlConfig *SqlConfig // nil to disable

	mongoConfig *MongoConfig // nil to disable

	connectionDataConfig *ConnectionDataConfig // nil to diable

	stopChannel chan<- error

	blobEnv *bloblang.Environment

	transformPiiTextApi transformers.TransformPiiTextApi

	redisConfig *RedisConfig // nil to disable
}

type Option func(cfg *RegisterConfig)

func WithMeter(meter metric.Meter) Option {
	return func(cfg *RegisterConfig) {
		cfg.meter = meter
	}
}

func WithSqlConfig(sqlcfg *SqlConfig) Option {
	return func(cfg *RegisterConfig) {
		cfg.sqlConfig = sqlcfg
	}
}
func WithStopChannel(c chan<- error) Option {
	return func(cfg *RegisterConfig) {
		cfg.stopChannel = c
	}
}
func WithMongoConfig(mongocfg *MongoConfig) Option {
	return func(cfg *RegisterConfig) {
		cfg.mongoConfig = mongocfg
	}
}
func WithRedisConfig(redisConfig *RedisConfig) Option {
	return func(cfg *RegisterConfig) {
		cfg.redisConfig = redisConfig
	}
}
func WithConnectionDataConfig(connectionDataCfg *ConnectionDataConfig) Option {
	return func(cfg *RegisterConfig) {
		cfg.connectionDataConfig = connectionDataCfg
	}
}
func WithBlobEnv(b *bloblang.Environment) Option {
	return func(cfg *RegisterConfig) {
		cfg.blobEnv = b
	}
}
func WithTransformPiiTextApi(transformPiiTextApi transformers.TransformPiiTextApi) Option {
	return func(cfg *RegisterConfig) {
		cfg.transformPiiTextApi = transformPiiTextApi
	}
}

type SqlConfig struct {
	Provider               husonym_benthos_sql.ConnectionProvider
	IsRetry                bool
	InputHasMorePages      husonym_benthos_sql.OnHasMorePagesFn
	InputContinuationToken *continuation_token.ContinuationToken
}

type MongoConfig struct {
	Provider husonym_benthos_mongodb.MongoPoolProvider
}

type RedisConfig struct {
	Client redis.UniversalClient
}

type ConnectionDataConfig struct {
	HusonymConnectionDataApi mgmtv1alpha1connect.ConnectionDataServiceClient
}

func NewEnvironment(logger *slog.Logger, opts ...Option) (*service.Environment, error) {
	return NewWithEnvironment(service.NewEnvironment(), logger, opts...)
}

func NewWithEnvironment(
	env *service.Environment,
	logger *slog.Logger,
	opts ...Option,
) (*service.Environment, error) {
	if env == nil {
		env = service.NewEnvironment()
	}
	config := &RegisterConfig{}

	for _, opt := range opts {
		opt(config)
	}

	if config.stopChannel == nil {
		return nil, errors.New("must provide non-nil StopChannel")
	}

	if config.meter != nil {
		err := benthos_metrics.RegisterOtelMetricsExporter(env, config.meter)
		if err != nil {
			return nil, fmt.Errorf(
				"unable to register otel_collector for benthos metering: %w",
				err,
			)
		}
	}

	if config.sqlConfig != nil {
		err := husonym_benthos_sql.RegisterPooledSqlInsertOutput(
			env,
			config.sqlConfig.Provider,
			config.sqlConfig.IsRetry,
			logger,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"unable to register pooled_sql_insert output to benthos instance: %w",
				err,
			)
		}
		err = husonym_benthos_sql.RegisterPooledSqlUpdateOutput(env, config.sqlConfig.Provider)
		if err != nil {
			return nil, fmt.Errorf(
				"unable to register pooled_sql_update output to benthos instance: %w",
				err,
			)
		}
		err = husonym_benthos_sql.RegisterPooledSqlRawInput(
			env,
			config.sqlConfig.Provider,
			config.stopChannel,
			config.sqlConfig.InputHasMorePages,
			config.sqlConfig.InputContinuationToken,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"unable to register pooled_sql_raw input to benthos instance: %w",
				err,
			)
		}
	}

	if config.mongoConfig != nil {
		err := husonym_benthos_mongodb.RegisterPooledMongoDbInput(env, config.mongoConfig.Provider)
		if err != nil {
			return nil, fmt.Errorf(
				"unable to register pooled_mongodb input to benthos instance: %w",
				err,
			)
		}
		err = husonym_benthos_mongodb.RegisterPooledMongoDbOutput(env, config.mongoConfig.Provider)
		if err != nil {
			return nil, fmt.Errorf(
				"unable to register pooled_mongodb output to benthos instance: %w",
				err,
			)
		}
	}

	if config.redisConfig != nil {
		err := benthos_redis.RegisterRedisHashOutput(env, config.redisConfig.Client)
		if err != nil {
			return nil, fmt.Errorf(
				"unable to register redis_hash output to benthos instance: %w",
				err,
			)
		}

		err = benthos_redis.RegisterRedisProcessor(env, config.redisConfig.Client)
		if err != nil {
			return nil, fmt.Errorf(
				"unable to register redis processor to benthos instance: %w",
				err,
			)
		}
	}

	if config.connectionDataConfig != nil {
		err := husonym_benthos_connectiondata.RegisterHusonymConnectionDataInput(
			env,
			config.connectionDataConfig.HusonymConnectionDataApi,
			logger,
		)
		if err != nil {
			return nil, fmt.Errorf("unable to register husonym_connection_data input: %w", err)
		}
	}

	err := openaigenerate.RegisterOpenaiGenerate(env)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to register openai_generate input to benthos instance: %w",
			err,
		)
	}

	err = husonym_benthos_error.RegisterErrorProcessor(env, config.stopChannel)
	if err != nil {
		return nil, fmt.Errorf("unable to register error processor to benthos instance: %w", err)
	}

	err = husonym_benthos_error.RegisterErrorOutput(env, config.stopChannel)
	if err != nil {
		return nil, fmt.Errorf("unable to register error output to benthos instance: %w", err)
	}

	err = husonym_benthos_dynamodb.RegisterDynamoDbInput(env)
	if err != nil {
		return nil, fmt.Errorf("unable to register dynamodb input to benthos instance: %w", err)
	}

	err = husonym_benthos_dynamodb.RegisterDynamoDbOutput(env)
	if err != nil {
		return nil, fmt.Errorf("unable to register dynamodb output to benthos instance: %w", err)
	}

	err = husonym_benthos_defaulttransform.ReisterDefaultTransformerProcessor(env)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to register default mapping processor to benthos instance: %w",
			err,
		)
	}

	err = husonym_benthos_json.RegisterHusonymToJsonProcessor(env)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to register Husonym to JSON processor to benthos instance: %w",
			err,
		)
	}

	err = husonym_benthos_sql.RegisterHusonymToPgxProcessor(env)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to register Husonym to PGX processor to benthos instance: %w",
			err,
		)
	}

	err = husonym_benthos_sql.RegisterHusonymToMysqlProcessor(env)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to register Husonym to MYSQL processor to benthos instance: %w",
			err,
		)
	}

	err = husonym_benthos_sql.RegisterHusonymToMssqlProcessor(env)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to register Husonym to MSSQL processor to benthos instance: %w",
			err,
		)
	}

	err = javascript_processor.RegisterHusonymJavascriptProcessor(env, config.transformPiiTextApi)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to register javascript processor to benthos instance: %w",
			err,
		)
	}

	if config.blobEnv != nil {
		env.UseBloblangEnvironment(config.blobEnv)
	}

	return env, nil
}
