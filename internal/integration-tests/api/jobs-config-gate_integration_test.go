package integrationtests_test

import (
	"strings"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/stretchr/testify/require"
)

func javascriptMapping(column string) *mgmtv1alpha1.JobMapping {
	return columnMapping(column, &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_TransformJavascriptConfig{
			TransformJavascriptConfig: &mgmtv1alpha1.TransformJavascript{Code: "return value;"},
		},
	})
}

func generateBoolMapping(column string) *mgmtv1alpha1.JobMapping {
	return columnMapping(column, &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_GenerateBoolConfig{GenerateBoolConfig: &mgmtv1alpha1.GenerateBool{}},
	})
}

func piiDetectJobType() *mgmtv1alpha1.JobTypeConfig {
	return &mgmtv1alpha1.JobTypeConfig{JobType: &mgmtv1alpha1.JobTypeConfig_PiiDetect{
		PiiDetect: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect{
			TableScanFilter: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TableScanFilter{
				Mode: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TableScanFilter_IncludeAll{},
			},
		},
	}}
}

func syncJobType() *mgmtv1alpha1.JobTypeConfig {
	return &mgmtv1alpha1.JobTypeConfig{JobType: &mgmtv1alpha1.JobTypeConfig_Sync{
		Sync: &mgmtv1alpha1.JobTypeConfig_JobTypeSync{},
	}}
}

// storedJob reads the job again, as it is stored.
func (s *IntegrationTestSuite) storedJob(g *gateGround, jobId string) *mgmtv1alpha1.Job {
	resp, err := g.jobs.GetJob(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetJobRequest{Id: jobId}))
	requireNoErrResp(s.T(), resp, err)
	return resp.Msg.GetJob()
}

// transformerOf tells which transformer the job gives a column, by the name of its configuration.
func transformerOf(t testing.TB, job *mgmtv1alpha1.Job, column string) string {
	t.Helper()
	for _, mapping := range job.GetMappings() {
		if mapping.GetColumn() == column {
			config := mapping.GetTransformer().GetConfig().ProtoReflect()
			return string(config.WhichOneof(config.Descriptor().Oneofs().ByName("config")).Name())
		}
	}
	require.Failf(t, "the job does not map the column", "column %s", column)
	return ""
}

// whereClausesOf lists the WHERE clauses of the job's tables, trimmed, the empty ones left out.
func whereClausesOf(job *mgmtv1alpha1.Job) []string {
	clauses := []string{}
	for _, schema := range job.GetSource().GetOptions().GetPostgres().GetSchemas() {
		for _, table := range schema.GetTables() {
			if clause := strings.TrimSpace(table.GetWhereClause()); clause != "" {
				clauses = append(clauses, clause)
			}
		}
	}
	return clauses
}

// A job that uses a feature the license does not include is not created, and the person is told
// which feature. The same job is created by a license that includes that feature and no other.
func (s *IntegrationTestSuite) Test_CreateJob_RefusesAJobUsingAClosedFeature() {
	t := s.T()
	ctx := s.ctx
	g := s.newGateGround("create-closed-feature")

	for _, tc := range []struct {
		feature license.Feature
		use     func(req *mgmtv1alpha1.CreateJobRequest)
	}{
		{license.FeatureSubsetting, func(req *mgmtv1alpha1.CreateJobRequest) {
			req.Source = g.subsettingJobRequest("").GetSource()
		}},
		{license.FeatureCustomTransformers, func(req *mgmtv1alpha1.CreateJobRequest) {
			req.Mappings = []*mgmtv1alpha1.JobMapping{javascriptMapping("name")}
		}},
		{license.FeaturePiiDetection, func(req *mgmtv1alpha1.CreateJobRequest) {
			req.JobType = piiDetectJobType()
		}},
	} {
		name := string(tc.feature)
		t.Run(name, func(t *testing.T) {
			req := g.jobRequest("create-closed-" + name)
			tc.use(req)

			s.closeFeature(tc.feature)
			// No schedule is expected of the orchestrator: a refusal that came after asking it
			// for one would fail the test.
			_, err := g.jobs.CreateJob(ctx, connect.NewRequest(req))
			requireJobRefusal(t, err, name)

			s.Mocks.ExpiringLicense.SetFeatures(tc.feature)
			s.createJobUnderValidLicense(t, g.jobs, req)
		})
	}

	// What was refused was not written: the account has the three jobs that were created.
	stored, err := g.jobs.GetJobs(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobsRequest{AccountId: g.accountId}))
	requireNoErrResp(t, stored, err)
	require.Len(t, stored.Msg.GetJobs(), 3)
}

