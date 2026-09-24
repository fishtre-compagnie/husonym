// Package permission is the one place that says whether a scoped API key may do something.
//
// A key's scope is a list of permissions — the actions the RBAC already names, one entity and
// one action each (job:execute, connection:view_sensitive). It can only narrow what the key may
// do: a permission the scope does not name is refused, and a scope that names nothing allows
// nothing. There is no fallback to "whatever is open".
//
// It is asked at two places: at the entrance of every procedure, against what the procedure
// declares it requires (the mgmt.v1alpha1.requires option), and wherever a handler asks the
// RBAC about an entity — which is what tells a connection's view from its secrets within a
// single call. Both ask the same question here, so that they cannot drift apart.
package permission

import (
	"fmt"
	"strings"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/ee/rbac"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
)

const enumPrefix = "PERMISSION_"

// Name gives a permission the way it is stored and shown: entity:action.
func Name(p mgmtv1alpha1.Permission) string {
	entity, action, _ := strings.Cut(strings.TrimPrefix(p.String(), enumPrefix), "_")
	return strings.ToLower(entity) + ":" + strings.ToLower(action)
}

// Parse reads a permission back from its name. An unknown name is no permission at all.
func Parse(name string) (mgmtv1alpha1.Permission, bool) {
	entity, action, ok := strings.Cut(name, ":")
	if !ok {
		return mgmtv1alpha1.Permission_PERMISSION_UNSPECIFIED, false
	}
	value, ok := mgmtv1alpha1.Permission_value[enumPrefix+strings.ToUpper(entity)+"_"+strings.ToUpper(action)]
	if !ok || value == int32(mgmtv1alpha1.Permission_PERMISSION_UNSPECIFIED) {
		return mgmtv1alpha1.Permission_PERMISSION_UNSPECIFIED, false
	}
	return mgmtv1alpha1.Permission(value), true
}

// All is every permission there is, in the order the enum declares them.
func All() []mgmtv1alpha1.Permission {
	values := mgmtv1alpha1.Permission_PERMISSION_UNSPECIFIED.Descriptor().Values()
	all := make([]mgmtv1alpha1.Permission, 0, values.Len())
	for i := range values.Len() {
		if p := mgmtv1alpha1.Permission(values.Get(i).Number()); p != mgmtv1alpha1.Permission_PERMISSION_UNSPECIFIED {
			all = append(all, p)
		}
	}
	return all
}

// FromNames reads permissions as they are stored, dropping a name that is no permission.
func FromNames(names []string) []mgmtv1alpha1.Permission {
	permissions := make([]mgmtv1alpha1.Permission, 0, len(names))
	for _, name := range names {
		if p, ok := Parse(name); ok {
			permissions = append(permissions, p)
		}
	}
	return permissions
}

// Split gives the entity and the action a permission names, as the RBAC calls them.
func Split(p mgmtv1alpha1.Permission) (entity, action string) {
	entity, action, _ = strings.Cut(Name(p), ":")
	return entity, action
}

// Names gives permissions as they are stored.
func Names(permissions []mgmtv1alpha1.Permission) []string {
	names := make([]string, 0, len(permissions))
	for _, p := range permissions {
		names = append(names, Name(p))
	}
	return names
}

// Account, Connection and Job name the permission an RBAC action on that entity requires.
func Account(action rbac.AccountAction) mgmtv1alpha1.Permission {
	return mustParse("account:" + action.String())
}

func Connection(action rbac.ConnectionAction) mgmtv1alpha1.Permission {
	return mustParse("connection:" + action.String())
}

func Job(action rbac.JobAction) mgmtv1alpha1.Permission {
	return mustParse("job:" + action.String())
}

// mustParse fails loudly on an RBAC action no permission names: a new action with no permission
// would otherwise be one no scoped key could ever be granted, found in production. The test of
// this package walks every action so that this never happens there first.
func mustParse(name string) mgmtv1alpha1.Permission {
	p, ok := Parse(name)
	if !ok {
		panic(fmt.Sprintf("no permission names the RBAC action %q: add it to the Permission enum", name))
	}
	return p
}

// Scope is what a key was granted.
type Scope struct {
	granted map[mgmtv1alpha1.Permission]bool
}

// NewScope reads a scope as it is stored. A name it does not know grants nothing.
func NewScope(names []string) Scope {
	granted := make(map[mgmtv1alpha1.Permission]bool, len(names))
	for _, name := range names {
		if p, ok := Parse(name); ok {
			granted[p] = true
		}
	}
	return Scope{granted: granted}
}

// Allows says whether the scope grants a permission.
func (s Scope) Allows(p mgmtv1alpha1.Permission) bool {
	return s.granted[p]
}

// Require refuses, naming what is missing, unless the scope grants every permission given.
// Nothing given is nothing required.
func (s Scope) Require(required ...mgmtv1alpha1.Permission) error {
	for _, p := range required {
		if !s.Allows(p) {
			return Denied(p)
		}
	}
	return nil
}

// Denied is the refusal a scoped key gets. It names the permission, so that whoever holds the
// key — a person, or an agent — knows what to ask for.
func Denied(p mgmtv1alpha1.Permission) error {
	return husonymerrors.NewUnauthorized(fmt.Sprintf("this API key lacks the permission %s", Name(p)))
}
