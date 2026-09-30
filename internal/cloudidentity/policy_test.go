package cloudidentity

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

// A connection acts with keys of its own, a role assumed from them included, or has the server
// assume a role that demands an external id. Without keys, with a profile, with the instance
// role or with a role that demands nothing, it would act as the server: refused unless the
// deployment allows it.
func Test_Policy_CheckAws(t *testing.T) {
	str := func(s string) *string { return &s }
	yes := true
	keys := func(c *mgmtv1alpha1.AwsS3Credentials) *mgmtv1alpha1.AwsS3Credentials {
		c.AccessKeyId, c.SecretAccessKey = str("id"), str("secret")
		return c
	}
	own := map[string]*mgmtv1alpha1.AwsS3Credentials{
		"keys":                 keys(&mgmtv1alpha1.AwsS3Credentials{}),
		"role from keys":       keys(&mgmtv1alpha1.AwsS3Credentials{RoleArn: str("arn:aws:iam::1:role/r")}),
		"keys with a token":    keys(&mgmtv1alpha1.AwsS3Credentials{SessionToken: str("token")}),
		"role and external id": {RoleArn: str("arn:aws:iam::1:role/r"), RoleExternalId: str("external")},
	}
	server := map[string]*mgmtv1alpha1.AwsS3Credentials{
		"none":              nil,
		"empty":             {},
		"id without secret": {AccessKeyId: str("id")},
		"profile":           {Profile: str("default")},
		"profile and keys":  keys(&mgmtv1alpha1.AwsS3Credentials{Profile: str("default")}),
		"ec2 role":          {FromEc2Role: &yes},
		"ec2 role and keys": keys(&mgmtv1alpha1.AwsS3Credentials{FromEc2Role: &yes}),
		"role without keys": {RoleArn: str("arn:aws:iam::1:role/r")},
	}

	for name, credentials := range own {
		require.NoError(t, Policy{}.CheckAws(credentials), name)
	}
	for name, credentials := range server {
		require.ErrorContains(t, Policy{}.CheckAws(credentials), Variable, name)
		require.NoError(t, Policy{AllowServerIdentity: true}.CheckAws(credentials), name)
	}
}

// HusonymCloud never allows the server's identity, whatever the setting says.
func Test_FromEnvironment(t *testing.T) {
	t.Cleanup(func() { viper.Set(Variable, nil) })
	viper.Set(Variable, true)
	require.True(t, FromEnvironment(false).AllowServerIdentity)
	require.False(t, FromEnvironment(true).AllowServerIdentity)
	viper.Set(Variable, nil)
	require.False(t, FromEnvironment(false).AllowServerIdentity)
}
