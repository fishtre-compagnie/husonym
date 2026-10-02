package rbac

// Enforcer is what the rbac engine asks of its enforcer: these calls, and no other. The
// enforcer of the API keeps a change of the rules and a reload of them from one another, for
// the calls it offers only.
type Enforcer interface {
	Enforce(rvals ...any) (bool, error)
	HasPolicy(params ...any) (bool, error)
	GetNamedGroupingPolicy(ptype string) ([][]string, error)
	GetRolesForUserInDomain(name, domain string) []string

	AddPolicy(params ...any) (bool, error)
	AddNamedGroupingPolicies(ptype string, rules [][]string) (bool, error)
	AddRoleForUserInDomain(user, role, domain string) (bool, error)
	DeleteRoleForUserInDomain(user, role, domain string) (bool, error)
	DeleteRolesForUserInDomain(user, domain string) (bool, error)

	LoadPolicy() error
}

type Rbac struct {
	e Enforcer
}

// Combines RBAC interface that handles entity enforcement and role management
type Interface interface {
	EntityEnforcer
	RoleAdmin
}

var _ Interface = (*Rbac)(nil)

func New(
	e Enforcer,
) *Rbac {
	return &Rbac{e: e}
}
