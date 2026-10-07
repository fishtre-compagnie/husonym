-- The usage counters belong to the instance. They hold counts and identifiers, never a name or
-- a message a customer entered.

-- name: GetInstanceId :one
SELECT id
FROM husonym_api.instance;

-- A run already there is left as it is.
-- name: InsertRunUsageStarted :exec
INSERT INTO husonym_api.run_usage (
  run_id, account_id, job_id, job_kind, status, started_at
) VALUES (
  $1, $2, $3, $4, 'running', $5
)
ON CONFLICT (run_id) DO NOTHING;

-- Creates the row when the start was never recorded; a row already finished keeps what it
-- holds, so that the first end told wins.
-- name: UpsertRunUsageEnded :exec
INSERT INTO husonym_api.run_usage (
  run_id, account_id, job_id, job_kind, status, started_at, ended_at,
  rows_read, rows_discarded, retries
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
)
ON CONFLICT (run_id) DO UPDATE SET
  status = EXCLUDED.status,
  ended_at = EXCLUDED.ended_at,
  rows_read = EXCLUDED.rows_read,
  rows_discarded = EXCLUDED.rows_discarded,
  retries = EXCLUDED.retries
WHERE husonym_api.run_usage.status = 'running';

-- name: ListOpenRunUsageStartedBefore :many
SELECT run_id, account_id, started_at
FROM husonym_api.run_usage
WHERE status = 'running' AND started_at < $1
ORDER BY started_at, run_id;

-- Only a run still open is settled.
-- name: SettleRunUsage :exec
UPDATE husonym_api.run_usage
SET status = $2, ended_at = $3
WHERE run_id = $1 AND status = 'running';

-- name: IncrementGateRefusal :exec
INSERT INTO husonym_api.gate_refusals_daily (day, account_id, gate, count)
VALUES ($1, $2, $3, 1)
ON CONFLICT (day, account_id, gate) DO UPDATE
SET count = husonym_api.gate_refusals_daily.count + 1;
