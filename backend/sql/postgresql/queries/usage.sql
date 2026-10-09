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
-- holds, so that the first end told wins. The moment the end is recorded is the clock of the
-- database, and a second end does not move it. The category and the step of the error go with
-- the status: null for a run that completed, both set for any other.
-- name: UpsertRunUsageEnded :exec
INSERT INTO husonym_api.run_usage (
  run_id, account_id, job_id, job_kind, status, error_category, error_step, started_at, ended_at,
  rows_read, rows_discarded, retries, tables_uncounted, source_version_major, recorded_at
) VALUES (
  sqlc.arg(run_id), sqlc.arg(account_id), sqlc.arg(job_id), sqlc.arg(job_kind), sqlc.arg(status),
  sqlc.narg(error_category), sqlc.narg(error_step),
  sqlc.arg(started_at), sqlc.arg(ended_at), sqlc.arg(rows_read), sqlc.arg(rows_discarded),
  sqlc.arg(retries), sqlc.arg(tables_uncounted), NULLIF(sqlc.arg(source_version_major)::text, ''),
  CURRENT_TIMESTAMP
)
ON CONFLICT (run_id) DO UPDATE SET
  status = EXCLUDED.status,
  error_category = EXCLUDED.error_category,
  error_step = EXCLUDED.error_step,
  ended_at = EXCLUDED.ended_at,
  rows_read = EXCLUDED.rows_read,
  rows_discarded = EXCLUDED.rows_discarded,
  retries = EXCLUDED.retries,
  tables_uncounted = EXCLUDED.tables_uncounted,
  source_version_major = EXCLUDED.source_version_major,
  recorded_at = EXCLUDED.recorded_at
WHERE husonym_api.run_usage.status = 'running';

-- Closes the row of a run still running, and creates nothing.
-- name: CloseRunUsage :exec
UPDATE husonym_api.run_usage
SET status = sqlc.arg(status), error_category = sqlc.narg(error_category),
  error_step = sqlc.narg(error_step), ended_at = sqlc.arg(ended_at), rows_read = sqlc.arg(rows_read),
  rows_discarded = sqlc.arg(rows_discarded), retries = sqlc.arg(retries),
  tables_uncounted = sqlc.arg(tables_uncounted),
  source_version_major = NULLIF(sqlc.arg(source_version_major)::text, ''),
  recorded_at = CURRENT_TIMESTAMP
WHERE run_id = sqlc.arg(run_id) AND status = 'running';

-- name: ListOpenRunUsageStartedBefore :many
SELECT run_id, account_id, started_at
FROM husonym_api.run_usage
WHERE status = 'running' AND started_at < $1
ORDER BY started_at, run_id;

-- Only a run still open is settled.
-- name: SettleRunUsage :exec
UPDATE husonym_api.run_usage
SET status = sqlc.arg(status), error_category = sqlc.narg(error_category),
  error_step = sqlc.narg(error_step), ended_at = sqlc.arg(ended_at), recorded_at = CURRENT_TIMESTAMP
WHERE run_id = sqlc.arg(run_id) AND status = 'running';

-- name: IncrementGateRefusal :exec
INSERT INTO husonym_api.gate_refusals_daily (day, account_id, gate, count)
VALUES ($1, $2, $3, 1)
ON CONFLICT (day, account_id, gate) DO UPDATE
SET count = husonym_api.gate_refusals_daily.count + 1;

-- A run counts for the UTC day on which the API recorded its end, whichever way it learned of
-- it: nothing recorded after midnight belongs to the day before. A run still running counts for
-- no day. The days counted run from the first given to the day before the second: a day and the
-- next one for the runs of a day, the first days of two months for the runs of a month.
-- name: CountRunUsageByStatusBetween :many
SELECT job_kind, status, count(*)::bigint AS runs
FROM husonym_api.run_usage
WHERE recorded_at >= (sqlc.arg(from_day)::date)::timestamp AT TIME ZONE 'UTC'
  AND recorded_at < (sqlc.arg(before_day)::date)::timestamp AT TIME ZONE 'UTC'
GROUP BY job_kind, status
ORDER BY job_kind, status;

