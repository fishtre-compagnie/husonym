-- name: CountInstances :one
-- How many instances a license was seen on lately, and whether this one is among them. An
-- instance whose last report is of a day before seen_since is not counted.
SELECT
    count(*) AS total,
    count(*) FILTER (WHERE instance_id = sqlc.arg(instance_id)) AS this_one
FROM controlplane.instances
WHERE license_id = sqlc.arg(license_id) AND last_report_day >= sqlc.arg(seen_since);

-- name: UpsertInstance :exec
-- An instance is first seen at the earliest reception of a report of it. What tells its latest
-- state is only replaced by the report of a later day, so that a late report of an earlier day,
-- a repeat or a conflict change nothing of it; the last news never moves backwards. A report
-- without the diagnostics does not erase the kind of installation already known.
INSERT INTO controlplane.instances AS i (
    license_id, instance_id, first_seen_at, last_seen_at, last_report_day, husonym_version, install_kind
) VALUES (
    sqlc.arg(license_id), sqlc.arg(instance_id), sqlc.arg(seen_at), sqlc.arg(seen_at), sqlc.arg(report_day),
    sqlc.arg(husonym_version), sqlc.narg(install_kind)
)
ON CONFLICT (license_id, instance_id) DO UPDATE SET
    first_seen_at = LEAST(i.first_seen_at, EXCLUDED.first_seen_at),
    last_seen_at = CASE WHEN EXCLUDED.last_report_day > i.last_report_day
        THEN GREATEST(i.last_seen_at, EXCLUDED.last_seen_at) ELSE i.last_seen_at END,
    husonym_version = CASE WHEN EXCLUDED.last_report_day > i.last_report_day
        THEN EXCLUDED.husonym_version ELSE i.husonym_version END,
    install_kind = CASE WHEN EXCLUDED.last_report_day > i.last_report_day
        THEN COALESCE(EXCLUDED.install_kind, i.install_kind) ELSE i.install_kind END,
    last_report_day = GREATEST(i.last_report_day, EXCLUDED.last_report_day);

-- name: InsertUsageReport :execrows
-- The first report received for a license, an instance and a day is the one that stays.
INSERT INTO controlplane.usage_reports (license_id, instance_id, day, document, seal, received_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (license_id, instance_id, day) DO NOTHING;

-- name: CountUsageReportConflict :execrows
-- Counts a report that differs from the one already stored for its license, instance and day.
-- No row is touched when the document is the same: that is a repeat.
UPDATE controlplane.usage_reports
SET conflicts = conflicts + 1, last_conflict_at = GREATEST(last_conflict_at, sqlc.arg(at))
WHERE license_id = sqlc.arg(license_id) AND instance_id = sqlc.arg(instance_id) AND day = sqlc.arg(day)
    AND document <> sqlc.arg(document);

-- name: CountSealRejection :exec
INSERT INTO controlplane.seal_rejections (license_id, day, count, last_at)
VALUES ($1, $2, 1, $3)
ON CONFLICT (license_id, day) DO UPDATE SET
    count = controlplane.seal_rejections.count + 1,
    last_at = EXCLUDED.last_at;

-- name: PendingReportExists :one
SELECT EXISTS (
    SELECT 1 FROM controlplane.pending_reports
    WHERE key_fingerprint = $1 AND instance_id = $2 AND day = $3 AND seal = $4
);

-- name: CountPendingReportsOfFingerprint :one
-- Read from the primary key, which starts with the fingerprint.
SELECT count(*) FROM controlplane.pending_reports
WHERE key_fingerprint = $1;

-- name: CountPendingReportsUpTo :one
-- How many reports are pending, counted no further than the cap it is compared with.
SELECT count(*) FROM (
    SELECT 1 FROM controlplane.pending_reports LIMIT sqlc.arg(up_to)::bigint
) AS counted;

-- name: InsertPendingReport :exec
INSERT INTO controlplane.pending_reports (key_fingerprint, instance_id, day, document, seal, received_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (key_fingerprint, instance_id, day, seal) DO NOTHING;

-- name: ListPendingReports :many
SELECT key_fingerprint, instance_id, day, document, seal, received_at
FROM controlplane.pending_reports
WHERE key_fingerprint = $1
ORDER BY day, instance_id, received_at, seal;

-- name: DeletePendingReport :execrows
DELETE FROM controlplane.pending_reports
WHERE key_fingerprint = $1 AND instance_id = $2 AND day = $3 AND seal = $4;

-- name: ListPendingFingerprintsNowKnown :many
SELECT DISTINCT p.key_fingerprint
FROM controlplane.pending_reports p
JOIN controlplane.licenses l ON l.key_fingerprint = p.key_fingerprint
ORDER BY p.key_fingerprint;

-- name: PurgePendingReports :execrows
DELETE FROM controlplane.pending_reports
WHERE received_at < $1;
