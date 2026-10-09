package login_cmd

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
)

func Test_defaultAccount(t *testing.T) {
	personal := &mgmtv1alpha1.UserAccount{Id: "p", Name: "Personal", Type: mgmtv1alpha1.UserAccountType_USER_ACCOUNT_TYPE_PERSONAL}
	organization := &mgmtv1alpha1.UserAccount{Id: "o", Name: "organization", Type: mgmtv1alpha1.UserAccountType_USER_ACCOUNT_TYPE_TEAM}
	team := &mgmtv1alpha1.UserAccount{Id: "t", Name: "team", Type: mgmtv1alpha1.UserAccountType_USER_ACCOUNT_TYPE_TEAM}
	other := &mgmtv1alpha1.UserAccount{Id: "u", Name: "other", Type: mgmtv1alpha1.UserAccountType_USER_ACCOUNT_TYPE_TEAM}
	accounts := func(of ...*mgmtv1alpha1.UserAccount) []*mgmtv1alpha1.UserAccount { return of }

	t.Run("the personal account, as before, even beside the organization", func(t *testing.T) {
		require.Same(t, personal, defaultAccount(accounts(organization, personal, team), organization.GetId()))
		require.Same(t, personal, defaultAccount(accounts(team, personal), ""))
	})

	t.Run("without a personal account, the organization of the instance the person is in", func(t *testing.T) {
		require.Same(t, organization, defaultAccount(accounts(organization), organization.GetId()))
		require.Same(t, organization, defaultAccount(accounts(team, organization, other), organization.GetId()))
	})

	t.Run("without either, the only account there is", func(t *testing.T) {
		require.Same(t, team, defaultAccount(accounts(team), ""))
		// The instance retains an organization the person is not in.
		require.Same(t, team, defaultAccount(accounts(team), organization.GetId()))
	})

	t.Run("otherwise none", func(t *testing.T) {
		require.Nil(t, defaultAccount(accounts(team, other), ""))
		require.Nil(t, defaultAccount(accounts(team, other), organization.GetId()))
		require.Nil(t, defaultAccount(nil, organization.GetId()))
		require.Nil(t, defaultAccount(nil, ""))
	})
}
