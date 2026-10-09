package integrationtests_test

import "github.com/fishtre-compagnie/husonym/backend/internal/auth/permission"

// PermissionNames are all the permissions an API key can hold, as they are stored. The tests
// outside of the API cannot reach the package that names them.
func PermissionNames() []string {
	return permission.Names(permission.All())
}
