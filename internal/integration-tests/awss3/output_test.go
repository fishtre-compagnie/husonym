package awss3_test

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/internal/cloudidentity"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	awsmanager "github.com/fishtre-compagnie/husonym/internal/aws"
	connectionmanager "github.com/fishtre-compagnie/husonym/internal/connection-manager"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	husonym_benthos_awss3 "github.com/fishtre-compagnie/husonym/worker/pkg/benthos/awss3"
	_ "github.com/redpanda-data/benthos/v4/public/components/pure"
	"github.com/redpanda-data/benthos/v4/public/service"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	accessKey = "husonym-it"
	secretKey = "husonym-it-secret"
	bucket    = "husonym-it"
)

// husonym_aws_s3 against a real S3 API: the stream config names the connection only, and the
// bucket, endpoint and credentials it writes with are those of the connection.
func Test_HusonymAwsS3Output(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := context.Background()

	endpoint := startS3(ctx, t)
	s3Connection := func(secret string) *mgmtv1alpha1.Connection {
		return &mgmtv1alpha1.Connection{
			Id: "s3-connection",
			ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{
				Config: &mgmtv1alpha1.ConnectionConfig_AwsS3Config{
					AwsS3Config: &mgmtv1alpha1.AwsS3ConnectionConfig{
						Bucket:   bucket,
						Region:   aws.String("us-east-1"),
						Endpoint: aws.String(endpoint),
						Credentials: &mgmtv1alpha1.AwsS3Credentials{
							AccessKeyId:     aws.String(accessKey),
							SecretAccessKey: aws.String(secret),
						},
					},
				},
			},
		}
	}
	connection := s3Connection(secretKey)
	client, err := awsmanager.New(cloudidentity.Policy{}).NewS3Client(ctx, connection.GetConnectionConfig().GetAwsS3Config())
	require.NoError(t, err)
	// The gateway listens before it serves: the bucket is created once it answers.
	require.Eventually(t, func() bool {
		_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
		return err == nil
	}, time.Minute, time.Second)
	// Its first write takes its time, while it makes room for the bucket: longer, on a busy
	// machine, than the stream gives an upload. An upload cut short may be stored all the same,
	// and is written again under the next key: the stream would write a row twice. The first
	// write is made here, outside the keys the stream writes, and waited for.
	require.Eventually(t, func() bool {
		_, err := client.PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String("first-write"),
			Body:   strings.NewReader("x"),
		})
		return err == nil
	}, time.Minute, time.Second)

	// The gateway checks credentials: a key other than the connection's is refused, so a
	// stream that writes did so with the connection's.
	wrong, err := awsmanager.New(cloudidentity.Policy{}).
		NewS3Client(ctx, s3Connection("not-the-secret").GetConnectionConfig().GetAwsS3Config())
	require.NoError(t, err)
	_, err = wrong.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String("refused"),
		Body:   strings.NewReader("x"),
	})
	require.Error(t, err)

	env := service.NewEnvironment()
	require.NoError(t, husonym_benthos_awss3.RegisterAwsS3Output(
		env,
		func(connectionId string) (connectionmanager.ConnectionInput, error) {
			if connectionId != connection.GetId() {
				return nil, fmt.Errorf("unable to find connection by id: %q", connectionId)
			}
			return connection, nil
		},
		cloudidentity.Policy{},
	))
	builder := env.NewStreamBuilder()
	require.NoError(t, builder.SetYAML(`
input:
  generate:
    count: 2
    interval: ""
    mapping: 'root = "row " + counter().string()'
output:
  husonym_aws_s3:
    connection_id: s3-connection
    path: 'workflows/run/${! count("husonym-it-files") }.txt'
    content_type: text/plain
    timeout: 30s
logger:
  level: ERROR
`))
	// The errors of the stream are told with the test: an upload that fails says why.
	builder.SetLogger(testutil.GetTestLogger(t))
	stream, err := builder.Build()
	require.NoError(t, err)
	runCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	require.NoError(t, stream.Run(runCtx))

	// The keys are not asserted: count() is evaluated again when a write is retried, as it
	// was by the native aws_s3 output, so a retry moves them on.
	listed, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String("workflows/run/"),
	})
	require.NoError(t, err)
	bodies, keys := []string{}, []string{}
	for _, item := range listed.Contents {
		keys = append(keys, aws.ToString(item.Key))
		object, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: item.Key})
		require.NoError(t, err)
		body, err := io.ReadAll(object.Body)
		require.NoError(t, err)
		require.Equal(t, "text/plain", aws.ToString(object.ContentType))
		bodies = append(bodies, string(body))
	}
	require.ElementsMatch(t, []string{"row 1", "row 2"}, bodies, "objects written: %v", keys)
}

// startS3 starts a SeaweedFS S3 gateway which knows one identity, that of the connection.
func startS3(ctx context.Context, t *testing.T) string {
	t.Helper()
	identities := fmt.Sprintf(
		`{"identities":[{"name":"husonym-it","credentials":[{"accessKey":%q,"secretKey":%q}],"actions":["Admin","Read","Write","List"]}]}`,
		accessKey,
		secretKey,
	)
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "chrislusf/seaweedfs:4.47",
			Cmd:          []string{"server", "-dir=/data", "-s3", "-s3.config=/etc/seaweedfs/s3.json"},
			ExposedPorts: []string{"8333/tcp"},
			Files: []testcontainers.ContainerFile{{
				Reader:            strings.NewReader(identities),
				ContainerFilePath: "/etc/seaweedfs/s3.json",
				FileMode:          0o644,
			}},
			WaitingFor: wait.ForListeningPort("8333/tcp"),
		},
		Started: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	port, err := container.MappedPort(ctx, "8333/tcp")
	require.NoError(t, err)
	// An IP host makes the SDK address the bucket in the path, as the gateway expects.
	return fmt.Sprintf("http://127.0.0.1:%d", port.Num())
}
