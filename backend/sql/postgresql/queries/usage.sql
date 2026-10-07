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
  rows_read, rows_discarded, retries, tables_uncounted, source_version_major
) VALUES (
  sqlc.arg(run_id), sqlc.arg(account_id), sqlc.arg(job_id), sqlc.arg(job_kind), sqlc.arg(status),
  sqlc.arg(started_at), sqlc.arg(ended_at), sqlc.arg(rows_read), sqlc.arg(rows_discarded),
  sqlc.arg(retries), sqlc.arg(tables_uncounted), NULLIF(sqlc.arg(source_version_major)::text, '')
)
ON CONFLICT (run_id) DO UPDATE SET
  status = EXCLUDED.status,
  ended_at = EXCLUDED.ended_at,
  rows_read = EXCLUDED.rows_read,
  rows_discarded = EXCLUDED.rows_discarded,
  retries = EXCLUDED.retries,
  tables_uncounted = EXCLUDED.tables_uncounted,
  source_version_major = EXCLUDED.source_version_major
WHERE husonym_api.run_usage.status = 'running';

-- Closes the row of a run still running, and creates nothing.
-- name: CloseRunUsage :exec
UPDATE husonym_api.run_usage
SET status = sqlc.arg(status), ended_at = sqlc.arg(ended_at), rows_read = sqlc.arg(rows_read),
  rows_discarded = sqlc.arg(rows_discarded), retries = sqlc.arg(retries),
  tables_uncounted = sqlc.arg(tables_uncounted),
  source_version_major = NULLIF(sqlc.arg(source_version_major)::text, '')
WHERE run_id = sqlc.arg(run_id) AND status = 'running';

-- name: ListOpenRunUsageStartedBefore :many
SELECT run_id, account_id, started_at
FROM husonym_api.run_usage
WHERE status = 'running' AND started_at < $1
ORDER BY started_at, run_id;

-- Only a run still open is settled.
-- name: SettleRunUsage :exec
UPDATE husonym_api.run_usage
SET status = $2, ended_at = $3, settled_at = CURRENT_TIMESTAMP
WHERE run_id = $1 AND status = 'running';

-- name: IncrementGateRefusal :exec
INSERT INTO husonym_api.gate_refusals_daily (day, account_id, gate, count)
VALUES ($1, $2, $3, 1)
ON CONFLICT (day, account_id, gate) DO UPDATE
SET count = husonym_api.gate_refusals_daily.count + 1;

-- A run counts for the UTC day of its end, or of its settling when it has no end. A run still
-- running counts for no day.
-- name: CountRunUsageByStatusOfDay :many
SELECT job_kind, status, count(*)::bigint AS runs
FROM husonym_api.run_usage
WHERE COALESCE(ended_at, settled_at) >= ($1::date)::timestamp AT TIME ZONE 'UTC'
  AND COALESCE(ended_at, settled_at) < (($1::date) + 1)::timestamp AT TIME ZONE 'UTC'
GROUP BY job_kind, status
ORDER BY job_kind, status;

-- Durations come from the runs that have an end only.
-- name: SumRunUsageOfDay :one
SELECT
  count(*) FILTER (WHERE ended_at IS NOT NULL)::bigint AS runs_with_end,
  COALESCE(round(percentile_cont(0.5) WITHIN GROUP (
    ORDER BY extract(epoch FROM ended_at - started_at)
  ) FILTER (WHERE ended_at IS NOT NULL)), 0)::bigint AS duration_median,
  COALESCE(round(percentile_cont(0.95) WITHIN GROUP (
    ORDER BY extract(epoch FROM ended_at - started_at)
  ) FILTER (WHERE ended_at IS NOT NULL)), 0)::bigint AS duration_p95,
  COALESCE(sum(rows_read), 0)::bigint AS rows_read,
  COALESCE(sum(rows_discarded), 0)::bigint AS rows_discarded,
  COALESCE(sum(retries), 0)::bigint AS retries,
  count(*) FILTER (WHERE tables_uncounted > 0)::bigint AS with_uncounted_rows
FROM husonym_api.run_usage
WHERE COALESCE(ended_at, settled_at) >= ($1::date)::timestamp AT TIME ZONE 'UTC'
  AND COALESCE(ended_at, settled_at) < (($1::date) + 1)::timestamp AT TIME ZONE 'UTC';

-- name: CountRunUsageBySourceVersionOfDay :many
SELECT job_id, source_version_major, count(*)::bigint AS runs
FROM husonym_api.run_usage
WHERE source_version_major IS NOT NULL
  AND COALESCE(ended_at, settled_at) >= ($1::date)::timestamp AT TIME ZONE 'UTC'
  AND COALESCE(ended_at, settled_at) < (($1::date) + 1)::timestamp AT TIME ZONE 'UTC'
GROUP BY job_id, source_version_major
ORDER BY job_id, source_version_major;

-- name: SumGateRefusalsOfDay :many
SELECT gate, sum(count)::bigint AS refusals
FROM husonym_api.gate_refusals_daily
WHERE day = $1
GROUP BY gate
ORDER BY gate;

-- Only a later day moves the date.
-- name: UpsertUserActivity :exec
INSERT INTO husonym_api.user_activity (user_id, last_seen_on)
VALUES ($1, $2)
ON CONFLICT (user_id) DO UPDATE
SET last_seen_on = EXCLUDED.last_seen_on
WHERE husonym_api.user_activity.last_seen_on < EXCLUDED.last_seen_on;

-- name: CountUsersSeenSince :one
SELECT count(*)::bigint
FROM husonym_api.user_activity
WHERE last_seen_on >= $1;

-- The report of a day already there is left as it is. The count of rows tells whether this
-- call made it.
-- name: InsertUsageReport :execrows
INSERT INTO husonym_api.usage_reports (day, document, seal, key_fingerprint, prepared_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (day) DO NOTHING;

-- name: GetUsageReport :one
SELECT day, document, seal, key_fingerprint, prepared_at
FROM husonym_api.usage_reports
WHERE day = $1;

-- name: DeleteUsageReportsBefore :exec
DELETE FROM husonym_api.usage_reports
WHERE day < $1;
