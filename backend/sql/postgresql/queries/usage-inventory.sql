-- What the instance holds, read across every account for the usage report of the instance.
--
-- These queries select identifiers, stored configurations and counts. None of them selects how
-- a connection, a job or an account is called, an account's slug or a user's email: what is not
-- selected cannot reach the report.

-- The stored JSON is handed over as it is, not as the Go models: a job whose JSON cannot be
-- decoded is then left out on its own instead of failing the whole list.
-- name: ListJobsOfInstanceForUsage :many
SELECT
  j.id,
  j.account_id,
  j.connection_options::jsonb AS connection_options,
  j.jobtype_config::jsonb AS jobtype_config,
  j.cron_schedule,
  j.mappings::jsonb AS mappings
FROM husonym_api.jobs j
ORDER BY j.id;

-- As for the jobs, the configuration is handed over as stored.
-- name: ListConnectionsOfInstance :many
SELECT
  c.id,
  c.connection_config::jsonb AS connection_config
FROM husonym_api.connections c
ORDER BY c.id;

-- name: ListJobDestinationsOfInstance :many
SELECT jdca.job_id, jdca.connection_id
FROM husonym_api.job_destination_connection_associations jdca
ORDER BY jdca.job_id, jdca.connection_id;

-- The types of the columns the runs saw, counted by type. The schema, the table and the column
-- are not selected. The columns of the jobs given are not counted: they are the jobs the caller
-- could not read, which it leaves out of every count.
-- name: CountSourceColumnTypesOfInstance :many
SELECT jsc.data_type, count(*)::bigint AS columns
FROM husonym_api.job_source_columns jsc
WHERE NOT (jsc.job_id = ANY(sqlc.arg('excludedJobIds')::uuid[]))
GROUP BY jsc.data_type
ORDER BY jsc.data_type;

-- People only: the user of an API key is not counted.
-- name: CountUsersOfInstance :one
SELECT count(*)::bigint
FROM husonym_api.users
WHERE user_type = 0;

-- name: CountAccounts :one
SELECT count(*)::bigint
FROM husonym_api.accounts;

-- name: CountUserDefinedTransformersOfInstance :one
SELECT count(*)::bigint
FROM husonym_api.transformers;

-- The accounts that declared an identity provider of their own. The provider is not read.
-- name: CountAccountOidcProviders :one
SELECT count(*)::bigint
FROM husonym_api.account_settings
WHERE setting_type = 'oidc_provider';