// A job that uses no licensed feature is created whatever the features of the license.
func (s *IntegrationTestSuite) Test_CreateJob_AJobUsingNoFeatureNeedsNone() {
	g := s.newGateGround("create-no-feature")
	s.Mocks.ExpiringLicense.SetFeatures()

	job := s.createJobUnderValidLicense(s.T(), g.jobs, g.jobRequest("create-no-feature-job"))
	require.NotEmpty(s.T(), job.GetId())
}

// Changing the source and the mappings of a job is judged on the job as the change leaves it: one
// that keeps a feature the license lacks is refused, one that gives it up is accepted.
func (s *IntegrationTestSuite) Test_UpdateJobSourceConnection_GatesTheJobAsItWouldBe() {
	t := s.T()
	ctx := s.ctx
	g := s.newGateGround("update-closed-feature")

	update := func(jobId string, req *mgmtv1alpha1.UpdateJobSourceConnectionRequest) error {
		req.Id = jobId
		if req.GetSource() == nil {
			req.Source = g.sourceWhere(nil)
		}
		if req.GetMappings() == nil {
			req.Mappings = []*mgmtv1alpha1.JobMapping{passthroughMapping("name")}
		}
		_, err := g.jobs.UpdateJobSourceConnection(ctx, connect.NewRequest(req))
		return err
	}

	t.Run("mappings", func(t *testing.T) {
		s.Mocks.ExpiringLicense.ClearFeatures()
		req := g.jobRequest("update-closed-mappings")
		req.Mappings = []*mgmtv1alpha1.JobMapping{javascriptMapping("name")}
		jobId := s.createJobUnderValidLicense(t, g.jobs, req).GetId()
		s.closeFeature(license.FeatureCustomTransformers)

		err := update(jobId, &mgmtv1alpha1.UpdateJobSourceConnectionRequest{
			Mappings: []*mgmtv1alpha1.JobMapping{javascriptMapping("name")},
		})
		requireJobRefusal(t, err, "custom_transformers")

		require.NoError(t, update(jobId, &mgmtv1alpha1.UpdateJobSourceConnectionRequest{}))
		require.Equal(t, "passthrough_config", transformerOf(t, s.storedJob(g, jobId), "name"))

		err = update(jobId, &mgmtv1alpha1.UpdateJobSourceConnectionRequest{
			Mappings: []*mgmtv1alpha1.JobMapping{javascriptMapping("name")},
		})
		requireJobRefusal(t, err, "custom_transformers")
		require.Equal(t, "passthrough_config", transformerOf(t, s.storedJob(g, jobId), "name"),
			"a refused change writes nothing")
	})

	t.Run("source", func(t *testing.T) {
		s.Mocks.ExpiringLicense.ClearFeatures()
		jobId := s.createJobUnderValidLicense(t, g.jobs, g.subsettingJobRequest("update-closed-source")).GetId()
		s.closeFeature(license.FeatureSubsetting)

		err := update(jobId, &mgmtv1alpha1.UpdateJobSourceConnectionRequest{
			Source: g.subsettingJobRequest("").GetSource(),
		})
		requireJobRefusal(t, err, "subsetting")
		require.NotEmpty(t, whereClausesOf(s.storedJob(g, jobId)))

		require.NoError(t, update(jobId, &mgmtv1alpha1.UpdateJobSourceConnectionRequest{}))
		require.Empty(t, whereClausesOf(s.storedJob(g, jobId)))
	})

	t.Run("job type", func(t *testing.T) {
		s.Mocks.ExpiringLicense.ClearFeatures()
		jobId := s.createJobUnderValidLicense(t, g.jobs, g.jobRequest("update-closed-job-type")).GetId()
		s.closeFeature(license.FeaturePiiDetection)

		err := update(jobId, &mgmtv1alpha1.UpdateJobSourceConnectionRequest{JobType: piiDetectJobType()})
		requireJobRefusal(t, err, "pii_detection")
		require.Nil(t, s.storedJob(g, jobId).GetJobType().GetPiiDetect())

		// The type the job already has counts when the change names none.
		s.Mocks.ExpiringLicense.ClearFeatures()
		require.NoError(t, update(jobId, &mgmtv1alpha1.UpdateJobSourceConnectionRequest{JobType: piiDetectJobType()}))
		s.closeFeature(license.FeaturePiiDetection)
		requireJobRefusal(t, update(jobId, &mgmtv1alpha1.UpdateJobSourceConnectionRequest{}), "pii_detection")

		require.NoError(t, update(jobId, &mgmtv1alpha1.UpdateJobSourceConnectionRequest{JobType: syncJobType()}))
		require.Nil(t, s.storedJob(g, jobId).GetJobType().GetPiiDetect())
	})
}

