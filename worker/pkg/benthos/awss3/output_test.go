package husonym_benthos_awss3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/fishtre-compagnie/husonym/internal/cloudidentity"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	connectionmanager "github.com/fishtre-compagnie/husonym/internal/connection-manager"
	_ "github.com/redpanda-data/benthos/v4/public/components/pure" // count(), which the builder's paths use
	"github.com/redpanda-data/benthos/v4/public/service"
	"github.com/stretchr/testify/require"
)

// fakeS3 records the uploads asked of it, and fails them with errs, one by upload.
type fakeS3 struct {
	mu   sync.Mutex
	puts []*s3.PutObjectInput
	errs []error
}

func (f *fakeS3) PutObject(
	ctx context.Context,
	params *s3.PutObjectInput,
	optFns ...func(*s3.Options),
) (*s3.PutObjectOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.puts = append(f.puts, params)
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		return nil, err
	}
	return &s3.PutObjectOutput{}, nil
}

func s3Connection(id, bucket string) *mgmtv1alpha1.Connection {
	return &mgmtv1alpha1.Connection{
		Id: id,
		ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{
			Config: &mgmtv1alpha1.ConnectionConfig_AwsS3Config{
				AwsS3Config: &mgmtv1alpha1.AwsS3ConnectionConfig{
					Bucket: bucket,
					Credentials: &mgmtv1alpha1.AwsS3Credentials{
						AccessKeyId: aws.String("the-key"), SecretAccessKey: aws.String("the-secret"),
					},
				},
			},
		},
	}
}

// connectionsOf resolves connections the way the worker does: by id, among those of the run.
func connectionsOf(
	connections ...*mgmtv1alpha1.Connection,
) func(connectionId string) (connectionmanager.ConnectionInput, error) {
	return func(connectionId string) (connectionmanager.ConnectionInput, error) {
		for _, connection := range connections {
			if connection.GetId() == connectionId {
				return connection, nil
			}
		}
		return nil, fmt.Errorf("unable to find connection by id: %q", connectionId)
	}
}

func newTestWriter(
	t *testing.T,
	yaml string,
	getConnection func(connectionId string) (connectionmanager.ConnectionInput, error),
) (*s3Writer, error) {
	t.Helper()
	conf, err := outputSpec().ParseYAML(yaml, service.NewEnvironment())
	require.NoError(t, err)
	return newS3Writer(conf, getConnection, cloudidentity.Policy{})
}

func Test_S3Writer_WritesToTheBucketOfTheConnection(t *testing.T) {
	writer, err := newTestWriter(t, `
connection_id: s3
path: workflows/run/${! meta("n") }.jsonl.gz
content_type: application/gzip
storage_class: STANDARD_IA
`, connectionsOf(s3Connection("s3", "the-bucket")))
	require.NoError(t, err)

	fake := &fakeS3{}
	writer.client = fake
	batch := service.MessageBatch{service.NewMessage([]byte("first")), service.NewMessage([]byte("second"))}
	batch[0].MetaSet("n", "1")
	batch[1].MetaSet("n", "2")
	require.NoError(t, writer.WriteBatch(context.Background(), batch))

	require.Len(t, fake.puts, 2)
	for i, want := range []struct{ key, body string }{
		{"workflows/run/1.jsonl.gz", "first"},
		{"workflows/run/2.jsonl.gz", "second"},
	} {
		put := fake.puts[i]
		require.Equal(t, "the-bucket", *put.Bucket)
		require.Equal(t, want.key, *put.Key)
		require.Equal(t, "application/gzip", *put.ContentType)
		require.Equal(t, types.StorageClassStandardIa, put.StorageClass)
		body, err := io.ReadAll(put.Body)
		require.NoError(t, err)
		require.Equal(t, want.body, string(body))
	}
}

func Test_S3Writer_LeavesTheStorageClassUnsetByDefault(t *testing.T) {
	writer, err := newTestWriter(t, `
connection_id: s3
path: object
`, connectionsOf(s3Connection("s3", "the-bucket")))
	require.NoError(t, err)

	fake := &fakeS3{}
	writer.client = fake
	require.NoError(t, writer.WriteBatch(context.Background(), service.MessageBatch{service.NewMessage([]byte("x"))}))
	require.Empty(t, fake.puts[0].StorageClass)
}

func Test_S3Writer_FailsTheBatchOnAFailedUpload(t *testing.T) {
	writer, err := newTestWriter(t, `
connection_id: s3
path: object
`, connectionsOf(s3Connection("s3", "the-bucket")))
	require.NoError(t, err)

	writer.client = &fakeS3{errs: []error{errors.New("access denied")}}
	err = writer.WriteBatch(context.Background(), service.MessageBatch{service.NewMessage([]byte("x"))})
	require.ErrorContains(t, err, "access denied")
}

// failedMessages tells the messages of the batch a write failed, by their index.
func failedMessages(t *testing.T, err error) map[int]string {
	t.Helper()
	var batchErr *service.BatchError
	require.ErrorAs(t, err, &batchErr, "the write does not tell which messages it failed")
	failed := map[int]string{}
	batchErr.WalkMessages(func(i int, _ *service.Message, err error) bool {
		if err != nil {
			failed[i] = err.Error()
		}
		return true
	})
	return failed
}

