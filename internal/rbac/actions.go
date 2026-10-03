package rbac

// Action is something a member does on one kind of object of an account: the account itself,
// its connections, its jobs. Each kind has its own actions, so that an action cannot be asked
// of the wrong kind.
type Action interface {
	// String is the word of the action, as the rules and the API keys name it.
	String() string
	// Kind is the kind of object the action is done on: account, connection or job.
	Kind() string
	// object spells what the action is done on in an account, the way the rules name it.
	object(account Account) string
}

type (
	AccountAction    string
	ConnectionAction string
	JobAction        string
)

const (
	AccountAction_View   AccountAction = "view"
	AccountAction_Edit   AccountAction = "edit"
	AccountAction_Create AccountAction = "create"
	AccountAction_Delete AccountAction = "delete"
)

const (
	ConnectionAction_View          ConnectionAction = "view"
	ConnectionAction_ViewSensitive ConnectionAction = "view_sensitive"
	ConnectionAction_Create        ConnectionAction = "create"
	ConnectionAction_Edit          ConnectionAction = "edit"
	ConnectionAction_Delete        ConnectionAction = "delete"
)

const (
	JobAction_View    JobAction = "view"
	JobAction_Execute JobAction = "execute"
	JobAction_Create  JobAction = "create"
	JobAction_Edit    JobAction = "edit"
	JobAction_Delete  JobAction = "delete"
)

const (
	anything       = "*"
	allAccounts    = "accounts/*"
	allConnections = "connections/*"
	allJobs        = "jobs/*"
)

func (a AccountAction) String() string { return string(a) }
func (AccountAction) Kind() string     { return "account" }

// The account an action is done on is the account it is asked in: nobody can ask, within one
// account, about another.
func (AccountAction) object(account Account) string { return account.stored() }

func (a ConnectionAction) String() string      { return string(a) }
func (ConnectionAction) Kind() string          { return "connection" }
func (ConnectionAction) object(Account) string { return allConnections }
func (a JobAction) String() string             { return string(a) }
func (JobAction) Kind() string                 { return "job" }
func (JobAction) object(Account) string        { return allJobs }

// Actions is every action there is, kind by kind.
func Actions() []Action {
	return []Action{
		AccountAction_View, AccountAction_Edit, AccountAction_Create, AccountAction_Delete,
		ConnectionAction_View, ConnectionAction_ViewSensitive, ConnectionAction_Create,
		ConnectionAction_Edit, ConnectionAction_Delete,
		JobAction_View, JobAction_Execute, JobAction_Create, JobAction_Edit, JobAction_Delete,
	}
}
