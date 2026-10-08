package login_cmd

import (
	"strings"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

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
