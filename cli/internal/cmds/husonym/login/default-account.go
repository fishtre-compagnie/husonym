package login_cmd

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

// systemInformation is what tells the organization of an instance.
type systemInformation interface {
	GetSystemInformation(
		ctx context.Context,
		req *connect.Request[mgmtv1alpha1.GetSystemInformationRequest],
	) (*connect.Response[mgmtv1alpha1.GetSystemInformationResponse], error)
}

// instanceOrganizationId gives the account the instance retains as its organization, empty when
// it retains none. It is what a person without a personal account works in.
//
// When the instance does not tell, it gives none and warns: the person is logged in by then, the
// account is a convenience on top, and it is picked as on an instance without an organization.
func instanceOrganizationId(ctx context.Context, instance systemInformation, logger *slog.Logger) string {
	resp, err := instance.GetSystemInformation(ctx, connect.NewRequest(&mgmtv1alpha1.GetSystemInformationRequest{}))
	if err != nil {
		logger.Warn(fmt.Sprintf("unable to read the organization of the instance: %s", err.Error()))
		return ""
	}
	return resp.Msg.GetInstanceOrganizationAccountId()
}

// defaultAccount picks, among the accounts of the person who just logged in, the one their
// commands use until they switch: their personal account; without one, the organization of the
// instance when they are in it; without either, the only account they have. It picks none when
// several are left to choose from, or when there is none.
//
// organizationId is the account the instance retains as its organization, empty when it retains
// none.
func defaultAccount(accounts []*mgmtv1alpha1.UserAccount, organizationId string) *mgmtv1alpha1.UserAccount {
	for _, account := range accounts {
		if strings.EqualFold(account.GetName(), "personal") {
			return account
		}
	}
	if organizationId != "" {
		for _, account := range accounts {
			if account.GetId() == organizationId {
				return account
			}
		}
	}
	if len(accounts) == 1 {
		return accounts[0]
	}
	return nil
}
