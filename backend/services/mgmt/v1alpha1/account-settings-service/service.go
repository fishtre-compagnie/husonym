// Package v1alpha1_accountsettingservice holds the settings of an account: a value that
// varies by account, part of which is a secret, and that a human sets once.
//
// The service is only wired when the deployment can keep a secret
// (HUSONYM_SYM_ENCRYPTION_PASSWORD); without it the handler answers Unimplemented, and a
// run reads that as "no account settings here" and falls back on its own variable.
package v1alpha1_accountsettingservice

import (
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
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