// A hook that is enabled is part of what the job uses: under a license without hooks, the job
// cannot be changed while it keeps one, and can be again once the hook is turned off, which
// needs no feature.
func (s *IntegrationTestSuite) Test_UpdateJobSourceConnection_AnEnabledHookCountsUntilItIsTurnedOff() {
	t := s.T()
	ctx := s.ctx
	g := s.newGateGround("update-closed-hooks")
	jobId := s.createJobUnderValidLicense(t, g.jobs, g.jobRequest("update-closed-hooks-job")).GetId()
	hook := s.createSqlJobHook(
		ctx, t, g.jobs, "update-closed-hooks-hook", jobId, g.source.GetId(), true,
		&mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing{
			Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing_PreSync{},
		},
	)
	update := func() error {
		_, err := g.jobs.UpdateJobSourceConnection(ctx, connect.NewRequest(&mgmtv1alpha1.UpdateJobSourceConnectionRequest{
			Id:       jobId,
			Source:   g.sourceWhere(nil),
			Mappings: []*mgmtv1alpha1.JobMapping{generateBoolMapping("name")},
		}))
		return err
	}

	s.closeFeature(license.FeatureJobHooks)
	requireJobRefusal(t, update(), "job_hooks")
	require.Equal(t, "passthrough_config", transformerOf(t, s.storedJob(g, jobId), "name"),
		"a refused change writes nothing")

	off, err := g.jobs.SetJobHookEnabled(ctx, connect.NewRequest(&mgmtv1alpha1.SetJobHookEnabledRequest{
		Id: hook.GetId(), Enabled: false,
	}))
	requireNoErrResp(t, off, err)
	require.False(t, off.Msg.GetHook().GetEnabled())

	require.NoError(t, update())
	require.Equal(t, "generate_bool_config", transformerOf(t, s.storedJob(g, jobId), "name"))
}

// Setting transformers from the review is judged on the mappings as they will be, the columns the
// request does not name included.
func (s *IntegrationTestSuite) Test_ApplyMappingChanges_GatesTheMappingsAsTheyWouldBe() {
	t := s.T()
	ctx := s.ctx
	g := s.newGateGround("apply-closed-feature")
	req := g.jobRequest("apply-closed-feature-job")
	req.Mappings = []*mgmtv1alpha1.JobMapping{passthroughMapping("name"), javascriptMapping("email")}
	jobId := s.createJobUnderValidLicense(t, g.jobs, req).GetId()

	apply := func(mapping *mgmtv1alpha1.JobMapping) error {
		_, err := g.jobs.ApplyMappingChanges(ctx, connect.NewRequest(&mgmtv1alpha1.ApplyMappingChangesRequest{
			AccountId: g.accountId,
			JobId:     jobId,
			Mappings:  []*mgmtv1alpha1.JobMapping{mapping},
		}))
		return err
	}

	s.closeFeature(license.FeatureCustomTransformers)

	// The column the request leaves alone still runs JavaScript.
	requireJobRefusal(t, apply(generateBoolMapping("name")), "custom_transformers")
	require.Equal(t, "passthrough_config", transformerOf(t, s.storedJob(g, jobId), "name"),
		"a refused change writes nothing")

	require.NoError(t, apply(passthroughMapping("email")))
	require.Equal(t, "passthrough_config", transformerOf(t, s.storedJob(g, jobId), "email"))

	requireJobRefusal(t, apply(javascriptMapping("name")), "custom_transformers")
	require.Equal(t, "passthrough_config", transformerOf(t, s.storedJob(g, jobId), "name"))

	require.NoError(t, apply(generateBoolMapping("name")))
	require.Equal(t, "generate_bool_config", transformerOf(t, s.storedJob(g, jobId), "name"))
}

