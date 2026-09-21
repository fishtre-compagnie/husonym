-- name: InsertJobMappingChange :exec
INSERT INTO husonym_api.job_mapping_changes (
  account_id, job_id, job_run_id, table_schema, table_name, column_name,
  kind, transformer, data_type, previous_data_type, pii_category
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
);

-- name: GetPendingJobMappingChangesByAccount :many
SELECT * FROM husonym_api.job_mapping_changes
WHERE account_id = sqlc.arg('accountId') AND reviewed_at IS NULL
ORDER BY created_at, table_schema, table_name, column_name;

-- name: GetPendingJobMappingChangesByJob :many
SELECT * FROM husonym_api.job_mapping_changes
WHERE account_id = sqlc.arg('accountId') AND job_id = sqlc.arg('jobId') AND reviewed_at IS NULL
ORDER BY created_at, table_schema, table_name, column_name;

-- Only pending changes of the job: an id of another job, or one already reviewed, is left alone.
-- name: ReviewJobMappingChanges :many
UPDATE husonym_api.job_mapping_changes
SET reviewed_at = CURRENT_TIMESTAMP,
    reviewed_by_id = sqlc.arg('reviewedById'),
    note = sqlc.narg('note')
WHERE job_id = sqlc.arg('jobId')
  AND id = ANY(sqlc.arg('ids')::uuid[])
  AND reviewed_at IS NULL
RETURNING id;

-- name: GetJobSourceColumns :many
SELECT * FROM husonym_api.job_source_columns WHERE job_id = $1;

-- name: DeleteJobSourceColumns :exec
DELETE FROM husonym_api.job_source_columns WHERE job_id = $1;

-- name: InsertJobSourceColumns :exec
INSERT INTO husonym_api.job_source_columns (job_id, table_schema, table_name, column_name, data_type)
SELECT
  sqlc.arg('jobId')::uuid,
  unnest(sqlc.arg('schemas')::text[]),
  unnest(sqlc.arg('tables')::text[]),
  unnest(sqlc.arg('columns')::text[]),
  unnest(sqlc.arg('dataTypes')::text[]);
