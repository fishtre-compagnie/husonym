package husonym_benthos_awss3

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/fishtre-compagnie/husonym/internal/cloudidentity"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	awsmanager "github.com/fishtre-compagnie/husonym/internal/aws"
	connectionmanager "github.com/fishtre-compagnie/husonym/internal/connection-manager"
	"github.com/redpanda-data/benthos/v4/public/service"
)

const (
	fieldConnectionId = "connection_id"
	fieldPath         = "path"
	fieldContentType  = "content_type"
	fieldStorageClass = "storage_class"
	fieldTimeout      = "timeout"
	fieldBatching     = "batching"
)

func outputSpec() *service.ConfigSpec {
	return service.NewConfigSpec().
		Categories("Services", "AWS").
		Summary("Writes each message as an object of the bucket of an AWS S3 connection.").
		Fields(
			service.NewStringField(fieldConnectionId).
				Description("The Husonym connection whose bucket, region, endpoint and credentials are used."),
			service.NewInterpolatedStringField(fieldPath).
				Description("The key of each object."),
			service.NewStringField(fieldContentType).
				Default("application/octet-stream"),
			service.NewStringField(fieldStorageClass).
				Description("The storage class of each object; S3 stores it as STANDARD when empty.").
				Default(""),
			service.NewDurationField(fieldTimeout).
				Description("The maximum time an upload may take.").
				Default("5s"),
			service.NewOutputMaxInFlightField(),
			service.NewBatchPolicyField(fieldBatching),
		)
}

// RegisterAwsS3Output registers husonym_aws_s3, which resolves its connection when the stream
// runs: the bucket, region, endpoint and credentials live in the connection only, so that the
// stored config of a run holds no secret.
func RegisterAwsS3Output(
	env *service.Environment,
	getConnection func(connectionId string) (connectionmanager.ConnectionInput, error),
	identity cloudidentity.Policy,
) error {
	return env.RegisterBatchOutput(
		"husonym_aws_s3",
		outputSpec(),
		func(conf *service.ParsedConfig, mgr *service.Resources) (out service.BatchOutput, batchPolicy service.BatchPolicy, maxInFlight int, err error) {
			if maxInFlight, err = conf.FieldMaxInFlight(); err != nil {
				return
			}
			if batchPolicy, err = conf.FieldBatchPolicy(fieldBatching); err != nil {
				return
			}
			out, err = newS3Writer(conf, getConnection, identity)
			return
		},
	)
}

type putObjectAPI interface {
	PutObject(
		ctx context.Context,
		params *s3.PutObjectInput,
		optFns ...func(*s3.Options),
	) (*s3.PutObjectOutput, error)
}

type s3Writer struct {
	awsConfig    *aws.Config
	endpoint     string
	bucket       string
	path         *service.InterpolatedString
	contentType  string
	storageClass types.StorageClass
	timeout      time.Duration

	mu     sync.RWMutex
	client putObjectAPI // set by Connect
}

func newS3Writer(
	conf *service.ParsedConfig,
	getConnection func(connectionId string) (connectionmanager.ConnectionInput, error),
	identity cloudidentity.Policy,
) (*s3Writer, error) {
	connectionId, err := conf.FieldString(fieldConnectionId)
	if err != nil {
		return nil, err
	}
	connection, err := getConnection(connectionId)
	if err != nil {
		return nil, err
	}
	s3Config := connection.GetConnectionConfig().GetAwsS3Config()
	if s3Config == nil {
		return nil, fmt.Errorf("connection %q is not an AWS S3 connection", connectionId)
	}
	// Resolved here rather than in Connect: a config that cannot be resolved must fail the
	// stream, not have Connect retried forever while the fallback never reaches its error output.
	awsConfig, err := awsmanager.S3AwsConfig(context.Background(), s3Config, identity)
	if err != nil {
		return nil, fmt.Errorf("unable to resolve the aws config of connection %q: %w", connectionId, err)
	}

	path, err := conf.FieldInterpolatedString(fieldPath)
	if err != nil {
		return nil, err
	}
	contentType, err := conf.FieldString(fieldContentType)
	if err != nil {
		return nil, err
	}
	storageClass, err := conf.FieldString(fieldStorageClass)
	if err != nil {
		return nil, err
	}
	timeout, err := conf.FieldDuration(fieldTimeout)
	if err != nil {
		return nil, err
	}
	return &s3Writer{
		awsConfig:    awsConfig,
		endpoint:     s3Config.GetEndpoint(),
		bucket:       s3Config.GetBucket(),
		path:         path,
		contentType:  contentType,
		storageClass: types.StorageClass(storageClass),
		timeout:      timeout,
	}, nil
}

func (w *s3Writer) Connect(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.client != nil {
		return nil
	}
	w.client = awsmanager.NewS3ClientFromConfig(w.awsConfig, w.endpoint)
	return nil
}

func (w *s3Writer) WriteBatch(ctx context.Context, batch service.MessageBatch) error {
	w.mu.RLock()
	client := w.client
	w.mu.RUnlock()
	if client == nil {
		return service.ErrNotConnected
	}

	// The uploads that fail are told one by one: told failed as a whole, the batch would be
	// written again whole, and the objects already uploaded uploaded once more, under the key
	// their path gives them then.
	return batch.WalkWithBatchedErrors(func(i int, msg *service.Message) error {
		key, err := batch.TryInterpolatedString(i, w.path)
		if err != nil {
			return fmt.Errorf("unable to interpolate the object key: %w", err)
		}
		body, err := msg.AsBytes()
		if err != nil {
			return err
		}
		return w.put(ctx, client, key, body)
	})
}

func (w *s3Writer) put(ctx context.Context, client putObjectAPI, key string, body []byte) error {
	ctx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()
	_, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:       aws.String(w.bucket),
		Key:          aws.String(key),
		Body:         bytes.NewReader(body),
		ContentType:  aws.String(w.contentType),
		StorageClass: w.storageClass,
	})
	if err != nil {
		return fmt.Errorf("unable to upload object %q: %w", key, err)
	}
	return nil
}

func (w *s3Writer) Close(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.client = nil
	return nil
}
