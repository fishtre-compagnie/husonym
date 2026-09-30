// Package cloudidentity says whether a cloud connection may act with the cloud identity of
// the server that uses it, the API's or the worker's, rather than with credentials of its own.
package cloudidentity

import (
	"errors"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/spf13/viper"
)

// Variable is the setting of a deployment that lets connections use the server's identity.
const Variable = "CONNECTIONS_ALLOW_SERVER_IDENTITY"

// Policy says whether a connection may use the cloud identity of the server. A connection
// without credentials of its own, or naming a profile or the instance role, acts with the
// server's: with several accounts, one would reach what the host reaches. Only a deployment
// that serves one party allows it.
type Policy struct {
	AllowServerIdentity bool
}

// FromEnvironment is the policy a server reads from its deployment. HusonymCloud never allows
// the server's identity.
func FromEnvironment(isHusonymCloud bool) Policy {
	return Policy{AllowServerIdentity: !isHusonymCloud && viper.GetBool(Variable)}
}

// awsCredentials are the credentials of an AWS connection, S3 or DynamoDB.
type awsCredentials interface {
	GetProfile() string
	GetAccessKeyId() string
	GetSecretAccessKey() string
	GetFromEc2Role() bool
}

var _ awsCredentials = (*mgmtv1alpha1.AwsS3Credentials)(nil)

// CheckAws refuses AWS credentials that fall back on the server's identity: none, a profile
// read from the server, or its instance role. A role is assumed from the connection's own
// keys: assumed by the server, it would be any role the server may assume, whatever external
// id the connection names, since a role that does not demand one ignores it.
func (p Policy) CheckAws(credentials awsCredentials) error {
	if p.AllowServerIdentity {
		return nil
	}
	switch {
	case credentials.GetProfile() != "":
		return refused(errors.New("an AWS profile is read from the server"))
	case credentials.GetFromEc2Role():
		return refused(errors.New("the EC2 role is the server's"))
	case credentials.GetAccessKeyId() == "" || credentials.GetSecretAccessKey() == "":
		return refused(errors.New("without an access key, the connection acts as the server, a role it names included"))
	default:
		return nil
	}
}

func refused(reason error) error {
	return husonymerrors.NewBadRequest(
		"the connection must bring credentials of its own: " + reason.Error() +
			" (a deployment serving a single party may allow it with " + Variable + ")",
	)
}
