package integrationtests_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	integrationtests_test "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const modulePath = "github.com/fishtre-compagnie/husonym/"

// gateProbes is the ground the probes of Test_EveryFeatureHasAGate share: an account for what
// belongs to a job and a team account for roles.
type gateProbes struct {
	s            *IntegrationTestSuite
	jobs         *gateGround
	jobId        string
	accountHooks mgmtv1alpha1connect.AccountHookServiceClient
	roles        *rolesGround
	// memberId is a member of the roles account who is not its administrator.
	memberId string
}

// gateEntry says where the gate of a feature is shown: by a probe, a call the suite makes under a
// license that includes every other feature, or by the test that proves it where the suite
// cannot reach it.
type gateEntry struct {
	// probe makes one call that configures or uses the feature, and returns the error it got.
	probe func(p *gateProbes) error
	// serving arms what the orchestrator mock must answer when the call is served, not refused.
	serving func(p *gateProbes)
	// referencePackage and referenceTest name, by import path and function, the test that proves
	// the gate of a feature the probes cannot reach; why says why they cannot.
	referencePackage string
	referenceTest    string
	why              string
}

func (p *gateProbes) createJobHook() error {
	_, err := p.jobs.jobs.CreateJobHook(p.s.ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobHookRequest{
		JobId: p.jobId,
		Hook: &mgmtv1alpha1.NewJobHook{
			Name: "hook-" + uuid.NewString(),
			Config: &mgmtv1alpha1.JobHookConfig{Config: &mgmtv1alpha1.JobHookConfig_Sql{
				Sql: &mgmtv1alpha1.JobHookConfig_JobSqlHook{
					Query:        "select 1;",
					ConnectionId: p.jobs.source.GetId(),
					Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing{
						Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing_PreSync{},
					},
				},
			}},
		},
	}))
	return err
}

func (p *gateProbes) createAccountHook() error {
	_, err := p.accountHooks.CreateAccountHook(p.s.ctx, connect.NewRequest(&mgmtv1alpha1.CreateAccountHookRequest{
		AccountId: p.jobs.accountId,
		Hook: &mgmtv1alpha1.NewAccountHook{
			Name:   "hook-" + uuid.NewString(),
			Events: []mgmtv1alpha1.AccountHookEvent{mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED},
			Config: &mgmtv1alpha1.AccountHookConfig{Config: &mgmtv1alpha1.AccountHookConfig_Webhook{
				Webhook: &mgmtv1alpha1.AccountHookConfig_WebHook{Url: "https://example.com", Secret: "foo"},
			}},
		},
	}))
	return err
}

func (p *gateProbes) createTransformer(
	g *transformersGround,
	source mgmtv1alpha1.TransformerSource,
	config *mgmtv1alpha1.TransformerConfig,
) error {
	_, err := g.create(p.s, "transformer-"+uuid.NewString(), source, config)
	return err
}

