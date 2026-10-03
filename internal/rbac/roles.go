package rbac

import (
	"slices"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

// The roles, as the role assignments name them.
const (
	roleAdmin     = "account_admin"
	roleDeveloper = "job_developer"
	roleExecutor  = "job_executor"
	roleViewer    = "job_viewer"
)

// roles lists the roles from the one that may do the most to the one that may do the least:
// each may do everything the next one may.
var roles = []struct {
	word string
	role mgmtv1alpha1.AccountRole
}{
	{roleAdmin, mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_ADMIN},
	{roleDeveloper, mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_DEVELOPER},
	{roleExecutor, mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_EXECUTOR},
	{roleViewer, mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_VIEWER},
}

// roleWord gives the word a role is stored under. The unspecified role, and a number that is
// no role, have none.
func roleWord(role mgmtv1alpha1.AccountRole) (string, bool) {
	for _, known := range roles {
		if known.role == role {
			return known.word, true
		}
	}
	return "", false
}

// highestRole reads a role back from the words a member's roles are stored under: of several,
// the one that may do the most. It is the unspecified role if none of them is a role.
func highestRole(words []string) mgmtv1alpha1.AccountRole {
	for _, known := range roles {
		if slices.Contains(words, known.word) {
			return known.role
		}
	}
	return mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_UNSPECIFIED
}
