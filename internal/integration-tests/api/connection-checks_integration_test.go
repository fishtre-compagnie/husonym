package integrationtests_test

import (
	"fmt"
	"net/url"
	"strings"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// A connection tested in its role is told what the role needs and it cannot do, with the
// statement that grants it: what a run would stop on at its start, found before it.
func (s *IntegrationTestSuite) Test_CheckConnectionConfigById_Role() {
	t := s.T()
	clients := s.OSSUnauthenticatedLicensedClients
	accountId := s.createPersonalAccount(s.ctx, clients.Users())

	// An account that may read one table, and nothing more.
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	schema, role := "checks_"+suffix, "reader_"+suffix
	for _, statement := range []string{
		fmt.Sprintf("CREATE SCHEMA %s", schema),
		fmt.Sprintf("CREATE TABLE %s.orders (id int PRIMARY KEY, note text)", schema),
		fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD 'reader'", role),
		fmt.Sprintf("GRANT USAGE ON SCHEMA %s TO %s", schema, role),
		fmt.Sprintf("GRANT SELECT ON %s.orders TO %s", schema, role),
	} {
		_, err := s.Pgcontainer.DB.Exec(s.ctx, statement)
		require.NoError(t, err, statement)
	}
	t.Cleanup(func() {
		_, _ = s.Pgcontainer.DB.Exec(s.ctx, fmt.Sprintf("DROP SCHEMA %s CASCADE", schema))
		_, _ = s.Pgcontainer.DB.Exec(s.ctx, fmt.Sprintf("DROP OWNED BY %s; DROP ROLE %s", role, role))
	})
	readerURL, err := url.Parse(s.Pgcontainer.URL)
	require.NoError(t, err)
	readerURL.User = url.UserPassword(role, "reader")
	conn := s.createPostgresConnection(clients.Connections(), accountId, "reporting", readerURL.String())

	check := func(scope *mgmtv1alpha1.ConnectionCheckScope) *mgmtv1alpha1.CheckConnectionConfigByIdResponse {
		t.Helper()
		resp, err := clients.Connections().CheckConnectionConfigById(s.ctx, connect.NewRequest(
			&mgmtv1alpha1.CheckConnectionConfigByIdRequest{Id: conn.GetId(), Scope: scope},
		))
		requireNoErrResp(t, resp, err)
		require.True(t, resp.Msg.GetIsConnected())
		return resp.Msg
	}
	orders := []*mgmtv1alpha1.ConnectionCheckTable{{Schema: schema, Table: "orders"}}

	// The server's own tables are no job's: asking about them is refused, and no grant on
	// them is ever written.
	_, err = clients.Connections().CheckConnectionConfigById(s.ctx, connect.NewRequest(
		&mgmtv1alpha1.CheckConnectionConfigByIdRequest{Id: conn.GetId(), Scope: &mgmtv1alpha1.ConnectionCheckScope{
			Role:   mgmtv1alpha1.ConnectionRole_CONNECTION_ROLE_DESTINATION,
			Tables: []*mgmtv1alpha1.ConnectionCheckTable{{Schema: "pg_catalog", Table: "pg_authid"}},
		}},
	))
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	// Without a scope, the answer is what it always was.
	require.Empty(t, check(nil).GetChecks())

	// Read, the table is fine.
	require.Empty(t, check(&mgmtv1alpha1.ConnectionCheckScope{
		Role: mgmtv1alpha1.ConnectionRole_CONNECTION_ROLE_SOURCE, Tables: orders,
	}).GetChecks())

	// Written by Benthos, it lacks what a run writes with, and is told how to get it.
	written := check(&mgmtv1alpha1.ConnectionCheckScope{
		Role:   mgmtv1alpha1.ConnectionRole_CONNECTION_ROLE_DESTINATION,
		Engine: mgmtv1alpha1.JobEngine_JOB_ENGINE_BENTHOS,
		Tables: orders,
	}).GetChecks()
	require.Len(t, written, 1)
	require.Equal(t, mgmtv1alpha1.ConnectionCheck_KIND_WRITABLE, written[0].GetKind())
	require.Equal(t, mgmtv1alpha1.ConnectionCheck_LEVEL_BLOCKING, written[0].GetLevel())
	require.Equal(t, schema+".orders", written[0].GetTable())
	require.Equal(t, []string{"DELETE", "INSERT", "UPDATE"}, written[0].GetMissing())
	require.Contains(t, written[0].GetMessage(), `destination "reporting" cannot write`)
	require.Equal(t,
		fmt.Sprintf(`GRANT DELETE, INSERT, UPDATE ON TABLE "%s"."orders" TO "%s";`, schema, role),
		written[0].GetRemedy())

	// The remedy, run as is, is enough.
	_, err = s.Pgcontainer.DB.Exec(s.ctx, written[0].GetRemedy())
	require.NoError(t, err)
	require.Empty(t, check(&mgmtv1alpha1.ConnectionCheckScope{
		Role:   mgmtv1alpha1.ConnectionRole_CONNECTION_ROLE_DESTINATION,
		Engine: mgmtv1alpha1.JobEngine_JOB_ENGINE_BENTHOS,
		Tables: orders,
	}).GetChecks())

	// Suspending foreign keys is Athanor's: required of it, a warning when the engine is the
	// deployment's own, which the API does not know. The server as a whole is checked
	// without tables.
	suspension := func(engine mgmtv1alpha1.JobEngine) []*mgmtv1alpha1.ConnectionCheck {
		return check(&mgmtv1alpha1.ConnectionCheckScope{
			Role: mgmtv1alpha1.ConnectionRole_CONNECTION_ROLE_DESTINATION, Engine: engine,
		}).GetChecks()
	}
	athanor := suspension(mgmtv1alpha1.JobEngine_JOB_ENGINE_ATHANOR)
	require.Len(t, athanor, 1)
	require.Equal(t, mgmtv1alpha1.ConnectionCheck_KIND_FOREIGN_KEY_SUSPENSION, athanor[0].GetKind())
	require.Equal(t, mgmtv1alpha1.ConnectionCheck_LEVEL_BLOCKING, athanor[0].GetLevel())
	unknown := suspension(mgmtv1alpha1.JobEngine_JOB_ENGINE_UNSPECIFIED)
	require.Len(t, unknown, 1)
	require.Equal(t, mgmtv1alpha1.ConnectionCheck_LEVEL_WARNING, unknown[0].GetLevel())
	require.Empty(t, suspension(mgmtv1alpha1.JobEngine_JOB_ENGINE_BENTHOS))
}
