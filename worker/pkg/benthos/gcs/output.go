package husonym_benthos_gcs

import (
	"context"
	"fmt"
	"sync"
	"time"

	"cloud.google.com/go/storage"
	"github.com/fishtre-compagnie/husonym/internal/cloudidentity"
	connectionmanager "github.com/fishtre-compagnie/husonym/internal/connection-manager"
	husonym_gcp "github.com/fishtre-compagnie/husonym/internal/gcp"
	"github.com/redpanda-data/benthos/v4/public/service"
)

const (
	fieldConnectionId    = "connection_id"
	fieldPath            = "path"
	fieldContentType     = "content_type"
	fieldContentEncoding = "content_encoding"
	fieldTimeout         = "timeout"
	fieldBatching        = "batching"
)

func outputSpec() *service.ConfigSpec {
	return service.NewConfigSpec().
		Categories("Services", "GCP").
		Summary("Writes each message as an object of the bucket of a GCP Cloud Storage connection.").
		Fields(
			service.NewStringField(fieldConnectionId).
				Description("The Husonym connection whose bucket and credentials are used."),
			service.NewInterpolatedStringField(fieldPath).
				Description("The name of each object."),
			service.NewStringField(fieldContentType).
				Default("application/octet-stream"),
			service.NewStringField(fieldContentEncoding).
				Default(""),
			service.NewDurationField(fieldTimeout).
				Description("The maximum time an upload may take.").
				Default("5s"),
			service.NewOutputMaxInFlightField(),
			service.NewBatchPolicyField(fieldBatching),
		)
}

// RegisterGcpCloudStorageOutput registers husonym_gcp_cloud_storage, which resolves its
// connection when the stream runs: the bucket and the service account live in the connection
// only, so that the stored config of a run holds no secret.
func RegisterGcpCloudStorageOutput(
	env *service.Environment,
	getConnection func(connectionId string) (connectionmanager.ConnectionInput, error),
	identity cloudidentity.Policy,
) error {
	return env.RegisterBatchOutput(
		"husonym_gcp_cloud_storage",
		outputSpec(),
		func(conf *service.ParsedConfig, mgr *service.Resources) (out service.BatchOutput, batchPolicy service.BatchPolicy, maxInFlight int, err error) {
			if maxInFlight, err = conf.FieldMaxInFlight(); err != nil {
				return
			}
			if batchPolicy, err = conf.FieldBatchPolicy(fieldBatching); err != nil {
				return
			}
			out, err = newGcsWriter(conf, getConnection, identity)
			return
		},
	)
}

// objectStore writes one object of the bucket.
type objectStore interface {
	put(ctx context.Context, name, contentType, contentEncoding string, body []byte) error
	close() error
}

type gcsWriter struct {
	path            *service.InterpolatedString
	contentType     string
	contentEncoding string
	timeout         time.Duration

	mu    sync.RWMutex
	store objectStore // set at build, released by Close
}

func newGcsWriter(
	conf *service.ParsedConfig,
	getConnection func(connectionId string) (connectionmanager.ConnectionInput, error),
	identity cloudidentity.Policy,
) (*gcsWriter, error) {
	connectionId, err := conf.FieldString(fieldConnectionId)
	if err != nil {
		return nil, err
	}
	connection, err := getConnection(connectionId)
	if err != nil {
		return nil, err
	}
	gcsConfig := connection.GetConnectionConfig().GetGcpCloudstorageConfig()
	if gcsConfig == nil {
		return nil, fmt.Errorf("connection %q is not a GCP Cloud Storage connection", connectionId)
	}
	// Made here rather than in Connect: credentials that cannot be used must fail the stream,
	// not have Connect retried forever while the fallback never reaches its error output.
	client, err := husonym_gcp.NewStorageClient(context.Background(), gcsConfig, identity)
	if err != nil {
		return nil, fmt.Errorf("unable to use the credentials of connection %q: %w", connectionId, err)
	}

	path, err := conf.FieldInterpolatedString(fieldPath)
	if err != nil {
		return nil, err
	}
	contentType, err := conf.FieldString(fieldContentType)
	if err != nil {
		return nil, err
	}
	contentEncoding, err := conf.FieldString(fieldContentEncoding)
	if err != nil {
		return nil, err
	}
	timeout, err := conf.FieldDuration(fieldTimeout)
	if err != nil {
		return nil, err
	}
	return &gcsWriter{
		path:            path,
		contentType:     contentType,
		contentEncoding: contentEncoding,
		timeout:         timeout,
		store:           &bucketStore{bucket: client.Bucket(gcsConfig.GetBucket()), client: client},
	}, nil
}

func (w *gcsWriter) Connect(ctx context.Context) error {
	return nil
}

func (w *gcsWriter) WriteBatch(ctx context.Context, batch service.MessageBatch) error {
	w.mu.RLock()
	store := w.store
	w.mu.RUnlock()
	if store == nil {
		return service.ErrNotConnected
	}

	for i, msg := range batch {
		name, err := batch.TryInterpolatedString(i, w.path)
		if err != nil {
			return fmt.Errorf("unable to interpolate the object name: %w", err)
		}
		body, err := msg.AsBytes()
		if err != nil {
			return err
		}
		putCtx, cancel := context.WithTimeout(ctx, w.timeout)
		err = store.put(putCtx, name, w.contentType, w.contentEncoding, body)
		cancel()
		if err != nil {
			return fmt.Errorf("unable to upload object %q: %w", name, err)
		}
	}
	return nil
}

func (w *gcsWriter) Close(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.store == nil {
		return nil
	}
	err := w.store.close()
	w.store = nil
	return err
}

// bucketStore writes to the bucket of a connection with its storage client.
type bucketStore struct {
	bucket *storage.BucketHandle
	client *storage.Client
}

func (b *bucketStore) put(ctx context.Context, name, contentType, contentEncoding string, body []byte) error {
	writer := b.bucket.Object(name).NewWriter(ctx)
	writer.ContentType = contentType
	writer.ContentEncoding = contentEncoding
	if _, err := writer.Write(body); err != nil {
		_ = writer.Close()
		return err
	}
	return writer.Close()
}

func (b *bucketStore) close() error {
	return b.client.Close()
}
