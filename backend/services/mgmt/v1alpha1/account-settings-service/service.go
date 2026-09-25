// Package v1alpha1_accountsettingservice holds the settings of an account: a value that
// varies by account, part of which is a secret, and that a human sets once.
//
// The service is only wired when the deployment can keep a secret
// (HUSONYM_SYM_ENCRYPTION_PASSWORD); without it the handler answers Unimplemented, and a
// run reads that as "no account settings here" and falls back on its own variable.
package v1alpha1_accountsettingservice

import (
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/backend/internal/safehttp"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	sym_encrypt "github.com/fishtre-compagnie/husonym/internal/encrypt/sym"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
)

type Service struct {
	cfg            *Config
	db             *husonymdb.HusonymDb
	userdataclient userdata.Interface
	encryptor      sym_encrypt.Interface
}

type Config struct {
	IsHusonymCloud bool

	// AcceptedSignatureAlgorithms is what this deployment validates token signatures
	// with. A provider that signs with none of them is refused when it is tried, rather
	// than after it is saved.
	AcceptedSignatureAlgorithms []string

	// IssuerPolicy says where an account's provider may be: https, on the public internet,
	// unless the deployment allows otherwise. Checked when a provider is tried and when it
	// is saved.
	IssuerPolicy safehttp.Policy
}

var _ mgmtv1alpha1connect.AccountSettingServiceHandler = (*Service)(nil)

func New(
	cfg *Config,
	db *husonymdb.HusonymDb,
	userdataclient userdata.Interface,
	encryptor sym_encrypt.Interface,
) *Service {
	return &Service{
		cfg:            cfg,
		db:             db,
		userdataclient: userdataclient,
		encryptor:      encryptor,
	}
}
