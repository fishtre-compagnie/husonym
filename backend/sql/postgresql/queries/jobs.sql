-- name: GetJobById :one
SELECT * from husonym_api.jobs WHERE id = $1;

-- name: GetJobByNameAndAccount :one
SELECT j.* from husonym_api.jobs j
INNER JOIN husonym_api.accounts a ON a.id = j.account_id
WHERE a.id = sqlc.arg('accountId') AND j.name = sqlc.arg('jobName');

-- name: GetJobsByAccount :many
SELECT j.* from husonym_api.jobs j
INNER JOIN husonym_api.accounts a ON a.id = j.account_id
WHERE a.id = sqlc.arg('accountId')
ORDER BY j.created_at DESC;

-- name: RemoveJobById :exec
DELETE FROM husonym_api.jobs WHERE id = $1;

-- name: CreateJob :one
INSERT INTO husonym_api.jobs (
  name, account_id, status, connection_options, mappings,
  cron_schedule, created_by_id, updated_by_id, workflow_options, sync_options,
  virtual_foreign_keys, jobtype_config
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
)
RETURNING *;

-- name: DeleteJob :exec
DELETE FROM husonym_api.jobs WHERE id = $1;

-- name: UpdateJobSchedule :one
UPDATE husonym_api.jobs
SET cron_schedule = $1,
updated_by_id = $2
WHERE id = $3
RETURNING *;

-- name: SetJobWorkflowOptions :one
UPDATE husonym_api.jobs
SET workflow_options = $1,
updated_by_id = $2
WHERE id = $3
RETURNING *;

-- name: SetJobSyncOptions :one
UPDATE husonym_api.jobs
SET sync_options = $1,
updated_by_id = $2
WHERE id = $3
RETURNING *;

-- name: UpdateJobMappings :one
UPDATE husonym_api.jobs
SET mappings = $1,
updated_by_id = $2
WHERE id = $3
RETURNING *;

-- name: UpdateJobSource :one
UPDATE husonym_api.jobs
SET connection_options = $1,
updated_by_id = $2
WHERE id = $3
RETURNING *;

-- name: UpdateJobVirtualForeignKeys :one
UPDATE husonym_api.jobs
SET virtual_foreign_keys = $1,
updated_by_id = $2
WHERE id = $3
RETURNING *;

-- name: UpdateJobTypeConfig :one
UPDATE husonym_api.jobs
SET jobtype_config = $1,
updated_by_id = $2
WHERE id = $3
RETURNING *;

-- name: IsJobNameAvailable :one
SELECT count(j.id) from husonym_api.jobs j
INNER JOIN husonym_api.accounts a ON a.id = j.account_id
WHERE a.id = sqlc.arg('accountId') AND j.name = sqlc.arg('jobName');

-- name: CreateJobConnectionDestination :one
INSERT INTO husonym_api.job_destination_connection_associations (
  job_id, connection_id, options
) VALUES (
  $1, $2, $3
)
ON CONFLICT(job_id, connection_id)
DO NOTHING
RETURNING *;

-- name: CreateJobConnectionDestinations :copyfrom
INSERT INTO husonym_api.job_destination_connection_associations (
  job_id, connection_id, options
) VALUES (
  $1, $2, $3
);

-- name: GetJobConnectionDestination :one
SELECT jdca.* from husonym_api.job_destination_connection_associations jdca
WHERE jdca.id = $1;

-- name: GetJobConnectionDestinations :many
SELECT jdca.* from husonym_api.job_destination_connection_associations jdca
INNER JOIN husonym_api.jobs j ON j.id = jdca.job_id
WHERE j.id = $1
ORDER BY jdca.created_at;

-- name: GetJobConnectionDestinationsByJobIds :many
SELECT jdca.* from husonym_api.job_destination_connection_associations jdca
INNER JOIN husonym_api.jobs j ON j.id = jdca.job_id
WHERE j.id = ANY(sqlc.arg('jobIds')::uuid[])
ORDER BY j.created_at, jdca.created_at;

-- name: RemoveJobConnectionDestinations :exec
DELETE FROM husonym_api.job_destination_connection_associations
WHERE id = ANY(sqlc.arg('jobIds')::uuid[]);

-- name: UpdateJobConnectionDestination :one
UPDATE husonym_api.job_destination_connection_associations
SET options = $1,
connection_id = $2
WHERE id = $3
RETURNING *;

-- name: RemoveJobConnectionDestination :exec
DELETE FROM husonym_api.job_destination_connection_associations WHERE id = $1;

-- name: GetAccountIdFromJobId :one
SELECT account_id
FROM husonym_api.jobs
WHERE id = $1
LIMIT 1;

-- name: DoesJobHaveConnectionId :one
SELECT EXISTS (
    SELECT 1
    FROM (
        -- Check direct associations in the job_destination_connection_associations table
        SELECT connection_id
        FROM husonym_api.job_destination_connection_associations
        WHERE job_id = sqlc.arg('jobId')
            AND connection_id = sqlc.arg('connectionId')

        UNION

        -- Check connection IDs embedded in the jobs table connection_options
        SELECT connection_id
        FROM (
            SELECT CASE
                -- Generate options FK source connection
                WHEN connection_options->'generateOptions'->>'fkSourceConnectionId' IS NOT NULL THEN
                    (connection_options->'generateOptions'->>'fkSourceConnectionId')::uuid
                -- Postgres connection
                WHEN connection_options->'postgresOptions'->>'connectionId' IS NOT NULL THEN
                    (connection_options->'postgresOptions'->>'connectionId')::uuid
                -- MSSQL connection
                WHEN connection_options->'mssqlOptions'->>'connectionId' IS NOT NULL THEN
                    (connection_options->'mssqlOptions'->>'connectionId')::uuid
                -- MySQL connection
                WHEN connection_options->'mysqlOptions'->>'connectionId' IS NOT NULL THEN
                    (connection_options->'mysqlOptions'->>'connectionId')::uuid
                -- Mongo connection
                WHEN connection_options->'mongoOptions'->>'connectionId' IS NOT NULL THEN
                    (connection_options->'mongoOptions'->>'connectionId')::uuid
                -- DynamoDB connection
                WHEN connection_options->'dynamoDBOptions'->>'connectionId' IS NOT NULL THEN
                    (connection_options->'dynamoDBOptions'->>'connectionId')::uuid
            END AS connection_id
            FROM husonym_api.jobs
            WHERE id = sqlc.arg('jobId')
        ) embedded_connections
        WHERE connection_id = sqlc.arg('connectionId')
    ) all_connections
);