// Reviewing what runs changed in the mappings, and correcting it, is a feature of the license.
// Reading what is waiting for a review is not.
func (s *IntegrationTestSuite) Test_MappingReview_NeedsTheFeature() {
	t := s.T()
	ctx := s.ctx
	g := s.newGateGround("mapping-review-feature")
	jobId := s.createJobUnderValidLicense(t, g.jobs, g.jobRequest("mapping-review-feature-job")).GetId()

	review := func() error {
		_, err := g.jobs.ReviewMappingChanges(ctx, connect.NewRequest(&mgmtv1alpha1.ReviewMappingChangesRequest{
			AccountId: g.accountId, JobId: jobId,
		}))
		return err
	}
	apply := func() error {
		_, err := g.jobs.ApplyMappingChanges(ctx, connect.NewRequest(&mgmtv1alpha1.ApplyMappingChangesRequest{
			AccountId: g.accountId,
			JobId:     jobId,
			Mappings:  []*mgmtv1alpha1.JobMapping{generateBoolMapping("name")},
		}))
		return err
	}

	s.closeFeature(license.FeatureMappingReview)
	requireFeatureRefusal(t, review(), license.FeatureMappingReview)
	requireFeatureRefusal(t, apply(), license.FeatureMappingReview)
	require.Equal(t, "passthrough_config", transformerOf(t, s.storedJob(g, jobId), "name"),
		"a refused change writes nothing")

	pending, err := g.jobs.GetPendingMappingChanges(ctx, connect.NewRequest(&mgmtv1alpha1.GetPendingMappingChangesRequest{
		AccountId: g.accountId, JobId: &jobId,
	}))
	requireNoErrResp(t, pending, err)

	s.Mocks.ExpiringLicense.SetFeatures(license.FeatureMappingReview)
	require.NoError(t, review())
	require.NoError(t, apply())
	require.Equal(t, "generate_bool_config", transformerOf(t, s.storedJob(g, jobId), "name"))
}

func postgresSubsets(where *string) *mgmtv1alpha1.JobSourceSqlSubetSchemas {
	return &mgmtv1alpha1.JobSourceSqlSubetSchemas{Schemas: &mgmtv1alpha1.JobSourceSqlSubetSchemas_PostgresSubset{
		PostgresSubset: &mgmtv1alpha1.PostgresSourceSchemaSubset{
			PostgresSchemas: []*mgmtv1alpha1.PostgresSourceSchemaOption{{
				Schema: "public",
				Tables: []*mgmtv1alpha1.PostgresSourceTableOption{{Table: "users", WhereClause: where}},
			}},
		},
	}}
}

// Giving a table a WHERE clause is subsetting, a feature of the license.
func (s *IntegrationTestSuite) Test_SetSubsets_NeedsTheFeature() {
	t := s.T()
	ctx := s.ctx
	g := s.newGateGround("subsets-feature")
	jobId := s.createJobUnderValidLicense(t, g.jobs, g.jobRequest("subsets-feature-job")).GetId()
	where := "id > 10"
	setSubsets := func() error {
		_, err := g.jobs.SetJobSourceSqlConnectionSubsets(ctx, connect.NewRequest(
			&mgmtv1alpha1.SetJobSourceSqlConnectionSubsetsRequest{Id: jobId, Schemas: postgresSubsets(&where)},
		))
		return err
	}

	s.closeFeature(license.FeatureSubsetting)
	requireFeatureRefusal(t, setSubsets(), license.FeatureSubsetting)
	require.Empty(t, whereClausesOf(s.storedJob(g, jobId)), "a refused change writes nothing")

	s.Mocks.ExpiringLicense.SetFeatures(license.FeatureSubsetting)
	require.NoError(t, setSubsets())
	require.Equal(t, []string{where}, whereClausesOf(s.storedJob(g, jobId)))
}

// A job that subsets can always be brought back to one that does not: a request that carries no
// clause, or blank ones, needs no feature, and neither does following the foreign keys.
func (s *IntegrationTestSuite) Test_SetSubsets_ClearingIsAllowedWithoutTheFeature() {
	t := s.T()
	ctx := s.ctx
	g := s.newGateGround("subsets-clearing")
	jobId := s.createJobUnderValidLicense(t, g.jobs, g.subsettingJobRequest("subsets-clearing-job")).GetId()
	require.NotEmpty(t, whereClausesOf(s.storedJob(g, jobId)))

	s.Mocks.ExpiringLicense.SetFeatures()

	blank := "  "
	for name, where := range map[string]*string{"no clause": nil, "a blank clause": &blank} {
		t.Run(name, func(t *testing.T) {
			cleared, err := g.jobs.SetJobSourceSqlConnectionSubsets(ctx, connect.NewRequest(
				&mgmtv1alpha1.SetJobSourceSqlConnectionSubsetsRequest{
					Id:                            jobId,
					Schemas:                       postgresSubsets(where),
					SubsetByForeignKeyConstraints: true,
				},
			))
			requireNoErrResp(t, cleared, err)
			require.Empty(t, whereClausesOf(cleared.Msg.GetJob()))
		})
	}
}
