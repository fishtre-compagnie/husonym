-- name: UpsertUnmappedPassthrough :exec
INSERT INTO husonym_api.unmapped_passthroughs (
    account_id,
    job_id,
    table_schema,
    table_name,
    column_name,
    data_type,
    last_seen_job_run_id
)
VALUES (
    $1,  -- account_id
    $2,  -- job_id
    $3,  -- table_schema
    $4,  -- table_name
    $5,  -- column_name
    $6,  -- data_type
    $7   -- last_seen_job_run_id
)
ON CONFLICT (job_id, table_schema, table_name, column_name)
DO UPDATE SET
    data_type = EXCLUDED.data_type,
    last_seen_at = CURRENT_TIMESTAMP,
    last_seen_job_run_id = EXCLUDED.last_seen_job_run_id;

-- name: DeleteUnmappedPassthroughsNotSeenInRun :exec
DELETE FROM husonym_api.unmapped_passthroughs
WHERE job_id = sqlc.arg('jobId')
  AND account_id = sqlc.arg('accountId')
  AND last_seen_job_run_id <> sqlc.arg('jobRunId')::text;

-- name: GetUnmappedPassthroughsByJob :many
SELECT * FROM husonym_api.unmapped_passthroughs
WHERE job_id = sqlc.arg('jobId')
  AND account_id = sqlc.arg('accountId')
ORDER BY table_schema, table_name, column_name;

-- name: GetUnmappedPassthroughsByAccount :many
SELECT * FROM husonym_api.unmapped_passthroughs
WHERE account_id = sqlc.arg('accountId')
ORDER BY job_id, table_schema, table_name, column_name;
