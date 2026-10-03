package rbac

import (
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/jackc/pgx/v5/pgtype"
)

// User is a person whose access is checked. An API key is not one: what a key may do is not
// decided here.
type User struct{ id string }

// Account is an account roles are held in.
type Account struct{ id string }

// NewUser is the person of that identifier.
func NewUser(id string) User { return User{id: id} }

// NewPgUser is the person of that identifier, as the database gives it.
func NewPgUser(id pgtype.UUID) User { return NewUser(husonymdb.UUIDString(id)) }

// NewAccount is the account of that identifier.
func NewAccount(id string) Account { return Account{id: id} }

// stored spells the person the way the role assignments name them.
func (u User) stored() string { return "users/" + u.id }

// stored spells the account the way the role assignments name it.
func (a Account) stored() string { return "accounts/" + a.id }