-- Durations come from the runs that have an end only, and are never negative: an end told
-- before its start counts for nothing.
-- name: SumRunUsageBetween :one
SELECT
  count(*) FILTER (WHERE ended_at IS NOT NULL)::bigint AS runs_with_end,
  COALESCE(round(percentile_cont(0.5) WITHIN GROUP (
    ORDER BY GREATEST(extract(epoch FROM ended_at - started_at), 0)
  ) FILTER (WHERE ended_at IS NOT NULL)), 0)::bigint AS duration_median,
  COALESCE(round(percentile_cont(0.95) WITHIN GROUP (
    ORDER BY GREATEST(extract(epoch FROM ended_at - started_at), 0)
  ) FILTER (WHERE ended_at IS NOT NULL)), 0)::bigint AS duration_p95,
  COALESCE(sum(rows_read), 0)::bigint AS rows_read,
  COALESCE(sum(rows_discarded), 0)::bigint AS rows_discarded,
  COALESCE(sum(retries), 0)::bigint AS retries,
  count(*) FILTER (WHERE tables_uncounted > 0)::bigint AS with_uncounted_rows
FROM husonym_api.run_usage
WHERE recorded_at >= (sqlc.arg(from_day)::date)::timestamp AT TIME ZONE 'UTC'
  AND recorded_at < (sqlc.arg(before_day)::date)::timestamp AT TIME ZONE 'UTC';

-- name: CountRunUsageBySourceVersionOfDay :many
SELECT job_id, source_version_major, count(*)::bigint AS runs
FROM husonym_api.run_usage
WHERE source_version_major IS NOT NULL
  AND recorded_at >= ($1::date)::timestamp AT TIME ZONE 'UTC'
  AND recorded_at < (($1::date) + 1)::timestamp AT TIME ZONE 'UTC'
GROUP BY job_id, source_version_major
ORDER BY job_id, source_version_major;

-- The days counted run from the first given to the day before the second, as for the runs.
-- name: SumGateRefusalsBetween :many
SELECT gate, sum(count)::bigint AS refusals
FROM husonym_api.gate_refusals_daily
WHERE day >= sqlc.arg(from_day)::date AND day < sqlc.arg(before_day)::date
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

-- The reports of the days from the first given to the day before the second, the oldest first.
-- name: ListUsageReportsBetween :many
SELECT day, document, seal, key_fingerprint, prepared_at
FROM husonym_api.usage_reports
WHERE day >= sqlc.arg(from_day)::date AND day < sqlc.arg(before_day)::date
ORDER BY day;

-- name: DeleteUsageReportsBefore :exec
DELETE FROM husonym_api.usage_reports
WHERE day < $1;

-- name: GetSendingSince :one
SELECT sending_since
FROM husonym_api.instance;

-- Only an instance that does not send yet starts: the first date stays.
-- name: StartUsageSending :exec
UPDATE husonym_api.instance
SET sending_since = $1
WHERE sending_since IS NULL;

-- name: StopUsageSending :exec
UPDATE husonym_api.instance
SET sending_since = NULL;

-- One statement takes the oldest report that is due and marks the attempt: a report another
-- call holds is skipped, so two calls never get the same one. When a bound is given on the
-- preparation, a report prepared after it is not due yet. When the reports are to leave without
-- the diagnostics, a report whose document carries them is not due at all. Nothing is due once
-- the instance was told not to send: a call that still believes it sends gets no report.
-- name: ClaimUsageReport :one
UPDATE husonym_api.usage_reports
SET attempts = attempts + 1, last_attempt_at = sqlc.arg(now)
WHERE day = (
  SELECT r.day
  FROM husonym_api.usage_reports r
  WHERE r.sent_at IS NULL
    AND EXISTS (SELECT 1 FROM husonym_api.instance i WHERE i.sending_since IS NOT NULL)
    AND r.day >= sqlc.arg(from_day) AND r.day <= sqlc.arg(to_day)
    AND (r.last_attempt_at IS NULL OR r.last_attempt_at < sqlc.arg(not_attempted_since))
    AND (sqlc.narg(prepared_by)::timestamptz IS NULL OR r.prepared_at <= sqlc.narg(prepared_by)::timestamptz)
    AND NOT (sqlc.arg(without_diagnostics)::boolean AND r.document::jsonb ? 'diagnostics')
  ORDER BY r.day
  LIMIT 1
  FOR UPDATE SKIP LOCKED
)
RETURNING day, document, seal, key_fingerprint, prepared_at;

-- A report already sent keeps the date it was sent on.
-- name: MarkUsageReportSent :exec
UPDATE husonym_api.usage_reports
SET sent_at = $2
WHERE day = $1 AND sent_at IS NULL;

-- Tells of each report whether its document carries the diagnostics, as the claim reads it.
-- name: ListUsageReportSendings :many
SELECT day, prepared_at, sent_at, last_attempt_at, attempts,
  (document::jsonb ? 'diagnostics')::boolean AS carries_diagnostics
FROM husonym_api.usage_reports
WHERE day >= $1 AND day <= $2
ORDER BY day DESC;

-- name: GetLastUsageReportSentAt :one
SELECT max(sent_at)::timestamptz AS sent_at
FROM husonym_api.usage_reports;