// Every feature the license declares has a gate, and this test is what says so: a name added to
// license.AllFeatures() without an entry here fails it. An entry is a probe, one call that is
// served by a license that includes everything and refused, naming the feature, by one that
// includes everything else; or a reference to the test of a gate the API suite cannot reach, which
// must exist.
func (s *IntegrationTestSuite) Test_EveryFeatureHasAGate() {
	t := s.T()
	p := s.newGateProbes()
	transformers := s.newTransformersGround("every-feature-gate-transformers")
	javascript := mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_JAVASCRIPT
	piiText := mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_PII_TEXT
	viewer := mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_VIEWER
	where := "id > 10"
	cron := everyNight

	gates := map[license.Feature]gateEntry{
		license.FeatureJobHooks:     {probe: func(p *gateProbes) error { return p.createJobHook() }},
		license.FeatureAccountHooks: {probe: func(p *gateProbes) error { return p.createAccountHook() }},
		license.FeaturePiiText: {probe: func(p *gateProbes) error {
			return p.createTransformer(transformers, piiText, piiTextTransformerConfig())
		}},
		license.FeaturePiiDetection: {
			probe: func(p *gateProbes) error {
				req := p.jobs.jobRequest("pii-detect-" + uuid.NewString())
				req.JobType = piiDetectJobType()
				_, err := p.jobs.jobs.CreateJob(s.ctx, connect.NewRequest(req))
				return err
			},
			serving: func(p *gateProbes) { s.MockTemporalForCreateJob("pii-detect") },
		},
		license.FeatureCustomTransformers: {probe: func(p *gateProbes) error {
			return p.createTransformer(transformers, javascript, javascriptConfig(`return "x";`))
		}},
		license.FeatureSubsetting: {probe: func(p *gateProbes) error {
			_, err := p.jobs.jobs.SetJobSourceSqlConnectionSubsets(s.ctx, connect.NewRequest(
				&mgmtv1alpha1.SetJobSourceSqlConnectionSubsetsRequest{Id: p.jobId, Schemas: postgresSubsets(&where)},
			))
			return err
		}},
		license.FeatureScheduling: {
			probe: func(p *gateProbes) error {
				_, err := p.jobs.jobs.UpdateJobSchedule(s.ctx, connect.NewRequest(&mgmtv1alpha1.UpdateJobScheduleRequest{
					Id: p.jobId, CronSchedule: &cron,
				}))
				return err
			},
			serving: func(p *gateProbes) {
				s.Mocks.TemporalClientManager.EXPECT().
					UpdateSchedule(mock.Anything, p.jobs.accountId, p.jobId, mock.Anything, mock.Anything).
					Return(nil).Once()
			},
		},
		license.FeatureMappingReview: {probe: func(p *gateProbes) error {
			_, err := p.jobs.jobs.ReviewMappingChanges(s.ctx, connect.NewRequest(&mgmtv1alpha1.ReviewMappingChangesRequest{
				AccountId: p.jobs.accountId, JobId: p.jobId,
			}))
			return err
		}},
		license.FeatureRbac: {probe: func(p *gateProbes) error {
			return p.roles.setRole(s, p.memberId, viewer)
		}},
		license.FeatureSso: {probe: func(*gateProbes) error {
			// The gate is on the first provider of an account, so each call declares one for
			// an account that has none: the served call must not open the way to the next.
			g := s.newSsoGround("every-feature-gate-sso-" + uuid.NewString())
			return g.set(s, oidcSetting(publicIssuer(), "a-client"))
		}},
		license.FeatureApiKeys: {
			referencePackage: modulePath + "backend/services/mgmt/v1alpha1/api-key-service",
			referenceTest:    "Test_Service_CreateAccountApiKey_NeedsTheApiKeysFeature",
			why:              "the integration mux serves no API-key service",
		},
		license.FeatureRunLogs: {
			referencePackage: modulePath + "backend/services/mgmt/v1alpha1/job-service",
			referenceTest:    "Test_JobRunLogs_NeedTheRunLogsFeature",
			why:              "the gate comes after the run is found and needs a log source, which the integration mux does not configure",
		},
		license.FeatureMcp: {
			referencePackage: modulePath + "cli/internal/mcp",
			referenceTest:    "Test_ToolCalls_NeedTheMcpFeature",
			why:              "the gate sits in the CLI's MCP server, which the API suite does not run",
		},
	}

	t.Run("every declared feature has an entry and every entry a declared feature", func(t *testing.T) {
		var declared, entered []string
		for _, feature := range license.AllFeatures() {
			declared = append(declared, string(feature))
		}
		for feature := range gates {
			entered = append(entered, string(feature))
		}
		require.ElementsMatch(t, declared, entered)
	})

	for _, feature := range license.AllFeatures() {
		entry, ok := gates[feature]
		if !ok {
			continue
		}
		t.Run(string(feature), func(t *testing.T) {
			if entry.probe == nil {
				requireTestExists(t, entry.referencePackage, entry.referenceTest)
				require.NotEmpty(t, entry.why, "a reference says why the suite cannot reach the gate")
				return
			}

			// Served: the license includes every feature, as a key that names no feature list does.
			s.Mocks.ExpiringLicense.ClearFeatures()
			if entry.serving != nil {
				entry.serving(p)
			}
			require.NoError(t, entry.probe(p), "the call is served when the license includes the feature")

			// Refused, naming the feature: the license includes every other one.
			s.closeFeature(feature)
			err := entry.probe(p)
			require.Error(t, err)
			require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "%v", err)
			require.ErrorContains(t, err, "license does not include")
			require.ErrorContains(t, err, string(feature))
		})
	}
}

// newGateProbes makes the ground of the probes, and leaves the license as a key without a feature
// list when the test ends.
func (s *IntegrationTestSuite) newGateProbes() *gateProbes {
	p := &gateProbes{s: s}
	p.jobs = s.newGateGround("every-feature-gate")
	p.jobId = s.createJobUnderValidLicense(s.T(), p.jobs.jobs, p.jobs.jobRequest("every-feature-gate-job")).GetId()
	p.accountHooks = s.OSSAuthenticatedExpiringClients.AccountHooks(integrationtests_test.WithUserId("every-feature-gate"))
	p.roles = s.newRolesGround("every-feature-gate-roles")
	member := p.roles.member(s, "every-feature-gate-member", mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_UNSPECIFIED)
	p.memberId = memberIdOf(s, member)
	return p
}

// requireTestExists checks that the package at the import path, which must belong to this module,
// has a test function or method of that name, so that a reference cannot go stale unnoticed.
func requireTestExists(t testing.TB, importPath, name string) {
	t.Helper()
	require.True(t, strings.HasPrefix(importPath, modulePath), "%s is not a package of this module", importPath)

	root, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		require.NotEqual(t, root, parent, "no go.mod above the test's directory")
		root = parent
	}

	dir := filepath.Join(root, strings.TrimPrefix(importPath, modulePath))
	files, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	require.NoError(t, err)
	require.NotEmpty(t, files, "no test file in %s", dir)

	for _, file := range files {
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		require.NoError(t, err)
		if slices.ContainsFunc(parsed.Decls, func(decl ast.Decl) bool {
			fn, ok := decl.(*ast.FuncDecl)
			return ok && fn.Name.Name == name
		}) {
			return
		}
	}
	require.Failf(t, "the referenced test does not exist", "%s in %s", name, importPath)
}