// A batch is written object by object. The write tells which uploads failed: told failed as a
// whole, the batch would be written again whole, and the objects already uploaded uploaded
// once more, under the key their path gives them then.
func Test_S3Writer_AFailedUploadFailsItsMessageOnly(t *testing.T) {
	writer, err := newTestWriter(t, `
connection_id: s3
path: workflows/run/${! meta("n") }.jsonl.gz
`, connectionsOf(s3Connection("s3", "the-bucket")))
	require.NoError(t, err)

	fake := &fakeS3{errs: []error{nil, errors.New("slow down")}}
	writer.client = fake
	batch := service.MessageBatch{
		service.NewMessage([]byte("first")),
		service.NewMessage([]byte("second")),
		service.NewMessage([]byte("third")),
	}
	for i, message := range batch {
		message.MetaSet("n", strconv.Itoa(i+1))
	}
	err = writer.WriteBatch(context.Background(), batch)

	failed := failedMessages(t, err)
	require.Len(t, failed, 1)
	require.Contains(t, failed[1], "slow down")
	require.Contains(t, failed[1], "workflows/run/2.jsonl.gz")
	require.Len(t, fake.puts, 3, "an upload that fails does not keep the next from being tried")
}

// In a stream, the upload that failed is the only one made again.
func Test_S3Output_OnlyTheFailedUploadIsMadeAgain(t *testing.T) {
	fake := &fakeS3{errs: []error{nil, errors.New("slow down")}}
	env := service.NewEnvironment()
	require.NoError(t, env.RegisterBatchOutput("s3_under_test", outputSpec(),
		func(conf *service.ParsedConfig, _ *service.Resources) (service.BatchOutput, service.BatchPolicy, int, error) {
			writer, err := newS3Writer(conf, connectionsOf(s3Connection("s3", "the-bucket")), cloudidentity.Policy{})
			if err != nil {
				return nil, service.BatchPolicy{}, 0, err
			}
			// Connect keeps the client it finds.
			writer.client = fake
			return writer, service.BatchPolicy{}, 1, nil
		}))
	builder := env.NewStreamBuilder()
	require.NoError(t, builder.SetYAML(`
input:
  generate:
    count: 3
    batch_size: 3
    interval: ""
    mapping: 'root = "row " + counter().string()'
output:
  s3_under_test:
    connection_id: s3
    path: 'workflows/run/${! count("s3-test-stream") }.txt'
logger:
  level: none
`))
	stream, err := builder.Build()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, stream.Run(ctx))

	bodies := []string{}
	for _, put := range fake.puts {
		body, err := io.ReadAll(put.Body)
		require.NoError(t, err)
		bodies = append(bodies, string(body))
	}
	// The second upload failed, and is the one made again.
	require.Equal(t, []string{"row 1", "row 2", "row 3", "row 2"}, bodies)
}

func Test_S3Writer_WritesNothingBeforeConnect(t *testing.T) {
	writer, err := newTestWriter(t, `
connection_id: s3
path: object
`, connectionsOf(s3Connection("s3", "the-bucket")))
	require.NoError(t, err)

	err = writer.WriteBatch(context.Background(), service.MessageBatch{service.NewMessage([]byte("x"))})
	require.ErrorIs(t, err, service.ErrNotConnected)
}

func Test_S3Writer_ResolvesAnS3ConnectionOfTheRun(t *testing.T) {
	_, err := newTestWriter(t, `
connection_id: unknown
path: object
`, connectionsOf(s3Connection("s3", "the-bucket")))
	require.ErrorContains(t, err, "unknown")

	_, err = newTestWriter(t, `
connection_id: pg
path: object
`, connectionsOf(&mgmtv1alpha1.Connection{
		Id: "pg",
		ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{
			Config: &mgmtv1alpha1.ConnectionConfig_PgConfig{PgConfig: &mgmtv1alpha1.PostgresConnectionConfig{}},
		},
	}))
	require.ErrorContains(t, err, "not an AWS S3 connection")
}

func Test_RegisterAwsS3Output(t *testing.T) {
	require.NoError(t, RegisterAwsS3Output(service.NewEmptyEnvironment(), connectionsOf(), cloudidentity.Policy{}))
}

// A connection whose AWS config cannot be resolved fails the stream when it is built: from
// Connect it would be retried forever, and the fallback would never reach its error output.
func Test_S3Writer_FailsToBuildOnAnUnresolvableAwsConfig(t *testing.T) {
	t.Setenv("AWS_CONFIG_FILE", t.TempDir()+"/config")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", t.TempDir()+"/credentials")
	t.Setenv("AWS_PROFILE", "")
	profile := "husonym-absent-profile"
	connection := s3Connection("s3", "the-bucket")
	connection.GetConnectionConfig().GetAwsS3Config().Credentials = &mgmtv1alpha1.AwsS3Credentials{Profile: &profile}

	conf, err := outputSpec().ParseYAML(`
connection_id: s3
path: object
`, service.NewEnvironment())
	require.NoError(t, err)

	// Where the deployment allows the server's identity, the profile is looked up, and missed.
	_, err = newS3Writer(conf, connectionsOf(connection), cloudidentity.Policy{AllowServerIdentity: true})
	require.ErrorContains(t, err, profile)
	// Elsewhere, a profile is the server's: the connection is refused before anything is read.
	_, err = newS3Writer(conf, connectionsOf(connection), cloudidentity.Policy{})
	require.ErrorContains(t, err, "credentials of its own")
}
