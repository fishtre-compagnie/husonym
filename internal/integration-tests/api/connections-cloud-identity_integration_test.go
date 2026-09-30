package integrationtests_test

import (
	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/gotypeutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// A cloud connection brings credentials of its own, a role assumed from them included: without
// keys, with a profile or with the instance role, it would act as the server, which a deployment
// allows only when it serves a single party. The tests' API does not.
func (s *IntegrationTestSuite) Test_Connection_CloudIdentityOfItsOwn() {
	t := s.T()
	ctx := s.ctx
	client := s.OSSUnauthenticatedLicensedClients.Connections()
	accountId := s.createPersonalAccount(ctx, s.OSSUnauthenticatedLicensedClients.Users())

	keys := &mgmtv1alpha1.AwsS3Credentials{
		AccessKeyId:     gotypeutil.ToPtr("the-key"),
		SecretAccessKey: gotypeutil.ToPtr("the-secret"),
	}
	s3 := func(credentials *mgmtv1alpha1.AwsS3Credentials) *mgmtv1alpha1.ConnectionConfig {
		return &mgmtv1alpha1.ConnectionConfig{Config: &mgmtv1alpha1.ConnectionConfig_AwsS3Config{
			AwsS3Config: &mgmtv1alpha1.AwsS3ConnectionConfig{Bucket: "bucket", Credentials: credentials},
		}}
	}
	dynamo := func(credentials *mgmtv1alpha1.AwsS3Credentials) *mgmtv1alpha1.ConnectionConfig {
		return &mgmtv1alpha1.ConnectionConfig{Config: &mgmtv1alpha1.ConnectionConfig_DynamodbConfig{
			DynamodbConfig: &mgmtv1alpha1.DynamoDBConnectionConfig{Credentials: credentials},
		}}
	}
	create := func(config *mgmtv1alpha1.ConnectionConfig) (*connect.Response[mgmtv1alpha1.CreateConnectionResponse], error) {
		return client.CreateConnection(ctx, connect.NewRequest(&mgmtv1alpha1.CreateConnectionRequest{
			AccountId: accountId, Name: uuid.NewString(), ConnectionConfig: config,
		}))
	}

	gcs := func(credentials *string) *mgmtv1alpha1.ConnectionConfig {
		return &mgmtv1alpha1.ConnectionConfig{Config: &mgmtv1alpha1.ConnectionConfig_GcpCloudstorageConfig{
			GcpCloudstorageConfig: &mgmtv1alpha1.GcpCloudStorageConnectionConfig{Bucket: "bucket", ServiceAccountCredentials: credentials},
		}}
	}
	for name, config := range map[string]*mgmtv1alpha1.ConnectionConfig{
		"gcs without a service account": gcs(nil),
		"s3 without keys":               s3(nil),
		"s3 with a profile":             s3(&mgmtv1alpha1.AwsS3Credentials{Profile: gotypeutil.ToPtr("default")}),
		"s3 with the ec2 role":          s3(&mgmtv1alpha1.AwsS3Credentials{FromEc2Role: gotypeutil.ToPtr(true)}),
		"dynamodb without keys":         dynamo(nil),
		"dynamodb with a role":          dynamo(&mgmtv1alpha1.AwsS3Credentials{RoleArn: gotypeutil.ToPtr("arn:aws:iam::1:role/r")}),
		"dynamodb with a role and an external id": dynamo(&mgmtv1alpha1.AwsS3Credentials{
			RoleArn: gotypeutil.ToPtr("arn:aws:iam::1:role/r"), RoleExternalId: gotypeutil.ToPtr("external"),
		}),
	} {
		_, err := create(config)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "%s: %v", name, err)
		require.ErrorContains(t, err, "credentials of its own", name)
	}

	createdGcs, err := create(gcs(gotypeutil.ToPtr(`{"type": "service_account"}`)))
	requireNoErrResp(t, createdGcs, err)
	for name, credentials := range map[string]*string{
		"no service account": nil,
		"another type":       gotypeutil.ToPtr(`{"type": "external_account"}`),
		"another endpoint":   gotypeutil.ToPtr(`{"type": "service_account", "token_uri": "http://internal-service/"}`),
	} {
		_, err = client.UpdateConnection(ctx, connect.NewRequest(&mgmtv1alpha1.UpdateConnectionRequest{
			Id: createdGcs.Msg.GetConnection().GetId(), Name: createdGcs.Msg.GetConnection().GetName(), ConnectionConfig: gcs(credentials),
		}))
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "%s: %v", name, err)
	}
	created, err := create(s3(keys))
	requireNoErrResp(t, created, err)
	createdDynamo, err := create(dynamo(keys))
	requireNoErrResp(t, createdDynamo, err)
	assumed, err := create(dynamo(&mgmtv1alpha1.AwsS3Credentials{
		AccessKeyId: keys.AccessKeyId, SecretAccessKey: keys.SecretAccessKey, RoleArn: gotypeutil.ToPtr("arn:aws:iam::1:role/r"),
	}))
	requireNoErrResp(t, assumed, err)

	_, err = client.UpdateConnection(ctx, connect.NewRequest(&mgmtv1alpha1.UpdateConnectionRequest{
		Id: created.Msg.GetConnection().GetId(), Name: created.Msg.GetConnection().GetName(), ConnectionConfig: s3(nil),
	}))
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "%v", err)
}
