-- name: UpsertPiiScanResult :exec
INSERT INTO husonym_api.pii_scan_results (
  connection_id, schema_name, table_name, columns, result, scanned_at
) VALUES (
  $1, $2, $3, $4, $5, CURRENT_TIMESTAMP
)
ON CONFLICT ON CONSTRAINT pii_scan_results_pkey DO UPDATE
SET columns = EXCLUDED.columns,
    result = EXCLUDED.result,
    scanned_at = EXCLUDED.scanned_at;

-- name: GetPiiScanResultsByConnection :many
SELECT * FROM husonym_api.pii_scan_results
WHERE connection_id = $1
ORDER BY schema_name, table_name;

-- name: GetPiiScanResultsByTables :many
-- The tables are given as two arrays of the same length, the schema of each table and its
-- name, which unnest reads side by side.
SELECT * FROM husonym_api.pii_scan_results
WHERE connection_id = sqlc.arg(connection_id)
  AND (schema_name, table_name) IN (
    SELECT unnest(sqlc.arg(schema_names)::text[]), unnest(sqlc.arg(table_names)::text[])
  )
ORDER BY schema_name, table_name;
