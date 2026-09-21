-- name: GetColumnReviewsByJob :many
SELECT * from husonym_api.column_reviews
WHERE job_id = sqlc.arg('jobId')
  AND account_id = sqlc.arg('accountId')
ORDER BY table_schema, table_name, column_name;

-- name: GetColumnReviewsByAccount :many
SELECT * from husonym_api.column_reviews
WHERE account_id = sqlc.arg('accountId')
ORDER BY job_id, table_schema, table_name, column_name;

-- name: SetColumnReview :one
INSERT INTO husonym_api.column_reviews (
    account_id,
    job_id,
    table_schema,
    table_name,
    column_name,
    reviewed_data_type,
    reviewed_pii_category,
    note,
    created_by_id,
    updated_by_id
)
VALUES (
    $1,  -- account_id
    $2,  -- job_id
    $3,  -- table_schema
    $4,  -- table_name
    $5,  -- column_name
    $6,  -- reviewed_data_type
    $7,  -- reviewed_pii_category
    $8,  -- note
    $9,  -- created_by_id
    $10  -- updated_by_id
)
ON CONFLICT (job_id, table_schema, table_name, column_name)
DO UPDATE SET
    reviewed_data_type = EXCLUDED.reviewed_data_type,
    reviewed_pii_category = EXCLUDED.reviewed_pii_category,
    note = EXCLUDED.note,
    updated_by_id = EXCLUDED.updated_by_id
RETURNING *;

-- name: RemoveColumnReview :execrows
DELETE FROM husonym_api.column_reviews
WHERE job_id = sqlc.arg('jobId')
  AND account_id = sqlc.arg('accountId')
  AND table_schema = sqlc.arg('tableSchema')
  AND table_name = sqlc.arg('tableName')
  AND column_name = sqlc.arg('columnName');
