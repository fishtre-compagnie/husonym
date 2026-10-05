package rbac

// grant is one thing a role may do: an action on the objects of a kind.
type grant struct {
	role   string
	object string
	action string
}

// grants is what each role may do, in whatever account it is held. It is the whole of the
// permissions: nothing else is granted, and a member without a role may do nothing.
//
// The admin may do anything; the developer anything on jobs and connections, and view the
// account; the executor may view everything and execute jobs; the viewer may view everything.
// Viewing a connection is not viewing its secrets, which is an action of its own.
var grants = []grant{
	{roleAdmin, anything, anything},
	{roleDeveloper, allJobs, anything},
	{roleDeveloper, allConnections, anything},
	{roleDeveloper, allAccounts, string(AccountAction_View)},
	{roleExecutor, allJobs, string(JobAction_View)},
	{roleExecutor, allConnections, string(ConnectionAction_View)},
	{roleExecutor, allAccounts, string(AccountAction_View)},
	{roleExecutor, allJobs, string(JobAction_Execute)},
	{roleViewer, allJobs, string(JobAction_View)},
	{roleViewer, allConnections, string(ConnectionAction_View)},
	{roleViewer, allAccounts, string(AccountAction_View)},
}

// fixedRules gives the grants as the rules the enforcer decides with: role, account, object,
// action — the account being every account.
func fixedRules() [][]string {
	rules := make([][]string, 0, len(grants))
	for _, g := range grants {
		rules = append(rules, []string{g.role, anything, g.object, g.action})
	}
	return rules
}
