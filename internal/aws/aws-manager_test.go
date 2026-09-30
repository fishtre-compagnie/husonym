package awsmanager

import (
	"context"
	"testing"

	"github.com/fishtre-compagnie/husonym/internal/cloudidentity"

	"github.com/aws/aws-sdk-go-v2/aws"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
)

// The endpoint of a connection is where its data goes: one the environment sets for the service
// must not send it elsewhere.
func Test_ClientsPreferTheEndpointOfTheConnection(t *testing.T) {
	t.Setenv("AWS_CONFIG_FILE", t.TempDir()+"/config")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", t.TempDir()+"/credentials")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_ENDPOINT_URL", "")
	t.Setenv("AWS_IGNORE_CONFIGURED_ENDPOINT_URLS", "")
	t.Setenv("AWS_ENDPOINT_URL_S3", "http://from-the-environment:1")
	t.Setenv("AWS_ENDPOINT_URL_DYNAMODB", "http://from-the-environment:2")
	ctx := context.Background()

	s3Cfg, err := S3AwsConfig(ctx, &mgmtv1alpha1.AwsS3ConnectionConfig{
		Region: aws.String("us-east-1"), Endpoint: aws.String("http://s3-connection:9000"),
	}, cloudidentity.Policy{AllowServerIdentity: true})
	require.NoError(t, err)
	require.Equal(t, "http://s3-connection:9000",
		aws.ToString(NewS3ClientFromConfig(s3Cfg, "http://s3-connection:9000").Options().BaseEndpoint))

	dynamoCfg, err := DynamoDbAwsConfig(ctx, &mgmtv1alpha1.DynamoDBConnectionConfig{
		Region: aws.String("us-east-1"), Endpoint: aws.String("http://dynamodb-connection:8000"),
	}, cloudidentity.Policy{AllowServerIdentity: true})
	require.NoError(t, err)
	require.Equal(t, "http://dynamodb-connection:8000",
		aws.ToString(NewDynamoDbClientFromConfig(dynamoCfg, "http://dynamodb-connection:8000").Options().BaseEndpoint))

	// Without an endpoint of its own, the connection leaves the environment's in place.
	require.Equal(t, "http://from-the-environment:1",
		aws.ToString(NewS3ClientFromConfig(s3Cfg, "").Options().BaseEndpoint))
}

// Without the deployment's leave, a connection that would act with the server's identity is
// refused before any config is resolved; one with keys of its own is resolved.
func Test_AwsConfig_ServerIdentity(t *testing.T) {
	ctx := context.Background()
	keys := &mgmtv1alpha1.AwsS3Credentials{AccessKeyId: aws.String("id"), SecretAccessKey: aws.String("secret")}

	_, err := S3AwsConfig(ctx, &mgmtv1alpha1.AwsS3ConnectionConfig{Region: aws.String("us-east-1")}, cloudidentity.Policy{})
	require.ErrorContains(t, err, cloudidentity.Variable)
	_, err = DynamoDbAwsConfig(ctx, &mgmtv1alpha1.DynamoDBConnectionConfig{Region: aws.String("us-east-1")}, cloudidentity.Policy{})
	require.ErrorContains(t, err, cloudidentity.Variable)

	_, err = S3AwsConfig(
		ctx,
		&mgmtv1alpha1.AwsS3ConnectionConfig{Region: aws.String("us-east-1"), Credentials: keys},
		cloudidentity.Policy{},
	)
	require.NoError(t, err)
	_, err = DynamoDbAwsConfig(
		ctx,
		&mgmtv1alpha1.DynamoDBConnectionConfig{Region: aws.String("us-east-1"), Credentials: keys},
		cloudidentity.Policy{},
	)
	require.NoError(t, err)
}

// A role assumed with the process's own identity is never asked of the connection's endpoint,
// which would receive that identity; with the connection's own keys, the connection's endpoint
// serves STS as it serves the rest.
func Test_newStsClient_KeepsTheServerIdentityAwayFromTheConnection(t *testing.T) {
	t.Setenv("AWS_ENDPOINT_URL_STS", "")
	t.Setenv("AWS_ENDPOINT_URL", "")
	endpoint := "https://endpoint-of-the-connection"
	conf := aws.Config{Region: "us-east-1", BaseEndpoint: &endpoint}

	require.Nil(t, newStsClient(&conf, &AwsCredentialsConfig{Role: "r"}).Options().BaseEndpoint)
	require.Nil(t, newStsClient(&conf, &AwsCredentialsConfig{Role: "r", Profile: "p", Id: "id"}).Options().BaseEndpoint)
	require.Equal(t, endpoint, aws.ToString(
		newStsClient(&conf, &AwsCredentialsConfig{Role: "r", Id: "id", Secret: "secret"}).Options().BaseEndpoint))
}
